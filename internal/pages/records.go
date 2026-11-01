// Package pages holds the isolated screens of the application.
//
// Each page is a self-contained [tea.Model] that owns its own view state
// and communicates with the root model through the shared messages defined
// in this package.
package pages

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// RecordsMsg is the message carrying the result of a record listing.
type RecordsMsg struct {
	// Result holds the fetched page.
	Result *pbstore.PageResult
	// Err holds the error, when the listing failed.
	Err error
	// Page is the page number that was requested.
	Page int
}

// RecordsFilterMsg is the message emitted when the user submits a new
// filter expression.
type RecordsFilterMsg struct {
	// Filter is the new filter expression.
	Filter string
}

// RecordsPage is the record list screen.
//
// It displays the records of the active collection in a bubbles table and
// lets the user filter them with the native PocketBase filter syntax.
type RecordsPage struct {
	// store is the data layer.
	store pbstore.Store
	// runtime holds the shared state.
	runtime *state.Runtime
	// theme is the shared style palette.
	theme *theme.Theme

	// table displays the records.
	table table.Model
	// filter is the filter input.
	filter textinput.Model
	// spinner is shown while a listing is in flight.
	spinner spinner.Model
	// pagination renders the page navigation.
	pagination *components.Pagination
	// renderer formats the record rows.
	renderer components.RecordRenderer

	// loading reports whether a listing is in flight.
	loading bool
	// records holds the records of the current page.
	records []*pbstore.Record
	// width is the available rendering width.
	width int
	// height is the available rendering height.
	height int
	// filterFocused reports whether the filter input has the focus.
	filterFocused bool
}

// NewRecordsPage creates the record list page.
func NewRecordsPage(store pbstore.Store, runtime *state.Runtime, t *theme.Theme, renderer components.RecordRenderer) *RecordsPage {
	if t == nil {
		t = theme.Default()
	}

	if runtime == nil {
		runtime = state.NewRuntime()
	}

	if renderer == nil {
		renderer = &components.DefaultRecordRenderer{}
	}

	filter := textinput.New()
	filter.Placeholder = "filter, eg. title ~ \"go\""
	filter.Prompt = "filter: "
	filter.CharLimit = 200

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = t.Accent

	tbl := table.New(
		table.WithColumns(defaultColumns()),
		table.WithFocused(true),
		table.WithHeight(10),
		// Bubble Tea v2 renders nothing when the table viewport has no
		// width, so a default is required before the first resize.
		table.WithWidth(defaultTableWidth),
	)
	tbl.SetStyles(tableStyles(t))

	return &RecordsPage{
		store:      store,
		runtime:    runtime,
		theme:      t,
		table:      tbl,
		filter:     filter,
		spinner:    sp,
		pagination: components.NewPagination(t, runtime.PerPage),
		renderer:   renderer,
	}
}

// Init implements [tea.Model].
func (p *RecordsPage) Init() tea.Cmd {
	return tea.Batch(p.spinner.Tick, p.Load())
}

// Update implements [tea.Model].
func (p *RecordsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		p.resize()

		return p, nil

	case RecordsMsg:
		p.loading = false
		if msg.Err != nil {
			return p, nil
		}
		// Discard the response of a query that is no longer current.
		if msg.Page != p.runtime.PageNumber {
			return p, nil
		}

		p.applyResult(msg.Result)

		return p, nil

	case RecordsFilterMsg:
		p.runtime.Filter = msg.Filter
		p.runtime.PageNumber = 1
		p.loading = true

		return p, tea.Batch(p.spinner.Tick, p.Load())

	case tea.KeyPressMsg:
		if p.filterFocused {
			switch msg.String() {
			case "enter":
				p.filterFocused = false
				p.filter.Blur()

				return p, tea.Batch(
					func() tea.Msg { return RecordsFilterMsg{Filter: p.filter.Value()} },
					p.Load(),
				)
			case "esc":
				p.filterFocused = false
				p.filter.Blur()
				p.filter.SetValue(p.runtime.Filter)

				return p, nil
			}

			// The focused input consumes the key.
			var cmd tea.Cmd

			p.filter, cmd = p.filter.Update(msg)

			return p, cmd
		}

		switch msg.String() {
		case "/":
			p.filterFocused = true
			p.filter.Focus()
			p.filter.SetValue(p.runtime.Filter)

			return p, nil

		case "enter":
			// Open the highlighted record in the detail page.
			if rec := p.SelectedRecord(); rec != nil {
				return p, func() tea.Msg {
					return OpenRecordMsg{ID: rec.ID}
				}
			}

			return p, nil

		case "up", "k":
			p.table.MoveUp(1)
			return p, nil
		case "down", "j":
			p.table.MoveDown(1)
			return p, nil
		case "left", "h":
			if p.pagination.Page() > 1 {
				p.pagination.SetPage(p.pagination.Page() - 1)
				p.runtime.PageNumber = p.pagination.Page()
				p.loading = true

				return p, tea.Batch(p.spinner.Tick, p.Load())
			}

			return p, nil
		case "right", "l":
			if p.pagination.Page() < p.pagination.TotalPages() {
				p.pagination.SetPage(p.pagination.Page() + 1)
				p.runtime.PageNumber = p.pagination.Page()
				p.loading = true

				return p, tea.Batch(p.spinner.Tick, p.Load())
			}

			return p, nil
		}
	}

	var cmd tea.Cmd

	p.table, cmd = p.table.Update(msg)
	cmds = append(cmds, cmd)

	return p, tea.Batch(cmds...)
}

// View implements [tea.Model].
func (p *RecordsPage) View() tea.View {
	if p.runtime.Collection == "" {
		return tea.NewView(p.theme.Muted.Render("no collection selected — pick one on the home page"))
	}

	var b []string

	header := p.theme.Title.Render("Records · " + p.runtime.Collection)
	if p.runtime.Filter != "" {
		header += "  " + p.theme.Muted.Render("filter: "+p.runtime.Filter)
	}

	b = append(b, header)
	b = append(b, p.filter.View())
	b = append(b, "")

	if p.loading {
		b = append(b, p.spinner.View()+" "+p.theme.Muted.Render("loading records…"))
	} else {
		b = append(b, p.table.View())
		b = append(b, "")
		b = append(b, p.pagination.View().Content)
	}

	return tea.NewView(joinLines(b))
}

// Refresh reloads the current page of records.
//
// It starts the spinner so that the loading state is visible.
func (p *RecordsPage) Refresh() tea.Cmd {
	p.loading = true
	return tea.Batch(p.spinner.Tick, p.Load())
}

// Load returns the command that fetches the current page of records.
//
// All the store access happens inside the command goroutine, so the
// render loop is never blocked by a database call.
func (p *RecordsPage) Load() tea.Cmd {
	store := p.store
	collection := p.runtime.Collection
	filter := p.runtime.Filter
	sortExpr := p.runtime.Sort
	page := p.runtime.PageNumber
	perPage := p.runtime.PerPage

	return func() tea.Msg {
		if store == nil || collection == "" {
			return RecordsMsg{Err: fmt.Errorf("pages: no collection selected"), Page: page}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := store.List(ctx, pbstore.Query{
			Collection: collection,
			Filter:     filter,
			Sort:       sortExpr,
			Page:       page,
			PerPage:    perPage,
		})
		if err != nil {
			return RecordsMsg{Err: err, Page: page}
		}

		return RecordsMsg{Result: result, Page: page}
	}
}

// applyResult updates the table with the fetched records.
func (p *RecordsPage) applyResult(result *pbstore.PageResult) {
	if result == nil {
		return
	}

	rows := make([]table.Row, 0, len(result.Records))
	for _, rec := range result.Records {
		rows = append(rows, table.Row{
			p.renderer.Title(rec),
			p.renderer.Summary(rec),
			rec.ID,
		})
	}

	p.records = result.Records
	p.table.SetRows(clampRows(rows, len(p.table.Columns())))
	p.table.SetCursor(0)
	p.pagination.SetTotal(result.TotalItems, result.Page, result.PerPage)
	p.pagination.SetTruncated(result.Truncated)
	p.runtime.PageNumber = result.Page
}

// clampRows truncates the rows that have more cells than the table has
// columns.
//
// Bubble Tea v2 indexes the columns with the cell position, so a row
// with too many cells panics with an index out of range. The rows built
// here always match the default columns, but the columns are a
// documented extension point, so the mismatch is defended against
// rather than assumed away.
func clampRows(rows []table.Row, columns int) []table.Row {
	if columns <= 0 {
		return rows
	}

	for i, row := range rows {
		if len(row) > columns {
			rows[i] = row[:columns]
		}
	}

	return rows
}

// resize adapts the table height to the available space.
func (p *RecordsPage) resize() {
	// The header, the filter, the blank lines and the pagination take up
	// roughly 7 lines.
	height := p.height - 7
	if height < 3 {
		height = 3
	}

	p.table.SetWidth(p.width)
	p.table.SetHeight(height)
}

// FilterFocused reports whether the filter input has the keyboard focus.
//
// The root model uses it to suspend the global hotkey layer.
func (p *RecordsPage) FilterFocused() bool { return p.filterFocused }

// SelectedRecord returns the record highlighted in the table.
//
// It returns nil when the table is empty.
func (p *RecordsPage) SelectedRecord() *pbstore.Record {
	cursor := p.table.Cursor()
	if cursor < 0 || cursor >= len(p.records) {
		return nil
	}

	return p.records[cursor]
}
