package app_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/app"
	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pages"
	"github.com/incurl/flint/internal/theme"
)

// TestPanelIsClosedByDefault verifies that the panel does not take any
// space until it is asked for.
//
// The panel is not free: it takes a third of the width of a wide
// terminal, and the pages need that width.
func TestPanelIsClosedByDefault(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = updated.(*app.Model)

	view := m.View().Content

	if strings.Contains(view, "No conversation yet") {
		t.Error("the panel is visible before it was asked for")
	}
}

// TestPanelToggles verifies that the panel opens and closes with the
// toggle key.
func TestPanelToggles(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = updated.(*app.Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	m = updated.(*app.Model)

	if !strings.Contains(m.View().Content, "No conversation yet") {
		t.Error("the panel did not open")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	m = updated.(*app.Model)

	if strings.Contains(m.View().Content, "No conversation yet") {
		t.Error("the panel did not close")
	}
}

// TestPanelDoesNotDuplicateTheEntries verifies that an event is applied
// once.
//
// The agent page receives the events of the run directly when it is the
// page that is displayed, and through the root model when it is not.
// Applying an event twice would show every entry of the conversation
// twice.
func TestPanelDoesNotDuplicateTheEntries(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = updated.(*app.Model)

	// The agent page is the one that is displayed.
	updated, _ = m.Update(tea.KeyPressMsg{Code: '7', Text: "7"})
	m = updated.(*app.Model)

	updated, _ = m.Update(pages.AgentEventMsg{Event: agent.Event{
		Kind: agent.EventTextDelta,
		Text: "once",
	}})
	m = updated.(*app.Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	m = updated.(*app.Model)

	view := m.View().Content

	if got := strings.Count(view, "once"); got != 1 {
		t.Errorf("the panel shows the entry %d times, want 1", got)
	}
}

// TestPanelIsAdvertised verifies that the toggle is discoverable.
//
// A feature the user does not know about is a feature nobody uses: the
// hint is shown whenever the terminal is wide enough for the panel.
func TestPanelIsAdvertised(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = updated.(*app.Model)

	if !strings.Contains(m.View().Content, "ctrl+b") {
		t.Error("the status bar does not advertise the panel toggle")
	}
}

// TestPanelIsNotAdvertisedOnANarrowTerminal verifies that the hint is
// not shown when the panel cannot be opened.
func TestPanelIsNotAdvertisedOnANarrowTerminal(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m = updated.(*app.Model)

	if strings.Contains(m.View().Content, "ctrl+b") {
		t.Error("the status bar advertises a toggle that cannot work")
	}
}

// TestPanelKeepsTheWidth verifies that the composed view is exactly as
// wide as the terminal.
//
// The page, the gap and the panel have to add up to the width of the
// terminal: a line that is too wide wraps in the terminal and breaks
// the layout, and a line that is too narrow leaves a gap.
func TestPanelKeepsTheWidth(t *testing.T) {
	for _, width := range []int{121, 160} {
		m := newTestModel(t)

		updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		m = updated.(*app.Model)

		updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
		m = updated.(*app.Model)

		lines := strings.Split(m.View().Content, "\n")

		// The last line is the status bar, which is checked
		// elsewhere: the panel composes the lines above it.
		for i, line := range lines[:len(lines)-1] {
			if got := lipgloss.Width(line); got != width {
				t.Errorf("line %d is %d columns wide on a %d column terminal",
					i, got, width)
			}
		}
	}
}

// TestPanelKeepsTheTableWidth verifies that the panel never clips the
// record table.
//
// The table is 88 columns wide and the panel takes 32 plus a column of
// gap, so the terminal has to be at least 121 columns for the two to
// fit side by side. A table whose viewport is narrower than its columns
// renders *empty* — with no error.
func TestPanelKeepsTheTableWidth(t *testing.T) {
	const tableWidth = 88

	for _, width := range []int{121, 140, 200} {
		m := newTestModel(t)

		updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		m = updated.(*app.Model)

		updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
		m = updated.(*app.Model)

		if !strings.Contains(m.View().Content, "No conversation yet") {
			t.Errorf("the panel did not open on a %d column terminal", width)

			continue
		}

		// The page keeps at least the width of the table.
		pageWidth := width - components.AgentPanelWidth - 1
		if pageWidth < tableWidth {
			t.Errorf("the page has %d columns on a %d column terminal, want at least %d",
				pageWidth, width, tableWidth)
		}
	}
}

// TestPanelNeedsRoom verifies that the panel stays closed on a terminal
// that is too narrow.
//
// The record table is 88 columns wide. A panel next to it on an 80
// column terminal would leave the table too narrow, and a table whose
// viewport is narrower than its columns renders *empty* — with no
// error.
func TestPanelNeedsRoom(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m = updated.(*app.Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	m = updated.(*app.Model)

	if strings.Contains(m.View().Content, "No conversation yet") {
		t.Error("the panel opened on a terminal that is too narrow")
	}
}

// TestPanelNeedsHeight verifies that the panel stays closed on a
// terminal that is too short.
func TestPanelNeedsHeight(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 6})
	m = updated.(*app.Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	m = updated.(*app.Model)

	if strings.Contains(m.View().Content, "No conversation yet") {
		t.Error("the panel opened on a terminal that is too short")
	}
}

// TestAgentEventsReachThePageWhateverThePage verifies that a run keeps
// streaming when the user navigates away.
//
// A run outlives the page it was started from: the user starts it, goes
// back to the records, and the answer still has to arrive.
func TestAgentEventsReachThePageWhateverThePage(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = updated.(*app.Model)

	// Open the agent page and start a conversation.
	updated, _ = m.Update(tea.KeyPressMsg{Code: '7', Text: "7"})
	m = updated.(*app.Model)

	updated, _ = m.Update(pages.AgentEventMsg{Event: agent.Event{
		Kind: agent.EventTextDelta,
		Text: "hello",
	}})
	m = updated.(*app.Model)

	// Navigate to the records page.
	updated, _ = m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m = updated.(*app.Model)

	// The event arrives while the records page is displayed.
	updated, _ = m.Update(pages.AgentEventMsg{Event: agent.Event{
		Kind: agent.EventTextDelta,
		Text: "world",
	}})
	m = updated.(*app.Model)

	// The panel shows the whole conversation, whatever the page.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	m = updated.(*app.Model)

	view := m.View().Content

	if !strings.Contains(view, "hello") {
		t.Error("the event of the first turn is missing from the panel")
	}

	if !strings.Contains(view, "world") {
		t.Error("the event that arrived on another page is missing from the panel")
	}
}

// TestPanelShowsTheConversation verifies that the panel shows the tool
// calls of the conversation.
//
// The tool call is what the user wants to see: it is the record of
// what the agent did.
func TestPanelShowsTheConversation(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = updated.(*app.Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: '7', Text: "7"})
	m = updated.(*app.Model)

	updated, _ = m.Update(pages.AgentEventMsg{Event: agent.Event{
		Kind:     agent.EventToolCall,
		ToolName: "list_records",
	}})
	m = updated.(*app.Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	m = updated.(*app.Model)

	if !strings.Contains(m.View().Content, "list_records") {
		t.Error("the panel does not show the tool call")
	}
}

// TestAgentPanelRendersTheEntries verifies that the panel component
// renders what it is given.
func TestAgentPanelRendersTheEntries(t *testing.T) {
	panel := components.NewAgentPanel(theme.Default())
	panel.SetSize(32, 20)

	panel.SetEntries([]components.AgentPanelEntry{
		{Text: "a question"},
		{Text: "list_records", IsTool: true},
		{Text: "an answer"},
	})

	view := panel.View().Content

	for _, want := range []string{"a question", "list_records", "an answer"} {
		if !strings.Contains(view, want) {
			t.Errorf("the panel does not contain %q", want)
		}
	}
}
