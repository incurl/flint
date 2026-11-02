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
