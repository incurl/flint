package config

import (
	"fmt"
	"strings"
)

// Render returns the configuration as a commented TOML document.
//
// The document is an **example**: every setting is there, with the
// value it holds, and the ones that matter carry a comment that says
// what they do. A user reads it, deletes the lines they do not need,
// and keeps the rest.
//
// The API key of the agent is not in it: the configuration names the
// environment variable that holds it.
func Render(cfg *Config) string {
	if cfg == nil {
		cfg = Default()
	}

	var b strings.Builder

	b.WriteString("# flint.toml — the configuration of the application.\n")
	b.WriteString("#\n")
	b.WriteString("# Every setting is shown with the value it holds. Delete the lines\n")
	b.WriteString("# you do not need: the default applies to the ones that are\n")
	b.WriteString("# missing.\n")
	b.WriteString("#\n")
	b.WriteString("# The precedence is: flags > environment variables > this file.\n")
	b.WriteString("\n")

	b.WriteString("# The store: \"embedded\" runs a local PocketBase, \"remote\" uses a\n")
	b.WriteString("# server over REST.\n")
	fmt.Fprintf(&b, "mode = %q\n", cfg.Mode)
	b.WriteString("\n")

	b.WriteString("# The theme: \"dark\" or \"light\".\n")
	fmt.Fprintf(&b, "theme = %q\n", cfg.Theme)
	b.WriteString("\n")

	b.WriteString("# The number of records on a page of the record list.\n")
	fmt.Fprintf(&b, "per_page = %d\n", cfg.PerPage)
	b.WriteString("\n")

	b.WriteString("# The log verbosity: \"debug\", \"info\", \"warn\" or \"error\".\n")
	fmt.Fprintf(&b, "log_level = %q\n", cfg.LogLevel)
	b.WriteString("\n")

	b.WriteString("# The bound of the local evaluation of a JSON filter. 0 uses the default.\n")
	fmt.Fprintf(&b, "max_local_records = %d\n", cfg.MaxLocalRecords)
	b.WriteString("\n")

	b.WriteString("# The JavaScript record renderer. It overrides how a record is\n")
	b.WriteString("# displayed.\n")
	fmt.Fprintf(&b, "js_renderer = %t\n", cfg.JSRenderer)
	b.WriteString("\n")

	b.WriteString("# The directory holding the renderer script.\n")
	fmt.Fprintf(&b, "renderer_dir = %q\n", cfg.RendererDir)
	b.WriteString("\n")

	b.WriteString("[embedded]\n")
	b.WriteString("# The PocketBase data directory: the SQLite database and the files.\n")
	fmt.Fprintf(&b, "data_dir = %q\n", cfg.Embedded.DataDir)
	b.WriteString("\n")
	b.WriteString("# The environment variable holding the 32 character key that\n")
	b.WriteString("# encrypts the PocketBase settings. Leave it empty to disable the\n")
	b.WriteString("# encryption. It does NOT encrypt the records.\n")
	fmt.Fprintf(&b, "encryption_env = %q\n", cfg.Embedded.EncryptionEnv)
	b.WriteString("\n")
	b.WriteString("# The verbose PocketBase logs. Keep it off in production.\n")
	fmt.Fprintf(&b, "dev = %t\n", cfg.Embedded.Dev)
	b.WriteString("\n")
	b.WriteString("# The PocketBase JavaScriptVM plugin: the JS hooks and migrations.\n")
	fmt.Fprintf(&b, "javascript = %t\n", cfg.Embedded.JavaScript)
	b.WriteString("\n")
	b.WriteString("# The directories holding the JavaScript hooks and migrations.\n")
	fmt.Fprintf(&b, "hooks_dir = %q\n", cfg.Embedded.HooksDir)
	fmt.Fprintf(&b, "migrations_dir = %q\n", cfg.Embedded.MigrationsDir)
	b.WriteString("\n")

	b.WriteString("[remote]\n")
	b.WriteString("# The base URL of the PocketBase server.\n")
	fmt.Fprintf(&b, "url = %q\n", cfg.Remote.URL)
	b.WriteString("\n")
	b.WriteString("# SECURITY: do not commit a token. Prefer the FLINT_REMOTE_TOKEN\n")
	b.WriteString("# environment variable.\n")
	fmt.Fprintf(&b, "token = %q\n", cfg.Remote.Token)
	b.WriteString("\n")
	b.WriteString("# The collections the application may query. An empty list allows\n")
	b.WriteString("# every collection.\n")
	fmt.Fprintf(&b, "collections = %s\n", formatStringList(cfg.Remote.Collections))
	b.WriteString("\n")
	b.WriteString("# The timeout of a single HTTP request, in seconds.\n")
	fmt.Fprintf(&b, "timeout = %d\n", cfg.Remote.Timeout)
	b.WriteString("\n")

	b.WriteString("[sync]\n")
	b.WriteString("# The collections synced into the local cache.\n")
	fmt.Fprintf(&b, "collections = %s\n", formatStringList(cfg.Sync.Collections))
	b.WriteString("\n")
	b.WriteString("# The number of records fetched per remote request.\n")
	fmt.Fprintf(&b, "batch_size = %d\n", cfg.Sync.BatchSize)
	b.WriteString("\n")
	b.WriteString("# The cap of the records synced per collection.\n")
	fmt.Fprintf(&b, "max_records = %d\n", cfg.Sync.MaxRecords)
	b.WriteString("\n")

	renderAgent(&b, cfg)

	return b.String()
}

// renderAgent writes the agent section.
//
// The section is written on its own because it is the one the guided
// setup fills in, and it is the one a user looks for.
func renderAgent(b *strings.Builder, cfg *Config) {
	b.WriteString("[agent]\n")
	b.WriteString("# The provider: \"minimax\", \"minimax-cn\", \"openai\",\n")
	b.WriteString("# \"openaicompat\", \"openrouter\" or \"local\".\n")
	fmt.Fprintf(b, "provider = %q\n", cfg.Agent.Provider)
	b.WriteString("\n")
	b.WriteString("# The model. The providers do not expose a models endpoint, so the\n")
	b.WriteString("# name is the one the platform documents.\n")
	fmt.Fprintf(b, "model = %q\n", cfg.Agent.Model)
	b.WriteString("\n")
	b.WriteString("# The endpoint of the provider. Leave it empty for the default of\n")
	b.WriteString("# the provider.\n")
	fmt.Fprintf(b, "base_url = %q\n", cfg.Agent.BaseURL)
	b.WriteString("\n")
	b.WriteString("# SECURITY: the API key is NEVER read from this file. This is the\n")
	b.WriteString("# name of the environment variable that holds it.\n")
	fmt.Fprintf(b, "api_key_env = %q\n", cfg.Agent.APIKeyEnv)
	b.WriteString("\n")
	b.WriteString("# The instruction given to the model. Leave it empty for the built-in\n")
	b.WriteString("# one.\n")
	fmt.Fprintf(b, "system_prompt = %q\n", cfg.Agent.SystemPrompt)
	b.WriteString("\n")
	b.WriteString("# The gate on the tools that change the data: \"ask\" or \"allow\".\n")
	b.WriteString("# Any other value falls back to \"ask\".\n")
	fmt.Fprintf(b, "permission_mode = %q\n", cfg.Agent.PermissionMode)
	b.WriteString("\n")
	b.WriteString("# The bound of the model calls of a single turn. 0 uses the default.\n")
	fmt.Fprintf(b, "max_steps = %d\n", cfg.Agent.MaxSteps)
}

// formatStringList renders a list of strings as a TOML array.
func formatStringList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}

	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%q", value))
	}

	return "[" + strings.Join(parts, ", ") + "]"
}
