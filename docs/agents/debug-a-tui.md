# Skill: debug a TUI

**Goal:** find out why the UI is frozen, blank or wrong.

**Time:** depends. Work through the symptoms below in order.

---

## The diagnostic order

Work top to bottom. Each step eliminates a class of causes.

1. **Is the render loop blocked?** → §1
2. **Is the message arriving?** → §2
3. **Is the message arriving too late?** → §3 (the stale response)
4. **Is the state reaching the view?** → §4
5. **Is the layout wrong?** → §5

---

## §1 The UI is frozen

**The symptom:** keys do nothing, the spinner stops, the screen is stuck.

**Cause 1 — I/O in `Update` or `View`.** A store call, an HTTP request,
a file read or a `fmt.Println` inside `Update` blocks the whole program.

```go
// WRONG: blocks the render loop.
func (p *RecordsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	result, err := p.store.List(ctx, q) // ❌
	// ...
}

// RIGHT: the call happens in the command goroutine.
func (p *RecordsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// ...
}

func (p *RecordsPage) Load() tea.Cmd {
	// Capture the values, not the model: the model mutates while the
	// command runs.
	store := p.store
	collection := p.runtime.Collection
	filter := p.runtime.Filter
	page := p.runtime.PageNumber
	perPage := p.runtime.PerPage

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := store.List(ctx, pbstore.Query{
			Collection: collection,
			Filter:     filter,
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

**How to confirm:** put a `fmt.Fprintln(os.Stderr, "update")` at the top
of `Update`. If it stops printing, `Update` is blocked.

**Cause 2 — a slow `View`.** `View` runs on every keystroke. If it
marshals JSON, formats a large slice or computes something expensive, the
UI stutters. Precompute in `Update`, store the string.

**Cause 3 — a blocking channel.** A `chan` with no reader blocks the
goroutine forever.

```go
// WRONG
ch := make(chan int)
ch <- 1 // ❌ blocks forever if nobody reads

// RIGHT
ch := make(chan int, 1)
ch <- 1
```

**Cause 4 — a deadlock on a mutex.** Never hold a lock across a call
that can block.

---

## §2 The message never arrives

**The symptom:** the page stays empty, no error is shown.

**Check 1 — is the message handled in the right place?** The root model
handles global messages; the page handles its own. If the root model
consumes a message with an early `return`, the page never sees it.

```go
// In app.Model.Update, this RETURNS and skips updateActivePage.
case pages.HealthMsg:
	return m, cmd // ❌ the page never receives it
```

**Check 2 — is the message type right?** A message defined in the page
package and a message defined in `app` are different types. The root
model matches on `pages.RecordsMsg`; the page matches on
`pages.RecordsMsg` — but `app.HealthMsg` is a different type entirely.

**Check 3 — is the page active?** `updateActivePage` switches on
`m.runtime.Page`. If the page constant is not in the switch, its
`Update` is never called.

**Check 4 — is the command actually returned?** A command that is
created but not returned never runs.

```go
// WRONG
cmd := p.load()
return p, nil // ❌ cmd is dropped

// RIGHT
return p, cmd
```

**Check 5 — is `Refresh()` called?** The root model calls `loadPage` on
navigation. A page that is missing from `loadPage` never loads.

---

## §3 The data is stale

**The symptom:** you press `right` to go to page 2, and page 1's data
appears — or the data flickers between two pages.

**Cause — the missing stale response guard.** A slow page 1 query
resolves after the user moved to page 2 and overwrites the rows.

```go
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
```

The message must carry the page it was requested for, and the handler
must compare it to the current page. See
[`REFERENCE.md`](REFERENCE.md) § *Stale response guard*.

---

## §4 The state does not reach the view

**The symptom:** the data is in the model but the screen shows nothing.

**Check 1 — is `applyResult` called?** If the guard rejects the message,
or the handler returns early, the state is never applied.

**Check 2 — is the field initialised?** A struct field that is never set
keeps its zero value. `total int` shows `0`, `err error` is `nil`.

**Check 3 — is the view reading the right field?** A typo
(`p.records` vs `p.records`) compiles and shows nothing.

**Check 4 — is the content actually there?** Dump it:

```go
func (p *RecordsPage) View() string {
	out := p.theme.Title.Render("Records")
	out += "\nDEBUG records=" + fmt.Sprintf("%d", len(p.records))

	return out
}
```

Remove the debug line afterwards.

**Check 5 — is the row actually added to the table?** `applyResult` must
call `p.table.SetRows(rows)`. The table renders its own rows; setting
`p.records` alone does not update the screen.

---

## §5 The layout is wrong

**The symptom:** overlapping text, truncated columns, a status bar that
overruns the terminal.

**Cause 1 — the width is not propagated.** The root model receives
`tea.WindowSizeMsg` and must forward it. Each page stores it and
recomputes its layout.

**Cause 2 — a fixed width.** Prefer `lipgloss.Width` and the layout
helpers:

```go
left, right := theme.SplitWidth(p.width, 40, 2)
```

**Cause 3 — wide characters.** `len(s)` counts bytes; a CJK character is
3 bytes but 2 columns wide. Use `lipgloss.Width(s)`.

**Cause 4 — a style that adds padding.** `lipgloss.Style.Render` adds
padding and borders. Measure the *rendered* string, not the raw one.

**Cause 5 — the terminal is genuinely too narrow.** The status bar
degrades by dropping hints; check that path.

---

## §6 The remote backend misbehaves

**The symptom:** the embedded mode works, the remote mode returns
nothing.

**Check 1 — the token.** A collection that is not public returns an
**empty list**, not an error, for an unauthenticated request. This is
the most common cause of "the remote returns no data".

```bash
FLINT_REMOTE_TOKEN=your_token flint --mode remote --remote-url http://...
```

**Check 2 — the allowlist.** `remote.collections` restricts what is
reachable. An empty list means "all".

**Check 3 — the URL.** It is the base URL, without `/api`.

**Check 4 — the health check.** The settings page shows the connection
status. Run:

```bash
curl -s http://127.0.0.1:8090/api/health
```

**Check 5 — the collection rules.** In the PocketBase admin UI, check
the list rule of the collection. If it is not `""`, it needs
authentication.

---

## §7 The embedded backend misbehaves

**The symptom:** "no such table", a stale schema, migrations not applied.

**Check 1 — the data directory.** The default is `./pb_data`. If you
changed it, the old database is elsewhere.

**Check 2 — the migrations.** Apply them explicitly:

```bash
go run ./cmd/tui --mode embedded --data-dir ./pb_data --migrate-only
```

**Check 3 — a stale schema.** PocketBase does not remove a field when
you delete it from the migration file. Write a new migration, or delete
`pb_data` in development.

**Check 4 — the collection does not exist.** The dictionary collections
are created on demand by `EnsureDictionaryCollection`. A `sql: no rows in
result set` error usually means the collection was never created.

---

## §8 The program exits with the wrong status

**The symptom:** `ctrl+c` exits with status 1 and prints an error.

Bubble Tea returns `tea.ErrInterrupted` on SIGINT. A locally invented
sentinel never matches.

```go
func isBenignShutdown(err error) bool {
	return errors.Is(err, tea.ErrInterrupted) ||
		errors.Is(err, tea.ErrProgramKilled)
}
```

**Do not install a second `signal.Notify`.** Bubble Tea already handles
SIGINT and SIGTERM; a second handler races with it and the outcome
becomes timing dependent.

---

## §9 The tests pass but the app is broken

**The cause:** the test does not exercise the real path.

- The test calls `Update` directly with a hand-made message, so the
  command never runs and the real store is never touched.
- The test uses a stub store that returns fixed data, so a broken filter
  or a wrong field name is invisible.

**Fix:** add a test that goes through the store:

```go
func TestRecordsPageLoadsRealRecords(t *testing.T) {
	// Use a real embedded store with a real collection.
	store := newEmbeddedStore(t)
	// ...
}
```

The `internal/pbstore` tests already do this — look at
`embedded_test.go` for the pattern.

---

## Escape hatches

```bash
# Run the tests verbosely.
go test ./internal/pages/ -run TestRecords -v

# Race detector — catches concurrent map access.
go test -race ./...

# Check what the linters think.
golangci-lint run

# Print the actual output of a view.
go test ./internal/pages/ -run TestRecords -v 2>&1 | head -50
```

---

## When you are truly stuck

The book has a symptom → cause → fix appendix:

[`../../book/appendix-d-troubleshooting.md`](../../book/appendix-d-troubleshooting.md)

It covers the frozen UI, the garbled terminal, the empty table, the
filtered list, the invalid filter and the field resolution errors.
