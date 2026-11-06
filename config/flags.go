package config

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
)

// Flags holds the command line flags of the application.
//
// The zero value means "not provided", so that the loader can apply the
// precedence rules correctly.
type Flags struct {
	// ConfigPath is the path of the TOML config file.
	ConfigPath string
	// Mode overrides the store mode.
	Mode string
	// Theme overrides the theme variant.
	Theme string
	// PerPage overrides the page size.
	PerPage int
	// LogLevel overrides the log verbosity.
	LogLevel string
	// MaxLocalRecords overrides the bound of the local JSON filter
	// evaluation.
	MaxLocalRecords int
	// DataDir overrides the embedded PocketBase data directory.
	DataDir string
	// EncryptionEnv overrides the settings encryption env var name.
	EncryptionEnv string
	// Dev enables the verbose PocketBase logs.
	Dev bool
	// HooksDir overrides the JavaScript application hooks directory.
	HooksDir string
	// MigrationsDir overrides the JavaScript migrations directory.
	MigrationsDir string
	// JavaScript enables the PocketBase JavaScriptVM plugin.
	JavaScript bool
	// JSRenderer enables the JavaScript record renderer.
	JSRenderer bool
	// RendererDir overrides the renderer script directory.
	RendererDir string
	// CodeEnabled turns the code tools on or off.
	//
	// It is a pointer because the default is on: a plain bool cannot
	// tell "not given" from "given as false", and the flag has to be
	// able to turn the tools off.
	CodeEnabled *bool
	// CodeDisabled turns the code tools off.
	//
	// It is the counterpart of [Flags.CodeEnabled]: the flag package
	// has no "--no-" prefix of its own, so the flag is registered
	// under its own name.
	CodeDisabled *bool
	// CodeRoot overrides the directory the code tools may read.
	CodeRoot string
	// WikidataEnabled turns the Wikidata tools on or off.
	//
	// It is a pointer because the default is off: a plain bool cannot
	// tell "not given" from "given as false", and the flag has to be
	// able to turn the tools on.
	WikidataEnabled *bool
	// WikidataEndpoint overrides the SPARQL endpoint of Wikidata.
	WikidataEndpoint string
	// WikidataDelayMS overrides the pause between two SPARQL queries,
	// in milliseconds.
	WikidataDelayMS int
	// WikidataSampleSize overrides the number of records a run asks
	// Wikidata about.
	WikidataSampleSize int
	// RemoteURL overrides the remote PocketBase server URL.
	RemoteURL string
	// RemoteToken overrides the remote API token.
	//
	// SECURITY: prefer the FLINT_REMOTE_TOKEN environment variable so
	// that the token does not end up in the shell history.
	RemoteToken string
	// RemoteCollections restricts the queryable remote collections.
	RemoteCollections []string
	// SyncCollections lists the collections synced by default.
	SyncCollections []string
	// SyncBatchSize is the number of records fetched per sync request.
	SyncBatchSize int
	// SyncMaxRecords caps the number of synced records per collection.
	SyncMaxRecords int
	// MigrateOnly asks the application to apply the migrations and exit
	// without starting the TUI.
	MigrateOnly bool
	// CheckAgent asks the application to check the agent configuration
	// and the API key, and to exit without starting the TUI.
	//
	// It is the diagnostic for the question that has no good answer
	// otherwise: why does the provider refuse the key? The answer is
	// the same for a key that is empty, one that is invalid, one that
	// belongs to the other platform and one that carries a stray
	// space -- the provider answers with a message about the
	// Authorization header, and the header is not the problem.
	CheckAgent bool
}

// ParseFlags parses the command line arguments.
//
// It returns the parsed flags and the remaining positional arguments.
func ParseFlags(args []string) (*Flags, []string, error) {
	fs := flag.NewFlagSet("flint", flag.ContinueOnError)

	var (
		flags       Flags
		remoteList  string
		syncList    string
		collections string
	)

	fs.StringVar(&flags.ConfigPath, "config", "", "path of the TOML config file")
	fs.StringVar(&flags.Mode, "mode", "", "store mode: embedded or remote")
	fs.StringVar(&flags.Theme, "theme", "", "theme variant: dark or light")
	fs.IntVar(&flags.PerPage, "per-page", 0, "number of records per page")
	fs.StringVar(&flags.LogLevel, "log-level", "", "log verbosity: debug, info, warn or error")
	fs.IntVar(&flags.MaxLocalRecords, "max-local-records", 0, "bound of the records fetched to evaluate a local JSON filter")
	fs.StringVar(&flags.DataDir, "data-dir", "", "embedded PocketBase data directory")
	fs.StringVar(&flags.EncryptionEnv, "encryption-env", "", "env variable holding the 32 char settings encryption key")
	fs.BoolVar(&flags.Dev, "dev", false, "enable the verbose PocketBase logs")
	fs.StringVar(&flags.HooksDir, "hooks-dir", "", "directory of the JavaScript application hooks")
	fs.StringVar(&flags.MigrationsDir, "migrations-dir", "", "directory of the JavaScript migrations")
	fs.BoolVar(&flags.JavaScript, "javascript", false, "enable the PocketBase JavaScriptVM plugin")
	fs.BoolVar(&flags.JSRenderer, "js-renderer", false, "enable the JavaScript record renderer")
	fs.StringVar(&flags.RendererDir, "renderer-dir", "", "directory of the JavaScript record renderer")
	fs.Var(newBoolValue(&flags.CodeEnabled), "code", "enable the code tools (they need gopls on the PATH)")
	fs.Var(newBoolValue(&flags.CodeDisabled), "no-code", "disable the code tools")
	fs.StringVar(&flags.CodeRoot, "code-root", "", "directory the code tools are allowed to read")
	fs.Var(newBoolValue(&flags.WikidataEnabled), "wikidata", "enable the Wikidata tools")
	fs.StringVar(&flags.WikidataEndpoint, "wikidata-endpoint", "", "SPARQL endpoint of the Wikidata tools")
	fs.IntVar(&flags.WikidataDelayMS, "wikidata-delay-ms", 0, "pause between two SPARQL queries, in milliseconds")
	fs.IntVar(&flags.WikidataSampleSize, "wikidata-sample-size", 0, "how many records a run asks Wikidata about")
	fs.StringVar(&flags.RemoteURL, "remote-url", "", "base URL of the remote PocketBase server")
	fs.StringVar(&flags.RemoteToken, "remote-token", "", "API token of the remote PocketBase server (prefer FLINT_REMOTE_TOKEN)")
	fs.StringVar(&remoteList, "remote-collections", "", "comma separated allowlist of the remote collections")
	fs.StringVar(&collections, "collections", "", "comma separated allowlist of the collections")
	fs.StringVar(&syncList, "sync-collections", "", "comma separated list of the collections to sync")
	fs.IntVar(&flags.SyncBatchSize, "sync-batch-size", 0, "number of records fetched per sync request")
	fs.IntVar(&flags.SyncMaxRecords, "sync-max-records", 0, "maximum number of records synced per collection")
	fs.BoolVar(&flags.MigrateOnly, "migrate-only", false, "apply the embedded migrations and exit without starting the TUI")
	fs.BoolVar(&flags.CheckAgent, "check-agent", false, "check the agent configuration and the API key, then exit")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "flint — a Bubble Tea + PocketBase TUI scaffold\n\n")
		fmt.Fprintf(fs.Output(), "Usage:\n  flint [flags]\n\n")
		fmt.Fprintf(fs.Output(), "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), "\nEnvironment variables (flags take precedence):\n")
		fmt.Fprintf(fs.Output(), "  %sMODE, %sTHEME, %sPER_PAGE, %sLOG_LEVEL\n", EnvPrefix, EnvPrefix, EnvPrefix, EnvPrefix)
		fmt.Fprintf(fs.Output(), "  %sDATA_DIR, %sENCRYPTION_ENV, %sDEV\n", EnvPrefix, EnvPrefix, EnvPrefix)
		fmt.Fprintf(fs.Output(), "  %sREMOTE_URL, %sREMOTE_TOKEN, %sREMOTE_COLLECTIONS, %sREMOTE_TIMEOUT\n", EnvPrefix, EnvPrefix, EnvPrefix, EnvPrefix)
		fmt.Fprintf(fs.Output(), "  %sSYNC_COLLECTIONS, %sSYNC_BATCH_SIZE, %sSYNC_MAX_RECORDS\n", EnvPrefix, EnvPrefix, EnvPrefix)
	}

	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}

	flags.RemoteCollections = parseList(remoteList)
	if collections != "" {
		// The generic --collections flag applies to both backends.
		flags.RemoteCollections = parseList(collections)
	}

	flags.SyncCollections = parseList(syncList)

	return &flags, fs.Args(), nil
}

// boolValue is a boolean flag that remembers whether it was given.
//
// The standard boolean flag of the flag package cannot tell "not given"
// from "given as false", and a setting whose default is on needs that
// difference: `--no-code` has to turn the tools off, and the absence
// of the flag has to leave them as they are.
type boolValue struct {
	// target is the pointer the value is written to.
	target **bool
}

// newBoolValue returns a flag value that writes to a pointer to a
// boolean.
func newBoolValue(target **bool) *boolValue {
	return &boolValue{target: target}
}

// Set implements [flag.Value].
func (b *boolValue) Set(s string) error {
	value, err := strconv.ParseBool(s)
	if err != nil {
		return fmt.Errorf("config: %q is not a boolean", s)
	}

	*b.target = &value

	return nil
}

// String implements [flag.Value].
//
// It renders the value that is set, and "true" when the flag was not
// given: the help text is the only place it is read.
func (b *boolValue) String() string {
	if b.target == nil || *b.target == nil {
		return "true"
	}

	return strconv.FormatBool(**b.target)
}

// IsBoolFlag implements [flag.boolFlag], the interface that tells the
// flag package that the flag may be given without a value.
//
// It is what makes `--no-code` work: the flag package takes the name
// after the dashes, and the value is the part that is left.
func (b *boolValue) IsBoolFlag() bool { return true }

// String renders the flags as a compact, log friendly string.
//
// The API token is never included.
func (f *Flags) String() string {
	parts := []string{}
	if f.Mode != "" {
		parts = append(parts, "mode="+f.Mode)
	}

	if f.Theme != "" {
		parts = append(parts, "theme="+f.Theme)
	}

	if f.DataDir != "" {
		parts = append(parts, "data-dir="+f.DataDir)
	}

	if f.RemoteURL != "" {
		parts = append(parts, "remote-url="+f.RemoteURL)
	}

	if len(f.RemoteCollections) > 0 {
		parts = append(parts, "collections="+strings.Join(f.RemoteCollections, ","))
	}

	return strings.Join(parts, " ")
}
