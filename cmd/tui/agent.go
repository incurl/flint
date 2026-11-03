package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/pbstore"
)

// defaultAgentSystemPrompt is the instruction given to the model when
// the configuration does not provide one.
//
// It tells the model what it can do and, more importantly, that it must
// ask before it changes anything: the permission gate enforces it, and
// the prompt tells the model to expect it.
const defaultAgentSystemPrompt = `You are the assistant of a terminal application that reads and writes PocketBase records.

You answer questions about the records with the tools. Use the read-only tools to look at the data before you answer, and say so when the data does not answer the question.

You change the data only when the user asks for it. The user approves every change, so a refused change is not a failure: explain what you wanted to do and try another way.`

// newRunner builds the agent runner described by the configuration.
//
// It returns nil when no provider is configured: the agent is optional,
// and the application must still run without it.
func newRunner(cfg *config.Config, store pbstore.Store, logger *slog.Logger) (*agent.Runner, error) {
	if cfg.Agent.Provider == "" || cfg.Agent.Model == "" {
		logger.Info("no agent provider is configured", "provider", cfg.Agent.Provider)

		return nil, nil
	}

	apiKey := agentAPIKey(cfg)

	model, err := newModel(cfg, apiKey)
	if err != nil {
		return nil, err
	}

	registry := agent.NewRegistry()
	agent.RegisterStoreTools(registry, store, agent.NewGate(permissionMode(cfg), nil))

	systemPrompt := cfg.Agent.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultAgentSystemPrompt
	}

	return agent.NewRunner(agent.Options{
		Model:        model,
		Registry:     registry,
		SystemPrompt: systemPrompt,
		MaxSteps:     cfg.Agent.MaxSteps,
	}), nil
}

// permissionMode returns the permission mode of the configuration.
//
// The default is to ask: an agent that writes to the data of the user
// must not do it silently.
func permissionMode(cfg *config.Config) agent.PermissionMode {
	switch strings.ToLower(cfg.Agent.PermissionMode) {
	case "allow", "yolo":
		return agent.PermissionModeAllow
	default:
		return agent.PermissionModeAsk
	}
}

// agentAPIKey returns the API key of the provider.
//
// The key is read from the environment variable the configuration names,
// never from the configuration file itself.
func agentAPIKey(cfg *config.Config) string {
	name := cfg.Agent.APIKeyEnv
	if name == "" {
		name = defaultAPIKeyEnv(cfg.Agent.Provider)
	}

	if name == "" {
		return ""
	}

	return os.Getenv(name)
}

// defaultAPIKeyEnv returns the conventional environment variable of a
// provider.
func defaultAPIKeyEnv(provider string) string {
	switch strings.ToLower(provider) {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	case "google":
		return "GOOGLE_API_KEY"
	case "azure":
		return "AZURE_API_KEY"
	case "bedrock":
		return "AWS_ACCESS_KEY_ID"
	default:
		return ""
	}
}

// newModel builds the language model of the configuration.
//
// The template ships the OpenAI compatible providers only: they cover
// OpenAI itself, OpenRouter and any local server, and they do not pull
// the AWS and Google SDKs into the binary. A downstream project that
// needs another provider adds it here.
func newModel(cfg *config.Config, apiKey string) (fantasy.LanguageModel, error) {
	provider := strings.ToLower(cfg.Agent.Provider)

	switch provider {
	case "openai":
		opts := []openai.Option{openai.WithAPIKey(apiKey)}
		if cfg.Agent.BaseURL != "" {
			opts = append(opts, openai.WithBaseURL(cfg.Agent.BaseURL))
		}

		built, err := openai.New(opts...)
		if err != nil {
			return nil, fmt.Errorf("cmd/tui: build the provider: %w", err)
		}

		return newProviderModel(cfg.Agent.Model, built)

	case "openaicompat", "openai-compatible", "openrouter", "local":
		opts := []openaicompat.Option{openaicompat.WithAPIKey(apiKey)}
		if cfg.Agent.BaseURL != "" {
			opts = append(opts, openaicompat.WithBaseURL(cfg.Agent.BaseURL))
		}

		built, err := openaicompat.New(opts...)
		if err != nil {
			return nil, fmt.Errorf("cmd/tui: build the provider: %w", err)
		}

		return newProviderModel(cfg.Agent.Model, built)

	default:
		return nil, fmt.Errorf("cmd/tui: unknown agent provider %q", cfg.Agent.Provider)
	}
}

// newProviderModel returns the model of a provider.
func newProviderModel(modelID string, provider fantasy.Provider) (fantasy.LanguageModel, error) {
	model, err := provider.LanguageModel(context.Background(), modelID)
	if err != nil {
		return nil, fmt.Errorf("cmd/tui: build the model %q: %w", modelID, err)
	}

	return model, nil
}
