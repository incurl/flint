package pages

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
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

// The timeouts bound the store calls that the pages run in a command.
//
// They are deliberately different: a single read is expected to be
// quick, a list query may scan many records, and a synchronisation
// copies a whole collection over the network.
const (
	// readTimeout bounds a single read: one record, or the list of
	// collections.
	readTimeout = 15 * time.Second
	// listTimeout bounds a list query.
	listTimeout = 30 * time.Second
	// writeTimeout bounds a mutation of a single record.
	writeTimeout = 15 * time.Second
	// syncTimeout bounds a full synchronisation, which copies a whole
	// collection.
	syncTimeout = 10 * time.Minute
)

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

// listRecords runs a timed list query and preloads the script renderer.
//
// It is the shared body of the list commands: the pages differ only in
// the query they build and in the message they return, so the plumbing
// lives here once rather than in each of them.
//
// It must be called from a command. It blocks for the duration of the
// query, and the preload blocks for as long as the script takes.
func listRecords(
	store pbstore.Store,
	renderer components.RecordRenderer,
	query pbstore.Query,
) (*pbstore.PageResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()

	result, err := store.List(ctx, query)
	if err != nil {
		return nil, err
	}

	// The script renderer computes its display data here, off the render
	// loop, rather than while a frame is drawn.
	components.Preload(renderer, result.Records)

	return result, nil
}
