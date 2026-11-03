package agent

import (
	"context"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
)

// The MiniMax endpoints.
//
// MiniMax runs two platforms: the Chinese one and the international
// one. They have different hosts and different accounts, so they are
// two providers rather than one with a configurable host.
const (
	// MinimaxChinaURL is the OpenAI compatible endpoint of the Chinese
	// platform.
	MinimaxChinaURL = "https://api.minimax.cn/v1"
	// MinimaxGlobalURL is the OpenAI compatible endpoint of the
	// international platform.
	MinimaxGlobalURL = "https://api.minimax.io/v1"
)

// MinimaxDefaultModel is the model used when the configuration does not
// name one.
//
// MiniMax does not expose a models endpoint through the compatible API,
// so the names cannot be discovered. This is the model the platform
// recommends for agents and tool use.
const MinimaxDefaultModel = "MiniMax-M3.1-Flash"

// ProviderConfig describes the provider of a model.
//
// It is a plain struct and not the configuration type, so that the
// agent package does not depend on the configuration package.
type ProviderConfig struct {
	// Provider is the name of the provider: "minimax",
	// "minimax-global", "openai", "openaicompat", "openrouter" or
	// "local".
	Provider string
	// Model is the name of the model.
	Model string
	// BaseURL overrides the endpoint of the provider.
	BaseURL string
	// APIKey is the key of the provider.
	APIKey string
}

// NewModel builds the language model of a provider.
//
// Only the OpenAI compatible providers are built in: they cover OpenAI
// itself, MiniMax, OpenRouter and any local server, and they do not
// pull the AWS and Google SDKs into the binary. A project that needs
// another provider adds it here.
func NewModel(cfg ProviderConfig) (fantasy.LanguageModel, error) {
	provider := strings.ToLower(cfg.Provider)

	switch provider {
	case "openai":
		opts := []openai.Option{openai.WithAPIKey(cfg.APIKey)}
		if cfg.BaseURL != "" {
			opts = append(opts, openai.WithBaseURL(cfg.BaseURL))
		}

		built, err := openai.New(opts...)
		if err != nil {
			return nil, fmt.Errorf("agent: build the provider: %w", err)
		}

		return providerModel(cfg.Model, built)

	case "minimax", "minimax-cn", "minimax-china":
		// The Chinese platform. The endpoint is fixed, but a base URL
		// in the configuration still wins: a proxy or a gateway is a
		// legitimate reason to override it.
		baseURL := MinimaxChinaURL
		if cfg.BaseURL != "" {
			baseURL = cfg.BaseURL
		}

		return compatModel(cfg.Model, cfg.APIKey, baseURL)

	case "minimax-global", "minimax-intl", "minimax-io":
		// The international platform.
		baseURL := MinimaxGlobalURL
		if cfg.BaseURL != "" {
			baseURL = cfg.BaseURL
		}

		return compatModel(cfg.Model, cfg.APIKey, baseURL)

	case "openaicompat", "openai-compatible", "openrouter", "local":
		opts := []openaicompat.Option{openaicompat.WithAPIKey(cfg.APIKey)}
		if cfg.BaseURL != "" {
			opts = append(opts, openaicompat.WithBaseURL(cfg.BaseURL))
		}

		built, err := openaicompat.New(opts...)
		if err != nil {
			return nil, fmt.Errorf("agent: build the provider: %w", err)
		}

		return providerModel(cfg.Model, built)

	default:
		return nil, fmt.Errorf("agent: unknown provider %q", cfg.Provider)
	}
}

// compatModel builds a model from an OpenAI compatible endpoint.
//
// It is the path of the providers that only differ by their host: the
// protocol is the same.
func compatModel(modelID, apiKey, baseURL string) (fantasy.LanguageModel, error) {
	opts := []openaicompat.Option{
		openaicompat.WithAPIKey(apiKey),
		openaicompat.WithBaseURL(baseURL),
	}

	built, err := openaicompat.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("agent: build the provider: %w", err)
	}

	if modelID == "" {
		modelID = MinimaxDefaultModel
	}

	return providerModel(modelID, built)
}

// providerModel returns the model of a provider.
func providerModel(modelID string, provider fantasy.Provider) (fantasy.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("agent: the model is required")
	}

	model, err := provider.LanguageModel(context.Background(), modelID)
	if err != nil {
		return nil, fmt.Errorf("agent: build the model %q: %w", modelID, err)
	}

	return model, nil
}
