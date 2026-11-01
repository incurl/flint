package pages

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// DetailMsg is the message carrying the loaded record.
type DetailMsg struct {
	// Record holds the fetched record.
	Record *pbstore.Record
	// Err holds the error, when the loading failed.
	Err error
}

// DetailPage is the record detail screen.
//
// It combines the generic record card with the collapsible JSON viewer, so
// that both the structured fields and the raw payload are reachable.
type DetailPage struct {
	// store is the data layer.
	store pbstore.Store
	// runtime holds the shared state.
	runtime *state.Runtime
	// theme is the shared style palette.
	theme *theme.Theme

	// card renders the structured fields.
	card *components.RecordCard
	// viewer renders the collapsible JSON tree.
	viewer *components.JSONViewer
	// editor is the raw JSON editing form.
	editor *components.JSONEditor
	// renderer formats the record content.
	renderer components.RecordRenderer

	// record is the displayed record.
	record *pbstore.Record
	// editing reports whether the raw JSON editor is active.
	editing bool
	// width is the available rendering width.
	width int
	// height is the available rendering height.
	height int
}

// NewDetailPage creates the record detail page.
func NewDetailPage(store pbstore.Store, runtime *state.Runtime, t *theme.Theme, renderer components.RecordRenderer) *DetailPage {
	if t == nil {
		t = theme.Default()
	}

	if runtime == nil {
		runtime = state.NewRuntime()
	}

	return &DetailPage{
		store:    store,
		runtime:  runtime,
		theme:    t,
		card:     components.NewRecordCard(t, renderer),
		viewer:   components.NewJSONViewer(t),
		editor:   components.NewJSONEditor(t),
		renderer: renderer,
	}
}

// Init implements [tea.Model].
func (p *DetailPage) Init() tea.Cmd { return nil }

// Update implements [tea.Model].
func (p *DetailPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		p.resize()

		return p, nil

	case DetailMsg:
		if msg.Err != nil || msg.Record == nil {
			return p, nil
		}

		p.setRecord(msg.Record)

		return p, nil

	case tea.KeyMsg:
		if p.editing {
			switch msg.String() {
			case "esc":
				p.editing = false
				p.editor.Blur()

				return p, nil
			case "ctrl+s":
				return p, p.save()
			}
		} else {
			switch msg.String() {
			case "j":
				p.viewer.Focus()
				return p, nil
			case "e":
				p.editing = true
				p.editor.Focus()

				return p, nil
			case "r":
				p.runtime.RawJSON = !p.runtime.RawJSON
				return p, nil
			}
		}
	}

	updated, cmd := p.viewer.Update(msg)
	p.viewer = updated.(*components.JSONViewer)

	return p, cmd
}

// View implements [tea.Model].
func (p *DetailPage) View() string {
	if p.record == nil {
		return p.theme.Muted.Render("no record selected")
	}

	var b []string

	b = append(b, p.theme.Title.Render("Record · "+p.record.CollectionName))
	b = append(b, "")

	if p.editing {
		b = append(b, p.theme.Subtitle.Render("Raw JSON editor"))
		b = append(b, "")
		b = append(b, p.editor.View())

		return joinLines(b)
	}

	if p.runtime.RawJSON {
		b = append(b, p.viewer.View())
		b = append(b, "")
		b = append(b, p.theme.Help.Render("↑/↓ move · enter expand/collapse · e edit · r structured view"))

		return joinLines(b)
	}

	b = append(b, p.card.View())
	b = append(b, "")
	b = append(b, p.theme.Subtitle.Render("Raw JSON"))
	b = append(b, "")
	b = append(b, p.viewer.View())
	b = append(b, "")
	b = append(b, p.theme.Help.Render("j json · r raw json · e edit · esc back"))

	return joinLines(b)
}

// Refresh reloads the displayed record.
func (p *DetailPage) Refresh() tea.Cmd { return p.Load() }

// Load returns the command that fetches the record by id.
func (p *DetailPage) Load() tea.Cmd {
	store := p.store
	collection := p.runtime.Collection
	id := p.runtime.SelectedID

	return func() tea.Msg {
		if store == nil || collection == "" || id == "" {
			return DetailMsg{Err: fmt.Errorf("pages: no record selected")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		rec, err := store.Get(ctx, collection, id)
		if err != nil {
			return DetailMsg{Err: err}
		}

		return DetailMsg{Record: rec}
	}
}

// save validates the edited JSON and writes it back through the store.
//
// The store errors are reported as a toast by the root model.
func (p *DetailPage) save() tea.Cmd {
	store := p.store
	collection := p.runtime.Collection
	id := p.runtime.SelectedID

	doc, err := p.editor.Validate()
	if err != nil {
		return func() tea.Msg {
			return components.ToastMsg{Level: components.ToastError, Text: "invalid JSON: " + err.Error()}
		}
	}

	return func() tea.Msg {
		if store == nil {
			return components.ToastMsg{Level: components.ToastError, Text: "no store configured"}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if _, err := store.Update(ctx, collection, id, doc); err != nil {
			return components.ToastMsg{Level: components.ToastError, Text: err.Error()}
		}

		return components.ToastMsg{Level: components.ToastSuccess, Text: "record saved"}
	}
}

// setRecord replaces the displayed record and refreshes the sub-components.
func (p *DetailPage) setRecord(rec *pbstore.Record) {
	p.record = rec
	p.card.SetRecord(rec)

	if raw, err := rec.JSON(); err == nil {
		_ = p.viewer.SetContent(raw)
		p.editor.SetContent(raw)
	} else {
		p.viewer.SetContentValue(rec.Fields)
		p.editor.SetContent("{}")
	}
}

// resize adapts the components to the available space.
func (p *DetailPage) resize() {
	// The header, the titles and the help line take up roughly 8 lines.
	viewerHeight := p.height - 8
	if viewerHeight < 3 {
		viewerHeight = 3
	}

	p.viewer.SetSize(p.width, viewerHeight)
	p.editor.SetSize(p.width, p.height-6)
	p.card.SetWidth(p.width)
}
