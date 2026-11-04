package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/agent"
)

// TestMiniMaxProvidersBuild verifies that the MiniMax providers build
// a model.
//
// MiniMax is the provider the template documents first, so a typo in
// the switch would break the documented setup.
func TestMiniMaxProvidersBuild(t *testing.T) {
	for _, provider := range []string{
		"minimax",
		"minimax-cn",
		"minimax-china",
		"minimax-global",
		"minimax-intl",
		"minimax-io",
	} {
		cfg := config.Default()
		cfg.Agent.Provider = provider
		cfg.Agent.Model = agent.MinimaxDefaultModel

		model, err := newModel(cfg, "key")
		if err != nil {
			t.Errorf("the provider %q does not build: %v", provider, err)

			continue
		}

		if model == nil {
			t.Errorf("the provider %q built no model", provider)
		}
	}
}

// TestMiniMaxDefaultModel verifies that MiniMax works without a model
// in the configuration.
//
// The platform has a current model, and a user who only pasted the
// provider should get a working setup.
func TestMiniMaxDefaultModel(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.Provider = "minimax"
	cfg.Agent.Model = ""

	model, err := newModel(cfg, "key")
	if err != nil {
		t.Fatalf("the provider does not build without a model: %v", err)
	}

	if model == nil {
		t.Fatal("the provider built no model")
	}

	if got := model.Model(); got != agent.MinimaxDefaultModel {
		t.Errorf("the default model is %q, want %q", got, agent.MinimaxDefaultModel)
	}
}

// TestMiniMaxAPIKeyEnv verifies that MiniMax reads the conventional
// environment variable.
func TestMiniMaxAPIKeyEnv(t *testing.T) {
	for _, provider := range []string{"minimax", "minimax-cn"} {
		cfg := config.Default()
		cfg.Agent.Provider = provider

		if got := defaultAPIKeyEnv(provider); got != "MINIMAX_API_KEY" {
			t.Errorf("the key variable of %q is %q, want MINIMAX_API_KEY", provider, got)
		}
	}
}

// TestUnknownProvider verifies that an unknown provider is refused.
//
// The error is the first thing a user sees when the provider name is
// wrong, so it has to be clear.
func TestUnknownProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.Provider = "not-a-provider"
	cfg.Agent.Model = "m"

	if _, err := newModel(cfg, "key"); err == nil {
		t.Error("an unknown provider was accepted")
	}
}

// TestAMissingKeyIsReported verifies that a provider with no key is
// refused, and that the error names the variable to export.
//
// The request would carry no Authorization header at all, and the
// provider would answer with a message about the header. That message
// says nothing about the variable that holds the key, and nothing
// about the fact that the variable is empty.
func TestAMissingKeyIsReported(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "")

	cfg := config.Default()
	cfg.Agent.Provider = "minimax"
	cfg.Agent.Model = agent.MinimaxDefaultModel

	_, err := newModel(cfg, agentAPIKey(cfg))
	if err == nil {
		t.Fatal("newModel() did not report the missing key")
	}

	if !errors.Is(err, agent.ErrNoAPIKey) {
		t.Errorf("newModel() error = %v, want ErrNoAPIKey", err)
	}

	if !strings.Contains(err.Error(), "MINIMAX_API_KEY") {
		t.Errorf("the error does not name the variable: %v", err)
	}
}

// TestTheKeyIsReadFromTheEnvironment verifies that the key of the
// provider is found in the variable the configuration names.
func TestTheKeyIsReadFromTheEnvironment(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "sk-the-key")

	cfg := config.Default()
	cfg.Agent.Provider = "minimax"
	cfg.Agent.Model = agent.MinimaxDefaultModel

	if got := agentAPIKey(cfg); got != "sk-the-key" {
		t.Errorf("agentAPIKey() = %q, want the key", got)
	}
}

// TestTheKeyVariableCanBeChanged verifies that the configuration can
// name another variable.
func TestTheKeyVariableCanBeChanged(t *testing.T) {
	t.Setenv("MY_OWN_KEY", "sk-other")

	cfg := config.Default()
	cfg.Agent.Provider = "minimax"
	cfg.Agent.Model = agent.MinimaxDefaultModel
	cfg.Agent.APIKeyEnv = "MY_OWN_KEY"

	if got := agentAPIKey(cfg); got != "sk-other" {
		t.Errorf("agentAPIKey() = %q, want the key of the named variable", got)
	}

	// And the error names that variable, so a mismatch is obvious.
	t.Setenv("MY_OWN_KEY", "")

	_, err := newModel(cfg, agentAPIKey(cfg))
	if err == nil {
		t.Fatal("newModel() did not report the missing key")
	}

	if !strings.Contains(err.Error(), "MY_OWN_KEY") {
		t.Errorf("the error does not name the variable: %v", err)
	}
}
