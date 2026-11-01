# Chapter 7 — Concurrency: The Purity of Update

## 7.1 One goroutine, one truth

Bubble Tea's event loop calls `Update` from a single goroutine. Commands
run in their own goroutines. That gives you a very specific concurrency
model:

```
goroutine A (the event loop)          goroutine B..N (the commands)
────────────────────────────          ──────────────────────────────
reads the model                       reads the captured values
mutates the model                     does the I/O
renders the frame                     returns a message
        ▲                                    │
        └──────────── the message ───────────┘
```

The model is only ever touched by goroutine A. So:

> **You do not need a mutex to protect the model — as long as only
> `Update` touches it.**

This is the single biggest simplification Bubble Tea offers, and it is
why the Flint template has no mutexes anywhere.

But the corollary is just as important:

> **Goroutine B must never read the model.**

## 7.2 The race

Here is the bug that every Bubble Tea developer writes once:

```go
func (m *Model) loadRecords() tea.Cmd {
    return func() tea.Msg {
        time.Sleep(100 * time.Millisecond)   // simulate I/O
        return recordsLoadedMsg{m.records}   // ✗ reads the model
    }
}
```

`m.records` is read on the command goroutine, while the event loop may
be writing it. That is a data race: undefined behaviour, and in
practice, intermittent crashes or corrupted data.

The Go race detector finds it reliably:

```bash
go test -race ./...
```

### The fix

Capture what you need, by value, before the closure:

```go
func (m *Model) loadRecords() tea.Cmd {
    store := m.store
    collection := m.runtime.Collection
    filter := m.runtime.Filter

    return func() tea.Msg {
        result, err := store.List(ctx, pbstore.Query{
            Collection: collection,
            Filter:     filter,
        })
        if err != nil {
            return loadFailedMsg{err}
        }
        return recordsLoadedMsg{result}
    }
}
```

Every captured value is either immutable (a string) or safe for
concurrent use (the store, which has its own locking).

⚠️ **This is easy to get right and easy to break later.** When you
refactor the command and add `m.something` inside the closure, you
reintroduce the race. The race detector is the only reliable guard, so
**run the tests with `-race` in CI**.

## 7.3 What *is* safe to share

| Value                              | Safe? | Why                              |
| ---------------------------------- | ----- | -------------------------------- |
| `string`, `int`, `bool`            | ✅    | Immutable                       |
| A `[]byte` you do not mutate       | ✅    | Immutable contents              |
| A map you only read                | ⚠️    | Only if nothing writes it        |
| A pointer to the model             | ❌    | The model is mutated concurrently |
| A `*textinput.Model`               | ❌    | Same                             |
| The `pbstore.Store`                | ✅    | Documented as concurrency-safe    |
| A `sync.Mutex`                     | ✅    | That is its job                  |
| A `context.Context`                | ✅    | Designed for concurrent use      |

⚠️ **A slice is a trap.** `m.items` is a slice header. Copying it
copies the header, which still points at the same backing array. If
the event loop appends to it (and the array has spare capacity), the
append writes into the shared backing array — which the command
goroutine may be reading.

The safe pattern:

```go
// ✗ Shares the backing array.
items := m.items

// ✓ Copies the contents.
items := append([]string{}, m.items...)
```

Or, better, do not share mutable slices with commands at all. Pass the
immutable parameters and let the command produce fresh data.

## 7.4 The store is concurrency-safe

The `pbstore.Store` contract says so explicitly:

```go
// Implementations must be safe for concurrent use: Bubble Tea commands
// execute in background goroutines and may call the store while the
// render loop is active.
```

Both implementations honour it:

- `Embedded` wraps a PocketBase `core.App`, whose db layer is safe for
  concurrent use. The `Embedded` struct itself guards its `closed` flag
  with a `sync.RWMutex`.
- `Remote` uses `net/http`, whose `Client` is safe for concurrent use,
  and guards its `closed` flag the same way.

```go
type Embedded struct {
    app     core.App
    config  EmbeddedConfig
    allowed map[string]bool

    mu     sync.RWMutex
    closed bool
}

func (s *Embedded) isClosed() bool {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.closed
}
```

⚠️ **The `allowed` map is read-only after construction**, which is why
it does not need locking. If you make it mutable, guard it.

## 7.5 Blocking the event loop

The worst thing that can happen to a TUI is a blocked `Update`. The
symptoms are: the UI freezes, keys stop working, and the terminal
appears hung.

Three ways to block it:

### 1. A synchronous database call

```go
// ✗ Blocks the event loop for the duration of the query.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    case recordsLoadedMsg:
        m.records = msg.Records      // fine
    case refreshMsg:
        result, _ := m.store.List(ctx, query)   // ✗ blocks!
        m.records = result.Records
    }
}
```

Always move the call into a command.

### 2. A slow `View`

```go
func (m *Model) View() string {
    data, _ := json.MarshalIndent(m.hugeThing, "", "  ")   // ✗ slow
    return data
}
```

Marshal in `Update`, store the string, render the string.

### 3. A blocking channel operation

```go
// ✗ If nothing reads the channel, this blocks forever.
m.results <- result
```

Use a buffered channel, or a `select` with a default.

### How to detect it

Add a watchdog. If `Update` has not been called for a while, something
is wrong. But the simpler diagnostic is: **if the UI feels sluggish,
look for I/O in `Update` or `View` first.**

## 7.6 Cancellation and the context

Every store call takes a `context.Context`. This is not ceremony — it
is how you stop a long query at shutdown.

### Per-call timeouts

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

result, err := store.List(ctx, query)
```

A query that never returns (a corrupted index, a locked database) is
now bounded.

### Application lifetime

Create a root context in `main` and cancel it when the program exits:

```go
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    store, err := openStore(cfg)
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()

    model := app.New(store, cfg, ctx)

    program := tea.NewProgram(model, tea.WithAltScreen())
    // ...
}
```

Then the commands derive from it:

```go
func (p *RecordsPage) Load() tea.Cmd {
    store, collection := p.store, p.runtime.Collection

    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
        defer cancel()
        // ...
    }
}
```

Now a `SIGINT` cancels the root context, which cancels every in-flight
query, which unblocks the commands, which lets the program exit
promptly.

### The shutdown sequence

```go
// cmd/tui/main.go
func run() error {
    // ...
    store, err := openStore(cfg)
    if err != nil {
        return err
    }
    defer func() {
        if err := store.Close(); err != nil {
            logger.Error("failed to close the store", "error", err)
        }
    }()

    program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
    defer signal.Stop(sigCh)

    go func() {
        <-sigCh
        program.Quit()
    }()

    _, err = program.Run()
    return err
}
```

The order is important:

1. `SIGTERM` arrives → `program.Quit()`.
2. `program.Run()` returns.
3. The deferred `store.Close()` runs, releasing the database handles.
4. `main` returns, and the deferred `cancel()` stops any remaining
   goroutines.

⚠️ **The signal handler must not call `store.Close()` directly.** It
runs on its own goroutine, concurrently with the event loop, and
closing the store under it would race. Quit the program, and let the
deferred close happen on the main goroutine after `Run` returns.

## 7.7 The race detector

The single most valuable tool for this chapter.

```bash
go test -race ./...
```

The race detector instruments every memory access and reports
accesses that could be concurrent. It finds:
- a command reading the model;
- a shared map written from two goroutines;
- a slice mutated while being read.

⚠️ **The race detector only reports races it actually observes.** It
needs the code path to execute concurrently during the test. A race in
a rarely-taken branch may not be caught.

Mitigations:
- write tests that exercise the concurrent paths;
- run the tests repeatedly (`-count=10`);
- run the TUI under the detector manually.

### A test that catches the model-read race

```go
func TestModelReadRace(t *testing.T) {
    m := &raceModel{}

    cmd := m.load()
    // Run the command while the event loop mutates the model.
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

## 7.8 Concurrency in the store

The store has two kinds of concurrency to handle.

### Multiple commands in flight

The user can trigger several queries before the first returns:

```
User types "g"   → query A starts
User types "go"  → query B starts
```

Both run concurrently. The store must handle that — and it does, because
both the embedded and the remote backends are concurrency-safe.

But the *results* can arrive out of order. That is the staleness
problem from Chapter 3, solved with the page-number guard.

### The `Close` race

`Close()` can be called while a query is in flight. The embedded store
handles it:

```go
func (s *Embedded) Close() error {
    s.mu.Lock()
    defer s.mu.Unlock()

    if s.closed {
        return nil
    }
    s.closed = true

    if s.app != nil {
        if err := s.app.ClearBootstrap(); err != nil {
            return fmt.Errorf("pbstore: close embedded store: %w", err)
        }
    }
    return nil
}
```

And every method checks:

```go
if s.isClosed() {
    return nil, ErrClosed
}
```

⚠️ **There is a small window here.** A command can pass the
`isClosed()` check and then have the store closed underneath it. The
PocketBase app returns an error in that case, which becomes a toast.
That is acceptable: the operation fails with a clear error rather than
crashing.

A stricter design would use an `RWMutex` held for the duration of each
operation, but that would serialise the queries and defeat the
concurrency.

## 7.9 Goroutines you own

Sometimes you need a goroutine that is not a command — a background
poller, a file watcher, a signal handler.

```go
go func() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            program.Send(pollMsg{})
        }
    }
}()
```

The `program.Send` method delivers a message to the event loop from
outside:

```go
// Send a message to the program.
func (p *Program) Send(msg Msg)
```

⚠️ **Use `program.Send` sparingly.** It is the escape hatch for the
cases a command cannot cover (a signal, an external subscription). If
you find yourself using it often, the message probably belongs in a
command.

⚠️ **Always stop the goroutine.** The `select` on `ctx.Done()` is what
prevents a leak. Without it, the goroutine runs until the process
exits.

### The subscription pattern

PocketBase has a realtime subscription broker. A background goroutine
can subscribe and forward the events:

```go
func (m *Model) subscribe(ctx context.Context) tea.Cmd {
    return func() tea.Msg {
        // ... subscribe and forward the events
    }
}
```

## 7.10 Designing for the single-threaded loop

The mental model that makes Bubble Tea programs easy:

> **The event loop is the only writer. Commands are pure readers of
> captured values. The bridge between them is a message.**

If you keep that picture in mind, most concurrency bugs become obvious
at design time rather than at test time.

Concretely:

| If you want to…                        | Do this                                            |
| --------------------------------------- | -------------------------------------------------- |
| Read the model                         | Do it in `Update`                                   |
| Fetch data                             | Do it in a command, capture the parameters          |
| Share data between components          | Do it in `Update`, or via a message                  |
| Run something periodically              | A `tea.Tick`/`tea.Every` command                    |
| React to an external event             | A goroutine + `program.Send`                        |
| Stop a long operation                  | A cancellable `context.Context`                     |

## 7.11 Exercises

1. Write a deliberately racy program: a command that reads the model
   after a sleep. Run it under `go test -race` and read the report.
   Then fix it and confirm the report is gone.

2. Write a test that runs 50 commands concurrently against the
   embedded store. Verify that the results are correct and that the
   race detector is silent.

3. Add a `context.Context` to the root model, cancel it on shutdown,
   and verify that a long-running query is cancelled when the user
   quits.

4. Write a program with a blocking `View` (a `time.Sleep` inside it)
   and observe the freeze. Then move the work into a command.

5. Implement a background poller that sends a message every 5 seconds
   using `program.Send`. Verify the goroutine stops when the context is
   cancelled.

6. Add a `Close` race detector to the store: call `Close` from one
   goroutine while a `List` runs in another. Verify that the operation
   fails with a clean error rather than crashing.

## 7.12 What you learned

- The event loop is **single-threaded**; only `Update` may touch the
  model.
- **Commands must capture values, not the model** — reading the model
  from a command is a data race.
- **Slices and maps are shared** even when copied by assignment; copy
  the contents when you need an independent snapshot.
- The store is **safe for concurrent use**; that is part of its
  contract.
- **Never block `Update` or `View`** — all I/O belongs in commands.
- Every store call takes a **cancellable context**; a root context
  ties them to the application lifetime.
- The **shutdown order** is: stop the program → close the store → exit.
- Run the tests with **`-race`**; it is the only reliable guard
  against the mistakes above.

## 7.13 Further reading

- `internal/pbstore/embedded.go` — the concurrency-safe store, with
  the `closed` guard.
- `internal/pbstore/store.go` — the concurrency contract in the
  interface documentation.
- `cmd/tui/main.go` — the shutdown sequence and the signal handler.
- `internal/pages/records.go` — a command that captures values only.

Next: [Chapter 8 — PocketBase's Mental Model](ch08-pocketbase-mental-model.md).
