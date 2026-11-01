# Chapter 18 — Testing Terminal Applications

## 18.1 The testing problem

A TUI is harder to test than a web application. The web has a DOM you
can inspect; the TUI has a terminal.

But Bubble Tea's architecture makes testing *easier* than it looks:

- the model is a **value** you can construct in a test;
- `Update` is a **pure function** you can call directly;
- `View` is a **pure function** you can call and compare;
- the commands are **functions you can call** to get the message they
  would produce.

You do not need a terminal to test a Bubble Tea application. You need
one only for the *golden* tests — the ones that check the exact
rendering.

## 18.2 The three layers of testing

| Layer              | What it covers                        | How                       |
| ------------------ | ------------------------------------- | ------------------------- |
| **The model**      | The state transitions                 | Call `Update` directly    |
| **The store**      | The data operations                   | A real or mocked backend  |
| **The rendering**  | The exact output                      | Golden files or snapshots |

The first two are the vast majority of the value, and both are easy.

## 18.3 Testing the model

### The basic shape

```go
func TestModelNavigation(t *testing.T) {
    m, _ := newTestModel(t)

    pages := []struct {
        key  string
        want state.Page
    }{
        {key: "1", want: state.PageHome},
        {key: "2", want: state.PageRecords},
        {key: "3", want: state.PageDetail},
    }

    for _, tt := range pages {
        t.Run(tt.key, func(t *testing.T) {
            updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
            m = updated.(*app.Model)

            view := m.View()
            if !contains(view, tt.want.Title()) {
                t.Errorf("View() = %q, want it to contain %q", view, tt.want.Title())
            }
        })
    }
}
```

Three observations:

1. **You construct the model directly.** No program, no terminal.
2. **You call `Update` with a synthetic message.** `tea.KeyMsg` is a
   plain struct.
3. **You assert on `View()`.** The output is a string, so it is easy to
   check.

⚠️ **The `contains` helper.** Go's standard library has
`strings.Contains`. The template's test defines its own because the
test package is `app_test` (external) and wanted a short name. Just
use `strings.Contains`.

### Asserting on the state, not the view

Asserting on the rendered output is a *smoke test*. For the important
transitions, assert on the state:

```go
func TestModelHealthCheck(t *testing.T) {
    m, _ := newTestModel(t)

    updated, _ := m.Update(app.HealthMsg{Status: "ok"})
    m = updated.(*app.Model)

    if m.Runtime().Connection != state.ConnectionOK {
        t.Errorf("Connection = %q, want %q", m.Runtime().Connection, state.ConnectionOK)
    }
}
```

⚠️ **This requires the model to expose its state.** The template's
`Model` does not export the runtime. For a template, adding an
accessor is reasonable:

```go
// Runtime returns the shared runtime state.
//
// It is exposed for testing and for the domain code that needs to
// inspect the current state.
func (m *Model) Runtime() *state.Runtime { return m.runtime }
```

⚠️ **Do not expose the state just to make the tests pass.** If the
state is genuinely internal, assert on the observable behaviour (the
view, the commands, the messages).

### Testing a command

A command is a function. You can call it and inspect the message:

```go
func TestRecordsLoad(t *testing.T) {
    store := &stubStore{
        records: []*pbstore.Record{
            {ID: "rec1", CollectionName: "vocab"},
        },
    }

    page := pages.NewRecordsPage(store, newRuntime(), theme.Default(), nil)

    msg := page.Load()()

    result, ok := msg.(pages.RecordsMsg)
    if !ok {
        t.Fatalf("Load() returned %T, want pages.RecordsMsg", msg)
    }
    if result.Err != nil {
        t.Fatalf("Load() error = %v", result.Err)
    }
    if len(result.Result.Records) != 1 {
        t.Errorf("Load() returned %d records, want 1", len(result.Result.Records))
    }
}
```

⚠️ **Calling `page.Load()()` runs the command synchronously.** It
blocks until the store call returns. That is fine in a test, and it is
the reason the store is injectable.

### Testing a state transition with a message

```go
func TestRecordsPageFilter(t *testing.T) {
    store := &stubStore{}
    page := pages.NewRecordsPage(store, newRuntime(), theme.Default(), nil)

    // The "/" key focuses the filter input.
    updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
    page = updated.(*pages.RecordsPage)

    if !page.FilterFocused() {
        t.Fatal("FilterFocused() = false after the \"/\" key")
    }

    // The typed keys reach the input.
    updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
    page = updated.(*pages.RecordsPage)

    if !page.FilterFocused() {
        t.Error("FilterFocused() = false after typing")
    }
}
```

## 18.4 The stub store

To test the pages without a database, you need a stub store:

```go
// stubStore is a minimal in-memory [pbstore.Store] used to drive the root
// model without a database.
type stubStore struct {
    mode        pbstore.Mode
    collections []pbstore.Collection
    records     []*pbstore.Record
    err         error
}

func (s *stubStore) Mode() pbstore.Mode { return s.mode }

func (s *stubStore) HealthCheck(ctx context.Context) (string, error) {
    return "ok", nil
}

func (s *stubStore) Collections(ctx context.Context) ([]pbstore.Collection, error) {
    return s.collections, s.err
}

func (s *stubStore) List(ctx context.Context, q pbstore.Query) (*pbstore.PageResult, error) {
    if s.err != nil {
        return nil, s.err
    }
    return &pbstore.PageResult{
        Records:    s.records,
        Page:       1,
        PerPage:    30,
        TotalItems: len(s.records),
        TotalPages: 1,
    }, nil}

// ... the rest of the interface ...
```

⚠️ **The stub must implement the whole interface.** When you add a
method to `Store`, the stub breaks — which is a *feature*: the compiler
tells you that your test double is out of date.

### A better approach: embed the interface

```go
// stubStore embeds the interface so that only the methods under test
// need to be implemented.
type stubStore struct {
    pbstore.Store   // the embedded (nil) interface

    mode        pbstore.Mode
    collections []pbstore.Collection
    records     []*pbstore.Record
}
```

⚠️ **This is a trade-off.** Embedding the interface means the
unimplemented methods panic with a nil pointer dereference at *runtime*
rather than failing to compile. For a large interface that is a
reasonable trade; for a small one, implement everything.

The template implements every method explicitly, which is safer.

## 18.5 Testing the store

### Against a real embedded database

This is the most valuable kind of store test, because it exercises the
real SQLite, the real schema and the real validation.

```go
func newEmbeddedStore(t *testing.T) *pbstore.Embedded {
    t.Helper()

    dir := filepath.Join(t.TempDir(), "pb_data")

    app := pocketbase.NewWithConfig(pocketbase.Config{
        DefaultDataDir:  dir,
        HideStartBanner: true,
    })

    store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{
        DataDir: dir,
    })
    if err != nil {
        t.Fatalf("OpenEmbedded() error = %v", err)
    }
    t.Cleanup(func() { _ = store.Close() })

    return store
}
```

⚠️ **`t.TempDir()` is essential.** It creates a unique directory per
test and removes it afterwards. A fixed path makes the second run fail
(the migrations are already applied) and makes parallel runs interfere.

### The table-driven listing test

```go
func TestEmbeddedListFilters(t *testing.T) {
    store := newEmbeddedStore(t)
    ctx := context.Background()

    if _, err := store.EnsureDictionaryCollection(ctx, "items", []pbstore.DictionaryField{
        {Name: "rank", Type: pbstore.FieldTypeNumber},
    }); err != nil {
        t.Fatalf("EnsureDictionaryCollection() error = %v", err)
    }

    seeds := []map[string]any{
        {"key": "alpha", "label": "Alpha", "rank": 1},
        {"key": "beta", "label": "Beta", "rank": 2},
        {"key": "gamma", "label": "Gamma", "rank": 3},
    }
    for _, seed := range seeds {
        if _, err := store.Create(ctx, "items", seed); err != nil {
            t.Fatalf("Create(%v) error = %v", seed, err)
        }
    }

    tests := []struct {
        name      string
        query     pbstore.Query
        want      int
        wantTotal int
        first     string
    }{
        {name: "no filter", query: pbstore.Query{Collection: "items"}, want: 3, wantTotal: 3, first: "alpha"},
        {name: "exact match", query: pbstore.Query{Collection: "items", Filter: `key = "beta"`}, want: 1, wantTotal: 1, first: "beta"},
        {name: "like match", query: pbstore.Query{Collection: "items", Filter: `key ~ "a"`}, want: 3, wantTotal: 3},
        {name: "and expression", query: pbstore.Query{Collection: "items", Filter: `rank > 1 && rank < 3`}, want: 1, wantTotal: 1, first: "beta"},
        {name: "descending sort", query: pbstore.Query{Collection: "items", Sort: "-key"}, want: 3, wantTotal: 3, first: "gamma"},
        {
            // The page holds a single record but the total count still
            // reflects every matching record.
            name: "pagination", query: pbstore.Query{Collection: "items", Page: 2, PerPage: 1, Sort: "key"},
            want: 1, wantTotal: 3, first: "beta",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := store.List(ctx, tt.query)
            if err != nil {
                t.Fatalf("List() error = %v", err)
            }
            if len(result.Records) != tt.want {
                t.Errorf("List() returned %d records, want %d", len(result.Records), tt.want)
            }
            if tt.first != "" && len(result.Records) > 0 {
                if got := result.Records[0].StringField("key"); got != tt.first {
                    t.Errorf("first record key = %q, want %q", got, tt.first)
                }
            }
            if result.TotalItems != tt.wantTotal {
                t.Errorf("TotalItems = %d, want %d", result.TotalItems, tt.wantTotal)
            }
        })
    }
}
```

⚠️ **Separate `want` from `wantTotal`.** My first version conflated
them, and the pagination case failed: the page has 1 record but the
total is 3. The distinction matters, and the test should make it
explicit.

⚠️ **The `rank` field must be in the schema.** The first version of
this test created the collection without a `rank` field and filtered on
it, which failed:

```
invalid left operand "rank" - unknown field "rank"
```

Custom (non-schema) data is **not filterable**. This is a real
PocketBase behaviour that is easy to forget.

### Testing the remote backend

```go
func TestRemoteList(t *testing.T) {
    var gotPath string
    var gotQuery string
    var gotAuth string

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotPath = r.URL.Path
        gotQuery = r.URL.RawQuery
        gotAuth = r.Header.Get("Authorization")

        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{ /* ... */ })
    }))
    defer server.Close()

    store, err := pbstore.OpenRemote(pbstore.RemoteConfig{
        BaseURL: server.URL,
        Token:   "secret-token",
    })
    if err != nil {
        t.Fatalf("OpenRemote() error = %v", err)
    }
    defer func() { _ = store.Close() }()

    result, err := store.List(context.Background(), pbstore.Query{
        Collection: "items",
        Filter:     `key = "alpha"`,
        Sort:       "key",
    })
    if err != nil {
        t.Fatalf("List() error = %v", err)
    }

    if gotPath != "/api/collections/items/records" {
        t.Errorf("request path = %q, want %q", gotPath, "/api/collections/items/records")
    }
    if gotAuth != "secret-token" {
        t.Errorf("Authorization header = %q, want %q", gotAuth, "secret-token")
    }
    if gotQuery == "" {
        t.Error("the request carried no query parameters")
    }
    // ...
}
```

⚠️ **Assert on the wire format, not just the result.** The path, the
query and the headers. A mock that ignores the query would let a broken
filter pass the test.

## 18.6 Golden tests

A golden test compares the rendered output against a stored file.

```go
func TestViewGolden(t *testing.T) {
    m, _ := newTestModel(t)

    golden := golden.T(t, m.View())

    if diff := golden.Diff(m.View()); diff != "" {
        t.Errorf("View() mismatch:\n%s", diff)
    }
}
```

The `charmbracelet/x/exp/golden` package provides this:

```go
import "github.com/charmbracelet/x/exp/golden"

func TestGolden(t *testing.T) {
    golden.T(t, actual)
}
```

It writes the golden file if it does not exist, and compares it
otherwise. Run with `-update` to regenerate.

### The problems with golden tests

⚠️ **They are brittle.** Any change to a space, a colour or a word
breaks them. The diff is usually not informative.

⚠️ **They are hard to review.** A 200-line golden file in a pull
request is noise.

⚠️ **The colours are in the file**, so the golden depends on the
terminal's capabilities. On a CI machine without colour support, the
output differs.

### When they are worth it

- A **complex layout** where the structure matters (a table with
  borders).
- A **regression guard** on a rendering that took a long time to get
  right.
- A **snapshot** of a JSON structure.

### The alternative

Assert on *properties* of the output rather than the whole string:

```go
view := m.View()

if !strings.Contains(view, "Go") {
    t.Error("the view does not contain the record title")
}
if strings.Contains(view, "goroutine") {
    t.Error("the view contains a stack trace")
}
```

⚠️ **A weaker assertion, but a much more maintainable one.** Use
property assertions for most things, and golden tests for the few
places where the exact output is the point.

## 18.7 The race detector

The most valuable tool for a Bubble Tea application:

```bash
go test -race ./...
```

It finds the command-reads-the-model bug (Chapter 7).

⚠️ **The race detector needs the concurrent path to execute.** A race
in a rarely-taken branch may not be caught. Write a test that exercises
it:

```go
func TestModelReadRace(t *testing.T) {
    m := &raceModel{}

    cmd := m.load()

    done := make(chan struct{})
    go func() {
        cmd()
        close(done)
    }()

    for i := 0; i < 1000; i++ {
        m.Update(incrementMsg{})
    }

    <-done
}
```

Run it with `-race` and the violation is reported with both stack
traces.

## 18.8 Integration tests

### The full stack

Boot the embedded store, seed it, and drive the model:

```go
func TestIntegrationBrowseRecords(t *testing.T) {
    store := newEmbeddedStore(t)
    ctx := context.Background()

    if _, err := store.EnsureDictionaryCollection(ctx, "vocab", nil); err != nil {
        t.Fatalf("EnsureDictionaryCollection() error = %v", err)
    }
    if _, err := store.Create(ctx, "vocab", map[string]any{"key": "go", "label": "Go"}); err != nil {
        t.Fatalf("Create() error = %v", err)
    }

    m := app.New(store, config.Default(), nil)

    // Select the collection.
    m.Update(pages.HomeMsg{Collections: []pbstore.Collection{{Name: "vocab"}}})
    m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("enter")})

    // The records page loads.
    view := m.View()
    if !strings.Contains(view, "Records") {
        t.Errorf("View() = %q, want the records page", view)
    }
}
```

⚠️ **The asynchronous parts are the problem.** The page's `Load` is a
command, so the records arrive asynchronously. In a test, you have two
options:

1. **Call the command synchronously** and feed the message back:

```go
msg := m.records.Load()()      // run the command now
m.Update(msg)                  // feed the result back
```

⚠️ **But `m.records` is unexported.** The test would need an accessor,
or the test would live in the `app` package rather than `app_test`.

2. **Use the exported flow**: send the message that the command would
   have produced.

The second is cleaner and keeps the test in the external test package.

### Testing the sync end to end

Covered in Chapter 17. The mock server returns a fixed payload, and
the test asserts on the local state afterwards.

## 18.9 Testability as a design constraint

The techniques above are not just test tricks; they are design
constraints that produce better code.

### Inject the dependencies

```go
func NewRecordsPage(store pbstore.Store, runtime *state.Runtime, t *theme.Theme, renderer components.RecordRenderer) *RecordsPage
```

Because the store is injected, the page is testable. Because the
renderer is injected, the domain look is testable. Because the theme is
injected, the rendering is deterministic.

⚠️ **A page that constructs its own store is untestable.** This is
the strongest argument for dependency injection in a TUI.

### Keep the model free of globals

```go
// ✗ A global makes the tests order-dependent.
var currentCollection string

// ✓ A field makes the tests independent.
type RecordsPage struct {
    runtime *state.Runtime
}
```

### Make the messages carry the context

```go
type RecordsMsg struct {
    Result *pbstore.PageResult
    Err    error
    Page   int      // the page that was requested
}
```

The `Page` field makes the staleness check possible *and* testable.

## 18.10 What to test

A useful priority list:

| Priority | What                                   | How                        |
| --------- | -------------------------------------- | -------------------------- |
| 1         | The **data layer**                     | A real embedded database    |
| 2         | The **filter grammar**                 | Table-driven listings       |
| 3         | The **state transitions**              | Direct `Update` calls      |
| 4         | The **error paths**                    | Stub stores that fail      |
| 5         | The **rendering**                      | Property assertions        |
| 6         | The **exact layout**                   | Golden files (sparingly)   |

⚠️ **Test the error paths.** A TUI that crashes on a database error is
worse than one that shows an error message. The stub store makes this
easy:

```go
func TestModelErrorHandling(t *testing.T) {
    m, _ := newTestModel(t)

    updated, _ := m.Update(pages.RecordsMsg{Err: errTest})
    m = updated.(*app.Model)

    if m.View() == "" {
        t.Error("View() returned an empty string after an error")
    }
}
```

## 18.11 CI

The template's CI runs:

```yaml
      - name: Run the tests
        run: go test -race -coverprofile=coverage.out ./...
```

⚠️ **Always run the tests with `-race` in CI.** A race that only
appears in production is the worst kind of bug.

## 18.12 Exercises

1. Write a test for every page that covers: the initial render, a
   message handled, and an error.

2. Write a stub store that fails every method, and drive every page
   through it. Assert that nothing panics.

3. Write a golden test for the record card. Change the theme and
   regenerate it. Observe how noisy the diff is.

4. Write a test that reproduces the command-reads-the-model race, and
   verify that the race detector catches it.

5. Write an integration test that: boots the store, seeds it, starts
   the model, navigates to the records page, applies a filter, and
   asserts on the result.

6. Measure the test coverage of your application. Identify the
   uncovered branches. Are they the important ones?

## 18.13 What you learned

- Bubble Tea's architecture makes the model **testable without a
  terminal**.
- Test the **model** by calling `Update` with synthetic messages.
- Test the **commands** by calling them and inspecting the message.
- Test the **store** against a real embedded database with
  `t.TempDir()`.
- Test the **remote store** against an `httptest` server, asserting on
  the **wire format**.
- Use **property assertions** rather than golden files, mostly.
- **Always run the tests with `-race`.**
- The stub store's **compile-time** breakage when the interface grows
  is a feature.
- Dependency injection is not a test trick — it is what makes the code
  testable.

## 18.14 Further reading

- `internal/app/app_test.go` — the root model tests.
- `internal/pages/pages_test.go` — the page tests with a stub store.
- `internal/pbstore/embedded_test.go` — the real-database tests.
- `internal/pbstore/remote_test.go` — the HTTP tests.
- `internal/components/components_test.go` — the component tests.

Next: [Chapter 19 — Performance](ch19-performance.md).
