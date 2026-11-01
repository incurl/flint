# Chapter 13 — Migrations

## 13.1 What a migration is

A migration is a versioned, reversible change to the database schema.

```go
type Migration struct {
    Up               func(txApp App) error
    Down             func(txApp App) error
    File             string
    ReapplyCondition func(txApp App, runner *MigrationsRunner, fileName string) (bool, error)
}
```

The `Up` function applies the change; the `Down` function reverts it.
The `File` name determines the *order* — the migrations are sorted by
filename.

## 13.2 The two lists

```go
var SystemMigrations MigrationsList   // PocketBase's own
var AppMigrations MigrationsList      // yours
```

The system migrations create the internal collections: `_collections`,
`_superusers`, `_externalAuths`, `_mfa`, `_otp`, `_authOrigins`.

The app migrations are yours.

```go
func (app *BaseApp) RunSystemMigrations() error {
    _, err := NewMigrationsRunner(app, SystemMigrations).Up()
    return err
}

func (app *BaseApp) RunAppMigrations() error {
    _, err := NewMigrationsRunner(app, AppMigrations).Up()
    return err
}

func (app *BaseApp) RunAllMigrations() error {
    list := MigrationsList{}
    list.Copy(SystemMigrations)
    list.Copy(AppMigrations)
    _, err := NewMigrationsRunner(app, list).Up()
    return err
}
```

⚠️ **`RunAllMigrations` copies both lists into one**, and the copy is
sorted by filename. So your migration filenames are interleaved with
the system ones. Since the system files use timestamps like
`1640988000_init.go`, your files should use a comparable scheme.

## 13.3 The filename convention

PocketBase uses a Unix timestamp as the filename prefix:

```
1640988000_init.go
1640988000_aux_init.go
1717233556_v0.23_migrate.go
1778828400_normalize_indexes.go
```

The number is the creation timestamp, and the string after it is a
description. The migrations are sorted lexicographically by filename,
so the timestamps give you a chronological order.

⚠️ **Use the same convention.** A migration named `001_create.go` would
sort *before* the system migrations, which means it would run before
`_collections` exists — and fail.

The template's example uses `1700000001_create_wikidata.go`.

⚠️ **Use a timestamp far in the future** if you want your migration to
run after a specific PocketBase version's migrations. The current date
is around `17xxxxxxxxxx` (October 2026), so `1700000000` is in the
past and `1799999999` would be safely in the future.

## 13.4 Writing a migration

```go
package myapp

import "github.com/pocketbase/pocketbase/core"

// Migrations returns the collections required by the application.
func Migrations() []core.Migration {
    return []core.Migration{
        {
            File: "1700000001_create_vocab.go",
            Up: func(txApp core.App) error {
                collection := core.NewBaseCollection("vocab")
                collection.Fields.Add(
                    &core.TextField{Name: "key", Required: true, Max: 200},
                    &core.TextField{Name: "label", Max: 500},
                    &core.JSONField{Name: "payload"},
                )
                return txApp.Save(collection)
            },
            Down: func(txApp core.App) error {
                collection, err := txApp.FindCollectionByNameOrId("vocab")
                if err != nil {
                    return nil
                }
                return txApp.Delete(collection)
            },
        },
    }
}
```

### The `txApp`

Both functions receive a `txApp`, not the app. It is a transactional
view:

- every write inside the migration is part of one transaction;
- if the migration fails, everything is rolled back.

⚠️ **Use `txApp`, not the outer `app`.** Using the outer app inside a
migration writes outside the transaction, so a failure leaves a
half-applied schema.

⚠️ **The `Down` function should be tolerant.** If the collection was
already deleted (by hand, or by a previous failed state), return `nil`
rather than an error:

```go
Down: func(txApp core.App) error {
    collection, err := txApp.FindCollectionByNameOrId("vocab")
    if err != nil {
        return nil    // already gone
    }
    return txApp.Delete(collection)
}
```

## 13.5 Registering the migrations

```go
// cmd/tui/main.go
func appMigrations() []core.Migration {
    return myapp.Migrations()
}
```

```go
store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{
    DataDir:    cfg.Embedded.DataDir,
    Migrations: appMigrations(),
})
```

And inside `OpenEmbedded`:

```go
// Register the user provided migrations before the app is bootstrapped
// so that they are applied together with the system migrations.
for _, m := range config.Migrations {
    core.AppMigrations.Add(&m)
}
```

⚠️ **`core.AppMigrations` is a package-level global.** Registering the
same migration twice (for example, by calling `OpenEmbedded` twice in
one process) would add it twice.

⚠️ **The loop variable address.** `for _, m := range config.Migrations`
creates a copy of each migration, and `&m` is the address of that copy.
In Go 1.22+, the loop variable is per-iteration, so this is safe. In
older Go versions it was a bug. Since the template requires Go 1.27,
you are fine.

## 13.6 The migrations runner

The runner tracks which migrations have been applied, in a table
(`_migrations`):

```go
runner := core.NewMigrationsRunner(app, list)

// Apply the pending migrations.
applied, err := runner.Up()

// Revert the last N.
reverted, err := runner.Down(1)
```

⚠️ **The runner tracks the applied state in the database**, so it is
per-data-directory. Two different data directories have independent
migration histories.

## 13.7 The `ReapplyCondition`

Some migrations should be re-run even if they were applied before. This
is how PocketBase handles the "normalise indexes" migration, which
must run after every schema change.

```go
{
    File: "1778828400_normalize_indexes.go",
    ReapplyCondition: func(txApp core.App, runner *core.MigrationsRunner, fileName string) (bool, error) {
        return true, nil    // always reapply
    },
    Up:   ...,
    Down: ...,
}
```

⚠️ **Use this sparingly.** A migration that always reapplies must be
idempotent, or it will corrupt the data on the second run.

## 13.8 The alternative: `EnsureDictionaryCollection`

The template offers a second way to create a collection, at runtime,
rather than through a migration.

```go
// EnsureDictionaryCollection implements [Store].
//
// The collection is created with the native PocketBase collection model, so
// it behaves like any hand-crafted collection created through the admin UI:
// it has system fields, indexes and validation.
func (s *Embedded) EnsureDictionaryCollection(ctx context.Context, name string, fields []DictionaryField) (Collection, error) {
    // Reuse the existing collection when it is already there.
    if existing, err := s.app.FindCachedCollectionByNameOrId(name); err == nil && existing != nil {
        return Collection{ID: existing.Id, Name: existing.Name, Type: existing.Type}, nil
    }

    collection := core.NewBaseCollection(name)
    // ...
    if err := s.app.Save(collection); err != nil {
        return Collection{}, fmt.Errorf("pbstore: create dictionary collection %q: %w", name, err)
    }
    return Collection{ID: collection.Id, Name: collection.Name, Type: collection.Type}, nil
}
```

### When to use which

| Approach                        | Use when                                              |
| ------------------------------- | ----------------------------------------------------- |
| **Migration**                   | The schema is part of your application's contract      |
| **`EnsureDictionaryCollection`** | The collection is a runtime convenience (a cache, a user-created vocabulary) |

The distinction is about **ownership**:

- A migration says "this application *has* this collection". It is
  versioned, reversible, and reviewed.
- `EnsureDictionaryCollection` says "this application *uses* a
  collection with this shape, if it is not there". It is idempotent and
  self-healing.

⚠️ **Do not use `EnsureDictionaryCollection` for your core schema.**
If you later need to change the schema, you have no migration to
change it with. Use it for the things that are genuinely optional.

⚠️ **The template uses it for the sync destination**, because the
collection being synced into is determined by the *user* at runtime:

```go
// The local collection is created on the fly when it does not exist
// yet: the whole point of the sync is to populate the local cache.
if _, err := s.app.FindCachedCollectionByNameOrId(local); err != nil {
    if _, err := s.EnsureDictionaryCollection(ctx, local, nil); err != nil {
        return nil, fmt.Errorf("pbstore: create the local collection %q: %w", local, err)
    }
}
```

## 13.9 A migration that adds a field

```go
{
    File: "1700000002_add_vocab_notes.go",
    Up: func(txApp core.App) error {
        collection, err := txApp.FindCollectionByNameOrId("vocab")
        if err != nil {
            return err
        }
        collection.Fields.Add(&core.TextField{Name: "notes", Max: 2000})
        return txApp.Save(collection)
    },
    Down: func(txApp core.App) error {
        collection, err := txApp.FindCollectionByNameOrId("vocab")
        if err != nil {
            return nil
        }
        collection.Fields.RemoveByName("notes")
        return txApp.Save(collection)
    },
}
```

⚠️ **Adding a field with `Required: true` to a table with existing
rows requires a table rebuild.** PocketBase does it for you, but it
takes an exclusive lock on the table. For a large table, this is a
noticeable pause.

⚠️ **Removing a field drops its data.** The `Down` migration is
therefore not a perfect inverse. That is normal and expected.

## 13.10 A migration that backfills data

```go
{
    File: "1700000003_backfill_vocab_keys.go",
    Up: func(txApp core.App) error {
        records, err := txApp.FindRecordsByFilter("vocab", `key = ""`, "", 0, 0)
        if err != nil {
            return err
        }

        for _, record := range records {
            label := record.GetString("label")
            record.Set("key", strings.ToLower(strings.ReplaceAll(label, " ", "_")))
            if err := txApp.Save(record); err != nil {
                return err
            }
        }
        return nil
    },
    Down: func(txApp core.App) error {
        return nil    // the backfill is not reversible
    },
}
```

⚠️ **A data migration is not reversible in general.** It is fine for
the `Down` to be a no-op, but be aware that a rollback does not undo
the data change.

⚠️ **A migration that reads and writes many records can be slow.** It
runs inside a transaction, so it holds the write lock for the whole
duration. For a large backfill, consider doing it in batches outside
the migration.

## 13.11 Testing the migrations

The template's test does exactly what you should do:

```go
func TestEmbeddedMigrations(t *testing.T) {
    dir := filepath.Join(t.TempDir(), "pb_data")

    app := pocketbase.NewWithConfig(pocketbase.Config{
        DefaultDataDir:  dir,
        HideStartBanner: true,
    })

    store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{
        DataDir: dir,
        Migrations: []core.Migration{{
            File: "9999999999_test_collection.go",
            Up: func(txApp core.App) error {
                collection := core.NewBaseCollection("migrated")
                collection.Fields.Add(&core.TextField{Name: "value"})
                return txApp.Save(collection)
            },
            Down: func(txApp core.App) error { return nil },
        }}},
    })
    if err != nil {
        t.Fatalf("OpenEmbedded() error = %v", err)
    }
    defer func() { _ = store.Close() }()

    collections, err := store.Collections(context.Background())
    if err != nil {
        t.Fatalf("Collections() error = %v", err)
    }

    found := false
    for _, c := range collections {
        if c.Name == "migrated" {
            found = true
            break
        }
    }
    if !found {
        t.Error("the migrated collection was not created")
    }
}
```

The pattern:

1. a **temporary data directory**, so the test is hermetic;
2. the migrations passed **in the config**, so the test does not
   depend on the global `core.AppMigrations`;
3. the assertion on the **result**, not on the implementation.

⚠️ **Use `t.TempDir()`,** never a fixed path. A fixed path makes the
test fail the second time (the migration is already applied) and makes
parallel runs interfere.

## 13.12 The `pb-migrate` target

The Makefile has a target for the migrations:

```make
.PHONY: pb-migrate
pb-migrate: ## Apply the PocketBase migrations to the embedded database
	$(GO) run $(CMD) --mode embedded --data-dir $(DATA_DIR) --migrate-only
	@echo "the migrations have been applied to $(DATA_DIR)"
```

⚠️ **Do not fake this with `--help`.** An earlier version of the template
ran `$(CMD) --help`, which prints the usage and exits without ever
touching the database. The target looked like it worked and silently
migrated nothing. A Makefile target that does not do what it says is worse
than no target at all.

The flag it relies on is a small addition to the command line:

```go
// config/flags.go
fs.BoolVar(&flags.MigrateOnly, "migrate-only", false,
    "apply the embedded migrations and exit without starting the TUI")
```

And the entry point honours it:

```go
// cmd/tui/main.go
store, err := openStore(cfg)
if err != nil {
    return err
}
defer func() { _ = store.Close() }()

// The store has already run the migrations while it was opened, so
// opening it is enough to migrate.
if flags.MigrateOnly {
    return nil
}
```

⚠️ **But note the trade-off.** If the migrations run *only* via the
flag, a user who runs the app without migrating gets a broken schema.
Running them automatically on startup is the safer default for a
single-user tool. Many projects do **both**: the automatic run is
idempotent, and the flag exists for CI. The template does exactly this —
`OpenEmbedded` always calls `RunAllMigrations`, and `--migrate-only`
simply skips the rest of the startup.

## 13.13 Exercises

1. Write a migration that creates a collection with a relation field,
   and another that adds the relation once both collections exist.

2. Write a migration whose `Down` is not the exact inverse of `Up`
   (a data backfill). Document why.

3. Write a test that runs the same migrations twice and verifies that
   the second run is a no-op.

4. Write a migration that fails halfway (return an error after adding
   one field). Verify that the schema is unchanged — the transaction
   rolled back.

5. Add a `--migrate-only` flag that migrates and exits, and wire it to a
   `make pb-migrate` target. Verify that it is idempotent: running it
   twice must leave the schema unchanged.

6. Measure how long a migration that adds a `Required` field takes on
   a table with 100,000 rows.

7. Take the template's `EnsureDictionaryCollection` and add a
   migration-based equivalent. Compare the two approaches in terms of
   what happens when the schema needs to change later.

## 13.14 What you learned

- A migration is an **up/down pair**, ordered by its **filename**.
- The filenames use a **Unix timestamp prefix**, and your migrations
  must sort *after* the system ones.
- There are **two lists**: `SystemMigrations` and `AppMigrations`;
  `RunAllMigrations` merges and sorts them.
- The migration functions receive a **transactional `txApp`** — using
  the outer app defeats the transaction.
- `core.AppMigrations` is a **global**: do not register the same
  migration twice.
- `ReapplyCondition` makes a migration re-run every time; use it
  sparingly.
- **Migrations for your contract**, `EnsureDictionaryCollection` for
  runtime conveniences.
- A **data migration is not reversible**; that is fine.
- Test the migrations with a **temporary data directory**.

## 13.15 Further reading

- `core/migrations_list.go` — the `Migration` and `MigrationsList`
  types.
- `core/migrations_runner.go` — the runner.
- `migrations/` — the system migrations, as examples.
- `examples/wikidata/migrations.go` — the template's example.
- `internal/pbstore/embedded_test.go` — the migration test.

Next: [Chapter 14 — Embedded or Server: One Library, Two Deployments](ch14-embedded-vs-server.md).
