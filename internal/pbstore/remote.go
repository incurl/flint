package pbstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// RemoteConfig configures a [Remote] store.
type RemoteConfig struct {
	// BaseURL is the root URL of the PocketBase server,
	// eg. "http://127.0.0.1:8090".
	BaseURL string
	// Token is the API token used to authenticate the requests.
	//
	// It is sent as the `Authorization` header, exactly like the
	// PocketBase admin UI does.
	//
	// SECURITY: never hardcode a token in a committed file. Prefer the
	// `FLINT_REMOTE_TOKEN` environment variable or an untracked config
	// file.
	Token string
	// Collections restricts the collections that can be queried.
	//
	// An empty list allows every collection.
	Collections []string
	// Timeout limits the duration of a single HTTP request.
	//
	// Zero falls back to [DefaultRemoteTimeout].
	Timeout time.Duration
	// HTTPClient overrides the http.Client used for the requests.
	//
	// It is mostly useful for tests.
	HTTPClient *http.Client
	// RetryAttempts is the number of extra attempts performed when a
	// request fails with a transient error (network or 5xx).
	//
	// Zero disables the retries.
	RetryAttempts int
}

// DefaultRemoteTimeout is the default timeout of a single remote request.
const DefaultRemoteTimeout = 30 * time.Second

// Remote is a [Store] backed by a PocketBase server reached over HTTP.
//
// It speaks the native PocketBase REST API, so the filter, sort and
// pagination semantics are identical to the embedded backend.
type Remote struct {
	baseURL string
	token   string
	allowed map[string]bool
	client  *http.Client
	retries int

	mu     sync.RWMutex
	closed bool
}

// Compile-time check that Remote satisfies the Store contract.
var _ Store = (*Remote)(nil)

// OpenRemote creates a [Remote] store.
//
// The returned store is not connected yet: the first request triggers the
// connection. Use [Remote.HealthCheck] to verify the connectivity eagerly.
func OpenRemote(config RemoteConfig) (*Remote, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, errors.New("pbstore: remote base url is required")
	}

	parsed, err := url.Parse(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("pbstore: invalid remote url %q: %w", config.BaseURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("pbstore: invalid remote url %q: expected http or https scheme", config.BaseURL)
	}

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = DefaultRemoteTimeout
	}

	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	return &Remote{
		baseURL: strings.TrimRight(config.BaseURL, "/"),
		token:   config.Token,
		allowed: toSet(config.Collections),
		client:  client,
		retries: config.RetryAttempts,
	}, nil
}

// Mode implements [Store].
func (s *Remote) Mode() Mode { return ModeRemote }

// BaseURL returns the configured server root URL.
func (s *Remote) BaseURL() string { return s.baseURL }

// HealthCheck implements [Store].
func (s *Remote) HealthCheck(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "cancelled", err
	}
	if s.isClosed() {
		return "closed", ErrClosed
	}

	// The health endpoint is cheap and does not require authentication.
	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}

	if err := s.doJSON(ctx, http.MethodGet, "/api/health", nil, &payload, false); err != nil {
		return "unreachable", fmt.Errorf("%w: %v", ErrNotConnected, err)
	}

	return fmt.Sprintf("ok (server code %d)", payload.Code), nil
}

// Collections implements [Store].
func (s *Remote) Collections(ctx context.Context) ([]Collection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.isClosed() {
		return nil, ErrClosed
	}

	var payload struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"items"`
		Page       int `json:"page"`
		PerPage    int `json:"perPage"`
		TotalItems int `json:"totalItems"`
		TotalPages int `json:"totalPages"`
	}

	err := s.doJSON(ctx, http.MethodGet, "/api/collections?perPage=200", nil, &payload, true)
	if err != nil {
		// The collections endpoint requires an authenticated (usually
		// superuser) request. Without a token the allowlist is the only
		// source of truth, so the configured collections are used as a
		// fallback.
		if len(s.allowed) > 0 && isUnauthorized(err) {
			return s.allowedCollections(), nil
		}
		return nil, err
	}

	out := make([]Collection, 0, len(payload.Items))
	for _, item := range payload.Items {
		if !s.isAllowed(item.Name) {
			continue
		}
		out = append(out, Collection{ID: item.ID, Name: item.Name, Type: item.Type})
	}
	return out, nil
}

// allowedCollections returns the configured allowlist as a collection list.
//
// It is the fallback used when the server does not expose its collection
// registry to the current credentials.
func (s *Remote) allowedCollections() []Collection {
	names := make([]string, 0, len(s.allowed))
	for name := range s.allowed {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]Collection, 0, len(names))
	for _, name := range names {
		out = append(out, Collection{Name: name, Type: "base"})
	}
	return out
}

// List implements [Store].
func (s *Remote) List(ctx context.Context, q Query) (*PageResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.isClosed() {
		return nil, ErrClosed
	}
	if q.Collection == "" {
		return nil, errors.New("pbstore: collection is required")
	}
	if !s.isAllowed(q.Collection) {
		return nil, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, q.Collection)
	}

	params := QueryParams(q)

	var payload struct {
		Items      []map[string]any `json:"items"`
		Page       int              `json:"page"`
		PerPage    int              `json:"perPage"`
		TotalItems int              `json:"totalItems"`
		TotalPages int              `json:"totalPages"`
	}

	endpoint := SearchURL(q.Collection) + "?" + params.Encode()
	if err := s.doJSON(ctx, http.MethodGet, endpoint, nil, &payload, true); err != nil {
		return nil, err
	}

	out := &PageResult{
		Records:    make([]*Record, 0, len(payload.Items)),
		Page:       payload.Page,
		PerPage:    payload.PerPage,
		TotalItems: payload.TotalItems,
		TotalPages: payload.TotalPages,
	}
	for _, item := range payload.Items {
		out.Records = append(out.Records, recordFromMap(item))
	}
	return out, nil
}

// Get implements [Store].
func (s *Remote) Get(ctx context.Context, collection, id string) (*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.isClosed() {
		return nil, ErrClosed
	}
	if !s.isAllowed(collection) {
		return nil, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, collection)
	}

	var payload map[string]any
	if err := s.doJSON(ctx, http.MethodGet, RecordURL(collection, id), nil, &payload, true); err != nil {
		return nil, err
	}
	return recordFromMap(payload), nil
}

// FindFirst implements [Store].
func (s *Remote) FindFirst(ctx context.Context, collection, filter string) (*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.isClosed() {
		return nil, ErrClosed
	}
	if !s.isAllowed(collection) {
		return nil, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, collection)
	}

	params := url.Values{}
	if filter != "" {
		params.Set("filter", filter)
	}
	params.Set("perPage", "1")

	var payload struct {
		Items []map[string]any `json:"items"`
	}

	endpoint := SearchURL(collection) + "?" + params.Encode()
	if err := s.doJSON(ctx, http.MethodGet, endpoint, nil, &payload, true); err != nil {
		return nil, err
	}
	if len(payload.Items) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, collection)
	}
	return recordFromMap(payload.Items[0]), nil
}

// Create implements [Store].
func (s *Remote) Create(ctx context.Context, collection string, data map[string]any) (*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.isClosed() {
		return nil, ErrClosed
	}
	if !s.isAllowed(collection) {
		return nil, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, collection)
	}

	var payload map[string]any
	if err := s.doJSON(ctx, http.MethodPost, SearchURL(collection), data, &payload, true); err != nil {
		return nil, err
	}
	return recordFromMap(payload), nil
}

// Update implements [Store].
func (s *Remote) Update(ctx context.Context, collection, id string, data map[string]any) (*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.isClosed() {
		return nil, ErrClosed
	}
	if !s.isAllowed(collection) {
		return nil, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, collection)
	}

	var payload map[string]any
	if err := s.doJSON(ctx, http.MethodPatch, RecordURL(collection, id), data, &payload, true); err != nil {
		return nil, err
	}
	return recordFromMap(payload), nil
}

// Delete implements [Store].
func (s *Remote) Delete(ctx context.Context, collection, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.isClosed() {
		return ErrClosed
	}
	if !s.isAllowed(collection) {
		return fmt.Errorf("%w: %q", ErrCollectionNotAllowed, collection)
	}

	if err := s.doJSON(ctx, http.MethodDelete, RecordURL(collection, id), nil, nil, true); err != nil {
		return err
	}
	return nil
}

// Count implements [Store].
func (s *Remote) Count(ctx context.Context, collection, filter string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if s.isClosed() {
		return 0, ErrClosed
	}
	if !s.isAllowed(collection) {
		return 0, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, collection)
	}

	params := url.Values{}
	if filter != "" {
		params.Set("filter", filter)
	}
	params.Set("perPage", "1")

	var payload struct {
		TotalItems int `json:"totalItems"`
	}

	endpoint := SearchURL(collection) + "?" + params.Encode()
	if err := s.doJSON(ctx, http.MethodGet, endpoint, nil, &payload, true); err != nil {
		return 0, err
	}
	return payload.TotalItems, nil
}

// EnsureDictionaryCollection implements [Store].
//
// The remote backend cannot create collections: the operation is reserved
// for the embedded store. Use a PocketBase migration or the admin UI to
// create the collection on the server.
func (s *Remote) EnsureDictionaryCollection(ctx context.Context, name string, fields []DictionaryField) (Collection, error) {
	return Collection{}, errors.New("pbstore: the remote backend cannot create collections; use the embedded store or a PocketBase migration")
}

// SyncCollection implements [Store].
//
// A remote store has no local database to sync into, so the operation is
// rejected. Run the app in embedded mode to populate a local cache.
func (s *Remote) SyncCollection(ctx context.Context, req SyncRequest) (*SyncReport, error) {
	return nil, errors.New("pbstore: the remote backend cannot sync into a local cache; use the embedded mode")
}

// Close implements [Store].
func (s *Remote) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// isClosed reports whether the store has been closed.
func (s *Remote) isClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

// isAllowed reports whether the collection passes the configured allowlist.
func (s *Remote) isAllowed(collection string) bool {
	if len(s.allowed) == 0 {
		return true
	}
	return s.allowed[strings.ToLower(collection)]
}

// doJSON performs an HTTP request against the PocketBase server and
// decodes the JSON response into out.
//
// When body is not nil it is JSON-encoded and sent as the request payload.
// Transient failures (network errors and 5xx responses) are retried up to
// the configured number of attempts.
func (s *Remote) doJSON(ctx context.Context, method, path string, body any, out any, auth bool) error {
	var payload []byte

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("pbstore: encode request body: %w", err)
		}
		payload = encoded
	}

	attempts := s.retries + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			// Linear backoff between the retries.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 250 * time.Millisecond):
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("pbstore: build request: %w", err)
		}

		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if auth && s.token != "" {
			req.Header.Set("Authorization", s.token)
		}

		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("pbstore: %s %s: %w", method, path, err)
			continue
		}

		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("pbstore: %s %s: server error %d", method, path, resp.StatusCode)
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %s", ErrNotFound, path)
		}

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("pbstore: %s %s: unexpected status %d: %s", method, path, resp.StatusCode, summarize(raw))
		}

		if readErr != nil {
			return fmt.Errorf("pbstore: read response: %w", readErr)
		}

		if out == nil {
			return nil
		}
		if len(raw) == 0 {
			return nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("pbstore: decode response: %w", err)
		}
		return nil
	}

	return lastErr
}

// isUnauthorized reports whether the error is an authentication failure.
func isUnauthorized(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "unexpected status 401") ||
		strings.Contains(err.Error(), "unexpected status 403")
}

// summarize trims a raw error payload to a short, single line message.
func summarize(raw []byte) string {
	const maxLen = 200

	text := strings.TrimSpace(string(raw))
	if len(text) > maxLen {
		return text[:maxLen] + "..."
	}
	return text
}

// recordFromMap converts a raw JSON record payload into the
// backend-agnostic [Record] view.
//
// The PocketBase REST API wraps a single record in a `{"data": {...}}`
// envelope, while a list returns the items directly. Both shapes are
// handled here.
func recordFromMap(payload map[string]any) *Record {
	if payload == nil {
		return nil
	}

	// Unwrap the single-record envelope when present.
	if data, ok := payload["data"].(map[string]any); ok {
		payload = data
	}

	out := &Record{Fields: make(map[string]any, len(payload))}
	for k, v := range payload {
		out.Fields[k] = v
	}

	if v, ok := out.Fields["id"]; ok {
		out.ID = fmt.Sprintf("%v", v)
	}
	if v, ok := out.Fields["collectionId"]; ok {
		out.CollectionID = fmt.Sprintf("%v", v)
	}
	if v, ok := out.Fields["collectionName"]; ok {
		out.CollectionName = fmt.Sprintf("%v", v)
	}
	if v, ok := out.Fields["created"]; ok {
		out.Created = fmt.Sprintf("%v", v)
	}
	if v, ok := out.Fields["updated"]; ok {
		out.Updated = fmt.Sprintf("%v", v)
	}
	if v, ok := out.Fields["expand"].(map[string]any); ok {
		out.Expand = v
	}

	return out
}
