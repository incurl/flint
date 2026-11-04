package components

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// Scroller shows a block of content that may be taller than the space
// it has.
//
// It is a viewport with one behaviour on top: it **follows the end**
// while the reader is at the end. A conversation that grows is read
// from its last line, and the reader who scrolls up to look at
// something is not yanked back down by the next message.
//
// The reader decides: scrolling up detaches the scroller from the
// end, and scrolling back to the end attaches it again.
type Scroller struct {
	// viewport scrolls the content.
	viewport viewport.Model
	// follow reports whether the scroller follows the end of the
	// content.
	follow bool
	// width is the available rendering width.
	width int
	// height is the available rendering height.
	height int
}

// NewScroller creates a scroller.
//
// The height is the number of lines it shows. A scroller with no
// height shows nothing.
func NewScroller(height int) *Scroller {
	v := viewport.New(viewport.WithHeight(height))

	return &Scroller{
		viewport: v,
		follow:   true,
		width:    80,
		height:   height,
	}
}

// SetSize sets the available rendering area.
func (s *Scroller) SetSize(width, height int) {
	if width > 0 {
		s.width = width
	}

	if height > 0 {
		s.height = height
	}

	s.viewport.SetWidth(s.width)
	s.viewport.SetHeight(s.height)
}

// SetContent replaces the content.
//
// The scroller follows the end when it was following it: a new line
// appears at the bottom, and the reader is already there.
func (s *Scroller) SetContent(content string) {
	s.viewport.SetContent(content)

	if s.follow {
		s.viewport.GotoBottom()
	}
}

// Content returns the content of the scroller.
func (s *Scroller) Content() string { return s.viewport.GetContent() }

// AtBottom reports whether the scroller shows the end of the content.
//
// It is what the page uses to decide whether the "more below" marker
// is needed.
func (s *Scroller) AtBottom() bool { return s.viewport.AtBottom() }

// Follow reports whether the scroller follows the end of the content.
func (s *Scroller) Follow() bool { return s.follow }

// SetFollow attaches or detaches the scroller from the end.
//
// It is what a page does when the reader takes over: the reader
// decides what is in view, and the content does not move under them.
func (s *Scroller) SetFollow(follow bool) { s.follow = follow }

// GotoBottom shows the end of the content and follows it again.
func (s *Scroller) GotoBottom() {
	s.follow = true
	s.viewport.GotoBottom()
}

// Update handles the keys that scroll.
//
// The keys are the pager ones: pgup and pgdn for a page, and ctrl+u
// and ctrl+d for half a page.
//
// The up and down arrows are **not** handled: they belong to the text
// input next to the content, and a caret that does not move is a
// caret the reader cannot place.
//
// The scroller stops following the end as soon as the reader scrolls
// up, and follows it again when they come back.
func (s *Scroller) Update(msg tea.Msg) (tea.Cmd, bool) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, false
	}

	before := s.viewport.YOffset()

	switch key.String() {
	case "pgup":
		s.viewport.HalfPageUp()
	case "pgdown":
		s.viewport.HalfPageDown()
	case "ctrl+u":
		s.viewport.HalfPageUp()
	case "ctrl+d":
		s.viewport.HalfPageDown()
	default:
		return nil, false
	}

	// The reader moved: the scroller follows the end only when the
	// reader is at it.
	if s.viewport.YOffset() != before {
		s.follow = s.viewport.AtBottom()
	}

	return nil, true
}

// View renders the scroller.
//
// It returns the visible lines, and nothing else: the height is the
// height, so the content never spills over the space it was given.
func (s *Scroller) View() string {
	if s.height <= 0 {
		return ""
	}

	return s.viewport.View()
}

// TruncateLines returns the last lines of a content, and reports
// whether there were more.
//
// It is for the pages that do not scroll: a page that cannot scroll
// shows the end of its content, because the end is what the reader
// came for.
func TruncateLines(content string, height int) (string, bool) {
	if height <= 0 {
		return "", false
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= height {
		return content, false
	}

	return strings.Join(lines[len(lines)-height:], "\n"), true
}
