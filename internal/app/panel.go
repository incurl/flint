package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/theme"
)

// The layout of the agent panel.
//
// The panel is a *side* panel: it takes a fixed slice of the width and
// leaves the rest to the page. The values match the sidebar of the
// reference implementation, for the same reason: a transcript needs
// enough room for a line of prose.
const (
	// panelColumnWidth is the width of the agent panel.
	panelColumnWidth = components.AgentPanelWidth
	// panelGap is the space between the page and the panel.
	panelGap = 1
	// panelMinWidth is the terminal width below which the panel does not
	// fit.
	//
	// The record table is 88 columns wide. The panel needs 32 columns
	// and a column of gap, so the terminal has to be at least
	// 88 + 32 + 1 wide for the table to keep its full width. Below
	// this the table would be clipped — and a table whose viewport is
	// narrower than its columns renders *empty*, with no error.
	panelMinWidth = 121
	// panelMinHeight is the terminal height below which the panel does
	// not fit.
	//
	// A panel shorter than a few lines is a decoration, not a
	// transcript.
	panelMinHeight = 12
)

// panelState is the state of the agent panel.
//
// It is separate from the pages: the panel is chrome of the root model,
// the same way the status bar and the toast are.
type panelState struct {
	// panel renders the conversation.
	panel *components.AgentPanel
	// visible reports whether the panel is shown.
	visible bool
	// width is the terminal width.
	width int
	// height is the terminal height.
	height int
}

// newPanelState returns the state of a closed panel.
func newPanelState(t *theme.Theme) *panelState {
	return &panelState{panel: components.NewAgentPanel(t)}
}

// canShow reports whether the panel fits on the terminal.
//
// The panel is not shown on a terminal that is too narrow: the pages
// would lose the width they need, and the record table renders empty
// when its viewport is narrower than its columns.
func (p *panelState) canShow() bool {
	return p.width >= panelMinWidth && p.height >= panelMinHeight
}

// toggle shows or hides the panel.
//
// It returns false when the panel does not fit, so that the caller can
// leave the key to the page: a key that does nothing is better than a
// key that hides a panel that was never there.
func (p *panelState) toggle() bool {
	if !p.canShow() {
		return false
	}

	p.visible = !p.visible

	return true
}

// setSize records the terminal size.
func (p *panelState) setSize(width, height int) {
	p.width = width
	p.height = height
}

// pageWidth returns the width the pages get.
//
// The panel takes its slice of the width, and the page gets the rest.
// The gap is not part of either: it is the column between them.
func (p *panelState) pageWidth() int {
	if !p.visible {
		return p.width
	}

	left, _ := theme.SplitWidth(p.width, p.width-panelColumnWidth-panelGap, panelGap)

	return left
}

// panelWidth returns the width the panel gets.
func (p *panelState) panelWidth() int {
	if !p.visible {
		return 0
	}

	_, right := theme.SplitWidth(p.width, p.width-panelColumnWidth-panelGap, panelGap)

	return right
}

// panelGapWidth returns the width of the column between the page and the
// panel.
func (p *panelState) panelGapWidth() int {
	if !p.visible {
		return 0
	}

	return panelGap
}

// contentHeight returns the height of the space above the status bar.
//
// The status bar spans the whole width at the bottom, so the panel and
// the page share what is left.
func (p *panelState) contentHeight() int {
	height := p.height - 1
	if height < 1 {
		return 1
	}

	return height
}

// resize adapts the panel to the available space.
//
// The panel is as tall as the space above the status bar: the status
// bar spans the whole width at the bottom.
func (p *panelState) resize() {
	p.panel.SetSize(p.panelWidth(), p.contentHeight())
}

// togglePanel shows or hides the agent panel.
//
// It returns the command of the active page, which has to be told the
// width it has now, and whether the panel was toggled: it cannot be shown
// on a terminal that is too narrow, and a key that does nothing is better
// than a key that hides a panel that was never there.
func (m *Model) togglePanel() (tea.Cmd, bool) {
	if !m.panel.toggle() {
		return nil, false
	}

	// The panel is a view of the conversation, so it is filled when it
	// opens: the user may toggle it long after the run started.
	m.refreshPanel()

	// The pages change width, so they have to be told: the page is
	// narrower while the panel is open, and its layout is computed from
	// the width it is given.
	m.resize()

	return m.resizeActivePage(), true
}

// viewWithPanel renders the page with the agent panel on its right.
//
// The status bar spans the whole width at the bottom, so the panel sits
// next to the body only: the panel stops where the status bar starts.
//
// The panel is as tall as the space above the status bar, and the body
// is padded to the same height, so that the panel shows its whole
// transcript even when the page is short.
func (m *Model) viewWithPanel(parts []string, status string) tea.View {
	body := joinLines(parts)

	panel := m.panel.panel.View().Content
	if panel == "" {
		return tea.NewView(joinLines(append(parts, status)))
	}

	bodyLines := strings.Split(body, "\n")
	panelLines := strings.Split(panel, "\n")

	// The body is padded to the width of the page, so that the panel
	// starts at the same column on every line.
	bodyWidth := m.panel.pageWidth()

	// The gap is the column between the page and the panel.
	gap := m.panel.panelGapWidth()

	// The panel is as tall as the space above the status bar.
	height := m.panel.contentHeight()

	if len(bodyLines) > height {
		bodyLines = bodyLines[:height]
	}

	if len(panelLines) > height {
		panelLines = panelLines[:height]
	}

	padded := make([]string, 0, height)

	for i := 0; i < height; i++ {
		left := ""
		if i < len(bodyLines) {
			left = bodyLines[i]
		}

		left = theme.Truncate(left, bodyWidth)
		left = padRight(left, bodyWidth+gap)

		right := ""
		if i < len(panelLines) {
			right = panelLines[i]
		}

		padded = append(padded, left+right)
	}

	view := tea.NewView(joinLines(append(padded, status)))

	// The alternate screen and the mouse mode are properties of the
	// view in Bubble Tea v2, not of the program: they must be set on
	// every rendered frame.
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	return view
}

// padRight pads a string with spaces to the given width.
//
// The width is measured on the visible characters, so the ANSI
// sequences do not count.
func padRight(s string, width int) string {
	gap := width - lipgloss.Width(s)
	if gap <= 0 {
		return s
	}

	return s + strings.Repeat(" ", gap)
}
