# Chapter 24 — Patterns and Practices

This chapter is the "what I wish I had known" chapter. Each pattern
describes a recurring problem, the naive solution, the better one, and
the trap in between.

## 24.1 The command closure capture

**The problem.** A command needs some data from the model. The obvious
way is to reach into it.

**The trap.**

```go
// ✗ Reads the model from the command goroutine: a data race.
func (m *Model) load() tea.Cmd {
    return func() tea.Msg {
        time.Sleep(100 * time.Millisecond)
        return loadedMsg{m.records}
    }
}
```

**The pattern.** Capture by value, before the closure.

```go
// ✓ Everything the command needs, captured immutably.
func (m *Model) load() tea.Cmd {
    store, collection, filter := m.store, m.runtime.Collection, m.runtime.Filter

    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        result, err := store.List(ctx, pbstore.Query{
            Collection: collection,
            Filter:     filter,
        })
        if err != nil {
            return loadFailedMsg{err}
        }
        return loadedMsg{result}
    }
}
```

**The rule.**

> **A command captures values, never the model.**

This is the single most important rule in Bubble Tea programming, and
the race detector is the only reliable guard against breaking it.

## 24.2 The staleness guard

**The problem.** The user types fast; the queries finish out of order.

```
User types "g"   → query A starts
User types "go"  → query B starts
Query B finishes first → the results for "go" are shown
Query A finishes second → the results for "g" are shown  ← STALE
```

**The pattern.** Tag the request, discard the stale response.

```go
type RecordsMsg struct {
    Result *pbstore.PageResult
    Err    error
    Page   int      // the page that was requested
}

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

**The generalisation.** The guard does not have to be a page number. It
can be a query hash, a generation counter, or a timestamp:

```go
// A generation counter: every new request bumps it.
type listMsg struct {
    result    *Result
    generation int
    err       error
}

func (p *Model) load() tea.Cmd {
    generation := p.generation + 1
    p.generation = generation
    // ...
    return func() tea.Msg {
        return listMsg{result: result, generation: generation}
    }
}

func (p *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case listMsg:
        if msg.generation != p.generation {
            return p, nil    // a newer request has been issued
        }
        // ...
    }
}
```

⚠️ **The generation counter is more general than the page number**,
because it also handles the case where the user re-runs the *same*
query.

## 24.3 The debounce

**The problem.** A query per keystroke hammers the database.

**The pattern.** Schedule the query after a quiet period, and check
that the input has not changed when it fires.

```go
const debounceDelay = 250 * time.Millisecond

type debounceMsg struct{ query string }

func (p *SearchPage) scheduleSearch() tea.Cmd {
    query := p.input.Value()
    return tea.Tick(debounceDelay, func(time.Time) tea.Msg {
        return debounceMsg{query: query}
    })
}

func (p *SearchPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case debounceMsg:
        // The user typed more while we were waiting: ignore this tick.
        if msg.query != p.input.Value() {
            return p, nil
        }
        p.loading = true
        return p, tea.Batch(p.spinner.Tick, p.Search())
    }
}
```

⚠️ **Do not try to cancel the pending tick.** The "check the query
still matches" guard handles the stale ticks correctly, and it is much
simpler than a cancellation scheme.

⚠️ **250 ms is a good default.** Below 150 ms the user perceives a lag;
above 400 ms it feels unresponsive.

## 24.4 The refresh convention

**The problem.** `Init()` runs once. A page the user navigates to later
never loads its data.

**The trap.**

```go
// ✗ The page is created at startup; Init runs once, at startup.
func NewRecordsPage(...) *RecordsPage {
    p := &RecordsPage{...}
    return p
}
```

**The pattern.** Every data-loading page has a `Refresh()`.

```go
// Refresh reloads the current page of records.
//
// It starts the spinner so that the loading state is visible.
func (p *RecordsPage) Refresh() tea.Cmd {
    p.loading = true
    return tea.Batch(p.spinner.Tick, p.Load())
}
```

And the root model calls it on navigation:

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
    // ...
    }
}
```

⚠️ **A related trap: the sizing.** A component that computes its
layout from `tea.WindowSizeMsg` may never be sized, because the message
arrived before the component existed. The `Refresh()` should re-apply
the layout:

```go
func (p *SearchPage) Refresh() tea.Cmd {
    p.loading = true
    p.input.Focus()
    p.resize()        // re-apply the layout
    return tea.Batch(p.spinner.Tick, p.Search())
}
```

## 24.5 The focused-input guard

**The problem.** The global hotkey layer steals the keys the user is
typing.

**The trap.**

```go
// ✗ "q" quits while the user is typing a filter.
case tea.KeyPressMsg:
    if cmd, handled := m.handleGlobalKey(msg); handled {
        return m, cmd
    }
```

**The pattern.** The page owns the knowledge of whether it has the
focus; the root model asks.

```go
// FocusAware is implemented by the pages that own a focused input.
type FocusAware interface {
    InputFocused() bool
}

// In the root model.
func (m *Model) inputFocused() bool {
    switch page := m.activePage().(type) {
    case FocusAware:
        return page.InputFocused()
    default:
        return false
    }
}
```

⚠️ **The alternative — a hardcoded switch on the page — works until you
add a page.** Then the new page silently loses the guard, and the bug
only shows up when someone types "q".

## 24.6 The explicit-render cache

**The problem.** `View()` is called on every message. If it does
something expensive, the UI stutters.

**The trap.**

```go
// ✗ Re-marshals the payload on every render.
func (m *Model) View() tea.View {
    data, _ := json.MarshalIndent(m.payload, "", "  ")
    return tea.NewView(data)
}
```

**The pattern.** Do the expensive work in `Update`, cache the result.

```go
type Model struct {
    // ...
    renderedPayload string
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case payloadMsg:
        m.payload = msg.payload
        m.renderedPayload, _ = json.MarshalIndent(m.payload, "", "  ")
    }
    return m, nil
}

func (m *Model) View() tea.View {
    return tea.NewView(string(m.renderedPayload))
}
```

⚠️ **The cache must be invalidated when the state changes.** The
simplest correct approach is to compute it in the same place you
compute the state.

## 24.7 The map iteration rule

**The problem.** Go randomises the map iteration order. Rendering a map
directly produces a different order on every frame — a flickering
display.

**The trap.**

```go
// ✗ The order changes on every render.
for key, value := range m.fields {
    b.WriteString(key + ": " + value + "\n")
}
```

**The pattern.** Sort the keys.

```go
keys := make([]string, 0, len(m.fields))
for key := range m.fields {
    keys = append(keys, key)
}
sort.Strings(keys)

for _, key := range keys {
    b.WriteString(key + ": " + m.fields[key] + "\n")
}
```

⚠️ **This applies everywhere**, not just in the views. A map
serialised to JSON with the standard encoder is sorted (the encoder
sorts the map keys), but a map rendered by hand is not.

## 24.8 The path-based tree key

**The problem.** A tree keyed by the node's own label collides when
siblings share a label.

**The trap.**

```go
// ✗ "a.x" and "b.x" are the same key.
expanded map[string]bool
```

**The pattern.** Key by the path from the root.

```go
func buildJSONNode(key string, value any, depth int, parentPath string) *JSONNode {
    path := key
    if parentPath != "" {
        path = parentPath + "/" + key
    }
    node := &JSONNode{Key: key, Value: value, depth: depth, path: path}
    // ...
}
```

⚠️ **The same rule applies to any hierarchical structure** — a file
tree, a category tree, a dependency graph.

## 24.9 The idempotent ensure

**The problem.** You need a collection to exist. Where do you create
it?

**The trap.**

```go
// ✗ Fails on the second run: the collection already exists.
collection := core.NewBaseCollection("vocab")
if err := app.Save(collection); err != nil {
    return err
}
```

**The pattern.** Check first, create second.

```go
if existing, err := app.FindCachedCollectionByNameOrId(name); err == nil && existing != nil {
    return existing, nil
}

collection := core.NewBaseCollection(name)
// ...
```

**The rule.**

> **An operation that runs on every startup must be idempotent.**

This applies to migrations too (they are tracked by the database), to
the dictionary creation, and to the seed data.

## 24.10 The error-to-message conversion

**The problem.** A command can fail. The error must reach the user.

**The trap.**

```go
// ✗ The error is lost.
func (p *RecordsPage) Load() tea.Cmd {
    return func() tea.Msg {
        result, err := store.List(ctx, query)
        if err != nil {
            return nil    // a nil message is ignored!
        }
        return RecordsMsg{Result: result}
    }
}
```

**The pattern.** The message carries the error.

```go
if err != nil {
    return RecordsMsg{Err: err}
}
```

And the root model converts it into a visible state:

```go
case pages.RecordsMsg:
    if msg.Err != nil {
        m.runtime.SetError(msg.Err)
        return m, m.toast.Show(components.ToastError, msg.Err.Error())
    }
    m.runtime.SetError(nil)
```

⚠️ **Centralise the conversion.** If each page handles its own errors
differently, the user sees a mix of toasts, status bar entries and
silent failures. The root model is the right place: one place, one
behaviour.

## 24.11 The wrapped error

**The problem.** You want `errors.Is` to work, and you want the message
to say what failed.

**The trap.**

```go
// ✗ The chain is broken.
return nil, fmt.Errorf("pbstore: list %q: %v", q.Collection, err)
```

**The pattern.**

```go
// ✓ The chain survives.
return nil, fmt.Errorf("pbstore: list %q: %w", q.Collection, err)
```

⚠️ **`%w` only works with `error` values.** If a function returns
`map[string]error`, you cannot wrap it directly — you must join the
values first (the template's `joinErrors`).

## 24.12 The one-line rule

**The problem.** A long function with a long switch.

**The pattern.** Extract the switch, keep the update function about the
*flow*.

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    cmd := m.update(msg)
    m.refreshStatusBar()
    return m, cmd
}

// update handles the message and returns the command.
func (m *Model) update(msg tea.Msg) tea.Cmd {
    switch msg := msg.(type) {
    // ...
    }
}
```

⚠️ **This pattern exists for a reason.** The template's `Update` had a
bug where the early `return`s skipped the `refreshStatusBar()` call.
Extracting the switch made the bug impossible: there is now exactly one
place where the refresh happens.

## 24.13 The message per concern

**The problem.** One message type with many optional fields.

**The trap.**

```go
type loadMsg struct {
    records    []*Record
    collections []Collection
    count      int
    err        error
}
```

Every handler must check which fields are meaningful.

**The pattern.** One type per result.

```go
type loadDoneMsg struct{ records []*Record }
type loadFailedMsg struct{ err error }
```

⚠️ **Use separate types for the error.** A message with an optional
error field invites `if msg.err != nil` checks in two places. Separate
types route them through the type switch, where they cannot be
confused.

## 24.14 The context everywhere

**The pattern.** Every blocking call takes a `context.Context`.

```go
func (s *Embedded) List(ctx context.Context, q Query) (*PageResult, error) {
    if err := ctx.Err(); err != nil {
        return nil, err
    }
    // ...
}
```

**Why it matters.**

```go
// A per-call timeout bounds a single query.
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

```go
// A root context ties everything to the application lifetime.
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
```

⚠️ **The `ctx.Err()` check at the top of the method** is a cheap way to
avoid starting work that is already cancelled. It is worth the two
lines.

## 24.15 The interface seam

**The pattern.** Define the interface where it is *used*, not where it
is *implemented*.

```go
// The page defines what it needs.
type RecordSource interface {
    List(ctx context.Context, q pbstore.Query) (*pbstore.PageResult, error)
}

// The concrete type satisfies it implicitly.
var _ RecordSource = (*pbstore.Embedded)(nil)
```

⚠️ **The template's `Store` interface lives in `pbstore`** because it
is a *published contract* that downstream projects implement. That is
the exception, not the rule. For an internal interface, define it in
the consumer package.

## 24.16 The checklist

Before you commit a change, run through this:

| Check                                       | How                                   |
| ------------------------------------------- | ------------------------------------- |
| No I/O in `Update` or `View`                 | Grep for the store calls              |
| No command reads the model                   | Grep for `m.` inside a closure        |
| The tests pass with `-race`                  | `go test -race ./...`                 |
| The formatting is clean                      | `gofmt -l .`                          |
| `go vet` is clean                            | `go vet ./...`                        |
| The errors are wrapped with `%w`             | Grep for `%v` in `Errorf`             |
| The maps are sorted before rendering         | Grep for `range` over a map           |
| The new pages have a `Refresh()`             | Read the page                        |
| The new components handle their own messages | Check the root model forwards them    |
| The new hotkeys do not collide               | Read `handleGlobalKey`                |

## 24.17 Exercises

1. Implement the generation-counter staleness guard in the search
   page, replacing the page-number guard.

2. Add a debounce to the records page filter.

3. Refactor the root model's `Update` with the one-line rule from
   §24.12, and verify that the status bar refresh can no longer be
   skipped.

4. Find every `range` over a map in your code and sort the keys.

5. Write a test that fails if a command reads the model. (Hint: use
   the race detector.)

6. Audit your code with the §24.16 checklist.

## 24.18 What you learned

- **Commands capture values, never the model.** The rule and the trap.
- The **staleness guard** and the **debounce** are the two halves of a
  responsive search.
- **`Init()` runs once** — every data-loading page needs a
  `Refresh()`.
- The **focused-input guard** must be asked of the page, not
  hardcoded.
- **Cache the expensive renders** and invalidate them with the state.
- **Never iterate a map to produce output.**
- **Idempotence** is the property of everything that runs at startup.
- **Centralise the error-to-message conversion** in the root model.
- **Wrap with `%w`**, never `%v`.
- The **one-line rule** makes the shared-state refresh impossible to
  forget.
- **One message type per result**, with the error as its own type.

## 24.19 Further reading

- `internal/app/app.go` — the root model, as the pattern library.
- `internal/pages/records.go` — the staleness guard and the `Refresh`.
- `internal/components/jsonviewer.go` — the path-based tree key.
- `internal/pbstore/embedded.go` — the wrapped errors and the contexts.

Next: [Chapter 25 — Advanced Patterns](ch25-advanced-patterns.md).
