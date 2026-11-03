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
	}

	// Without a model there is nothing the prompt can do, so the page
	// offers the guided setup instead.
	if runner == nil {
		page.setup = components.NewAgentSetup(t)
	}

	return page
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

	switch msg.String() {
	case "ctrl+c":
		return p.cancel()
	case "esc":
		if p.running {
			return p.cancel()
		}

		p.input.Reset()
		p.input.Blur()

		return nil
	case "enter":
		if p.running {
			return nil
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

// send starts a run with the text of the prompt.
//
// The command captures the values it needs, not the page: the page
// keeps mutating while the run is in flight.
func (p *AgentPage) send() tea.Cmd {
	prompt := strings.TrimSpace(p.input.Value())
	if prompt == "" {
		return nil
	}

	if p.runner == nil {
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

	return nil
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
	b = append(b, "")

	if len(p.entries) == 0 {
		b = append(b, p.theme.Muted.Render("Ask about your records. The agent reads and writes them through the tools."))
		b = append(b, "")
	}

	for _, entry := range p.entries {
		b = append(b, p.renderEntry(entry)...)
	}

	b = append(b, "")

	if p.running {
		b = append(b, p.theme.Muted.Render(p.spinner.View()+" working…"))
		b = append(b, "")
	}

	b = append(b, p.input.View())
	b = append(b, "")
	b = append(b, p.theme.Help.Render("enter send · esc cancel/clear · ctrl+c quit"))

	return tea.NewView(joinLines(b))
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

	// The header, the conversation and the help line take up the rest.
	inputHeight := 5

	if p.height > 0 && p.height-inputHeight > 3 {
		inputHeight = p.height - inputHeight
	}

	p.input.SetWidth(p.width)
	p.input.SetHeight(inputHeight)
}
