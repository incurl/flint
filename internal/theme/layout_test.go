package theme_test

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/incurl/flint/internal/theme"
)

// TestWrapBreaksOnTheWords verifies that a text is broken on the words.
//
// A viewport that soft-wraps cuts the lines at the width, which splits
// the words in the middle: a line has to read as text.
func TestWrapBreaksOnTheWords(t *testing.T) {
	lines := theme.Wrap("a very long sentence that does not fit", 12)

	for _, line := range lines {
		if lipgloss.Width(line) > 12 {
			t.Errorf("the line %q is %d wide, want at most 12", line, lipgloss.Width(line))
		}
	}

	// Every word is there.
	joined := strings.Join(lines, " ")

	for _, word := range strings.Fields("a very long sentence that does not fit") {
		if !strings.Contains(joined, word) {
			t.Errorf("the word %q is missing from %q", word, joined)
		}
	}
}

// TestWrapKeepsTheParagraphs verifies that the blank lines of a text
// are kept.
func TestWrapKeepsTheParagraphs(t *testing.T) {
	lines := theme.Wrap("first\n\nsecond", 20)

	if len(lines) != 3 {
		t.Fatalf("the wrap returned %d lines, want 3: %q", len(lines), lines)
	}

	if lines[1] != "" {
		t.Errorf("the blank line is %q, want it empty", lines[1])
	}
}

// TestWrapBreaksALongWord verifies that a word that is longer than the
// width does not overflow.
func TestWrapBreaksALongWord(t *testing.T) {
	lines := theme.Wrap("supercalifragilistic", 10)

	for _, line := range lines {
		if lipgloss.Width(line) > 10 {
			t.Errorf("the line %q is %d wide, want at most 10", line, lipgloss.Width(line))
		}
	}
}

// TestWrapKeepsTheStyles verifies that the wrapping keeps the ANSI
// sequences.
func TestWrapKeepsTheStyles(t *testing.T) {
	styled := theme.Default().Accent.Render("a very long styled sentence indeed")

	lines := theme.Wrap(styled, 12)

	if len(lines) < 2 {
		t.Fatalf("the wrap returned %d lines: %q", len(lines), lines)
	}

	for _, line := range lines {
		if !strings.Contains(line, "\x1b[") {
			t.Errorf("the line %q lost its style", line)
		}
	}
}
