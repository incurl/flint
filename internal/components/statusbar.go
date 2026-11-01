package components

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/incurl/flint/internal/theme"
)

// StatusInfo carries the data rendered by the [StatusBar].
type StatusInfo struct {
	// Mode is the store mode, eg. "embedded" or "remote".
	Mode string
	// Connection is the connection state, eg. "ok" or "error".
	Connection string
	// ConnectionDetail is the short detail of the last health check.
	ConnectionDetail string
	// Collection is the active collection name.
	Collection string
	// Sync is the sync state, eg. "idle" or "running".
	Sync string
	// SyncProgress is the human readable sync progress.
	SyncProgress string
	// Page is the active page name.
	Page string
	// Error holds the latest error, if any.
	Error string
	// Hints holds the page specific hotkey hints.
	Hints []string
}

// StatusBar renders the persistent bottom bar of the application.
//
// It always shows the store mode, the connection health, the active
// collection and the sync status, plus the contextual hotkey hints.
type StatusBar struct {
	// theme is the shared style palette.
	theme *theme.Theme
	// width is the available rendering width.
	width int
	// info holds the currently displayed data.
	info StatusInfo
}

// NewStatusBar creates a status bar component.
func NewStatusBar(t *theme.Theme) *StatusBar {
	if t == nil {
		t = theme.Default()
	}

	return &StatusBar{theme: t}
}

// SetInfo replaces the displayed data.
func (s *StatusBar) SetInfo(info StatusInfo) { s.info = info }

// Info returns the currently displayed data.
func (s *StatusBar) Info() StatusInfo { return s.info }

// SetWidth sets the available rendering width.
func (s *StatusBar) SetWidth(width int) { s.width = width }

// View renders the status bar.
//
// The bar degrades gracefully: the hints are dropped one by one when the
// terminal is too narrow, then the right section.
func (s *StatusBar) View() tea.View {
	left := s.leftSection()
	right := s.rightSection()

	// The minimum room needed to render the sections without overlap.
	const minGap = 1
	// The separator inserted between the hints and the right section.
	const sepWidth = 2

	// The number of displayed hints is computed locally so that the
	// component state is never mutated by a render.
	visible := len(s.info.Hints)
	for visible > 0 && s.width > 0 &&
		lipgloss.Width(left)+lipgloss.Width(right)+lipgloss.Width(s.hintsSectionN(visible))+sepWidth > s.width {
		visible--
	}

	hints := s.hintsSectionN(visible)

	// Drop the right section when it still does not fit.
	if s.width > 0 && lipgloss.Width(left)+lipgloss.Width(right)+minGap > s.width {
		right = ""
	}

	// The separator keeps the hints apart from the right section.
	separator := ""
	if hints != "" && right != "" {
		separator = strings.Repeat(" ", sepWidth)
	}

	gap := minGap
	if s.width > 0 {
		gap = s.width - lipgloss.Width(left) - lipgloss.Width(right) -
			lipgloss.Width(hints) - lipgloss.Width(separator)
		if gap < minGap {
			gap = minGap
		}
	}

	return tea.NewView(s.theme.StatusBar.Render(left + strings.Repeat(" ", gap) + hints + separator + right))
}

// leftSection renders the store mode and the connection health.
func (s *StatusBar) leftSection() string {
	mode := s.theme.StatusMode.Render(strings.ToUpper(s.info.Mode))

	connection := s.info.Connection
	if connection == "" {
		connection = "unknown"
	}

	conn := s.theme.StatusColor(connection).Render("● " + connection)

	parts := []string{mode, conn}

	if s.info.Collection != "" {
		parts = append(parts, s.theme.Muted.Render(s.info.Collection))
	}

	if s.info.Sync != "" && s.info.Sync != "idle" {
		parts = append(parts, s.theme.StatusColor(s.info.Sync).Render(s.info.Sync))
	}

	if s.info.SyncProgress != "" {
		parts = append(parts, s.theme.Muted.Render(s.info.SyncProgress))
	}

	return strings.Join(parts, s.theme.Muted.Render(" │ "))
}

// rightSection renders the page name and the latest error.
func (s *StatusBar) rightSection() string {
	parts := make([]string, 0, 2)

	if s.info.Page != "" {
		parts = append(parts, s.theme.Muted.Render(s.info.Page))
	}

	if s.info.Error != "" {
		parts = append(parts, s.theme.Error.Render("✗ "+s.info.Error))
	}

	return strings.Join(parts, s.theme.Muted.Render(" │ "))
}

// hintsSectionN renders the first n contextual hotkey hints.
//
// It is used to drop the trailing hints when the terminal is too narrow.
func (s *StatusBar) hintsSectionN(n int) string {
	if n <= 0 || len(s.info.Hints) == 0 {
		return ""
	}

	if n > len(s.info.Hints) {
		n = len(s.info.Hints)
	}

	rendered := make([]string, 0, n)
	for _, h := range s.info.Hints[:n] {
		rendered = append(rendered, s.theme.Help.Render(h))
	}

	return strings.Join(rendered, "  ")
}

// FormatHints renders a list of "key action" pairs into the compact form
// used by the status bar.
//
// The result looks like "1 home  2 records  / search  q quit".
func FormatHints(pairs [][2]string) []string {
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p[0] == "" || p[1] == "" {
			continue
		}

		out = append(out, fmt.Sprintf("%s %s", p[0], p[1]))
	}

	return out
}
