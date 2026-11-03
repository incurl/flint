package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultFileName is the name of the configuration file in the working
// directory.
const DefaultFileName = "flint.toml"

// WritePath returns the path the configuration is written to.
//
// It is the file that was loaded, when there is one: a user who keeps
// the configuration in `~/.config/flint/config.toml` expects the
// application to write there, not in the working directory.
//
// When no configuration file exists yet, it is `flint.toml` in the
// working directory.
func WritePath(explicit string) string {
	if explicit != "" {
		return explicit
	}

	candidates := []string{DefaultFileName}

	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		candidates = append(candidates, filepath.Join(dir, "flint", "config.toml"))
	}

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}

	return DefaultFileName
}

// Save writes the configuration where it was loaded from.
//
// It is the convenience of [Write] for the application: the path is
// taken from the configuration itself.
//
// The values written are the ones the *file* held, not the effective
// ones: a value that came from the environment does not belong in a
// file.
func (c *Config) Save() error {
	if c == nil {
		return fmt.Errorf("config: nil configuration")
	}

	values := c.FileValues()

	return Write(&values, WritePath(c.path))
}

// SaveAgent writes the configuration with the agent settings.
//
// The agent settings are the ones the guided setup collected. The
// other settings are the ones the file held.
//
// The API key is **never** written: the configuration names the
// environment variable that holds it.
func (c *Config) SaveAgent(agent AgentConfig, path string) error {
	if c == nil {
		return fmt.Errorf("config: nil configuration")
	}

	values := c.FileValues()
	values.Agent = agent

	return Write(&values, path)
}

// Write saves the configuration to a TOML file.
//
// The file is written with the permissions of a secret store (0600),
// because a configuration file may hold a remote token, and it is
// created atomically: a failure in the middle of the write leaves the
// previous file intact.
//
// The API key of the agent is **never** written: the configuration
// names the environment variable that holds it.
func Write(cfg *Config, path string) error {
	if cfg == nil {
		return fmt.Errorf("config: nil configuration")
	}

	if path == "" {
		path = DefaultFileName
	}

	content := Render(cfg)

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("config: cannot create %q: %w", dir, err)
		}
	}

	// The file is written to a temporary name and renamed, so that a
	// failure leaves the previous configuration intact.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".flint-*.toml")
	if err != nil {
		return fmt.Errorf("config: cannot create the temporary file: %w", err)
	}

	name := tmp.Name()

	defer func() {
		// The cleanup is a no-op when the file was renamed.
		_ = os.Remove(name)
	}()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("config: cannot write the configuration: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: cannot close the configuration: %w", err)
	}

	// 0600: the file may hold a token.
	if err := os.Chmod(name, 0o600); err != nil {
		return fmt.Errorf("config: cannot set the permissions: %w", err)
	}

	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("config: cannot write %q: %w", path, err)
	}

	return nil
}
