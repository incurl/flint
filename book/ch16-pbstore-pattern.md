# Chapter 16 — The `pbstore` Pattern: One Interface, Two Backends

## 16.1 The problem

You want your application to work in two situations:

1. **Standalone** — a single user, one binary, a local database.
2. **Connected** — a shared server, possibly several users.

And you want the *domain code* — the parts that know about vocabularies,
items, properties — to be **identical** in both.

The naive approach is a flag:

```go
if m.remote {
    result, err = fetchFromServer(collection, query)
} else {
    result, err = queryLocalDatabase(collection, query)
}
```

This spreads the branch through every call site. Add a third mode and
you have a mess.

The interface approach is the fix.

## 16.2 The interface

```go
type Store interface {
    Mode() Mode
    HealthCheck(ctx context.Context) (string, error)
    Collections(ctx context.Context) ([]Collection, error)
    List(ctx context.Context, q Query) (*PageResult, error)
    Get(ctx context.Context, collection, id string) (*Record, error)
    FindFirst(ctx context.Context, collection, filter string) (*Record, error)
    Create(ctx context.Context, collection string, data map[string]any) (*Record, error)
    Update(ctx context.Context, collection, id string, data map[string]any) (*Record, error)
    Delete(ctx context.Context, collection, id string) error
    Count(ctx context.Context, collection, filter string) (int, error)
    EnsureDictionaryCollection(ctx context.Context, name string, fields []DictionaryField) (Collection, error)
    SyncCollection(ctx context.Context, req SyncRequest) (*SyncReport, error)
    Close() error
}
```

The domain code now reads:

```go
result, err := store.List(ctx, pbstore.Query{
    Collection: "vocab",
    Filter:     `label ~ "go"`,
})
```

No branch. No flag. The store is chosen once, at startup.

## 16.3 The record view

The interface returns a backend-agnostic record:

```go
type Record struct {
    ID             string         `json:"id"`
    CollectionID   string         `json:"collectionId"`
    CollectionName string         `json:"collectionName"`
    Fields         map[string]any `json:"fields"`
    Expand         map[string]any `json:"expand,omitempty"`
    Created        string         `json:"created,omitempty"`
    Updated        string         `json:"updated,omitempty"`
}
```

### Converting from the embedded backend

```go
func recordFromCore(rec *core.Record) *Record {
    if rec == nil {
        return nil
    }

    out := &Record{
        ID:             rec.Id,
        CollectionID:   rec.Collection().Id,
        CollectionName: rec.Collection().Name,
        Fields:         make(map[string]any),
        Expand:         rec.Expand(),
    }

    // PublicExport only returns schema fields, so the raw field data is
    // merged in to also expose custom (non-schema) values.
    for k, v := range rec.FieldsData() {
        out.Fields[k] = v
    }
    for k, v := range rec.CustomData() {
        if _, ok := out.Fields[k]; !ok {
            out.Fields[k] = v
        }
    }

    if v, ok := out.Fields[core.FieldNameId]; ok {
        out.ID = fmt.Sprintf("%v", v)
    }
    // ... the same for the collection id, name, created and updated

    return out
}
```

⚠️ **The `FieldsData()` + `CustomData()` merge is important.**
`PublicExport()` alone would omit the custom (non-schema) data, which
is exactly where a domain's arbitrary JSON payload lives.

### Converting from the remote backend

```go
func recordFromMap(payload map[string]any) *Record {
    if payload == nil {
        return nil
    }

    // Unwrap the single-record envelope when present.
    if data, ok := payload["data"].(map[string]any); ok {
        payload = data
    }

    out := &Record{Fields: make(map[string]any, len(payload))}
    for k, v := range payload {
        out.Fields[k] = v
    }

    if v, ok := out.Fields["id"]; ok {
        out.ID = fmt.Sprintf("%v", v)
    }
    // ...

    return out
}
```

⚠️ **The two converters produce slightly different results.** The
embedded one knows the collection name from the model; the remote one
reads it from the JSON. They agree in practice because the REST API
includes `collectionName`.

But if a record has a *custom* field called `id`, the custom value
overwrites the system one. This is an edge case; if it matters, read the
system fields from a dedicated key.

## 16.4 The query: the symmetry trick

Here is the trick that makes the two backends behave identically.

The remote backend sends **URL query parameters**. The embedded backend
**parses the same URL query parameters**. So there is exactly one
definition of what a query means.

```go
func QueryParams(q Query) url.Values {
    values := url.Values{}

    if q.Filter != "" {
        values.Set(search.FilterQueryParam, q.Filter)
    }
    if q.Sort != "" {
        values.Set(search.SortQueryParam, q.Sort)
    }
    if q.Expand != "" {
        values.Set("expand", q.Expand)
    }
    if q.Fields != "" {
        values.Set("fields", q.Fields)
    }
    if q.SkipTotal {
        values.Set(search.SkipTotalQueryParam, "true")
    }

    values.Set(search.PageQueryParam, strconv.Itoa(NormalizePage(q.Page)))
    values.Set(search.PerPageQueryParam, strconv.Itoa(NormalizePerPage(q.PerPage)))

    return values
}
```

**Embedded:**

```go
provider := search.NewProvider(resolver).Query(query)
if err := provider.Parse(QueryParams(q).Encode()); err != nil {
    return nil, fmt.Errorf("%w: %v", ErrInvalidFilter, err)
}
```

**Remote:**

```go
endpoint := SearchURL(q.Collection) + "?" + QueryParams(q).Encode()
```

⚠️ **This is a genuinely elegant solution to a genuinely hard
problem.** The alternative — reimplementing the parameter handling in
each backend — is exactly how the two backends drift apart.

⚠️ **But it constrains the query shape.** The `Query` struct can only
express what the REST API can express. If you need a parameter the API
does not have, you cannot add it to the shared `Query`.

### The normalisation

```go
func NormalizePage(page int) int {
    if page < 1 {
        return 1
    }
    return page
}

func NormalizePerPage(perPage int) int {
    if perPage <= 0 {
        return search.DefaultPerPage
    }
    if perPage > search.MaxPerPage {
        return search.MaxPerPage
    }
    return perPage
}
```

Applied in `QueryParams`, so both backends clamp identically.

## 16.5 The embedded implementation

```go
type Embedded struct {
    app     core.App
    config  EmbeddedConfig
    allowed map[string]bool

    mu     sync.RWMutex
    closed bool
}

var _ Store = (*Embedded)(nil)
```

### Opening it

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

    if err := app.RunAllMigrations(); err != nil {
        _ = app.ClearBootstrap()
        return nil, fmt.Errorf("pbstore: run embedded migrations: %w", err)
    }

    return &Embedded{
        app:     app,
        config:  config,
        allowed: toSet(config.Collections),
    }, nil
}
```

### The listing

Covered in Chapter 10. The key points: the cached collection lookup,
the record resolver, the provider, the expansion.

### The writes

```go
func (s *Embedded) Create(ctx context.Context, collection string, data map[string]any) (*Record, error) {
    // ... validation ...

    model, err := s.app.FindCachedCollectionByNameOrId(collection)
    if err != nil {
        return nil, fmt.Errorf("pbstore: resolve collection %q: %w", collection, err)
    }

    rec := core.NewRecord(model)
    rec.Load(data)

    if err := s.app.Save(rec); err != nil {
        return nil, fmt.Errorf("pbstore: create %q: %w", collection, err)
    }
    return recordFromCore(rec), nil
}
```

⚠️ **`Save` runs the validation and the hooks.** The remote backend's
`POST` also runs them (on the server). So the semantics match.

## 16.6 The remote implementation

```go
type Remote struct {
    baseURL string
    token   string
    allowed map[string]bool
    client  *http.Client
    retries int

    mu     sync.RWMutex
    closed bool
}

var _ Store = (*Remote)(nil)
```

### Opening it

```go
func OpenRemote(config RemoteConfig) (*Remote, error) {
    if strings.TrimSpace(config.BaseURL) == "" {
        return nil, errors.New("pbstore: remote base url is required")
    }

    parsed, err := url.Parse(config.BaseURL)
    if err != nil {
        return nil, fmt.Errorf("pbstore: invalid remote url %q: %w", config.BaseURL, err)
    }
    if parsed.Scheme != "http" && parsed.Scheme != "https" {
        return nil, fmt.Errorf("pbstore: invalid remote url %q: expected http or https scheme", config.BaseURL)
    }

    timeout := config.Timeout
    if timeout <= 0 {
        timeout = DefaultRemoteTimeout
    }

    client := config.HTTPClient
    if client == nil {
        client = &http.Client{Timeout: timeout}
    }

    return &Remote{
        baseURL: strings.TrimRight(config.BaseURL, "/"),
        token:   config.Token,
        allowed: toSet(config.Collections),
        client:  client,
        retries: config.RetryAttempts,
    }, nil
}
```

⚠️ **Validate the URL at open time, not at request time.** A typo in
the configuration should be reported immediately, not on the first
query.

### The listing

```go
func (s *Remote) List(ctx context.Context, q Query) (*PageResult, error) {
    // ... validation ...

    params := QueryParams(q)

    var payload struct {
        Items      []map[string]any `json:"items"`
        Page       int              `json:"page"`
        PerPage    int              `json:"perPage"`
        TotalItems int              `json:"totalItems"`
        TotalPages int              `json:"totalPages"`
    }

    endpoint := SearchURL(q.Collection) + "?" + params.Encode()
    if err := s.doJSON(ctx, http.MethodGet, endpoint, nil, &payload, true); err != nil {
        return nil, err
    }

    out := &PageResult{
        Records:    make([]*Record, 0, len(payload.Items)),
        Page:       payload.Page,
        PerPage:    payload.PerPage,
        TotalItems: payload.TotalItems,
        TotalPages: payload.TotalPages,
    }
    for _, item := range payload.Items {
        out.Records = append(out.Records, recordFromMap(item))
    }
    return out, nil
}
```

## 16.7 The operations the remote backend cannot do

Two methods of the interface are **not supported** by the remote
backend, and they say so explicitly:

```go
// EnsureDictionaryCollection implements [Store].
//
// The remote backend cannot create collections: the operation is reserved
// for the embedded store. Use a PocketBase migration or the admin UI to
// create the collection on the server.
func (s *Remote) EnsureDictionaryCollection(ctx context.Context, name string, fields []DictionaryField) (Collection, error) {
    return Collection{}, errors.New("pbstore: the remote backend cannot create collections; use the embedded store or a PocketBase migration")
}

// SyncCollection implements [Store].
//
// A remote store has no local database to sync into, so the operation is
// rejected. Run the app in embedded mode to populate a local cache.
func (s *Remote) SyncCollection(ctx context.Context, req SyncRequest) (*SyncReport, error) {
    return nil, errors.New("pbstore: the remote backend cannot sync into a local cache; use the embedded mode")
}
```

⚠️ **An interface method that always fails is a design smell.** It
means the interface is not minimal — it describes the *union* of the
capabilities rather than the common denominator.

The alternative is to split the interface:

```go
// The operations every backend supports.
type Store interface {
    Mode() Mode
    HealthCheck(ctx context.Context) (string, error)
    Collections(ctx context.Context) ([]Collection, error)
    List(ctx context.Context, q Query) (*PageResult, error)
    Get(ctx context.Context, collection, id string) (*Record, error)
    FindFirst(ctx context.Context, collection, filter string) (*Record, error)
    Create(ctx context.Context, collection string, data map[string]any) (*Record, error)
    Update(ctx context.Context, collection, id string, data map[string]any) (*Record, error)
    Delete(ctx context.Context, collection, id string) error
    Count(ctx context.Context, collection, filter string) (int, error)
    Close() error
}

// The operations that need a local database.
type LocalStore interface {
    Store
    EnsureDictionaryCollection(ctx context.Context, name string, fields []DictionaryField) (Collection, error)
    SyncCollection(ctx context.Context, req SyncRequest) (*SyncReport, error)
}
```

Then the caller type-asserts:

```go
if local, ok := store.(pbstore.LocalStore); ok {
    report, err := local.SyncCollection(ctx, req)
    // ...
}
```

⚠️ **The template keeps the single interface**, for two reasons:

1. **The sync page already handles the mode.** It checks
   `store.Mode() != pbstore.ModeEmbedded` and shows a message. A
   type assertion would be more Go-idiomatic but less explicit.
2. **A minimal interface is easier to implement in a downstream
   project.** If a project writes its own store, it must implement
   everything in the interface. The smaller the interface, the easier
   that is.

The second reason is decisive for a template. Keep the interface
small, even if a method is occasionally unsupported.

## 16.8 The error mapping

Each backend maps its native errors to the domain sentinels.

### Embedded

```go
rec, err := s.app.FindRecordById(collection, id)
if err != nil {
    if errors.Is(err, sql.ErrNoRows) {
        return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, collection, id)
    }
    return nil, fmt.Errorf("pbstore: get %s/%s: %w", collection, id, err)
}
```

### Remote

```go
if resp.StatusCode == http.StatusNotFound {
    return fmt.Errorf("%w: %s", ErrNotFound, path)
}
```

⚠️ **Always wrap with `%w`.** The wrapping preserves the chain, so
`errors.Is(err, pbstore.ErrNotFound)` works, and the message keeps the
context ("get vocab/rec1: sql: no rows in result set").

## 16.9 The allowlist

```go
// toSet converts a slice of names into a lower-cased lookup set.
func toSet(names []string) map[string]bool {
    if len(names) == 0 {
        return nil
    }
    set := make(map[string]bool, len(names))
    for _, n := range names {
        n = strings.TrimSpace(strings.ToLower(n))
        if n != "" {
            set[n] = true
        }
    }
    return set
}

// isAllowed reports whether the collection passes the configured allowlist.
//
// An empty allowlist allows every collection.
func (s *Embedded) isAllowed(collection string) bool {
    if len(s.allowed) == 0 {
        return true
    }
    return s.allowed[strings.ToLower(collection)]
}
```

⚠️ **The allowlist check happens on every operation**, not just on
the listing. Otherwise a user could bypass it by guessing an id and
calling `Get`.

## 16.10 Testing the pattern

The strongest test of the pattern is a **contract test** that runs
against every implementation.

```go
// storeContract is the test suite every Store implementation must pass.
func storeContract(t *testing.T, newStore func(t *testing.T) pbstore.Store) {
    t.Run("List", func(t *testing.T) {
        store := newStore(t)
        // ...
    })

    t.Run("Get", func(t *testing.T) {
        // ...
    })
}

func TestEmbedded(t *testing.T) {
    storeContract(t, func(t *testing.T) pbstore.Store {
        return newEmbeddedStore(t)
    })
}

func TestRemote(t *testing.T) {
    storeContract(t, func(t *testing.T) pbstore.Store {
        return newRemoteStore(t)   // backed by an httptest server
    })
}
```

⚠️ **The remote contract test needs a fake server that behaves like
PocketBase.** The simplest approach is to run the *embedded* store
behind an HTTP handler:

```go
// A test server that exposes the embedded store over HTTP.
func newRemoteStore(t *testing.T) pbstore.Store {
    embedded := newEmbeddedStore(t)

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Translate the HTTP request into an embedded store call.
        // ...
    }))
    t.Cleanup(server.Close)

    remote, err := pbstore.OpenRemote(pbstore.RemoteConfig{BaseURL: server.URL})
    if err != nil {
        t.Fatalf("OpenRemote() error = %v", err)
    }
    t.Cleanup(func() { _ = remote.Close() })

    return remote
}
```

That is more work than a hand-written mock, but it tests the *actual*
wire format against the *actual* semantics.

⚠️ **Do not hand-write a mock that ignores the query string.** You
will write a test that passes while the filter is broken on the wire.

## 16.11 Extending the interface

When you need a new operation, here is the procedure.

### Step 1: check whether it is a query or a command

A **query** returns data and does not change anything. It goes on the
`Store` interface.

A **command** changes something. It also goes on the `Store`
interface, but it must respect the same rules: a context, wrapped
errors, backend-agnostic types.

### Step 2: add it to the interface

```go
// Count returns the number of records matching the provided filter.
Count(ctx context.Context, collection, filter string) (int, error)
```

### Step 3: implement it in both backends

```go
// Embedded.
func (s *Embedded) Count(ctx context.Context, collection, filter string) (int, error) {
    // ...
}

// Remote.
func (s *Remote) Count(ctx context.Context, collection, filter string) (int, error) {
    // ...
}
```

### Step 4: add it to the contract test

### Step 5: document it

⚠️ **Adding a method to the interface is a breaking change** for any
downstream project that implements `Store`. The template's `Store`
interface is documented as "the stable contract every downstream project
depends on". Treat additions carefully.

## 16.12 Exercises

1. Write the contract test from §16.10 and run it against both
   backends. Fix whatever breaks — it is probably a real asymmetry.

2. Split the `Store` interface into `Store` and `LocalStore` as
   described in §16.7. Measure how the call sites change.

3. Implement a third backend: a pure in-memory one for tests. Verify
   that every page works with it unchanged.

4. Add a `Search` method that does a full-text search across several
   fields. Implement it in both backends.

5. Write a mock remote server that records the requests it receives,
   and assert that the filter, the sort and the pagination are all
   transmitted correctly.

6. Benchmark the two backends for a listing of 100 records.

## 16.13 What you learned

- The interface **eliminates the mode branch** from the domain code.
- The record view is **deliberately less rich** than `core.Record`, so
  the pages stay backend-agnostic.
- The **`QueryParams` symmetry** is what guarantees the two backends
  agree: the embedded backend parses the same URL parameters the remote
  backend sends.
- The normalisation of the page and the per-page **happens in one
  place**.
- The embedded backend uses the **native search provider**; the remote
  backend uses the **native REST API**.
- Each backend **maps its native errors** to the domain sentinels, with
  `%w` so the chain survives.
- The **allowlist is checked on every operation**.
- An interface method that always fails is a smell, but a **minimal
  interface** is worth more for a template.
- The **contract test** is how you prove the two backends are
  equivalent.

## 16.14 Further reading

- `internal/pbstore/store.go` — the interface and the types.
- `internal/pbstore/queries.go` — `QueryParams` and the helpers.
- `internal/pbstore/embedded.go` — the embedded implementation.
- `internal/pbstore/remote.go` — the remote implementation.
- `internal/pbstore/embedded_test.go` and `remote_test.go` — the tests.

Next: [Chapter 17 — Sync, Caching, and the Offline-First Problem](ch17-sync-and-caching.md).
