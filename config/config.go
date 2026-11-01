// Package config resolves the application configuration.
//
// The resolution order is: CLI flags > environment variables > TOML
// config file > built-in defaults.
//
// The package is deliberately dependency free (besides the TOML parser) so
// that it can be reused by any downstream project.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/theme"
)

// EnvPrefix is the prefix of the environment variables read by the loader.
//
// Example: FLINT_REMOTE_URL, FLINT_THEME, FLINT_PER_PAGE.
const EnvPrefix = "FLINT_"

// Default values used when neither a flag, an env var nor a config file
// provides a value.
const (
	// DefaultMode is the default store mode.
	DefaultMode = string(pbstore.ModeEmbedded)
	// DefaultDataDir is the default PocketBase data directory.
	DefaultDataDir = "./pb_data"
	// DefaultPerPage is the default page size of the record list.
	DefaultPerPage = 30
	// DefaultSyncBatchSize is the default number of records fetched per
	// remote request during a sync.
	DefaultSyncBatchSize = 100
	// DefaultTheme is the default theme variant.
	DefaultTheme = string(theme.VariantDark)
)

// Config is the effective configuration of the application.
type Config struct {
	// Mode is the store mode: "embedded" or "remote".
	Mode string `toml:"mode"`
	// Theme is the theme variant: "dark" or "light".
	Theme string `toml:"theme"`
	// PerPage is the page size of the record list.
	PerPage int `toml:"per_page"`
	// LogLevel is the log verbosity: "debug", "info", "warn" or "error".
	LogLevel string `toml:"log_level"`
	// MaxLocalRecords bounds the number of records fetched to evaluate a
	// JSON filter that the native PocketBase grammar cannot express.
	//
	// A value of 0 uses the default.
	MaxLocalRecords int `toml:"max_local_records"`

	// Embedded holds the embedded store settings.
	Embedded EmbeddedConfig `toml:"embedded"`
	// Remote holds the remote store settings.
	Remote RemoteConfig `toml:"remote"`
	// Sync holds the cache synchronisation settings.
	Sync SyncConfig `toml:"sync"`
}

// EmbeddedConfig holds the embedded store settings.
type EmbeddedConfig struct {
	// DataDir is the PocketBase data directory (SQLite db + files).
	DataDir string `toml:"data_dir"`
	// EncryptionEnv is the name of the env variable holding the settings
	// encryption key.
	EncryptionEnv string `toml:"encryption_env"`
	// Dev enables the verbose PocketBase logs.
	Dev bool `toml:"dev"`
}

// RemoteConfig holds the remote store settings.
type RemoteConfig struct {
	// URL is the base URL of the PocketBase server.
	URL string `toml:"url"`
	// Token is the API token used to authenticate the requests.
	//
	// SECURITY: do not commit a token. Prefer the FLINT_REMOTE_TOKEN
	// environment variable.
	Token string `toml:"token"`
	// Collections restricts the collections that can be queried.
	Collections []string `toml:"collections"`
	// Timeout is the timeout of a single HTTP request, in seconds.
	Timeout int `toml:"timeout"`
}

// SyncConfig holds the cache synchronisation settings.
type SyncConfig struct {
	// Collections lists the collections synced by default.
	Collections []string `toml:"collections"`
	// BatchSize is the number of records fetched per remote request.
	BatchSize int `toml:"batch_size"`
	// MaxRecords caps the number of synced records per collection.
	MaxRecords int `toml:"max_records"`
}

// Default returns the built-in default configuration.
func Default() *Config {
	return &Config{
		Mode:     DefaultMode,
		Theme:    DefaultTheme,
		PerPage:  DefaultPerPage,
		LogLevel: "info",

		MaxLocalRecords: pbstore.DefaultMaxLocalRecords,
		Embedded: EmbeddedConfig{
			DataDir: DefaultDataDir,
		},
		Remote: RemoteConfig{
			Timeout: 30,
		},
		Sync: SyncConfig{
			BatchSize: DefaultSyncBatchSize,
		},
	}
}

// Load resolves the effective configuration.
//
// The flags take precedence over the environment variables, which take
// precedence over the TOML config file.
//
// The configPath may be empty, in which case the default locations are
// probed: "./flint.toml" and "$XDG_CONFIG_HOME/flint/config.toml".
func Load(configPath string, flags Flags) (*Config, error) {
	cfg := Default()

	// 1. TOML config file (lowest priority).
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return nil, err
	}

	if path != "" {
		if err := loadFile(cfg, path); err != nil {
			return nil, err
		}
	}

	// 2. Environment variables.
	applyEnv(cfg)

	// 3. CLI flags (highest priority).
	applyFlags(cfg, flags)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks the consistency of the configuration.
func (c *Config) Validate() error {
	switch pbstore.Mode(c.Mode) {
	case pbstore.ModeEmbedded, pbstore.ModeRemote:
	default:
		return fmt.Errorf("config: invalid mode %q (expected %q or %q)",
			c.Mode, pbstore.ModeEmbedded, pbstore.ModeRemote)
	}

	if !theme.Variant(c.Theme).Valid() {
		return fmt.Errorf("config: invalid theme %q (expected %q or %q)",
			c.Theme, theme.VariantDark, theme.VariantLight)
	}

	if c.PerPage <= 0 {
		return fmt.Errorf("config: per_page must be positive, got %d", c.PerPage)
	}

	if pbstore.Mode(c.Mode) == pbstore.ModeRemote && c.Remote.URL == "" {
		return fmt.Errorf("config: remote.url is required in the %q mode", c.Mode)
	}

	return nil
}

// SettingsRows renders the configuration as "name: value" pairs.
//
// The rows are displayed by the settings page and are also useful to log
// the effective configuration at startup.
func (c *Config) SettingsRows() [][2]string {
	rows := [][2]string{
		{"mode", c.Mode},
		{"theme", c.Theme},
		{"per_page", fmt.Sprintf("%d", c.PerPage)},
		{"log_level", c.LogLevel},
		{"max_local_records", fmt.Sprintf("%d", c.MaxLocalRecords)},
	}

	if pbstore.Mode(c.Mode) == pbstore.ModeEmbedded {
		rows = append(rows,
			[2]string{"embedded.data_dir", c.Embedded.DataDir},
			[2]string{"embedded.dev", fmt.Sprintf("%t", c.Embedded.Dev)},
		)
	}

	rows = append(rows,
		[2]string{"remote.url", c.Remote.URL},
		[2]string{"remote.collections", strings.Join(c.Remote.Collections, ", ")},
		[2]string{"sync.collections", strings.Join(c.Sync.Collections, ", ")},
		[2]string{"sync.batch_size", fmt.Sprintf("%d", c.Sync.BatchSize)},
		[2]string{"sync.max_records", fmt.Sprintf("%d", c.Sync.MaxRecords)},
	)

	return rows
}

// RemoteTimeout returns the configured remote timeout.
func (c *Config) RemoteTimeout() time.Duration {
	if c.Remote.Timeout <= 0 {
		return pbstore.DefaultRemoteTimeout
	}

	return time.Duration(c.Remote.Timeout) * time.Second
}

// resolveConfigPath returns the path of the config file to load.
//
// It returns an empty string when no config file exists.
func resolveConfigPath(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("config: cannot read %q: %w", explicit, err)
		}

		return explicit, nil
	}

	candidates := []string{"flint.toml"}

	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		candidates = append(candidates, filepath.Join(dir, "flint", "config.toml"))
	}

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", nil
}

// loadFile merges the content of a TOML file into the configuration.
func loadFile(cfg *Config, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: cannot read %q: %w", path, err)
	}

	if err := toml.Unmarshal(raw, cfg); err != nil {
		return fmt.Errorf("config: cannot parse %q: %w", path, err)
	}

	return nil
}

// applyEnv overrides the configuration with the environment variables.
func applyEnv(cfg *Config) {
	if v := os.Getenv(EnvPrefix + "MODE"); v != "" {
		cfg.Mode = v
	}

	if v := os.Getenv(EnvPrefix + "THEME"); v != "" {
		cfg.Theme = v
	}

	if v := os.Getenv(EnvPrefix + "PER_PAGE"); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.PerPage = n
		}
	}

	if v := os.Getenv(EnvPrefix + "LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	if v := os.Getenv(EnvPrefix + "MAX_LOCAL_RECORDS"); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.MaxLocalRecords = n
		}
	}

	if v := os.Getenv(EnvPrefix + "DATA_DIR"); v != "" {
		cfg.Embedded.DataDir = v
	}

	if v := os.Getenv(EnvPrefix + "ENCRYPTION_ENV"); v != "" {
		cfg.Embedded.EncryptionEnv = v
	}

	if v := os.Getenv(EnvPrefix + "DEV"); v != "" {
		cfg.Embedded.Dev = parseBool(v)
	}

	if v := os.Getenv(EnvPrefix + "REMOTE_URL"); v != "" {
		cfg.Remote.URL = v
	}

	if v := os.Getenv(EnvPrefix + "REMOTE_TOKEN"); v != "" {
		cfg.Remote.Token = v
	}

	if v := os.Getenv(EnvPrefix + "REMOTE_COLLECTIONS"); v != "" {
		cfg.Remote.Collections = parseList(v)
	}

	if v := os.Getenv(EnvPrefix + "REMOTE_TIMEOUT"); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.Remote.Timeout = n
		}
	}

	if v := os.Getenv(EnvPrefix + "SYNC_COLLECTIONS"); v != "" {
		cfg.Sync.Collections = parseList(v)
	}

	if v := os.Getenv(EnvPrefix + "SYNC_BATCH_SIZE"); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.Sync.BatchSize = n
		}
	}

	if v := os.Getenv(EnvPrefix + "SYNC_MAX_RECORDS"); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.Sync.MaxRecords = n
		}
	}
}

// applyFlags overrides the configuration with the CLI flags.
//
// Only the flags that were explicitly set are applied.
func applyFlags(cfg *Config, flags Flags) {
	if flags.Mode != "" {
		cfg.Mode = flags.Mode
	}

	if flags.Theme != "" {
		cfg.Theme = flags.Theme
	}

	if flags.PerPage > 0 {
		cfg.PerPage = flags.PerPage
	}

	if flags.LogLevel != "" {
		cfg.LogLevel = flags.LogLevel
	}

	if flags.MaxLocalRecords > 0 {
		cfg.MaxLocalRecords = flags.MaxLocalRecords
	}

	if flags.DataDir != "" {
		cfg.Embedded.DataDir = flags.DataDir
	}

	if flags.EncryptionEnv != "" {
		cfg.Embedded.EncryptionEnv = flags.EncryptionEnv
	}

	if flags.Dev {
		cfg.Embedded.Dev = true
	}

	if flags.RemoteURL != "" {
		cfg.Remote.URL = flags.RemoteURL
	}

	if flags.RemoteToken != "" {
		cfg.Remote.Token = flags.RemoteToken
	}

	if len(flags.RemoteCollections) > 0 {
		cfg.Remote.Collections = flags.RemoteCollections
	}

	if len(flags.SyncCollections) > 0 {
		cfg.Sync.Collections = flags.SyncCollections
	}

	if flags.SyncBatchSize > 0 {
		cfg.Sync.BatchSize = flags.SyncBatchSize
	}

	if flags.SyncMaxRecords > 0 {
		cfg.Sync.MaxRecords = flags.SyncMaxRecords
	}
}

// parseInt parses an integer, ignoring the invalid values.
func parseInt(v string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err != nil {
		return 0, err
	}

	return n, nil
}

// parseBool parses a boolean, ignoring the invalid values.
func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// parseList splits a comma separated list.
func parseList(v string) []string {
	parts := strings.Split(v, ",")

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}

	return out
}
