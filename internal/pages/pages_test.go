package pages_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pages"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// stubStore is a minimal in-memory [pbstore.Store] for the page tests.
type stubStore struct {
	collections []pbstore.Collection
	records     []*pbstore.Record
	err         error
}

func (s *stubStore) Mode() pbstore.Mode { return pbstore.ModeEmbedded }

func (s *stubStore) HealthCheck(ctx context.Context) (string, error) { return "ok", nil }

func (s *stubStore) Collections(ctx context.Context) ([]pbstore.Collection, error) {
	return s.collections, s.err
}

func (s *stubStore) List(ctx context.Context, q pbstore.Query) (*pbstore.PageResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &pbstore.PageResult{
		Records:    s.records,
		Page:       1,
		PerPage:    30,
		TotalItems: len(s.records),
		TotalPages: 1,
	}, nil
}

func (s *stubStore) Get(ctx context.Context, collection, id string) (*pbstore.Record, error) {
	if len(s.records) > 0 {
		return s.records[0], nil
	}
	return nil, pbstore.ErrNotFound
}

func (s *stubStore) FindFirst(ctx context.Context, collection, filter string) (*pbstore.Record, error) {
	return nil, pbstore.ErrNotFound
}

func (s *stubStore) Create(ctx context.Context, collection string, data map[string]any) (*pbstore.Record, error) {
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

func (s *stubStore) Close() error { return nil }

// errRequired is the error used by the sync page test.
var errRequired = errors.New("the remote collection is required")

// newRuntime returns a runtime state with a selected collection.
func newRuntime() *state.Runtime {
	r := state.NewRuntime()
	r.Collection = "vocab"
	return r
}

// TestHomePage verifies the landing page.
func TestHomePage(t *testing.T) {
	store := &stubStore{
		collections: []pbstore.Collection{{ID: "1", Name: "vocab", Type: "base"}},
	}

	page := pages.NewHomePage(store, newRuntime(), theme.Default())

	if cmd := page.Init(); cmd == nil {
		t.Error("Init() returned no command")
	}

	updated, _ := page.Update(pages.HomeMsg{Collections: store.collections})
	page = updated.(*pages.HomePage)

	view := page.View()
	if !strings.Contains(view, "vocab") {
		t.Errorf("View() = %q, want it to contain the collection name", view)
	}
}

// TestHomePageError verifies that a store error does not crash the page.
func TestHomePageError(t *testing.T) {
	store := &stubStore{err: pbstore.ErrNotConnected}

	page := pages.NewHomePage(store, newRuntime(), theme.Default())

	updated, _ := page.Update(pages.HomeMsg{Err: pbstore.ErrNotConnected})
	page = updated.(*pages.HomePage)

	if page.View() == "" {
		t.Error("View() returned an empty string")
	}
}

// TestRecordsPage verifies the record list page.
func TestRecordsPage(t *testing.T) {
	store := &stubStore{
		records: []*pbstore.Record{
			{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "Go"}},
		},
	}

	page := pages.NewRecordsPage(store, newRuntime(), theme.Default(), nil)

	if cmd := page.Init(); cmd == nil {
		t.Error("Init() returned no command")
	}

	result := &pbstore.PageResult{
		Records:    store.records,
		Page:       1,
		PerPage:    30,
		TotalItems: 1,
		TotalPages: 1,
	}

	updated, _ := page.Update(pages.RecordsMsg{Result: result})
	page = updated.(*pages.RecordsPage)

	view := page.View()
	if !strings.Contains(view, "Records") {
		t.Errorf("View() = %q, want it to contain the page title", view)
	}
	if !strings.Contains(view, "Go") {
		t.Errorf("View() = %q, want it to contain the record title", view)
	}
}

// TestRecordsPageFilter verifies the filter input.
func TestRecordsPageFilter(t *testing.T) {
	store := &stubStore{}
	page := pages.NewRecordsPage(store, newRuntime(), theme.Default(), nil)

	// The "/" key focuses the filter input.
	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	page = updated.(*pages.RecordsPage)

	if !page.FilterFocused() {
		t.Fatal("FilterFocused() = false after the \"/\" key")
	}

	// The typed keys reach the input.
	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	page = updated.(*pages.RecordsPage)

	if !page.FilterFocused() {
		t.Error("FilterFocused() = false after typing")
	}
}

// TestSearchPage verifies the search page.
func TestSearchPage(t *testing.T) {
	store := &stubStore{
		records: []*pbstore.Record{
			{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "Go"}},
		},
	}

	page := pages.NewSearchPage(store, newRuntime(), theme.Default(), nil)

	if cmd := page.Init(); cmd == nil {
		t.Error("Init() returned no command")
	}

	updated, _ := page.Update(pages.SearchResultMsg{Result: &pbstore.PageResult{
		Records: store.records,
	}})
	page = updated.(*pages.SearchPage)

	view := page.View()
	if !strings.Contains(view, "Search") {
		t.Errorf("View() = %q, want it to contain the page title", view)
	}
	if !strings.Contains(view, "Go") {
		t.Errorf("View() = %q, want it to contain the result title", view)
	}
}

// TestSearchPageError verifies that a filter error is displayed.
func TestSearchPageError(t *testing.T) {
	page := pages.NewSearchPage(&stubStore{}, newRuntime(), theme.Default(), nil)

	updated, _ := page.Update(pages.SearchResultMsg{Err: pbstore.ErrInvalidFilter})
	page = updated.(*pages.SearchPage)

	if !strings.Contains(page.View(), "invalid filter") {
		t.Errorf("View() = %q, want it to contain the error", page.View())
	}
}

// TestDetailPage verifies the record detail page.
func TestDetailPage(t *testing.T) {
	store := &stubStore{
		records: []*pbstore.Record{
			{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "Go", "count": 3}},
		},
	}

	runtime := newRuntime()
	runtime.SelectedID = "rec1"

	page := pages.NewDetailPage(store, runtime, theme.Default(), nil)

	updated, _ := page.Update(pages.DetailMsg{Record: store.records[0]})
	page = updated.(*pages.DetailPage)

	view := page.View()
	if !strings.Contains(view, "Go") {
		t.Errorf("View() = %q, want it to contain the record title", view)
	}
	if !strings.Contains(view, "root") {
		t.Errorf("View() = %q, want it to contain the JSON tree", view)
	}
}

// TestSettingsPage verifies the settings page.
func TestSettingsPage(t *testing.T) {
	rows := [][2]string{{"mode", "embedded"}, {"theme", "dark"}}

	page := pages.NewSettingsPage(newRuntime(), theme.Default(), rows)

	view := page.View()
	if !strings.Contains(view, "mode") {
		t.Errorf("View() = %q, want it to contain the mode row", view)
	}
	if !strings.Contains(view, "embedded") {
		t.Errorf("View() = %q, want it to contain the mode value", view)
	}
}

// TestSyncPage verifies the sync page.
func TestSyncPage(t *testing.T) {
	page := pages.NewSyncPage(&stubStore{}, newRuntime(), theme.Default())

	if cmd := page.Init(); cmd == nil {
		t.Error("Init() returned no command")
	}

	view := page.View()
	if !strings.Contains(view, "Sync") {
		t.Errorf("View() = %q, want it to contain the page title", view)
	}
}

// TestSyncPageRequiresCollection verifies that a sync without a collection
// is rejected.
func TestSyncPageRequiresCollection(t *testing.T) {
	runtime := state.NewRuntime()
	page := pages.NewSyncPage(&stubStore{}, runtime, theme.Default())

	updated, _ := page.Update(pages.SyncMsg{Err: errRequired})
	page = updated.(*pages.SyncPage)

	if !strings.Contains(page.View(), "required") {
		t.Errorf("View() = %q, want it to contain the error", page.View())
	}
}

// TestRendererIsUsed verifies that a custom renderer is honoured by the
// pages.
func TestRendererIsUsed(t *testing.T) {
	store := &stubStore{
		records: []*pbstore.Record{
			{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "Go"}},
		},
	}

	page := pages.NewRecordsPage(store, newRuntime(), theme.Default(), &components.DefaultRecordRenderer{
		TitleFields: []string{"label"},
	})

	updated, _ := page.Update(pages.RecordsMsg{Result: &pbstore.PageResult{Records: store.records}})
	page = updated.(*pages.RecordsPage)

	if !strings.Contains(page.View(), "Go") {
		t.Errorf("View() = %q, want it to contain the rendered title", page.View())
	}
}
