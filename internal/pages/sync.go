package pages

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// SyncMsg is the message carrying the result of a synchronisation.
type SyncMsg struct {
	// Report holds the sync summary.
	Report *pbstore.SyncReport
	// Err holds the error, when the sync failed.
	Err error
}

// SyncPage drives the remote to local cache synchronisation.
//
// The sync is never started automatically: the user has to trigger it
// explicitly, which keeps the template free of background network traffic.
type SyncPage struct {
	// store is the data layer.
	store pbstore.Store
	// runtime holds the shared state.
	runtime *state.Runtime
	// theme is the shared style palette.
	theme *theme.Theme

	// collection is the remote collection to pull.
	collection textinput.Model
	// local is the local collection to write into.
	local textinput.Model
	// filter limits the synced records.
	filter textinput.Model
	// spinner is shown while the sync runs.
	spinner spinner.Model

	// focus tracks the focused input.
	focus int
	// running reports whether a sync is in flight.
	running bool
	// report holds the last sync summary.
	report *pbstore.SyncReport
	// err holds the last sync error.
	err error
	// width is the available rendering width.
	width int
	// height is the available rendering height.
	height int
}

// NewSyncPage creates the sync page.
func NewSyncPage(store pbstore.Store, runtime *state.Runtime, t *theme.Theme) *SyncPage {
	if t == nil {
		t = theme.Default()
	}

	if runtime == nil {
		runtime = state.NewRuntime()
	}

	collection := textinput.New()
	collection.Placeholder = "remote collection"
	collection.CharLimit = 100

	local := textinput.New()
	local.Placeholder = "local collection (same as remote by default)"
	local.CharLimit = 100

	filter := textinput.New()
	filter.Placeholder = "optional filter"
	filter.CharLimit = 300

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = t.Accent

	p := &SyncPage{
		store:      store,
		runtime:    runtime,
		theme:      t,
		collection: collection,
		local:      local,
		filter:     filter,
		spinner:    sp,
	}
	p.focus = 0
	p.collection.Focus()

	return p
}

// Init implements [tea.Model].
func (p *SyncPage) Init() tea.Cmd { return p.spinner.Tick }

// Update implements [tea.Model].
func (p *SyncPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height

		return p, nil

	case SyncMsg:
		p.running = false
		if msg.Err != nil {
			p.err = msg.Err
			return p, nil
		}

		p.err = nil
		p.report = msg.Report

		return p, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "tab":
			p.focus = (p.focus + 1) % 3
			p.applyFocus()

			return p, nil
		case "shift+tab":
			p.focus = (p.focus + 2) % 3
			p.applyFocus()

			return p, nil
		case "ctrl+r":
			if p.running {
				return p, nil
			}

			p.running = true
			p.err = nil

			return p, tea.Batch(p.spinner.Tick, p.Sync())
		}
	}

	var cmds []tea.Cmd

	var cmd tea.Cmd

	p.collection, cmd = p.collection.Update(msg)
	cmds = append(cmds, cmd)

	p.local, cmd = p.local.Update(msg)
	cmds = append(cmds, cmd)

	p.filter, cmd = p.filter.Update(msg)
	cmds = append(cmds, cmd)

	return p, tea.Batch(cmds...)
}

// View implements [tea.Model].
func (p *SyncPage) View() tea.View {
	var b []string

	b = append(b, p.theme.Title.Render("Sync — pull a remote collection into the local store"))
	b = append(b, "")

	if p.store == nil || p.store.Mode() != pbstore.ModeEmbedded {
		b = append(b, p.theme.Warning.Render("sync is only available in the embedded mode"))
		b = append(b, "")
		b = append(b, p.theme.Muted.Render("the remote mode queries the server directly"))

		return tea.NewView(joinLines(b))
	}

	b = append(b, p.theme.Label.Render("remote collection"))
	b = append(b, p.collection.View())
	b = append(b, "")
	b = append(b, p.theme.Label.Render("local collection"))
	b = append(b, p.local.View())
	b = append(b, "")
	b = append(b, p.theme.Label.Render("filter"))
	b = append(b, p.filter.View())
	b = append(b, "")

	if p.running {
		b = append(b, p.spinner.View()+" "+p.theme.Muted.Render("syncing…"))
	}

	if p.err != nil {
		b = append(b, p.theme.Error.Render("✗ "+p.err.Error()))
	}

	if p.report != nil {
		b = append(b, p.theme.Success.Render(fmt.Sprintf(
			"✓ %s: %d fetched · %d created · %d updated · %s",
			p.report.Collection,
			p.report.Fetched,
			p.report.Created,
			p.report.Updated,
			p.report.Duration.Round(time.Millisecond),
		)))
	}

	b = append(b, "")
	b = append(b, p.theme.Help.Render("tab next field · ctrl+r run sync"))

	return tea.NewView(joinLines(b))
}

// Sync returns the command that runs the synchronisation.
//
// The store access happens in the command goroutine, so the render loop
// stays responsive during a long sync.
func (p *SyncPage) Sync() tea.Cmd {
	store := p.store
	req := pbstore.SyncRequest{
		RemoteCollection: p.collection.Value(),
		LocalCollection:  p.local.Value(),
		Filter:           p.filter.Value(),
		BatchSize:        pbstore.DefaultSyncBatchSize,
	}

	return func() tea.Msg {
		if store == nil {
			return SyncMsg{Err: fmt.Errorf("pages: no store configured")}
		}

		if req.RemoteCollection == "" {
			return SyncMsg{Err: fmt.Errorf("pages: the remote collection is required")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
		defer cancel()

		report, err := store.SyncCollection(ctx, req)
		if err != nil {
			return SyncMsg{Err: err}
		}

		return SyncMsg{Report: report}
	}
}

// applyFocus moves the keyboard focus to the selected input.
func (p *SyncPage) applyFocus() {
	p.collection.Blur()
	p.local.Blur()
	p.filter.Blur()

	switch p.focus {
	case 0:
		p.collection.Focus()
	case 1:
		p.local.Focus()
	case 2:
		p.filter.Focus()
	}
}
