# Chapter 19 — Performance

## 19.1 The three clocks

A TUI has three performance budgets, and they are different:

| Clock              | Budget    | Consequence of exceeding it           |
| ------------------ | --------- | -------------------------------------- |
| **The key latency**| < 16 ms   | The UI feels laggy under the finger    |
| **The render**     | < 16 ms   | The frame rate drops below 60 fps      |
| **The query**      | seconds   | The user waits (with a spinner)        |

The first two are *interactive* budgets: they must be met on every
frame. The third is a *network/disk* budget: it is allowed to be slow,
as long as the UI stays responsive and the user is told.

⚠️ **Most TUI performance problems are a query executed in the render
path.** If a query takes 500 ms and it happens inside `Update`, the UI
freezes for half a second. Move it to a command and the problem
disappears.

## 19.2 Measuring

### The render

```go
func BenchmarkView(b *testing.B) {
    m := newTestModel(b)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = m.View()
    }
}
```

### The update

```go
func BenchmarkUpdate(b *testing.B) {
    m := newTestModel(b)
    msg := tea.KeyPressMsg{Code: 'j', Text: "j"}

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        m.Update(msg)
    }
}
```

### The query

```go
func BenchmarkList(b *testing.B) {
    store := newEmbeddedStore(b)
    // ... seed 10,000 records ...

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        store.List(ctx, pbstore.Query{Collection: "items", PerPage: 30})
    }
}
```

⚠️ **Benchmark the query separately from the render.** They have
different bottlenecks and different fixes.

### In the terminal

For a real measurement, add a temporary instrumentation to `View`:

```go
func (m *Model) View() tea.View {
    start := time.Now()
    defer func() {
        if d := time.Since(start); d > 16*time.Millisecond {
            fmt.Fprintf(os.Stderr, "slow render: %s\n", d)
        }
    }()
    // ...
}
```

⚠️ **Remove the instrumentation before committing.** Or make it
conditional on a debug flag.

## 19.3 The render budget

### What is expensive in a render

| Operation                  | Cost      | Fix                            |
| -------------------------- | --------- | ------------------------------ |
| `fmt.Sprintf` in a loop    | Small     | Precompute the constants       |
| `lipgloss.Width` in a loop | Small     | Measure once                   |
| `json.Marshal`             | **Large** | Marshal in `Update`, store the string |
| `strings.Join` of 1000 items | Moderate | Cache it                       |
| Building a large table     | **Large** | Use a viewport                 |
| A database call            | **Fatal** | Never                           |

### The JSON viewer

The JSON viewer rebuilds the visible tree on every render:

```go
func (v *JSONViewer) render() {
    nodes := v.visibleNodes()

    lines := make([]string, 0, len(nodes))
    for i, node := range nodes {
        lines = append(lines, v.renderNode(node, i == v.cursor))
    }

    v.viewport.SetContent(strings.Join(lines, "\n"))
}
```

For a document with a few thousand nodes, this is fine. For a hundred
thousand, it is not.

The fix is to render only what the viewport shows:

```go
func (v *JSONViewer) render() {
    nodes := v.visibleNodes()

    // Only render the lines the viewport can display.
    start, end := v.viewport.VisibleBounds()
    if start >= len(nodes) {
        start, end = 0, 0
    } else if end > len(nodes) {
        end = len(nodes)
    }

    lines := make([]string, 0, end-start)
    for i := start; i < end; i++ {
        lines = append(lines, v.renderNode(nodes[i], i == v.cursor))
    }

    v.viewport.SetContent(strings.Join(lines, "\n"))
}
```

⚠️ **This is more complex, and it breaks the viewport's scrolling.** The
viewport scrolls by moving its own offset; if you only give it the
visible lines, it cannot scroll.

The correct approach for a large tree is to keep the full content but
cache the *rendered lines* and invalidate them only when the expansion
state changes:

```go
type JSONViewer struct {
    // ...
    cachedLines []string
    cacheValid  bool
}

func (v *JSONViewer) render() {
    if v.cacheValid {
        v.viewport.SetContent(strings.Join(v.cachedLines, "\n"))
        return
    }

    // ... rebuild v.cachedLines ...

    v.cacheValid = true
    v.viewport.SetContent(strings.Join(v.cachedLines, "\n"))
}
```

And invalidate on a toggle:

```go
func (v *JSONViewer) toggleCurrent() {
    // ...
    v.cacheValid = false
    v.render()
}
```

## 19.4 The update budget

### The cost of a message

Some messages are cheap; some are expensive.

| Message              | Cost                                    |
| -------------------- | --------------------------------------- |
| `tea.KeyPressMsg`    | Trivial                                 |
| `tea.WindowSizeMsg`  | Trivial, but causes a full re-layout    |
| `tea.MouseMsg`       | Trivial, but the mouse fires a lot      |
| A spinner tick       | Trivial                                 |
| A page message       | Depends on the handler                  |

⚠️ **The mouse is a message flood.** With mouse reporting enabled, a
drag generates dozens of `tea.MouseMsg` per second. Every one triggers
a full render.

The fix is to coalesce them:

```go
case tea.MouseMsg:
    // Remember the position, but do not re-render for every event.
    m.mouseX = msg.X
    m.mouseY = msg.Y
    return m, nil
```

⚠️ **But then the view is stale.** The right pattern is to schedule a
redraw:

```go
case tea.MouseMsg:
    m.mouseX, m.mouseY = msg.X, msg.Y
    return m, tea.Quit    // no; use a redraw message
```

Bubble Tea does not have a "request a redraw" primitive in the stable
API. The practical approach is to keep the mouse handling cheap and
let the next real message trigger the render.

### The resize storm

Dragging a terminal window generates a stream of `WindowSizeMsg`. Each
one triggers a full re-layout of every component.

```go
case tea.WindowSizeMsg:
    m.width = msg.Width
    m.height = msg.Height
    m.resize()
```

The `resize()` propagates to every page and component. That is
normally cheap, but if a component does something expensive (rebuilding
a large table), the storm is visible.

## 19.5 The query budget

### The count query

This is the biggest hidden cost. The search provider runs **two**
queries: the page query and the count query.

```go
if !s.skipTotal {
    // execute the 2 queries concurrently
    errg := new(errgroup.Group)
    errg.Go(routine.SafeWrap(countExec))
    errg.Go(routine.SafeWrap(modelsExec))
    if err := errg.Wait(); err != nil {
        return nil, err
    }
}
```

They run concurrently, so the wall-clock cost is the slower of the two,
not the sum. But the count query on a large table is not free.

⚠️ **`SkipTotal` removes it entirely.** Use it when the total count is
not displayed.

The template's pagination *does* display the total ("3 records · page
1/1"), so it needs the count. But the search page does not, so it could
use `SkipTotal`.

### The indexes

The filter `key = "go"` is fast **if there is an index on `key`**.

PocketBase creates the indexes you declare on the collection:

```go
collection.Indexes = []string{
    "CREATE INDEX `idx_vocab_key` ON `vocab` (`key`)",
}
```

⚠️ **Add an index for every field you filter or sort on.** Without one,
SQLite does a full table scan.

⚠️ **The index name must be unique across the database**, and the
column must be quoted with the backticks that PocketBase uses.

⚠️ **An index on a JSON field's extracted path is possible**, since the
resolver generates `JSON_EXTRACT(...)` expressions:

```go
collection.Indexes = []string{
    "CREATE INDEX `idx_items_payload_lang` ON `items` (JSON_EXTRACT(`payload`, '$.lang'))",
}
```

This is a real optimisation for a lookup table keyed by a JSON
attribute.

### The pagination

`LIMIT`/`OFFSET` pagination is O(offset). Page 1000 of 30 records per
page scans 30,000 rows to skip them.

⚠️ **For deep pagination, use keyset pagination** (the "seek"
method): filter with `id > "last_id"` instead of using an offset.

```go
result, err := store.List(ctx, pbstore.Query{
    Collection: "items",
    Filter:     fmt.Sprintf(`id > %q`, lastID),
    Sort:       "id",
    PerPage:    30,
})
```

The `Store` interface supports this because the filter is the native
grammar.

⚠️ **Keyset pagination requires a stable, unique sort key.** The `id`
is ideal.

### The N+1 problem

```go
// ✗ One query per record.
for _, rec := range records {
    author, _ := store.Get(ctx, "authors", rec.StringField("authorId"))
    fmt.Println(author)
}
```

⚠️ **This is the classic N+1.** The fix is the `expand` parameter:

```go
result, err := store.List(ctx, pbstore.Query{
    Collection: "items",
    Expand:     "author",
})
```

The expansion is resolved in one query (or a small number of them),
not one per record.

## 19.6 The sync budget

A sync of 100,000 records involves:
- 500 HTTP requests (at 200 per batch);
- 100,000 local reads (to check if the record exists);
- 100,000 local writes.

The local reads are the problem. Each `FindRecordById` is a query.

### The optimisation

Fetch the existing local ids **once**, then compare in memory:

```go
// Fetch the ids of the local records that are already there.
existing := map[string]bool{}
records, err := s.app.FindAllRecords(local)
for _, rec := range records {
    existing[rec.Id] = true
}

// Then, in the loop:
if existing[rec.ID] {
    // update
} else {
    // insert
}
```

⚠️ **This trades memory for time.** For 100,000 records, the id map is
a few megabytes — acceptable. For 10 million, it is not.

### The batch insert

PocketBase's `Save` does one INSERT per record. For a large sync, a
bulk insert would be much faster, but it would bypass the validation
and the hooks.

⚠️ **Do not use `SaveNoValidate` for a bulk insert** unless you have
validated the data yourself. A malformed record would corrupt the
cache.

## 19.7 The embedded database

### SQLite pragmas

The default SQLite settings are conservative. PocketBase configures
some of them, but you can tune them:

```go
app := pocketbase.NewWithConfig(pocketbase.Config{
    DefaultDataDir: dir,
})
```

For a read-heavy workload, the important pragma is the WAL mode, which
PocketBase enables by default:

```sql
PRAGMA journal_mode = WAL;
```

WAL lets readers and a writer work concurrently, which matters when the
TUI is reading while a sync is writing.

### The busy timeout

If a write is attempted while another connection holds the lock,
SQLite returns `SQLITE_BUSY` immediately. The busy timeout makes it
wait:

```go
PRAGMA busy_timeout = 5000;
```

PocketBase sets a query timeout (`core.DefaultQueryTimeout`), but the
busy timeout is a different thing.

⚠️ **Do not set a very long busy timeout.** A TUI that blocks for 30
seconds on a lock is worse than one that reports the error.

## 19.8 The remote backend

### The connection pool

`net/http` pools connections by default. The default limits are
generous for a TUI (which makes few concurrent requests).

```go
client := &http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        MaxIdleConnsPerHost: 10,
    },
}
```

### The compression

The REST API supports gzip:

```go
req.Header.Set("Accept-Encoding", "gzip")
```

⚠️ **Go's `http.Transport` handles this automatically** — it adds the
header and decompresses the response. Setting it manually can cause a
double-decompression bug.

### The timeout

```go
Timeout: 30 * time.Second,
```

A TUI should never wait more than a few seconds for a query. The user
can always retry.

⚠️ **A timeout that is too long makes the UI feel broken.** 30 seconds
is already generous. Consider 10.

## 19.9 Profiling

### The CPU profile

```go
import "runtime/pprof"

f, _ := os.Create("cpu.prof")
pprof.StartCPUProfile(f)
defer pprof.StopCPUProfile()
```

Then:

```bash
go tool pprof cpu.prof
```

⚠️ **Profile the whole program, not a benchmark**, if the problem is
in the render loop. A benchmark profile shows the hot function but not
the cumulative call tree of the real application.

### The trace

```go
import "runtime/trace"

f, _ := os.Create("trace.out")
trace.Start(f)
defer trace.Stop()
```

```bash
go tool trace trace.out
```

The trace shows the goroutine interactions — which is exactly what you
need for a concurrency bug.

## 19.10 A performance checklist

Run through this when the application feels slow:

1. **Is there I/O in `Update` or `View`?** Move it to a command.
2. **Is `View` doing expensive work?** Cache it, invalidate on change.
3. **Is the query slow?** Check the indexes.
4. **Are you fetching more than you display?** Set `SkipTotal`, use
   `expand` instead of N+1.
5. **Is the query returning too much?** Use the `fields` parameter.
6. **Are you deep-paginating?** Use keyset pagination.
7. **Is the sync slow?** Fetch the local ids once, not per record.
8. **Is the mouse flooding you?** Coalesce the events.
9. **Is the render measured?** Profile it.
10. **Is it the terminal?** A slow terminal (SSH over a slow link)
    makes everything look slow. Test locally.

## 19.11 Exercises

1. Write a benchmark for `View()` of your root model. Find the hot
   spot.
2. Add 100,000 records to a collection and measure a listing with and
   without an index on the filtered field.
3. Implement keyset pagination and compare it with the offset
   pagination at page 500.
4. Profile a JSON viewer with a 100,000-node document. Implement the
   line cache and measure the improvement.
5. Measure a sync of 10,000 records. Implement the "fetch the ids
   once" optimisation and measure.
6. Find the N+1 in a real application of yours and replace it with an
   `expand`.
7. Enable the mouse in your application and measure the CPU usage while
   dragging.

## 19.12 What you learned

- Three budgets: **key latency**, **render**, **query**.
- **I/O in `Update` or `View` is the #1 performance bug.**
- `View` must be cheap: **cache** expensive output, invalidate on
  change.
- The **count query** is a hidden cost; `SkipTotal` removes it.
- **Indexes** make the filters fast; add one per filtered field.
- **Deep pagination is O(offset)**; use **keyset pagination**.
- The **N+1 problem** is solved by `expand`, not by more queries.
- A sync can be optimised by fetching the **local ids once**.
- The **WAL mode** lets the TUI read while a sync writes.
- Measure with `pprof` and `go tool trace`.

## 19.13 Further reading

- `internal/components/jsonviewer.go` — the rendering hot path.
- `internal/pbstore/embedded.go` — the query construction.
- `internal/pbstore/queries.go` — `QueryParams` and `SkipTotal`.

Next: [Chapter 20 — Shipping: Configuration, Shutdown, Packaging](ch20-shipping.md).
