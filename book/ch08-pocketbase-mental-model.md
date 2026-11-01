# Chapter 8 — PocketBase's Mental Model

## 8.1 What PocketBase actually is

Most people meet PocketBase as a **backend**: a Go binary that serves a
REST API and a web dashboard, backed by SQLite. That is how the
project presents itself, and it is a good product.

But PocketBase is also, first and foremost, a **Go library**. The
`github.com/pocketbase/pocketbase` module contains:

- `core` — the data model, the query engine, the hooks, the migrations;
- `apis` — the REST API layer (built *on top of* `core`);
- `tools` — reusable pieces: `dbx` (the query builder), `search` (the
  filter/sort engine), `store` (the runtime key/value store), `hook`,
  `list`, `types`, and more;
- `migrations` — the system migrations that create the internal
  collections.

The `apis` package is *optional*. The `core` package is the product.

This matters enormously for a TUI application, because it changes the
deployment model:

```
┌─────────────────────────────────────────────────┐
│  Traditional:                                   │
│                                                 │
│    your TUI ──HTTP──▶ PocketBase server ──▶ SQLite │
│                                                 │
│  = 2 processes, 1 network hop, 1 failure point   │
└─────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────┐
│  Embedded (this book):                          │
│                                                 │
│    your TUI ──direct calls──▶ core.App ──▶ SQLite  │
│                                                 │
│  = 1 process, 0 network hops, 0 ports            │
└─────────────────────────────────────────────────┘
```

The second diagram is what the Flint template implements, with the
remote mode available for when you need a shared server.

## 8.2 The core concepts

PocketBase has four concepts you must understand.

### The App

`core.App` is the runtime. It owns:

- the database connections (a "data" db and an "aux" db);
- the settings;
- the **collection registry**;
- the **hooks**;
- the **runtime store**;
- the cron scheduler;
- the migration list.

```go
app := pocketbase.New()
// or
app := pocketbase.NewWithConfig(pocketbase.Config{
    DefaultDataDir:  "./pb_data",
    DefaultDev:      false,
    HideStartBanner: true,
})
```

The `core.App` interface is huge, but you only need a fraction of it:

```go
type App interface {
    Bootstrap() error
    ClearBootstrap() error
    DataDir() string
    IsBootstrapped() bool
    Logger() *slog.Logger
    Store() *store.Store[string, any]

    RunSystemMigrations() error
    RunAppMigrations() error
    RunAllMigrations() error

    Save(model Model) error
    Delete(model Model) error
    Validate(model Model) error

    FindAllCollections(collectionTypes ...string) ([]*Collection, error)
    FindCollectionByNameOrId(nameOrId string) (*Collection, error)
    FindCachedCollectionByNameOrId(nameOrId string) (*Collection, error)

    FindAllRecords(collectionModelOrIdentifier any, exprs ...dbx.Expression) ([]*Record, error)
    FindRecordById(collectionModelOrIdentifier any, recordId string, optFilters ...func(q *dbx.SelectQuery) error) (*Record, error)
    FindRecordsByIds(collectionModelOrIdentifier any, recordIds []string, optFilters ...func(q *dbx.SelectQuery) error) ([]*Record, error)
    FindRecordsByFilter(collectionModelOrIdentifier any, filter string, sort string, limit int, offset int, params ...dbx.Params) ([]*Record, error)
    FindFirstRecordByFilter(collectionModelOrIdentifier any, filter string, params ...dbx.Params) (*Record, error)

    RecordQuery(collectionModelOrIdentifier any) *dbx.SelectQuery
    ExpandRecord(record *Record, expands []string, optFetchFunc ExpandFetchFunc) map[string]error
    ExpandRecords(records []*Record, expands []string, optFetchFunc ExpandFetchFunc) map[string]error

    OnRecordCreate(tags ...string) *hook.TaggedHook[*RecordEvent]
    OnRecordUpdate(tags ...string) *hook.TaggedHook[*RecordEvent]
    OnRecordValidate(tags ...string) *hook.TaggedHook[*RecordEvent]
    // ... dozens more
}
```

### The Collection

A collection is a **table plus a schema**. It defines the fields, the
indexes, and the access rules.

```go
type Collection struct {
    baseCollection
    collectionAuthOptions
    collectionViewOptions
}

type baseCollection struct {
    BaseModel

    ListRule   *string
    ViewRule   *string
    CreateRule *string
    UpdateRule *string
    DeleteRule *string

    Name    string
    Type    string          // "base", "auth" or "view"
    Fields  FieldsList
    Indexes types.JSONArray[string]
    Created types.DateTime
    Updated types.DateTime

    System bool
}
```

Three collection types:

| Type    | Purpose                                                 |
| ------- | ------------------------------------------------------- |
| `base`  | Ordinary records with your own schema                    |
| `auth`  | Records with email/password, verified flag, tokens       |
| `view`  | A read-only SQL view presented as a collection           |

### The Field

A field is a typed column with validation rules:

```go
type TextField struct {
    Name     string
    Id       string
    System   bool
    Hidden   bool
    Presentable bool
    Help     string
    Min      int
    Max      int
    Pattern  string
    AutogeneratePattern string
    Required bool
    PrimaryKey bool
}
```

The available field types are:

`TextField`, `BoolField`, `NumberField`, `DateField`, `SelectField`,
`JSONField`, `EditorField`, `FileField`, `RelationField`,
`GeoPointField`, `PasswordField`, `EmailField`, `AutodateField`.

### The Record

A record is a row, typed by its collection:

```go
type Record struct {
    collection       *Collection
    originalData     map[string]any
    customVisibility *store.Store[string, bool]
    data             *store.Store[string, any]
    expand           *store.Store[string, any]

    BaseModel

    exportCustomData      bool
    ignoreEmailVisibility bool
    ignoreUnchangedFields bool
}
```

Note that a record is **not** a plain struct with typed fields. It is a
dynamic map, because the schema is defined at runtime, in the
database. That is the design decision that makes PocketBase
schema-flexible, and it has consequences for how you write code against
it (Chapter 9).

## 8.3 The schema is data

This is the idea that trips up newcomers.

In a typical Go application, the schema is in the code:

```go
type Term struct {
    ID    string
    Label string
}
```

and migrations are hand-written SQL.

In PocketBase, the schema is **rows in a table**. The `_collections`
table holds the collection definitions, and each collection has a
`_collections` schema row plus a real table for the records.

The consequences:

1. **You can create a collection at runtime.** No code change, no
   deployment. The admin UI does exactly this.
2. **The record type is dynamic.** `Record.Get("name")` returns `any`.
3. **You can inspect the schema at runtime.** `app.FindAllCollections()`
   gives you the full schema.
4. **Validation is driven by the schema.** The `Required`, `Min`, `Max`
   and `Pattern` fields are enforced by the framework.

For a TUI that browses arbitrary user-defined collections, this is
exactly right. The template's home page lists the collections
discovered at runtime, and the record table works with any of them,
because it treats the fields as a dynamic map.

## 8.4 Booting the app

```go
app := pocketbase.NewWithConfig(pocketbase.Config{
    DefaultDataDir:  "./pb_data",
    HideStartBanner: true,
})

if err := app.Bootstrap(); err != nil {
    log.Fatal(err)
}
defer app.ClearBootstrap()
```

`Bootstrap()` does the setup: it creates the data directory, opens the
database connections, loads the settings and starts the cron ticker.

`ClearBootstrap()` is the inverse: it closes the connections and stops
the ticker.

⚠️ **`Bootstrap` is not idempotent in the sense you want.** Calling it
twice re-initialises. Call it once, and always pair it with
`ClearBootstrap`.

⚠️ **The data directory is created with restrictive permissions.** If
your program runs as a different user than the one that created the
directory, it will fail to open the database. This is a common source
of "it works for me" bugs.

## 8.5 The lifecycle of an embedded app

```
   main()
     │
     ├── pocketbase.NewWithConfig(...)
     │        └── creates the App struct, inits the hooks
     │
     ├── app.Bootstrap()
     │        ├── create/verify the data dir
     │        ├── open the SQLite connections
     │        ├── load the settings
     │        └── start the cron ticker
     │
     ├── app.RunAllMigrations()
     │        ├── apply the system migrations (create _collections, _superusers, ...)
     │        └── apply the app migrations (your collections)
     │
     ├── ... your application runs ...
     │
     ├── app.ClearBootstrap()
     │        ├── close the connections
     │        └── stop the cron ticker
     │
     └── exit
```

The template's `OpenEmbedded` does exactly this:

```go
func OpenEmbedded(app core.App, config EmbeddedConfig) (*Embedded, error) {
    if app == nil {
        return nil, errors.New("pbstore: nil pocketbase app")
    }
    if config.DataDir == "" {
        return nil, errors.New("pbstore: embedded data dir is required")
    }

    // Register the user provided migrations before the app is bootstrapped
    // so that they are applied together with the system migrations.
    for _, m := range config.Migrations {
        core.AppMigrations.Add(&m)
    }

    if err := app.Bootstrap(); err != nil {
        return nil, fmt.Errorf("pbstore: bootstrap embedded store: %w", err)
    }

    // Apply the system + app migrations (creating the collections needed by
    // the dictionary/reference data).
    if err := app.RunAllMigrations(); err != nil {
        _ = app.ClearBootstrap()
        return nil, fmt.Errorf("pbstore: run embedded migrations: %w", err)
    }

    return &Embedded{ /* ... */ }, nil
}
```

⚠️ **Notice the error path.** If the migrations fail, the bootstrap is
rolled back with `ClearBootstrap()`. Otherwise you leak the database
connections on a failed start.

## 8.6 The two databases

PocketBase uses **two** SQLite databases:

| Database | Purpose                                                  |
| -------- | -------------------------------------------------------- |
| `data.db` | Your records, the collections, the settings                |
| `aux.db` | Logs, backups metadata, and other auxiliary data           |

They are separated so that a large log table does not bloat the main
database, and so that some operations can be done on the auxiliary
database without contending with the main one.

The `core.App` interface exposes both:

```go
DB() dbx.Builder              // the data database
AuxDB() dbx.Builder           // the auxiliary database
ConcurrentDB() dbx.Builder    // a pooled connection
NonconcurrentDB() dbx.Builder // a single connection
```

⚠️ **Use `ConcurrentDB()` for your queries.** `DB()` returns the
non-concurrent builder, which serialises the queries. Since Bubble Tea
runs commands concurrently, you want the concurrent one. PocketBase's
own `RecordQuery` uses it internally, so the standard query methods are
already safe.

## 8.7 PocketBase is not an ORM

There is no `db.Where("name = ?", x).First(&user)`. There is no
struct-to-table mapping.

Instead there are two ways to query:

### The model API (typed, simple)

```go
records, err := app.FindAllRecords("items")
records, err := app.FindRecordsByFilter("items", `name = "go"`, "-created", 10, 0)
record, err := app.FindFirstRecordByFilter("items", `key = "go"`)
```

### The query builder (raw, powerful)

```go
query := app.RecordQuery("items")
query.AndWhere(dbx.NewExp("[[name]] = {:name}", dbx.Params{"name": "go"}))
query.AndOrderBy("[[created]] DESC")
query.Limit(10)

records := []*core.Record{}
err := query.All(&records)
```

The template uses **both**: the model API for the simple operations and
the search provider for the listings (Chapter 10).

⚠️ **Do not write raw SQL against the PocketBase tables.** The schema
is an implementation detail that changes between versions. Use the
model API or the query builder.

## 8.8 The `dbx` query builder

`dbx` is PocketBase's own query builder (a fork of `squirrel`). It is
the layer underneath every PocketBase query.

```go
import "github.com/pocketbase/dbx"

// Expressions.
dbx.NewExp("[[name]] = {:name}", dbx.Params{"name": "go"})
dbx.HashExp{"name": "go"}
dbx.In("id", "a", "b", "c")
dbx.NotIn("id", "a", "b")
dbx.And(expr1, expr2)
dbx.Or(expr1, expr2)
dbx.Exists(subquery)

// Queries.
q := dbx.Select("*").From("items").Where(expr).OrderBy("created DESC").Limit(10)
```

Two conventions matter:

**`[[column]]`** is the quoting syntax. It becomes the correctly quoted
identifier for the current dialect (SQLite, MySQL, Postgres). Use it
for every column name.

**`{:param}`** is the placeholder syntax. The values go in `dbx.Params`
and are bound, not interpolated:

```go
dbx.NewExp("[[name]] = {:name}", dbx.Params{"name": userInput})
```

⚠️ **Never build SQL by string concatenation with user input.** The
placeholder syntax is not a convenience — it is the defence against
SQL injection. The template's `dbx` usage always uses it.

## 8.9 The `search` package

The `tools/search` package implements PocketBase's filter grammar and
its pagination. It is the machinery behind the REST API's
`?filter=`, `?sort=`, `?page=`, `?perPage=` parameters.

```go
import "github.com/pocketbase/pocketbase/tools/search"

// The filter grammar.
type FilterData string

func (f FilterData) BuildExpr(
    fieldResolver FieldResolver,
    placeholderReplacements ...dbx.Params,
) (dbx.Expression, error)
```

```go
// The sort syntax.
func ParseSortFromString(str string) (fields []SortField)

type SortField struct {
    Name      string
    Direction string   // search.SortAsc or search.SortDesc
}
```

```go
// The provider: filter + sort + paginate + execute.
provider := search.NewProvider(fieldResolver).Query(query)
provider.Parse(urlQuery)          // parse "page=2&filter=..."
result, err := provider.Exec(&records)
```

Chapter 10 is entirely about this package, because it is the heart of
the query story.

## 8.10 The runtime store

`app.Store()` returns a concurrent-safe key/value store:

```go
func (app *BaseApp) Store() *store.Store[string, any]
```

```go
type Store[K comparable, T any] struct {
    data    map[K]T
    mu      sync.RWMutex
    deleted int64
}
```

It is PocketBase's own memoisation cache. PocketBase uses it to store:
- the cached collection registry (`StoreKeyCachedCollections`);
- the active backup flag (`StoreKeyActiveBackup`);
- the rate limiters;
- the verification resend timestamps.

⚠️ **It is not a database.** It is in-process memory. It disappears
when the process exits, and it is not shared between processes.

Chapter 11 examines this in detail, because it is the single most
misunderstood API in PocketBase — and the template prompt's framing of
it deserves a careful correction.

## 8.11 The hooks

PocketBase has a rich hook system. Every model event has a hook:

```go
app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{
    Id: "my-handler",
    Func: func(e *core.RecordEvent) error {
        // e.Record is the record being created.
        return e.Next()   // call the next handler
    },
})
```

The `hook.Handler` pattern with `e.Next()` is a middleware chain: each
handler can act before or after calling `Next()`.

```go
app.OnRecordValidate().BindFunc(func(e *core.RecordEvent) error {
    if e.Record.GetString("status") == "invalid" {
        return validation.NewError("validation_invalid_status", "invalid status")
    }
    return e.Next()
})
```

⚠️ **Forgetting `e.Next()` stops the chain.** If you bind a handler
and do not call `Next()`, no other handler runs — including the
framework's own validation. Your hook becomes a black hole.

The available record hooks include:

```go
OnRecordValidate
OnRecordCreate
OnRecordCreateExecute
OnRecordAfterCreateSuccess
OnRecordAfterCreateError
OnRecordUpdate
OnRecordUpdateExecute
OnRecordAfterUpdateSuccess
OnRecordAfterUpdateError
OnRecordDelete
OnRecordDeleteExecute
OnRecordAfterDeleteSuccess
OnRecordAfterDeleteError
OnRecordEnrich
OnRecordsListRequest
OnRecordViewRequest
OnRecordCreateRequest
OnRecordUpdateRequest
OnRecordDeleteRequest
// ... plus the auth-specific ones
```

Chapter 12 covers them in detail.

## 8.12 The migrations

PocketBase's migration system is a list of up/down functions:

```go
type Migration struct {
    Up               func(txApp App) error
    Down             func(txApp App) error
    File             string
    ReapplyCondition func(txApp App, runner *MigrationsRunner, fileName string) (bool, error)
}
```

```go
type MigrationsList struct {
    list []*Migration
}
```

There are two lists:

```go
var SystemMigrations MigrationsList   // the internal collections
var AppMigrations MigrationsList      // your collections
```

`RunAllMigrations()` applies both. Chapter 13 covers the details.

## 8.13 Deciding: library or server?

Now the practical question. When should you embed PocketBase and when
should you run it as a server?

### Embed it when

- the data belongs to one user or one machine;
- you want **offline** operation;
- you want **zero-infrastructure** distribution (one binary);
- the data is a **cache** of something else;
- latency matters (no network hop);
- you are building a **single-user desktop-like tool**.

### Run it as a server when

- the data is **shared** between users or machines;
- you need **multi-user** access and concurrency control;
- the data must be **durable and backed up** centrally;
- you need the **admin dashboard** for non-technical users;
- you want **realtime** subscriptions across clients.

### Use both when

This is the template's design, and it is the most flexible option:

```
┌──────────────────────────────┐
│  embedded mode               │  ← the default: local, offline
│  (SQLite in the app)         │
└──────────────────────────────┘

┌──────────────────────────────┐
│  remote mode                 │  ← a shared server
│  (REST API)                  │
└──────────────────────────────┘

┌──────────────────────────────┐
│  embedded + sync             │  ← a local cache of a shared server
│  (pull into the local db)    │
└──────────────────────────────┘
```

The `pbstore.Store` interface makes all three look the same to the
application code (Chapter 16).

## 8.14 Exercises

1. Write a program that boots PocketBase in a temporary directory,
   creates a collection with three fields, inserts a record, reads it
   back, and shuts down. Print the collection schema at each step.

2. Print `app.FindAllCollections()` after the migrations and identify
   which ones are system collections (`collection.System == true`).

3. Write a program that opens the same data directory twice (two apps
   in one process) and observes what happens. Explain the result in
   terms of the SQLite locking model.

4. Use `dbx` to write a query that counts the records in a collection
   whose name starts with "a". Compare it with the equivalent native
   filter.

5. Write a hook on `OnRecordValidate` that rejects a record whose
   "email" field is empty. Verify that it fires, and verify that
   forgetting `e.Next()` breaks the *other* validation.

6. Decide, for three concrete applications you can think of, whether
   each should embed PocketBase, use it as a server, or use both.
   Justify each answer.

## 8.15 What you learned

- PocketBase is **a Go library first**; the REST API is a layer on
  top of `core`, and it is optional.
- The four concepts are the **App**, **Collection**, **Field** and
  **Record**.
- The **schema is data**, stored in rows — which is why collections
  can be created at runtime and records are dynamic maps.
- The boot sequence is **New → Bootstrap → RunAllMigrations →
  … → ClearBootstrap**.
- There are **two databases** (`data.db` and `aux.db`).
- `dbx` is the query builder; use `[[column]]` quoting and `{:param}`
  placeholders — the latter is your SQL-injection defence.
- `tools/search` implements the filter grammar and the pagination.
- `app.Store()` is an **in-memory** store, not a database.
- Hooks are a **middleware chain**; forgetting `e.Next()` breaks it.
- **Embed for single-user and offline; serve for shared and durable;
  use both for a local cache of a shared server.**

## 8.16 Further reading

- `core/app.go` — the `App` interface, in full.
- `core/base.go` — `BaseApp`, `Bootstrap`, `ClearBootstrap`.
- `core/collection_model.go` — the `Collection` struct.
- `core/record_model.go` — the `Record` struct.
- `core/db.go` — `Save`, `Delete`, `Validate`.
- `tools/search/filter.go` — the filter grammar.
- `tools/dbutils` and `dbx` — the query builder.

Next: [Chapter 9 — The App: Collections, Fields, Records](ch09-app-collections-records.md).
