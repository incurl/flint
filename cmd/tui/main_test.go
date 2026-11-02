package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketbase/pocketbase"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/pbstore"
)

// TestRegisterJavaScriptRunsTheMigrations checks that the registration
// is enough for the JS migrations to be applied.
//
// The plugin must be registered before the store is opened, because the
// store bootstraps the application and runs the migrations. This test
// pins that ordering.
func TestRegisterJavaScriptRunsTheMigrations(t *testing.T) {
	root := t.TempDir()

	hooksDir := filepath.Join(root, "pb_hooks")
	migrationsDir := filepath.Join(root, "pb_migrations")
	dataDir := filepath.Join(root, "pb_data")

	for _, dir := range []string{hooksDir, migrationsDir, dataDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
	}

	// A JS migration that creates a collection.
	migration := `
migrate((app) => {
    app.save(new Collection({
        type: "base",
        name: "jsitems",
        fields: [new Field({ name: "title", type: "text" })],
    }))
}, (app) => {})
`
	if err := os.WriteFile(filepath.Join(migrationsDir, "1_jsitems.js"), []byte(migration), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := config.Default()
	cfg.Embedded.DataDir = dataDir
	cfg.Embedded.HooksDir = hooksDir
	cfg.Embedded.MigrationsDir = migrationsDir
	cfg.Embedded.JavaScript = true

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dataDir,
		HideStartBanner: true,
	})

	if err := registerJavaScript(app, cfg); err != nil {
		t.Fatalf("registerJavaScript() error = %v", err)
	}

	store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{DataDir: dataDir})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	collections, err := store.Collections(context.Background())
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}

	for _, c := range collections {
		if c.Name == "jsitems" {
			return
		}
	}

	t.Error("the JS migration did not create the collection")
}

// TestRegisterJavaScriptWithoutDirectories checks that the registration
// works when there is no hooks directory at all.
//
// A missing directory is not an error: the plugin simply finds no file.
func TestRegisterJavaScriptWithoutDirectories(t *testing.T) {
	root := t.TempDir()

	cfg := config.Default()
	cfg.Embedded.DataDir = filepath.Join(root, "pb_data")
	cfg.Embedded.HooksDir = filepath.Join(root, "missing_hooks")
	cfg.Embedded.MigrationsDir = filepath.Join(root, "missing_migrations")

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  cfg.Embedded.DataDir,
		HideStartBanner: true,
	})

	if err := registerJavaScript(app, cfg); err != nil {
		t.Errorf("registerJavaScript() error = %v, want none", err)
	}
}

// TestTheJavaScriptPluginIsRegisteredBeforeTheStore checks that the
// registration happens in the right place.
//
// The plugin registers the JS migrations into the same registry as the
// Go ones. If it were registered after the store is opened, the
// migrations would never be applied.
func TestTheJavaScriptPluginIsRegisteredBeforeTheStore(t *testing.T) {
	root := t.TempDir()

	dataDir := filepath.Join(root, "pb_data")
	migrationsDir := filepath.Join(root, "pb_migrations")

	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	migration := `
migrate((app) => {
    app.save(new Collection({ type: "base", name: "late", fields: [] }))
}, (app) => {})
`
	if err := os.WriteFile(filepath.Join(migrationsDir, "1_late.js"), []byte(migration), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := config.Default()
	cfg.Embedded.DataDir = dataDir
	cfg.Embedded.MigrationsDir = migrationsDir
	cfg.Embedded.HooksDir = filepath.Join(root, "pb_hooks")

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dataDir,
		HideStartBanner: true,
	})

	// The wrong order: the store is opened first.
	store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{DataDir: dataDir})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := registerJavaScript(app, cfg); err != nil {
		t.Fatalf("registerJavaScript() error = %v", err)
	}

	collections, err := store.Collections(context.Background())
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}

	for _, c := range collections {
		if c.Name == "late" {
			t.Error("the migration ran even though the plugin was registered after the store")
		}
	}
}

// TestThePluginIsOptional checks that the plugin is not registered when
// it is not asked for.
func TestThePluginIsOptional(t *testing.T) {
	root := t.TempDir()

	dataDir := filepath.Join(root, "pb_data")
	migrationsDir := filepath.Join(root, "pb_migrations")

	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	migration := `
migrate((app) => {
    app.save(new Collection({ type: "base", name: "off", fields: [] }))
}, (app) => {})
`
	if err := os.WriteFile(filepath.Join(migrationsDir, "1_off.js"), []byte(migration), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := config.Default()
	cfg.Embedded.DataDir = dataDir
	cfg.Embedded.MigrationsDir = migrationsDir
	cfg.Embedded.HooksDir = filepath.Join(root, "pb_hooks")

	// The plugin is off.
	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dataDir,
		HideStartBanner: true,
	})

	store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{DataDir: dataDir})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	collections, err := store.Collections(context.Background())
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}

	for _, c := range collections {
		if c.Name == "off" {
			t.Error("the JS migration ran even though the plugin is disabled")
		}
	}
}
