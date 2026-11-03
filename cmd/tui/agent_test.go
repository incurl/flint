package main

import (
	"testing"

	"github.com/incurl/flint/config"
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
		cfg.Agent.Model = minimaxDefaultModel

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

	if got := model.Model(); got != minimaxDefaultModel {
		t.Errorf("the default model is %q, want %q", got, minimaxDefaultModel)
	}
}

// TestMiniMaxAPIKeyEnv verifies that MiniMax reads the conventional
// environment variable.
func TestMiniMaxAPIKeyEnv(t *testing.T) {
	for _, provider := range []string{"minimax", "minimax-global"} {
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
