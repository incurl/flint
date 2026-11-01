# Skill: add a collection

**Goal:** create a new PocketBase collection with a migration, and make
it usable from the TUI.

**Time:** ~15 minutes. Touches 2–3 files.

---

## The two ways to create a collection

| Method                        | Use when                                                |
| ----------------------------- | ------------------------------------------------------- |
| **Migration** (`core.Migration`) | The collection is part of your schema. It must exist for the app to work. |
| **`EnsureDictionaryCollection`** | The collection is a lookup/vocabulary cache that should be created on demand. |

**Prefer a migration** for anything your application reads or writes. It
is versioned, transactional and reviewable. Use `EnsureDictionaryCollection`
only for caches that can be rebuilt at any time.

---

## Step 1 — write the migration

Create `internal/pbstore/migrations/<name>.go`, or put it in your own
domain package. The `examples/wikidata/migrations.go` file is the
reference implementation.

```go
package migrations

import (
	"github.com/pocketbase/pocketbase/core"
)

// ArticleCollection holds the articles.
const ArticleCollection = "articles"

// Migrations returns the migrations of the application.
//
// Register them in cmd/tui/main.go:
//
//	appMigrations() []core.Migration { return migrations.All() }
func All() []core.Migration {
	return []core.Migration{
		{
			// The file name must sort AFTER the PocketBase system
			// migrations, which is why it uses a timestamp prefix.
			File: "1700000001_create_articles.go",
			Up: func(txApp core.App) error {
				articles := core.NewBaseCollection(ArticleCollection)
				articles.Fields.Add(
					&core.TextField{Name: "title", Required: true, Max: 200},
					&core.TextField{Name: "body", Max: 100000},
					&core.SelectField{
						Name:     "status",
						Values:   []string{"draft", "published"},
						MaxSelect: 1,
					},
					&core.BoolField{Name: "featured"},
					&core.DateField{Name: "published_at"},
					&core.JSONField{Name: "meta"},
					&core.NumberField{Name: "rating", OnlyInt: false},
				)

				return txApp.Save(articles)
			},
			Down: func(txApp core.App) error {
				// Leave the collection in place on rollback: dropping
				// user data is rarely what you want.
				return nil
			},
		},
	}
}
```

### Rules for migrations

1. **Use `txApp`, never the outer app.** The outer app escapes the
   transaction, so the migration is no longer atomic.
2. **The file name must sort after the system migrations.** PocketBase
   orders migrations by file name; a `1700000001_` prefix puts yours at
   the end.
3. **`Down` is optional in practice.** Returning `nil` is a valid choice.
4. **Never register the same migration twice.** `core.AppMigrations` is a
   global.
5. **A failed migration rolls back.** If `Up` returns an error, the
   transaction is undone and the schema is unchanged.

### Field types

| PocketBase type  | Go type in `Record.Fields` |
| ---------------- | -------------------------- |
| `core.TextField`    | `string`                |
| `core.BoolField`    | `bool`                  |
| `core.NumberField`  | `float64`                |
| `core.JSONField`    | `map[string]any` or `[]any` |
| `core.DateField`    | `string` (RFC3339)       |
| `core.SelectField`  | `string`                 |
| `core.EditorField`  | `string`                 |
| `core.RelationField`| `string` (the id)       |

---

## Step 2 — register the migration

In `cmd/tui/main.go`:

```go
// appMigrations returns the application migrations executed on startup.
//
// Downstream projects add their own dictionary/reference collection
// migrations here.
func appMigrations() []core.Migration {
	return migrations.All()
}
```

The embedded store applies them automatically during `OpenEmbedded` —
you do not call them anywhere else.

---

## Step 3 — verify it works

```bash
# Apply the migrations to a scratch database.
go run ./cmd/tui --mode embedded --data-dir /tmp/flint-check --migrate-only

# The SQLite database must now exist.
ls -la /tmp/flint-check/data.db
```

Or in a test:

```go
func TestArticleCollectionExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pb_data")

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dir,
		HideStartBanner: true,
	})

	store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{
		DataDir:    dir,
		Migrations: migrations.All(),
	})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	cols, err := store.Collections(ctx)
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}

	var found bool
	for _, c := range cols {
		if c.Name == migrations.ArticleCollection {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("the collection %q was not created", migrations.ArticleCollection)
	}
}
```

---

## Step 4 — use it from the TUI

The collection appears automatically in the home page list. To give it a
nicer title or a custom renderer, see
[`add-a-record-renderer.md`](add-a-record-renderer.md).

To restrict which collections the user can reach, use the allowlist:

```bash
flint --mode embedded --collections articles,authors
```

Or in the config:

```toml
[remote]
collections = ["articles", "authors"]
```

An empty allowlist allows every collection.

---

## Step 5 — adding a field to an existing collection

Write a **new** migration. Never edit a migration that has shipped —
PocketBase records which files have already run.

```go
{
	File: "1700000002_add_articles_rating.go",
	Up: func(txApp core.App) error {
		collection, err := txApp.FindCollectionByNameOrId(ArticleCollection)
		if err != nil {
			return err
		}

		collection.Fields.Add(&core.NumberField{Name: "rating"})

		return txApp.Save(collection)
	},
	Down: func(txApp core.App) error {
		collection, err := txApp.FindCollectionByNameOrId(ArticleCollection)
		if err != nil {
			return err
		}

		collection.Fields.RemoveByName("rating")

		return txApp.Save(collection)
	},
},
```

---

## Gotchas

| Symptom                                        | Cause                                                     |
| ---------------------------------------------- | --------------------------------------------------------- |
| `collection not found` at runtime               | The migration is not registered in `appMigrations()`.     |
| The collection does not appear in the home page | The allowlist excludes it.                                |
| `field not found` in a filter                   | The field is not in the collection schema.                |
| The migration does not run                      | The file name sorts before the system migrations.         |
| Data is lost on rollback                        | `Down` drops the collection. Usually you do not want that. |

---

## Relations

```go
&core.RelationField{
	Name:          "author",
	CollectionId:  authorsCollectionID, // the collection id, not the name
	Required:      false,
	MaxSelect:     1,
	CascadeDelete: false,
},
```

To display the related record, use `Expand`:

```go
result, err := store.List(ctx, pbstore.Query{
	Collection: "articles",
	Expand:     "author",
})
```

The expanded data lands in `rec.Expand["author"]`.
