package pages

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// SearchResultMsg is the message carrying the search results.
type SearchResultMsg struct {
	// Result holds the fetched page.
	Result *pbstore.PageResult
	// Err holds the error, when the search failed.
	Err error
}

// SearchItem is a single search result.
//
// It implements [list.Item] so that it can be used with the bubbles list.
type SearchItem struct {
	// Record is the matched record.
	Record *pbstore.Record
	// Name is the short title of the record.
	Name string
	// Summary is the one line description of the record.
	Summary string
}

// FilterValue implements [list.Item].
func (i SearchItem) FilterValue() string { return i.Name + " " + i.Summary }

// Title implements [list.DefaultItem].
//
// The bubbles default delegate only renders the items that implement it.
func (i SearchItem) Title() string { return i.Name }

// Description implements [list.DefaultItem].
func (i SearchItem) Description() string { return i.Summary }

// SearchPage is the dedicated search screen.
//
// The query is expressed with the native PocketBase filter syntax, so it
// supports the full `field ~ "value"`, `field = 1`, `a && b` grammar
// instead of a custom mini-language.
type SearchPage struct {
	// store is the data layer.
	store pbstore.Store
	// runtime holds the shared state.
	runtime *state.Runtime
	// theme is the shared style palette.
	theme *theme.Theme

	// input holds the filter expression.
	input textinput.Model
	// list displays the results.
	list list.Model
	// spinner is shown while a search is in flight.
	spinner spinner.Model
	// renderer formats the result items.
	renderer components.RecordRenderer

	// loading reports whether a search is in flight.
	loading bool
	// err holds the last search error.
	err error
	// width is the available rendering width.
	width int
	// height is the available rendering height.
	height int
}

// NewSearchPage creates the search page.
func NewSearchPage(store pbstore.Store, runtime *state.Runtime, t *theme.Theme, renderer components.RecordRenderer) *SearchPage {
	if t == nil {
		t = theme.Default()
	}
	if runtime == nil {
		runtime = state.NewRuntime()
	}

	input := textinput.New()
	input.Placeholder = `title ~ "go" && status = true`
	input.Prompt = "filter: "
	input.CharLimit = 500

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = t.ListSelectedTitle.Inherit(t.ListSelectedBorder)
	delegate.Styles.SelectedDesc = t.ListSelectedDesc

	l := list.New([]list.Item{}, delegate, 40, 12)
	l.Title = "Search results"
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)

	// The filtering is driven by the explicit PocketBase filter input, so
	// the list itself never filters its items.
	l.SetFilterState(list.Unfiltered)
	l.Styles.Title = t.Title

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = t.Accent

	if renderer == nil {
		renderer = &components.DefaultRecordRenderer{}
	}

	return &SearchPage{
		store:    store,
		runtime:  runtime,
		theme:    t,
		input:    input,
		list:     l,
		spinner:  sp,
		renderer: renderer,
	}
}

// Init implements [tea.Model].
func (p *SearchPage) Init() tea.Cmd {
	p.input.Focus()
	return tea.Batch(p.spinner.Tick, p.Search())
}

// Update implements [tea.Model].
func (p *SearchPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		p.resize()
		return p, nil

	case SearchResultMsg:
		p.loading = false
		if msg.Err != nil {
			p.err = msg.Err
			return p, nil
		}
		p.err = nil
		p.applyResult(msg.Result)
		return p, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			p.loading = true
			return p, tea.Batch(p.spinner.Tick, p.Search())

		case "esc":
			p.input.SetValue("")
			return p, nil
		}
	}

	// The page owns a dedicated PocketBase filter input, so the key
	// presses are never forwarded to the bubbles list: its own filter
	// binding ("/") would otherwise shadow the page filter.
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return p, cmd
}

// View implements [tea.Model].
func (p *SearchPage) View() string {
	var b []string

	b = append(b, p.theme.Title.Render("Search"))
	b = append(b, p.input.View())
	b = append(b, "")

	if p.err != nil {
		b = append(b, p.theme.Error.Render("✗ "+p.err.Error()))
		b = append(b, "")
		b = append(b, p.theme.Help.Render("the filter must use the PocketBase filter syntax"))
		return joinLines(b)
	}

	if p.loading {
		b = append(b, p.spinner.View()+" "+p.theme.Muted.Render("searching…"))
		return joinLines(b)
	}

	b = append(b, p.list.View())
	b = append(b, "")
	b = append(b, p.theme.Help.Render("enter run · esc clear · ↑/↓ navigate"))

	return joinLines(b)
}

// Refresh re-runs the current search.
//
// The filter input takes the keyboard focus, so the user can type a new
// expression as soon as the page is displayed.
func (p *SearchPage) Refresh() tea.Cmd {
	p.loading = true
	p.err = nil
	p.input.Focus()
	p.resize()
	return tea.Batch(p.spinner.Tick, p.Search())
}

// resize adapts the results list to the available space.
func (p *SearchPage) resize() {
	width := p.width
	if width <= 0 {
		width = 80
	}

	// The page renders its own header (title, input) and footer (help),
	// so the list only gets the remaining space.
	height := p.height - 7
	if height < 10 {
		height = 10
	}

	p.list.SetSize(width, height)
}

// Search returns the command that runs the current filter.
//
// The filter is validated by the store, which uses the native PocketBase
// filter parser. An invalid expression is reported as an error message
// instead of crashing the TUI.
func (p *SearchPage) Search() tea.Cmd {
	store := p.store
	collection := p.runtime.Collection
	filter := strings.TrimSpace(p.input.Value())

	return func() tea.Msg {
		if store == nil {
			return SearchResultMsg{Err: fmt.Errorf("pages: no store configured")}
		}
		if collection == "" {
			return SearchResultMsg{Err: fmt.Errorf("pages: select a collection on the home page first")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := store.List(ctx, pbstore.Query{
			Collection: collection,
			Filter:     filter,
			Sort:       p.runtime.Sort,
			Page:       1,
			PerPage:    p.runtime.PerPage,
		})
		if err != nil {
			return SearchResultMsg{Err: err}
		}
		return SearchResultMsg{Result: result}
	}
}

// applyResult updates the list with the search results.
func (p *SearchPage) applyResult(result *pbstore.PageResult) {
	if result == nil {
		return
	}

	items := make([]list.Item, 0, len(result.Records))
	for _, rec := range result.Records {
		items = append(items, SearchItem{
			Record:  rec,
			Name:    p.renderer.Title(rec),
			Summary: p.renderer.Summary(rec),
		})
	}

	// The results are never filtered by the list itself: the page filter
	// input is the single source of truth.
	p.list.SetFilterState(list.Unfiltered)
	p.list.SetItems(items)
}
