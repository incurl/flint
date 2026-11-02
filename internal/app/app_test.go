package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/app"
	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pages"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/state"
)

// errTest is the error injected in the error handling test.
var errTest = errors.New("test error")

// stubStore is a minimal in-memory [pbstore.Store] used to drive the root
// model without a database.
type stubStore struct {
	mode        pbstore.Mode
	collections []pbstore.Collection
	records     []*pbstore.Record
	closed      bool
}

func (s *stubStore) Mode() pbstore.Mode { return s.mode }

func (s *stubStore) HealthCheck(ctx context.Context) (string, error) {
	return "ok", nil
}

func (s *stubStore) Collections(ctx context.Context) ([]pbstore.Collection, error) {
	return s.collections, nil
}

func (s *stubStore) List(ctx context.Context, q pbstore.Query) (*pbstore.PageResult, error) {
	return &pbstore.PageResult{
		Records:    s.records,
		Page:       1,
		PerPage:    30,
		TotalItems: len(s.records),
		TotalPages: 1,
	}, nil
}

func (s *stubStore) Get(ctx context.Context, collection, id string) (*pbstore.Record, error) {
	return nil, pbstore.ErrNotFound
}

func (s *stubStore) FindFirst(ctx context.Context, collection, filter string) (*pbstore.Record, error) {
	return nil, pbstore.ErrNotFound
}

func (s *stubStore) Create(ctx context.Context, collection string, data map[string]any) (*pbstore.Record, error) {
	// The preferences are stored in the collection, so the stub accepts
	// them instead of failing like a missing collection would.
	if collection == pbstore.PreferencesCollection {
		return &pbstore.Record{ID: "prefs", Fields: data}, nil
	}

	return nil, pbstore.ErrNotFound
}

func (s *stubStore) Update(ctx context.Context, collection, id string, data map[string]any) (*pbstore.Record, error) {
	return nil, pbstore.ErrNotFound
}

func (s *stubStore) Delete(ctx context.Context, collection, id string) error {
	return pbstore.ErrNotFound
}

func (s *stubStore) Count(ctx context.Context, collection, filter string) (int, error) {
	return len(s.records), nil
}

func (s *stubStore) EnsureDictionaryCollection(ctx context.Context, name string, fields []pbstore.DictionaryField) (pbstore.Collection, error) {
	return pbstore.Collection{Name: name}, nil
}

func (s *stubStore) SyncCollection(ctx context.Context, req pbstore.SyncRequest) (*pbstore.SyncReport, error) {
	return &pbstore.SyncReport{}, nil
}

func (s *stubStore) Close() error {
	s.closed = true
	return nil
}

// newTestModel builds a root model backed by the stub store.
func newTestModel(t *testing.T) *app.Model {
	t.Helper()

	store := &stubStore{
		mode: pbstore.ModeEmbedded,
		collections: []pbstore.Collection{
			{ID: "1", Name: "vocab", Type: "base"},
		},
		records: []*pbstore.Record{
			{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"key": "go"}},
		},
	}

	return app.New(store, config.Default(), nil)
}

// TestModelInit verifies that the model starts without panicking.
func TestModelInit(t *testing.T) {
	m := newTestModel(t)

	if cmd := m.Init(); cmd == nil {
		t.Error("Init() returned no command")
	}

	view := m.View().Content
	if view == "" {
		t.Error("View() returned an empty string")
	}
}

// TestModelNavigation verifies that the global hotkeys switch the page.
func TestModelNavigation(t *testing.T) {
	m := newTestModel(t)

	pages := []struct {
		key  string
		want state.Page
	}{
		{key: "1", want: state.PageHome},
		{key: "2", want: state.PageRecords},
		{key: "3", want: state.PageDetail},
		{key: "4", want: state.PageSearch},
		{key: "5", want: state.PageSettings},
		{key: "6", want: state.PageSync},
	}

	for _, tt := range pages {
		t.Run(tt.key, func(t *testing.T) {
			updated, _ := m.Update(tea.KeyPressMsg{Code: rune(tt.key[0]), Text: tt.key})
			m = updated.(*app.Model)

			view := m.View().Content
			if !contains(view, tt.want.Title()) {
				t.Errorf("View() = %q, want it to contain %q", view, tt.want.Title())
			}
		})
	}
}

// TestModelHealthCheck verifies that a health check updates the status bar.
func TestModelHealthCheck(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(app.HealthMsg{Status: "ok"})
	m = updated.(*app.Model)

	if !contains(m.View().Content, "ok") {
		t.Error("View() does not show the connection status")
	}
}

// TestModelErrorHandling verifies that a store error is surfaced instead of
// panicking.
func TestModelErrorHandling(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(pages.RecordsMsg{Err: errTest, Page: 1})
	m = updated.(*app.Model)

	if m.View().Content == "" {
		t.Error("View() returned an empty string after an error")
	}
}

// TestModelDiscardsStaleRecordsError verifies that an error for a query
// that is no longer current does not set the global error state.
//
// A slow query that fails after the user moved to another page of the
// records must not raise a toast for a page the user has already left.
func TestModelDiscardsStaleRecordsError(t *testing.T) {
	m := newTestModel(t)

	// The user opens the records page: the runtime page number is 1.
	updated, _ := m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m = updated.(*app.Model)

	// The response of the page 2 request arrives while the user is back
	// on page 1: it is stale and must not raise a toast.
	updated, _ = m.Update(pages.RecordsMsg{Err: errTest, Page: 2})
	m = updated.(*app.Model)

	if contains(m.View().Content, errTest.Error()) {
		t.Errorf("View() = %q, want the stale error to be discarded", m.View().Content)
	}
}

// TestModelToast verifies that a toast message is handled.
func TestModelToast(t *testing.T) {
	m := newTestModel(t)

	updated, cmd := m.Update(components.ToastMsg{Level: components.ToastInfo, Text: "hello"})
	m = updated.(*app.Model)

	if cmd == nil {
		t.Error("the toast message did not return a command")
	}

	if !contains(m.View().Content, "hello") {
		t.Error("View() does not show the toast")
	}
}

// contains reports whether the string contains the substring.
func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

// TestModelToastExpires verifies that the toast is dismissed once its
// timeout expires.
//
// The expiry message is owned by the toast component, so the root model
// must forward it instead of swallowing it.
func TestModelToastExpires(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(components.ToastMsg{Level: components.ToastInfo, Text: "hello"})
	m = updated.(*app.Model)

	if !contains(m.View().Content, "hello") {
		t.Fatal("the toast is not displayed")
	}

	// The expiry message hides the toast.
	updated, _ = m.Update(components.ToastExpiredMsg{})
	m = updated.(*app.Model)

	if contains(m.View().Content, "hello") {
		t.Error("the toast is still displayed after the expiry")
	}
}
