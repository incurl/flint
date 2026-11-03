package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// TestRenderIsValidTOML verifies that the rendered example parses.
//
// A configuration file that the application cannot load is worse than
// no configuration file at all.
func TestRenderIsValidTOML(t *testing.T) {
	cfg := Default()
	cfg.Agent.Provider = "minimax"
	cfg.Agent.Model = "MiniMax-M3.1-Flash"
	cfg.Agent.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.Agent.BaseURL = "https://api.minimax.cn/v1"
	cfg.Remote.Collections = []string{"vocab", "notes"}
	cfg.Remote.Timeout = 45

	raw := Render(cfg)

	parsed := &Config{}
	if err := toml.Unmarshal([]byte(raw), parsed); err != nil {
		t.Fatalf("the rendered example does not parse: %v", err)
	}

	if parsed.Agent.Provider != "minimax" {
		t.Errorf("the provider is %q, want minimax", parsed.Agent.Provider)
	}

	if parsed.Agent.Model != "MiniMax-M3.1-Flash" {
		t.Errorf("the model is %q, want MiniMax-M3.1-Flash", parsed.Agent.Model)
	}

	if parsed.Agent.APIKeyEnv != "MINIMAX_API_KEY" {
		t.Errorf("the key variable is %q, want MINIMAX_API_KEY", parsed.Agent.APIKeyEnv)
	}

	if len(parsed.Remote.Collections) != 2 {
		t.Errorf("the collections are %v, want two of them", parsed.Remote.Collections)
	}
}

// TestRenderHasNoKey verifies that the rendered example does not carry
// a secret.
//
// The configuration names the environment variable that holds the key,
// and the key itself is never in the file.
func TestRenderHasNoKey(t *testing.T) {
	cfg := Default()
	cfg.Agent.Provider = "minimax"
	cfg.Agent.APIKeyEnv = "MINIMAX_API_KEY"

	raw := Render(cfg)

	if !strings.Contains(raw, "api_key_env = \"MINIMAX_API_KEY\"") {
		t.Errorf("the example does not name the key variable:\n%s", raw)
	}

	if strings.Contains(raw, "api_key =") {
		t.Errorf("the example has a field for the key itself:\n%s", raw)
	}
}

// TestWriteCreatesTheFile verifies that the configuration is written.
func TestWriteCreatesTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flint.toml")

	cfg := Default()
	cfg.Agent.Provider = "minimax"

	if err := Write(cfg, path); err != nil {
		t.Fatalf("the configuration is not written: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the file is not readable: %v", err)
	}

	if !strings.Contains(string(raw), "provider = \"minimax\"") {
		t.Errorf("the file does not hold the provider:\n%s", raw)
	}
}

// TestWriteIsPrivate verifies that the file is not world readable.
//
// A configuration file may hold a remote token.
func TestWriteIsPrivate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flint.toml")

	if err := Write(Default(), path); err != nil {
		t.Fatalf("the configuration is not written: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the file is not there: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the file has the permissions %o, want 600", perm)
	}
}

// TestWriteReplacesTheFile verifies that an existing configuration is
// replaced, and that it is still valid.
func TestWriteReplacesTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flint.toml")

	if err := os.WriteFile(path, []byte("mode = \"remote\"\n"), 0o600); err != nil {
		t.Fatalf("the file is not written: %v", err)
	}

	cfg := Default()
	cfg.Agent.Provider = "minimax"

	if err := Write(cfg, path); err != nil {
		t.Fatalf("the configuration is not written: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the file is not readable: %v", err)
	}

	if !strings.Contains(string(raw), "provider = \"minimax\"") {
		t.Errorf("the file does not hold the provider:\n%s", raw)
	}

	// The temporary file is not left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("the directory is not readable: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("the directory holds %d files, want only the configuration", len(entries))
	}
}

// TestWritePathPrefersTheExistingFile verifies that the configuration
// is written where it was read from.
//
// A user who keeps the configuration in the XDG directory expects the
// application to write there, not in the working directory.
func TestWritePathPrefersTheExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if err := os.WriteFile(path, []byte("mode = \"remote\"\n"), 0o600); err != nil {
		t.Fatalf("the file is not written: %v", err)
	}

	if got := WritePath(path); got != path {
		t.Errorf("the path is %q, want %q", got, path)
	}
}

// TestWritePathDefaultsToTheWorkingDirectory verifies the default path.
func TestWritePathDefaultsToTheWorkingDirectory(t *testing.T) {
	if got := WritePath(""); got != DefaultFileName {
		t.Errorf("the path is %q, want %q", got, DefaultFileName)
	}
}
