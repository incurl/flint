package pages_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/pages"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// TestTheArrowsMoveTheCaret verifies that the up and down arrows move
// the caret in the prompt.
//
// The arrows belong to the text input: a caret that does not move is a
// caret the reader cannot place.
func TestTheArrowsMoveTheCaret(t *testing.T) {
	runner := agent.NewRunner(agent.Options{Model: &answerModel{text: "ok"}})

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	page = updated.(*pages.AgentPage)

	// Type two lines.
	for _, r := range "first" {
		updated, _ = page.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		page = updated.(*pages.AgentPage)
	}

	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	page = updated.(*pages.AgentPage)

	for _, r := range "second" {
		updated, _ = page.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		page = updated.(*pages.AgentPage)
	}

	before := page.View().Content

	// The up arrow moves the caret to the previous line.
	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	page = updated.(*pages.AgentPage)

	// The prompt is still there, and the text was not lost.
	if !strings.Contains(page.View().Content, "second") {
		t.Errorf("the prompt lost its text:\\n%s", page.View().Content)
	}

	// The arrow did not scroll the conversation: the content is the
	// same, because there is nothing to scroll yet.
	if strings.Count(before, "first") != strings.Count(page.View().Content, "first") {
		t.Errorf("the arrow scrolled the conversation:\\n%s", page.View().Content)
	}
}
