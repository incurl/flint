package pages

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/session"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// AgentEventMsg carries an event of a running agent.
//
// The agent publishes its events from the goroutine that runs the
// model, and the page receives them as a message. The run id is part
// of the event: a page that started a run discards the events of the
// runs it has left, which is the same guard the other pages apply to
// their responses.
type AgentEventMsg struct {
	// Event is the event of the run.
	Event agent.Event
}

// AgentDoneMsg reports the end of a run.
//
// It is separate from the events because the outcome of a run is not an
// event: it is the result the page has to react to.
type AgentDoneMsg struct {
	// RunID identifies the run that ended.
	RunID string
	// Text is the final answer of the assistant.
	Text string
	// Err holds the error, when the run failed.
	Err error
	// SessionID names the conversation the run belongs to.
	//
	// The page keeps it, so that the next turn continues the same
	// conversation.
	SessionID string
}

// AgentPermissionMsg asks the user to approve a tool call.
//
// The request arrives while the model is waiting, and the answer goes
// back through the same channel: the page holds the pending request
// and the user answers it with a key.
type AgentPermissionMsg struct {
	// ID identifies the request, so that the answer can be routed back
	// to the run that asked for it.
	ID string
	// Request is the pending request.
	Request agent.PermissionRequest
}

// AgentPermissionDecisionMsg answers a permission request.
//
// It is what the page sends back to the run when the user decided.
type AgentPermissionDecisionMsg struct {
	// ID identifies the request the answer belongs to.
	ID string
	// Decision is the answer of the user.
	Decision agent.PermissionDecision
}

// AgentEntry is one line of the conversation.
//
// The page keeps the conversation as a list of entries rather than as
// a single blob of text, so that a tool call can be shown next to the
// text that explains it.
type AgentEntry struct {
	// Role is who produced the entry: "you" or the assistant.
	Role string
	// Text is the content of the entry.
	Text string
	// ToolName holds the name of the tool, for a tool entry.
	ToolName string
	// IsTool reports whether the entry is a tool call.
	IsTool bool
	// Failed reports whether the entry is an error.
	Failed bool
}

// The roles of the conversation entries.
const (
	// agentRoleUser marks what the user typed.
	agentRoleUser = "you"
	// agentRoleAssistant marks what the model answered.
	agentRoleAssistant = "assistant"
	// agentRoleTool marks a tool call.
	agentRoleTool = "tool"
)

// AgentPage is the agent chat screen.
//
// The page owns the conversation and the prompt, and it subscribes to
// the events of the run. The agent itself knows nothing about the UI:
// it publishes events and the page draws them.
type AgentPage struct {
	// runner runs the turns.
	runner *agent.Runner
	// resolver carries the answers of the user back to the runs.
	resolver PermissionResolver
	// sessions reads and writes the conversations.
	sessions *session.Service
	// runtime holds the shared state.
	runtime *state.Runtime
	// theme is the shared style palette.
	theme *theme.Theme
	// input is the prompt editor.
	input textarea.Model
	// spinner shows that a run is in flight.
	spinner spinner.Model
	// entries holds the conversation.
	entries []AgentEntry
	// sessionID names the conversation of the page.
	sessionID string
	// runID identifies the current run.
	runID string
	// running reports whether a run is in flight.
	running bool
	// pending holds the permission request the user has to answer.
	pending *AgentPermissionMsg
	// setup is the guided setup of the agent.
	//
	// It is only used when no model is configured: the page then walks
	// the user through the provider, the model and the key instead of
	// showing a prompt that cannot work.
	setup *components.AgentSetup
	// mode is the mode of the next run.
	//
	// It is [agent.ModeAct] normally, and [agent.ModePlan] when the
	// user asked for a plan. The mode decides which tools the model can
	// call, so it is the page that changes it.
	mode agent.Mode
	// planReady reports whether a plan is waiting for the approval.
	//
	// It is set when a planning run finishes with the end marker, and
	// cleared when the plan is approved or dismissed.
	planReady bool
	// planText holds the plan of the last planning run.
	//
	// It is the text that the approval carries over into the acting
	// run, so that the model knows what it is implementing.
	planText string
	// todos holds the task list of the conversation.
	//
	// It is read from the runner after every turn: the list belongs to
	// the conversation, and the model owns it.
	todos []agent.Todo
	// scroller shows the conversation.
	//
	// The conversation grows without a bound, and the page has a
	// height: the scroller shows the part of it that fits, and it
	// follows the end while the reader is there.
	scroller *components.Scroller
	// width is the available rendering width.
	width int
	// height is the available rendering height.
	height int
}

// PermissionResolver carries the answers of the user back to the runs.
//
// The agent layer defines the interface, so the page does not have to
// import the implementation.
type PermissionResolver = agent.Resolver

// NewAgentPage creates the agent page.
//
// A nil runner makes the page inert: it accepts the prompt and reports
// that no model is configured, which is what a template without a
// provider should do.
//
// The resolver carries the answers of the user back to the runs. A nil
// resolver means that every write is refused, which is the safe
// default: an agent that writes to the data of the user must not do it
// without an answer.
//
// A nil service means that the conversation is not persisted: the page
// then keeps it in memory only.
func NewAgentPage(
	runner *agent.Runner,
	resolver PermissionResolver,
	sessions *session.Service,
	runtime *state.Runtime,
	t *theme.Theme,
) *AgentPage {
	if t == nil {
		t = theme.Default()
	}

	if runtime == nil {
		runtime = state.NewRuntime()
	}

	input := textarea.New()
	input.Placeholder = "ask the agent about your records…"
	input.ShowLineNumbers = false
	input.CharLimit = 0

	page := &AgentPage{
		runner:   runner,
		resolver: resolver,
		sessions: sessions,
		runtime:  runtime,
		theme:    t,
		input:    input,
		spinner:  spinner.New(),
		scroller: components.NewScroller(10),
	}

	// The setup is offered when there is no model: the runner always
	// exists, but it has no model until one is configured.
	if runner == nil || runner.Model() == nil {
		page.setup = components.NewAgentSetup(t)
	}

	return page
}

// SetSize sets the size of the page.
//
// The application calls it with the space the page really has: the
// height above the status bar, and the width the panel leaves.
//
// The window size message is not enough: it carries the size of the
// terminal, and the page is not the whole terminal.
func (p *AgentPage) SetSize(width, height int) {
	p.width = width
	p.height = height

	p.resize()
}

// Init implements [tea.Model].
func (p *AgentPage) Init() tea.Cmd {
	if p.setup != nil {
		return p.setup.Focus()
	}

	p.input.Focus()

	return nil
}

// Update implements [tea.Model].
func (p *AgentPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		p.resize()

		// The prompt takes the keyboard as soon as the page is shown.
		// The focus cannot live in Init: the root model only calls it
		// once, for the home page, so a page that is reached later would
		// never get the focus.
		if p.setup != nil {
			return p, p.setup.Focus()
		}

		p.input.Focus()

		return p, nil

	case AgentEventMsg:
		p.applyEvent(msg.Event)

		return p, nil

	case AgentDoneMsg:
		return p, p.applyDone(msg)

	case AgentPermissionMsg:
		// The request arrives while the model waits: the page holds it
		// until the user answers.
		pending := msg
		p.pending = &pending

		return p, nil

	case tea.KeyPressMsg:
		return p, p.handleKey(msg)
	}

	return p, nil
}

// handleKey reacts to a key press.
//
// The permission request takes the keyboard while it is pending: the
// model is waiting for the answer, so the prompt must not swallow the
// keys.
func (p *AgentPage) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if p.pending != nil {
		return p.handlePermissionKey(msg)
	}

	// The setup owns the keyboard while it is on screen: it is the
	// only thing that can work without a model.
	if p.setup != nil {
		cmd, handled := p.setup.Update(msg)
		if handled {
			return cmd
		}
	}

	// The content is prepared before the keys are handled: a scroll
	// has to act on the conversation as it is now, not on the one of
	// the last frame.
	p.prepareConversation()

	// The scroll keys belong to the conversation. They are handled
	// before the prompt, so that a reader who scrolls up does not type
	// into the prompt instead.
	if cmd, handled := p.scroller.Update(msg); handled {
		return cmd
	}

	switch msg.String() {
	case "ctrl+c":
		return p.cancel()
	case "esc":
		if p.running {
			return p.cancel()
		}

		// The escape key dismisses a plan that waits for the approval.
		if p.planReady {
			p.planReady = false
			p.planText = ""

			return toastCmd(components.ToastInfo, "the plan was dismissed")
		}

		p.input.Reset()
		p.input.Blur()

		return nil
	case "tab":
		return p.toggleMode()
	case "enter":
		if p.running {
			return nil
		}

		// A plan that waits for the approval is implemented by the
		// next run: the enter key is the approval.
		if p.planReady {
			return p.approvePlan()
		}

		return p.send()
	}

	updated, cmd := p.input.Update(msg)
	p.input = updated

	return cmd
}

// handlePermissionKey answers a pending permission request.
//
// The answer goes back to the run through the resolver: the run is
// blocked waiting for it, and a decision that never arrives would hang
// the turn until the timeout.
func (p *AgentPage) handlePermissionKey(msg tea.KeyPressMsg) tea.Cmd {
	pending := p.pending
	p.pending = nil

	decision := agent.PermissionDeny

	switch msg.String() {
	case "y", "enter":
		decision = agent.PermissionAllow
	case "n", "esc":
		decision = agent.PermissionDeny
	}

	resolver := p.resolver
	id := pending.Request.ID

	return func() tea.Msg {
		if resolver != nil {
			resolver.Resolve(id, decision)
		}

		return AgentPermissionDecisionMsg{ID: id, Decision: decision}
	}
}

// toggleMode switches between the planning and the acting mode.
//
// The mode is the page's, because it decides what the user is doing:
// a plan is a proposal, and an action is a change. The runner is told
// about it, and the runner gives the model the tools of the mode.
//
// A run that is already in flight keeps the mode it started with.
func (p *AgentPage) toggleMode() tea.Cmd {
	if p.running {
		return toastCmd(components.ToastInfo, "wait for the run to finish")
	}

	if p.mode.IsPlan() {
		p.mode = agent.ModeAct
	} else {
		p.mode = agent.ModePlan
	}

	if p.runner != nil {
		p.runner.SetMode(p.mode)
	}

	if p.mode.IsPlan() {
		return toastCmd(components.ToastInfo, "plan mode: the agent cannot change the records")
	}

	return toastCmd(components.ToastInfo, "act mode: the agent can change the records")
}

// Mode returns the mode of the next run.
func (p *AgentPage) Mode() agent.Mode { return p.mode }

// approvePlan carries the plan over into an acting run.
//
// It is the handoff: the mode returns to acting, and the plan becomes
// the first message of the run that implements it. The user does not
// have to copy the plan, and the model does not have to be told twice.
func (p *AgentPage) approvePlan() tea.Cmd {
	p.mode = agent.ModeAct
	p.planReady = false

	if p.runner != nil {
		p.runner.SetMode(p.mode)
	}

	plan := strings.TrimSpace(p.planText)
	p.planText = ""

	// The prompt carries the plan, so that the model knows what it is
	// implementing even when the conversation is short.
	prompt := "Implement the plan."

	if plan != "" {
		prompt = "Implement this plan:\n\n" + plan
	}

	p.input.SetValue(prompt)

	return p.send()
}

// send starts a run with the text of the prompt.
//
// The command captures the values it needs, not the page: the page
// keeps mutating while the run is in flight.
func (p *AgentPage) send() tea.Cmd {
	prompt := strings.TrimSpace(p.input.Value())
	if prompt == "" {
		return nil
	}

	if p.runner == nil || p.runner.Model() == nil {
		return toastCmd(components.ToastError, "no agent is configured")
	}

	p.input.Reset()
	p.entries = append(p.entries, AgentEntry{Role: agentRoleUser, Text: prompt})
	p.running = true
	p.runID = fmt.Sprintf("run-%d", len(p.entries))

	runner := p.runner
	runID := p.runID
	sessions := p.sessions
	sessionID := p.sessionID

	return func() tea.Msg {
		// The conversation is created on the first turn: a page that is
		// never used leaves nothing behind.
		if sessions != nil && sessionID == "" {
			created, err := sessions.CreateSession(context.Background(), "")
			if err != nil {
				return AgentDoneMsg{RunID: runID, Err: err}
			}

			sessionID = created.ID
		}

		text, err := runner.Run(context.Background(), agent.RunRequest{
			RunID:     runID,
			Prompt:    prompt,
			SessionID: sessionID,
		})

		return AgentDoneMsg{RunID: runID, Text: text, Err: err, SessionID: sessionID}
	}
}

// cancel stops the run in flight.
func (p *AgentPage) cancel() tea.Cmd {
	if p.runner == nil || !p.running {
		return nil
	}

	runner, runID := p.runner, p.runID

	return func() tea.Msg {
		runner.Cancel(runID)

		return nil
	}
}

// applyEvent applies an event of a run to the conversation.
//
// The events of a run the page has left are discarded: a late delta of
// a cancelled turn must not appear in the next one.
func (p *AgentPage) applyEvent(event agent.Event) {
	if event.RunID != p.runID {
		return
	}

	switch event.Kind {
	case agent.EventToolCall:
		p.entries = append(p.entries, AgentEntry{
			Role:     agentRoleTool,
			Text:     event.ToolInput,
			ToolName: event.ToolName,
			IsTool:   true,
		})

	case agent.EventTextDelta:
		p.appendText(event.Text)
	}
}

// appendText adds streamed text to the last assistant entry.
func (p *AgentPage) appendText(text string) {
	if text == "" {
		return
	}

	if len(p.entries) > 0 {
		last := &p.entries[len(p.entries)-1]
		if last.Role == agentRoleAssistant {
			last.Text += text

			return
		}
	}

	p.entries = append(p.entries, AgentEntry{Role: agentRoleAssistant, Text: text})
}

// applyDone reacts to the end of a run.
func (p *AgentPage) applyDone(msg AgentDoneMsg) tea.Cmd {
	if msg.RunID != p.runID {
		return nil
	}

	p.running = false
	p.pending = nil

	// The conversation is created by the run: the page keeps its id so
	// that the next turn continues it.
	if msg.SessionID != "" {
		p.sessionID = msg.SessionID
	}

	if msg.Err != nil {
		p.entries = append(p.entries, AgentEntry{
			Role:   agentRoleAssistant,
			Text:   msg.Err.Error(),
			Failed: true,
		})

		return toastCmd(components.ToastError, msg.Err.Error())
	}

	if msg.Text != "" {
		p.appendText(msg.Text)
	}

	// The task list belongs to the conversation, and the model owns it:
	// it is read again after every turn.
	p.refreshTodos()

	// A planning run that ends with the marker leaves a plan that waits
	// for the approval. The markers themselves are not shown: they are
	// instructions to the model, not content for the user.
	if p.mode.IsPlan() && agent.HasPlanReady(msg.Text) {
		p.planReady = true
		p.planText = agent.StripPlanMarkers(msg.Text)

		// The markers are removed from the conversation too, so that
		// the plan that is approved is the plan the user read.
		p.stripPlanMarkers()

		return toastCmd(components.ToastInfo, "the plan is ready: press enter to implement it")
	}

	return nil
}

// refreshTodos reads the task list of the conversation.
//
// The list is in the runner, because the tool that writes it belongs to
// the runner.
func (p *AgentPage) refreshTodos() {
	if p.runner == nil {
		p.todos = nil

		return
	}

	p.todos = p.runner.Todos(p.sessionID)
}

// stripPlanMarkers removes the plan markers from the conversation.
//
// The markers are an instruction to the model. The user reads the plan,
// not the scaffolding around it.
func (p *AgentPage) stripPlanMarkers() {
	for i := range p.entries {
		if p.entries[i].Role == agentRoleAssistant {
			p.entries[i].Text = agent.StripPlanMarkers(p.entries[i].Text)
		}
	}
}

// PanelEntries returns the conversation in the form the side panel
// renders it.
//
// The panel is a different view of the same conversation, so the
// conversion happens here rather than in the panel: the panel knows
// nothing about the roles and the tool calls of the page.
func (p *AgentPage) PanelEntries() []components.AgentPanelEntry {
	out := make([]components.AgentPanelEntry, 0, len(p.entries))

	for _, entry := range p.entries {
		text := entry.Text
		if entry.IsTool {
			text = entry.ToolName
			if entry.Text != "" {
				text += " " + entry.Text
			}
		}

		out = append(out, components.AgentPanelEntry{
			Text:   text,
			IsTool: entry.IsTool,
			Failed: entry.Failed,
		})
	}

	return out
}

// SetConfigWriter sets the function that writes the configuration.
//
// The setup offers to write an example configuration file, and the
// application owns the configuration: it is the one that knows where
// the file goes and what it holds.
//
// The writer receives the path and returns an error when it could not
// write.
func (p *AgentPage) SetConfigWriter(path string, writer func(path string, settings components.AgentSetupSettings) error) {
	if p.setup == nil {
		return
	}

	p.setup.SetWriter(path, writer)
}

// SetModelApplier sets the function that installs the model.
//
// The guided setup can build a model from the key that was typed and
// install it in the runner, so that the agent works right away without
// a restart.
func (p *AgentPage) SetModelApplier(applier func(settings components.AgentSetupSettings) error) {
	if p.setup == nil {
		return
	}

	p.setup.SetApplier(applier)
}

// HasModel reports whether the page has a model to run.
//
// It is false while the guided setup is on screen: the agent cannot
// answer a question before a model is configured.
func (p *AgentPage) HasModel() bool {
	return p.runner != nil && p.runner.Model() != nil
}

// DropSetup removes the guided setup from the page.
//
// It is called when a model is installed: the setup has done its work,
// and the page shows the chat from then on.
func (p *AgentPage) DropSetup() {
	p.setup = nil

	p.input.Focus()
}

// View implements [tea.Model].
func (p *AgentPage) View() tea.View {
	// A pending permission request is a blocking state: the model is
	// waiting for the answer. It is shown even when the setup is on
	// screen, because a request that is hidden is a request that is
	// never answered.
	if p.pending != nil {
		return p.viewWithPermission()
	}

	// Without a model, the setup is the page.
	if p.setup != nil {
		return p.setup.View()
	}

	var b []string

	b = append(b, p.theme.Title.Render("Agent"))

	if p.mode.IsPlan() {
		b = append(b, p.theme.Warning.Render("plan mode — the agent reads the records and proposes a plan, it does not change them."))
	}

	// The conversation is scrolled: it grows without a bound, and the
	// page has a height. The scroller shows the part that fits, and it
	// follows the end while the reader is there.
	p.prepareConversation()

	b = append(b, p.scroller.View())

	if p.running {
		b = append(b, p.theme.Muted.Render(p.spinner.View()+" working…"))
	}

	if p.planReady {
		b = append(b, p.theme.Success.Render("The plan is ready."))
		b = append(b, p.theme.Muted.Render("enter implement it · esc dismiss it"))
	} else {
		b = append(b, p.todoView()...)
		b = append(b, p.input.View())
	}

	b = append(b, p.theme.Help.Render(p.helpLine()))

	return tea.NewView(joinLines(b))
}

// prepareConversation puts the conversation in the scroller.
//
// It is called before the keys are handled and before the page is
// drawn: a scroll has to act on the conversation as it is now.
func (p *AgentPage) prepareConversation() {
	p.scroller.SetSize(p.width, p.conversationHeight())
	p.scroller.SetContent(joinLines(p.conversationLines()))
}

// conversationLines renders the conversation of the page.
//
// It is the content of the scroller: the whole conversation, not the
// part that fits.
func (p *AgentPage) conversationLines() []string {
	var b []string

	if len(p.entries) == 0 {
		b = append(b, p.theme.Muted.Render("Ask about your records. The agent reads and writes them through the tools."))
		b = append(b, "")

		return b
	}

	for _, entry := range p.entries {
		b = append(b, p.renderEntry(entry)...)
	}

	return b
}

// conversationHeight returns the height the conversation gets.
//
// The header, the input, the task list and the help line take the rest
// of the page.
func (p *AgentPage) conversationHeight() int {
	// A page that has not been sized yet still shows its conversation:
	// the height falls back to the content.
	if p.height <= 0 {
		return len(p.entries) * 2
	}

	height := p.height - p.chromeHeight()
	if height < 1 {
		height = 1
	}

	return height
}

// chromeHeight returns the height the page uses for everything but the
// conversation.
//
// The counts are the *rendered* heights: the prompt is as tall as it
// renders, and the task list is as tall as its entries. A page that
// guesses them renders too many lines, and the lines at the end are
// the ones that fall off the screen.
func (p *AgentPage) chromeHeight() int {
	// The title, the plan banner, the prompt, the help line and the
	// blank line between the conversation and the prompt.
	height := 5

	if p.mode.IsPlan() {
		height++
	}

	if p.running {
		height++
	}

	if p.planReady {
		// The "the plan is ready" line and its hint.
		height += 2
	} else {
		// The task list, and the prompt.
		height += len(p.todoView())
		height += p.promptHeight()
	}

	return height
}

// promptHeight returns the height the prompt renders.
//
// It is the height of the input, and the height of the input is what
// the last resize set: a prompt that is three lines tall is three
// lines tall, and the page has to leave the room for it.
func (p *AgentPage) promptHeight() int {
	lines := strings.Count(p.input.View(), "\n") + 1
	if lines < 1 {
		return 1
	}

	return lines
}

// helpLine returns the help of the page.
//
// The scroll keys are advertised only when the conversation overflows:
// a key that does nothing is noise.
func (p *AgentPage) helpLine() string {
	line := "tab plan/act · enter send · esc cancel/clear · ctrl+c quit"

	if p.scroller.AtBottom() {
		return line
	}

	return "pgup/pgdn scroll · " + line
}

// todoView renders the task list of the conversation.
//
// It is above the prompt: it is what the agent is doing right now, and
// the user watches it while the agent works.
//
// The list is only shown when it has something to say. An empty list
// is noise, and a model that writes one for every question would fill
// the screen with it.
func (p *AgentPage) todoView() []string {
	if len(p.todos) == 0 {
		return nil
	}

	completed, total := agent.TodoProgress(p.todos)

	var b []string

	b = append(b, p.theme.Muted.Render(fmt.Sprintf("To-do %d/%d", completed, total)))

	for _, todo := range agent.SortTodos(p.todos) {
		text := todo.Content
		if todo.Status == agent.TodoInProgress && todo.ActiveForm != "" {
			text = todo.ActiveForm
		}

		switch todo.Status {
		case agent.TodoCompleted:
			b = append(b, p.theme.Muted.Render("  ✓ "+text))
		case agent.TodoInProgress:
			b = append(b, p.theme.Accent.Render("  → "+text))
		default:
			b = append(b, p.theme.Muted.Render("  · "+text))
		}
	}

	b = append(b, "")

	return b
}

// viewWithPermission renders the page while a request is pending.
//
// The request replaces the prompt: the model is waiting for the
// answer, and the prompt must not take the keyboard.
func (p *AgentPage) viewWithPermission() tea.View {
	var b []string

	b = append(b, p.theme.Title.Render("Agent"))
	b = append(b, "")

	for _, entry := range p.entries {
		b = append(b, p.renderEntry(entry)...)
	}

	b = append(b, "")
	b = append(b, p.renderPending())
	b = append(b, "")

	return tea.NewView(joinLines(b))
}

// renderEntry renders one line of the conversation.
func (p *AgentPage) renderEntry(entry AgentEntry) []string {
	switch {
	case entry.IsTool:
		text := entry.ToolName
		if entry.Text != "" {
			text += " " + entry.Text
		}

		return []string{p.theme.Accent.Render("→ " + text)}

	case entry.Failed:
		return []string{p.theme.Error.Render(entry.Text)}

	case entry.Role == agentRoleUser:
		return []string{p.theme.Subtitle.Render("you: " + entry.Text)}

	default:
		return []string{entry.Text}
	}
}

// renderPending renders the permission request the user has to answer.
func (p *AgentPage) renderPending() string {
	lines := []string{
		p.theme.Warning.Render("The agent wants to " + p.pending.Request.Description),
	}

	if p.pending.Request.Arguments != "" {
		lines = append(lines, p.theme.Muted.Render(p.pending.Request.Arguments))
	}

	lines = append(lines, p.theme.Help.Render("y allow · n deny"))

	return strings.Join(lines, "\n")
}

// resize adapts the components to the available space.
func (p *AgentPage) resize() {
	if p.setup != nil {
		p.setup.SetSize(p.width, p.height)

		return
	}

	// The input keeps a fixed height: the conversation takes the rest,
	// and a prompt that grows with the page leaves nothing for it.
	p.input.SetWidth(p.width)
	p.input.SetHeight(3)

	// The scroller is sized in the view, because the height it gets
	// depends on what else is on the page.
	p.scroller.SetSize(p.width, p.conversationHeight())
}
