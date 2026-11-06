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
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/incurl/flint/internal/hybridstore"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/theme"
	"github.com/incurl/flint/internal/wikidata"
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
	// JSRenderer enables the JavaScript record renderer.
	//
	// The renderer overrides how a record is displayed. It is local to
	// the application: unlike the PocketBase hooks, it does not run on
	// a server.
	JSRenderer bool `toml:"js_renderer"`
	// RendererDir is the directory holding the renderer script.
	//
	// It defaults to a "flint_hooks" directory next to the data
	// directory.
	RendererDir string `toml:"renderer_dir"`

	// Embedded holds the embedded store settings.
	Embedded EmbeddedConfig `toml:"embedded"`
	// Remote holds the remote store settings.
	Remote RemoteConfig `toml:"remote"`
	// Sync holds the cache synchronisation settings.
	Sync SyncConfig `toml:"sync"`
	// Agent holds the language model settings.
	Agent AgentConfig `toml:"agent"`
	// Code holds the settings of the code tools.
	Code CodeConfig `toml:"code"`
	// Wikidata holds the settings of the Wikidata tools.
	Wikidata WikidataConfig `toml:"wikidata"`

	// path is the file the configuration was loaded from.
	//
	// It is not a setting: it is not read from the file, and it is not
	// written to it. It is remembered so that a writer saves the
	// configuration where it was read from.
	path string
	// fromFile holds the values the file held, before the environment
	// and the flags overrode them.
	//
	// It is what a writer saves: a value that came from the
	// environment does not belong in a file.
	//
	// It is a pointer so that the type stays flat: a Config that
	// holds a Config is a recursive type, and Go does not allow it.
	fromFile *Config
}

// AgentConfig holds the settings of the language model.
//
// The agent is optional: without a provider the template still runs,
// and the agent page reports that no model is configured.
type AgentConfig struct {
	// Provider is the name of the provider: openai, minimax,
	// minimax-cn, openaicompat, openrouter or local.
	//
	// Only the OpenAI compatible providers are built in: they cover
	// OpenAI itself, MiniMax, OpenRouter and any local server, and they
	// do not pull the AWS and Google SDKs into the binary. A project
	// that needs another provider adds it to newModel in
	// cmd/tui/agent.go.
	Provider string `toml:"provider"`
	// Model is the name of the model, for example "gpt-4o".
	Model string `toml:"model"`
	// BaseURL is the endpoint of the provider.
	//
	// It is only used by the OpenAI compatible providers, which lets a
	// local server be used instead of a hosted one.
	BaseURL string `toml:"base_url"`
	// APIKeyEnv is the name of the environment variable holding the API
	// key.
	//
	// The key is never read from the configuration file itself: a file
	// is easy to copy around, an environment variable is not.
	APIKeyEnv string `toml:"api_key_env"`
	// SystemPrompt is the instruction given to the model.
	SystemPrompt string `toml:"system_prompt"`
	// PermissionMode tells how the mutating tools are gated: "ask" or
	// "allow".
	//
	// "ask" is the default, and any value that is not "allow" falls
	// back to it: a typo must not turn the questions off.
	PermissionMode string `toml:"permission_mode"`
	// MaxSteps bounds the number of model calls of a single run.
	MaxSteps int `toml:"max_steps"`
	// ContextWindow is the size of the context window of the model, in
	// tokens.
	//
	// It is what the compaction is measured against: a conversation is
	// summarised when the context window is nearly full, and not before.
	//
	// Zero means the window is unknown, and the compaction falls back
	// to a character count. A model that reports its window through
	// the provider -- the ones that do -- needs no configuration.
	ContextWindow int `toml:"context_window"`
}

// WikidataConfig holds the settings of the Wikidata tools.
//
// The tools are off by default: they talk to a public endpoint, and an
// application that never asks about Wikidata must not open a connection
// to it.
type WikidataConfig struct {
	// Enabled turns the Wikidata tools on.
	//
	// Turn them on with `wikidata.enabled = true`, FLINT_WIKIDATA=1
	// or --wikidata.
	Enabled bool `toml:"enabled"`
	// Endpoint is the SPARQL endpoint the queries are sent to.
	//
	// It is the public endpoint of Wikidata by default.
	Endpoint string `toml:"endpoint"`
	// Delay is the pause between two queries, in milliseconds.
	//
	// The public endpoint asks the client to wait between the
	// queries, and a client that does not is a client that gets
	// blocked.
	//
	// It is an integer and not a duration: the TOML file holds
	// numbers, and the convention of the file is a number of
	// milliseconds.
	DelayMS int `toml:"delay_ms"`
	// SampleSize is the number of records that are asked about by a
	// run.
	SampleSize int `toml:"sample_size"`
	// MaxSolutions is the budget of solutions of a single query.
	//
	// The budget bounds the memory of a query: a deadline bounds the
	// time, and a query that builds a million solutions in ten
	// seconds is still a million solutions in memory.
	MaxSolutions int `toml:"max_solutions"`
}

// CodeConfig holds the settings of the code tools.
//
// The code tools let the agent read and change the source code of the
// application. They need a language server — gopls, for Go — which is
// looked up on the PATH.
type CodeConfig struct {
	// Root is the directory the agent is allowed to read.
	//
	// It is the boundary of the code tools: a path outside of it is
	// refused, and the language server is started with it as its
	// workspace root.
	//
	// It defaults to a "flint_code" directory next to the data
	// directory, which keeps the code beside the data rather than in
	// the working directory.
	Root string `toml:"root"`
	// Enabled turns the code tools on.
	//
	// They are on by default: an application whose agent is asked
	// about the code needs to be able to answer. The language server
	// is started lazily, so an agent that never asks pays nothing.
	//
	// Turn them off with `code.enabled = false`, FLINT_CODE=0 or
	// --no-code.
	Enabled bool `toml:"enabled"`
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
	// HooksDir is the directory holding the JavaScript application
	// hooks.
	//
	// It defaults to a "pb_hooks" directory next to the data directory.
	HooksDir string `toml:"hooks_dir"`
	// MigrationsDir is the directory holding the JavaScript migrations.
	//
	// It defaults to a "pb_migrations" directory next to the data
	// directory.
	MigrationsDir string `toml:"migrations_dir"`
	// JavaScript enables the PocketBase JavaScriptVM plugin.
	//
	// The plugin embeds a JavaScript runtime and loads the hooks and
	// the migrations from the directories above. It is disabled by
	// default: the plugin adds about 10 MB to the binary and the hooks
	// are an application specific choice.
	JavaScript bool `toml:"javascript"`
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

		MaxLocalRecords: hybridstore.DefaultMaxLocalRecords,
		Embedded: EmbeddedConfig{
			DataDir: DefaultDataDir,
		},
		Remote: RemoteConfig{
			Timeout: 30,
		},
		Sync: SyncConfig{
			BatchSize: DefaultSyncBatchSize,
		},
		// The code tools are on by default: the language server is
		// started lazily, so an agent that never asks about the code
		// never pays for it.
		Code: CodeConfig{
			Enabled: true,
		},
		// The Wikidata tools are off by default: they reach a public
		// endpoint, and an application that never asks about Wikidata
		// must not open a connection to it.
		Wikidata: WikidataConfig{
			Endpoint:     wikidata.DefaultEndpoint,
			DelayMS:      int(wikidata.DefaultDelay.Milliseconds()),
			SampleSize:   wikidata.DefaultSampleSize,
			MaxSolutions: wikidata.DefaultMaxSolutions,
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

	// The path is remembered, so that a writer saves the configuration
	// where it was read from.
	cfg.path = path

	// The values of the file are kept, before the environment and the
	// flags override them: a value that came from the environment does
	// not belong in a file.
	snapshot := *cfg
	snapshot.fromFile = nil
	cfg.fromFile = &snapshot

	// 2. Environment variables.
	applyEnv(cfg)

	// 3. CLI flags (highest priority).
	applyFlags(cfg, flags)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Path returns the path the configuration was loaded from.
//
// It is empty when no configuration file exists yet. Use [WritePath]
// to know where a new one would be written.
func (c *Config) Path() string {
	if c == nil {
		return ""
	}

	return c.path
}

// FileValues returns the values the configuration file held.
//
// The environment variables and the flags are not in it: a value that
// came from the environment does not belong in a file.
//
// It is what a writer saves.
func (c *Config) FileValues() Config {
	if c == nil {
		return *Default()
	}

	if c.fromFile == nil {
		// The configuration was not loaded from a file: the effective
		// values are the ones to write.
		values := *c
		values.path = ""
		values.fromFile = nil

		return values
	}

	values := *c.fromFile

	// The path is not a setting.
	values.path = ""
	values.fromFile = nil

	return values
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
		{"js_renderer", fmt.Sprintf("%t", c.JSRenderer)},
	}

	if c.JSRenderer {
		rows = append(rows, [2]string{"renderer_dir", c.RendererDirPath()})
	}

	if c.Agent.Provider != "" {
		rows = append(rows,
			[2]string{"agent.provider", c.Agent.Provider},
			[2]string{"agent.model", c.Agent.Model},
			[2]string{"agent.permission_mode", c.Agent.PermissionMode},
			[2]string{"agent.context_window", fmt.Sprintf("%d", c.Agent.ContextWindow)},
		)

		// The endpoint is shown because it is the setting that is
		// invisible and decisive: a provider that reaches the wrong
		// endpoint does not fail with "unknown provider", it fails
		// with whatever the endpoint says.
		if c.Agent.BaseURL != "" {
			rows = append(rows, [2]string{"agent.base_url", c.Agent.BaseURL})
		}
	}

	// The code tools are shown even when they are off: the setting
	// that decides whether the agent can read the code is one the user
	// wants to see.
	rows = append(rows,
		[2]string{"code.enabled", fmt.Sprintf("%t", c.Code.Enabled)},
		[2]string{"code.root", c.CodeRoot()},
	)

	// The Wikidata tools are shown even when they are off: they reach
	// a public endpoint, and the user wants to see that they are off.
	rows = append(rows,
		[2]string{"wikidata.enabled", fmt.Sprintf("%t", c.Wikidata.Enabled)},
		[2]string{"wikidata.endpoint", c.Wikidata.Endpoint},
		[2]string{"wikidata.delay_ms", fmt.Sprintf("%d", c.Wikidata.DelayMS)},
	)

	if pbstore.Mode(c.Mode) == pbstore.ModeEmbedded {
		rows = append(rows,
			[2]string{"embedded.data_dir", c.Embedded.DataDir},
			[2]string{"embedded.dev", fmt.Sprintf("%t", c.Embedded.Dev)},
		)

		if c.Embedded.JavaScript {
			rows = append(rows,
				[2]string{"embedded.javascript", "enabled"},
				[2]string{"embedded.hooks_dir", c.HooksDir()},
				[2]string{"embedded.migrations_dir", c.MigrationsDir()},
			)
		}
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

// HooksDir returns the directory holding the JavaScript application
// hooks.
//
// It defaults to a "pb_hooks" directory next to the data directory,
// which is the PocketBase convention.
func (c *Config) HooksDir() string {
	if c.Embedded.HooksDir != "" {
		return c.Embedded.HooksDir
	}

	return filepath.Join(filepath.Dir(c.Embedded.DataDir), "pb_hooks")
}

// MigrationsDir returns the directory holding the JavaScript migrations.
//
// It defaults to a "pb_migrations" directory next to the data
// directory, which is the PocketBase convention.
func (c *Config) MigrationsDir() string {
	if c.Embedded.MigrationsDir != "" {
		return c.Embedded.MigrationsDir
	}

	return filepath.Join(filepath.Dir(c.Embedded.DataDir), "pb_migrations")
}

// RendererDirPath returns the directory holding the renderer script.
//
// It defaults to a "flint_hooks" directory next to the data directory,
// which keeps the script beside the data rather than in the working
// directory.
func (c *Config) RendererDirPath() string {
	if c.RendererDir != "" {
		return c.RendererDir
	}

	return filepath.Join(filepath.Dir(c.Embedded.DataDir), "flint_hooks")
}

// CodeRoot returns the directory the code tools are allowed to read.
//
// It defaults to a "flint_code" directory next to the data directory,
// which keeps the code beside the data rather than in the working
// directory.
func (c *Config) CodeRoot() string {
	if c.Code.Root != "" {
		return c.Code.Root
	}

	return filepath.Join(filepath.Dir(c.Embedded.DataDir), "flint_code")
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

	if v := os.Getenv(EnvPrefix + "HOOKS_DIR"); v != "" {
		cfg.Embedded.HooksDir = v
	}

	if v := os.Getenv(EnvPrefix + "MIGRATIONS_DIR"); v != "" {
		cfg.Embedded.MigrationsDir = v
	}

	if v := os.Getenv(EnvPrefix + "JAVASCRIPT"); v != "" {
		cfg.Embedded.JavaScript = v == "1" || strings.EqualFold(v, "true")
	}

	if v := os.Getenv(EnvPrefix + "JS_RENDERER"); v != "" {
		cfg.JSRenderer = v == "1" || strings.EqualFold(v, "true")
	}

	if v := os.Getenv(EnvPrefix + "RENDERER_DIR"); v != "" {
		cfg.RendererDir = v
	}

	if v := os.Getenv(EnvPrefix + "CODE"); v != "" {
		cfg.Code.Enabled = parseBool(v)
	}

	if v := os.Getenv(EnvPrefix + "CODE_ROOT"); v != "" {
		cfg.Code.Root = v
	}

	if v := os.Getenv(EnvPrefix + "WIKIDATA"); v != "" {
		cfg.Wikidata.Enabled = parseBool(v)
	}

	if v := os.Getenv(EnvPrefix + "WIKIDATA_ENDPOINT"); v != "" {
		cfg.Wikidata.Endpoint = v
	}

	if v := os.Getenv(EnvPrefix + "WIKIDATA_DELAY_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Wikidata.DelayMS = n
		}
	}

	if v := os.Getenv(EnvPrefix + "WIKIDATA_SAMPLE_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Wikidata.SampleSize = n
		}
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

	if v := os.Getenv(EnvPrefix + "AGENT_PROVIDER"); v != "" {
		cfg.Agent.Provider = v
	}

	if v := os.Getenv(EnvPrefix + "AGENT_MODEL"); v != "" {
		cfg.Agent.Model = v
	}

	if v := os.Getenv(EnvPrefix + "AGENT_BASE_URL"); v != "" {
		cfg.Agent.BaseURL = v
	}

	if v := os.Getenv(EnvPrefix + "AGENT_API_KEY_ENV"); v != "" {
		cfg.Agent.APIKeyEnv = v
	}

	if v := os.Getenv(EnvPrefix + "AGENT_SYSTEM_PROMPT"); v != "" {
		cfg.Agent.SystemPrompt = v
	}

	if v := os.Getenv(EnvPrefix + "AGENT_PERMISSION_MODE"); v != "" {
		cfg.Agent.PermissionMode = v
	}

	if v := os.Getenv(EnvPrefix + "AGENT_MAX_STEPS"); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.Agent.MaxSteps = n
		}
	}

	if v := os.Getenv(EnvPrefix + "AGENT_CONTEXT_WINDOW"); v != "" {
		if n, err := parseInt(v); err == nil {
			cfg.Agent.ContextWindow = n
		}
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

	if flags.HooksDir != "" {
		cfg.Embedded.HooksDir = flags.HooksDir
	}

	if flags.MigrationsDir != "" {
		cfg.Embedded.MigrationsDir = flags.MigrationsDir
	}

	if flags.JavaScript {
		cfg.Embedded.JavaScript = true
	}

	if flags.JSRenderer {
		cfg.JSRenderer = true
	}

	if flags.RendererDir != "" {
		cfg.RendererDir = flags.RendererDir
	}

	if flags.CodeEnabled != nil {
		cfg.Code.Enabled = *flags.CodeEnabled
	}

	if flags.CodeDisabled != nil && *flags.CodeDisabled {
		cfg.Code.Enabled = false
	}

	if flags.CodeRoot != "" {
		cfg.Code.Root = flags.CodeRoot
	}

	if flags.WikidataEnabled != nil {
		cfg.Wikidata.Enabled = *flags.WikidataEnabled
	}

	if flags.WikidataEndpoint != "" {
		cfg.Wikidata.Endpoint = flags.WikidataEndpoint
	}

	if flags.WikidataDelayMS > 0 {
		cfg.Wikidata.DelayMS = flags.WikidataDelayMS
	}

	if flags.WikidataSampleSize > 0 {
		cfg.Wikidata.SampleSize = flags.WikidataSampleSize
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
