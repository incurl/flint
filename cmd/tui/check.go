package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/agent"
)

// checkAgent reports the effective agent configuration and tests the
// key against the endpoint.
//
// It exists because the provider's own answer is useless: MiniMax
// answers "Please carry the API secret key in the 'Authorization'
// field of the request header" for a key that is empty, one that is
// invalid, one that belongs to the other platform and one that
// carries a stray space. The user is left looking for a header that
// was there all along.
//
// The check prints what the application resolved, so the difference
// between "the variable is not exported" and "the variable holds the
// wrong key" is visible.
func checkAgent(cfg *config.Config, out io.Writer) error {
	fmt.Fprintf(out, "The agent configuration:\n\n")
	fmt.Fprintf(out, "  provider    %s\n", orNone(cfg.Agent.Provider))
	fmt.Fprintf(out, "  model       %s\n", orNone(cfg.Agent.Model))
	fmt.Fprintf(out, "  base_url    %s\n", orNone(effectiveBaseURL(cfg)))
	fmt.Fprintf(out, "  api_key_env %s\n", orNone(apiKeyEnvName(cfg)))
	fmt.Fprintf(out, "  permission  %s\n\n", orNone(cfg.Agent.PermissionMode))

	name := apiKeyEnvName(cfg)
	if name == "" {
		fmt.Fprintf(out, "The provider needs no key.\n")

		return nil
	}

	raw, present := os.LookupEnv(name)
	key := agentAPIKey(cfg)

	fmt.Fprintf(out, "The key:\n\n")
	fmt.Fprintf(out, "  variable    %s\n", name)
	fmt.Fprintf(out, "  exported    %v\n", present)

	switch {
	case !present:
		fmt.Fprintf(out, "  length      0\n\n")
		fmt.Fprintf(out, "The variable is not exported. The shell that starts the application does not have it.\n")
		fmt.Fprintf(out, "\n  export %s=\"...\"\n", name)

		return fmt.Errorf("the variable %s is not exported", name)

	case key == "":
		fmt.Fprintf(out, "  length      0\n\n")
		fmt.Fprintf(out, "The variable is exported but empty.\n")

		return fmt.Errorf("the variable %s is empty", name)
	}

	fmt.Fprintf(out, "  length      %d\n", len(key))
	fmt.Fprintf(out, "  trimmed     %v\n\n", raw != key)

	if raw != key {
		fmt.Fprintf(out, "The key had whitespace around it. It is trimmed before it is sent.\n\n")
	}

	fmt.Fprintf(out, "Testing the key against %s ...\n\n", effectiveBaseURL(cfg))

	if err := keyTester(cfg, key, out); err != nil {
		return err
	}

	fmt.Fprintf(out, "The key works.\n")

	return nil
}

// keyTester sends the request that tests a key.
//
// It is a variable so that a test does not reach the network.
var keyTester = testKey

// testKey sends one request and reports what came back.
func testKey(cfg *config.Config, key string, out io.Writer) error {
	model, err := agent.NewModel(agent.ProviderConfig{
		Provider: cfg.Agent.Provider,
		Model:    cfg.Agent.Model,
		BaseURL:  cfg.Agent.BaseURL,
		APIKey:   key,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, err = model.Generate(ctx, fantasy.Call{
		Prompt: fantasy.Prompt{fantasy.NewUserMessage("Reply with the single word: ok")},
	})
	if err == nil {
		return nil
	}

	fmt.Fprintf(out, "The provider refused the key:\n\n  %v\n\n", err)
	fmt.Fprintf(out, "This is what the provider says for a key that is empty, one that is invalid,\n")
	fmt.Fprintf(out, "one that belongs to the other platform, and one that is otherwise wrong.\n")
	fmt.Fprintf(out, "The message names the header, and the header is not the problem.\n")

	return err
}

// effectiveBaseURL returns the endpoint the application will use.
//
// The configuration may leave it empty, and then the provider decides.
func effectiveBaseURL(cfg *config.Config) string {
	if cfg.Agent.BaseURL != "" {
		return cfg.Agent.BaseURL
	}

	switch strings.ToLower(cfg.Agent.Provider) {
	case "minimax", "minimax-global", "minimax-intl", "minimax-io":
		return agent.MinimaxGlobalURL
	case "minimax-cn", "minimax-china":
		return agent.MinimaxChinaURL
	default:
		return ""
	}
}

// orNone returns a value, or a dash when it is empty.
func orNone(value string) string {
	if value == "" {
		return "-"
	}

	return value
}
