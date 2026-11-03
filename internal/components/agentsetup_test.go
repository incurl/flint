package components_test

import (
	"errors"
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

// TestAgentSetupWritesTheFile verifies that the setup writes the
// configuration.
//
// The writer is a callback: the components do not know about the
// configuration package, and the application wires them.
func TestAgentSetupWritesTheFile(t *testing.T) {
	setup := components.NewAgentSetup(theme.Default())
	setup.SetSize(80, 24)

	var (
		gotPath     string
		gotSettings components.AgentSetupSettings
	)

	setup.SetWriter("/tmp/flint.toml", func(path string, settings components.AgentSetupSettings) error {
		gotPath = path
		gotSettings = settings

		return nil
	})

	// Walk to the last step.
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	// The write.
	_, _ = setup.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})

	if !setup.Written() {
		t.Fatalf("the configuration was not written: %v", setup.WriteError())
	}

	if gotPath != "/tmp/flint.toml" {
		t.Errorf("the path is %q, want /tmp/flint.toml", gotPath)
	}

	if gotSettings.Provider != "minimax" {
		t.Errorf("the provider is %q, want minimax", gotSettings.Provider)
	}

	if gotSettings.Model != "MiniMax-M3.1-Flash" {
		t.Errorf("the model is %q, want MiniMax-M3.1-Flash", gotSettings.Model)
	}

	if gotSettings.APIKeyEnv != "MINIMAX_API_KEY" {
		t.Errorf("the key variable is %q, want MINIMAX_API_KEY", gotSettings.APIKeyEnv)
	}
}

// TestAgentSetupSettingsHaveNoKey verifies that the settings the setup
// hands over do not carry the key.
//
// The key never leaves the masked input field.
func TestAgentSetupSettingsHaveNoKey(t *testing.T) {
	setup := components.NewAgentSetup(theme.Default())
	setup.SetSize(80, 24)

	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	for _, r := range "sk-secret" {
		_, _ = setup.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	settings := setup.Settings()

	// The settings have no field for the key: the name of the
	// environment variable is what they carry.
	if settings.APIKeyEnv != "MINIMAX_API_KEY" {
		t.Errorf("the key variable is %q, want MINIMAX_API_KEY", settings.APIKeyEnv)
	}
}

// TestAgentSetupReportsTheWriteError verifies that a failed write is
// reported.
//
// A silent failure would leave the user believing the configuration was
// saved.
func TestAgentSetupReportsTheWriteError(t *testing.T) {
	setup := components.NewAgentSetup(theme.Default())
	setup.SetSize(80, 24)

	setup.SetWriter("/tmp/flint.toml", func(string, components.AgentSetupSettings) error {
		return errors.New("the disk is full")
	})

	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})

	if setup.Written() {
		t.Error("the setup says it wrote the configuration, but it failed")
	}

	if setup.WriteError() == nil {
		t.Error("the setup does not report the error")
	}

	if !strings.Contains(setup.View().Content, "the disk is full") {
		t.Errorf("the view does not show the error:\n%s", setup.View().Content)
	}
}
