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

// TestTheConversationWraps verifies that a long answer is wrapped and
// not cut.
//
// A viewport cuts the lines that are too long: the rest of the answer
// would be lost, and the reader would never know it was there.
func TestTheConversationWraps(t *testing.T) {
	const width = 40

	long := "This is a very long answer that does not fit on one line of the terminal and has to be wrapped."

	runner := agent.NewRunner(agent.Options{Model: &answerModel{text: long}})

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: width, Height: 20})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(pages.AgentDoneMsg{RunID: "run-1", Text: long, SessionID: "s1"})
	page = updated.(*pages.AgentPage)

	view := page.View().Content

	t.Logf("VIEW:\n%s", view)

	// Every word of the answer is on the screen.
	for _, word := range strings.Fields(long) {
		if !strings.Contains(view, word) {
			t.Errorf("the word %q is not on the screen:\n%s", word, view)
		}
	}
}
