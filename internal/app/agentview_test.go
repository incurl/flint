package app_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/fantasy"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/app"
	"github.com/incurl/flint/internal/pages"
	"github.com/incurl/flint/internal/pbstore"
)

// quietModel is a model that never says anything.
type quietModel struct{}

func (m *quietModel) Provider() string { return "stub" }

func (m *quietModel) Model() string { return "stub" }

func (m *quietModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("not used")
}

func (m *quietModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not used")
}

func (m *quietModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not used")
}

func (m *quietModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {}, nil
}

// TestTheAgentPageFitsTheTerminal verifies that the agent page does not
// render more lines than the terminal has.
//
// The page is not the whole terminal: the status bar takes a line, and
// the panel takes a column. A page that thinks it has the whole
// terminal renders too many lines, and the prompt at the bottom falls
// off the screen.
func TestTheAgentPageFitsTheTerminal(t *testing.T) {
	const height = 20

	cfg := config.Default()
	store := &stubStore{mode: pbstore.ModeEmbedded}
	runner := agent.NewRunner(agent.Options{Registry: agent.NewRegistry()})
	runner.SetModel(&quietModel{})

	m := app.New(store, cfg, nil, &agent.Setup{Runner: runner})

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: height})
	m = updated.(*app.Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: '7', Text: "7"})
	m = updated.(*app.Model)

	page := m.AgentPage()

	for i := range 8 {
		updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
		page = updated.(*pages.AgentPage)

		updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		page = updated.(*pages.AgentPage)

		updated, _ = page.Update(pages.AgentDoneMsg{
			RunID:     fmt.Sprintf("run-%d", 2*i+1),
			Text:      fmt.Sprintf("the answer %d", i),
			SessionID: "s1",
		})
		page = updated.(*pages.AgentPage)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: '7', Text: "7"})
	m = updated.(*app.Model)

	view := m.View().Content
	lines := strings.Split(view, "\n")

	if len(lines) > height {
		t.Errorf("the agent page rendered %d lines for a %d line terminal", len(lines), height)
	}

	// The prompt is on the screen: it is at the bottom, and it is the
	// first thing that falls off when the page is too tall.
	//
	// The placeholder is searched for without its style, so that the
	// assertion does not depend on the escape sequences.
	if !strings.Contains(stripStyle(view), "ask the agent about your records") {
		t.Errorf("the prompt is not on the screen:\n%s", view)
	}

	// The conversation shows the last message: the page follows the
	// end of the conversation.
	if !strings.Contains(stripStyle(view), "the answer 7") {
		t.Errorf("the conversation does not show the last message:\n%s", view)
	}
}

// stripStyle removes the ANSI sequences of a text.
//
// The assertions do not depend on the colours: they depend on the
// content.
func stripStyle(text string) string {
	var b strings.Builder

	for i := 0; i < len(text); i++ {
		if text[i] != '\x1b' {
			b.WriteByte(text[i])

			continue
		}

		// The sequence ends at the "m".
		for i < len(text) && text[i] != 'm' {
			i++
		}
	}

	return b.String()
}
