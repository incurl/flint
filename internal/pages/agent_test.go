package pages_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/fantasy"

	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/pages"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// answerModel is a model that streams a fixed answer.
type answerModel struct {
	// text is the answer of the model.
	text string
}

func (m *answerModel) Provider() string { return "fake" }

func (m *answerModel) Model() string { return "fake" }

func (m *answerModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("not used")
}

func (m *answerModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not used")
}

func (m *answerModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not used")
}

func (m *answerModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "t1"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "t1", Delta: m.text})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "t1"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

// TestAgentPageShowsTheAnswer verifies that a finished run shows the
// answer of the model.
func TestAgentPageShowsTheAnswer(t *testing.T) {
	runner := agent.NewRunner(agent.Options{Model: &answerModel{text: "there are 3 records"}})

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	page = updated.(*pages.AgentPage)

	updated, cmd := page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	page = updated.(*pages.AgentPage)

	if cmd == nil {
		t.Fatal("the page did not return a command for the prompt")
	}

	done := cmd()

	updated, _ = page.Update(done)
	page = updated.(*pages.AgentPage)

	if !strings.Contains(page.View().Content, "there are 3 records") {
		t.Errorf("View() = %q, want the answer of the model", page.View().Content)
	}
}

// TestAgentPageDiscardsStaleEvents verifies that the events of a run
// the page has left are discarded.
//
// A late delta of a cancelled turn must not appear in the next one.
func TestAgentPageDiscardsStaleEvents(t *testing.T) {
	page := pages.NewAgentPage(nil, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.AgentPage)

	// The event belongs to a run the page knows nothing about.
	updated, _ = page.Update(pages.AgentEventMsg{Event: agent.Event{
		RunID: "run-unknown",
		Kind:  agent.EventTextDelta,
		Text:  "leaked",
	}})
	page = updated.(*pages.AgentPage)

	if strings.Contains(page.View().Content, "leaked") {
		t.Error("the event of an unknown run reached the page")
	}
}

// TestAgentPageShowsThePrompt verifies that the prompt of the user is
// shown in the conversation.
func TestAgentPageShowsThePrompt(t *testing.T) {
	runner := agent.NewRunner(agent.Options{Model: &answerModel{text: "ok"}})

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	page = updated.(*pages.AgentPage)

	if !strings.Contains(page.View().Content, "you: h") {
		t.Errorf("View() = %q, want the prompt of the user", page.View().Content)
	}
}

// TestAgentPageWithoutRunner verifies that a page without a runner
// offers the guided setup instead of a prompt that cannot work.
//
// A prompt that does nothing is worse than no prompt: the user needs
// to know what to configure, and the setup says it.
func TestAgentPageWithoutRunner(t *testing.T) {
	page := pages.NewAgentPage(nil, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.AgentPage)

	view := page.View().Content

	if !strings.Contains(view, "Set up the agent") {
		t.Errorf("View() = %q, want the guided setup", view)
	}

	if !strings.Contains(view, "MiniMax") {
		t.Errorf("View() = %q, want the providers on offer", view)
	}
}

// TestAgentPageAsksForPermission verifies that a pending request is
// shown and answered.
//
// The model waits for the answer, so the page has to show it and turn
// the key into a decision.
func TestAgentPageAsksForPermission(t *testing.T) {
	page := pages.NewAgentPage(nil, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(pages.AgentPermissionMsg{Request: agent.PermissionRequest{
		RunID:       "run-1",
		ToolName:    "delete_record",
		Arguments:   `{"id":"abc"}`,
		Description: "Delete a record.",
	}})
	page = updated.(*pages.AgentPage)

	view := page.View().Content
	if !strings.Contains(view, "Delete a record") {
		t.Errorf("View() = %q, want the request of the agent", view)
	}

	if !strings.Contains(view, "y allow") {
		t.Errorf("View() = %q, want the answer options", view)
	}

	// The answer is a decision the run receives.
	updated, cmd := page.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	page = updated.(*pages.AgentPage)

	if cmd == nil {
		t.Fatal("the page did not return the decision")
	}

	decision, ok := cmd().(pages.AgentPermissionDecisionMsg)
	if !ok {
		t.Fatal("the command did not produce a decision")
	}

	if decision.Decision != agent.PermissionAllow {
		t.Errorf("the decision is %v, want allow", decision.Decision)
	}

	// The request is not shown any more.
	if strings.Contains(page.View().Content, "y allow") {
		t.Error("the request is still pending after the answer")
	}
}

// TestAgentPageDeniesPermission verifies that a refusal is reported as
// a denial.
func TestAgentPageDeniesPermission(t *testing.T) {
	page := pages.NewAgentPage(nil, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(pages.AgentPermissionMsg{Request: agent.PermissionRequest{
		ToolName:    "delete_record",
		Description: "Delete a record.",
	}})
	page = updated.(*pages.AgentPage)

	_, cmd := page.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})

	if cmd == nil {
		t.Fatal("the page did not return the decision")
	}

	decision, ok := cmd().(pages.AgentPermissionDecisionMsg)
	if !ok {
		t.Fatal("the command did not produce a decision")
	}

	if decision.Decision != agent.PermissionDeny {
		t.Errorf("the decision is %v, want deny", decision.Decision)
	}
}

// TestAgentPageTogglesTheMode verifies that the tab key switches
// between the planning and the acting mode.
//
// The mode decides what the model can do, so the page is what owns
// it.
func TestAgentPageTogglesTheMode(t *testing.T) {
	runner := agent.NewRunner(agent.Options{Model: &answerModel{text: "ok"}})

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.AgentPage)

	if page.Mode() != agent.ModeAct {
		t.Fatalf("the mode is %q, want act", page.Mode())
	}

	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	page = updated.(*pages.AgentPage)

	if page.Mode() != agent.ModePlan {
		t.Errorf("the mode is %q, want plan", page.Mode())
	}

	if runner.Mode() != agent.ModePlan {
		t.Errorf("the runner is in %q mode, want plan", runner.Mode())
	}

	if !strings.Contains(page.View().Content, "plan mode") {
		t.Errorf("the page does not show the mode:\n%s", page.View().Content)
	}

	// And back.
	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	page = updated.(*pages.AgentPage)

	if page.Mode() != agent.ModeAct {
		t.Errorf("the mode is %q, want act", page.Mode())
	}
}

// TestAgentPageShowsThePlanForApproval verifies that a finished plan
// waits for the approval.
//
// The plan is a proposal: the user reads it and decides.
func TestAgentPageShowsThePlanForApproval(t *testing.T) {
	plan := "# Plan\n\nCreate the missing records.\n\n" + agent.PlanReadyMarker

	runner := agent.NewRunner(agent.Options{Model: &answerModel{text: plan}})

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	page = updated.(*pages.AgentPage)

	updated, cmd := page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	page = updated.(*pages.AgentPage)

	if cmd == nil {
		t.Fatal("the page did not return a command for the prompt")
	}

	updated, _ = page.Update(cmd())
	page = updated.(*pages.AgentPage)

	view := page.View().Content

	if !strings.Contains(view, "The plan is ready") {
		t.Errorf("the page does not offer the plan:\n%s", view)
	}

	if !strings.Contains(view, "Create the missing records.") {
		t.Errorf("the plan is not shown:\n%s", view)
	}

	// The markers are scaffolding: the user reads the plan, not them.
	if strings.Contains(view, agent.PlanReadyMarker) {
		t.Errorf("the marker is still on the screen:\n%s", view)
	}
}

// TestAgentPageApprovesThePlan verifies the handoff.
//
// The enter key approves the plan, and the run that follows carries
// the plan: the user does not have to copy it.
func TestAgentPageApprovesThePlan(t *testing.T) {
	plan := "# Plan\n\nCreate the missing records.\n\n" + agent.PlanReadyMarker

	runner := agent.NewRunner(agent.Options{Model: &answerModel{text: plan}})

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	page = updated.(*pages.AgentPage)

	updated, cmd := page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(cmd())
	page = updated.(*pages.AgentPage)

	// The approval is the enter key.
	updated, cmd = page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	page = updated.(*pages.AgentPage)

	if cmd == nil {
		t.Fatal("the approval did not return a command")
	}

	// The mode is back to acting: the plan is about to be implemented.
	if page.Mode() != agent.ModeAct {
		t.Errorf("the mode is %q, want act", page.Mode())
	}

	updated, _ = page.Update(cmd())
	page = updated.(*pages.AgentPage)

	view := page.View().Content

	// The prompt that was sent carries the plan.
	if !strings.Contains(view, "Create the missing records.") {
		t.Errorf("the plan was not carried over:\n%s", view)
	}

	if !strings.Contains(view, "Implement this plan") {
		t.Errorf("the run does not implement the plan:\n%s", view)
	}
}

// TestAgentPageDismissesThePlan verifies that the escape key dismisses
// a plan that waits for the approval.
func TestAgentPageDismissesThePlan(t *testing.T) {
	plan := "# Plan\n\nDo the thing.\n\n" + agent.PlanReadyMarker

	runner := agent.NewRunner(agent.Options{Model: &answerModel{text: plan}})

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	page = updated.(*pages.AgentPage)

	updated, cmd := page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(cmd())
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	page = updated.(*pages.AgentPage)

	if strings.Contains(page.View().Content, "The plan is ready") {
		t.Errorf("the plan is still waiting:\n%s", page.View().Content)
	}
}

// TestAgentPageShowsTheTaskList verifies that the task list is shown
// above the prompt.
//
// The list is what the agent is doing right now, and the user watches
// it while the agent works.
func TestAgentPageShowsTheTaskList(t *testing.T) {
	registry := agent.NewRegistry()
	runner := agent.NewRunner(agent.Options{Registry: registry, Model: &answerModel{text: "ok"}})

	agent.RegisterTodosTool(registry, runner)

	tool, _ := registry.Get(agent.ToolTodos)

	ctx := agent.WithSessionID(context.Background(), "s1")

	input := `{"todos":[
		{"content":"Find the records with no label","status":"completed","active_form":""},
		{"content":"Propose a label","status":"in_progress","active_form":"Proposing a label"},
		{"content":"Write the summary","status":"pending","active_form":""}
	]}`

	if _, err := tool.Run(ctx, input); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.AgentPage)

	// A turn is started, so that the page knows the run it belongs to.
	updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	page = updated.(*pages.AgentPage)

	// The page reads the list when a turn ends.
	updated, _ = page.Update(pages.AgentDoneMsg{RunID: "run-1", SessionID: "s1"})
	page = updated.(*pages.AgentPage)

	view := page.View().Content

	if !strings.Contains(view, "To-do 1/3") {
		t.Errorf("the progress line is missing:\n%s", view)
	}

	if !strings.Contains(view, "Proposing a label") {
		t.Errorf("the task in progress is missing:\n%s", view)
	}

	if !strings.Contains(view, "Find the records with no label") {
		t.Errorf("the finished task is missing:\n%s", view)
	}
}
