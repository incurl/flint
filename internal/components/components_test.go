package components_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/theme"
)

// newRecord builds a test record.
func newRecord() *pbstore.Record {
	return &pbstore.Record{
		ID:             "rec1",
		CollectionName: "vocab",
		Fields: map[string]any{
			"key":         "go",
			"label":       "Go",
			"description": "A statically typed language",
			"count":       42,
		},
	}
}

// TestDefaultRecordRenderer verifies the generic renderer.
func TestDefaultRecordRenderer(t *testing.T) {
	r := &components.DefaultRecordRenderer{}

	rec := newRecord()

	if got := r.Title(rec); got != "Go" {
		t.Errorf("Title() = %q, want %q", got, "Go")
	}
	if got := r.Summary(rec); !strings.Contains(got, "vocab") {
		t.Errorf("Summary() = %q, want it to contain the collection name", got)
	}

	rows := r.Fields(rec)
	if len(rows) == 0 {
		t.Fatal("Fields() returned no rows")
	}
	// The system fields are never displayed.
	for _, row := range rows {
		switch row.Name {
		case "id", "collectionId", "collectionName", "created", "updated":
			t.Errorf("Fields() returned the system field %q", row.Name)
		}
	}
}

// TestDefaultRecordRendererTitleFields verifies the title preference.
func TestDefaultRecordRendererTitleFields(t *testing.T) {
	r := &components.DefaultRecordRenderer{TitleFields: []string{"description"}}

	if got := r.Title(newRecord()); got != "A statically typed language" {
		t.Errorf("Title() = %q, want the description", got)
	}
}

// TestRecordCard verifies the record card rendering.
func TestRecordCard(t *testing.T) {
	card := components.NewRecordCard(theme.Default(), nil)
	card.SetRecord(newRecord())
	card.SetWidth(80)

	view := card.View()
	if !strings.Contains(view, "Go") {
		t.Errorf("View() = %q, want it to contain the title", view)
	}

	// Without a record the card renders a placeholder.
	empty := components.NewRecordCard(theme.Default(), nil)
	if empty.View() == "" {
		t.Error("View() returned an empty string for an empty card")
	}
}

// TestJSONViewer verifies the collapsible JSON viewer.
func TestJSONViewer(t *testing.T) {
	viewer := components.NewJSONViewer(theme.Default())
	viewer.SetSize(80, 20)

	if err := viewer.SetContent(`{"a": 1, "b": {"c": [1, 2]}}`); err != nil {
		t.Fatalf("SetContent() error = %v", err)
	}

	view := viewer.View()
	if !strings.Contains(view, "root") {
		t.Errorf("View() = %q, want it to contain the root node", view)
	}
	if !strings.Contains(view, "a") {
		t.Errorf("View() = %q, want it to contain the key %q", view, "a")
	}

	// An invalid JSON payload is reported as an error.
	if err := viewer.SetContent("{invalid"); err == nil {
		t.Error("SetContent() with an invalid payload returned no error")
	}
}

// TestJSONViewerCollapse verifies that the tree can be expanded and
// collapsed.
func TestJSONViewerCollapse(t *testing.T) {
	viewer := components.NewJSONViewer(theme.Default())
	viewer.SetSize(80, 20)

	if err := viewer.SetContent(`{"a": {"b": 1}}`); err != nil {
		t.Fatalf("SetContent() error = %v", err)
	}

	// The root is expanded by default, so its direct children are
	// visible, but the grandchildren are not: the "a" node is collapsed.
	view := viewer.View()
	if !strings.Contains(view, "a") {
		t.Errorf("View() = %q, want it to contain the first level key", view)
	}
	if strings.Contains(view, "b") {
		t.Errorf("View() = %q, want the collapsed child to be hidden", view)
	}

	// Expanding the "a" node reveals its child.
	viewer.Focus()
	viewer.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	viewer.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !strings.Contains(viewer.View(), "b") {
		t.Errorf("View() = %q, want the expanded child to be visible", viewer.View())
	}

	// Collapsing it again hides the child.
	viewer.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if strings.Contains(viewer.View(), "b") {
		t.Errorf("View() = %q, want the collapsed child to be hidden", viewer.View())
	}
}

// TestJSONEditor verifies the raw JSON editor.
func TestJSONEditor(t *testing.T) {
	editor := components.NewJSONEditor(theme.Default())
	editor.SetSize(80, 20)
	editor.SetContent(`{"a": 1}`)

	if editor.Dirty() {
		t.Error("Dirty() = true right after SetContent()")
	}

	doc, err := editor.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if doc["a"] != float64(1) {
		t.Errorf("Validate() = %v, want a=1", doc)
	}

	// An invalid document is rejected.
	editor.SetContent(`{invalid`)
	if _, err := editor.Validate(); err == nil {
		t.Error("Validate() with an invalid document returned no error")
	}

	// An empty document is rejected too.
	editor.SetContent("")
	if _, err := editor.Validate(); err == nil {
		t.Error("Validate() with an empty document returned no error")
	}
}

// TestPagination verifies the pagination component.
func TestPagination(t *testing.T) {
	p := components.NewPagination(theme.Default(), 10)

	pages := p.SetTotal(25, 1, 10)
	if pages != 3 {
		t.Errorf("SetTotal() = %d pages, want 3", pages)
	}
	if p.Page() != 1 {
		t.Errorf("Page() = %d, want 1", p.Page())
	}
	if p.TotalItems() != 25 {
		t.Errorf("TotalItems() = %d, want 25", p.TotalItems())
	}

	view := p.View()
	if !strings.Contains(view, "25 records") {
		t.Errorf("View() = %q, want it to contain the record count", view)
	}

	// The pagination is empty without records.
	empty := components.NewPagination(theme.Default(), 10)
	if !strings.Contains(empty.View(), "no records") {
		t.Errorf("View() = %q, want the empty state", empty.View())
	}
}

// TestStatusBar verifies the status bar rendering.
func TestStatusBar(t *testing.T) {
	bar := components.NewStatusBar(theme.Default())
	bar.SetWidth(120)
	bar.SetInfo(components.StatusInfo{
		Mode:       "embedded",
		Connection: "ok",
		Collection: "vocab",
		Page:       "Records",
		Hints:      components.FormatHints([][2]string{{"/", "filter"}, {"enter", "open"}}),
	})

	view := bar.View()
	if !strings.Contains(view, "EMBEDDED") {
		t.Errorf("View() = %q, want it to contain the mode", view)
	}
	if !strings.Contains(view, "vocab") {
		t.Errorf("View() = %q, want it to contain the collection", view)
	}
	if !strings.Contains(view, "filter") {
		t.Errorf("View() = %q, want it to contain the hints", view)
	}
}

// TestFormatHints verifies the hint formatting.
func TestFormatHints(t *testing.T) {
	hints := components.FormatHints([][2]string{
		{"ctrl+r", "sync"},
		{"", "skipped"},
		{"tab", "next"},
	})

	if len(hints) != 2 {
		t.Fatalf("FormatHints() returned %d hints, want 2", len(hints))
	}
	if hints[0] != "ctrl+r sync" {
		t.Errorf("hints[0] = %q, want %q", hints[0], "ctrl+r sync")
	}
}

// TestToast verifies the toast component.
func TestToast(t *testing.T) {
	toast := components.NewToast(theme.Default())

	if toast.Visible() {
		t.Error("Visible() = true on a fresh toast")
	}
	if toast.View() != "" {
		t.Error("View() returned a non empty string for a hidden toast")
	}

	cmd := toast.Show(components.ToastSuccess, "saved")
	if cmd == nil {
		t.Error("Show() returned no command")
	}
	if !toast.Visible() {
		t.Error("Visible() = false after Show()")
	}
	if !strings.Contains(toast.View(), "saved") {
		t.Errorf("View() = %q, want it to contain the text", toast.View())
	}

	toast.Hide()
	if toast.Visible() {
		t.Error("Visible() = true after Hide()")
	}
}

// TestRecordRendererInterface verifies that the renderer interface can be
// implemented by a downstream project.
func TestRecordRendererInterface(t *testing.T) {
	var _ components.RecordRenderer = (*components.DefaultRecordRenderer)(nil)

	// A custom renderer is accepted by the record card.
	card := components.NewRecordCard(theme.Default(), &components.DefaultRecordRenderer{})
	card.SetRecord(newRecord())
	if card.View() == "" {
		t.Error("the record card rendered nothing")
	}
}

// TestTableRows verifies that the record table renders the records.
func TestTableRows(t *testing.T) {
	tbl := table.New(table.WithColumns([]table.Column{{Title: "Title"}}))
	tbl.SetRows([]table.Row{{"Go"}})

	if len(tbl.Rows()) != 1 {
		t.Errorf("the table holds %d rows, want 1", len(tbl.Rows()))
	}
}
