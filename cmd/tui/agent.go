package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/pages"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/session"
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

// newRunner builds the agent setup described by the configuration.
//
// It returns nil when no provider is configured: the agent is optional,
// and the application must still run without it.
//
// The publisher is what carries the events of a run to the UI. It is
// created here, before the program exists, and the program is given to
// it once it is running.
func newRunner(cfg *config.Config, store pbstore.Store, logger *slog.Logger) (*agent.Setup, error) {
	if cfg.Agent.Provider == "" || cfg.Agent.Model == "" {
		logger.Info("no agent provider is configured", "provider", cfg.Agent.Provider)

		return nil, nil
	}

	apiKey := agentAPIKey(cfg)

	model, err := newModel(cfg, apiKey)
	if err != nil {
		return nil, err
	}

	// The conversations live in PocketBase, like every other piece of
	// data of the template.
	sessions := session.NewService(store)
	if err := session.EnsureCollections(context.Background(), store); err != nil {
		// The collections are a convenience: an application that cannot
		// store its conversations still runs, it just does not remember
		// them.
		logger.Warn("the conversations cannot be stored", "error", err)
	}

	publisher := &programPublisher{}

	// The broker asks the user through the publisher, and the page
	// answers through the broker.
	broker := agent.NewDecisionBroker(publisher)

	registry := agent.NewRegistry()
	agent.RegisterStoreTools(registry, store, agent.NewGate(permissionMode(cfg), broker))

	systemPrompt := cfg.Agent.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultAgentSystemPrompt
	}

	runner := agent.NewRunner(agent.Options{
		Model:        model,
		Registry:     registry,
		SystemPrompt: systemPrompt,
		MaxSteps:     cfg.Agent.MaxSteps,
		// The history is a template: the run replaces it with the
		// conversation it names, because the page creates one on the
		// first turn.
		History: agent.NewHistory(sessions, ""),
	})
	runner.AddPublisher(publisher)

	return &agent.Setup{
		Runner:    runner,
		Resolver:  broker,
		Service:   sessions,
		Publisher: publisher,
	}, nil
}

// programPublisher carries the events of a run to the Bubble Tea
// program.
//
// The runs happen in their own goroutines, so the only way to reach the
// update loop is to send a message to the program.
type programPublisher struct {
	// mu guards the program and the counter.
	mu sync.Mutex
	// program is the running program.
	program *tea.Program
}

// Attach gives the publisher the program to send to.
//
// It is called once the program is created, before it runs.
func (p *programPublisher) Attach(program *tea.Program) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.program = program
}

// Publish implements [agent.Publisher].
func (p *programPublisher) Publish(event agent.Event) {
	p.mu.Lock()
	program := p.program
	p.mu.Unlock()

	if program == nil {
		return
	}

	program.Send(pages.AgentEventMsg{Event: event})
}

// PublishPermission implements [agent.RequestPublisher].
func (p *programPublisher) PublishPermission(request agent.PermissionRequest) {
	p.mu.Lock()
	program := p.program
	p.mu.Unlock()

	if program != nil {
		program.Send(pages.AgentPermissionMsg{ID: request.ID, Request: request})
	}
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
	case "minimax", "minimax-cn", "minimax-china", "minimax-global", "minimax-intl", "minimax-io":
		// Both MiniMax platforms use the same variable name: the
		// accounts are separate, not the convention.
		return "MINIMAX_API_KEY"
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

// The MiniMax endpoints.
//
// MiniMax runs two platforms: the Chinese one and the international
// one. They have different hosts and different accounts, so they are
// two providers rather than one with a configurable host.
const (
	// minimaxChinaURL is the OpenAI compatible endpoint of the Chinese
	// platform.
	minimaxChinaURL = "https://api.minimax.cn/v1"
	// minimaxGlobalURL is the OpenAI compatible endpoint of the
	// international platform.
	minimaxGlobalURL = "https://api.minimax.io/v1"
)

// The MiniMax models.
//
// MiniMax does not expose a models endpoint through the compatible API,
// so the name of the default is listed here. It is the model the
// platform recommends for agents and tool use.
const (
	// minimaxDefaultModel is the model used when the configuration does
	// not name one.
	minimaxDefaultModel = "MiniMax-M3.1-Flash"
)

// newModel builds the language model of the configuration.
//
// The template ships the OpenAI compatible providers only: they cover
// OpenAI itself, OpenRouter, MiniMax and any local server, and they do
// not pull the AWS and Google SDKs into the binary. A downstream
// project that needs another provider adds it here.
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

	case "minimax", "minimax-cn", "minimax-china":
		// The Chinese platform. The endpoint is fixed, but a base URL
		// in the configuration still wins: a proxy or a gateway is a
		// legitimate reason to override it.
		baseURL := minimaxChinaURL
		if cfg.Agent.BaseURL != "" {
			baseURL = cfg.Agent.BaseURL
		}

		return newCompatModel(cfg.Agent.Model, apiKey, baseURL)

	case "minimax-global", "minimax-intl", "minimax-io":
		// The international platform.
		baseURL := minimaxGlobalURL
		if cfg.Agent.BaseURL != "" {
			baseURL = cfg.Agent.BaseURL
		}

		return newCompatModel(cfg.Agent.Model, apiKey, baseURL)

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

// newCompatModel builds a model from an OpenAI compatible endpoint.
//
// It is the path of the providers that only differ by their host: the
// protocol is the same.
func newCompatModel(modelID, apiKey, baseURL string) (fantasy.LanguageModel, error) {
	opts := []openaicompat.Option{
		openaicompat.WithAPIKey(apiKey),
		openaicompat.WithBaseURL(baseURL),
	}

	built, err := openaicompat.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("cmd/tui: build the provider: %w", err)
	}

	if modelID == "" {
		modelID = minimaxDefaultModel
	}

	return newProviderModel(modelID, built)
}

// newProviderModel returns the model of a provider.
func newProviderModel(modelID string, provider fantasy.Provider) (fantasy.LanguageModel, error) {
	model, err := provider.LanguageModel(context.Background(), modelID)
	if err != nil {
		return nil, fmt.Errorf("cmd/tui: build the model %q: %w", modelID, err)
	}

	return model, nil
}
