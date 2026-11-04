package app_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/app"
	"github.com/incurl/flint/internal/pbstore"
)

// TestTheApplicationFitsTheTerminal verifies that the application does
// not render more lines than the terminal has.
//
// The application adds the status bar to the page, so a page that
// fits on its own can still overflow the terminal.
func TestTheApplicationFitsTheTerminal(t *testing.T) {
	const height = 20

	cfg := config.Default()

	store := &stubStore{mode: pbstore.ModeEmbedded}
	runner := agent.NewRunner(agent.Options{Registry: agent.NewRegistry()})

	m := app.New(store, cfg, nil, &agent.Setup{Runner: runner})

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: height})
	m = updated.(*app.Model)

	for _, key := range []string{"1", "2", "3", "4", "5", "6", "7"} {
		updated, _ = m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m = updated.(*app.Model)

		lines := strings.Split(m.View().Content, "\n")

		if len(lines) > height {
			t.Errorf("the page %q rendered %d lines for a %d line terminal", key, len(lines), height)
		}
	}
}
