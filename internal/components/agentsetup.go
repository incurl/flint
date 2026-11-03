package components

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/theme"
)

// The steps of the agent setup.
//
// The order is the order of the questions: which provider, which model,
// and then the key that authenticates the calls.
const (
	// AgentSetupProvider asks for the provider.
	AgentSetupProvider = "provider"
	// AgentSetupModel asks for the model.
	AgentSetupModel = "model"
	// AgentSetupKey asks for the API key.
	AgentSetupKey = "key"
	// AgentSetupDone shows the configuration to copy.
	AgentSetupDone = "done"
)

// AgentSetupProviderInfo describes a provider the setup offers.
type AgentSetupProviderInfo struct {
	// ID is the value of the "provider" setting.
	ID string
	// Name is what the user reads.
	Name string
	// Detail is the one line that says what it is.
	Detail string
	// BaseURL is the endpoint, when the provider has a fixed one.
	BaseURL string
	// KeyEnv is the environment variable that holds the key.
	KeyEnv string
	// Models are the models the provider offers.
	Models []string
	// DefaultModel is the model that is preselected.
	DefaultModel string
}

// The providers the setup offers.
//
// The list is deliberately short: a user who does not know which
// provider to pick does not need twenty of them, and the ones that
// need an account they do not have are noise.
func AgentSetupProviders() []AgentSetupProviderInfo {
	return []AgentSetupProviderInfo{
		{
			ID:           "minimax",
			Name:         "MiniMax",
			Detail:       "MiniMax, the Chinese platform. api.minimax.cn",
			BaseURL:      "https://api.minimax.cn/v1",
			KeyEnv:       "MINIMAX_API_KEY",
			Models:       []string{"MiniMax-M3.1-Flash", "MiniMax-M3"},
			DefaultModel: "MiniMax-M3.1-Flash",
		},
		{
			ID:           "minimax-global",
			Name:         "MiniMax (global)",
			Detail:       "MiniMax, the international platform. api.minimax.io",
			BaseURL:      "https://api.minimax.io/v1",
			KeyEnv:       "MINIMAX_API_KEY",
			Models:       []string{"MiniMax-M3.1-Flash", "MiniMax-M3"},
			DefaultModel: "MiniMax-M3.1-Flash",
		},
		{
			ID:           "openai",
			Name:         "OpenAI",
			Detail:       "The OpenAI API. Needs an OpenAI account.",
			KeyEnv:       "OPENAI_API_KEY",
			Models:       []string{"gpt-4o", "gpt-4o-mini"},
			DefaultModel: "gpt-4o",
		},
		{
			ID:           "openrouter",
			Name:         "OpenRouter",
			Detail:       "Many models behind one key. Needs an OpenRouter account.",
			KeyEnv:       "OPENROUTER_API_KEY",
			Models:       []string{"anthropic/claude-sonnet-4", "openai/gpt-4o"},
			DefaultModel: "anthropic/claude-sonnet-4",
		},
		{
			ID:           "local",
			Name:         "Local server",
			Detail:       "Ollama, LM Studio, or any OpenAI compatible server.",
			KeyEnv:       "",
			Models:       []string{"llama3.1"},
			DefaultModel: "llama3.1",
		},
	}
}

// AgentSetup is the guided setup of the agent.
//
// It appears when the application starts without a model, and it walks
// the user through the three things a setup needs: the provider, the
// model, and the key.
//
// The key is typed into a masked field and it is **never written to a
// file**. The setup produces the configuration to copy, and the
// environment variable to export, and leaves the two to the user: a
// secret belongs in the environment, not in a file that is copied
// around and committed by accident.
type AgentSetup struct {
	// theme is the shared style palette.
	theme *theme.Theme
	// step is the current step.
	step string
	// providers are the providers on offer.
	providers []AgentSetupProviderInfo
	// providerIndex is the selected provider.
	providerIndex int
	// modelIndex is the selected model.
	modelIndex int
	// keyInput takes the API key.
	keyInput textinput.Model
	// keySet reports whether a key was typed.
	keySet bool
	// width is the available rendering width.
	width int
	// height is the available rendering height.
	height int
}

// NewAgentSetup creates the agent setup.
func NewAgentSetup(t *theme.Theme) *AgentSetup {
	if t == nil {
		t = theme.Default()
	}

	key := textinput.New()
	key.Placeholder = "the API key"
	key.EchoMode = textinput.EchoPassword
	key.CharLimit = 0

	return &AgentSetup{
		theme:     t,
		step:      AgentSetupProvider,
		providers: AgentSetupProviders(),
		keyInput:  key,
	}
}

// Step returns the current step.
func (s *AgentSetup) Step() string { return s.step }

// Provider returns the selected provider.
func (s *AgentSetup) Provider() AgentSetupProviderInfo {
	return s.providers[s.providerIndex]
}

// Model returns the selected model.
func (s *AgentSetup) Model() string {
	provider := s.Provider()
	if len(provider.Models) == 0 {
		return provider.DefaultModel
	}

	return provider.Models[s.modelIndex]
}

// APIKey returns the key the user typed.
func (s *AgentSetup) APIKey() string { return s.keyInput.Value() }

// SetSize sets the available rendering area.
func (s *AgentSetup) SetSize(width, height int) {
	s.width = width
	s.height = height

	s.keyInput.SetWidth(width - 2)
}

// Focus gives the keyboard to the step that needs it.
func (s *AgentSetup) Focus() tea.Cmd {
	if s.step == AgentSetupKey {
		return s.keyInput.Focus()
	}

	return nil
}

// Update handles a key press.
//
// It returns the command to run, if any, and whether the key was
// handled: an unhandled key belongs to the page.
func (s *AgentSetup) Update(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch s.step {
	case AgentSetupProvider:
		return s.updateProvider(msg)
	case AgentSetupModel:
		return s.updateModel(msg)
	case AgentSetupKey:
		return s.updateKey(msg)
	default:
		return nil, false
	}
}

// updateProvider handles the keys of the provider step.
func (s *AgentSetup) updateProvider(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "up", "k":
		s.providerIndex = (s.providerIndex - 1 + len(s.providers)) % len(s.providers)

		return nil, true

	case "down", "j":
		s.providerIndex = (s.providerIndex + 1) % len(s.providers)

		return nil, true

	case "enter":
		s.step = AgentSetupModel
		s.modelIndex = s.defaultModelIndex()

		return nil, true

	case "esc":
		s.step = AgentSetupDone

		return nil, true
	}

	return nil, false
}

// updateModel handles the keys of the model step.
func (s *AgentSetup) updateModel(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	models := s.Provider().Models

	switch msg.String() {
	case "up", "k":
		if len(models) > 0 {
			s.modelIndex = (s.modelIndex - 1 + len(models)) % len(models)
		}

		return nil, true

	case "down", "j":
		if len(models) > 0 {
			s.modelIndex = (s.modelIndex + 1) % len(models)
		}

		return nil, true

	case "enter":
		s.step = AgentSetupKey

		return s.keyInput.Focus(), true

	case "esc":
		s.step = AgentSetupProvider

		return nil, true
	}

	return nil, false
}

// updateKey handles the keys of the key step.
func (s *AgentSetup) updateKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "enter":
		s.keySet = s.keyInput.Value() != ""
		s.step = AgentSetupDone
		s.keyInput.Blur()

		return nil, true

	case "esc":
		s.step = AgentSetupModel

		return nil, true
	}

	var cmd tea.Cmd

	s.keyInput, cmd = s.keyInput.Update(msg)

	return cmd, true
}

// defaultModelIndex returns the index of the default model of the
// selected provider.
func (s *AgentSetup) defaultModelIndex() int {
	provider := s.Provider()

	for i, model := range provider.Models {
		if model == provider.DefaultModel {
			return i
		}
	}

	return 0
}

// View renders the setup.
func (s *AgentSetup) View() tea.View {
	var b []string

	b = append(b, s.theme.Title.Render("Set up the agent"))
	b = append(b, "")

	switch s.step {
	case AgentSetupProvider:
		b = append(b, s.theme.Muted.Render("Which provider?"))
		b = append(b, "")

		for i, provider := range s.providers {
			line := "  " + provider.Name
			if i == s.providerIndex {
				line = s.theme.Accent.Render("▸ " + provider.Name)
			}

			b = append(b, line)
			b = append(b, s.theme.Muted.Render("    "+provider.Detail))
		}

		b = append(b, "")
		b = append(b, s.theme.Help.Render("↑↓ choose · enter next · esc skip"))

	case AgentSetupModel:
		provider := s.Provider()

		b = append(b, s.theme.Muted.Render("Which model of "+provider.Name+"?"))
		b = append(b, "")

		for i, model := range provider.Models {
			line := "  " + model
			if i == s.modelIndex {
				line = s.theme.Accent.Render("▸ " + model)
			}

			b = append(b, line)
		}

		b = append(b, "")
		b = append(b, s.theme.Help.Render("↑↓ choose · enter next · esc back"))

	case AgentSetupKey:
		provider := s.Provider()

		b = append(b, s.theme.Muted.Render("The API key of "+provider.Name+"."))
		b = append(b, "")

		if provider.KeyEnv == "" {
			b = append(b, s.theme.Muted.Render("This provider needs no key."))
		} else {
			b = append(b, s.theme.Muted.Render("It is read from the "+provider.KeyEnv+" environment variable."))
			b = append(b, s.theme.Muted.Render("The key is never written to a file."))
			b = append(b, "")
			b = append(b, s.keyInput.View())
		}

		b = append(b, "")
		b = append(b, s.theme.Help.Render("enter next · esc back"))

	case AgentSetupDone:
		b = append(b, s.doneView()...)
	}

	return tea.NewView(strings.Join(b, "\n"))
}

// doneView renders the summary and the configuration to copy.
func (s *AgentSetup) doneView() []string {
	provider := s.Provider()

	var b []string

	if s.keySet {
		b = append(b, s.theme.Muted.Render("The key was typed. The configuration still has to name it:"))
	} else {
		b = append(b, s.theme.Muted.Render("The key is read from the environment. Export it:"))
	}

	b = append(b, "")

	if provider.KeyEnv != "" {
		b = append(b, s.theme.Accent.Render("export "+provider.KeyEnv+"=\"…\""))
		b = append(b, "")
	}

	b = append(b, s.theme.Muted.Render("And put this in flint.toml:"))
	b = append(b, "")

	for _, line := range s.configSnippet() {
		b = append(b, "  "+line)
	}

	b = append(b, "")
	b = append(b, s.theme.Help.Render("Restart the application to use it."))

	return b
}

// configSnippet returns the configuration to copy.
//
// It is the TOML the user needs, and nothing else: the key is not in
// it, because a key does not belong in a file.
func (s *AgentSetup) configSnippet() []string {
	provider := s.Provider()

	lines := []string{
		"[agent]",
		"provider = \"" + provider.ID + "\"",
		"model = \"" + s.Model() + "\"",
	}

	if provider.KeyEnv != "" {
		lines = append(lines, "api_key_env = \""+provider.KeyEnv+"\"")
	}

	if provider.BaseURL != "" {
		lines = append(lines, "base_url = \""+provider.BaseURL+"\"")
	}

	return lines
}

// ConfigSnippet returns the configuration the setup produced.
//
// It is what the page shows and what a caller can copy.
func (s *AgentSetup) ConfigSnippet() string {
	return strings.Join(s.configSnippet(), "\n")
}

// KeyEnv returns the name of the environment variable that holds the
// key of the selected provider.
func (s *AgentSetup) KeyEnv() string {
	return s.Provider().KeyEnv
}

// Summary returns a one line description of the setup.
func (s *AgentSetup) Summary() string {
	provider := s.Provider()

	return fmt.Sprintf("%s · %s", provider.Name, s.Model())
}
