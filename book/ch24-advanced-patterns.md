# Chapter 24 — Advanced Patterns

The patterns in Chapter 23 cover the common cases. This chapter covers
the ones you hit when the application grows past a single-screen
browser.

## 24.1 The background worker

**The situation.** Something must happen periodically: refresh a
counter, check for updates, re-index something.

**The trap.** A goroutine that reads the model.

```go
// ✗ The goroutine reads the model concurrently with the event loop.
go func() {
    for range time.Tick(time.Minute) {
        m.Refresh()          // data race
    }
}()
```

**The pattern.** The worker owns its state, and communicates through
`program.Send`.

```go
// The worker.
type poller struct {
    // The program is used to deliver the messages.
    program *tea.Program
    // The context bounds the worker's lifetime.
    ctx context.Context
    // The interval between two polls.
    interval time.Duration
}

// pollMsg is delivered to the event loop on every tick.
type pollMsg struct {
    value int
    err   error
}

// start returns the command that launches the worker.
func (p *poller) start() tea.Cmd {
    return func() tea.Msg {
        ticker := time.NewTicker(p.interval)
        defer ticker.Stop()

        for {
            select {
            case <-p.ctx.Done():
                return nil    // stop the worker
            case <-ticker.C:
                value, err := p.poll()
                p.program.Send(pollMsg{value: value, err: err})
            }
        }
    }
}

// poll does the actual work, outside the event loop.
func (p *poller) poll() (int, error) {
    ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
    defer cancel()

    // ... the work ...
    return 42, nil
}
```

**And in the root model:**

```go
case pollMsg:
    m.counter = msg.value
    if msg.err != nil {
        m.runtime.SetError(msg.err)
    }
```

⚠️ **`program.Send` from a worker is the sanctioned escape hatch.**
It is the only way for an *external* goroutine to reach the event loop.
Chapter 3 recommends against using it for internal communication, but
a background worker is genuinely external.

⚠️ **The worker must be cancellable.** The `ctx.Done()` case is what
prevents a goroutine leak. Without it, the worker runs until the
process exits.

⚠️ **The worker must not touch the model.** It captures only what it
needs — a store handle, a URL, a config value.

### The `tea.Tick` alternative

For a *one-shot* delay, prefer a command:

```go
func (m *Model) scheduleRefresh() tea.Cmd {
    return tea.Tick(30*time.Second, func(time.Time) tea.Msg {
        return refreshDueMsg{}
    })
}
```

The difference: `tea.Tick` runs in the event loop's command machinery,
so it is automatically cancelled when the program quits. A `go func()`
is not.

⚠️ **Use `tea.Tick` unless you need a repeating schedule or a real
goroutine.** It is simpler and it cannot leak.

## 24.2 The repeating tick

**The situation.** The spinner, a clock, a progress indicator.

**The pattern.** A self-rescheduling tick, driven entirely by messages.

```go
type tickMsg time.Time

func tick() tea.Cmd {
    return tea.Tick(time.Second, func(t time.Time) tea.Msg {
        return tickMsg(t)
    })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tickMsg:
        m.clock = msg.Format("15:04:05")
        return m, tick()      // reschedule
    }
    return m, nil
}
```

⚠️ **The reschedule must be conditional.** If the component is not
visible, stop rescheduling:

```go
case tickMsg:
    if !m.visible {
        return m, nil    // stop the chain
    }
    m.clock = msg.Format("15:04:05")
    return m, tick()
```

Otherwise the tick runs forever, even after the component is gone.

⚠️ **Use `tea.Every` for a fixed interval**, not a self-rescheduling
`tea.Tick`. `tea.Every` is simpler:

```go
return m, tea.Every(time.Second, func(t time.Time) tea.Msg {
    return tickMsg(t)
})
```

But `tea.Every` never stops. Both approaches need a guard.

## 24.3 The modal overlay

**The situation.** A confirmation dialog, a help screen, a quick
"jump to" prompt that appears over the current page.

**The trap.** Rendering the modal inside every page.

```go
// ✗ Every page has to know about the modal.
func (p *RecordsPage) View() tea.View {
    if m.modal != nil {
        return tea.NewView(m.modal.View())
    }
    return tea.NewView(p.content())
}
```

**The pattern.** The root model owns the modal and overlays it.

```go
type Model struct {
    // ...
    modal tea.Model    // nil when there is no modal
}

// showModal displays a modal overlay.
func (m *Model) showModal(modal tea.Model) tea.Cmd {
    m.modal = modal
    return nil
}

// dismissModal closes the modal overlay.
func (m *Model) dismissModal() {
    m.modal = nil
}
```

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // The modal gets the message first, and consumes it if it wants.
    if m.modal != nil {
        updated, cmd := m.modal.Update(msg)
        m.modal = updated.(tea.Model)
        return m, cmd
    }
    // ... the normal flow ...
}
```

```go
func (m *Model) View() tea.View {
    body := m.pageView()

    if m.modal != nil {
        // Overlay the modal on top of the body.
        return tea.NewView(overlay(modalView, body))
    }

    return tea.NewView(body)
}
```

### The overlay

```go
// overlay draws the modal on top of the background, centring it.
func overlay(modal, background string) string {
    modalLines := strings.Split(modal, "\n")
    backgroundLines := strings.Split(background, "\n")

    // The modal is centred horizontally.
    modalWidth := 0
    for _, line := range modalLines {
        if w := lipgloss.Width(line); w > modalWidth {
            modalWidth = w
        }
    }

    out := make([]string, len(backgroundLines))
    copy(out, backgroundLines)

    for i, line := range modalLines {
        row := (len(backgroundLines) - len(modalLines)) / 2 + i
        if row < 0 || row >= len(out) {
            continue
        }
        out[row] = centerLine(line, modalWidth, out[row])
    }

    return strings.Join(out, "\n")
}
```

⚠️ **The overlay must be built from the *rendered* widths**, not the
byte lengths. A modal with a border and a background colour has ANSI
sequences, and `len` will be wrong.

⚠️ **A modal that consumes every message blocks the page.** That is
usually correct — a modal should be modal — but it means the *global*
hotkeys are also blocked. If the user must be able to quit, handle
`ctrl+c` before the modal.

## 24.4 The command palette

**The situation.** A single key (`ctrl+p`) opens a fuzzy search over
every action the application can perform.

This is the pattern that makes a TUI feel like a power tool.

**The implementation.**

```go
// Command is a named action.
type Command struct {
    // Name is the label shown in the palette.
    Name string
    // Description is the longer explanation.
    Description string
    // Shortcut is the key hint.
    Shortcut string
    // Run performs the action.
    Run func(m *Model) tea.Cmd
}

// Commands returns the available commands.
func (m *Model) Commands() []Command {
    return []Command{
        {
            Name:        "Refresh",
            Description: "Reload the current page",
            Shortcut:    "r",
            Run: func(m *Model) tea.Cmd {
                return m.reloadActivePage()
            },
        },
        {
            Name:        "Sync",
            Description: "Pull the remote collection into the local store",
            Shortcut:    "y",
            Run: func(m *Model) tea.Cmd {
                return m.triggerSync()
            },
        },
        {
            Name:        "Quit",
            Description: "Exit the application",
            Shortcut:    "q",
            Run: func(m *Model) tea.Cmd {
                return tea.Quit
            },
        },
    }
}
```

The palette itself is a `bubbles/list` with the filtering enabled:

```go
palette := list.New(items, list.NewDefaultDelegate(), 60, 15)
palette.Title = "Commands"
palette.SetShowStatusBar(false)
palette.SetFilteringEnabled(true)   // ← the built-in fuzzy filter
```

**Why `bubbles/list` with its filtering is perfect here**: it already
implements the fuzzy matching (via `sahilm/fuzzy`), the pagination, and
the "/" key to focus the filter.

⚠️ **The palette is a modal.** It must consume the keys until it is
dismissed, including `esc` and `enter`.

```go
case "enter":
    item, ok := m.palette.SelectedItem().(CommandItem)
    if !ok {
        return m, nil
    }
    m.palette = nil
    return m, item.Command.Run(m)

case "esc":
    m.palette = nil
    return m, nil
```

⚠️ **The command list must be rebuilt each time the palette opens**, so
that it reflects the current state (the active collection, the
connection status).

## 24.5 The two-phase load

**The situation.** A query takes two seconds. You want to show a
progress indicator with a percentage.

**The trap.** A single command that does everything.

```go
// ✗ No progress until the very end.
func (p *Page) load() tea.Cmd {
    return func() tea.Msg {
        result, err := p.store.List(ctx, query)
        return loadedMsg{result, err}
    }
}
```

**The pattern.** Split it into a command that reports progress and a
command that reports the result.

```go
// The progress is reported through the program.
func (p *Page) load() tea.Cmd {
    store, query, program := p.store, p.query, p.program

    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
        defer cancel()

        // Phase 1: count the total.
        total, err := store.Count(ctx, query.Collection, query.Filter)
        if err != nil {
            return loadFailedMsg{err}
        }

        program.Send(progressMsg{phase: "counted", total: total})

        // Phase 2: fetch in batches, reporting the progress.
        for page := 1; ; page++ {
            result, err := store.List(ctx, pbstore.Query{
                Collection: query.Collection,
                Filter:     query.Filter,
                Sort:       query.Sort,
                Page:       page,
                PerPage:    500,
            })
            if err != nil {
                return loadFailedMsg{err}
            }

            program.Send(progressMsg{
                phase:   "fetching",
                fetched: page * 500,
                total:   total,
            })

            p.records = append(p.records, result.Records...)
            if len(result.Records) < 500 {
                break
            }
        }

        return loadDoneMsg{records: p.records}
    }
}
```

⚠️ **The command must not touch the page.** `p.records = append(...)`
reads and writes the page from the command goroutine — a data race.
The records must be accumulated in a **local variable** and returned in
the final message.

⚠️ **`program.Send` for the progress.** The progress messages go
straight to the event loop, bypassing the normal return path.

## 24.6 The undo stack

**The situation.** The user edits a record and wants to undo.

**The pattern.** Keep the previous values, and restore them.

```go
type undoEntry struct {
    collection string
    id         string
    before     map[string]any
}

type Model struct {
    // ...
    undoStack []undoEntry
}
```

```go
// Before a write, capture the current state.
func (p *DetailPage) save() tea.Cmd {
    store, collection, id := p.store, p.runtime.Collection, p.runtime.SelectedID

    // Read the current values first.
    before, err := p.currentFields()
    if err != nil {
        return nil
    }

    doc, err := p.editor.Validate()
    if err != nil {
        return func() tea.Msg {
            return components.ToastMsg{Level: components.ToastError, Text: "invalid JSON: " + err.Error()}
        }
    }

    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()

        if _, err := store.Update(ctx, collection, id, doc); err != nil {
            return components.ToastMsg{Level: components.ToastError, Text: err.Error()}
        }
        return savedMsg{before: before}
    }
}
```

```go
case savedMsg:
    m.undoStack = append(m.undoStack, undoEntry{
        collection: m.runtime.Collection,
        id:         m.runtime.SelectedID,
        before:     msg.before,
    })
    // Keep the stack bounded.
    if len(m.undoStack) > 50 {
        m.undoStack = m.undoStack[1:]
    }
    return m, m.toast.Show(components.ToastSuccess, "record saved")
```

**And the undo:**

```go
case "u":
    if len(m.undoStack) == 0 {
        return m, m.toast.Show(components.ToastInfo, "nothing to undo")
    }

    entry := m.undoStack[len(m.undoStack)-1]
    m.undoStack = m.undoStack[:len(m.undoStack)-1]

    store := m.store
    return m, func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()

        if _, err := store.Update(ctx, entry.collection, entry.id, entry.before); err != nil {
            return components.ToastMsg{Level: components.ToastError, Text: err.Error()}
        }
        return components.ToastMsg{Level: components.ToastSuccess, "reverted"}
    }
```

⚠️ **Bound the undo stack.** An unbounded one is a memory leak.

⚠️ **Undo is not a transaction.** The undo restores the *fields*, not
the whole record state. If a write triggers side effects (a hook that
sends an email), the undo does not undo them.

## 24.7 The virtualised list

**The situation.** A collection with a million records. The table
cannot hold them all.

**The pattern.** Fetch the pages as the user scrolls.

The bubbles `table` does not virtualise — it holds all the rows. For a
large dataset, you need to manage the window yourself.

```go
type virtualList struct {
    // The rows currently displayed.
    rows []table.Row
    // The index of the first displayed row.
    offset int
    // The total number of rows.
    total int
    // The number of rows per screen.
    pageSize int
}
```

```go
// scrollDown moves the window down by one page.
func (l *virtualList) scrollDown(store pbstore.Store, collection string) tea.Cmd {
    l.offset += l.pageSize
    return l.fetch(store, collection)
}

// fetch loads the rows for the current window.
func (l *virtualList) fetch(store pbstore.Store, collection string) tea.Cmd {
    offset := l.offset
    perPage := l.pageSize

    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()

        result, err := store.List(ctx, pbstore.Query{
            Collection: collection,
            Page:       offset/perPage + 1,
            PerPage:    perPage,
        })
        if err != nil {
            return windowFailedMsg{err}
        }
        return windowLoadedMsg{rows: result.Records, offset: offset}
    }
}
```

⚠️ **The offset pagination is O(offset).** For a million records,
page 30,000 is slow. Use **keyset pagination** instead:

```go
// The filter carries the cursor.
Filter: fmt.Sprintf(`id > %q`, lastID),
Sort:   "id",
```

⚠️ **Keyset pagination does not support "jump to page 500".** That is
the trade-off. For a lookup tool, sequential scrolling is usually the
right model anyway.

## 24.8 The export

**The situation.** The user wants the current results as CSV or JSON.

**The pattern.** A command that writes a file and reports the result.

```go
type exportMsg struct {
    path string
    count int
    err  error
}

func (p *RecordsPage) exportCSV() tea.Cmd {
    store, collection, filter := p.store, p.runtime.Collection, p.runtime.Filter

    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
        defer cancel()

        result, err := store.List(ctx, pbstore.Query{
            Collection: collection,
            Filter:     filter,
            PerPage:    1000,
        })
        if err != nil {
            return exportMsg{err: err}
        }

        path := filepath.Join(os.TempDir(), "flint-export.csv")
        f, err := os.Create(path)
        if err != nil {
            return exportMsg{err: err}
        }
        defer f.Close()

        w := csv.NewWriter(f)
        defer w.Flush()

        if err := w.Write([]string{"id", "key", "label"}); err != nil {
            return exportMsg{err: err}
        }

        for _, rec := range result.Records {
            if err := w.Write([]string{
                rec.ID,
                rec.StringField("key"),
                rec.StringField("label"),
            }); err != nil {
                return exportMsg{err: err}
            }
        }

        return exportMsg{path: path, count: len(result.Records)}
    }
}
```

⚠️ **The export must respect the active filter.** Exporting
everything when the user filtered is a data-leak surprise.

⚠️ **The export is a long operation.** It belongs in a command, with a
progress indicator (see §24.5).

⚠️ **Do not export to the terminal.** A thousand CSV rows would
destroy the TUI. Write to a file and tell the user where it is.

## 24.9 The import

**The situation.** The user wants to bulk-load a CSV.

**The pattern.** Validate everything first, then write in a
transaction.

```go
func (p *Page) importCSV(path string) tea.Cmd {
    store, collection := p.store, p.runtime.Collection

    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
        defer cancel()

        f, err := os.Open(path)
        if err != nil {
            return importMsg{err: err}
        }
        defer f.Close()

        r := csv.NewReader(f)
        records, err := r.ReadAll()
        if err != nil {
            return importMsg{err: err}
        }

        // Parse and validate everything first.
        payloads := make([]map[string]any, 0, len(records))
        for i, row := range records {
            if i == 0 {
                continue    // the header
            }
            if len(row) < 2 {
                return importMsg{err: fmt.Errorf("row %d: expected 2 columns, got %d", i+1, len(row))}
            }
            payloads = append(payloads, map[string]any{
                "key":   row[0],
                "label": row[1],
            })
        }

        // Then write.
        for _, payload := range payloads {
            if _, err := store.Create(ctx, collection, payload); err != nil {
                return importMsg{err: fmt.Errorf("import failed: %w", err)}
            }
        }

        return importMsg{count: len(payloads)}
    }
}
```

⚠️ **Validate everything before writing anything.** A partial import
that fails on row 900 leaves the database in a state the user does not
understand.

⚠️ **For a large import, use a transaction.** The store interface does
not expose one, so you need the embedded app directly — or add a
`BulkCreate` method to your own `Store` implementation.

## 24.10 The status poller

**The situation.** The status bar shows the connection health, but the
health should be checked periodically, not just at startup.

**The pattern.** A `tea.Tick` that re-schedules the health check.

```go
type healthCheckMsg struct {
    status string
    err    error
}

func (m *Model) scheduleHealthCheck() tea.Cmd {
    return tea.Tick(30*time.Second, func(time.Time) tea.Msg {
        return healthCheckDueMsg{}
    })
}
```

```go
case healthCheckDueMsg:
    m.refreshStatusBar()
    return m, tea.Batch(m.checkHealth(), m.scheduleHealthCheck())
```

⚠️ **Do not poll too often.** Every check is a query (embedded) or an
HTTP request (remote). Thirty seconds is reasonable; five seconds is
wasteful.

⚠️ **Do not poll when the application is not visible.** There is no
such thing as a background TUI, but there *is* a suspend/resume
concept for some terminals. Ignore it unless you need it.

## 24.11 The connection indicator

**The situation.** The remote server goes away. The user should know.

**The pattern.** Track the last successful contact and the failures.

```go
type Runtime struct {
    // ...
    Connection        ConnectionState
    ConnectionDetail  string
    LastCheck         time.Time
    ConsecutiveErrors int
}
```

```go
func (m *Model) applyHealth(msg HealthMsg) {
    m.runtime.LastCheck = time.Now()

    if msg.Err != nil {
        m.runtime.ConsecutiveErrors++
        m.runtime.Connection = state.ConnectionError
        m.runtime.ConnectionDetail = msg.Err.Error()
        m.runtime.SetError(msg.Err)
        return
    }

    m.runtime.ConsecutiveErrors = 0
    m.runtime.Connection = state.ConnectionOK
    m.runtime.ConnectionDetail = msg.Status
    m.runtime.SetError(nil)
}
```

And in the status bar, show the *severity*:

```go
switch {
case m.runtime.ConsecutiveErrors == 0:
    // green
case m.runtime.ConsecutiveErrors < 3:
    // yellow: the server may be restarting
default:
    // red: it is really down
}
```

⚠️ **One failed request is not an outage.** A single timeout during a
network hiccup should not turn the status bar red. The counter makes
the indicator robust.

## 24.12 The graceful degradation

**The situation.** The remote server is unreachable. The application
should still work with the local cache.

**The pattern.** Fall back transparently, and tell the user.

```go
func (s *Remote) List(ctx context.Context, q Query) (*PageResult, error) {
    result, err := s.listRemote(ctx, q)
    if err == nil {
        return result, nil
    }

    // Fall back to the local cache, if there is one.
    if s.fallback != nil {
        cached, cacheErr := s.fallback.List(ctx, q)
        if cacheErr == nil {
            return cached, nil
        }
    }

    return nil, err
}
```

⚠️ **A silent fallback is dangerous.** The user would believe they are
seeing fresh data. Return a flag with the result:

```go
type PageResult struct {
    Records    []*Record
    Page       int
    PerPage    int
    TotalItems int
    TotalPages int
    // Stale is true when the data comes from a fallback.
    Stale bool
}
```

And the UI shows it:

```go
if result.Stale {
    b.WriteString(m.theme.Warning.Render("showing cached data — the server is unreachable"))
}
```

## 24.13 The transaction across the boundary

**The situation.** You want to write to two collections atomically.

**The trap.** Two separate `Create` calls.

```go
// ✗ Not atomic: the second can fail after the first succeeds.
rec1, _ := store.Create(ctx, "a", data1)
rec2, _ := store.Create(ctx, "b", data2)
```

**The pattern.** The embedded backend can do it; the remote backend
cannot (not through the REST API).

```go
// In the embedded store.
func (s *Embedded) CreateMany(ctx context.Context, collection string, payloads []map[string]any) ([]*Record, error) {
    var created []*Record

    err := s.app.RunInTransaction(func(txApp core.App) error {
        for _, payload := range payloads {
            model, err := txApp.FindCachedCollectionByNameOrId(collection)
            if err != nil {
                return err
            }

            rec := core.NewRecord(model)
            rec.Load(payload)

            if err := txApp.Save(rec); err != nil {
                return err
            }
            created = append(created, recordFromCore(rec))
        }
        return nil
    })

    if err != nil {
        return nil, err
    }
    return created, nil
}
```

⚠️ **This is a method on the concrete type, not on the `Store`
interface.** Adding it to the interface would break the downstream
implementations.

The right way to expose it is an **optional interface**:

```go
// BulkStore is an optional interface for the stores that support
// transactional bulk writes.
type BulkStore interface {
    CreateMany(ctx context.Context, collection string, payloads []map[string]any) ([]*Record, error)
}
```

```go
if bulk, ok := store.(pbstore.BulkStore); ok {
    records, err := bulk.CreateMany(ctx, collection, payloads)
    // ...
}
```

⚠️ **The remote backend cannot do this.** The REST API has a batch
endpoint, but it is not transactionally equivalent across collections.
For the remote mode, accept the partial-failure risk, or design the
schema so that one record holds the aggregate.

## 24.14 Exercises

1. Implement the background poller from §24.1 and verify that the
   goroutine stops when the program quits.
2. Implement a modal overlay and a command palette.
3. Implement the two-phase load with a progress bar.
4. Implement the undo stack and the `u` hotkey.
5. Implement a virtualised list with keyset pagination.
6. Implement the CSV export and the CSV import.
7. Implement the connection indicator with the error counter.
8. Implement the stale-data fallback, with the `Stale` flag.

## 24.15 What you learned

- A **background worker** owns its state and uses `program.Send`; it
  must be cancellable.
- Prefer **`tea.Tick`** over a goroutine for a one-shot delay — it
  cannot leak.
- A **repeating tick** must stop rescheduling when it is not needed.
- A **modal** is owned by the root model and overlays the page; it
  consumes the messages.
- A **command palette** turns the application into a power tool.
- A **two-phase load** reports the progress; the command must not
  touch the page.
- An **undo stack** must be bounded and is not a transaction.
- A **virtualised list** needs keyset pagination to stay fast.
- An **export** writes to a file and respects the active filter.
- An **import** validates everything before writing anything.
- A **connection indicator** needs a failure counter to avoid flapping.
- A **fallback** must be flagged as stale — silence is dangerous.
- Optional capabilities go in **optional interfaces**, not in the
  published contract.

## 24.16 Further reading

- `tea.go` — `Send`, `Kill`, `ReleaseTerminal`.
- `commands.go` — `Tick`, `Every`.
- `bubbles/list` — the filtering that powers the command palette.
- `core/base.go` — `RunInTransaction`.

Next: [Chapter 25 — Domain Patterns](ch25-domain-patterns.md).
