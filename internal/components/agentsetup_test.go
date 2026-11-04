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

	for _, want := range []string{"minimax", "minimax-cn"} {
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

// TestAgentSetupNamesThePlatforms verifies that the two MiniMax
// platforms are told apart.
//
// The two platforms have separate accounts, separate keys and separate
// balances. A provider called "MiniMax" that points at the Chinese
// endpoint is a trap: the key of the international account is refused
// there, and the error says the balance is insufficient.
func TestAgentSetupNamesThePlatforms(t *testing.T) {
	providers := components.AgentSetupProviders()

	want := map[string]string{
		"minimax":    "https://api.minimax.io/v1",
		"minimax-cn": "https://api.minimax.cn/v1",
	}

	for _, provider := range providers {
		baseURL, ok := want[provider.ID]
		if !ok {
			continue
		}

		if provider.BaseURL != baseURL {
			t.Errorf("the provider %q uses %q, want %q", provider.ID, provider.BaseURL, baseURL)
		}
	}
}

// TestAgentSetupTheDefaultIsInternational verifies that the first
// provider of the setup is the international platform.
//
// It is the one the global website serves, and the one a key from
// platform.minimax.io belongs to.
func TestAgentSetupTheDefaultIsInternational(t *testing.T) {
	providers := components.AgentSetupProviders()

	if len(providers) == 0 {
		t.Fatal("the setup offers no provider")
	}

	first := providers[0]

	if first.ID != "minimax" {
		t.Errorf("the default provider is %q, want minimax", first.ID)
	}

	if first.BaseURL != "https://api.minimax.io/v1" {
		t.Errorf("the default provider uses %q, want the international endpoint", first.BaseURL)
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

// TestAgentSetupAppliesTheModel verifies that the setup installs the
// model.
//
// It is what makes the agent work right away, without a restart.
func TestAgentSetupAppliesTheModel(t *testing.T) {
	setup := components.NewAgentSetup(theme.Default())
	setup.SetSize(80, 24)

	var gotSettings components.AgentSetupSettings

	setup.SetApplier(func(settings components.AgentSetupSettings) error {
		gotSettings = settings

		return nil
	})

	// Walk to the last step.
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Apply.
	_, _ = setup.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})

	if !setup.Applied() {
		t.Fatalf("the model was not installed: %v", setup.ApplyError())
	}

	if gotSettings.Provider != "minimax" {
		t.Errorf("the provider is %q, want minimax", gotSettings.Provider)
	}

	if gotSettings.APIKeyEnv != "MINIMAX_API_KEY" {
		t.Errorf("the key variable is %q, want MINIMAX_API_KEY", gotSettings.APIKeyEnv)
	}
}

// TestAgentSetupAppliesTheTypedKey verifies that the key the user typed
// reaches the applier.
//
// The key stays in memory: it is used to build the model, and it is
// never written to a file.
func TestAgentSetupAppliesTheTypedKey(t *testing.T) {
	setup := components.NewAgentSetup(theme.Default())
	setup.SetSize(80, 24)

	var gotSettings components.AgentSetupSettings

	setup.SetApplier(func(settings components.AgentSetupSettings) error {
		gotSettings = settings

		return nil
	})

	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	for _, r := range "sk-secret" {
		_, _ = setup.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})

	if gotSettings.APIKey != "sk-secret" {
		t.Errorf("the key is %q, want sk-secret", gotSettings.APIKey)
	}
}

// TestAgentSetupReportsTheApplyError verifies that a failed apply is
// reported.
func TestAgentSetupReportsTheApplyError(t *testing.T) {
	setup := components.NewAgentSetup(theme.Default())
	setup.SetSize(80, 24)

	setup.SetApplier(func(components.AgentSetupSettings) error {
		return errors.New("the provider refused the key")
	})

	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = setup.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})

	if setup.Applied() {
		t.Error("the setup says it installed the model, but it failed")
	}

	if !strings.Contains(setup.View().Content, "the provider refused the key") {
		t.Errorf("the view does not show the error:\n%s", setup.View().Content)
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
