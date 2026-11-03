package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pages"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// The timeouts bound the background calls that the root model runs.
const (
	// healthTimeout bounds the health probe of the backend.
	healthTimeout = 10 * time.Second
	// syncTimeout bounds a full synchronisation, which copies a whole
	// collection.
	syncTimeout = 10 * time.Minute
)

// Model is the root Bubble Tea model.
//
// It composes the page submodels and routes the messages between them:
//
//   - the global messages (store errors, sync events, toasts) are handled
//     here;
//   - the page local messages are forwarded to the active page only.
type Model struct {
	// store is the data layer.
	store pbstore.Store
	// cfg is the configuration the application was started with.
	//
	// It is kept so that the guided setup can write a configuration
	// file with the settings the application holds.
	cfg *config.Config
	// setup is the agent setup.
	//
	// It is kept so that the guided setup can install a model in the
	// runner, without a restart.
	setup *agent.Setup
	// runtime holds the shared runtime state.
	runtime *state.Runtime
	// theme is the shared style palette.
	theme *theme.Theme
	// settings holds the rendered configuration rows.
	settings [][2]string
	// prefs holds the user preferences that survive a restart.
	prefs pbstore.Preferences

	// home is the landing page.
	home *pages.HomePage
	// records is the record list page.
	records *pages.RecordsPage
	// detail is the record detail page.
	detail *pages.DetailPage
	// search is the search page.
	search *pages.SearchPage
	// agentPage is the chat with the language model.
	agentPage *pages.AgentPage
	// panel is the side panel that shows the conversation of the agent.
	panel *panelState
	// settingsPage is the settings page.
	settingsPage *pages.SettingsPage
	// sync is the sync page.
	sync *pages.SyncPage

	// statusBar is the persistent bottom bar.
	statusBar *components.StatusBar
	// toast is the transient notification.
	toast *components.Toast

	// width is the terminal width.
	width int
	// height is the terminal height.
	height int
	// pageWidth is the width the pages get, which is narrower than the
	// terminal when the panel is visible.
	pageWidth int
	// quitting reports whether the application is shutting down.
	quitting bool
}

// New creates the root model.
//
// The renderer is the optional domain specific record renderer: when nil
// the generic [components.DefaultRecordRenderer] is used.
func New(store pbstore.Store, cfg *config.Config, renderer components.RecordRenderer, setup *agent.Setup) *Model {
	if cfg == nil {
		cfg = config.Default()
	}

	if renderer == nil {
		renderer = &components.DefaultRecordRenderer{}
	}

	if setup == nil {
		setup = &agent.Setup{}
	}

	// The user preferences override the deployment configuration: they
	// describe how the application looks to the person using it.
	prefs := pbstore.Preferences{
		Theme:   cfg.Theme,
		Scale:   int(theme.ScaleDefault),
		PerPage: cfg.PerPage,
	}

	if stored, err := pbstore.LoadPreferences(context.Background(), store); err == nil {
		if stored.Theme != "" {
			prefs.Theme = stored.Theme
		}

		if stored.Scale > 0 {
			prefs.Scale = stored.Scale
		}

		if stored.PerPage > 0 {
			prefs.PerPage = stored.PerPage
		}

		prefs.Compact = stored.Compact
	}

	t := theme.New(theme.Variant(prefs.Theme), theme.Scale(prefs.Scale))
	runtime := state.NewRuntime()
	runtime.PerPage = prefs.PerPage
	runtime.Compact = prefs.Compact

	m := &Model{
		store:        store,
		cfg:          cfg,
		setup:        setup,
		runtime:      runtime,
		prefs:        prefs,
		theme:        t,
		settings:     cfg.SettingsRows(),
		statusBar:    components.NewStatusBar(t),
		toast:        components.NewToast(t),
		home:         pages.NewHomePage(store, runtime, t),
		records:      pages.NewRecordsPage(store, runtime, t, renderer),
		detail:       pages.NewDetailPage(store, runtime, t, renderer),
		search:       pages.NewSearchPage(store, runtime, t, renderer),
		agentPage:    pages.NewAgentPage(setup.Runner, setup.Resolver, setup.Service, runtime, t),
		panel:        newPanelState(t),
		settingsPage: pages.NewSettingsPage(runtime, t, cfg.SettingsRows()),
		sync:         pages.NewSyncPage(store, runtime, t),
	}

	// The settings page shows the effective values, so the stored
	// preferences must be reflected in it from the start.
	m.refreshSettings()

	// The guided setup of the agent can write an example configuration
	// file. The application owns the configuration, so it is the one
	// that does the writing.
	m.agentPage.SetConfigWriter(config.WritePath(cfg.Path()), func(path string, settings components.AgentSetupSettings) error {
		return cfg.SaveAgent(agentSettings(settings), path)
	})

	// The guided setup can also install a model, so that the agent works
	// right away without a restart.
	m.agentPage.SetModelApplier(func(settings components.AgentSetupSettings) error {
		return m.installModel(settings)
	})

	return m
}

// installModel builds a model from the settings of the setup and
// installs it in the runner.
//
// The key is the one the user typed, and it is used to build the model
// in memory: it is never written to a file. The configuration names
// the environment variable that holds it, so the next start finds it
// there.
//
// The model takes effect on the next turn: a run that is already in
// flight keeps the model it started with.
func (m *Model) installModel(settings components.AgentSetupSettings) error {
	if m.setup == nil || m.setup.Runner == nil {
		return errors.New("app: the agent is not set up")
	}

	model, err := agent.NewModel(agent.ProviderConfig{
		Provider: settings.Provider,
		Model:    settings.Model,
		BaseURL:  settings.BaseURL,
		APIKey:   settings.APIKey,
	})
	if err != nil {
		return fmt.Errorf("app: build the model: %w", err)
	}

	if err := m.setup.Runner.SetModel(model); err != nil {
		return fmt.Errorf("app: install the model: %w", err)
	}

	return nil
}

// SetConfigPath sets the path the configuration file is written to.
//
// The guided setup of the agent writes an example configuration file,
// and the path is the one the application was started with: a user who
// keeps the configuration in a specific place expects the application
// to write there.
func (m *Model) SetConfigPath(path string) {
	if path == "" {
		return
	}

	m.agentPage.SetConfigWriter(path, func(path string, settings components.AgentSetupSettings) error {
		return m.cfg.SaveAgent(agentSettings(settings), path)
	})
}

// agentSettings maps the settings of the setup to the configuration.
//
// The API key is not in them: the setup never gives it up, and the
// configuration names the environment variable that holds it.
func agentSettings(settings components.AgentSetupSettings) config.AgentConfig {
	return config.AgentConfig{
		Provider:  settings.Provider,
		Model:     settings.Model,
		BaseURL:   settings.BaseURL,
		APIKeyEnv: settings.APIKeyEnv,
	}
}

// Init implements [tea.Model].
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.home.Init(),
		m.checkHealth(),
	)
}

// Update implements [tea.Model].
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()

		// The pages get the width the panel does not use, so the
		// resize is forwarded with the narrower width. A page that
		// renders a table needs to know how much room it has.
		if m.pageWidth != m.width {
			msg.Width = m.pageWidth
		}

	case tea.KeyPressMsg:
		// The global hotkey layer is only active when no page specific
		// input has the focus.
		if cmd, handled := m.handleGlobalKey(msg); handled {
			m.refreshStatusBar()
			return m, cmd
		}

	case pages.NavigateMsg:
		return m, m.navigate(msg.Page)

	case pages.OpenRecordMsg:
		m.runtime.SelectedID = msg.ID
		return m, m.navigate(state.PageDetail)

	case HealthMsg:
		m.applyHealth(msg)
		m.refreshStatusBar()

		return m, nil

	case CollectionsMsg:
		if msg.Err != nil {
			m.runtime.SetError(msg.Err)
		} else {
			m.runtime.Collections = msg.Collections
		}

		m.refreshStatusBar()

		return m, nil

	case SyncStartedMsg:
		m.runtime.Sync = state.SyncRunning
		m.runtime.SyncProgress = msg.Collection
		m.refreshStatusBar()

		return m, nil

	case SyncFinishedMsg:
		m.runtime.Sync = state.SyncDone

		m.runtime.SyncProgress = ""
		if msg.Err != nil {
			m.runtime.Sync = state.SyncFailed
			m.runtime.SetError(msg.Err)
			m.refreshStatusBar()

			return m, tea.Batch(
				m.toast.Show(components.ToastError, "sync failed: "+msg.Err.Error()),
				m.reloadActivePage(),
			)
		}

		m.refreshStatusBar()

		return m, tea.Batch(
			m.toast.Show(components.ToastSuccess, fmt.Sprintf(
				"synced %d records (%d created, %d updated)",
				msg.Report.Fetched, msg.Report.Created, msg.Report.Updated)),
			m.reloadActivePage(),
		)

	case components.ToastMsg:
		m.refreshStatusBar()
		return m, m.toast.Show(msg.Level, msg.Text)

	case components.ToastExpiredMsg:
		// The toast component owns its own expiry.
		updated, cmd := m.toast.Update(msg)
		m.toast = updated.(*components.Toast)

		return m, cmd

	case pages.RecordsMsg:
		// Discard the response of a query that is no longer current, so
		// that a slow response does not resurrect a stale error toast.
		if msg.Page != m.runtime.PageNumber {
			break
		}

		if msg.Err != nil {
			m.runtime.SetError(msg.Err)
			m.refreshStatusBar()

			return m, m.toast.Show(components.ToastError, msg.Err.Error())
		}

		m.runtime.SetError(nil)
	}

	// Forward the message to the active page.
	cmds = append(cmds, m.updateActivePage(msg)...)

	// The status bar reflects the shared runtime state, so it is refreshed
	// on every update cycle.
	m.refreshStatusBar()

	// The panel shows the conversation of the agent, which changes with
	// the agent events.
	m.refreshPanel()

	// The guided setup has done its work once a model is installed: the
	// page shows the chat from then on.
	if m.agentPage.HasModel() {
		m.agentPage.DropSetup()
	}

	return m, tea.Batch(cmds...)
}

// refreshStatusBar pushes the current runtime state to the status bar.
func (m *Model) refreshStatusBar() {
	m.statusBar.SetInfo(m.statusInfo())
}

// refreshPanel updates the conversation the panel shows.
//
// The panel is a view of the agent page's conversation, so it is
// refreshed whenever the page is: there is no second copy of the
// conversation to keep in sync.
func (m *Model) refreshPanel() {
	if !m.panel.visible {
		return
	}

	m.panel.panel.SetEntries(m.agentPage.PanelEntries())
}

// View implements [tea.Model].
func (m *Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	var body string

	switch m.runtime.Page {
	case state.PageRecords:
		body = m.records.View().Content
	case state.PageDetail:
		body = m.detail.View().Content
	case state.PageSearch:
		body = m.search.View().Content
	case state.PageAgent:
		body = m.agentPage.View().Content
	case state.PageSettings:
		body = m.settingsPage.View().Content
	case state.PageSync:
		body = m.sync.View().Content
	default:
		body = m.home.View().Content
	}

	parts := []string{body}

	if toast := m.toast.View().Content; toast != "" {
		parts = append(parts, toast)
	}

	status := m.statusBar.View().Content

	// The agent panel sits on the right of the body and of the status
	// bar: the status bar spans the whole width, so the panel has to
	// stop before it.
	if m.panel.visible {
		return m.viewWithPanel(parts, status)
	}

	parts = append(parts, status)

	view := tea.NewView(joinLines(parts))

	// The alternate screen and the mouse mode are properties of the
	// view in Bubble Tea v2, not of the program: they must be set on
	// every rendered frame.
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	return view
}

// handleGlobalKey processes the global hotkeys.
//
// It returns the command to run and whether the key was consumed. The
// global layer is disabled while a text input has the focus, so that the
// user can type freely.
func (m *Model) handleGlobalKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	// A modal editor owns every key but the interrupt. The detail page is
	// the one page that goes modal, and it has to: a JSON document is
	// full of the characters the global layer binds, so `q` would quit
	// and the digits would navigate away from an unsaved edit.
	if m.modalInputFocused() {
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return tea.Quit, true
		}

		return nil, false
	}

	// While a text input has the focus the global layer only handles the
	// interrupt key and the page switch digits, so that the user can type
	// freely in the input while still being able to jump between pages.
	if m.inputFocused() {
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return tea.Quit, true
		case "1":
			return m.navigate(state.PageHome), true
		case "2":
			return m.navigate(state.PageRecords), true
		case "3":
			return m.navigate(state.PageDetail), true
		case "4":
			return m.navigate(state.PageSearch), true
		case "5":
			return m.navigate(state.PageSettings), true
		case "6":
			return m.navigate(state.PageSync), true
		case "7":
			return m.navigate(state.PageAgent), true
		}

		return nil, false
	}

	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return tea.Quit, true

	case "ctrl+b":
		// The panel is a chrome toggle, like the theme: it is not a
		// page and it does not navigate.
		if m.togglePanel() {
			return nil, true
		}

		// The panel does not fit on this terminal: the key is left to
		// the page, which may use it for something else.
		return nil, false

	case "+", "=":
		return m.zoom(theme.ScaleZoomIn), true

	case "-", "_":
		return m.zoom(theme.ScaleZoomOut), true

	case "T":
		return m.toggleTheme(), true

	case "c":
		return m.toggleCompact(), true

	case "1":
		return m.navigate(state.PageHome), true

	case "2":
		return m.navigate(state.PageRecords), true

	case "3":
		return m.navigate(state.PageDetail), true

	case "4":
		return m.navigate(state.PageSearch), true

	case "5":
		return m.navigate(state.PageSettings), true

	case "6":
		return m.navigate(state.PageSync), true

	case "7":
		return m.navigate(state.PageAgent), true

	case "s":
		return m.checkHealth(), true

	case "y":
		return m.triggerSync(), true

	case "esc":
		m.runtime.GoBack()
		m.refreshStatusBar()

		return m.loadPage(m.runtime.Page), true
	}

	// The "r" hotkey refreshes the active page, but it is not available
	// on the pages that use it for a local toggle (eg. the raw JSON view).
	if msg.String() == "r" && !m.hasLocalRBinding() {
		return m.reloadActivePage(), true
	}

	return nil, false
}

// hasLocalRBinding reports whether the active page uses the "r" key for a
// page local action.
//
// The global layer must not shadow those bindings.
func (m *Model) hasLocalRBinding() bool {
	return m.runtime.Page == state.PageDetail
}

// modalInputFocused reports whether the active page is in a modal
// editing state, where every key but the interrupt belongs to the page.
func (m *Model) modalInputFocused() bool {
	return m.runtime.Page == state.PageDetail && m.detail.Editing()
}

// inputFocused reports whether a page currently owns a focused text input.
//
// When it returns true the global hotkey layer is suspended.
func (m *Model) inputFocused() bool {
	switch m.runtime.Page {
	case state.PageSearch:
		return true
	case state.PageAgent:
		return true
	case state.PageRecords:
		return m.records.FilterFocused()
	default:
		return false
	}
}

// navigate switches the active page.
//
// It returns the command that loads the data of the target page, so that
// every page shows fresh content as soon as it becomes visible.
func (m *Model) navigate(page state.Page) tea.Cmd {
	m.runtime.SetPage(page)
	m.refreshStatusBar()

	return m.loadPage(page)
}

// loadPage returns the command that loads the data of a page.
func (m *Model) loadPage(page state.Page) tea.Cmd {
	switch page {
	case state.PageRecords:
		return m.records.Refresh()
	case state.PageDetail:
		if m.runtime.SelectedID == "" {
			return nil
		}

		return m.detail.Refresh()
	case state.PageSearch:
		return m.search.Refresh()
	case state.PageHome:
		return m.home.Refresh()
	default:
		return nil
	}
}

// updateActivePage forwards a message to the active page.
func (m *Model) updateActivePage(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	// The events of a running agent reach the agent page whatever page
	// is displayed: a run outlives the page it was started from, and the
	// side panel shows the conversation of a run the user started and
	// then navigated away from.
	//
	// The agent page is skipped below, so that an event is not applied
	// twice when it is the page that is displayed.
	if m.runtime.Page != state.PageAgent {
		switch msg.(type) {
		case pages.AgentEventMsg, pages.AgentDoneMsg, pages.AgentPermissionMsg:
			updated, cmd := m.agentPage.Update(msg)
			m.agentPage = updated.(*pages.AgentPage)

			cmds = append(cmds, cmd)
		}
	}

	switch m.runtime.Page {
	case state.PageRecords:
		updated, cmd := m.records.Update(msg)
		m.records = updated.(*pages.RecordsPage)

		cmds = append(cmds, cmd)
	case state.PageDetail:
		updated, cmd := m.detail.Update(msg)
		m.detail = updated.(*pages.DetailPage)

		cmds = append(cmds, cmd)
	case state.PageSearch:
		updated, cmd := m.search.Update(msg)
		m.search = updated.(*pages.SearchPage)

		cmds = append(cmds, cmd)
	case state.PageAgent:
		updated, cmd := m.agentPage.Update(msg)
		m.agentPage = updated.(*pages.AgentPage)

		cmds = append(cmds, cmd)
	case state.PageSettings:
		updated, cmd := m.settingsPage.Update(msg)
		m.settingsPage = updated.(*pages.SettingsPage)

		cmds = append(cmds, cmd)
	case state.PageSync:
		updated, cmd := m.sync.Update(msg)
		m.sync = updated.(*pages.SyncPage)

		cmds = append(cmds, cmd)
	default:
		updated, cmd := m.home.Update(msg)
		m.home = updated.(*pages.HomePage)

		cmds = append(cmds, cmd)
	}

	return cmds
}

// reloadActivePage refreshes the data of the active page.
func (m *Model) reloadActivePage() tea.Cmd {
	switch m.runtime.Page {
	case state.PageRecords:
		return m.records.Load()
	case state.PageDetail:
		return m.detail.Load()
	case state.PageSearch:
		return m.search.Search()
	case state.PageHome:
		return m.home.Load()
	default:
		return nil
	}
}

// checkHealth returns the command that verifies the store connectivity.
func (m *Model) checkHealth() tea.Cmd {
	store := m.store

	return func() tea.Msg {
		if store == nil {
			return HealthMsg{Status: "no store", Err: fmt.Errorf("app: no store configured")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
		defer cancel()

		if _, err := store.HealthCheck(ctx); err != nil {
			return HealthMsg{Status: "unreachable", Err: err}
		}

		return HealthMsg{Status: "ok"}
	}
}

// triggerSync returns the command that starts a synchronisation.
//
// The sync is always explicit: it is triggered by the user hotkey and
// never runs in the background.
func (m *Model) triggerSync() tea.Cmd {
	store := m.store
	collection := m.runtime.Collection

	if store == nil {
		return m.toast.Show(components.ToastError, "no store configured")
	}

	if store.Mode() != pbstore.ModeEmbedded {
		return m.toast.Show(components.ToastWarning, "sync is only available in the embedded mode")
	}

	if collection == "" {
		return m.toast.Show(components.ToastWarning, "select a collection first")
	}

	req := pbstore.SyncRequest{
		RemoteCollection: collection,
		LocalCollection:  collection,
		BatchSize:        pbstore.DefaultSyncBatchSize,
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
		defer cancel()

		report, err := store.SyncCollection(ctx, req)
		if err != nil {
			return SyncFinishedMsg{Err: err}
		}

		return SyncFinishedMsg{Report: report}
	}
}

// applyHealth updates the runtime state from a health check result.
func (m *Model) applyHealth(msg HealthMsg) {
	m.runtime.LastCheck = time.Now()

	if msg.Err != nil {
		m.runtime.Connection = state.ConnectionError
		m.runtime.ConnectionDetail = msg.Err.Error()
		m.runtime.SetError(msg.Err)

		return
	}

	m.runtime.Connection = state.ConnectionOK
	m.runtime.ConnectionDetail = msg.Status
	m.runtime.SetError(nil)
}

// statusInfo builds the data rendered by the status bar.
func (m *Model) statusInfo() components.StatusInfo {
	info := components.StatusInfo{
		Mode:             m.store.Mode().String(),
		Connection:       string(m.runtime.Connection),
		ConnectionDetail: m.runtime.ConnectionDetail,
		Collection:       m.runtime.Collection,
		Sync:             string(m.runtime.Sync),
		SyncProgress:     m.runtime.SyncProgress,
		Page:             m.runtime.Page.Title(),
		Error:            m.runtime.LastError,
		Hints:            m.hints(),
	}

	return info
}

// hints returns the contextual hotkey hints of the active page.
func (m *Model) hints() []string {
	hints := m.pageHints()

	// The panel is a global toggle, so its hint is shown on every
	// page. It is only offered when the terminal is wide enough for it.
	if m.panel.canShow() {
		if m.panel.visible {
			hints = append(hints, components.FormatHints([][2]string{
				{"ctrl+b", "hide panel"},
			})...)
		} else {
			hints = append(hints, components.FormatHints([][2]string{
				{"ctrl+b", "panel"},
			})...)
		}
	}

	return hints
}

// pageHints returns the contextual hotkey hints of the active page.
func (m *Model) pageHints() []string {
	switch m.runtime.Page {
	case state.PageRecords:
		return components.FormatHints([][2]string{
			{"/", "filter"}, {"↑↓", "move"}, {"enter", "open"}, {"←→", "page"},
		})
	case state.PageDetail:
		return components.FormatHints([][2]string{
			{"j", "json"}, {"r", "raw"}, {"e", "edit"},
		})
	case state.PageSearch:
		return components.FormatHints([][2]string{
			{"enter", "run"}, {"esc", "clear"},
		})
	case state.PageAgent:
		return components.FormatHints([][2]string{
			{"enter", "send"}, {"esc", "cancel"},
		})
	case state.PageSync:
		return components.FormatHints([][2]string{
			{"ctrl+r", "sync"}, {"tab", "next"},
		})
	case state.PageHome:
		return components.FormatHints([][2]string{
			{"enter", "open"}, {"↑↓", "move"}, {"+/-", "zoom"}, {"T", "theme"},
		})
	default:
		return components.FormatHints([][2]string{
			{"s", "status"}, {"y", "sync"}, {"+/-", "zoom"}, {"T", "theme"},
			{"c", "compact"}, {"q", "quit"},
		})
	}
}

// resize adapts the components to the terminal size.
func (m *Model) resize() {
	// The pages get the width the panel does not use.
	m.panel.setSize(m.width, m.height)
	m.panel.resize()

	m.statusBar.SetWidth(m.width)

	// The pages are told about the narrower width, so that the record
	// table and the JSON viewer adapt to it.
	m.pageWidth = m.panel.pageWidth()
}

// joinLines renders a list of lines as a single block.
func joinLines(lines []string) string {
	out := ""

	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}

		out += l
	}

	return out
}
