package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/incurl/flint/config"
)

// TestCheckAgentReportsTheMissingKey verifies that the diagnostic says
// the variable is not exported.
//
// The provider answers with the same message for an empty key, an
// invalid key and a key of the other platform, so the check has to
// tell them apart itself.
func TestCheckAgentReportsTheMissingKey(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "")

	cfg := config.Default()
	cfg.Agent.Provider = "minimax"
	cfg.Agent.Model = "MiniMax-M3.1-Flash"
	cfg.Agent.APIKeyEnv = "MINIMAX_API_KEY"

	var out bytes.Buffer

	err := checkAgent(cfg, &out)
	if err == nil {
		t.Fatal("checkAgent() did not report the missing key")
	}

	text := out.String()

	if !strings.Contains(text, "MINIMAX_API_KEY") {
		t.Errorf("the report does not name the variable:\n%s", text)
	}

	if !strings.Contains(text, "not exported") && !strings.Contains(text, "empty") {
		t.Errorf("the report does not say what is wrong:\n%s", text)
	}
}

// TestCheckAgentReportsTheConfiguration verifies that the effective
// configuration is in the report.
//
// The endpoint is the setting that is invisible and decisive.
func TestCheckAgentReportsTheConfiguration(t *testing.T) {
	stubKeyTester(t)

	t.Setenv("MINIMAX_API_KEY", "sk-fake")

	cfg := config.Default()
	cfg.Agent.Provider = "minimax"
	cfg.Agent.Model = "MiniMax-M3.1-Flash"
	cfg.Agent.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.Agent.BaseURL = "https://api.minimax.io/v1"

	var out bytes.Buffer

	// The request fails against the fake key, and the report is what
	// matters here.
	_ = checkAgent(cfg, &out)

	text := out.String()

	for _, want := range []string{"minimax", "MiniMax-M3.1-Flash", "https://api.minimax.io/v1", "MINIMAX_API_KEY"} {
		if !strings.Contains(text, want) {
			t.Errorf("the report does not hold %q:\n%s", want, text)
		}
	}
}

// TestCheckAgentReportsTheTrimmedKey verifies that a key with
// whitespace is reported as trimmed.
//
// A trailing space makes the Authorization header wrong, and the
// provider answers with a message about the header.
func TestCheckAgentReportsTheTrimmedKey(t *testing.T) {
	stubKeyTester(t)

	t.Setenv("MINIMAX_API_KEY", "sk-fake ")

	cfg := config.Default()
	cfg.Agent.Provider = "minimax"
	cfg.Agent.Model = "MiniMax-M3.1-Flash"
	cfg.Agent.APIKeyEnv = "MINIMAX_API_KEY"

	var out bytes.Buffer

	_ = checkAgent(cfg, &out)

	if !strings.Contains(out.String(), "trimmed") {
		t.Errorf("the report does not say the key was trimmed:\n%s", out.String())
	}
}

// TestEffectiveBaseURL verifies that the report shows the endpoint the
// application will use, even when the configuration leaves it empty.
func TestEffectiveBaseURL(t *testing.T) {
	cfg := config.Default()

	cfg.Agent.Provider = "minimax"

	if got := effectiveBaseURL(cfg); got != "https://api.minimax.io/v1" {
		t.Errorf("effectiveBaseURL() = %q, want the international endpoint", got)
	}

	cfg.Agent.Provider = "minimax-cn"

	if got := effectiveBaseURL(cfg); got != "https://api.minimax.cn/v1" {
		t.Errorf("effectiveBaseURL() = %q, want the Chinese endpoint", got)
	}

	cfg.Agent.Provider = "minimax"
	cfg.Agent.BaseURL = "https://example.test/v1"

	if got := effectiveBaseURL(cfg); got != "https://example.test/v1" {
		t.Errorf("effectiveBaseURL() = %q, want the configured endpoint", got)
	}
}

// stubKeyTester replaces the request that tests a key.
//
// The tests must not reach the network: they check the report, not the
// provider.
func stubKeyTester(t *testing.T) {
	t.Helper()

	previous := keyTester

	keyTester = func(_ *config.Config, _ string, _ io.Writer) error {
		return errors.New("the key was refused")
	}

	t.Cleanup(func() { keyTester = previous })
}
