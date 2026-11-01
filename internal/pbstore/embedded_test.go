package pbstore_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"github.com/incurl/flint/internal/pbstore"
)

// newEmbeddedStore creates an embedded store backed by a temporary
// PocketBase data directory.
func newEmbeddedStore(t *testing.T) *pbstore.Embedded {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "pb_data")

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dir,
		HideStartBanner: true,
	})

	store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{
		DataDir: dir,
	})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}

	t.Cleanup(func() { _ = store.Close() })

	return store
}

// TestEmbeddedCRUD exercises the full record life cycle against a real
// embedded PocketBase database.
func TestEmbeddedCRUD(t *testing.T) {
	store := newEmbeddedStore(t)
	ctx := context.Background()

	// Create the dictionary collection.
	collection, err := store.EnsureDictionaryCollection(ctx, "vocab", []pbstore.DictionaryField{
		{Name: "term", Type: pbstore.FieldTypeText, Required: true},
		{Name: "weight", Type: pbstore.FieldTypeNumber},
	})
	if err != nil {
		t.Fatalf("EnsureDictionaryCollection() error = %v", err)
	}

	if collection.Name != "vocab" {
		t.Errorf("collection name = %q, want %q", collection.Name, "vocab")
	}

	// The call must be idempotent.
	again, err := store.EnsureDictionaryCollection(ctx, "vocab", nil)
	if err != nil {
		t.Fatalf("EnsureDictionaryCollection() second call error = %v", err)
	}

	if again.ID != collection.ID {
		t.Errorf("collection id = %q, want %q", again.ID, collection.ID)
	}

	// Create a record.
	rec, err := store.Create(ctx, "vocab", map[string]any{
		"key":    "go",
		"label":  "Go",
		"term":   "golang",
		"weight": 3,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if rec.ID == "" {
		t.Error("Create() returned an empty id")
	}

	if got := rec.StringField("label"); got != "Go" {
		t.Errorf("label = %q, want %q", got, "Go")
	}

	// Read it back.
	got, err := store.Get(ctx, "vocab", rec.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.StringField("key") != "go" {
		t.Errorf("key = %q, want %q", got.StringField("key"), "go")
	}

	// Update it.
	updated, err := store.Update(ctx, "vocab", rec.ID, map[string]any{
		"label": "Golang",
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if updated.StringField("label") != "Golang" {
		t.Errorf("label = %q, want %q", updated.StringField("label"), "Golang")
	}

	// Count the records.
	count, err := store.Count(ctx, "vocab", "")
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}

	if count != 1 {
		t.Errorf("Count() = %d, want 1", count)
	}

	// Delete it.
	if err := store.Delete(ctx, "vocab", rec.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	count, err = store.Count(ctx, "vocab", "")
	if err != nil {
		t.Fatalf("Count() after delete error = %v", err)
	}

	if count != 0 {
		t.Errorf("Count() after delete = %d, want 0", count)
	}
}

// TestEmbeddedListFilters verifies that the native PocketBase filter and
// sort expressions are honoured.
func TestEmbeddedListFilters(t *testing.T) {
	store := newEmbeddedStore(t)
	ctx := context.Background()

	if _, err := store.EnsureDictionaryCollection(ctx, "items", []pbstore.DictionaryField{
		{Name: "rank", Type: pbstore.FieldTypeNumber},
	}); err != nil {
		t.Fatalf("EnsureDictionaryCollection() error = %v", err)
	}

	seeds := []map[string]any{
		{"key": "alpha", "label": "Alpha", "rank": 1},
		{"key": "beta", "label": "Beta", "rank": 2},
		{"key": "gamma", "label": "Gamma", "rank": 3},
	}
	for _, seed := range seeds {
		if _, err := store.Create(ctx, "items", seed); err != nil {
			t.Fatalf("Create(%v) error = %v", seed, err)
		}
	}

	tests := []struct {
		name string
		// query is the listing request.
		query pbstore.Query
		// want is the expected number of records in the returned page.
		want int
		// wantTotal is the expected total number of matching records.
		wantTotal int
		// first is the expected "key" of the first returned record.
		first string
	}{
		{
			name:      "no filter",
			query:     pbstore.Query{Collection: "items"},
			want:      3,
			wantTotal: 3,
			first:     "alpha",
		},
		{
			name:      "exact match",
			query:     pbstore.Query{Collection: "items", Filter: `key = "beta"`},
			want:      1,
			wantTotal: 1,
			first:     "beta",
		},
		{
			name:      "like match",
			query:     pbstore.Query{Collection: "items", Filter: `key ~ "a"`},
			want:      3,
			wantTotal: 3,
		},
		{
			name:      "and expression",
			query:     pbstore.Query{Collection: "items", Filter: `rank > 1 && rank < 3`},
			want:      1,
			wantTotal: 1,
			first:     "beta",
		},
		{
			name:      "descending sort",
			query:     pbstore.Query{Collection: "items", Sort: "-key"},
			want:      3,
			wantTotal: 3,
			first:     "gamma",
		},
		{
			name: "pagination",
			// The page holds a single record but the total count still
			// reflects every matching record.
			query:     pbstore.Query{Collection: "items", Page: 2, PerPage: 1, Sort: "key"},
			want:      1,
			wantTotal: 3,
			first:     "beta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := store.List(ctx, tt.query)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}

			if len(result.Records) != tt.want {
				t.Errorf("List() returned %d records, want %d", len(result.Records), tt.want)
			}

			if tt.first != "" && len(result.Records) > 0 {
				if got := result.Records[0].StringField("key"); got != tt.first {
					t.Errorf("first record key = %q, want %q", got, tt.first)
				}
			}

			if result.TotalItems != tt.wantTotal {
				t.Errorf("TotalItems = %d, want %d", result.TotalItems, tt.wantTotal)
			}
		})
	}
}

// TestEmbeddedInvalidFilter verifies that an invalid filter is reported as
// an error instead of panicking.
func TestEmbeddedInvalidFilter(t *testing.T) {
	store := newEmbeddedStore(t)
	ctx := context.Background()

	if _, err := store.EnsureDictionaryCollection(ctx, "items", nil); err != nil {
		t.Fatalf("EnsureDictionaryCollection() error = %v", err)
	}

	_, err := store.List(ctx, pbstore.Query{
		Collection: "items",
		Filter:     `key = `,
	})
	if err == nil {
		t.Fatal("List() with an invalid filter returned no error")
	}
}

// TestEmbeddedAllowlist verifies that the collection allowlist is enforced.
func TestEmbeddedAllowlist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pb_data")

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dir,
		HideStartBanner: true,
	})

	store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{
		DataDir:     dir,
		Collections: []string{"allowed"},
	})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	if _, err := store.EnsureDictionaryCollection(ctx, "allowed", nil); err != nil {
		t.Fatalf("EnsureDictionaryCollection() error = %v", err)
	}

	if _, err := store.EnsureDictionaryCollection(ctx, "denied", nil); err != nil {
		t.Fatalf("EnsureDictionaryCollection() error = %v", err)
	}

	// The denied collection must not be queryable.
	if _, err := store.List(ctx, pbstore.Query{Collection: "denied"}); err == nil {
		t.Error("List() on a denied collection returned no error")
	}

	// It must not be listed either.
	collections, err := store.Collections(ctx)
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}

	for _, c := range collections {
		if c.Name == "denied" {
			t.Error("Collections() returned the denied collection")
		}
	}
}

// TestEmbeddedRuntimeStore verifies that the native PocketBase runtime
// store is exposed and usable.
func TestEmbeddedRuntimeStore(t *testing.T) {
	store := newEmbeddedStore(t)

	rt := store.RuntimeStore()
	if rt == nil {
		t.Fatal("RuntimeStore() returned nil")
	}

	rt.Set("flint:test", "value")

	if got := rt.Get("flint:test"); got != "value" {
		t.Errorf("RuntimeStore().Get() = %v, want %q", got, "value")
	}

	if !rt.Has("flint:test") {
		t.Error("RuntimeStore().Has() = false, want true")
	}
}

// TestEmbeddedHealthCheck verifies the health check of the embedded store.
func TestEmbeddedHealthCheck(t *testing.T) {
	store := newEmbeddedStore(t)

	status, err := store.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}

	if status == "" {
		t.Error("HealthCheck() returned an empty status")
	}
}

// TestEmbeddedClosedStore verifies that a closed store rejects the calls.
func TestEmbeddedClosedStore(t *testing.T) {
	store := newEmbeddedStore(t)

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	// Closing twice must be a no-op.
	if err := store.Close(); err != nil {
		t.Fatalf("Close() second call error = %v", err)
	}

	if _, err := store.Collections(context.Background()); err == nil {
		t.Error("Collections() on a closed store returned no error")
	}
}

// TestEmbeddedMigrations verifies that the application migrations are
// applied on startup.
func TestEmbeddedMigrations(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pb_data")

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dir,
		HideStartBanner: true,
	})

	store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{
		DataDir: dir,
		Migrations: []core.Migration{{
			File: "9999999999_test_collection.go",
			Up: func(txApp core.App) error {
				collection := core.NewBaseCollection("migrated")
				collection.Fields.Add(&core.TextField{Name: "value"})

				return txApp.Save(collection)
			},
			Down: func(txApp core.App) error { return nil },
		}},
	})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	collections, err := store.Collections(context.Background())
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}

	found := false

	for _, c := range collections {
		if c.Name == "migrated" {
			found = true
			break
		}
	}

	if !found {
		t.Error("the migrated collection was not created")
	}
}

// TestEmbeddedSyncSendsToken verifies that the sync authenticates against
// the remote source with the configured token.
//
// Without the token the remote server answers with an empty list for a
// non-public collection, so the sync would silently replicate nothing.
func TestEmbeddedSyncSendsToken(t *testing.T) {
	const token = "sync-secret-token"

	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"page": 1,
			"perPage": 30,
			"totalItems": 1,
			"totalPages": 1,
			"items": [{
				"id": "remote1",
				"collectionId": "col1",
				"collectionName": "vocab",
				"key": "go",
				"label": "Go"
			}]
		}`))
	}))
	defer server.Close()

	dir := filepath.Join(t.TempDir(), "pb_data")

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dir,
		HideStartBanner: true,
	})

	store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{
		DataDir:         dir,
		SyncRemoteURL:   server.URL,
		SyncRemoteToken: token,
	})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	report, err := store.SyncCollection(context.Background(), pbstore.SyncRequest{
		RemoteCollection: "vocab",
		LocalCollection:  "vocab",
	})
	if err != nil {
		t.Fatalf("SyncCollection() error = %v", err)
	}

	if gotAuth != token {
		t.Errorf("Authorization header = %q, want %q", gotAuth, token)
	}

	if report.Fetched != 1 {
		t.Errorf("Fetched = %d, want 1", report.Fetched)
	}
}

// TestEmbeddedSyncCreatesLocalCollection verifies that a sync creates the
// local collection when it does not exist yet.
//
// The sync source is an httptest server that mimics the PocketBase
// records endpoint.
func TestEmbeddedSyncCreatesLocalCollection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"page": 1,
			"perPage": 30,
			"totalItems": 1,
			"totalPages": 1,
			"items": [{
				"id": "remote1",
				"collectionId": "col1",
				"collectionName": "vocab",
				"key": "go",
				"label": "Go"
			}]
		}`))
	}))
	defer server.Close()

	dir := filepath.Join(t.TempDir(), "pb_data")

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dir,
		HideStartBanner: true,
	})

	store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{
		DataDir:       dir,
		SyncRemoteURL: server.URL,
	})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	report, err := store.SyncCollection(ctx, pbstore.SyncRequest{
		RemoteCollection: "vocab",
		LocalCollection:  "vocab",
	})
	if err != nil {
		t.Fatalf("SyncCollection() error = %v", err)
	}

	if report.Fetched != 1 {
		t.Errorf("Fetched = %d, want 1", report.Fetched)
	}

	if report.Created != 1 {
		t.Errorf("Created = %d, want 1", report.Created)
	}

	// The record must be readable from the local store.
	rec, err := store.Get(ctx, "vocab", "remote1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got := rec.StringField("label"); got != "Go" {
		t.Errorf("label = %q, want %q", got, "Go")
	}

	// A second sync must update the existing record instead of
	// duplicating it.
	report, err = store.SyncCollection(ctx, pbstore.SyncRequest{
		RemoteCollection: "vocab",
		LocalCollection:  "vocab",
	})
	if err != nil {
		t.Fatalf("SyncCollection() second call error = %v", err)
	}

	if report.Created != 0 {
		t.Errorf("Created = %d on the second sync, want 0", report.Created)
	}

	if report.Updated != 1 {
		t.Errorf("Updated = %d on the second sync, want 1", report.Updated)
	}

	count, err := store.Count(ctx, "vocab", "")
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}

	if count != 1 {
		t.Errorf("Count() = %d, want 1", count)
	}
}
