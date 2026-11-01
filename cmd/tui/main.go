// Command tui is the entry point of the Flint TUI.
//
// It wires the configuration, the PocketBase store and the root Bubble Tea
// model together, and takes care of the graceful shutdown.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/app"
	"github.com/incurl/flint/internal/pbstore"
)

func main() {
	if err := run(); err != nil {
		// A killed program and an interrupted one (Ctrl+C) are normal
		// shutdown paths, not failures: report them silently and exit
		// with a zero status.
		if isBenignShutdown(err) {
			return
		}

		fmt.Fprintf(os.Stderr, "flint: %v\n", err)
		os.Exit(1)
	}
}

// isBenignShutdown reports whether the error is one of the expected
// shutdown outcomes of a Bubble Tea program.
//
// Bubble Tea returns [tea.ErrInterrupted] when the program receives a
// SIGINT (the usual Ctrl+C) and [tea.ErrProgramKilled] when it is killed.
// Both are normal shutdown paths and must not produce a non-zero exit
// status.
func isBenignShutdown(err error) bool {
	return errors.Is(err, tea.ErrInterrupted) ||
		errors.Is(err, tea.ErrProgramKilled)
}

// run starts the application and blocks until it exits.
func run() error {
	flags, args, err := config.ParseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	if len(args) > 0 {
		return fmt.Errorf("unexpected argument %q", args[0])
	}

	cfg, err := config.Load(flags.ConfigPath, *flags)
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	store, err := openStore(cfg)
	if err != nil {
		return err
	}

	// Graceful shutdown: the store is closed once the TUI exits.
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error("failed to close the store", "error", err)
		}
	}()

	// The migrate-only mode applies the migrations and exits: the store
	// has already run them while it was opened.
	if flags.MigrateOnly {
		logger.Info("the migrations have been applied", "data_dir", cfg.Embedded.DataDir)

		return nil
	}

	model := app.New(store, cfg, nil)

	// In Bubble Tea v2 the alternate screen and the mouse mode are
	// properties of the rendered view, not of the program: the root
	// model sets them on every frame.
	program := tea.NewProgram(model)

	// Bubble Tea installs its own SIGINT and SIGTERM handler: SIGINT
	// interrupts the program (tea.ErrInterrupted) and SIGTERM asks for a
	// graceful shutdown. Both return from Program.Run, so the deferred
	// store close below always runs.
	if _, err := program.Run(); err != nil {
		return err
	}

	return nil
}

// openStore creates the store described by the configuration.
//
// The store is wrapped in a [pbstore.Hybrid], which evaluates the JSON
// filters that the native PocketBase grammar cannot express, such as
// the ones that reach into an array.
func openStore(cfg *config.Config) (pbstore.Store, error) {
	store, err := newBackendStore(cfg)
	if err != nil {
		return nil, err
	}

	return pbstore.NewHybrid(store, pbstore.HybridConfig{
		MaxLocalRecords: cfg.MaxLocalRecords,
	}), nil
}

// newBackendStore creates the backend described by the configuration.
func newBackendStore(cfg *config.Config) (pbstore.Store, error) {
	switch pbstore.Mode(cfg.Mode) {
	case pbstore.ModeRemote:
		return pbstore.OpenRemote(pbstore.RemoteConfig{
			BaseURL:       cfg.Remote.URL,
			Token:         cfg.Remote.Token,
			Collections:   cfg.Remote.Collections,
			Timeout:       cfg.RemoteTimeout(),
			RetryAttempts: 2,
		})

	case pbstore.ModeEmbedded:
		pb := pocketbase.NewWithConfig(pocketbase.Config{
			DefaultDataDir:       cfg.Embedded.DataDir,
			DefaultEncryptionEnv: cfg.Embedded.EncryptionEnv,
			DefaultDev:           cfg.Embedded.Dev,
			HideStartBanner:      true,
		})

		return pbstore.OpenEmbedded(pb, pbstore.EmbeddedConfig{
			DataDir:         cfg.Embedded.DataDir,
			EncryptionEnv:   cfg.Embedded.EncryptionEnv,
			Dev:             cfg.Embedded.Dev,
			Collections:     cfg.Remote.Collections,
			SyncRemoteURL:   cfg.Remote.URL,
			SyncRemoteToken: cfg.Remote.Token,
			Migrations:      appMigrations(),
		})

	default:
		return nil, fmt.Errorf("unsupported mode %q", cfg.Mode)
	}
}

// appMigrations returns the application migrations executed on startup.
//
// Downstream projects add their own dictionary/reference collection
// migrations here.
func appMigrations() []core.Migration {
	return nil
}

// newLogger creates the structured logger.
//
// The logs are written to the stderr so that they never corrupt the TUI
// rendering.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level

	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: lvl,
	}))
}
