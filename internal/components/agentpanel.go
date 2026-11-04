package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/incurl/flint/internal/theme"
)

// AgentPanelWidth is the width of the agent panel.
//
// The value matches the width of the sidebar of the reference
// implementation: the panel shows a transcript, and a transcript needs
// enough room for a line of prose, not for a table.
const AgentPanelWidth = 32

// AgentPanel renders the conversation of the agent as a narrow side
// panel.
//
// It is the *reading* half of the agent: the transcript of what the
// model did, next to the records it did it to. The *writing* half stays
// on the agent page, because a question needs more room than 32 columns.
//
// The panel is read-only on purpose. A prompt in a narrow panel is
// cramped, and the permission prompts need room to show what is about
// to change.
type AgentPanel struct {
	// theme is the shared style palette.
	theme *theme.Theme
	// entries holds the conversation, oldest first.
	entries []AgentPanelEntry
	// width is the available rendering width.
	width int
	// height is the available rendering height.
	height int
	// scroller shows the conversation.
	//
	// The panel is a window on the conversation: it shows the recent
	// work, and the reader can scroll back through the older messages.
	scroller *Scroller
}

// AgentPanelEntry is one line of the panel.
//
// It is a flattened version of the entry of the agent page: the panel
// does not need the role, it needs the text.
type AgentPanelEntry struct {
	// Text is the content of the entry.
	Text string
	// IsTool reports whether the entry is a tool call.
	IsTool bool
	// Failed reports whether the entry is an error.
	Failed bool
}

// NewAgentPanel creates an agent panel.
func NewAgentPanel(t *theme.Theme) *AgentPanel {
	if t == nil {
		t = theme.Default()
	}

	return &AgentPanel{theme: t, width: AgentPanelWidth, scroller: NewScroller(10)}
}

// SetEntries replaces the conversation of the panel.
//
// The entries are copied, so the panel keeps its own view of the
// conversation and the page can keep mutating its own.
func (p *AgentPanel) SetEntries(entries []AgentPanelEntry) {
	p.entries = entries
}

// SetSize sets the available rendering area.
func (p *AgentPanel) SetSize(width, height int) {
	p.width = width
	p.height = height

	p.scroller.SetSize(width, p.scrollerHeight())
}

// Update handles the keys that scroll the conversation.
//
// The panel is read-only, so the pager keys are all it needs.
func (p *AgentPanel) Update(msg tea.Msg) (tea.Cmd, bool) {
	return p.scroller.Update(msg)
}

// scrollerHeight returns the height the conversation gets.
//
// The header takes one line.
func (p *AgentPanel) scrollerHeight() int {
	height := p.height - 1
	if height < 1 {
		height = 1
	}

	return height
}

// View renders the panel.
//
// The panel draws a border on its left, so that the two columns read as
// two regions rather than as one broken table.
func (p *AgentPanel) View() tea.View {
	if p.width <= 0 {
		return tea.NewView("")
	}

	lines := p.lines()

	// The panel fills the height it was given: the border has to run
	// the whole height of the column, or it looks like a fragment.
	for len(lines) < p.height {
		lines = append(lines, "")
	}

	// Every line is padded to the same width: the border is drawn on the
	// left of the block, and a short line would leave the border ragged.
	padded := make([]string, 0, len(lines))

	for _, line := range lines {
		padded = append(padded, padRight(line, p.textWidth()))
	}

	return tea.NewView(p.border().Render(strings.Join(padded, "\n")))
}

// border returns the left border of the panel.
//
// The width is the *total* width of the panel, the border and the
// padding included: a panel that is 32 columns wide has 30 columns of
// text.
func (p *AgentPanel) border() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		PaddingLeft(1).
		Width(p.width)
}

// lines returns the rendered lines of the conversation.
//
// The conversation is truncated to the last lines that fit: the panel
// shows the *recent* work, because that is what the user is watching.
func (p *AgentPanel) lines() []string {
	rendered := make([]string, 0, len(p.entries))

	for _, entry := range p.entries {
		rendered = append(rendered, p.renderEntry(entry)...)
	}

	out := make([]string, 0, len(rendered)+1)
	out = append(out, p.theme.Subtitle.Render("Agent"))

	if len(rendered) == 0 {
		return append(out, p.theme.Muted.Render("No conversation yet."))
	}

	// The conversation is scrolled: the panel shows the recent work,
	// and the reader can scroll back through the older messages.
	p.scroller.SetSize(p.width, p.scrollerHeight())
	p.scroller.SetContent(strings.Join(rendered, "\n"))

	out = append(out, strings.Split(p.scroller.View(), "\n")...)

	return out
}

// renderEntry renders one entry of the conversation.
func (p *AgentPanel) renderEntry(entry AgentPanelEntry) []string {
	switch {
	case entry.IsTool:
		return []string{p.theme.Accent.Render("→ " + theme.Truncate(entry.Text, p.textWidth()))}

	case entry.Failed:
		return []string{p.theme.Error.Render(theme.Truncate(entry.Text, p.textWidth()))}

	default:
		return wrapText(entry.Text, p.textWidth())
	}
}

// textWidth returns the width of the text of the panel.
//
// The border and its padding take two cells.
func (p *AgentPanel) textWidth() int {
	width := p.width - 2
	if width < 1 {
		return 1
	}

	return width
}

// wrapText breaks a text into lines of at most the given width.
//
// The text is broken on the words, so a long word is broken on its
// characters rather than overflowing the panel.
func wrapText(text string, width int) []string {
	if width < 1 {
		return []string{""}
	}

	var out []string

	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			out = append(out, "")

			continue
		}

		line := ""

		for _, word := range words {
			switch {
			case line == "":
				line = word
			case len(line)+1+len(word) <= width:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}

		if line != "" {
			out = append(out, line)
		}
	}

	// A word that is longer than the panel is broken on its characters.
	for i, line := range out {
		if len(line) <= width {
			continue
		}

		out[i] = theme.Truncate(line, width)
	}

	return out
}
