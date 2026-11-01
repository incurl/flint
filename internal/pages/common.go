package pages

import (
	"strings"

	"charm.land/bubbles/v2/table"

	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// defaultTableWidth is the width used by the record table before the
// first window resize.
//
// Bubble Tea v2 renders an empty table when its viewport has no width,
// so the table needs a usable default from the start. The value matches
// the sum of the default column widths.
const defaultTableWidth = 88

// defaultColumns returns the columns of the record table.
//
// The layout is generic on purpose: a downstream project can replace the
// table columns through the record renderer without touching the core
// packages.
func defaultColumns() []table.Column {
	return []table.Column{
		{Title: "Title", Width: 32},
		{Title: "Summary", Width: 36},
		{Title: "ID", Width: 20},
	}
}

// tableStyles returns the lipgloss styles of the record table.
//
// Every style comes from the theme package: the pages never build an
// inline lipgloss style.
func tableStyles(t *theme.Theme) table.Styles {
	return table.Styles{
		Selected: t.TableSelected,
		Header:   t.TableHeader.Inherit(t.TableHeaderBorder),
		Cell:     t.TableCell,
	}
}

// joinLines renders a list of lines as a single block.
func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

// NavigateMsg is emitted when a page requests a navigation to another
// page.
//
// It lives in the pages package so that a page can request a navigation
// without importing the root model (which would create an import cycle).
type NavigateMsg struct {
	// Page is the target page.
	Page state.Page
}

// OpenRecordMsg is emitted when the user opens a record in the detail page.
//
// It lives in the pages package so that a page can request the detail
// view without importing the root model.
type OpenRecordMsg struct {
	// ID is the identifier of the record to open.
	ID string
}
