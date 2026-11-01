# The `pbstore` guide

This guide explains how to use the `pbstore` package: how to compose the
native PocketBase parameters, how to build a data dictionary, how to
implement a lookup cache and how to run an incremental sync.

## Why a wrapper at all?

The `pbstore` package does **not** re-implement PocketBase. It provides:

1. **One interface for two backends.** An in-process PocketBase and a
   remote PocketBase server behave identically.
2. **A backend-agnostic record view.** `pbstore.Record` exposes the field
   data as a plain map, so your code works with any schema.
3. **A small set of composition helpers.** The native parameter types
   are re-exported so that you never have to import the PocketBase
   packages directly.

Everything else — the filter grammar, the sorting, the pagination, the
validation, the record model — is PocketBase's own.

## The native primitives

### Filters

The filter syntax is the one documented by
[PocketBase](https://pocketbase.io/docs/api-records/#filter-syntax):

```go
result, err := store.List(ctx, pbstore.Query{
    Collection: "items",
    Filter:     `title ~ "go" && rank > 1`,
})
```

| Operator            | Meaning                          |
| ------------------- | -------------------------------- |
| `=`                 | equality                         |
| `!=`                | inequality                       |
| `~`                 | "like" (case insensitive)        |
| `!~`                | "not like"                       |
| `>`, `>=`, `<`, `<=` | comparison                       |
| `&&`, `\|\|`        | logical and / or                 |

The filter is parsed by PocketBase itself, so a malformed expression
produces an error that wraps `pbstore.ErrInvalidFilter`:

```go
if errors.Is(err, pbstore.ErrInvalidFilter) {
    // show the syntax error to the user
}
```

To build a filter programmatically, reuse the native builder:

```go
expr, err := pbstore.FilterExpr(
    `title = {:title}`,
    search.NewSimpleFieldResolver("title"),
    dbx.Params{"title": "Go"},
)
```

The returned `dbx.Expression` can be embedded into any
`dbx.SelectQuery`, exactly like in a PocketBase hook.

### Sorting

```go
result, err := store.List(ctx, pbstore.Query{
    Collection: "items",
    Sort:       "-created,name",   // newest first, then by name
})
```

`pbstore.SortFields` parses the expression into the native
`search.SortField` values if you need them programmatically.

### Pagination

```go
result, err := store.List(ctx, pbstore.Query{
    Collection: "items",
    Page:       2,
    PerPage:    50,
})

fmt.Println(result.TotalItems)  // the total number of matching records
fmt.Println(result.TotalPages)  // the number of pages
```

`pbstore.QueryParams` converts a `Query` into the native PocketBase URL
parameters (`page`, `perPage`, `filter`, `sort`, `expand`, `fields`,
`skipTotal`). It is what the remote backend sends over the wire, which
guarantees that both backends interpret a query identically.

### Expanding relations

```go
result, err := store.List(ctx, pbstore.Query{
    Collection: "items",
    Expand:     "author,editor",
})
```

The expanded records land in `record.Expand`.

## Building a data dictionary

A controlled vocabulary is a **collection**, not a Go map. The store
creates it for you:

```go
collection, err := store.EnsureDictionaryCollection(ctx, "vocab", []pbstore.DictionaryField{
    {Name: "term", Type: pbstore.FieldTypeText, Required: true},
    {Name: "weight", Type: pbstore.FieldTypeNumber},
    {Name: "kind", Type: pbstore.FieldTypeSelect, Options: []string{"noun", "verb"}},
})
```

The call is **idempotent**: it returns the existing collection when it is
already there, so it is safe to run on every start. The collection is
created with the native PocketBase collection model, which means it gets
the system fields, the indexes and the validation for free.

The supported field types are `text`, `editor`, `json`, `number`,
`bool`, `date` and `select`.

Then query it like any other collection:

```go
result, err := store.List(ctx, pbstore.Query{
    Collection: "vocab",
    Filter:     `term ~ "go"`,
})
```

> **Rule:** a lookup or a dictionary requirement is implemented as a
> collection plus a query against `pbstore`. A `map[string]any` in Go
> memory is only acceptable for a *volatile* view state.

## Implementing a lookup cache

A lookup cache is a **local copy** of a remote dataset, stored in a local
collection and queried through the store.

```go
// 1. Make sure the local collection exists.
if _, err := store.EnsureDictionaryCollection(ctx, "labels", nil); err != nil {
    return err
}

// 2. Pull the records from the remote source.
report, err := store.SyncCollection(ctx, pbstore.SyncRequest{
    RemoteCollection: "labels",
    LocalCollection:  "labels",
    Filter:           `namespace = "en"`,
    BatchSize:        200,
})
```

The sync matches the records **by id**, so repeating it refreshes the
existing copies instead of duplicating them:

```go
fmt.Printf("fetched %d, created %d, updated %d in %s\n",
    report.Fetched, report.Created, report.Updated, report.Duration)
```

### Incremental sync

Pass a `Since` timestamp to only pull the records that changed since the
last run:

```go
since := lastSuccessfulSync

report, err := store.SyncCollection(ctx, pbstore.SyncRequest{
    RemoteCollection: "labels",
    Since:            &since,
})
```

The native filter is built for you:

```
updated > "2024-05-01 10:00:00.000Z"
```

Combine it with your own filter using the native `&&` operator:

```go
// user filter + since → (user filter) && updated > "..."
pbstore.SyncFilter(`namespace = "en"`, &since)
```

Store the timestamp of the last successful sync **in a PocketBase
collection** (a small `_sync_state` collection works well), not in a Go
map, so that it survives a restart.

### A complete cache example

```go
// Lookup resolves a label from the local cache.
func Lookup(ctx context.Context, store pbstore.Store, id string) (string, error) {
    rec, err := store.FindFirst(ctx, "labels", fmt.Sprintf("id = %q", id))
    if err != nil {
        if errors.Is(err, pbstore.ErrNotFound) {
            return "", ErrUnknownLabel
        }
        return "", err
    }
    return rec.StringField("label"), nil
}
```

## The runtime store

`Embedded.RuntimeStore()` returns PocketBase's own runtime key/value
store (`*store.Store[string, any]`). It is concurrent-safe and it is
exactly what PocketBase uses internally to memoise the collection
registry.

Use it for **volatile** runtime data only:

```go
rt := embedded.RuntimeStore()
rt.Set("lastFilter", "title ~ \"go\"")
value := rt.Get("lastFilter")
```

Do **not** use it as a persistent cache: it lives in the process memory
and disappears on exit. A persistent cache belongs in a collection.

## Migrations

The embedded store applies the system migrations and the application
migrations on startup. Register your own migrations in `cmd/tui/main.go`:

```go
func appMigrations() []core.Migration {
    return []core.Migration{
        {
            File: "1700000000_create_vocab.go",
            Up: func(txApp core.App) error {
                collection := core.NewBaseCollection("vocab")
                collection.Fields.Add(
                    &core.TextField{Name: "key", Required: true},
                    &core.TextField{Name: "label"},
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

Migrations are the right place to create the dictionary and reference
collections: they run once, they are versioned and they are reversible.

## The remote backend

The remote backend speaks the native PocketBase REST API:

| Operation        | Endpoint                                            |
| ---------------- | --------------------------------------------------- |
| List             | `GET /api/collections/{collection}/records`          |
| Get              | `GET /api/collections/{collection}/records/{id}`      |
| Create           | `POST /api/collections/{collection}/records`         |
| Update           | `PATCH /api/collections/{collection}/records/{id}`   |
| Delete           | `DELETE /api/collections/{collection}/records/{id}`  |
| Collections      | `GET /api/collections`                               |
| Health check     | `GET /api/health`                                    |

The token is sent as the `Authorization` header, exactly like the admin
UI does.

The remote backend **cannot** create collections and **cannot** sync into
a local cache — both operations return an explicit error. Use the embedded
mode for those.

## Error handling

The package exposes stable sentinels:

| Error                       | Meaning                                       |
| --------------------------- | --------------------------------------------- |
| `pbstore.ErrNotFound`       | The record or collection does not exist       |
| `pbstore.ErrCollectionNotAllowed` | The collection is outside the allowlist |
| `pbstore.ErrInvalidFilter`  | The filter cannot be parsed                   |
| `pbstore.ErrInvalidSort`    | The sort cannot be parsed                     |
| `pbstore.ErrClosed`         | The store has been closed                     |
| `pbstore.ErrNotConnected`   | The remote server is unreachable              |

Always use `errors.Is`:

```go
if errors.Is(err, pbstore.ErrNotFound) {
    // the record is gone
}
```

## Performance notes

- The embedded backend executes the listing with the **native PocketBase
  search provider**, so the pagination is a real `LIMIT`/`OFFSET` and the
  total count is a real `COUNT(DISTINCT ...)`.
- Set `SkipTotal: true` when you do not need the total count: it removes
  the count query from the round trip.
- The remote backend retries the transient failures (network errors and
  5xx responses) with a linear backoff. Configure it with
  `RemoteConfig.RetryAttempts`.
- Every call accepts a `context.Context`: use a timeout to bound the
  queries and to cancel them during the shutdown.
