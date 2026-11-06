package app_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/fantasy"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/app"
	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pages"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// errTest is the error injected in the error handling test.
var errTest = errors.New("test error")

// fakeModel is a language model that never answers.
//
// It is enough to build a runner: the tests drive the events of the
// agent themselves, and the model is never called.
type fakeModel struct{}

// Provider implements [fantasy.LanguageModel].
func (m *fakeModel) Provider() string { return "fake" }

// Model implements [fantasy.LanguageModel].
func (m *fakeModel) Model() string { return "fake-model" }

// Generate implements [fantasy.LanguageModel].
func (m *fakeModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("the fake model does not answer")
}

// Stream implements [fantasy.LanguageModel].
func (m *fakeModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	return nil, errors.New("the fake model does not answer")
}

// GenerateObject implements [fantasy.LanguageModel].
func (m *fakeModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("the fake model does not answer")
}

// StreamObject implements [fantasy.LanguageModel].
func (m *fakeModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("the fake model does not answer")
}

// newTestSetup builds an agent setup with a fake model.
//
// A setup with a runner is what a configured application has, and the
// tests need one to reach the chat screen.
func newTestSetup(t *testing.T) *agent.Setup {
	t.Helper()

	runner := agent.NewRunner(agent.Options{Model: &fakeModel{}})

	return &agent.Setup{Runner: runner}
}

// stubStore is a minimal in-memory [pbstore.Store] used to drive the root
// model without a database.
type stubStore struct {
	mode        pbstore.Mode
	collections []pbstore.Collection
	records     []*pbstore.Record
	closed      bool
	// preferences are the stored user preferences, when the test sets
	// them.
	preferences pbstore.Preferences
	// hasPreferences reports whether the preferences were stored.
	hasPreferences bool
	// writes counts the writes that reached the store.
	//
	// The counter is what tells a write that happened in the update
	// loop from one that happened in a command. It is atomic, because
	// a command runs in its own goroutine.
	writes atomic.Int64
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
	// The preferences are stored in their own collection.
	if collection == pbstore.PreferencesCollection && s.hasPreferences {
		return &pbstore.Record{
			ID: "prefs",
			Fields: map[string]any{
				"theme":    s.preferences.Theme,
				"scale":    s.preferences.Scale,
				"per_page": s.preferences.PerPage,
				"compact":  s.preferences.Compact,
			},
		}, nil
	}

	return nil, pbstore.ErrNotFound
}

func (s *stubStore) Create(ctx context.Context, collection string, data map[string]any) (*pbstore.Record, error) {
	// The preferences are stored in the collection, so the stub accepts
	// them instead of failing like a missing collection would.
	if collection == pbstore.PreferencesCollection {
		s.writes.Add(1)

		return &pbstore.Record{ID: "prefs", Fields: data}, nil
	}

	return nil, pbstore.ErrNotFound
}

func (s *stubStore) Update(ctx context.Context, collection, id string, data map[string]any) (*pbstore.Record, error) {
	if collection == pbstore.PreferencesCollection {
		s.writes.Add(1)

		return &pbstore.Record{ID: "prefs", Fields: data}, nil
	}

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

	return app.New(store, config.Default(), nil, nil)
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

// TestThePreferencesAreWrittenInACommand verifies that the preferences
// are written by a command, and not by the update loop.
//
// The update loop must never block: a store call in it freezes the
// interface, and a frozen interface is an application that looks
// broken. The write of the preferences is a store call, so it has to
// run in the command.
//
// The test looks at the store right after the key press: the write must
// not have happened yet, because the command was not run.
func TestThePreferencesAreWrittenInACommand(t *testing.T) {
	store := &stubStore{mode: pbstore.ModeEmbedded}

	model := app.New(store, config.Default(), nil, nil)

	// The theme is switched, which writes the preferences.
	updated, cmd := model.Update(tea.KeyPressMsg{Code: 'T', Text: "T"})
	model = updated.(*app.Model)

	if got := store.writes.Load(); got != 0 {
		t.Errorf("the update loop wrote to the store %d time(s), want none", got)
	}

	if cmd == nil {
		t.Fatal("the switch returned no command")
	}

	// The change itself is applied right away: the theme is a view
	// state, and it must not wait for the write.
	if model.Theme().Variant != theme.VariantLight {
		t.Errorf("the theme is %q, want %q", model.Theme().Variant, theme.VariantLight)
	}
}

// TestModelModalEditorOwnsTheKeys verifies that the global hotkeys stay
// out of the raw JSON editor.
//
// A JSON document is made of the very characters the global layer binds,
// so `q` would quit the program and the digits would navigate away from
// the record, discarding an unsaved edit.
func TestModelModalEditorOwnsTheKeys(t *testing.T) {
	m := newTestModel(t)

	record := &pbstore.Record{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"key": "go"}}

	// Open the detail page on a record, then its raw JSON editor.
	updated, _ := m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	m = updated.(*app.Model)

	updated, _ = m.Update(pages.OpenRecordMsg{ID: "rec1"})
	m = updated.(*app.Model)

	updated, _ = m.Update(pages.DetailMsg{Record: record, ID: "rec1"})
	m = updated.(*app.Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	m = updated.(*app.Model)

	if !contains(m.View().Content, "Raw JSON editor") {
		t.Fatalf("View() = %q, want the editor to be open", m.View().Content)
	}

	// `q` must reach the editor instead of quitting the program.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = updated.(*app.Model)

	if m.View().Content == "" {
		t.Error("`q` quit the program while the editor was open")
	}

	// A digit must not navigate away from the unsaved document.
	updated, _ = m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	m = updated.(*app.Model)

	if !contains(m.View().Content, "Raw JSON editor") {
		t.Errorf("View() = %q, want the editor to stay open", m.View().Content)
	}

	// `esc` cancels the edit rather than navigating back.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(*app.Model)

	if contains(m.View().Content, "Raw JSON editor") {
		t.Errorf("View() = %q, want the editor closed by esc", m.View().Content)
	}

	if !contains(m.View().Content, "Record") {
		t.Errorf("View() = %q, want the detail page to stay open", m.View().Content)
	}

	// The interrupt still works: the editor must never trap the user.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = updated.(*app.Model)

	if m.View().Content != "" {
		t.Error("ctrl+c did not quit while the editor was open")
	}
}
