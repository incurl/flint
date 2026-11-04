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
// The setup is always built: the tools, the conversations and the
// permission gate do not need a model. Only the model is missing when
// no provider is configured, and the guided setup installs one without
// a restart.
//
// The publisher is what carries the events of a run to the UI. It is
// created here, before the program exists, and the program is given to
// it once it is running.
func newRunner(cfg *config.Config, store pbstore.Store, logger *slog.Logger) (*agent.Setup, error) {
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
		Registry:     registry,
		SystemPrompt: systemPrompt,
		MaxSteps:     cfg.Agent.MaxSteps,
		// The history is a template: the run replaces it with the
		// conversation it names, because the page creates one on the
		// first turn.
		History: agent.NewHistory(sessions, ""),
	})
	runner.AddPublisher(publisher)

	// The task list is a tool of the runner: the runner owns the lists
	// of the conversations.
	agent.RegisterTodosTool(registry, runner)

	// The model is the only part that needs a provider. Without one the
	// runner has no model, and the guided setup installs one later.
	if cfg.Agent.Provider == "" || cfg.Agent.Model == "" {
		logger.Info("no agent provider is configured", "provider", cfg.Agent.Provider)

		return &agent.Setup{
			Runner:    runner,
			Resolver:  broker,
			Service:   sessions,
			Publisher: publisher,
		}, nil
	}

	// The endpoint is logged, because it is the setting that is
	// invisible and decisive: a provider that reaches the wrong
	// endpoint does not fail with "unknown provider", it fails with
	// whatever the endpoint says — for MiniMax, "insufficient
	// balance" — and the user has no way to tell that the endpoint is
	// the problem.
	logger.Info("the agent is configured",
		"provider", cfg.Agent.Provider,
		"model", cfg.Agent.Model,
		"base_url", cfg.Agent.BaseURL,
		"api_key_env", cfg.Agent.APIKeyEnv,
	)

	model, err := newModel(cfg, agentAPIKey(cfg))
	if err != nil {
		return nil, err
	}

	if err := runner.SetModel(model); err != nil {
		return nil, fmt.Errorf("cmd/tui: set the model: %w", err)
	}

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
	case "minimax", "minimax-global", "minimax-intl", "minimax-io",
		"minimax-cn", "minimax-china":
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

// newModel builds the language model of the configuration.
//
// The providers live in the agent package, so that the application can
// build one too when the guided setup installs a model.
func newModel(cfg *config.Config, apiKey string) (fantasy.LanguageModel, error) {
	return agent.NewModel(agent.ProviderConfig{
		Provider: cfg.Agent.Provider,
		Model:    cfg.Agent.Model,
		BaseURL:  cfg.Agent.BaseURL,
		APIKey:   apiKey,
	})
}
