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
	// byFilter holds the records a query returns for a given filter.
	//
	// A stub that answers the same thing to every query cannot tell
	// a current answer from a stale one: the test needs the two to
	// be different.
	byFilter map[string][]*pbstore.Record
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

	records := s.records
	if byFilter, ok := s.byFilter[q.Filter]; ok {
		records = byFilter
	}

	return &pbstore.PageResult{
		Records:    records,
		Page:       1,
		PerPage:    30,
		TotalItems: len(records),
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

// CollectionSchema implements [pbstore.Store].
func (s *stubStore) CollectionSchema(ctx context.Context, name string) ([]pbstore.CollectionField, error) {
	return nil, pbstore.ErrNotFound
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

// loadRecords runs the listing of the page and feeds the answer back
// to it.
//
// The tests drive the real command instead of building the message by
// hand: the message carries the token of the request, and a message
// without one is discarded by the page. A test that builds the message
// itself would not exercise the guard it claims to test.
func loadRecords(t *testing.T, page *pages.RecordsPage) *pages.RecordsPage {
	t.Helper()

	cmd := page.Load()
	if cmd == nil {
		t.Fatal("Load() returned no command")
	}

	msg, ok := cmd().(pages.RecordsMsg)
	if !ok {
		t.Fatalf("Load() returned %T, want a RecordsMsg", cmd())
	}

	updated, _ := page.Update(msg)

	return updated.(*pages.RecordsPage)
}

// runSearch runs the search of the page and feeds the answer back to
// it.
//
// The message carries the token of the request, so the tests drive the
// real command instead of building the message by hand.
func runSearch(t *testing.T, page *pages.SearchPage) *pages.SearchPage {
	t.Helper()

	cmd := page.Search()
	if cmd == nil {
		t.Fatal("Search() returned no command")
	}

	msg, ok := cmd().(pages.SearchResultMsg)
	if !ok {
		t.Fatalf("Search() returned %T, want a SearchResultMsg", cmd())
	}

	updated, _ := page.Update(msg)

	return updated.(*pages.SearchPage)
}

// applyRecords feeds the answer of a listing to the page.
//
// The command is not run here: the test holds it, and runs it when it
// wants the answer to arrive.
func applyRecords(t *testing.T, page *pages.RecordsPage, cmd tea.Cmd) *pages.RecordsPage {
	t.Helper()

	if cmd == nil {
		t.Fatal("the listing command is nil")
	}

	msg, ok := cmd().(pages.RecordsMsg)
	if !ok {
		t.Fatalf("the listing returned %T, want a RecordsMsg", cmd())
	}

	updated, _ := page.Update(msg)

	return updated.(*pages.RecordsPage)
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

	page = loadRecords(t, page)

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

	page = runSearch(t, page)

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
	store := &stubStore{err: pbstore.ErrInvalidFilter}

	page := pages.NewSearchPage(store, newRuntime(), theme.Default(), nil)

	page = runSearch(t, page)

	if !strings.Contains(page.View().Content, "invalid filter") {
		t.Errorf("View() = %q, want it to contain the error", page.View().Content)
	}
}

// TestSearchPageDiscardsStaleResponse verifies that the answer of a
// search the user has moved away from is discarded.
//
// The search page had no guard at all: a slow search replaced the
// results of the one the user was waiting for, and the list showed the
// records of a filter that was no longer the one on the screen.
func TestSearchPageDiscardsStaleResponse(t *testing.T) {
	store := &stubStore{
		records: []*pbstore.Record{
			{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "Current"}},
		},
		byFilter: map[string][]*pbstore.Record{
			"other": {},
		},
	}

	page := pages.NewSearchPage(store, newRuntime(), theme.Default(), nil)

	// The input takes the focus, so that the keys reach it.
	page.Init()

	// The first search is started, and its answer is held back.
	first := page.Search()

	// The user runs another search. The filter is typed with the
	// keys, because the input of the page is not exported.
	for _, r := range "other" {
		updated, _ := page.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		page = updated.(*pages.SearchPage)
	}

	second := page.Search()

	// The second answer arrives first.
	msg, ok := second().(pages.SearchResultMsg)
	if !ok {
		t.Fatalf("Search() returned %T, want a SearchResultMsg", second())
	}

	updated, _ := page.Update(msg)
	page = updated.(*pages.SearchPage)

	if strings.Contains(page.View().Content, "Current") {
		t.Errorf("View() = %q, want the current response to be applied", page.View().Content)
	}

	// The first answer arrives late, and it must be discarded.
	msg, ok = first().(pages.SearchResultMsg)
	if !ok {
		t.Fatalf("Search() returned %T, want a SearchResultMsg", first())
	}

	updated, _ = page.Update(msg)
	page = updated.(*pages.SearchPage)

	if strings.Contains(page.View().Content, "Current") {
		t.Errorf("View() = %q, want the stale response to be discarded", page.View().Content)
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

	page = loadRecords(t, page)

	if !strings.Contains(page.View().Content, "Go") {
		t.Errorf("View() = %q, want it to contain the rendered title", page.View().Content)
	}
}

// TestRecordsPageDiscardsStaleResponse verifies that the answer of a
// request the user has moved away from is discarded.
//
// Without this guard a slow query that resolves after the user changed
// the filter would overwrite the visible rows with stale data. The
// page number alone does not catch it: two requests for the same page
// with two different filters are two different requests, and the
// slower one must not win.
func TestRecordsPageDiscardsStaleResponse(t *testing.T) {
	current := []*pbstore.Record{
		{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "Current"}},
	}
	stale := []*pbstore.Record{
		{ID: "rec2", CollectionName: "vocab", Fields: map[string]any{"label": "Stale"}},
	}

	store := &stubStore{
		records: current,
		byFilter: map[string][]*pbstore.Record{
			"other": stale,
		},
	}

	runtime := newRuntime()
	page := pages.NewRecordsPage(store, runtime, theme.Default(), nil)

	// The first request is started, and the answer is held back.
	first := page.Load()

	// The user changes the filter, and a second request is started.
	runtime.Filter = "other"
	second := page.Load()

	// The second answer arrives first, and it is the current one.
	page = applyRecords(t, page, second)

	if !strings.Contains(page.View().Content, "Stale") {
		t.Errorf("View() = %q, want the current response to be applied", page.View().Content)
	}

	// The first answer arrives late. It belongs to a request the user
	// has moved away from, and it must not replace the rows.
	page = applyRecords(t, page, first)

	if strings.Contains(page.View().Content, "Current") {
		t.Errorf("View() = %q, want the stale response to be discarded", page.View().Content)
	}
}

// TestRecordsPageKeepsCurrentResponse verifies that the answer of the
// request the page is waiting for is applied.
func TestRecordsPageKeepsCurrentResponse(t *testing.T) {
	store := &stubStore{
		records: []*pbstore.Record{
			{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"label": "Current"}},
		},
	}

	page := pages.NewRecordsPage(store, newRuntime(), theme.Default(), nil)

	page = loadRecords(t, page)

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

	page = loadRecords(t, page)

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

// TestDetailPageExpandsTheTree verifies that the record view expands and
// collapses the JSON tree.
//
// The page bound `j` to "focus the JSON viewer" and consumed the key
// before the viewer saw it, so the cursor never moved: `enter` collapsed
// the root and there was no way back into a child node. The tree takes
// the keys on its own now.
func TestDetailPageExpandsTheTree(t *testing.T) {
	record := &pbstore.Record{
		ID:             "rec1",
		CollectionName: "vocab",
		Fields: map[string]any{
			"label": "Go",
			"meta":  map[string]any{"tags": []any{"cli", "lang"}},
		},
	}

	store := &stubStore{records: []*pbstore.Record{record}}

	runtime := newRuntime()
	runtime.SelectedID = "rec1"

	page := pages.NewDetailPage(store, runtime, theme.Default(), nil)

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	page = updated.(*pages.DetailPage)

	updated, _ = page.Update(pages.DetailMsg{Record: record, ID: "rec1"})
	page = updated.(*pages.DetailPage)

	if strings.Contains(page.View().Content, "tags") {
		t.Fatal("the collapsed child is visible before anything was expanded")
	}

	// The cursor starts on the root: one step down is the "label"
	// scalar, two the "meta" object.
	for _, msg := range []tea.KeyPressMsg{
		{Code: 'j', Text: "j"},
		{Code: 'j', Text: "j"},
		{Code: tea.KeyEnter},
	} {
		updated, _ = page.Update(msg)
		page = updated.(*pages.DetailPage)
	}

	if view := page.View().Content; !strings.Contains(view, "tags") {
		t.Errorf("View() = %q, want the expanded child to be visible", view)
	}
}
