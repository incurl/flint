package agent

import (
	"context"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"github.com/charmbracelet/openai-go/option"
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
	// Provider is the name of the provider: "minimax" (the
	// international platform), "minimax-cn" (the Chinese one), "openai",
	// "openaicompat", "openrouter" or "local".
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
	return newModel(cfg, nil)
}

// NewModelWithClient builds a model with a custom HTTP client.
//
// It is the seam a test uses to see which host the model reaches: the
// client records the request instead of sending it.
//
// A provider that reaches the wrong endpoint does not fail with
// "unknown provider". It fails with whatever the endpoint says — for
// MiniMax, "insufficient balance" — and the user has no way to tell
// that the endpoint is the problem.
func NewModelWithClient(cfg ProviderConfig, client option.HTTPClient) (fantasy.LanguageModel, error) {
	return newModel(cfg, client)
}

// newModel builds a model, with an optional HTTP client.
func newModel(cfg ProviderConfig, client option.HTTPClient) (fantasy.LanguageModel, error) {
	provider := strings.ToLower(cfg.Provider)

	switch provider {
	case "openai":
		opts := []openai.Option{openai.WithAPIKey(cfg.APIKey)}
		if cfg.BaseURL != "" {
			opts = append(opts, openai.WithBaseURL(cfg.BaseURL))
		}

		if client != nil {
			opts = append(opts, openai.WithHTTPClient(client))
		}

		built, err := openai.New(opts...)
		if err != nil {
			return nil, fmt.Errorf("agent: build the provider: %w", err)
		}

		return providerModel(cfg.Model, built)

	case "minimax", "minimax-global", "minimax-intl", "minimax-io":
		// The international platform. It is the default: it is the one
		// the global website serves, and the one a key from
		// platform.minimax.io belongs to.
		//
		// The endpoint is fixed, but a base URL in the configuration
		// still wins: a proxy or a gateway is a legitimate reason to
		// override it.
		baseURL := MinimaxGlobalURL
		if cfg.BaseURL != "" {
			baseURL = cfg.BaseURL
		}

		return compatModel(cfg.Model, cfg.APIKey, baseURL, client)

	case "minimax-cn", "minimax-china", "minimax-china-platform":
		// The Chinese platform. It is a separate account, with a
		// separate key and a separate balance: a key from the
		// international platform is refused here, and the error says
		// the balance is insufficient.
		baseURL := MinimaxChinaURL
		if cfg.BaseURL != "" {
			baseURL = cfg.BaseURL
		}

		return compatModel(cfg.Model, cfg.APIKey, baseURL, client)

	case "openaicompat", "openai-compatible", "openrouter", "local":
		opts := []openaicompat.Option{openaicompat.WithAPIKey(cfg.APIKey)}
		if cfg.BaseURL != "" {
			opts = append(opts, openaicompat.WithBaseURL(cfg.BaseURL))
		}

		if client != nil {
			opts = append(opts, openaicompat.WithHTTPClient(client))
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
func compatModel(modelID, apiKey, baseURL string, client option.HTTPClient) (fantasy.LanguageModel, error) {
	opts := []openaicompat.Option{
		openaicompat.WithAPIKey(apiKey),
		openaicompat.WithBaseURL(baseURL),
	}

	if client != nil {
		opts = append(opts, openaicompat.WithHTTPClient(client))
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
