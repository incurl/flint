package pbstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/incurl/flint/internal/pbstore"
)

// TestRemoteList verifies that the remote backend speaks the native
// PocketBase REST API.
func TestRemoteList(t *testing.T) {
	var (
		gotPath  string
		gotQuery string
		gotAuth  string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"page":       1,
			"perPage":    30,
			"totalItems": 2,
			"totalPages": 1,
			"items": []map[string]any{
				{
					"id":             "rec1",
					"collectionId":   "col1",
					"collectionName": "items",
					"key":            "alpha",
					"title":          "Alpha",
				},
				{
					"id":    "rec2",
					"key":   "beta",
					"title": "Beta",
				},
			},
		})
	}))
	defer server.Close()

	store, err := pbstore.OpenRemote(pbstore.RemoteConfig{
		BaseURL: server.URL,
		Token:   "secret-token",
	})
	if err != nil {
		t.Fatalf("OpenRemote() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	result, err := store.List(context.Background(), pbstore.Query{
		Collection: "items",
		Filter:     `key = "alpha"`,
		Sort:       "key",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if gotPath != "/api/collections/items/records" {
		t.Errorf("request path = %q, want %q", gotPath, "/api/collections/items/records")
	}

	if gotAuth != "secret-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "secret-token")
	}

	if gotQuery == "" {
		t.Error("the request carried no query parameters")
	}

	if len(result.Records) != 2 {
		t.Fatalf("List() returned %d records, want 2", len(result.Records))
	}

	if result.Records[0].ID != "rec1" {
		t.Errorf("first record id = %q, want %q", result.Records[0].ID, "rec1")
	}

	if result.Records[0].StringField("title") != "Alpha" {
		t.Errorf("first record title = %q, want %q", result.Records[0].StringField("title"), "Alpha")
	}

	if result.TotalItems != 2 {
		t.Errorf("TotalItems = %d, want 2", result.TotalItems)
	}
}

// TestRemoteGetUnwrapsEnvelope verifies that the single record envelope is
// handled.
func TestRemoteGetUnwrapsEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"id":    "rec1",
				"title": "Alpha",
			},
		})
	}))
	defer server.Close()

	store, err := pbstore.OpenRemote(pbstore.RemoteConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("OpenRemote() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	rec, err := store.Get(context.Background(), "items", "rec1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if rec.ID != "rec1" {
		t.Errorf("record id = %q, want %q", rec.ID, "rec1")
	}

	if rec.StringField("title") != "Alpha" {
		t.Errorf("record title = %q, want %q", rec.StringField("title"), "Alpha")
	}
}

// TestRemoteErrorStatus verifies that an HTTP error is reported as an
// error and not as a panic.
func TestRemoteErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"status":403,"message":"forbidden"}`))
	}))
	defer server.Close()

	store, err := pbstore.OpenRemote(pbstore.RemoteConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("OpenRemote() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	_, err = store.List(context.Background(), pbstore.Query{Collection: "items"})
	if err == nil {
		t.Fatal("List() returned no error for a 403 response")
	}
}

// TestRemoteNotFound verifies that a 404 response maps to
// [pbstore.ErrNotFound].
func TestRemoteNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	store, err := pbstore.OpenRemote(pbstore.RemoteConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("OpenRemote() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	_, err = store.Get(context.Background(), "items", "missing")
	if err == nil {
		t.Fatal("Get() returned no error for a 404 response")
	}
}

// TestRemoteHealthCheck verifies the health check of the remote backend.
func TestRemoteHealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "API is healthy."})
	}))
	defer server.Close()

	store, err := pbstore.OpenRemote(pbstore.RemoteConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("OpenRemote() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	status, err := store.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}

	if status == "" {
		t.Error("HealthCheck() returned an empty status")
	}
}

// TestRemoteUnreachable verifies that an unreachable server is reported as
// an error.
func TestRemoteUnreachable(t *testing.T) {
	// A closed server is used to simulate an unreachable host.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	store, err := pbstore.OpenRemote(pbstore.RemoteConfig{BaseURL: url})
	if err != nil {
		t.Fatalf("OpenRemote() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if _, err := store.HealthCheck(context.Background()); err == nil {
		t.Error("HealthCheck() returned no error for an unreachable server")
	}
}

// TestRemoteInvalidURL verifies that an invalid URL is rejected.
func TestRemoteInvalidURL(t *testing.T) {
	tests := []string{"", "not-a-url", "ftp://example.com"}

	for _, url := range tests {
		if _, err := pbstore.OpenRemote(pbstore.RemoteConfig{BaseURL: url}); err == nil {
			t.Errorf("OpenRemote(%q) returned no error", url)
		}
	}
}

// TestRemoteUnsupportedOperations verifies that the remote backend rejects
// the operations that require a local database.
func TestRemoteUnsupportedOperations(t *testing.T) {
	store, err := pbstore.OpenRemote(pbstore.RemoteConfig{BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("OpenRemote() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	if _, err := store.EnsureDictionaryCollection(ctx, "vocab", nil); err == nil {
		t.Error("EnsureDictionaryCollection() returned no error")
	}

	if _, err := store.SyncCollection(ctx, pbstore.SyncRequest{RemoteCollection: "vocab"}); err == nil {
		t.Error("SyncCollection() returned no error")
	}
}

// TestRemoteAllowlist verifies that the collection allowlist is enforced.
func TestRemoteAllowlist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "totalItems": 0})
	}))
	defer server.Close()

	store, err := pbstore.OpenRemote(pbstore.RemoteConfig{
		BaseURL:     server.URL,
		Collections: []string{"allowed"},
	})
	if err != nil {
		t.Fatalf("OpenRemote() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if _, err := store.List(context.Background(), pbstore.Query{Collection: "denied"}); err == nil {
		t.Error("List() on a denied collection returned no error")
	}
}

// TestRemoteCollectionSchema verifies that the remote backend reads the
// schema of a collection from the collections endpoint.
func TestRemoteCollectionSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/collections" {
			t.Errorf("request path = %q, want %q", r.URL.Path, "/api/collections")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"page":       1,
			"perPage":    200,
			"totalItems": 1,
			"totalPages": 1,
			"items": []map[string]any{
				{
					"name": "items",
					"type": "base",
					"fields": []map[string]any{
						{"name": "title", "type": "text", "required": true, "system": false},
						{"name": "kind", "type": "select", "options": []string{"a", "b"}},
						{"name": "id", "type": "text", "system": true},
					},
				},
			},
		})
	}))
	defer server.Close()

	store, err := pbstore.OpenRemote(pbstore.RemoteConfig{
		BaseURL: server.URL,
		Token:   "secret-token",
	})
	if err != nil {
		t.Fatalf("OpenRemote() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	fields, err := store.CollectionSchema(context.Background(), "items")
	if err != nil {
		t.Fatalf("CollectionSchema() error = %v", err)
	}

	// The system field is left out.
	if len(fields) != 2 {
		t.Fatalf("the schema has %d fields, want 2: %+v", len(fields), fields)
	}

	if fields[0].Name != "title" || fields[0].Type != pbstore.FieldTypeText {
		t.Errorf("the first field = %+v, want the title field", fields[0])
	}

	if !fields[0].Required {
		t.Error("the title field is not required, want required")
	}

	if len(fields[1].Options) != 2 {
		t.Errorf("the kind field has %d options, want 2", len(fields[1].Options))
	}
}

// TestRemoteCollectionSchemaUnknown verifies that a collection that is
// not on the server is an error, and not an empty schema.
func TestRemoteCollectionSchemaUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"page":       1,
			"perPage":    200,
			"totalItems": 0,
			"totalPages": 0,
			"items":      []map[string]any{},
		})
	}))
	defer server.Close()

	store, err := pbstore.OpenRemote(pbstore.RemoteConfig{
		BaseURL: server.URL,
		Token:   "secret-token",
	})
	if err != nil {
		t.Fatalf("OpenRemote() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	_, err = store.CollectionSchema(context.Background(), "nothing")
	if err == nil {
		t.Fatal("CollectionSchema() error = nil, want an error")
	}

	if !errors.Is(err, pbstore.ErrNotFound) {
		t.Errorf("CollectionSchema() error = %v, want ErrNotFound", err)
	}
}
