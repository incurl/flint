package pages

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

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
	// ID is the identifier the request was made for.
	//
	// The page compares it to the currently selected record before
	// applying the result: a slow response for a record the user has
	// already left must not replace the one they are looking at.
	ID string
}

// SavedMsg reports the outcome of a write.
//
// The page separates a successful save from a failed one because only
// the success may reload: the reload replaces the editor content with
// the stored document, which would discard the very text the user needs
// in order to fix a rejected write.
type SavedMsg struct {
	// ID is the identifier the write was made for.
	//
	// The page compares it to the current selection: the write is
	// asynchronous, so the response can arrive after the user has
	// moved on to another record.
	ID string
	// Err holds the write error, when the save failed.
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
		// A response for a record the user has already left must not
		// replace the one they are looking at: the requests are
		// asynchronous and the slower one can arrive last.
		if msg.ID != p.runtime.SelectedID {
			return p, nil
		}

		if msg.Err != nil || msg.Record == nil {
			return p, nil
		}

		p.setRecord(msg.Record)

		return p, nil

	case tea.KeyPressMsg:
		if p.editing {
			switch msg.String() {
			case "esc":
				p.cancelEdit()

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

				return p, p.editor.Focus()
			case "r":
				p.runtime.RawJSON = !p.runtime.RawJSON
				return p, nil
			}
		}

	case SavedMsg:
		// A write for a record the user has already left must not
		// replace the one they are looking at.
		if msg.ID != p.runtime.SelectedID {
			return p, nil
		}

		if msg.Err != nil {
			return p, toastCmd(components.ToastError, msg.Err.Error())
		}

		// The write succeeded, so the editor closes and the page reloads
		// what the store holds rather than what was typed into it.
		p.editing = false
		p.editor.Blur()

		return p, tea.Batch(
			toastCmd(components.ToastSuccess, "record saved"),
			p.Load(),
		)
	}

	// The open editor owns the input: the viewer behind it must not
	// scroll or collapse nodes under the cursor while the user types.
	if p.editing {
		updated, cmd := p.editor.Update(msg)
		p.editor = updated.(*components.JSONEditor)

		return p, cmd
	}

	updated, cmd := p.viewer.Update(msg)
	p.viewer = updated.(*components.JSONViewer)

	// The card scrolls its fields. The keys are the pager ones, and
	// they are handled before the viewer, which has its own.
	if cmd, handled := p.card.Update(msg); handled {
		return p, cmd
	}

	return p, cmd
}

// View implements [tea.Model].
func (p *DetailPage) View() tea.View {
	if p.record == nil {
		return tea.NewView(p.theme.Muted.Render("no record selected"))
	}

	var b []string

	b = append(b, p.theme.Title.Render("Record · "+p.record.CollectionName))
	b = append(b, "")

	if p.editing {
		b = append(b, p.theme.Subtitle.Render("Raw JSON editor"))
		b = append(b, "")
		b = append(b, p.editor.View().Content)

		return tea.NewView(joinLines(b))
	}

	if p.runtime.RawJSON {
		b = append(b, p.viewer.View().Content)
		b = append(b, "")
		b = append(b, p.theme.Help.Render("↑/↓ move · enter expand/collapse · e edit · r structured view"))

		return tea.NewView(joinLines(b))
	}

	b = append(b, p.card.View().Content)
	b = append(b, "")
	b = append(b, p.theme.Subtitle.Render("Raw JSON"))
	b = append(b, "")
	b = append(b, p.viewer.View().Content)
	b = append(b, "")
	b = append(b, p.theme.Help.Render("j json · r raw json · e edit · esc back"))

	return tea.NewView(joinLines(b))
}

// Editing reports whether the raw JSON editor is open.
//
// The root model keeps the global hotkeys out of the editor while it is
// open, so that the document can be typed without losing it: `q` would
// quit, and the digits would navigate away from the record.
func (p *DetailPage) Editing() bool { return p.editing }

// Refresh reloads the displayed record.
func (p *DetailPage) Refresh() tea.Cmd { return p.Load() }

// Load returns the command that fetches the record by id.
func (p *DetailPage) Load() tea.Cmd {
	store := p.store
	renderer := p.renderer
	collection := p.runtime.Collection
	id := p.runtime.SelectedID

	return func() tea.Msg {
		if store == nil || collection == "" || id == "" {
			return DetailMsg{Err: fmt.Errorf("pages: no record selected"), ID: id}
		}

		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()

		rec, err := store.Get(ctx, collection, id)
		if err != nil {
			return DetailMsg{Err: err, ID: id}
		}

		components.Preload(renderer, []*pbstore.Record{rec})

		return DetailMsg{Record: rec, ID: id}
	}
}

// save validates the edited JSON and writes it back through the store.
//
// The validation runs before the command is returned: it is pure, and a
// document that cannot be saved must never leave the editor.
func (p *DetailPage) save() tea.Cmd {
	store := p.store
	collection := p.runtime.Collection
	id := p.runtime.SelectedID

	doc, err := p.editor.Validate()
	if err != nil {
		return toastCmd(components.ToastError, "invalid JSON: "+err.Error())
	}

	return func() tea.Msg {
		if store == nil {
			return SavedMsg{ID: id, Err: fmt.Errorf("pages: no store configured")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		defer cancel()

		if _, err := store.Update(ctx, collection, id, doc); err != nil {
			return SavedMsg{ID: id, Err: err}
		}

		return SavedMsg{ID: id}
	}
}

// cancelEdit closes the editor and discards the changes.
//
// The editor keeps the text that was typed, so cancelling has to put
// the stored document back: otherwise the next `e` would show the
// abandoned edit as if it were the record.
func (p *DetailPage) cancelEdit() {
	p.editing = false
	p.editor.Blur()

	if p.record == nil {
		return
	}

	if raw, err := p.record.JSON(); err == nil {
		p.editor.SetContent(raw)
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

	// The card holds the title, the summary and the fields, and the
	// viewer holds the raw JSON below it: they share the page.
	p.card.SetHeight(p.height - viewerHeight - 4)
}
