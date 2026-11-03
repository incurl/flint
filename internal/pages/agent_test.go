package pages_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/fantasy"

	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/components"
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
// reports the missing agent instead of failing.
func TestAgentPageWithoutRunner(t *testing.T) {
	page := pages.NewAgentPage(nil, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	page = updated.(*pages.AgentPage)

	_, cmd := page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("the page did not report the missing agent")
	}

	msg, ok := cmd().(components.ToastMsg)
	if !ok {
		t.Fatal("the command did not produce a toast")
	}

	if !strings.Contains(msg.Text, "agent") {
		t.Errorf("the toast is %q, want it to mention the agent", msg.Text)
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
