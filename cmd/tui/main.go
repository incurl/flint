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
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/app"
	"github.com/incurl/flint/internal/pbstore"
)

func main() {
	if err := run(); err != nil {
		if !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, errInterrupted) {
			fmt.Fprintf(os.Stderr, "flint: %v\n", err)
			os.Exit(1)
		}
	}
}

// errInterrupted is returned when the process is interrupted.
var errInterrupted = errors.New("interrupted")

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

	model := app.New(store, cfg, nil)

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// The signal handler cancels the program so that the deferred store
	// close always runs.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		program.Quit()
	}()

	if _, err := program.Run(); err != nil {
		return err
	}
	return nil
}

// openStore creates the store described by the configuration.
func openStore(cfg *config.Config) (pbstore.Store, error) {
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
			DataDir:       cfg.Embedded.DataDir,
			EncryptionEnv: cfg.Embedded.EncryptionEnv,
			Dev:           cfg.Embedded.Dev,
			Collections:   cfg.Remote.Collections,
			SyncRemoteURL: cfg.Remote.URL,
			Migrations:    appMigrations(),
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
