package pages

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// The geometry of the record view.
//
// The page fills the box it is given: the fields and the JSON tree share
// everything between the title and the help line, and the layout adds up
// to the height exactly. A page that renders one line too many loses the
// line at the end, and one that renders too few leaves the terminal
// empty.
const (
	// detailHeaderLines is the height of the title and the blank line
	// under it.
	detailHeaderLines = 2
	// detailFooterLines is the height of the help line at the bottom.
	detailFooterLines = 1
	// detailEditorLines is the height the raw JSON editor adds below
	// its textarea, for the save hints or the validation error.
	detailEditorLines = 1
	// detailEditorHeaderLines is the height of the editor title and the
	// blank line under it.
	detailEditorHeaderLines = 2
	// detailGutter is the number of blank columns between the panes.
	detailGutter = 2
	// detailMinHeight is the smallest useful height of a pane.
	detailMinHeight = 3
	// detailMinPaneWidth is the narrowest pane that still reads: a page
	// narrower than two of them stacks the panes instead of splitting
	// it.
	detailMinPaneWidth = 30
	// detailMaxCardWidth is the widest the fields are given: on a very
	// wide page the tree uses the room the card does not need.
	detailMaxCardWidth = 70
	// detailDefaultWidth and detailDefaultHeight are the size the page
	// assumes before the terminal reports its own, so that it renders
	// on the very first frame.
	detailDefaultWidth  = 80
	detailDefaultHeight = 24
)

// detailLayout is the geometry of the record view.
type detailLayout struct {
	// width is the whole width of the page.
	width int
	// cardWidth and cardHeight are the box of the structured fields.
	cardWidth  int
	cardHeight int
	// treeWidth and treeHeight are the box of the JSON tree.
	treeWidth  int
	treeHeight int
	// columns reports whether the two boxes sit side by side.
	columns bool
}

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

	page := &DetailPage{
		store:    store,
		runtime:  runtime,
		theme:    t,
		card:     components.NewRecordCard(t, renderer),
		viewer:   components.NewJSONViewer(t),
		editor:   components.NewJSONEditor(t),
		renderer: renderer,
	}

	// The panes are laid out from the first frame: the terminal sends
	// its size shortly after the program starts, and a page that waits
	// for it renders nothing until then.
	page.resize()

	return page
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
			case "e":
				p.editing = true

				// The editor owns the input: the tree behind it must
				// not move or collapse under the cursor.
				p.viewer.Blur()
				p.resize()

				return p, p.editor.Focus()
			case "r":
				p.runtime.RawJSON = !p.runtime.RawJSON
				p.resize()

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
		p.viewer.Focus()
		p.resize()

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
//
// The record view is the whole page: the structured fields and the raw
// JSON share the space between the title and the help line. On a wide
// terminal they sit side by side; on a narrow one the fields are above
// the tree.
func (p *DetailPage) View() tea.View {
	if p.record == nil {
		return tea.NewView(p.theme.Muted.Render("no record selected"))
	}

	header := p.theme.Title.Render("Record · " + p.record.CollectionName)

	if p.editing {
		return tea.NewView(joinLines([]string{
			header,
			"",
			p.theme.Subtitle.Render("Raw JSON editor"),
			"",
			p.editor.View().Content,
		}))
	}

	l := p.layout()

	body := p.bodyLines(l)

	lines := make([]string, 0, len(body)+detailHeaderLines+detailFooterLines)
	lines = append(lines, header, "")
	lines = append(lines, body...)
	lines = append(lines, p.theme.Help.Render(theme.Truncate(p.helpText(), l.width)))

	return tea.NewView(joinLines(lines))
}

// bodyLines renders the panes, exactly as tall as the layout says.
//
// Every block is cut or padded to the box it was given: the geometry of
// the page is fixed, and the content is whatever the record holds.
func (p *DetailPage) bodyLines(l detailLayout) []string {
	if p.runtime.RawJSON {
		return fitLines(p.viewer.View().Content, l.treeWidth, l.treeHeight)
	}

	if l.columns {
		card := fitLines(p.card.View().Content, l.cardWidth, l.cardHeight)
		tree := fitLines(p.viewer.View().Content, l.treeWidth, l.treeHeight)

		return joinColumns(card, tree, l.cardWidth, detailGutter)
	}

	body := fitLines(p.card.View().Content, l.cardWidth, l.cardHeight)

	return append(body, fitLines(p.viewer.View().Content, l.treeWidth, l.treeHeight)...)
}

// helpText returns the key hints of the page.
func (p *DetailPage) helpText() string {
	if p.runtime.RawJSON {
		return "↑/↓ move · pgup/pgdn scroll · enter expand/collapse · e edit · r structured view · esc back"
	}

	return "↑/↓ move · enter expand/collapse · e edit · r raw json · esc back"
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

	// The tree takes the keys again, and the page is laid out for the
	// two panes.
	p.viewer.Focus()
	p.resize()

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

	// The tree owns the keys while the page is browsed: the arrows move
	// the cursor and `enter` expands and collapses a node without a key
	// to arm the viewer first.
	if !p.editing {
		p.viewer.Focus()
	}

	p.resize()
}

// resize adapts the panes to the available space.
func (p *DetailPage) resize() {
	width, height := p.size()

	// The editor is the page while it is open: the title, the editor
	// title, and the document with its own help line under it.
	if p.editing {
		editorHeight := height - detailHeaderLines - detailEditorHeaderLines - detailEditorLines
		if editorHeight < detailMinHeight {
			editorHeight = detailMinHeight
		}

		p.editor.SetSize(width, editorHeight)

		return
	}

	l := p.layout()

	p.card.SetWidth(l.cardWidth)
	p.card.SetHeight(l.cardHeight)
	p.viewer.SetSize(l.treeWidth, l.treeHeight)
}

// size returns the space the page has.
//
// The terminal reports its size just after the program starts, and the
// page has to render before that: the defaults keep the first frame from
// being empty.
func (p *DetailPage) size() (int, int) {
	width := p.width
	if width <= 0 {
		width = detailDefaultWidth
	}

	height := p.height
	if height <= 0 {
		height = detailDefaultHeight
	}

	return width, height
}

// layout computes the geometry of the page.
//
// The fields and the tree get everything between the title and the help
// line: on a wide page side by side, on a narrow one stacked. Neither is
// given a fixed share of the terminal, so the record view uses all of it.
func (p *DetailPage) layout() detailLayout {
	width, height := p.size()

	// The body is everything between the title and the help line, and
	// it keeps at least one line: a tiny page is cut, never overflowed.
	body := height - detailHeaderLines - detailFooterLines
	if body < 1 {
		body = 1
	}

	// The raw view is the tree on its own, and it gets the whole body.
	if p.runtime.RawJSON {
		return detailLayout{
			width:      width,
			cardWidth:  width,
			cardHeight: body,
			treeWidth:  width,
			treeHeight: body,
		}
	}

	// Two columns as soon as the page is wide enough: the fields on the
	// left, the tree on the right, each as tall as the body. The card
	// stops growing on a very wide page, and the tree takes the rest.
	if theme.LayoutModeFor(width, theme.DefaultBreakpoints()) != theme.LayoutNarrow {
		cardWidth := min((width-detailGutter)/2, detailMaxCardWidth)

		left, right := theme.SplitWidth(width, cardWidth, detailGutter)
		if left >= detailMinPaneWidth && right >= detailMinPaneWidth {
			return detailLayout{
				width:      width,
				cardWidth:  left,
				cardHeight: body,
				treeWidth:  right,
				treeHeight: body,
				columns:    true,
			}
		}
	}

	// A narrow page stacks them: the fields on top, the tree below. The
	// tree keeps at least one line, and the card takes what is left, so
	// that the two add up to the body whatever its height.
	cardHeight := max(body/2, 1)
	if cardHeight > body-1 {
		cardHeight = body - 1
	}

	return detailLayout{
		width:      width,
		cardWidth:  width,
		cardHeight: cardHeight,
		treeWidth:  width,
		treeHeight: body - cardHeight,
	}
}

// fitLines returns exactly height lines of at most width columns.
//
// A block that is taller is cut and one that is shorter is padded: the
// panes have a fixed geometry, and the content is whatever the record
// holds. Padding the short lines is also what keeps the columns of a two
// column layout aligned.
func fitLines(content string, width, height int) []string {
	if height < 1 {
		return nil
	}

	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}

	out := make([]string, height)

	for i, line := range lines {
		out[i] = padLine(theme.Truncate(line, width), width)
	}

	for i := len(lines); i < height; i++ {
		out[i] = strings.Repeat(" ", max(width, 0))
	}

	return out
}

// joinColumns places the two panes side by side.
//
// The left pane is padded to its width: a line that is shorter would let
// the right pane start in a different column.
func joinColumns(left, right []string, width, gap int) []string {
	height := max(len(left), len(right))
	out := make([]string, 0, height)

	for i := range height {
		var l, r string

		if i < len(left) {
			l = left[i]
		}

		if i < len(right) {
			r = right[i]
		}

		out = append(out, padLine(l, width)+strings.Repeat(" ", gap)+strings.TrimRight(r, " "))
	}

	return out
}

// padLine pads a line with spaces up to the requested width.
//
// The padding is computed on the visible characters, so the ANSI escape
// sequences are not counted.
func padLine(line string, width int) string {
	visible := lipgloss.Width(line)
	if visible >= width {
		return line
	}

	return line + strings.Repeat(" ", width-visible)
}
