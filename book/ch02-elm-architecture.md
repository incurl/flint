# Chapter 2 — The Elm Architecture

## 2.1 Why Elm's ideas travel well

Elm is a pure functional language for the web. Its architecture has
three ideas that map perfectly onto a terminal application:

1. **A single immutable state value.**
2. **A single pure function** that maps `(state, message)` to
   `(new state, effects)`.
3. **A declarative render function** that maps state to a description of
   what should be on screen.

Elm's discipline buys you two enormous things: you can *reason* about
the program (the type checker proves the state transitions are total),
and you can *test* it (the pure function is trivially testable without
a terminal).

Bubble Tea adopts the shape but relaxes the discipline to idiomatic Go.
You get the *architecture* — one update function, one render function,
effects as values — without the language's purity requirements.

## 2.2 The three functions, precisely

### `Model` is the entire state

```go
type Model interface {
    Init() Cmd
    Update(Msg) (Model, Cmd)
    View() string
}
```

That is the whole interface. Note what is *not* there: no `SetState`,
no event handlers, no `Render` tree.

The practical consequence: **you can print your entire application
state** and understand what the program is doing. When debugging a TUI,
the fastest path is usually to add a debug line that dumps the state.

### `Update` is a transition function

Conceptually:

```
Update : (Model, Msg) -> (Model, Cmd)
```

It is a *pure function of its arguments* in the Elm sense. In Go, with a
pointer receiver, it mutates the receiver and returns it — but the
discipline is the same: given the same starting state and the same
message, the result should be the same.

The `Cmd` it returns is the "effects" half. Elm would model effects as
a list of commands; Bubble Tea returns at most one, but `tea.Batch`
composes several.

### `View` is a projection

```
View : Model -> String
```

It reads state and produces a frame. It does not modify state. It does
not perform I/O. It does not block.

## 2.3 Value receiver vs. pointer receiver

This is the first decision every Bubble Tea author makes, and it is
worth getting right.

### Value receivers

```go
type model struct {
    count int
    items []string
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg.(type) {
    case tickMsg:
        m.count++
    }
    return m, nil
}
```

Pros:

- The state is genuinely immutable in the model, which is the Elm
  ideal.
- Accidental aliasing bugs are less likely, because you return a copy.
- It works with `tea.Model` values, so you can construct and inspect
  models in tests without pointers.

Cons:

- Copying a large model on every update costs. With slices and maps
  inside, the copy is shallow, so it is cheap — but it is not free.
- The idiom "modify a slice" requires the awkward
  `append(append([]T{}, old...), new...)` dance.

### Pointer receivers

```go
type model struct {
    count int
    items []string
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg.(type) {
    case tickMsg:
        m.count++
    }
    return m, nil
}
```

Pros:

- Idiomatic Go. `m.count++` just works.
- No copying.
- Composes naturally with other pointer-receiver methods.

Cons:

- Aliasing is easier to get wrong.
- `return m, nil` returns a `*model` boxed in a `tea.Model`; you must
  remember that the concrete type is now a pointer.

### The rule of thumb

> **Use a pointer receiver once the model contains anything you mutate
> in place — slices, maps, buffers — and a value receiver for models
> that are essentially a few scalars.**

The Flint template uses a pointer receiver for the root model:

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    ...
    return m, tea.Batch(cmds...)
}
```

because it holds pages, a runtime state and components that all
contain slices and maps.

⚠️ **A subtle bug with pointer receivers.** If you store the model in a
variable and the program replaces it (which it does on every `Update`),
your variable can go stale:

```go
m := &Model{}
p := tea.NewProgram(m)
// Inside Update: m is replaced with a new *Model.
// Your local `m` still points at the old one.
```

Bubble Tea handles this correctly — it uses whatever `Update` returns.
But if *you* hold a reference to the model elsewhere (in a command
closure, say), you have a bug. The rule from Chapter 1 applies:
**commands must not capture the model.**

## 2.4 State ownership: who owns what

A multi-page application needs to decide where state lives. There are
three common arrangements.

### Arrangement A — everything in the root model

```go
type Model struct {
    records    []*Record
    filter     string
    page       int
    showDetail bool
    detail     *Record
    // ... 30 more fields
}
```

Simple to start with. Becomes unmanageable fast: the root model's
`Update` grows a hundred-case switch, and pages cannot be developed
independently.

### Arrangement B — each page owns everything

```go
type recordsPage struct {
    records []*Record
    filter  string
    page    int
}

type detailPage struct {
    record *Record
    editing bool
}
```

Each page is self-contained. The root model only knows which page is
active.

Problem: some state is genuinely global — the current collection, the
connection health, the sync status. Duplicating it across pages is
wrong.

### Arrangement C — shared runtime state + page-local state ✅

This is what the Flint template does.

```go
// internal/state/state.go
type Runtime struct {
    Page        Page
    Collection  string
    Filter      string
    Sort        string
    PageNumber  int
    PerPage     int
    SelectedID  string
    Connection  ConnectionState
    Sync        SyncState
    LastError   string
    RawJSON     bool
}
```

```go
// internal/pages/records.go
type RecordsPage struct {
    store   pbstore.Store
    runtime *state.Runtime   // shared
    theme   *theme.Theme     // shared

    table      table.Model   // page-local
    filter     textinput.Model
    spinner    spinner.Model
    pagination *components.Pagination

    loading  bool            // page-local
    records  []*pbstore.Record
    width    int
    height   int
}
```

The split rule:

> **Shared state** is anything two pages might both care about and that
> must agree: the active collection, the filter, the connection health,
> the sync status.
>
> **Page-local state** is anything only that page renders: the table
> cursor, whether the filter input has focus, the loading spinner.

The shared runtime is a plain pointer, so both the root model and the
pages see the same values. Because `Update` runs on a single goroutine,
there is no locking.

⚠️ **Do not put page-local state in the shared runtime.** The moment
you do, two pages will fight over it. The `Runtime` struct in the
template is deliberately small and about *global* facts.

### A note on `Runtime` being volatile

The name is a hint: this state is **not persisted**. It describes what
the user is looking at right now. It disappears when the program exits.

If you need something to survive a restart — the last filter, a sync
timestamp, a user preference — it belongs in one of two places:

- a **PocketBase collection** (for data), or
- a **config file** (for preferences).

Chapter 17 covers the sync-timestamp case, which is the interesting one.

## 2.5 Composing pages

The root model holds a page per screen and routes messages to the
active one.

```go
// internal/app/app.go
type Model struct {
    store   pbstore.Store
    runtime *state.Runtime
    theme   *theme.Theme

    home    *pages.HomePage
    records *pages.RecordsPage
    detail  *pages.DetailPage
    search  *pages.SearchPage
    // ...
}
```

The routing is a switch on the active page:

```go
func (m *Model) updateActivePage(msg tea.Msg) []tea.Cmd {
    var cmds []tea.Cmd

    switch m.runtime.Page {
    case state.PageRecords:
        updated, cmd := m.records.Update(msg)
        m.records = updated.(*pages.RecordsPage)
        cmds = append(cmds, cmd)
    case state.PageDetail:
        updated, cmd := m.detail.Update(msg)
        m.detail = updated.(*pages.DetailPage)
        cmds = append(cmds, cmd)
    // ...
    default:
        updated, cmd := m.home.Update(msg)
        m.home = updated.(*pages.HomePage)
        cmds = append(cmds, cmd)
    }

    return cmds
}
```

Two things to notice.

**The type assertion.** `m.records.Update(msg)` returns a `tea.Model`.
We assert it back to `*pages.RecordsPage` so we can keep the concrete
type. This is safe because the page always returns itself.

**The `default` case.** `state.PageHome` is the fallback. If a page
constant is added without updating the routing, the application silently
falls back to home rather than crashing. Not ideal, but a reasonable
failure mode.

### Navigation

Switching pages is a message, not a method call, so that a page can
request navigation without holding a reference to the root model.

```go
// internal/pages/common.go
type NavigateMsg struct {
    Page state.Page
}
```

```go
// A page asks for navigation:
return p, func() tea.Msg {
    return NavigateMsg{Page: state.PageRecords}
}
```

```go
// The root model handles it:
case pages.NavigateMsg:
    return m, m.navigate(msg.Page)
```

⚠️ **Why the message lives in `pages` and not in `app`.** If
`NavigateMsg` were defined in `app` and `pages` imported `app` to use
it, and `app` imported `pages` to hold the pages, you have an **import
cycle**. Go forbids that.

The fix is to put the message in the *lower* package (`pages`) and have
the higher package (`app`) import it. The dependency arrow points one
way: `app → pages`. The pages never import `app`.

This is a general rule for layered architectures in Go: **messages
travel down, commands travel up.** The lower layer defines the
vocabulary; the higher layer interprets it.

## 2.6 The render composition

The root model's `View` composes the active page with the global
chrome:

```go
func (m *Model) View() string {
    if m.quitting {
        return ""
    }

    var body string
    switch m.runtime.Page {
    case state.PageRecords:
        body = m.records.View()
    case state.PageDetail:
        body = m.detail.View()
    // ...
    default:
        body = m.home.View()
    }

    parts := []string{body}

    if toast := m.toast.View(); toast != "" {
        parts = append(parts, toast)
    }

    parts = append(parts, m.statusBar.View())

    return joinLines(parts)
}
```

Three regions, always in the same order:

```
┌──────────────────────────────┐
│                              │
│         page content         │
│                              │
├──────────────────────────────┤
│        toast (optional)      │
├──────────────────────────────┤
│         status bar           │
└──────────────────────────────┘
```

The status bar is always last, so it is always at the bottom of the
terminal. The toast sits just above it, so it does not cover the
content.

⚠️ **A common mistake: rendering the status bar first.** If you build
the string as `statusBar + "\n" + body`, the status bar appears at the
*top*. It is tempting because the status bar is "global" and feels like
a header, but the convention in every TUI is that persistent chrome
goes at the bottom, above the shell prompt.

## 2.7 A worked example: the toast

Let us build a small feature end to end, because it touches every part
of the architecture.

**Requirement:** show a transient message when an operation completes,
and hide it after a few seconds.

### Step 1 — the message and the component

```go
// internal/components/toast.go
type ToastLevel int

const (
    ToastInfo ToastLevel = iota
    ToastSuccess
    ToastWarning
    ToastError
)

type ToastMsg struct {
    Level    ToastLevel
    Text     string
    Duration time.Duration
}
```

The component holds the state and knows how to render itself:

```go
type Toast struct {
    theme     *theme.Theme
    level     ToastLevel
    text      string
    expiresAt time.Time
    visible   bool
    width     int
}
```

### Step 2 — showing it returns a command

```go
func (t *Toast) Show(level ToastLevel, text string) tea.Cmd {
    duration := DefaultToastDuration
    t.level = level
    t.text = text
    t.visible = true
    t.expiresAt = time.Now().Add(duration)

    return tea.Tick(duration, func(time.Time) tea.Msg {
        return ToastExpiredMsg{}
    })
}
```

Note the shape: `Show` both **mutates the component** and **returns a
command that will later mutate it again**. The first mutation is
immediate (the toast appears now); the second is deferred (the toast
disappears later).

This is the idiomatic Bubble Tea pattern for timed state changes.

### Step 3 — the component handles its own messages

```go
func (t *Toast) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case ToastExpiredMsg:
        t.visible = false
        t.text = ""
    case tea.WindowSizeMsg:
        t.width = msg.Width
    }
    return t, nil
}
```

### Step 4 — the root model routes to it

```go
// Showing a toast.
case components.ToastMsg:
    return m, m.toast.Show(msg.Level, msg.Text)

// The expiry message must reach the toast component.
case components.ToastExpiredMsg:
    updated, cmd := m.toast.Update(msg)
    m.toast = updated.(*components.Toast)
    return m, cmd
```

⚠️ **This is a real bug I hit while building the template.** The expiry
message was initially swallowed by the root model's switch, so the
toast never disappeared. The symptom was subtle: the toast appeared
correctly, and simply stayed there forever.

The lesson: **when a component schedules its own messages, the root
model must forward them.** A catch-all `default:` that forwards
everything to the active page is *not* enough, because the toast is
not the active page — it is global chrome.

### Step 5 — pages request toasts

Pages never touch the toast component. They emit a message:

```go
// In a page:
if err != nil {
    return p, func() tea.Msg {
        return components.ToastMsg{
            Level: components.ToastError,
            Text:  err.Error(),
        }
    }
}
```

And the root model turns it into a toast. This keeps the pages
independent of the toast implementation — you could swap it for a
different notification system without touching a single page.

## 2.8 Designing your own message set

A practical checklist for the messages in an application like ours:

| Message               | Produced by        | Handled by        |
| --------------------- | ------------------ | ----------------- |
| `tea.KeyMsg`          | the terminal       | global + page     |
| `tea.WindowSizeMsg`   | the terminal       | root + components |
| `recordsLoadedMsg`    | a page command     | the records page  |
| `loadFailedMsg`       | a page command     | the records page  |
| `NavigateMsg`         | a page             | the root model    |
| `OpenRecordMsg`       | a page             | the root model    |
| `ToastMsg`            | anywhere           | the root model    |
| `ToastExpiredMsg`     | the toast component| the root model    |

Two rules make the set coherent:

1. **A message has exactly one owner.** The component or page that
   understands its meaning handles it. Everyone else ignores it.
2. **A message that crosses a layer boundary is defined in the lower
   layer.** `NavigateMsg` is in `pages` because `pages` produces it and
   `app` consumes it.

## 2.9 Exercises

1. Refactor the Chapter 1 counter to use a pointer receiver. Confirm
   the behaviour is unchanged.

2. Write a program with two slices in the model and demonstrate the
   aliasing bug: mutate the slice in place inside `Update`, and observe
   that a previously captured copy sees the change. Then fix it with
   the copy-on-write idiom.

3. Take the Flint template and move `RawJSON` from `state.Runtime`
   into `DetailPage`. What breaks? (Hint: the raw-JSON toggle is a
   page-local concern; nothing else needs it. This exercise is about
   noticing that a field in the shared runtime is a smell.)

4. Design the message set for a hypothetical "favourites" feature:
   the user can mark a record as a favourite from the list page, and
   the home page shows the count. Write the message type and say which
   layer owns it.

5. Add a third global component to the template — a footer showing the
   current time. Where does the message that drives it live? Which
   layer owns it? Verify the toast still expires correctly afterwards.

## 2.10 What you learned

- The Elm architecture is `Model` / `Update` / `View`, with effects
  returned as values.
- Choose **value receivers** for scalar models, **pointer receivers**
  once you mutate slices or maps.
- Split state into **shared runtime** (global facts) and **page-local**
  (what one screen renders).
- **Messages travel down, commands travel up.** Define cross-layer
  messages in the lower package to avoid import cycles.
- Persistent state belongs in a **database** or a **config file** —
  never in the volatile runtime.
- A component that schedules its own messages needs the root model to
  **forward them**.

## 2.11 Further reading

- `internal/app/app.go` — the root model: routing, the global hotkey
  layer, the status bar wiring.
- `internal/state/state.go` — the shared runtime.
- `internal/pages/common.go` — the cross-layer messages.
- `internal/components/toast.go` — a complete component with a timer.

Next: [Chapter 3 — Messages, Commands, and the Event Loop](ch03-messages-and-commands.md).
