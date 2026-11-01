package theme

import "charm.land/lipgloss/v2"

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

// max returns the larger of the two provided integers.
func max(a, b int) int {
	if a > b {
		return a
	}

	return b
}
