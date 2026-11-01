package config

import (
	"flag"
	"fmt"
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
	// DataDir overrides the embedded PocketBase data directory.
	DataDir string
	// EncryptionEnv overrides the settings encryption env var name.
	EncryptionEnv string
	// Dev enables the verbose PocketBase logs.
	Dev bool
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
	fs.StringVar(&flags.DataDir, "data-dir", "", "embedded PocketBase data directory")
	fs.StringVar(&flags.EncryptionEnv, "encryption-env", "", "env variable holding the 32 char settings encryption key")
	fs.BoolVar(&flags.Dev, "dev", false, "enable the verbose PocketBase logs")
	fs.StringVar(&flags.RemoteURL, "remote-url", "", "base URL of the remote PocketBase server")
	fs.StringVar(&flags.RemoteToken, "remote-token", "", "API token of the remote PocketBase server (prefer FLINT_REMOTE_TOKEN)")
	fs.StringVar(&remoteList, "remote-collections", "", "comma separated allowlist of the remote collections")
	fs.StringVar(&collections, "collections", "", "comma separated allowlist of the collections")
	fs.StringVar(&syncList, "sync-collections", "", "comma separated list of the collections to sync")
	fs.IntVar(&flags.SyncBatchSize, "sync-batch-size", 0, "number of records fetched per sync request")
	fs.IntVar(&flags.SyncMaxRecords, "sync-max-records", 0, "maximum number of records synced per collection")

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
