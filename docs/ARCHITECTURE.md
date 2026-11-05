# Architecture

This document explains how Flint is put together, why it is shaped this
way, and — most importantly — **what you may safely change** once you have
clicked *Use this template*.

## The big picture

Flint is a single-process terminal application built on three layers:

```
┌─────────────────────────────────────────────────────────────────┐
│  cmd/tui — the entry point                                     │
│  parses the flags, resolves the config, opens the store, runs   │
│  the Bubble Tea program and closes the store on exit.           │
└───────────────────────────────┬─────────────────────────────────┘
                                │
┌───────────────────────────────▼─────────────────────────────────┐
│  internal/app — the root model                                 │
│  owns the page routing, the global hotkey layer, the status bar │
│  and the toast notifications.                                   │
└───────────────────────────────┬─────────────────────────────────┘
                                │
┌───────────────────────────────▼─────────────────────────────────┐
│  internal/pages — the screens                                  │
│  home, records, detail, search, settings, sync.                 │
│  Each one is a self-contained tea.Model.                        │
└───────────────────────────────┬─────────────────────────────────┘
                                │
┌───────────────────────────────▼─────────────────────────────────┐
│  internal/pbstore — the data layer                             │
│  the Store interface and its two implementations.                │
└───────────────────────────────┬─────────────────────────────────┘
                                │
                ┌───────────────┴───────────────┐
                ▼                               ▼
        Embedded PocketBase            Remote PocketBase
        (App + App.Store, local db)     (REST API)
```

## The Bubble Tea flow

Flint follows the strict, one-way Bubble Tea architecture. There are
exactly three kinds of messages, and each one has a single owner.

### 1. Global messages

Handled by the root model (`internal/app`):

| Message              | Meaning                                        |
| -------------------- | ---------------------------------------------- |
| `app.HealthMsg`      | The result of a store health check             |
| `app.CollectionsMsg` | The collections discovered at startup          |
| `app.SyncStartedMsg` | A synchronisation has started                  |
| `app.SyncFinishedMsg`| A synchronisation has finished                 |
| `components.ToastMsg`| A transient notification                       |
| `pages.NavigateMsg`  | A page asks the root model to switch the view  |
| `pages.OpenRecordMsg`| A page asks the root model to open a record    |

### 2. Page-local messages

Handled by the page that owns them, for example `pages.RecordsMsg` or
`pages.SearchResultMsg`. They never reach the other pages.

### 3. Store errors

Store errors are **never** raised as panics. They travel as messages and
are turned into a toast plus a status bar entry:

```go
case pages.RecordsMsg:
    if msg.Err != nil {
        m.runtime.SetError(msg.Err)
        return m, m.toast.Show(components.ToastError, msg.Err.Error())
    }
```

### The purity rule

`Update` functions must not block. Every database or network call happens
inside a `tea.Cmd`, which Bubble Tea runs in a goroutine:

```go
// pages/records.go
func (p *RecordsPage) Load() tea.Cmd {
    store, collection, filter := p.store, p.runtime.Collection, p.runtime.Filter

    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        result, err := store.List(ctx, pbstore.Query{...})
        if err != nil {
            return RecordsMsg{Err: err}
        }
        return RecordsMsg{Result: result}
    }
}
```

The command returns a **message**, never an error. The error travels
*inside* the message so that the update loop stays pure.

## The global hotkey layer

The root model intercepts the keys before the active page sees them. The
layer is **suspended** while a page owns a focused text input, so that
typing a `q` in a filter does not quit the application.

```
key press
   │
   ├─▶ global layer (app.handleGlobalKey)
   │      ctrl+c        → quit
   │      1 … 6         → page switch
   │      s             → health check
   │      y             → trigger a sync
   │      esc           → previous page
   │      r             → refresh (except on the pages that bind it locally)
   │
   └─▶ active page (page.Update)
          /             → focus the filter input
          enter         → open the selected record
          …
```

The `inputFocused()` helper is what makes the suspension explicit. If you
add a new page with a text input, add it to that helper.

`modalInputFocused()` is the stricter variant, and the detail page's raw
JSON editor is the one page that uses it. A modal editor binds nothing
globally but `ctrl+c`: a JSON document is made of the very characters the
global layer binds, so `q` would quit and the digits would navigate away
from an unsaved edit.

## The package boundaries

### `internal/pbstore` — the stable contract

`pbstore.Store` is **the contract every downstream project depends on**.
Keep it minimal and stable:

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
    EnsureCollection(ctx context.Context, name string, fields []CollectionField) (Collection, error)
    CollectionSchema(ctx context.Context, name string) ([]CollectionField, error)
    SyncCollection(ctx context.Context, req SyncRequest) (*SyncReport, error)
    Close() error
}
```

`EnsureCollection` creates a collection with the fields the application
needs. The embedded backend creates it; the remote backend cannot, and
returns an error — there, the collections are created by a migration on
the server.

`CollectionSchema` reads the fields of a collection. Both backends
answer it: the embedded one from the collection itself, the remote one
from the collections endpoint of the server. It returns the fields a
caller writes — the system fields of PocketBase are left out — and a
collection that does not exist is an error, and not an empty schema.

**Safe to change:** the internals of `embedded.go` and `remote.go`, the
helper functions in `queries.go`, the `DictionaryField` and `SyncRequest`
descriptions.

**Treat as frozen:** the method names, the signatures and the semantics
of the interface. Adding a method is a breaking change for the downstream
projects that already implement it — `CollectionSchema` was added for
the agent's `collection_schema` tool, and a downstream store that
implements `pbstore.Store` has to add it too.

### `internal/theme` — the single style source

Every colour and every style lives here. **Inline lipgloss styles are
forbidden** in the rest of the code base. A component that needs a new
style gets it from the theme:

```go
// ✗ forbidden
lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))

// ✓ correct
m.theme.Error.Render("boom")
```

To restyle the whole application, add a variant to `theme.Palette` and a
branch to `theme.New`.

### `internal/components` — the reusable widgets

The components are generic and unaware of any domain schema:

| Component        | Purpose                                              |
| ---------------- | ---------------------------------------------------- |
| `RecordCard`     | Renders a record through an injectable `RecordRenderer` |
| `JSONViewer`     | Collapsible JSON tree for nested payloads            |
| `JSONEditor`     | Raw JSON editing form with validation                |
| `Pagination`     | Page navigation driven by the PocketBase metadata    |
| `StatusBar`      | The persistent bottom bar                            |
| `Toast`          | Transient notifications                              |

### `internal/pages` — the screens

Each page owns its view state and knows nothing about the other pages. A
page reaches the root model through the messages declared in
`internal/pages/common.go` (`NavigateMsg`, `OpenRecordMsg`), which avoids
an import cycle.

### `internal/state` — the volatile runtime state

`state.Runtime` holds the *current* UI state: the active page, the
active collection, the connection health, the sync status. It is
**never persisted** — anything that must survive a restart belongs in a
PocketBase collection.

### `internal/agent` — the agentic layer

The agent runs a turn: it calls a model, the model calls **tools**, the
tools read and write the store. It knows nothing about the UI — it
publishes *events*, and the page draws them.

```text
internal/pages/agent.go  ──events──►  internal/agent
       ▲                                │
       └──────── permission request ────┘
```

Three rules hold it together:

- **The agent never draws.** It publishes `agent.Event` values; the page
  turns them into entries. This is what makes the layer testable
  without a terminal.
- **The UI never sees the model.** The page receives `AgentEventMsg`,
  `AgentDoneMsg` and `AgentPermissionMsg`.
- **The tools are ordinary store calls.** They go through `pbstore`,
  so the two backends work the same way.

The mutating tools go behind a **permission gate**. A gate that cannot
ask the user **refuses** — a missing requester is an error, never a
silent approval.

The loop itself belongs to `charm.land/fantasy`: the template adds the
tools, the events, the gate and the conversation.

### The agent panel

The root model owns a **read-only panel** that shows the conversation
of the agent next to the page. It is toggled with `ctrl+b`, it is 32
columns wide, and it is refused on a terminal narrower than 121
columns — the record table is 88 columns wide, and a table whose
viewport is narrower than its columns renders *empty*.

```text
internal/app/panel.go  ──renders──►  internal/components/agentpanel.go
        ▲
        └── reads the entries of internal/pages/agent.go
```

The panel holds no conversation of its own: it renders the entries of
the agent page, so the two views cannot drift apart. The events of a
run reach the agent page whatever page is displayed, so a run keeps
streaming after the user navigates away.

The *writing* half of the agent — the prompt and the permission
questions — stays on the agent page. A prompt in a 32-column panel
would be unusable, and a permission prompt has to show what is about to
change.

### `internal/session` — the conversations

The turns of a conversation live in PocketBase, in two collections:
`_flint_sessions` and `_flint_messages`. A turn is stored as JSON,
because it carries the text, the tool calls and their results — not a
flat record.

The embedded backend creates the collections on demand; the remote one
cannot, and `session.Migrations()` creates them on the server.

## The data layer

See [`PBSTORE_GUIDE.md`](PBSTORE_GUIDE.md) for the details. The short
version:

- **Filters** use the native PocketBase grammar (`title ~ "go"`,
  `rank > 1 && rank < 3`). They are parsed by PocketBase itself, so the
  embedded and the remote backends behave identically.
- **Sorting** uses the native PocketBase syntax (`-created,name`).
- **Pagination** uses the native `page`/`perPage`/`totalItems`/
  `totalPages` metadata.
- **Records** are exposed as a plain `map[string]any` so that any
  schema works without code changes.
- **Caches and dictionaries** are ordinary PocketBase collections. There
  is no parallel `map[string]any` cache layer.

### A note on `App.Store()`

PocketBase exposes a runtime key/value store through `core.App.Store()`.
It is a **concurrent-safe in-memory store**, not a database: PocketBase
itself uses it to memoise the collection registry, the active backup
flag and the rate limiters.

Flint exposes it as `Embedded.RuntimeStore()` for *volatile* runtime data
(e.g. memoising an expensive derived value during a session). **Persistent
lookup caches belong in a PocketBase collection**, reached through
`Store.List` / `Store.FindFirst`, not in this store.

## What you can safely modify

| File / package                | Safe? | Notes                                        |
| ----------------------------- | ----- | -------------------------------------------- |
| `cmd/tui/main.go`             | ✅    | Add your migrations, change the wiring        |
| `config/`                     | ✅    | Add your own knobs                            |
| `internal/theme/`             | ✅    | Add variants and styles                        |
| `internal/components/`        | ✅    | Add widgets, keep them generic                |
| `internal/pages/`             | ✅    | Add pages, keep them self-contained           |
| `internal/state/`             | ✅    | Add volatile state                             |
| `internal/pbstore/queries.go` | ✅    | Add filter/sort helpers                        |
| `internal/pbstore/store.go`   | ⚠️    | Extend carefully — the interface is the contract |
| `internal/pbstore/embedded.go`| ⚠️    | Keep the `Store` contract intact               |
| `internal/pbstore/remote.go`  | ⚠️    | Keep the `Store` contract intact               |
| `internal/hybridstore/`       | ✅    | The JSON filter wrapper — it sits above `pbstore`, not inside it |
| `internal/jsonfilter/`        | ✅    | The local evaluator; pure functions, no state  |
| `internal/app/app.go`         | ⚠️    | Extend the routing, keep the global layer consistent |

## What to leave alone

- **The `Store` interface.** Downstream projects code against it.
- **The native filter/sort semantics.** Do not wrap them in a custom
  query language: the point is to reuse PocketBase's.
- **The one-way data flow.** No store calls from `View`, no store calls
  from `Update`.
- **The theme package.** No inline styles anywhere else.

## Testing

The template ships a table-driven test skeleton:

- `internal/pbstore/embedded_test.go` — the CRUD life cycle, the native
  filters, the allowlist, the migrations, against a **real** embedded
  PocketBase database.
- `internal/pbstore/remote_test.go` — the REST contract, against an
  `httptest` server.
- `internal/pbstore/queries_test.go` — the parameter builders.
- `internal/theme/theme_test.go` — the palettes and the layout helpers.
- `config/config_test.go` — the configuration precedence.
- `internal/app/app_test.go` — the root model routing.

Run them with:

```bash
make test
```

## Graceful shutdown

`cmd/tui/main.go` owns the shutdown sequence:

1. `SIGINT` / `SIGTERM` → `program.Quit()`.
2. Bubble Tea returns from `program.Run()`.
3. The deferred `store.Close()` releases the PocketBase resources (the
   db connections, the cron ticker).
4. The process exits.

Every store call accepts a `context.Context`, so the in-flight queries
can be cancelled instead of blocking the exit.
