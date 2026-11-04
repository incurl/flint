package theme

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Breakpoints describe the terminal width ranges used to switch the layout.
type Breakpoints struct {
	// Narrow is the maximum width of a single column layout.
	Narrow int
	// Medium is the maximum width of a two column layout.
	Medium int
}

// DefaultBreakpoints returns the breakpoints used by the template.
//
// The values are chosen so that the record list stays readable on a 80
// column terminal and the two column layout kicks in on a 120 column one.
func DefaultBreakpoints() Breakpoints {
	return Breakpoints{Narrow: 100, Medium: 160}
}

// LayoutMode describes the responsive layout in use.
type LayoutMode string

const (
	// LayoutNarrow stacks the content vertically.
	LayoutNarrow LayoutMode = "narrow"
	// LayoutMedium renders the content in two columns.
	LayoutMedium LayoutMode = "medium"
	// LayoutWide renders the content in two columns with extra spacing.
	LayoutWide LayoutMode = "wide"
)

// LayoutModeFor returns the layout mode matching the given terminal width.
func LayoutModeFor(width int, bp Breakpoints) LayoutMode {
	switch {
	case width < bp.Narrow:
		return LayoutNarrow
	case width < bp.Medium:
		return LayoutMedium
	default:
		return LayoutWide
	}
}

// SplitWidth divides the available width between two columns.
//
// The right column receives the extra pixels so that the primary content
// (the record table) keeps its size. A gap of a few cells is left between
// the columns.
func SplitWidth(total, left, gap int) (int, int) {
	if gap < 0 {
		gap = 0
	}

	available := total - gap
	if available < 2 {
		return max(1, total), 0
	}

	if left <= 0 {
		left = available / 2
	}

	if left > available {
		left = available
	}

	right := available - left

	return left, max(0, right)
}

// Truncate shortens a string to the provided width, adding an ellipsis when
// the content is cut.
//
// It is a thin wrapper around [lipgloss.Width] aware truncation that keeps
// the ANSI sequences intact.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}

	if lipgloss.Width(s) <= width {
		return s
	}

	if width == 1 {
		return "…"
	}

	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

// Wrap breaks a text into lines of at most the given width.
//
// The text is broken on the words, so a line reads as text. A word that
// is longer than the width is broken on its characters rather than
// overflowing.
//
// It is the wrapping of a terminal: a viewport that soft-wraps cuts the
// lines at the width, which splits the words in the middle.
func Wrap(text string, width int) []string {
	if width < 1 {
		return []string{""}
	}

	var out []string

	for _, paragraph := range strings.Split(text, "\n") {
		words := splitWords(paragraph)
		if len(words) == 0 {
			out = append(out, "")

			continue
		}

		// The style of the paragraph is put on every line it is
		// broken into: a line that lost it reads wrong.
		style := leadingStyle(paragraph)

		line := ""

		for _, word := range words {
			switch {
			case line == "":
				line = word
			case lipgloss.Width(line)+1+lipgloss.Width(word) <= width:
				line += " " + word
			default:
				out = append(out, style+line+resetSequence)
				line = word
			}
		}

		if line != "" {
			out = append(out, style+line+resetSequence)
		}
	}

	// A word that is longer than the width is broken on its characters.
	for i, line := range out {
		if lipgloss.Width(line) <= width {
			continue
		}

		out[i] = Truncate(line, width)
	}

	return out
}

// splitWords splits a text into its words, keeping the ANSI sequences.
//
// [strings.Fields] keeps them: it splits on the whitespace, and the
// escape sequences stay attached to the words they style. A wrapped
// line that lost its style is a line that reads wrong.
//
// The words are rejoined with a single space, so a text that had a
// double space or a leading indentation loses it. A conversation does
// not care, and the alternative is a wrapper that understands the
// escape sequences.
func splitWords(text string) []string {
	return strings.Fields(text)
}

// leadingStyle returns the ANSI sequences a text starts with.
//
// It is the style of the text: the colour and the attributes it was
// rendered with.
func leadingStyle(text string) string {
	var style strings.Builder

	for len(text) > 0 && text[0] == '\x1b' {
		end := strings.IndexByte(text, 'm')
		if end < 0 {
			break
		}

		style.WriteString(text[:end+1])
		text = text[end+1:]
	}

	return style.String()
}

// resetSequence closes the style of a line.
//
// Without it the style of a line would bleed into the next one.
const resetSequence = "\x1b[0m"

// max returns the larger of the two provided integers.
func max(a, b int) int {
	if a > b {
		return a
	}

	return b
}
