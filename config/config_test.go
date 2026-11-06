package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/pbstore"
)

// TestLoadDefaults verifies the built-in defaults.
func TestLoadDefaults(t *testing.T) {
	// A non existing explicit path must be rejected.
	if _, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"), config.Flags{}); err == nil {
		t.Error("Load() with a missing file returned no error")
	}

	cfg, err := config.Load("", config.Flags{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Mode != config.DefaultMode {
		t.Errorf("Mode = %q, want %q", cfg.Mode, config.DefaultMode)
	}

	if cfg.PerPage != config.DefaultPerPage {
		t.Errorf("PerPage = %d, want %d", cfg.PerPage, config.DefaultPerPage)
	}
}

// TestLoadPrecedence verifies that the flags win over the env vars, which
// win over the config file.
func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flint.toml")

	content := []byte(`
mode = "remote"
theme = "light"
per_page = 11

[remote]
url = "http://from-file:8090"
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Config file only.
	cfg, err := config.Load(path, config.Flags{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Mode != "remote" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "remote")
	}

	if cfg.Theme != "light" {
		t.Errorf("Theme = %q, want %q", cfg.Theme, "light")
	}

	if cfg.PerPage != 11 {
		t.Errorf("PerPage = %d, want 11", cfg.PerPage)
	}

	if cfg.Remote.URL != "http://from-file:8090" {
		t.Errorf("Remote.URL = %q, want %q", cfg.Remote.URL, "http://from-file:8090")
	}

	// The env vars win over the config file.
	t.Setenv(config.EnvPrefix+"MODE", "embedded")
	t.Setenv(config.EnvPrefix+"THEME", "dark")
	t.Setenv(config.EnvPrefix+"PER_PAGE", "22")

	cfg, err = config.Load(path, config.Flags{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Mode != "embedded" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "embedded")
	}

	if cfg.Theme != "dark" {
		t.Errorf("Theme = %q, want %q", cfg.Theme, "dark")
	}

	if cfg.PerPage != 22 {
		t.Errorf("PerPage = %d, want 22", cfg.PerPage)
	}

	// The flags win over everything.
	cfg, err = config.Load(path, config.Flags{
		Mode:    "remote",
		Theme:   "light",
		PerPage: 33,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Mode != "remote" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "remote")
	}

	if cfg.Theme != "light" {
		t.Errorf("Theme = %q, want %q", cfg.Theme, "light")
	}

	if cfg.PerPage != 33 {
		t.Errorf("PerPage = %d, want 33", cfg.PerPage)
	}
}

// TestLoadEnvCollections verifies the list parsing of the env vars.
func TestLoadEnvCollections(t *testing.T) {
	t.Setenv(config.EnvPrefix+"REMOTE_COLLECTIONS", "a, b ,c")
	t.Setenv(config.EnvPrefix+"SYNC_COLLECTIONS", "x,y")

	cfg, err := config.Load("", config.Flags{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Remote.Collections) != 3 {
		t.Fatalf("Remote.Collections = %v, want 3 entries", cfg.Remote.Collections)
	}

	if cfg.Remote.Collections[1] != "b" {
		t.Errorf("Remote.Collections[1] = %q, want %q", cfg.Remote.Collections[1], "b")
	}

	if len(cfg.Sync.Collections) != 2 {
		t.Errorf("Sync.Collections = %v, want 2 entries", cfg.Sync.Collections)
	}
}

// TestValidate verifies the configuration validation.
func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr bool
	}{
		{
			name:   "valid embedded",
			mutate: func(c *config.Config) {},
		},
		{
			name: "invalid mode",
			mutate: func(c *config.Config) {
				c.Mode = "nope"
			},
			wantErr: true,
		},
		{
			name: "invalid theme",
			mutate: func(c *config.Config) {
				c.Theme = "nope"
			},
			wantErr: true,
		},
		{
			name: "invalid per page",
			mutate: func(c *config.Config) {
				c.PerPage = 0
			},
			wantErr: true,
		},
		{
			name: "remote without url",
			mutate: func(c *config.Config) {
				c.Mode = string(pbstore.ModeRemote)
				c.Remote.URL = ""
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			tt.mutate(cfg)

			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("Validate() returned no error")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("Validate() error = %v", err)
			}
		})
	}
}

// TestParseFlags verifies the command line parsing.
func TestParseFlags(t *testing.T) {
	flags, args, err := config.ParseFlags([]string{
		"--mode", "remote",
		"--remote-url", "http://example.com:8090",
		"--collections", "a,b",
		"--per-page", "5",
	})
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}

	if len(args) != 0 {
		t.Errorf("ParseFlags() returned %d positional arguments, want 0", len(args))
	}

	if flags.Mode != "remote" {
		t.Errorf("Mode = %q, want %q", flags.Mode, "remote")
	}

	if flags.RemoteURL != "http://example.com:8090" {
		t.Errorf("RemoteURL = %q", flags.RemoteURL)
	}

	if len(flags.RemoteCollections) != 2 {
		t.Errorf("RemoteCollections = %v, want 2 entries", flags.RemoteCollections)
	}

	if flags.PerPage != 5 {
		t.Errorf("PerPage = %d, want 5", flags.PerPage)
	}
}

// TestSettingsRows verifies that the settings rows are rendered.
func TestSettingsRows(t *testing.T) {
	cfg := config.Default()

	rows := cfg.SettingsRows()
	if len(rows) == 0 {
		t.Fatal("SettingsRows() returned no rows")
	}

	found := false

	for _, row := range rows {
		if row[0] == "mode" && row[1] == cfg.Mode {
			found = true
			break
		}
	}

	if !found {
		t.Error("SettingsRows() did not contain the mode row")
	}
}

// TestJavaScriptDefaults verifies the JavaScript defaults.
//
// The plugin is disabled by default: it embeds a JavaScript runtime and
// adds about 10 MB to the binary, so it has to be an explicit choice.
func TestJavaScriptDefaults(t *testing.T) {
	cfg, err := config.Load("", config.Flags{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Embedded.JavaScript {
		t.Error("Embedded.JavaScript = true, want false by default")
	}

	// The directories follow the PocketBase convention: they sit next
	// to the data directory.
	wantHooks := filepath.Join(filepath.Dir(cfg.Embedded.DataDir), "pb_hooks")
	if got := cfg.HooksDir(); got != wantHooks {
		t.Errorf("HooksDir() = %q, want %q", got, wantHooks)
	}

	wantMigrations := filepath.Join(filepath.Dir(cfg.Embedded.DataDir), "pb_migrations")
	if got := cfg.MigrationsDir(); got != wantMigrations {
		t.Errorf("MigrationsDir() = %q, want %q", got, wantMigrations)
	}
}

// TestJavaScriptDirectoriesAreOverridable verifies that the hooks and
// migrations directories can be set explicitly.
func TestJavaScriptDirectoriesAreOverridable(t *testing.T) {
	cfg := config.Default()
	cfg.Embedded.DataDir = "/data/pb_data"
	cfg.Embedded.HooksDir = "/custom/hooks"
	cfg.Embedded.MigrationsDir = "/custom/migrations"

	if got := cfg.HooksDir(); got != "/custom/hooks" {
		t.Errorf("HooksDir() = %q, want %q", got, "/custom/hooks")
	}

	if got := cfg.MigrationsDir(); got != "/custom/migrations" {
		t.Errorf("MigrationsDir() = %q, want %q", got, "/custom/migrations")
	}
}

// TestJavaScriptFlag verifies that the flag enables the plugin.
func TestJavaScriptFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flint.toml")

	content := []byte(`
mode = "embedded"

[embedded]
data_dir = "/data/pb_data"
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := config.Load(path, config.Flags{JavaScript: true})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Embedded.JavaScript {
		t.Error("Embedded.JavaScript = false, want true")
	}

	// The flag must not be lost when the directories are resolved.
	if got := cfg.HooksDir(); got == "" {
		t.Error("HooksDir() is empty")
	}
}

// TestJavaScriptEnv verifies that the environment variable enables the
// plugin.
func TestJavaScriptEnv(t *testing.T) {
	t.Setenv("FLINT_JAVASCRIPT", "true")

	cfg, err := config.Load("", config.Flags{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Embedded.JavaScript {
		t.Error("Embedded.JavaScript = false, want true")
	}
}

// TestJSRendererDefaults verifies the JavaScript renderer defaults.
func TestJSRendererDefaults(t *testing.T) {
	cfg, err := config.Load("", config.Flags{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.JSRenderer {
		t.Error("JSRenderer = true, want false by default")
	}

	// The directory sits next to the data directory.
	want := filepath.Join(filepath.Dir(cfg.Embedded.DataDir), "flint_hooks")
	if got := cfg.RendererDirPath(); got != want {
		t.Errorf("RendererDirPath() = %q, want %q", got, want)
	}
}

// TestJSRendererOverrides verifies the flag, the env var and the
// explicit directory.
func TestJSRendererOverrides(t *testing.T) {
	t.Run("flag", func(t *testing.T) {
		cfg, err := config.Load("", config.Flags{JSRenderer: true, RendererDir: "/custom"})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if !cfg.JSRenderer {
			t.Error("JSRenderer = false, want true")
		}

		if got := cfg.RendererDirPath(); got != "/custom" {
			t.Errorf("RendererDirPath() = %q, want /custom", got)
		}
	})

	t.Run("env", func(t *testing.T) {
		t.Setenv("FLINT_JS_RENDERER", "1")
		t.Setenv("FLINT_RENDERER_DIR", "/from-env")

		cfg, err := config.Load("", config.Flags{})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if !cfg.JSRenderer {
			t.Error("JSRenderer = false, want true")
		}

		if got := cfg.RendererDirPath(); got != "/from-env" {
			t.Errorf("RendererDirPath() = %q, want /from-env", got)
		}
	})
}

// TestCodeConfig verifies the code settings: the default directory, the
// default being on, and the three ways to change it.
func TestCodeConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg, err := config.Load("", config.Flags{})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		// The tools are on: the language server is started lazily, so
		// an agent that never asks about the code never pays for it.
		if !cfg.Code.Enabled {
			t.Error("Code.Enabled = false, want true by default")
		}

		// The directory sits next to the data directory.
		want := filepath.Join(filepath.Dir(cfg.Embedded.DataDir), "flint_code")
		if got := cfg.CodeRoot(); got != want {
			t.Errorf("CodeRoot() = %q, want %q", got, want)
		}
	})

	t.Run("root", func(t *testing.T) {
		cfg, err := config.Load("", config.Flags{CodeRoot: "/custom"})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if got := cfg.CodeRoot(); got != "/custom" {
			t.Errorf("CodeRoot() = %q, want /custom", got)
		}
	})

	t.Run("env turns them off", func(t *testing.T) {
		t.Setenv("FLINT_CODE", "0")

		cfg, err := config.Load("", config.Flags{})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if cfg.Code.Enabled {
			t.Error("Code.Enabled = true, want false")
		}
	})

	t.Run("file turns them off", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "flint.toml")

		if err := os.WriteFile(path, []byte("[code]\nenabled = false\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		cfg, err := config.Load(path, config.Flags{})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if cfg.Code.Enabled {
			t.Error("Code.Enabled = true, want false")
		}
	})

	t.Run("the flag wins over the env", func(t *testing.T) {
		t.Setenv("FLINT_CODE", "0")

		flags, _, err := config.ParseFlags([]string{"--code"})
		if err != nil {
			t.Fatalf("ParseFlags() error = %v", err)
		}

		cfg, err := config.Load("", *flags)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if !cfg.Code.Enabled {
			t.Error("Code.Enabled = false, want true: the flag beats the env")
		}
	})

	t.Run("no-code turns them off", func(t *testing.T) {
		flags, _, err := config.ParseFlags([]string{"--no-code"})
		if err != nil {
			t.Fatalf("ParseFlags() error = %v", err)
		}

		cfg, err := config.Load("", *flags)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if cfg.Code.Enabled {
			t.Error("Code.Enabled = true, want false")
		}
	})
}

// TestSettingsRowsShowTheEndpoint verifies that the endpoint of the
// agent is on the settings page.
//
// The endpoint is the setting that is invisible and decisive: a
// provider that reaches the wrong endpoint does not fail with "unknown
// provider", it fails with whatever the endpoint says — for MiniMax,
// "insufficient balance" — and the user has no way to tell that the
// endpoint is the problem.
func TestSettingsRowsShowTheEndpoint(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.Provider = "minimax"
	cfg.Agent.Model = "MiniMax-M3.1-Flash"
	cfg.Agent.BaseURL = "https://api.minimax.io/v1"

	var found bool

	for _, row := range cfg.SettingsRows() {
		if row[0] == "agent.base_url" {
			found = true

			if row[1] != "https://api.minimax.io/v1" {
				t.Errorf("agent.base_url = %q, want the endpoint", row[1])
			}
		}
	}

	if !found {
		t.Error("the settings page does not show the endpoint of the agent")
	}
}

// TestTheWikidataSettingsAreLoaded checks that the Wikidata settings are
// read from the environment.
//
// The tools reach a public endpoint, so the settings that decide whether
// they run are settings the user has to be able to set.
func TestTheWikidataSettingsAreLoaded(t *testing.T) {
	t.Setenv("FLINT_WIKIDATA", "1")
	t.Setenv("FLINT_WIKIDATA_ENDPOINT", "http://127.0.0.1:9999/sparql")
	t.Setenv("FLINT_WIKIDATA_DELAY_MS", "250")
	t.Setenv("FLINT_WIKIDATA_SAMPLE_SIZE", "7")

	cfg, err := config.Load("", config.Flags{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Wikidata.Enabled {
		t.Error("the Wikidata tools are not enabled")
	}

	if cfg.Wikidata.Endpoint != "http://127.0.0.1:9999/sparql" {
		t.Errorf("the endpoint is %q", cfg.Wikidata.Endpoint)
	}

	if cfg.Wikidata.DelayMS != 250 {
		t.Errorf("the delay is %d, want 250", cfg.Wikidata.DelayMS)
	}

	if cfg.Wikidata.SampleSize != 7 {
		t.Errorf("the sample size is %d, want 7", cfg.Wikidata.SampleSize)
	}
}

// TestTheWikidataToolsAreOffByDefault checks that the tools are off
// unless the configuration turns them on.
//
// They reach a public endpoint, and an application that never asks about
// Wikidata must not open a connection to it.
func TestTheWikidataToolsAreOffByDefault(t *testing.T) {
	cfg := config.Default()

	if cfg.Wikidata.Enabled {
		t.Error("the Wikidata tools are on by default")
	}

	if cfg.Wikidata.Endpoint == "" {
		t.Error("the endpoint has no default")
	}

	if cfg.Wikidata.DelayMS <= 0 {
		t.Error("the delay has no default")
	}
}
