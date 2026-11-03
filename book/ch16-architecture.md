# Chapter 16 — Architecture for TUI + Database Applications

## 16.1 The shape of the problem

You now have two libraries that each impose a discipline:

- **Bubble Tea** says: all state flows through one `Update`, all I/O
  happens in commands, all rendering is a pure function.
- **PocketBase** says: the schema is dynamic, the queries are
  string-based, the record is a map.

Put them together and you get a specific set of problems:

1. **Who owns the query?** The page, the root model, or a data layer?
2. **How do the two backends stay in sync?** Embedded and remote must
   behave identically.
3. **How do the pages stay independent?** A record list and a detail
   view both need records.
4. **Where do the "loading" and "error" states live?** Every page has
   them.
5. **How do you test it?** A TUI test needs a terminal; a database test
   needs a database.

The Flint template answers these with a specific layering, and this
chapter explains the reasoning behind each layer.

## 16.2 The layers

```
┌───────────────────────────────────────────────────────────────┐
│ cmd/tui — the entry point                                    │
│   parse the flags, resolve the config, open the store,        │
│   run the program, close the store                            │
└────────────────────────────┬──────────────────────────────────┘
                             │
┌────────────────────────────▼──────────────────────────────────┐
│ internal/app — the root model                                 │
│   the page routing, the global hotkey layer, the status bar,  │
│   the toast notifications                                     │
└────────────────────────────┬──────────────────────────────────┘
                             │
┌────────────────────────────▼──────────────────────────────────┐
│ internal/pages — the screens                                  │
│   home, records, detail, search, settings, sync               │
└────────────────────────────┬──────────────────────────────────┘
                             │
┌────────────────────────────▼──────────────────────────────────┐
│ internal/components — the reusable widgets                    │
│   record card, JSON viewer, JSON editor, pagination,          │
│   status bar, toast                                           │
└────────────────────────────┬──────────────────────────────────┘
                             │
┌────────────────────────────▼──────────────────────────────────┐
│ internal/pbstore — the data layer                             │
│   the Store interface, the embedded and remote backends       │
└────────────────────────────┬──────────────────────────────────┘
                             │
                    ┌────────┴────────┐
                    ▼                 ▼
              Embedded PocketBase  Remote PocketBase
```

The dependency arrows point **downward only**. `pbstore` does not know
that pages exist. `pages` does not know that `app` exists.

⚠️ **This is the single most important structural decision.** It is
what lets you replace any layer without touching the others.

## 16.3 The data layer: one interface

The `pbstore` package exposes exactly one contract:

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

### Why this shape

**Every method takes a `context.Context`.** Because these calls block,
and a TUI must be able to cancel them.

**The records are backend-agnostic.** `pbstore.Record` is not
`core.Record`:

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

⚠️ **This is a deliberate information loss.** The embedded backend
*could* return the `*core.Record`, which has more (the collection, the
original data, the visibility flags). But then the pages would depend
on `core`, and the remote backend could not produce one.

The price is that the pages cannot use the PocketBase-specific record
API. The benefit is that the pages work identically in both modes.

⚠️ **Do not "optimise" this by returning `core.Record`.** You will
couple the pages to PocketBase, and the remote mode will break.

**The query is a struct, not a parameter list.**

```go
type Query struct {
    Collection string
    Filter     string
    Sort       string
    Page       int
    PerPage    int
    Expand     string
    SkipTotal  bool
    Fields     string
}
```

⚠️ **Do not add a parameter for every filter field.** The whole point
of the native filter grammar is that one string expresses the whole
predicate. A `Query` with `Status string, Tag string, MinRank int`
would be a worse filter language.

**The errors are domain-neutral.**

```go
var (
    ErrNotFound             = errors.New("pbstore: not found")
    ErrCollectionNotAllowed = errors.New("pbstore: collection is not allowed")
    ErrInvalidFilter        = errors.New("pbstore: invalid filter")
    ErrInvalidSort          = errors.New("pbstore: invalid sort")
    ErrClosed               = errors.New("pbstore: store is closed")
    ErrNotConnected         = errors.New("pbstore: not connected")
)
```

The embedded backend wraps `sql.ErrNoRows` into `ErrNotFound`, so the
pages never import `database/sql`.

### The allowlist

Both backends support a collection allowlist:

```go
if !s.isAllowed(q.Collection) {
    return nil, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, q.Collection)
}
```

This is a **security feature**, not a convenience. In remote mode, a
user who can type a collection name should not be able to read a
collection the operator did not intend to expose.

⚠️ **An empty allowlist means "everything is allowed".** That is a
reasonable default for an embedded application (the user owns the
database) but a risky one for a remote server.

## 16.4 The runtime state

```go
type Runtime struct {
    Page        Page
    PreviousPage Page
    Collection  string
    Collections []pbstore.Collection
    Filter      string
    Sort        string
    PageNumber  int
    PerPage     int
    SelectedID  string
    Connection  ConnectionState
    ConnectionDetail string
    LastCheck   time.Time
    Sync        SyncState
    SyncProgress string
    LastError   string
    RawJSON     bool
}
```

The rule from Chapter 2: **shared state is what two pages might both
care about**.

Let us audit each field:

| Field            | Shared? | Why                                                     |
| ---------------- | ------- | ------------------------------------------------------- |
| `Page`           | ✅      | The router needs it                                     |
| `Collection`     | ✅      | The list, the search and the sync all target it         |
| `Filter`         | ✅      | The list and the search share it                        |
| `SelectedID`     | ✅      | The detail page reads it                                |
| `Connection`     | ✅      | The status bar shows it                                 |
| `Sync`           | ✅      | The status bar shows it                                 |
| `LastError`      | ✅      | The status bar shows it                                 |
| `RawJSON`        | ❓      | Only the detail page uses it                            |

`RawJSON` is a borderline case. It is currently in the runtime because
the global hotkey layer needs to know about it (the `r` binding
conflict from Chapter 4). If the hotkey conflict were solved
differently, it would move into the detail page.

⚠️ **Audit your shared state regularly.** Every field in the runtime
is a claim that two pages care about it. When that claim is wrong, the
field belongs in a page.

## 16.5 The page model

Each page is a self-contained `tea.Model`:

```go
type RecordsPage struct {
    // Injected dependencies.
    store   pbstore.Store
    runtime *state.Runtime
    theme   *theme.Theme
    renderer components.RecordRenderer

    // Page-local state.
    table      table.Model
    filter     textinput.Model
    spinner    spinner.Model
    pagination *components.Pagination
    loading    bool
    records    []*pbstore.Record
    width      int
    height     int
    filterFocused bool
}
```

Three categories of field:

1. **Injected dependencies** — the store, the shared runtime, the
   theme, and the domain renderer.
2. **Bubbles components** — the widgets the page renders.
3. **Page-local state** — the scalars and slices the page owns.

### The `Refresh` convention

Every page that loads data exposes a `Refresh()`:

```go
// Refresh reloads the current page of records.
//
// It starts the spinner so that the loading state is visible.
func (p *RecordsPage) Refresh() tea.Cmd {
    p.loading = true
    return tea.Batch(p.spinner.Tick, p.Load())
}
```

⚠️ **This exists because `Init()` is only called once.** A page
created at startup gets its `Init`; a page the user navigates to does
not. Without `Refresh`, the second page would never load anything.

This was a real bug in the template during development: the records
page showed "no records" because it was never told to load.

### The load command

```go
func (p *RecordsPage) Load() tea.Cmd {
    store := p.store
    collection := p.runtime.Collection
    filter := p.runtime.Filter
    sortExpr := p.runtime.Sort
    page := p.runtime.PageNumber
    perPage := p.runtime.PerPage

    return func() tea.Msg {
        if store == nil || collection == "" {
            return RecordsMsg{Err: fmt.Errorf("pages: no collection selected"), Page: page}
        }

        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        result, err := store.List(ctx, pbstore.Query{
            Collection: collection,
            Filter:     filter,
            Sort:       sortExpr,
            Page:       page,
            PerPage:    perPage,
        })
        if err != nil {
            return RecordsMsg{Err: err, Page: page}
        }
        return RecordsMsg{Result: result, Page: page}
    }
}
```

Read it as a checklist:

- ✅ captures values, not the model;
- ✅ returns a message, not an error;
- ✅ has a timeout;
- ✅ includes the request parameters, so the handler can discard a
  stale response.

## 16.6 The root model

The root model is the coordinator. It does four things:

1. **Owns the pages** and the shared state.
2. **Routes messages** to the right owner.
3. **Provides the global hotkey layer**.
4. **Renders the global chrome** (status bar, toast).

### The routing

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmds []tea.Cmd

    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
        m.resize()

    case tea.KeyPressMsg:
        if cmd, handled := m.handleGlobalKey(msg); handled {
            m.refreshStatusBar()
            return m, cmd
        }

    case pages.NavigateMsg:
        return m, m.navigate(msg.Page)

    case pages.OpenRecordMsg:
        m.runtime.SelectedID = msg.ID
        return m, m.navigate(state.PageDetail)

    case pages.RecordsMsg:
        if msg.Err != nil {
            m.runtime.SetError(msg.Err)
            return m, m.toast.Show(components.ToastError, msg.Err.Error())
        }
        m.runtime.SetError(nil)

    case components.ToastMsg:
        return m, m.toast.Show(msg.Level, msg.Text)

    case components.ToastExpiredMsg:
        updated, cmd := m.toast.Update(msg)
        m.toast = updated.(*components.Toast)
        return m, cmd
    }

    // Forward the message to the active page.
    cmds = append(cmds, m.updateActivePage(msg)...)

    // The status bar reflects the shared runtime state, so it is refreshed
    // on every update cycle.
    m.refreshStatusBar()

    return m, tea.Batch(cmds...)
}
```

⚠️ **The early `return`s skip the trailing `refreshStatusBar()`.**
This was a real bug: the status bar only updated on navigation, so the
connection state stayed "unknown" until the user changed page.

Two ways to fix it. The template's current fix is to call
`m.refreshStatusBar()` explicitly in each early-return branch. A more
robust alternative:

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    cmd := m.update(msg)
    m.refreshStatusBar()
    return m, cmd
}

// update handles the message and returns the command.
func (m *Model) update(msg tea.Msg) tea.Cmd {
    // ... the switch, with plain returns of a Cmd
}
```

The second shape is better because it makes it *impossible* to forget
the refresh. It costs one extra function.

### The navigation

```go
func (m *Model) navigate(page state.Page) tea.Cmd {
    m.runtime.SetPage(page)
    m.refreshStatusBar()

    return m.loadPage(page)
}

func (m *Model) loadPage(page state.Page) tea.Cmd {
    switch page {
    case state.PageRecords:
        return m.records.Refresh()
    case state.PageDetail:
        if m.runtime.SelectedID == "" {
            return nil
        }
        return m.detail.Refresh()
    case state.PageSearch:
        return m.search.Refresh()
    case state.PageHome:
        return m.home.Refresh()
    default:
        return nil
    }
}
```

⚠️ **Navigation must trigger the load.** Otherwise the user arrives at
an empty page. This is the `Refresh` convention from §16.5.

## 16.7 The global chrome

### The status bar

```go
func (m *Model) statusInfo() components.StatusInfo {
    return components.StatusInfo{
        Mode:            m.store.Mode().String(),
        Connection:      string(m.runtime.Connection),
        ConnectionDetail: m.runtime.ConnectionDetail,
        Collection:      m.runtime.Collection,
        Sync:            string(m.runtime.Sync),
        SyncProgress:    m.runtime.SyncProgress,
        Page:            m.runtime.Page.Title(),
        Error:           m.runtime.LastError,
        Hints:           m.hints(),
    }
}
```

The status bar is a **pure projection** of the runtime state. It holds
no state of its own beyond the width.

### The toast

```go
case components.ToastMsg:
    return m, m.toast.Show(msg.Level, msg.Text)
```

The toast is global chrome, so it lives in the root model, not in a
page.

### The composition

```go
func (m *Model) View() tea.View {
    if m.quitting {
        return tea.NewView("")
    }

    var body string
    switch m.runtime.Page {
    case state.PageRecords:
        body = m.records.View()
    // ...
    default:
        body = m.home.View()
    }

    parts := []string{body}

    if toast := m.toast.View(); toast != "" {
        parts = append(parts, toast)
    }

    parts = append(parts, m.statusBar.View())

    return tea.NewView(joinLines(parts))
}
```

## 16.8 The extension points

The template's value is that it is *extensible without being modified*.
There are five extension points.

### 1. The record renderer

```go
type RecordRenderer interface {
    Title(rec *pbstore.Record) string
    Summary(rec *pbstore.Record) string
    Fields(rec *pbstore.Record) []FieldRow
}
```

Injected into the root model:

```go
model := app.New(store, cfg, &myapp.TermRenderer{})
```

Used by the record table, the record card and the search results.

### 2. The custom pages

A page implements `tea.Model` and is registered in the root model.

### 3. The custom migrations

```go
func appMigrations() []core.Migration {
    return myapp.Migrations()
}
```

### 4. The custom sync strategy

The `SyncCollection` method on the `Store` interface is the extension
point; a downstream project can implement its own `Store` with a
different sync.

### 5. The theme

Add a variant to `theme.Palette`.

⚠️ **Do not add extension points that require modifying the core.** An
extension point that lives in `internal/app/app.go` is not an extension
point; it is a patch. The renderer is a good example: it lives in
`internal/components`, is passed through `app.New`, and requires no
core change.

## 16.9 The layering rules

Let us state the rules explicitly, because they are what makes the
architecture work.

### Rule 1: the dependency arrows point down

```
app → pages → components → pbstore → pocketbase
```

Never the other way. A cycle in Go is a compile error, so this is
enforced — but you can break it by moving a message into the wrong
package.

### Rule 2: cross-layer messages live in the lower layer

```go
// internal/pages/common.go
type NavigateMsg struct {
    Page state.Page
}

type OpenRecordMsg struct {
    ID string
}
```

`pages` defines them, `app` consumes them.

### Rule 3: the data layer knows nothing about the UI

`pbstore` does not import `pages`, `components`, `theme` or `app`. It
returns data and errors; it does not know what a toast is.

⚠️ **This is what makes the store testable.** The `pbstore` tests do
not import a single TUI package.

### Rule 4: the pages know nothing about each other

A page reaches another page only through a message handled by the root
model.

### Rule 5: the runtime is volatile

Anything that must survive a restart is in a collection or a config
file.

## 16.10 A worked flow

Let us trace the "open a record" flow through every layer.

```
User presses "enter" on the records page
   │
   ▼
RecordsPage.Update(tea.KeyPressMsg{...})
   │   the page finds the selected record
   │   returns: func() tea.Msg { return OpenRecordMsg{ID: rec.ID} }
   ▼
[command goroutine] the message is delivered to the root model
   │
   ▼
Model.Update(pages.OpenRecordMsg{ID: "rec1"})
   │   m.runtime.SelectedID = "rec1"
   │   m.navigate(state.PageDetail)
   │       → m.runtime.SetPage(state.PageDetail)
   │       → m.loadPage(state.PageDetail)
   │           → m.detail.Refresh()
   │               → tea.Batch(spinner.Tick, detail.Load())
   ▼
[command goroutine] the detail page fetches the record
   │
   ▼
DetailPage.Update(pages.DetailMsg{Record: rec})
   │   the page stores the record and updates the card and the viewer
   ▼
Model.Update(pages.DetailMsg{...})
   │   the root model forwards it to the active page (the detail page)
   ▼
View() → the detail page renders the record card and the JSON tree
```

Every step is a message. There is no direct call from the records page
to the detail page, and no direct call from the detail page to the
store.

## 16.11 Exercises

1. Draw the dependency graph of your own application and check that
   every arrow points down.

2. Take the template and add a page that displays a counter. Register
   it in the root model. Verify that the global hotkeys still work.

3. Move `RawJSON` from the runtime into the detail page. Solve the
   `r` binding conflict. Measure how much cleaner the runtime became.

4. Write a lint-style test that asserts the `pbstore` package does not
   import any TUI package.

5. Add a `Store` implementation that is neither embedded nor remote —
   a fixed in-memory one for tests. Verify that every page works with
   it unchanged.

6. Trace the "trigger a sync" flow. Write down every message and
   every command.

## 16.12 What you learned

- The layering is **app → pages → components → pbstore**, and the
  arrows point **downward only**.
- The `Store` interface is the **stable contract**: contexts on every
  call, backend-agnostic records, a struct-based query, domain-neutral
  errors.
- The records are **deliberately less rich** than `core.Record`, so the
  pages stay backend-agnostic.
- The shared runtime holds only **global facts**; everything else is
  page-local.
- Every data-loading page exposes a **`Refresh()`**, because `Init()`
  is only called once.
- The root model **routes**, **owns the global chrome** and provides
  the **global hotkey layer**.
- Cross-layer messages live in the **lower** layer.
- The seven extension points let you specialise the template **without
  modifying the core**.

## 16.13 Further reading

- `internal/app/app.go` — the root model.
- `internal/state/state.go` — the runtime.
- `internal/pages/records.go` — the canonical page.
- `internal/pages/common.go` — the cross-layer messages.
- `internal/pbstore/store.go` — the contract.
- `internal/agent/run.go` — the agent runner, the other kind of loop.

Next: [Chapter 17 — The pbstore Pattern: One Interface, Two Backends](ch17-pbstore-pattern.md).
