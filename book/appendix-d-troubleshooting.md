# Appendix D — Troubleshooting

A catalogue of the problems you are likely to hit, and how to diagnose
them.

## The TUI freezes

**Symptoms:** the keys stop working, the display is stuck, `ctrl+c`
does nothing.

**Cause 1 — I/O in `Update` or `View`.**

```go
// ✗ A database call in the update loop.
case refreshMsg:
    result, _ := m.store.List(ctx, query)     // blocks the event loop
    m.records = result.Records
```

**Fix:** move the call into a command.

**Cause 2 — a slow `View`.**

```go
// ✗ Marshalling on every render.
func (m *Model) View() string {
    data, _ := json.MarshalIndent(m.payload, "", "  ")
    return data
}
```

**Fix:** marshal in `Update`, store the string.

**Cause 3 — a blocking channel.**

```go
// ✗ Nobody reads the channel.
m.results <- result
```

**Fix:** a buffered channel, or a `select` with a default.

**Diagnosis:** add a temporary print at the top of `Update`. If it
stops being called, the loop is blocked.

## The terminal is left in a mess

**Symptoms:** after quitting, the terminal shows escape sequences, the
cursor is invisible, or the shell history is gone.

**Cause:** the alternate screen was not used, or the program was killed
with `SIGKILL`.

**Fix:** use `tea.WithAltScreen()`. For a `SIGKILL`, no program can
clean up — that is what `SIGTERM` is for.

⚠️ **Add a `SIGTERM` handler** so that a `kill` (not `kill -9`) exits
cleanly.

## The records never load

**Symptoms:** the table is empty, or it shows "no records" although
the collection has rows.

**Cause 1 — the page was never told to load.**

`Init()` is only called once, at startup. A page the user navigates to
later never gets its `Init`.

**Fix:** give every page a `Refresh()` and call it on navigation.

```go
func (m *Model) navigate(page state.Page) tea.Cmd {
    m.runtime.SetPage(page)
    return m.loadPage(page)
}
```

**Cause 2 — no collection is selected.**

**Fix:** the records page should show a clear message:
`"no collection selected — pick one on the home page"`.

**Cause 3 — the filter excludes everything.**

**Fix:** show the active filter in the header, and let the user clear
it with `esc`.

## The search returns nothing

**Symptoms:** the list shows "No items." even though `VisibleItems()`
returns the records.

**Cause:** the item does not implement `list.DefaultItem`.

```go
// ✗ Not rendered by the default delegate.
type SearchItem struct {
    Record *pbstore.Record
    Title  string
}
func (i SearchItem) FilterValue() string { return i.Title }

// ✓ Rendered.
type SearchItem struct {
    Record  *pbstore.Record
    Name    string
    Summary string
}
func (i SearchItem) FilterValue() string { return i.Name + " " + i.Summary }
func (i SearchItem) Title() string       { return i.Name }
func (i SearchItem) Description() string { return i.Summary }
```

**Cause:** the list is in the `Filtering` state, so `SetItems` filters
the items away.

**Fix:** `p.list.SetFilterState(list.Unfiltered)` before
`SetItems`.

**Cause:** the list's `/` binding stole the key and put it in the
`Filtering` state.

**Fix:** do not forward the key presses to the list.

## The filter returns an error

**Symptoms:** `invalid left operand "rank" - unknown field "rank"`.

**Cause:** the field is not in the schema. Custom (non-schema) data is
**not filterable**.

**Fix:** add the field to the collection:

```go
collection.Fields.Add(&core.NumberField{Name: "rank"})
```

**Symptoms:** `failed to resolve field "x"`.

**Cause:** the resolver does not allow the field.

**Fix:** list the allowed fields, or use a permissive resolver:

```go
search.NewSimpleFieldResolver(`^\w+[\w\.]*$`)
```

## The status bar shows "unknown"

**Symptoms:** the connection state is never updated.

**Cause:** the early `return`s in the root model's `Update` bypass the
status bar refresh.

**Fix:** call the refresh in every branch, or restructure:

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    cmd := m.update(msg)
    m.refreshStatusBar()
    return m, cmd
}
```

## The toast never disappears

**Symptoms:** the toast stays on the screen forever.

**Cause:** the expiry message is swallowed by the root model's switch.

**Fix:** handle it explicitly:

```go
case components.ToastExpiredMsg:
    updated, cmd := m.toast.Update(msg)
    m.toast = updated.(*components.Toast)
    return m, cmd
```

**Cause:** the toast's expiry check is inverted.

```go
// ✗ The guard prevents the toast from ever expiring.
if !t.visible || time.Now().Before(t.expiresAt) {
    return t, nil
}
```

**Fix:** the expiry message is scheduled by the `Show` method, so it
always refers to the current toast. Hide it unconditionally.

## Typing "q" quits the application

**Cause:** the global hotkey layer is not suspended when a text input
has focus.

**Fix:**

```go
func (m *Model) inputFocused() bool {
    switch m.runtime.Page {
    case state.PageSearch:
        return true
    case state.PageRecords:
        return m.records.FilterFocused()
    default:
        return false
    }
}
```

## The status bar overlaps itself

**Symptoms:** the hints run into the page name.

**Cause:** the width calculation does not account for the separator.

**Fix:** compute the gap from the *visible* widths, and drop the hints
one at a time until everything fits.

⚠️ **Use `lipgloss.Width`, never `len`.**

## The remote mode finds no collections

**Symptoms:** "no collection available".

**Cause:** `GET /api/collections` requires a superuser token.

```json
{"status": 401, "message": "The request requires valid record authorization token."}
```

**Fix:** provide a token, or use the `--collections` allowlist (the
store falls back to it when the endpoint is unauthorised).

## The sync fails with "no rows in result set"

**Cause:** the local collection does not exist.

**Fix:** create it in the sync:

```go
if _, err := s.app.FindCachedCollectionByNameOrId(local); err != nil {
    if _, err := s.EnsureDictionaryCollection(ctx, local, nil); err != nil {
        return nil, err
    }
}
```

## The sync fails with "id: Must be at least 15 character(s)"

**Cause:** the remote record id is shorter than the PocketBase
minimum.

**Fix:** normalise the id:

```go
func normalizeID(id string) string {
    if len(id) >= 15 {
        return id
    }
    return id + strings.Repeat("0", 15-len(id))
}
```

⚠️ **The padding must match `^\w+$`.** Padding with `-` produces
`Invalid value format`.

## A race is detected

**Symptoms:**

```
WARNING: DATA RACE
Write at 0x00c000... by goroutine 12:
  ...
Previous read at 0x00c000... by goroutine 8:
  ...
```

**Cause:** a command reads the model.

**Fix:** capture the values before the closure:

```go
// ✗ Reads the model on the command goroutine.
return func() tea.Msg {
    return loadedMsg{m.records}
}

// ✓ Captures only what it needs.
store, collection := m.store, m.runtime.Collection
return func() tea.Msg {
    // ...
}
```

## The build fails with "use of internal package not allowed"

**Cause:** an external module imports a package under `internal/`.

**Fix:** `internal/` packages are importable only from within the same
module. If you need to share the store with another module, move it
out of `internal/` or provide a public wrapper.

## `go build` fails with an `encoding/json/v2` error

**Cause:** PocketBase v0.40 uses `encoding/json/v2`, which is behind
the `GOEXPERIMENT=jsonv2` build tag.

**Fix:** the template does not import `encoding/json/v2` directly — it
uses the standard `encoding/json`. If you must import it, build with:

```bash
GOEXPERIMENT=jsonv2 go build ./...
```

## The binary is huge

**Symptoms:** the binary is 40 MB.

**Cause:** `modernc.org/sqlite` is a pure-Go SQLite implementation —
it is large.

**Fixes:**
- `-ldflags "-s -w"` strips the symbols (about 25% smaller);
- `-trimpath` removes the absolute paths;
- accept the size: it buys you a dependency-free, cross-compilable
  binary.

## The build is slow

**Cause:** the first build compiles the whole dependency tree.

**Fix:** this is a one-time cost. The Go build cache makes the
subsequent builds fast.

⚠️ **Do not clear `GOCACHE` to "fix" a slow build.** It makes it
slower.

## `go vet` reports a printf issue

**Symptoms:** `fmt.Errorf format %w has arg err of wrong type`.

**Cause:** the `%w` verb only works with an `error`.

**Fix:** check the actual type. For example, `ExpandRecords` returns a
`map[string]error`, not an `error` — join the values first.

## The tests pass locally but fail in CI

**Causes:**
- the tests are not hermetic (a fixed path, a shared file);
- the tests are order-dependent;
- a race that only appears sometimes.

**Fixes:**
- use `t.TempDir()`;
- run with `-race` in CI;
- run with `-count=10` locally to catch the flaky ones.

## The application is slow with a large collection

**Causes and fixes:**

| Symptom                        | Cause                         | Fix                          |
| ------------------------------ | ----------------------------- | ---------------------------- |
| The listing is slow            | No index on the filtered field | Add an index                 |
| The count is slow              | The `COUNT(DISTINCT ...)` query | `SkipTotal: true`            |
| The page switch is slow        | Deep pagination               | Keyset pagination            |
| Many queries per record        | The N+1 problem               | Use `expand`                 |
| The sync is slow               | One lookup per record         | Fetch the ids once           |

## The check-list for "something is wrong"

1. Does the `Update` get called? Add a print.
2. Is there I/O in `Update` or `View`? Move it to a command.
3. Run the tests with `-race`.
4. Check the stderr for the PocketBase logs (enable the dev mode
   temporarily).
5. Print the runtime state.
6. Reduce it: does the problem survive with a single page and a
   single record?
