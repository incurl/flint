# Chapter 11 — `App.Store()`: What It Really Is

## 11.1 A correction worth making early

This chapter exists because the design brief for the Flint template
contained a premise that is factually wrong, and because acting on that
premise would have produced a broken template.

The brief said:

> *"Treat PocketBase's `App.Store` as the application's primary local
> state / cache / data dictionary engine."*
>
> *"Reuse its existing query parameters, filter parser, sort rules,
> record model, validation hooks, and storage primitives."*

and asked for:

> *"Reuse native PocketBase `db.FindParams`, `db.ListParams`, filter
> expressions, sort, pagination, and expand rules everywhere possible."*

Let us be precise about what exists and what does not.

### `db.FindParams` and `db.ListParams` do not exist

There is no `db` package in PocketBase. The module layout is:

```
github.com/pocketbase/pocketbase/
├── core/       the data model, queries, hooks, migrations
├── apis/       the REST API layer (optional for you)
├── tools/      dbx, search, store, hook, list, types, ...
└── migrations/ the system migrations
```

The query parameters live in `tools/search`, as `search.FilterData`,
`search.SortField` and the `search.Provider`. The record model lives in
`core`. There is no `FindParams` or `ListParams` type anywhere in the
codebase.

**This is not a quibble about naming.** It changes what you write. The
real API is:

```go
// The real filter API.
search.FilterData(`key = "go"`).BuildExpr(resolver, dbx.Params{...})

// The real sort API.
search.ParseSortFromString("-created,name")

// The real pagination API.
search.NewProvider(resolver).Query(query).Page(2).PerPage(50).Exec(&records)
```

### `App.Store()` is not a data dictionary engine

This is the more consequential correction. Let us look at what
`App.Store()` actually is.

```go
// Store returns the app runtime store.
func (app *BaseApp) Store() *store.Store[string, any]
```

```go
// Store defines a concurrent safe in memory key-value data store.
type Store[K comparable, T any] struct {
    data    map[K]T
    mu      sync.RWMutex
    deleted int64
}
```

It is a **concurrent-safe, in-memory key/value map**. That is all it
is. It has:

- no persistence;
- no schema;
- no indexes;
- no filtering;
- no sorting;
- no pagination;
- no relations;
- no validation;
- no queries of any kind.

It is a `map[string]any` with a mutex.

## 11.2 What PocketBase itself uses it for

The most reliable way to understand an API is to see how the framework
uses it. Let us enumerate every use of `App.Store()` in the PocketBase
source (v0.40.6):

```go
// core/collection_query.go — the collection registry cache.
app.Store().Set(StoreKeyCachedCollections, collections)
collections, _ := app.Store().Get(StoreKeyCachedCollections).([]*Collection)

// core/backup_create.go — the "a backup is running" flag.
if app.Store().Has(StoreKeyActiveBackup) { ... }
app.Store().Set(StoreKeyActiveBackup, name)

// apis/middlewares_rate_limit.go — the rate limiter registry.
rateLimiters := e.App.Store().GetOrSet(rateLimitersStoreKey, func() any { ... })

// apis/record_auth_verification_request.go — the resend cooldown.
if !record.Verified() && e.App.Store().Has(resendKey) { ... }

// apis/record_auth_with_oauth2_redirect.go — a transient value.
e.App.Store().Set(nameKey, fullName)
```

Every single use is one of:

1. **a memoisation cache** for something expensive to recompute;
2. **a transient flag** that marks an in-progress operation;
3. **a cooldown timer** for a rate-limited action.

Not one of them stores *domain data*. Not one of them survives a
restart. Not one of them is queried.

### The collection cache, in detail

This is the most instructive example, because it shows exactly what
`App.Store()` is for:

```go
const StoreKeyCachedCollections = "pbAppCachedCollections"

// ReloadCachedCollections fetches all collections and caches them into the app store.
func (app *BaseApp) ReloadCachedCollections() error {
    collections, err := app.FindAllCollections()
    if err != nil {
        return err
    }

    app.Store().Set(StoreKeyCachedCollections, collections)

    return nil
}
```

and the read side:

```go
func (app *BaseApp) FindCachedCollectionByNameOrId(nameOrId string) (*Collection, error) {
    collections, _ := app.Store().Get(StoreKeyCachedCollections).([]*Collection)
    if collections == nil {
        // cache is not initialized yet (eg. run in a system migration)
        return app.FindCollectionByNameOrId(nameOrId)
    }
    // ...
}
```

The schema is cached in the runtime store **because** the schema is
still in the database. The cache is an optimisation, not the source of
truth.

That is the key distinction:

> **`App.Store()` is a cache of things that live somewhere else. The
> database is the source of truth.**

## 11.3 Why the premise would have produced a broken template

Suppose we had followed the brief literally. The brief asks for:

- *"preload vocabulary / property / label records into embedded store for
  fast offline lookup"*;
- *"Dictionary collections: dedicated collections for term definitions,
  mappings, aliases, controlled vocab, all persisted via PocketBase's
  own DB"*;
- *"Any lookup, dictionary, or caching requirement should be implemented
  as a collection + query against `pbstore`, not a separate
  `map[string]any` in Go memory."*

Notice that the brief's *own* rule — the last bullet — contradicts the
"treat `App.Store` as the data dictionary engine" instruction. The
brief says both "use `App.Store` as the dictionary" and "do not use a
`map[string]any` in Go memory".

The resolution is that these are two different things:

| | `App.Store()`                | A PocketBase collection          |
| --- | --------------------------- | -------------------------------- |
| Storage | In-process memory            | SQLite (or the server's database) |
| Survives a restart | No                 | Yes                              |
| Queryable | No (it is a map)             | Yes (the full filter grammar)    |
| Has a schema | No                  | Yes                              |
| Validated | No                   | Yes                              |
| Indexed | No                    | Yes                              |
| Shared between processes | No         | Yes                              |
| Mergeable with a remote | No             | Yes                              |

The brief's *behavioural* requirements — persist the dictionary, query
it, sync it, make it offline-capable — can only be met by a
**collection**. So the template uses collections for the dictionary, and
uses `App.Store()` for what it is actually good at.

## 11.4 What the template does

### The dictionary is a collection

```go
// EnsureDictionaryCollection implements [Store].
//
// The collection is created with the native PocketBase collection model, so
// it behaves like any hand-crafted collection created through the admin UI:
// it has system fields, indexes and validation.
func (s *Embedded) EnsureDictionaryCollection(ctx context.Context, name string, fields []DictionaryField) (Collection, error) {
    if err := ctx.Err(); err != nil {
        return Collection{}, err
    }
    if s.isClosed() {
        return Collection{}, ErrClosed
    }
    if name == "" {
        return Collection{}, errors.New("pbstore: dictionary collection name is required")
    }

    // Reuse the existing collection when it is already there.
    if existing, err := s.app.FindCachedCollectionByNameOrId(name); err == nil && existing != nil {
        return Collection{ID: existing.Id, Name: existing.Name, Type: existing.Type}, nil
    }

    collection := core.NewBaseCollection(name)
    collection.Fields.Add(
        &core.TextField{Name: "key", Required: true, Max: 200},
        &core.TextField{Name: "label", Max: 500},
        &core.TextField{Name: "description", Max: 5000},
    )

    for _, f := range fields {
        field, err := buildDictionaryField(f)
        if err != nil {
            return Collection{}, err
        }
        collection.Fields.Add(field)
    }

    if err := s.app.Save(collection); err != nil {
        return Collection{}, fmt.Errorf("pbstore: create dictionary collection %q: %w", name, err)
    }

    return Collection{ID: collection.Id, Name: collection.Name, Type: collection.Type}, nil
}
```

Note: it creates a **real collection**, with a real schema, real
validation and real persistence. The dictionary is queryable with the
full native filter grammar:

```go
result, err := store.List(ctx, pbstore.Query{
    Collection: "vocab",
    Filter:     `label ~ "go"`,
})
```

### The runtime store is exposed, honestly labelled

```go
// RuntimeStore exposes the native PocketBase runtime store.
//
// The returned store is the very same instance PocketBase uses internally
// (see [core.App] Store()). It is a concurrent-safe key/value store and is
// meant for *volatile* runtime data only — persistent lookup caches belong
// in a PocketBase collection, not here.
func (s *Embedded) RuntimeStore() *store.Store[string, any] { return s.app.Store() }
```

The doc comment is the important part. It tells the next developer
exactly what this is for and what it is not.

## 11.5 The correct uses of `App.Store()`

Here are the legitimate uses, with examples.

### 1. Memoising an expensive derivation

```go
// Cache the parsed schema of a collection.
rt := embedded.RuntimeStore()

if cached, ok := rt.Get("schema:" + collectionName).(*Collection); ok {
    return cached
}

collection, err := app.FindCachedCollectionByNameOrId(collectionName)
if err != nil {
    return nil, err
}
rt.Set("schema:"+collectionName, collection)
```

⚠️ **Invalidate the cache when the schema changes.** A collection that
is edited at runtime will have a stale cached copy.

### 2. Marking an in-progress operation

```go
// Prevent two concurrent syncs.
const syncLockKey = "flint:syncLock"

if rt.Has(syncLockKey) {
    return nil, errors.New("a sync is already running")
}
rt.Set(syncLockKey, true)
defer rt.Remove(syncLockKey)
```

This mirrors exactly what PocketBase does with `StoreKeyActiveBackup`.

### 3. A cooldown

```go
// Do not re-fetch the remote reference data more than once a minute.
const refreshKey = "flint:lastRefresh"

if last, ok := rt.Get(refreshKey).(time.Time); ok && time.Since(last) < time.Minute {
    return nil, nil    // too soon
}
rt.Set(refreshKey, time.Now())
```

### 4. Scratch space for a computation

```go
// Keep the intermediate results of an expensive scan.
rt.Set("scan:progress", 42)
```

## 11.6 The incorrect uses

### As a persistent dictionary

```go
// ✗ WRONG: the vocabulary disappears when the program exits.
rt.Set("vocab", vocabMap)
```

Use a collection.

### As a queryable index

```go
// ✗ WRONG: you cannot filter or sort a map efficiently, and you cannot
//   page through it.
for k, v := range rt.GetAll() {
    if strings.Contains(v.(string), query) { ... }
}
```

Use a collection with a `text` field and the `~` operator — SQLite
will do the work, and it can use an index.

### As a cache of remote records

```go
// ✗ WRONG: not persisted, not merged, not shared.
rt.Set("remote:"+id, record)
```

Use a collection and the sync mechanism (Chapter 17). That gives you
persistence, an incremental sync, and a merge strategy.

### As a schema

```go
// ✗ WRONG: a schema is rows in the _collections table.
rt.Set("schema", map[string]string{...})
```

Use `core.Collection` and `app.Save`.

## 11.7 The store API

The `store.Store` type is small but complete:

```go
func New[K comparable, T any](data map[K]T) *Store[K, T]

func (s *Store[K, T]) Reset(newData map[K]T)
func (s *Store[K, T]) RemoveAll()
func (s *Store[K, T]) Remove(key K)
func (s *Store[K, T]) Has(key K) bool
func (s *Store[K, T]) Get(key K) T
func (s *Store[K, T]) GetOk(key K) (T, bool)
func (s *Store[K, T]) GetAll() map[K]T
func (s *Store[K, T]) Keys() []K
func (s *Store[K, T]) Values() []T
func (s *Store[K, T]) Set(key K, value T)
func (s *Store[K, T]) SetFunc(key K, fn func(old T) T)
func (s *Store[K, T]) GetOrSet(key K, setFunc func() T) T
func (s *Store[K, T]) SetIfLessThanLimit(key K, value T, maxAllowedElements int) bool
```

A few are worth highlighting.

### `GetOk` versus `Get`

```go
value := rt.Get("missing")   // the zero value; you cannot tell it apart

value, ok := rt.GetOk("missing")   // ok == false
```

⚠️ **Use `GetOk` when the zero value is meaningful.** For a counter
stored as an `int`, `Get` returning `0` is ambiguous.

### `GetOrSet` — the atomic read-or-initialise

```go
// This is atomic; two goroutines cannot both run the initialiser.
limiters := rt.GetOrSet(rateLimitersKey, func() any {
    return newRateLimiter()
})
```

This is how PocketBase initialises its rate limiters. Use it for any
lazily-initialised shared value.

### `SetIfLessThanLimit` — a bounded cache

```go
// Returns false if the limit was reached and the value was NOT stored.
ok := rt.SetIfLessThanLimit(key, value, 500)
```

PocketBase uses this for its parsed-filter cache, which is bounded so
it cannot grow without limit.

⚠️ **If you cache user-derived values in the runtime store, bound it.**
An unbounded cache in a long-running process is a memory leak.

## 11.8 The interface segregation

There is a useful pattern hiding in the `store.Store` design: the
`GetOrSet` and `SetIfLessThanLimit` methods exist because PocketBase
needed *specific* cache behaviours, not because a key/value map is
interesting.

When you expose the runtime store to your application code, resist the
temptation to pass `*store.Store[string, any]` around. Instead, expose
*intents*:

```go
// A small interface with exactly what the application needs.
type RuntimeCache interface {
    Get(key string) (any, bool)
    Set(key string, value any)
    Remove(key string)
}
```

Then the application cannot accidentally use it as a database, because
the database operations are not on the interface.

⚠️ **This is a design heuristic, not a law.** If your application
genuinely needs the full store, expose it — but document the boundary.

## 11.9 A practical decision procedure

When you are about to reach for the runtime store, ask:

```
Does the data need to survive a restart?
├── yes → PocketBase collection
└── no  → Does it need to be queried, filtered, sorted or paged?
         ├── yes → PocketBase collection (the runtime store cannot do this)
         └── no  → Is it a cache of something else?
                  ├── yes → App.Store()  ✓
                  └── no  → Is it transient UI state?
                           ├── yes → the model's own field
                           └── no  → reconsider
```

The last branch deserves emphasis. Some "cache" data is really just
UI state:

```go
type Model struct {
    // The last filter the user typed, kept so that returning to the
    // records page restores it. This is UI state, not a cache.
    lastFilter string
}
```

That belongs in the model, not in the runtime store.

## 11.10 Verifying your understanding

Here is a test that distinguishes the two:

```go
func TestRuntimeStoreIsVolatile(t *testing.T) {
    store := newEmbeddedStore(t)
    rt := store.RuntimeStore()

    rt.Set("flint:test", "value")
    if !rt.Has("flint:test") {
        t.Error("the value was not stored")
    }

    // The value is in memory only: it is NOT in the database.
    count, err := store.Count(context.Background(), "_collections", "")
    // ... the point is that "flint:test" is nowhere in the database.
}
```

And the corresponding dictionary test:

```go
func TestDictionaryIsPersisted(t *testing.T) {
    store := newEmbeddedStore(t)
    ctx := context.Background()

    if _, err := store.EnsureDictionaryCollection(ctx, "vocab", nil); err != nil {
        t.Fatalf("EnsureDictionaryCollection() error = %v", err)
    }
    if _, err := store.Create(ctx, "vocab", map[string]any{"key": "go"}); err != nil {
        t.Fatalf("Create() error = %v", err)
    }

    // Reopen the store on the same directory.
    store2 := reopen(t)
    count, err := store2.Count(ctx, "vocab", "")
    if err != nil {
        t.Fatalf("Count() error = %v", err)
    }
    if count != 1 {
        t.Errorf("Count() = %d after a reopen, want 1", count)
    }
}
```

The second test is the one that proves the dictionary is a real
collection and not a map.

## 11.11 Exercises

1. Write a program that stores 10,000 vocabulary entries in the
   runtime store, then closes and reopens the app. Count what
   survived. Then do the same with a collection. Compare.

2. Grep the PocketBase source for `Store()` and classify every use.
   Verify your classification matches Chapter 11.2.

3. Implement a bounded LRU-ish cache on top of the runtime store using
   `SetIfLessThanLimit`, and measure the memory usage.

4. Write a `RuntimeCache` interface with only the three methods your
   application needs, and refactor a caller to use it.

5. Take the decision procedure from §11.9 and apply it to five
   concrete pieces of state in an application of your choice. Argue
   each one.

6. Implement `GetOrSet` semantics on top of a plain map, and
   demonstrate why the atomic version matters.

## 11.12 What you learned

- **`db.FindParams` and `db.ListParams` do not exist.** The real API
  is `tools/search`: `FilterData`, `SortField` and the `Provider`.
- **`App.Store()` is a concurrent-safe in-memory key/value map** — a
  `map[string]any` with a mutex. It has no schema, no queries, no
  persistence.
- PocketBase uses it for **three things**: memoisation caches,
  in-progress flags, and cooldown timers.
- It is a **cache of things that live elsewhere**; the database is the
  source of truth.
- A **persistent dictionary or lookup cache must be a collection**,
  because only a collection can be queried, filtered, sorted, paged,
  validated and persisted.
- The template exposes it as `RuntimeStore()` with a doc comment that
  states the boundary explicitly.
- Use `GetOk` over `Get` when the zero value is meaningful, `GetOrSet`
  for atomic initialisation, and `SetIfLessThanLimit` for bounded
  caches.

## 11.13 Further reading

- `tools/store/store.go` — the whole API, in 200 lines.
- `core/collection_query.go` — the collection cache, the canonical use.
- `core/backup.go` — `StoreKeyActiveBackup`.
- `apis/middlewares_rate_limit.go` — `GetOrSet` and `SetIfLessThanLimit`.
- `internal/pbstore/embedded.go` — `RuntimeStore()` and its doc comment.

Next: [Chapter 12 — Hooks, Validation, and the Record Life Cycle](ch12-hooks-and-validation.md).
