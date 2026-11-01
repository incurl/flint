package pages

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// HomeMsg is the message carrying the list of the available collections.
type HomeMsg struct {
	// Collections holds the discovered collections.
	Collections []pbstore.Collection
	// Err holds the error, when the discovery failed.
	Err error
}

// HomePage is the landing screen.
//
// It lists the available collections and lets the user pick the active
// one. The collection list is fetched through the store, so it works the
// same in the embedded and the remote mode.
type HomePage struct {
	// store is the data layer.
	store pbstore.Store
	// runtime holds the shared state.
	runtime *state.Runtime
	// theme is the shared style palette.
	theme *theme.Theme
	// spinner is shown while the collections are loaded.
	spinner spinner.Model
	// loading reports whether the collection list is being loaded.
	loading bool
	// cursor is the index of the highlighted collection.
	cursor int
	// width is the available rendering width.
	width int
	// height is the available rendering height.
	height int
}

// NewHomePage creates the landing page.
func NewHomePage(store pbstore.Store, runtime *state.Runtime, t *theme.Theme) *HomePage {
	if t == nil {
		t = theme.Default()
	}

	if runtime == nil {
		runtime = state.NewRuntime()
	}

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = t.Accent

	return &HomePage{
		store:   store,
		runtime: runtime,
		theme:   t,
		spinner: sp,
		loading: true,
	}
}

// Init implements [tea.Model].
func (p *HomePage) Init() tea.Cmd {
	return tea.Batch(p.spinner.Tick, p.Load())
}

// Update implements [tea.Model].
func (p *HomePage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height

		return p, nil

	case HomeMsg:
		p.loading = false
		if msg.Err != nil {
			return p, nil
		}

		p.runtime.Collections = msg.Collections

		// Keep the previously selected collection when it still exists.
		for i, c := range msg.Collections {
			if c.Name == p.runtime.Collection {
				p.cursor = i
				break
			}
		}

		return p, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}

			return p, nil
		case "down", "j":
			if p.cursor < len(p.runtime.Collections)-1 {
				p.cursor++
			}

			return p, nil
		case "enter":
			if c := p.selected(); c != nil {
				p.runtime.Collection = c.Name
				p.runtime.PageNumber = 1

				return p, tea.Sequence(
					func() tea.Msg { return NavigateMsg{Page: state.PageRecords} },
				)
			}

			return p, nil
		}
	}

	return p, nil
}

// View implements [tea.Model].
func (p *HomePage) View() string {
	var b []string

	b = append(b, p.theme.Title.Render("Flint — PocketBase TUI scaffold"))
	b = append(b, p.theme.Muted.Render("mode: "+p.store.Mode().String()))
	b = append(b, "")

	if p.loading {
		b = append(b, p.spinner.View()+" "+p.theme.Muted.Render("loading collections…"))
		return joinLines(b)
	}

	if len(p.runtime.Collections) == 0 {
		b = append(b, p.theme.Muted.Render("no collection available"))
		return joinLines(b)
	}

	b = append(b, p.theme.Subtitle.Render("Collections"))
	b = append(b, "")

	for i, c := range p.runtime.Collections {
		marker := "  "

		style := p.theme.Normal
		if i == p.cursor {
			marker = "▸ "
			style = p.theme.Selected
		}

		label := fmt.Sprintf("%-28s %s", c.Name, c.Type)
		b = append(b, style.Render(marker+label))
	}

	b = append(b, "")
	b = append(b, p.theme.Help.Render("↑/↓ select · enter open · 2 records · / search"))

	return joinLines(b)
}

// Refresh reloads the list of the available collections.
func (p *HomePage) Refresh() tea.Cmd {
	p.loading = true
	return tea.Batch(p.spinner.Tick, p.Load())
}

// Load returns the command that fetches the available collections.
func (p *HomePage) Load() tea.Cmd {
	store := p.store

	return func() tea.Msg {
		if store == nil {
			return HomeMsg{Err: fmt.Errorf("pages: no store configured")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		collections, err := store.Collections(ctx)
		if err != nil {
			return HomeMsg{Err: err}
		}

		return HomeMsg{Collections: collections}
	}
}

// selected returns the highlighted collection.
func (p *HomePage) selected() *pbstore.Collection {
	if p.cursor < 0 || p.cursor >= len(p.runtime.Collections) {
		return nil
	}

	return &p.runtime.Collections[p.cursor]
}
