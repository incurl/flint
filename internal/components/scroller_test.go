package components_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/components"
)

// TestTheScrollerShowsTheEnd verifies that the scroller shows the end
// of the content.
//
// A conversation is read from its last line: the reader is waiting for
// the answer.
func TestTheScrollerShowsTheEnd(t *testing.T) {
	scroller := components.NewScroller(5)
	scroller.SetSize(40, 5)

	scroller.SetContent(strings.Join([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, "\n"))

	view := scroller.View()

	if !strings.Contains(view, "10") {
		t.Errorf("the scroller does not show the end:\n%s", view)
	}

	if strings.Contains(view, "1\n") {
		t.Errorf("the scroller shows the beginning:\n%s", view)
	}

	if !scroller.AtBottom() {
		t.Error("the scroller is not at the bottom")
	}
}

// TestTheScrollerKeepsTheParagraphs verifies that the blank lines of a
// text are kept.
//
// The content is wrapped before it reaches the viewport, and the
// viewport counts the lines again: a blank line that is counted twice
// becomes two blank lines, and the text of a plan grows a line for
// every step it has.
func TestTheScrollerKeepsTheParagraphs(t *testing.T) {
	scroller := components.NewScroller(10)
	scroller.SetSize(40, 10)

	scroller.SetContent("# Plan\n\n1. Do this.\n2. Do that.")

	view := scroller.View()
	lines := strings.Split(view, "\n")

	// The lines are padded to the width, so they are trimmed.
	trimmed := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmed = append(trimmed, strings.TrimRight(line, " "))
	}

	if trimmed[0] != "# Plan" {
		t.Errorf("the first line is %q, want the title", trimmed[0])
	}

	if trimmed[1] != "" {
		t.Errorf("the second line is %q, want it empty", trimmed[1])
	}

	if trimmed[2] != "1. Do this." {
		t.Errorf("the third line is %q, want the first step", trimmed[2])
	}

	if trimmed[3] != "2. Do that." {
		t.Errorf("the fourth line is %q, want the second step", trimmed[3])
	}
}

// TestTheScrollerWrapsOnTheWords verifies that a long line is wrapped
// and not cut.
//
// A viewport cuts the lines that are too long, and the rest of the text
// is lost: the reader would never know that it was there.
func TestTheScrollerWrapsOnTheWords(t *testing.T) {
	scroller := components.NewScroller(10)
	scroller.SetSize(20, 10)

	scroller.SetContent(strings.Repeat("word ", 20))

	view := scroller.View()

	missing := 0

	for _, word := range strings.Fields(strings.Repeat("word ", 20)) {
		if !strings.Contains(view, word) {
			missing++
		}
	}

	if missing != 0 {
		t.Errorf("%d words are missing from the view:\n%s", missing, view)
	}

	// The words are not split: a line ends on a word boundary.
	for _, line := range strings.Split(view, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if !strings.HasPrefix(trimmed, "word") {
			t.Errorf("the line %q does not start on a word", trimmed)
		}
	}
}

// TestTheScrollerScrollsUp verifies that the reader can scroll back.
func TestTheScrollerScrollsUp(t *testing.T) {
	scroller := components.NewScroller(5)
	scroller.SetSize(40, 5)

	scroller.SetContent(strings.Join([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, "\n"))

	_, handled := scroller.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if !handled {
		t.Fatal("the page up key was not handled")
	}

	view := scroller.View()

	if !strings.Contains(view, "5") {
		t.Errorf("the scroller did not scroll up:\n%s", view)
	}

	if scroller.AtBottom() {
		t.Error("the scroller is still at the bottom")
	}

	// The scroller stopped following the end: the reader is in charge.
	if scroller.Follow() {
		t.Error("the scroller still follows the end")
	}
}

// TestTheScrollerFollowsTheEnd verifies that a new line does not move
// the reader who scrolled up.
//
// The reader decides what is in view.
func TestTheScrollerFollowsTheEnd(t *testing.T) {
	scroller := components.NewScroller(5)
	scroller.SetSize(40, 5)

	scroller.SetContent(strings.Join([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, "\n"))

	_, _ = scroller.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})

	// A new line arrives.
	scroller.SetContent(strings.Join([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}, "\n"))

	if !strings.Contains(scroller.View(), "5") {
		t.Errorf("the reader was moved by the new line:\n%s", scroller.View())
	}

	// The reader comes back to the end.
	scroller.GotoBottom()

	if !strings.Contains(scroller.View(), "11") {
		t.Errorf("the scroller did not go back to the end:\n%s", scroller.View())
	}
}

// TestTheScrollerFollowsTheEndWhenTheReaderIsThere verifies that a new
// line is followed while the reader is at the end.
func TestTheScrollerFollowsTheEndWhenTheReaderIsThere(t *testing.T) {
	scroller := components.NewScroller(5)
	scroller.SetSize(40, 5)

	scroller.SetContent(strings.Join([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, "\n"))

	scroller.SetContent(strings.Join([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}, "\n"))

	if !strings.Contains(scroller.View(), "11") {
		t.Errorf("the scroller did not follow the end:\n%s", scroller.View())
	}
}

// TestTheScrollerIgnoresTheOtherKeys verifies that the scroller does
// not swallow the keys that are not its own.
func TestTheScrollerIgnoresTheOtherKeys(t *testing.T) {
	scroller := components.NewScroller(5)

	if _, handled := scroller.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}); handled {
		t.Error("the scroller handled a key that is not its own")
	}
}

// TestTheScrollerLeavesTheArrowsToThePrompt verifies that the up and
// down arrows are not handled.
//
// They belong to the text input next to the content: a caret that does
// not move is a caret the reader cannot place.
func TestTheScrollerLeavesTheArrowsToThePrompt(t *testing.T) {
	scroller := components.NewScroller(5)
	scroller.SetSize(40, 5)

	scroller.SetContent(strings.Join([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, "\n"))

	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyUp}, {Code: tea.KeyDown}} {
		if _, handled := scroller.Update(key); handled {
			t.Errorf("the scroller handled %q", key.String())
		}
	}

	// The content did not move.
	if !strings.Contains(scroller.View(), "10") {
		t.Errorf("the scroller moved:\n%s", scroller.View())
	}
}

// TestTruncateLines verifies the helper for the pages that do not
// scroll.
//
// The end of the content is what the reader came for.
func TestTruncateLines(t *testing.T) {
	content := strings.Join([]string{"1", "2", "3", "4", "5"}, "\n")

	got, truncated := components.TruncateLines(content, 10)
	if truncated {
		t.Error("a short content was truncated")
	}

	if got != content {
		t.Errorf("TruncateLines() = %q, want the content", got)
	}

	got, truncated = components.TruncateLines(content, 2)
	if !truncated {
		t.Error("a long content was not truncated")
	}

	if got != "4\n5" {
		t.Errorf("TruncateLines() = %q, want the last lines", got)
	}
}
