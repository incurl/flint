package wikidata

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/theme"
)

// requestTimeout bounds the HTTP calls of the example.
const requestTimeout = 15 * time.Second

// SearchResultMsg carries the result of a Wikidata query.
type SearchResultMsg struct {
	// Record holds the first matching item.
	Record *pbstore.Record
	// Err holds the error, when the query failed.
	Err error
}

// Page is a Wikidata specific screen.
//
// It owns its own state and only talks to the store through the
// [pbstore.Store] interface.
type Page struct {
	// store is the data layer.
	store pbstore.Store
	// labels is the label lookup cache.
	labels *LabelStore
	// theme is the shared style palette.
	theme *theme.Theme
	// renderer formats the items.
	renderer *Renderer

	// query is the search input.
	query textinput.Model
	// card renders the selected item.
	card *components.RecordCard
	// loading reports whether a query is in flight.
	loading bool
	// err holds the last error.
	err error
}

// NewPage creates the Wikidata page.
func NewPage(store pbstore.Store, labels *LabelStore, t *theme.Theme) *Page {
	if t == nil {
		t = theme.Default()
	}

	query := textinput.New()
	query.Placeholder = "Q42 or a label"
	query.Prompt = "query: "

	return &Page{
		store:    store,
		labels:   labels,
		theme:    t,
		renderer: NewRenderer("en"),
		query:    query,
		card:     components.NewRecordCard(t, NewRenderer("en")),
	}
}

// Init implements [tea.Model].
func (p *Page) Init() tea.Cmd {
	p.query.Focus()
	return p.search()
}

// Update implements [tea.Model].
func (p *Page) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SearchResultMsg:
		p.loading = false
		if msg.Err != nil {
			p.err = msg.Err
			return p, nil
		}

		p.err = nil
		if msg.Record != nil {
			p.card.SetRecord(msg.Record)
		}

		return p, nil

	case tea.KeyPressMsg:
		if msg.String() == "enter" {
			p.loading = true
			return p, tea.Batch(p.search())
		}
	}

	var cmd tea.Cmd

	p.query, cmd = p.query.Update(msg)

	return p, cmd
}

// View implements [tea.Model].
func (p *Page) View() tea.View {
	header := p.theme.Title.Render("Wikidata") + "\n" + p.query.View()

	switch {
	case p.loading:
		return tea.NewView(header + "\n\n" + p.theme.Muted.Render("searching…"))
	case p.err != nil:
		return tea.NewView(header + "\n\n" + p.theme.Error.Render(p.err.Error()))
	default:
		return tea.NewView(header + "\n\n" + p.card.View().Content)
	}
}

// search returns the command that runs the query.
//
// The store access happens inside the command goroutine, so the render
// loop is never blocked.
func (p *Page) search() tea.Cmd {
	store, filter := p.store, BuildFilter(p.query.Value())

	return func() tea.Msg {
		if store == nil {
			return SearchResultMsg{Err: fmt.Errorf("wikidata: no store configured")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()

		result, err := store.List(ctx, pbstore.Query{
			Collection: ItemsCollection,
			Filter:     filter,
			PerPage:    20,
		})
		if err != nil {
			return SearchResultMsg{Err: err}
		}

		if len(result.Records) == 0 {
			return SearchResultMsg{}
		}

		return SearchResultMsg{Record: result.Records[0]}
	}
}
