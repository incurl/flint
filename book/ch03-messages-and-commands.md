# Chapter 3 — Messages, Commands, and the Event Loop

Chapter 1 introduced the event loop at a high level. This chapter gets
precise, because the details are where TUI bugs live.

## 3.1 The two halves of `Update`

Every `Update` implementation has the same shape:

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // 1. Handle the message: change the state.
    // 2. Return a command: schedule some future work.
    return m, nil
}
```

The two halves have different rules.

**State changes happen in `Update`, synchronously, immediately.** When
you handle a message, the effect is visible in the very next render.

**Work happens in commands, asynchronously, later.** A command is a
promise that "at some point, there will be another message".

This distinction explains a common confusion. When a user presses a key
and the UI does not respond, the question is always: *which half is
missing?*

- If the state did not change → the message was not handled.
- If the state changed but nothing happened next → the command was
  wrong or never returned.

## 3.2 Commands in detail

A command is `func() tea.Msg`:

```go
type Cmd func() Msg
```

Bubble Tea calls it in a goroutine and sends the returned message back
to the event loop.

### `tea.Quit`

```go
return m, tea.Quit
```

Ends the program. `tea.Quit` returns a `tea.QuitMsg`, which the program
intercepts to shut down.

### `tea.Batch`

```go
return m, tea.Batch(cmdA, cmdB, cmdC)
```

Runs all the commands **concurrently** and delivers their messages in
whatever order they finish.

Use it when several independent things must happen. The template uses it
to start a spinner and kick off a load at the same time:

```go
func (p *RecordsPage) Refresh() tea.Cmd {
    p.loading = true
    return tea.Batch(p.spinner.Tick, p.Load())
}
```

⚠️ **Batch does not preserve order.** If `cmdA` must complete before
`cmdB` starts, use `tea.Sequence`.

### `tea.Sequence`

```go
return m, tea.Sequence(cmdA, cmdB, cmdC)
```

Runs the commands **one after another**, each starting after the
previous one returns.

Use it when there is a genuine dependency:

```go
// Save the record, then reload the list.
return m, tea.Sequence(
    func() tea.Msg { return saveRecordMsg{} },
    m.loadRecords,
)
```

Note the shape: `tea.Sequence` takes `Cmd`s, and a `func() tea.Msg` is
exactly a `Cmd`. Any function returning a message can be passed
directly.

### `tea.Tick` and `tea.Every`

```go
// Once, after a delay.
tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })

// Repeatedly.
tea.Every(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
```

These are how you build animations, timeouts and polling.

⚠️ **`tea.Every` never stops on its own.** If you start one in `Init`
and the user navigates away, the tick keeps arriving forever. Two
defences:

1. Make the message harmless when nothing is listening (check a
   `visible` flag in the handler).
2. Use a `tea.Tick` that reschedules itself, and only reschedule while
   the component is active:

```go
// The spinner only ticks while it is visible.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case spinner.TickMsg:
        if !m.visible {
            return m, nil       // stop ticking
        }
        var cmd tea.Cmd
        m.spinner, cmd = m.spinner.Update(msg)
        return m, cmd           // cmd reschedules the next tick
    }
    return m, nil
}
```

The `spinner` component in Bubbles already implements exactly this
pattern — it returns a command that schedules the next tick, and the
tick stops when the spinner is not being updated.

### `tea.Println` and `tea.Printf`

```go
return m, tea.Printf("saved %s", name)
```

Prints a line above the TUI (in the normal screen buffer, not the
alternate one) and returns a message. Useful for reporting something
that must survive the UI, like a URL to open.

## 3.3 The command discipline

This is the most important section in the chapter.

### Rule 1: commands capture values, not the model

```go
// ✗ Captures the model — the command runs later, on another goroutine,
//    and reads a model that may have changed in the meantime.
func (m *Model) load() tea.Cmd {
    return func() tea.Msg {
        return loadedMsg{m.records}   // reads m.records concurrently!
    }
}

// ✓ Captures only what it needs.
func (m *Model) load() tea.Cmd {
    store, collection, page := m.store, m.runtime.Collection, m.runtime.PageNumber
    return func() tea.Msg {
        result, err := store.List(ctx, pbstore.Query{
            Collection: collection,
            Page:       page,
        })
        if err != nil {
            return loadFailedMsg{err}
        }
        return loadedMsg{result}
    }
}
```

The `✓` version is race-free because every captured value is either
immutable or a handle that is safe for concurrent use (the store is
documented as such).

⚠️ **Reading a field of the model inside the command closure is a data
race**, even if it "looks harmless". The event loop may be mutating
the model at that exact moment. The Go race detector will find it if
you run `go test -race`.

The Flint template uses this pattern consistently:

```go
func (p *RecordsPage) Load() tea.Cmd {
    store := p.store
    collection := p.runtime.Collection
    filter := p.runtime.Filter
    sortExpr := p.runtime.Sort
    page := p.runtime.PageNumber
    perPage := p.runtime.PerPage

    return func() tea.Msg {
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

### Rule 2: commands return messages, never errors

A command cannot return an error. It returns a `Msg`. If the work can
fail, the message must carry the error:

```go
// ✗ Not possible: Cmd returns Msg, not (Msg, error).
func save() tea.Cmd {
    rec, err := store.Save(data)
    return rec          // where does err go?
}

// ✓ The error travels inside the message.
type saveFailedMsg struct{ err error }

func save() tea.Cmd {
    return func() tea.Msg {
        rec, err := store.Save(data)
        if err != nil {
            return saveFailedMsg{err}
        }
        return saveDoneMsg{rec}
    }
}
```

This is not a limitation — it is a feature. It forces you to decide
*where* the error surfaces, which is usually "in the UI, as a message
the user can read".

### Rule 3: commands should be cancellable

A command that runs for 30 seconds will still be running when the user
quits. If it holds a database connection or a file handle, that leaks.

Pass a `context.Context` and cancel it when the program shuts down. The
template's store methods all take a context:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

result, err := store.List(ctx, query)
```

For the application's lifetime, create a root context in `main` and
cancel it on shutdown:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// Pass ctx into the model so that the commands can derive from it.
model := app.New(store, cfg, ctx)
```

Then in a command:

```go
return func() tea.Msg {
    ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
    defer cancel()
    // ...
}
```

The timeout and the cancellation both work: a timeout bounds a single
query, and the parent cancellation stops everything at shutdown.

### Rule 4: do not use commands to talk to other parts of your program

Bubble Tea's own documentation is explicit about this:

> Note that there's almost never a reason to use a command to send a
> message to another part of your program. That can almost always be
> done in the update function.

If two components need to talk, do it in `Update`, where you have all
the state. Commands are for *side effects* — network, disk, timers.

```go
// ✗ A command used to "call" another component.
func (m *Model) notifyStatus() tea.Cmd {
    return func() tea.Msg { return notifyStatusMsg{} }
}

// ✓ Do it directly.
m.statusBar.SetInfo(m.statusInfo())
```

The exception is when the communication must cross an *asynchronous*
boundary — which is precisely the case in the template: a page wants
the root model to show a toast, but the page does not hold a reference
to the root model. It emits a message, and the root model acts on it.

## 3.4 Message ordering

Because `tea.Batch` runs commands concurrently, **message order is not
guaranteed**. This has practical consequences.

### The stale-response problem

Suppose the user types a filter and the page fires a query on every
keystroke:

```
User types "g"  → query A starts
User types "go" → query B starts
Query B finishes first → results for "go" shown
Query A finishes second → results for "g" shown  ← STALE
```

The user now sees results for a filter they have already changed.

The standard fix is to tag each request and discard stale responses.
The template does this with the page number:

```go
type RecordsMsg struct {
    Result *pbstore.PageResult
    Err    error
    Page   int      // the page that was requested
}
```

```go
func (p *RecordsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case RecordsMsg:
        p.loading = false
        if msg.Err != nil {
            return p, nil
        }
        // Discard the response of a query that is no longer current.
        if msg.Page != p.runtime.PageNumber {
            return p, nil
        }
        p.applyResult(msg.Result)
        return p, nil
    }
}
```

This is a small addition with a large payoff. Without it, fast typing
produces visibly wrong results.

### The debounce pattern

The other half of the solution is to not fire a query on every
keystroke at all. Debounce by scheduling a query after a quiet period:

```go
const debounceDelay = 250 * time.Millisecond

type debounceMsg struct{ query string }

func (p *SearchPage) scheduleSearch() tea.Cmd {
    query := p.input.Value()
    return tea.Tick(debounceDelay, func(time.Time) tea.Msg {
        return debounceMsg{query: query}
    })
}
```

Then in `Update`, only run the search if the query still matches:

```go
case debounceMsg:
    if msg.query != p.input.Value() {
        return p, nil        // the user typed more; wait for the next tick
    }
    p.loading = true
    return p, tea.Batch(p.spinner.Tick, p.Search())
```

Together, the debounce and the stale check give you a search box that
feels responsive and never shows the wrong results.

⚠️ **The debounce timer must be reset on each keystroke.** If you
schedule a `tea.Tick` per keystroke without cancelling the previous
ones, you get a burst of queries. The "check the query still matches"
guard above handles this correctly: the stale ticks are simply
ignored.

## 3.5 Custom messages that carry state

A message is any value. Choose the shape that makes the handler
clearest.

### The empty struct

```go
type refreshMsg struct{}
```

Use when the message is a pure signal. The handler does the work.

### The result struct

```go
type recordsLoadedMsg struct {
    records []*pbstore.Record
    page    int
}
```

Use when the handler needs the payload. The `page` field is the
staleness guard from §3.4.

### The error struct

```go
type loadFailedMsg struct {
    err  error
    page int
}
```

Prefer a **separate** error message over an optional error field:

```go
// ✗ The handler must check for nil in two places.
type loadMsg struct {
    records []*Record
    err     error
}

// ✓ The type switch routes them separately.
type loadDoneMsg struct{ records []*Record }
type loadFailedMsg struct{ err error }
```

The second version is easier to read and impossible to misuse.

### The tagged union

When you have a family of related results, a tagged union keeps the
number of types down:

```go
type resultMsg struct {
    kind  resultKind
    data  any
    err   error
}

type resultKind int

const (
    resultRecords resultKind = iota
    resultCollections
    resultCount
)
```

This is more compact but less type-safe. For a template that people
will extend, I recommend **separate named types**: they are
self-documenting in a `switch`, they get caught by the compiler when you
rename a field, and they cannot be mixed up.

## 3.6 Handling messages by type switch

The type switch is the workhorse of every Bubble Tea application.

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmds []tea.Cmd

    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
        m.resize()

    case tea.KeyMsg:
        if cmd, handled := m.handleGlobalKey(msg); handled {
            return m, cmd
        }

    case pages.NavigateMsg:
        return m, m.navigate(msg.Page)

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

    // Forward to the active page.
    cmds = append(cmds, m.updateActivePage(msg)...)

    m.refreshStatusBar()

    return m, tea.Batch(cmds...)
}
```

### The shadowing idiom

`switch msg := msg.(type)` declares a new `msg` in each case, with the
case's concrete type. Inside `case pages.RecordsMsg:`, `msg` is a
`pages.RecordsMsg`, so `msg.Err` compiles. Outside the switch, `msg` is
still the `tea.Msg`.

This is idiomatic Go and worth using consistently.

### The fall-through problem

Notice that the global message cases `return` early, while the page
messages fall through to `updateActivePage`. That asymmetry is
intentional:

- Global messages are *consumed* by the root model.
- Page messages are *forwarded* to the page.

⚠️ **A bug I hit in the template**: the early `return`s bypassed the
final `m.refreshStatusBar()` call, so the status bar only updated when
the user changed page. The symptom was a status bar that showed
"unknown" for the connection state until you navigated somewhere.

The fix was to make every path refresh it. Chapter 15 develops the
final structure, where the refresh is factored into a helper called
from each branch.

A more robust alternative is to avoid early returns:

```go
switch msg := msg.(type) {
case tea.KeyMsg:
    if cmd, handled := m.handleGlobalKey(msg); handled {
        cmds = append(cmds, cmd)
        break        // breaks out of the switch, continues to the tail
    }
    fallthrough    // no — fallthrough only works for type switches
}
```

⚠️ `fallthrough` does not work in a type switch. The idiomatic Go
solution is to use a labelled break or to restructure. For clarity, the
early-return version with an explicit refresh in each branch is fine —
just make sure you do not forget a branch.

## 3.7 The `WindowSizeMsg`

Terminals are resized. Bubble Tea sends a `tea.WindowSizeMsg` with the
new dimensions.

```go
case tea.WindowSizeMsg:
    m.width = msg.Width
    m.height = msg.Height
    m.resize()
```

Every component that renders needs to know the size. The template
propagates it:

```go
func (m *Model) resize() {
    m.statusBar.SetWidth(m.width)
}
```

and each page does its own layout:

```go
func (p *RecordsPage) resize() {
    // The header, the filter, the blank lines and the pagination take up
    // roughly 7 lines.
    height := p.height - 7
    if height < 3 {
        height = 3
    }
    p.table.SetHeight(height)
}
```

⚠️ **The `tea.WindowSizeMsg` arrives before your pages exist** if you
construct them after the program starts. If a page computes its
layout only in response to `WindowSizeMsg`, it may never be sized.

The template's pages store the size and re-apply it in `Refresh()`:

```go
func (p *SearchPage) Refresh() tea.Cmd {
    p.loading = true
    p.err = nil
    p.input.Focus()
    p.resize()        // re-apply the layout
    return tea.Batch(p.spinner.Tick, p.Search())
}
```

This is the pattern to use for any component whose size depends on
something other than the terminal width.

## 3.8 Concurrency in practice: the spinner

Let us put the pieces together in a complete, real component: the
loading spinner.

```go
// internal/pages/records.go (excerpt)
type RecordsPage struct {
    spinner spinner.Model
    loading bool
    // ...
}

func NewRecordsPage(...) *RecordsPage {
    sp := spinner.New()
    sp.Spinner = spinner.Dot
    sp.Style = t.Accent
    // ...
}

func (p *RecordsPage) Refresh() tea.Cmd {
    p.loading = true
    return tea.Batch(p.spinner.Tick, p.Load())
}

func (p *RecordsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case pages.RecordsMsg:  // our own load result
        p.loading = false
        // ...

    case tea.KeyMsg:
        // ...

    default:
        // Everything else goes to the spinner.
        var cmd tea.Cmd
        p.spinner, cmd = p.spinner.Update(msg)
        return p, cmd
    }
}
```

The spinner works like this:

1. `Refresh` starts `p.spinner.Tick`, which returns a command that
   sleeps briefly and then emits a `spinner.TickMsg`.
2. The tick message reaches `Update`, which passes it to the spinner.
3. The spinner advances its frame and **returns a command that
   schedules the next tick**.
4. The next tick arrives, and the cycle repeats.

The spinner stops when it stops being updated. If you stop calling
`p.spinner.Update(msg)`, the chain of ticks ends.

This is why the template passes the spinner an update in the `default`
branch of the page's `Update`: any message the page does not handle
keeps the animation alive, and when the page stops being updated the
animation dies with it.

⚠️ **But** the `default` branch also receives *every* message,
including the ones the page handles in an earlier `case`. Because the
earlier cases `return`, the spinner does not see them — which is fine,
since the spinner only cares about tick messages.

## 3.9 Exercises

1. Write a program with a `tea.Tick` that increments a counter every
   100 ms. Use `tea.Every`. Then make the program quit after 10 ticks.
   How do you stop the `Every` loop?

2. Implement the stale-response guard from §3.4 in a program that
   queries on every keystroke. To reproduce the bug, deliberately
   omit the guard and make the second query return faster than the
   first.

3. Write a command that captures `m.runtime.Filter` and reads it after
   a 100 ms sleep. Run the test suite with `go test -race`. Then fix
   it.

4. Write a command that returns an error. Show that it is impossible
   to return `(Msg, error)` from a `Cmd`, and design the message type
   that carries the error instead.

5. Take the Flint template and add a debounce to the records page
   filter, so that the query fires 300 ms after the user stops typing
   rather than on every keystroke.

6. Explain why `tea.Batch` is not appropriate for "save the record,
   then reload the list". Write the `tea.Sequence` version.

## 3.10 What you learned

- `Update` has two halves: **state change** (immediate) and
  **commands** (deferred).
- `tea.Batch` is concurrent and unordered; `tea.Sequence` is ordered.
- Commands must **capture values, not the model** — capturing the model
  is a data race.
- Commands **return messages, not errors**; the error travels inside
  the message.
- Commands should be **cancellable** via `context.Context`.
- **Message order is not guaranteed**: use a debounce and a staleness
  guard for user-driven queries.
- Separate named message types are clearer than one type with optional
  fields.
- The **`default` branch** of a type switch keeps the spinner ticking.

## 3.11 Further reading

- `commands.go` in the Bubble Tea source — the exact semantics of
  `Batch` and `Sequence`.
- `internal/pages/records.go` — a page with a spinner, a load command
  and a staleness guard.
- `internal/components/toast.go` — a component with a self-scheduling
  timer.

Next: [Chapter 4 — Keys, Hotkeys, and the Routing Problem](ch04-keys-and-hotkeys.md).
