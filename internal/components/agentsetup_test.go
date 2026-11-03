package components_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/theme"
)

// TestAgentSetupWalksTheSteps verifies that the setup goes through the
// provider, the model and the key.
//
// The order is the point: the provider decides which models are
// offered, and the key is asked for last.
func TestAgentSetupWalksTheSteps(t *testing.T) {
	setup := components.NewAgentSetup(theme.Default())
	setup.SetSize(80, 24)

	if setup.Step() != components.AgentSetupProvider {
		t.Errorf("the setup starts at %q, want %q", setup.Step(), components.AgentSetupProvider)
	}

	// The provider.
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if setup.Step() != components.AgentSetupModel {
		t.Errorf("the setup is at %q, want %q", setup.Step(), components.AgentSetupModel)
	}

	// The model.
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if setup.Step() != components.AgentSetupKey {
		t.Errorf("the setup is at %q, want %q", setup.Step(), components.AgentSetupKey)
	}

	// The key.
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if setup.Step() != components.AgentSetupDone {
		t.Errorf("the setup is at %q, want %q", setup.Step(), components.AgentSetupDone)
	}
}

// TestAgentSetupOffersMiniMax verifies that MiniMax is on offer.
//
// It is the provider the template documents first, so it has to be
// reachable.
func TestAgentSetupOffersMiniMax(t *testing.T) {
	providers := components.AgentSetupProviders()

	for _, want := range []string{"minimax", "minimax-global"} {
		found := false

		for _, provider := range providers {
			if provider.ID == want {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("the setup does not offer %q", want)
		}
	}
}

// TestAgentSetupConfigHasNoKey verifies that the configuration the
// setup produces does not carry the key.
//
// A secret belongs in the environment, not in a file that is copied
// around and committed by accident.
func TestAgentSetupConfigHasNoKey(t *testing.T) {
	setup := components.NewAgentSetup(theme.Default())
	setup.SetSize(80, 24)

	// Walk to the key step and type a key.
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	for _, r := range "sk-secret" {
		_, _ = setup.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	snippet := setup.ConfigSnippet()

	if strings.Contains(snippet, "sk-secret") {
		t.Errorf("the configuration carries the key:\n%s", snippet)
	}

	if !strings.Contains(snippet, "api_key_env") {
		t.Errorf("the configuration does not name the key variable:\n%s", snippet)
	}
}

// TestAgentSetupMasksTheKey verifies that the key is not shown.
//
// It is typed on a screen that may be shared or recorded.
func TestAgentSetupMasksTheKey(t *testing.T) {
	setup := components.NewAgentSetup(theme.Default())
	setup.SetSize(80, 24)

	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	for _, r := range "sk-secret" {
		_, _ = setup.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	view := setup.View().Content

	if strings.Contains(view, "sk-secret") {
		t.Errorf("the key is shown on the screen:\n%s", view)
	}
}

// TestAgentSetupConfigIsCopyable verifies that the configuration the
// setup produces is the TOML the user needs.
func TestAgentSetupConfigIsCopyable(t *testing.T) {
	setup := components.NewAgentSetup(theme.Default())
	setup.SetSize(80, 24)

	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	snippet := setup.ConfigSnippet()

	for _, want := range []string{"[agent]", "provider = ", "model = "} {
		if !strings.Contains(snippet, want) {
			t.Errorf("the configuration does not contain %q:\n%s", want, snippet)
		}
	}
}
