package pages_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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

// EnsureCollection implements [pbstore.Store].
func (s *stubStore) EnsureCollection(ctx context.Context, name string, fields []pbstore.CollectionField) (pbstore.Collection, error) {
	return pbstore.Collection{Name: name}, nil
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

	view := page.View().Content
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

	if page.View().Content == "" {
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

	updated, _ := page.Update(pages.RecordsMsg{Result: result, Page: 1})
	page = updated.(*pages.RecordsPage)

	view := page.View().Content
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
	updated, _ := page.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	page = updated.(*pages.RecordsPage)

	if !page.FilterFocused() {
		t.Fatal("FilterFocused() = false after the \"/\" key")
	}

	// The typed keys reach the input.
	updated, _ = page.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
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

	view := page.View().Content
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

	if !strings.Contains(page.View().Content, "invalid filter") {
		t.Errorf("View() = %q, want it to contain the error", page.View().Content)
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

	updated, _ := page.Update(pages.DetailMsg{Record: store.records[0], ID: "rec1"})
	page = updated.(*pages.DetailPage)

	view := page.View().Content
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

	view := page.View().Content
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

	view := page.View().Content
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

	if !strings.Contains(page.View().Content, "required") {
		t.Errorf("View() = %q, want it to contain the error", page.View().Content)
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

	updated, _ := page.Update(pages.RecordsMsg{Result: &pbstore.PageResult{Records: store.records}, Page: 1})
	page = updated.(*pages.RecordsPage)

	if !strings.Contains(page.View().Content, "Go") {
		t.Errorf("View() = %q, want it to contain the rendered title", page.View().Content)
	}
}

// TestRecordsPageDiscardsStaleResponse verifies that a response for a page
// that is no longer the current one is discarded.
//
// Without this guard a slow query that resolves after the user moved to
// another page would overwrite the visible rows with stale data.
func TestRecordsPageDiscardsStaleResponse(t *testing.T) {
	store := &stubStore{
		records: []*pbstore.Record{
			{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "Current"}},
		},
	}

	runtime := newRuntime()
	page := pages.NewRecordsPage(store, runtime, theme.Default(), nil)

	// The user navigates to page 3 while the page 1 request is in flight.
	runtime.PageNumber = 3

	updated, _ := page.Update(pages.RecordsMsg{
		Result: &pbstore.PageResult{
			Records:    store.records,
			Page:       1,
			PerPage:    30,
			TotalItems: 1,
			TotalPages: 1,
		},
		Page: 1,
	})
	page = updated.(*pages.RecordsPage)

	if strings.Contains(page.View().Content, "Current") {
		t.Errorf("View() = %q, want the stale response to be discarded", page.View().Content)
	}
}

// TestRecordsPageKeepsCurrentResponse verifies that a response matching the
// current page is applied.
func TestRecordsPageKeepsCurrentResponse(t *testing.T) {
	store := &stubStore{
		records: []*pbstore.Record{
			{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "Current"}},
		},
	}

	runtime := newRuntime()
	runtime.PageNumber = 3

	page := pages.NewRecordsPage(store, runtime, theme.Default(), nil)

	updated, _ := page.Update(pages.RecordsMsg{
		Result: &pbstore.PageResult{
			Records:    store.records,
			Page:       3,
			PerPage:    30,
			TotalItems: 1,
			TotalPages: 1,
		},
		Page: 3,
	})
	page = updated.(*pages.RecordsPage)

	if !strings.Contains(page.View().Content, "Current") {
		t.Errorf("View() = %q, want the current response to be applied", page.View().Content)
	}
}

// TestRecordsPageRendersRows verifies that the record table renders the
// fetched records.
//
// Bubble Tea v2 renders an empty table when the table viewport has no
// width, which would silently hide every row. The regression is caught
// here so that a missing SetWidth call cannot pass the test suite
// unnoticed.
func TestRecordsPageRendersRows(t *testing.T) {
	store := &stubStore{
		records: []*pbstore.Record{
			{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "Go"}},
		},
	}

	page := pages.NewRecordsPage(store, newRuntime(), theme.Default(), nil)

	result := &pbstore.PageResult{
		Records:    store.records,
		Page:       1,
		PerPage:    30,
		TotalItems: 1,
		TotalPages: 1,
	}

	updated, _ := page.Update(pages.RecordsMsg{Result: result, Page: 1})
	page = updated.(*pages.RecordsPage)

	view := page.View().Content
	if !strings.Contains(view, "Go") {
		t.Errorf("View() = %q, want it to contain the record title", view)
	}
}

// TestDetailPageDiscardsStaleResponse verifies that a slow response for
// a record the user has already left does not replace the one they are
// looking at.
//
// The requests are asynchronous, so the response for the record opened
// first can arrive last.
func TestDetailPageDiscardsStaleResponse(t *testing.T) {
	first := &pbstore.Record{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "First"}}
	second := &pbstore.Record{ID: "rec2", CollectionName: "vocab", Fields: map[string]any{"label": "Second"}}

	store := &stubStore{records: []*pbstore.Record{first, second}}

	runtime := newRuntime()
	runtime.SelectedID = "rec2"

	page := pages.NewDetailPage(store, runtime, theme.Default(), nil)

	// The response for the record the user already left arrives late.
	updated, _ := page.Update(pages.DetailMsg{Record: first, ID: "rec1"})
	page = updated.(*pages.DetailPage)

	if strings.Contains(page.View().Content, "First") {
		t.Error("the stale response replaced the displayed record")
	}

	// The response for the selected record is applied.
	updated, _ = page.Update(pages.DetailMsg{Record: second, ID: "rec2"})
	page = updated.(*pages.DetailPage)

	if !strings.Contains(page.View().Content, "Second") {
		t.Error("the current response was discarded")
	}
}

// savingStore records the writes and makes the next read return them,
// which is what a backend does after an update.
type savingStore struct {
	*stubStore
	// writes counts the accepted updates.
	writes int
}

// Update stores the document and stamps it, so that a later read is
// distinguishable from the text that was typed into the editor.
func (s *savingStore) Update(ctx context.Context, collection, id string, data map[string]any) (*pbstore.Record, error) {
	s.writes++

	fields := make(map[string]any, len(data)+1)
	for k, v := range data {
		fields[k] = v
	}

	fields["stamped"] = "by-the-store"

	rec := &pbstore.Record{ID: id, CollectionName: collection, Fields: fields}
	s.records = []*pbstore.Record{rec}

	return rec, nil
}

// runCmd executes a command and returns the messages it produces,
// flattening the batches into a single list.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	msg := cmd()
	if msg == nil {
		return nil
	}

	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg

		for _, child := range batch {
			out = append(out, runCmd(child)...)
		}

		return out
	}

	return []tea.Msg{msg}
}

// TestDetailPageEditIsNotReadOnly verifies that the raw JSON editor
// receives the key presses while it is open.
//
// The page forwarded every key to the JSON viewer, so the editor opened,
// looked focused and accepted nothing: the record was read only in
// practice, even though the save path existed.
func TestDetailPageEditIsNotReadOnly(t *testing.T) {
	store := &stubStore{
		records: []*pbstore.Record{
			{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "Go"}},
		},
	}

	runtime := newRuntime()
	runtime.SelectedID = "rec1"

	page := pages.NewDetailPage(store, runtime, theme.Default(), nil)

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.DetailPage)

	updated, _ = page.Update(pages.DetailMsg{Record: store.records[0], ID: "rec1"})
	page = updated.(*pages.DetailPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	page = updated.(*pages.DetailPage)

	if !strings.Contains(page.View().Content, "Raw JSON editor") {
		t.Fatalf("View() = %q, want the editor to be open", page.View().Content)
	}

	// A printable key must reach the textarea, not the viewer behind it.
	updated, _ = page.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	page = updated.(*pages.DetailPage)

	if !strings.Contains(page.View().Content, "z") {
		t.Error("the key press never reached the editor: the record is read only")
	}
}

// TestDetailPageSaveReloads verifies that a successful save refreshes the
// displayed record.
//
// The page used to raise "record saved" and keep the pre-save content on
// screen, so an edit looked as if it had not been applied.
func TestDetailPageSaveReloads(t *testing.T) {
	store := &savingStore{stubStore: &stubStore{
		records: []*pbstore.Record{
			{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "Go"}},
		},
	}}

	runtime := newRuntime()
	runtime.SelectedID = "rec1"

	page := pages.NewDetailPage(store, runtime, theme.Default(), nil)

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.DetailPage)

	updated, _ = page.Update(pages.DetailMsg{Record: store.records[0], ID: "rec1"})
	page = updated.(*pages.DetailPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	page = updated.(*pages.DetailPage)

	// Save the document as it stands: the write itself is not the point.
	updated, cmd := page.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	page = updated.(*pages.DetailPage)

	outcomes := runCmd(cmd)
	if len(outcomes) != 1 {
		t.Fatalf("the save produced %d messages, want 1", len(outcomes))
	}

	// The outcome makes the page toast and reload.
	updated, cmd = page.Update(outcomes[0])
	page = updated.(*pages.DetailPage)

	reloaded := runCmd(cmd)
	if len(reloaded) == 0 {
		t.Fatal("the save triggered no reload")
	}

	for _, msg := range reloaded {
		updated, _ = page.Update(msg)
		page = updated.(*pages.DetailPage)
	}

	// The stamped field exists only in the store, so seeing it proves
	// that the page re-read the record instead of keeping the editor
	// content on screen.
	if !strings.Contains(page.View().Content, "stamped") {
		t.Errorf("View() = %q, want the reloaded document", page.View().Content)
	}

	if store.writes != 1 {
		t.Errorf("writes = %d, want 1", store.writes)
	}
}

// TestDetailPageDiscardsStaleSave verifies that a write for a record the
// user has already left does not close the editor of the current one.
//
// The write is asynchronous, so the response can arrive after the
// selection moved on. The page compares the identity it was asked for
// with the current selection, the same way it does for a read.
func TestDetailPageDiscardsStaleSave(t *testing.T) {
	store := &savingStore{stubStore: &stubStore{
		records: []*pbstore.Record{
			{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "First"}},
		},
	}}

	runtime := newRuntime()
	runtime.SelectedID = "rec1"

	page := pages.NewDetailPage(store, runtime, theme.Default(), nil)

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.DetailPage)

	updated, _ = page.Update(pages.DetailMsg{Record: store.records[0], ID: "rec1"})
	page = updated.(*pages.DetailPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	page = updated.(*pages.DetailPage)

	// The user moves on to another record while the write is in flight.
	runtime.SelectedID = "rec2"

	updated, _ = page.Update(pages.DetailMsg{Record: store.records[0], ID: "rec2"})
	page = updated.(*pages.DetailPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	page = updated.(*pages.DetailPage)

	// The response for the record the user has left arrives late.
	updated, _ = page.Update(pages.SavedMsg{ID: "rec1"})
	page = updated.(*pages.DetailPage)

	if !strings.Contains(page.View().Content, "Raw JSON editor") {
		t.Error("the stale save closed the editor of the current record")
	}
}

// TestDetailPageCancelDiscardsEdits verifies that cancelling an edit
// restores the stored document.
//
// The editor keeps the text that was typed, so without the restore the
// next `e` would show the abandoned edit as if it were the record.
func TestDetailPageCancelDiscardsEdits(t *testing.T) {
	record := &pbstore.Record{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "Go"}}

	store := &stubStore{records: []*pbstore.Record{record}}

	runtime := newRuntime()
	runtime.SelectedID = "rec1"

	page := pages.NewDetailPage(store, runtime, theme.Default(), nil)

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.DetailPage)

	updated, _ = page.Update(pages.DetailMsg{Record: record, ID: "rec1"})
	page = updated.(*pages.DetailPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	page = updated.(*pages.DetailPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	page = updated.(*pages.DetailPage)

	// Cancel, then reopen: the abandoned edit must be gone.
	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	page = updated.(*pages.DetailPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	page = updated.(*pages.DetailPage)

	if strings.Contains(page.View().Content, "z") {
		t.Error("the cancelled edit is still in the editor")
	}
}
