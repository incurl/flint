package app_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/incurl/flint/internal/app"
	"github.com/incurl/flint/internal/pages"
	"github.com/incurl/flint/internal/pbstore"
)

// TestTheRecordViewFillsTheTerminal verifies that a page is told the
// space it has when it becomes active.
//
// A resize message only reaches the page that is displayed at the time,
// so a page reached later kept the default size of its constructor: the
// record view rendered 80 columns by 24 lines on a 200 column terminal,
// and the rest of the screen stayed empty until the terminal was resized
// while the page was on screen.
func TestTheRecordViewFillsTheTerminal(t *testing.T) {
	const (
		width  = 200
		height = 40
	)

	m := newTestModel(t)

	// The terminal reports its size while the user is on the home page.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = updated.(*app.Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	m = updated.(*app.Model)

	m = showRecord(t, m)

	lines := strings.Split(m.View().Content, "\n")

	// The status bar is the last line, and it spans the whole width.
	body := lines[:len(lines)-1]

	if len(body) != height-1 {
		t.Errorf("the body is %d lines, want %d", len(body), height-1)
	}

	widest := 0

	for _, line := range body {
		if w := lipgloss.Width(line); w > widest {
			widest = w
		}
	}

	if widest != width {
		t.Errorf("the widest body line is %d cells, want %d", widest, width)
	}
}

// TestThePanelResizesThePage verifies that the page is told the narrower
// width when the panel takes its column.
//
// The toggle is a chrome key of the root model, and the page is laid out
// from the width it was given: a page that keeps the width it had before
// the panel opened is a page whose layout no longer fits the space.
func TestThePanelResizesThePage(t *testing.T) {
	// 121 columns is the narrowest terminal that shows the panel. The
	// page has 121 columns without it — side by side — and 88 with it,
	// where the fields are stacked above the tree.
	const (
		width  = 121
		height = 40
	)

	m := newTestModel(t)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = updated.(*app.Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	m = updated.(*app.Model)

	m = showRecord(t, m)

	if !strings.Contains(bodyLine(m, 2), "root") {
		t.Fatal("the tree is not beside the fields without the panel")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	m = updated.(*app.Model)

	if strings.Contains(bodyLine(m, 2), "root") {
		t.Error("the page kept the width it had before the panel opened")
	}
}

// showRecord loads a record into the record view of the model.
func showRecord(t *testing.T, m *app.Model) *app.Model {
	t.Helper()

	record := &pbstore.Record{
		ID:             "",
		CollectionName: "vocab",
		Fields: map[string]any{
			"label":       "Go",
			"description": strings.Repeat("a value ", 30),
			"meta":        map[string]any{"tags": []any{"cli"}},
		},
	}

	updated, _ := m.Update(pages.DetailMsg{Record: record, ID: ""})

	return updated.(*app.Model)
}

// bodyLine returns a line of the page, which is the view without the
// status bar at the bottom.
func bodyLine(m *app.Model, index int) string {
	lines := strings.Split(m.View().Content, "\n")

	if index < 0 || index >= len(lines)-1 {
		return ""
	}

	return lines[index]
}
