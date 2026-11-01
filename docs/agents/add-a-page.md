# Skill: add a page

**Goal:** add a new screen to the TUI.

**Time:** ~20 minutes. Touches 5 files.

---

## Before you start

Decide two things:

1. **What does the page show?** A list? A form? A detail view? A chart?
2. **Does it need the store?** If it reads or writes data, it needs
   `pbstore.Store` and a `Refresh()`.

If the page only renders state already in `state.Runtime` (a summary of
the current collection, say), you can skip the store entirely.

---

## Step 1 — the page

Create `internal/pages/<name>.go`.

```go
package pages

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// StatsPage shows the statistics of the selected collection.
//
// It follows the standard page shape: a constructor, the four
// [tea.Model] methods and a Refresh method.
type StatsPage struct {
	store   pbstore.Store
	runtime *state.Runtime
	theme   *theme.Theme

	// loading reports whether a query is in flight.
	loading bool
	// total holds the number of records in the collection.
	total int
	// err holds the last error.
	err error
	// width is the available rendering width.
	width int
}

// Compile-time check that StatsPage satisfies [tea.Model].
var _ tea.Model = (*StatsPage)(nil)

// NewStatsPage creates the statistics page.
func NewStatsPage(store pbstore.Store, runtime *state.Runtime, t *theme.Theme) *StatsPage {
	if t == nil {
		t = theme.Default()
	}
	if runtime == nil {
		runtime = state.NewRuntime()
	}

	return &StatsPage{
		store:   store,
		runtime: runtime,
		theme:   t,
	}
}

// Init implements [tea.Model].
func (p *StatsPage) Init() tea.Cmd { return nil }

// Update implements [tea.Model].
func (p *StatsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		return p, nil

	case StatsMsg:
		p.loading = false
		if msg.Err != nil {
			p.err = msg.Err
			return p, nil
		}
		p.err = nil
		p.total = msg.Total
		return p, nil
	}

	return p, nil
}

// View implements [tea.Model].
func (p *StatsPage) View() tea.View {
	b := []string{p.theme.Title.Render("Statistics")}

	if p.loading {
		return tea.NewView(joinLines(append(b, "", "loading…")))
	}

	if p.err != nil {
		return tea.NewView(joinLines(append(b, "", p.theme.Error.Render(p.err.Error()))))
	}

	b = append(b, "", fmt.Sprintf("%d records", p.total))

	return tea.NewView(joinLines(b))
}

// Refresh reloads the statistics.
//
// The root model calls it when the page becomes active.
func (p *StatsPage) Refresh() tea.Cmd {
	p.loading = true

	return p.Load()
}

// Load returns the command that fetches the statistics.
//
// The store access happens inside the command closure, so the render
// loop is never blocked by a database call.
func (p *StatsPage) Load() tea.Cmd {
	store := p.store
	collection := p.runtime.Collection

	return func() tea.Msg {
		if store == nil || collection == "" {
			return StatsMsg{Err: fmt.Errorf("pages: no collection selected")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		total, err := store.Count(ctx, collection, "")
		if err != nil {
			return StatsMsg{Err: err}
		}

		return StatsMsg{Total: total}
	}
}

// StatsMsg carries the result of a statistics query.
type StatsMsg struct {
	// Total holds the number of matching records.
	Total int
	// Err holds the error, when the query failed.
	Err error
}
```

**Notice what is *not* in `Update`:** no store call, no `fmt.Println`,
no file access. `Update` only mutates state and returns commands.

---

## Step 2 — the page constant

Add to `internal/state/state.go`:

```go
const (
	// … the existing constants
	// PageStats is the statistics screen.
	PageStats Page = "stats"
)
```

Then update `Page.Title()` and `AllPages()`:

```go
func (p Page) Title() string {
	switch p {
	// … the existing cases
	case PageStats:
		return "Statistics"
	}

	return string(p)
}
```

---

## Step 3 — wire it into the root model

In `internal/app/app.go`:

**a. Add the field** to the `Model` struct:

```go
type Model struct {
	// … the existing fields
	stats *pages.StatsPage
}
```

**b. Construct it** in `New`:

```go
stats: pages.NewStatsPage(store, runtime, t),
```

**c. Route to it** in `updateActivePage`:

```go
case state.PageStats:
	updated, cmd := m.stats.Update(msg)
	m.stats = updated.(*pages.StatsPage)

	cmds = append(cmds, cmd)
```

**d. Render it** in `View`:

```go
case state.PageStats:
	body = m.stats.View()
```

**e. Load it** in `loadPage`:

```go
case state.PageStats:
	return m.stats.Refresh()
```

**f. Add a hotkey** in `handleGlobalKey`:

```go
case "7":
	return m.navigate(state.PageStats), true
```

And add the hint to the status bar hints in `statusInfo()`:

```go
{"7", "stats"},
```

---

## Step 4 — handle the message at the root

If the page emits a message the root model must react to (to show a
toast, update the status bar, etc.), add a case in `Model.Update`:

```go
case pages.StatsMsg:
	if msg.Err != nil {
		m.runtime.SetError(msg.Err)
		m.refreshStatusBar()
		return m, m.toast.Show(components.ToastError, msg.Err.Error())
	}
	m.runtime.SetError(nil)
```

If the page does not need root-level reaction, the message is still
forwarded to the active page automatically — `updateActivePage` runs for
every message that the root does not consume.

---

## Step 5 — test it

```go
// TestStatsPage verifies that the statistics page renders the total.
func TestStatsPage(t *testing.T) {
	store := &stubStore{records: []*pbstore.Record{
		{ID: "rec1", CollectionName: "vocab", Fields: map[string]any{"key": "go"}},
	}}
	page := pages.NewStatsPage(store, newRuntime(), theme.Default())

	updated, _ := page.Update(pages.StatsMsg{Total: 1})
	page = updated.(*pages.StatsPage)

	if !strings.Contains(page.View(), "1 records") {
		t.Errorf("View() = %q, want it to contain the total", page.View())
	}
}
```

The stub store lives in `internal/pages/pages_test.go`. It already
implements every method of `pbstore.Store` — `Count` returns
`len(s.records)`, so no new stub method is needed for this example.

---

## Step 6 — verify

```bash
gofmt -w ./internal
go build ./... && go test ./...
golangci-lint run
```

---

## Common mistakes

| Mistake                                                     | Consequence                                          |
| ----------------------------------------------------------- | ---------------------------------------------------- |
| Calling the store directly in `Update`                      | Blocks the render loop. The UI freezes.              |
| Forgetting `Refresh()`                                      | The page shows nothing when you navigate to it.       |
| Not adding the case to `loadPage`                           | The page opens empty and never populates.            |
| Not adding the case to `View`                               | The page renders as a blank screen.                   |
| Importing `internal/app` from the page                      | Import cycle. Emit a `NavigateMsg` instead.          |
| Building a `lipgloss.Style` inline                          | Inconsistent with the rest of the UI.                 |
| Returning an error from a command                            | It is discarded. Return a message with an `Err`.      |
| Returning a string from `View()`                             | It must return a `tea.View`. Use `tea.NewView(s)`.    |
| Embedding a component view without `.Content`                | A type error: `tea.View` is not a string.            |
| Using a `table` without calling `SetWidth`                   | The table renders nothing, with no error.             |

---

## Bubble Tea v2 notes

The template uses Bubble Tea v2 (`charm.land/bubbletea/v2`). Four
things differ from v1 and will bite you if you copy an old snippet.

**1. `View()` returns a `tea.View`.**

```go
func (p *StatsPage) View() tea.View {
	return tea.NewView(joinLines(b))
}
```

**2. Embedding a component takes its `.Content`.**

```go
b = append(b, p.table.View().Content)
```

**3. The alternate screen belongs to the view.**

There is no `tea.WithAltScreen()` option. The root model sets
`view.AltScreen = true` on every frame; a page does not need to.

**4. A `table` needs a width.**

A v2 table whose viewport has no width renders an empty table: no
error, no panic, just missing rows. If your page shows a table, set
the width when the page is resized:

```go
p.table.SetWidth(p.width)
```

and give the table a default at construction so it works before the
first resize:

```go
tbl := table.New(
	table.WithColumns(defaultColumns()),
	table.WithFocused(true),
	table.WithHeight(10),
	table.WithWidth(88),
)
```

---

## Navigating from another page

Pages cannot import `app`. Emit `pages.NavigateMsg` and let the root
model handle it:

```go
case tea.KeyPressMsg:
	if msg.String() == "s" {
		return p, func() tea.Msg { return pages.NavigateMsg{Page: state.PageStats} }
	}
```

The root model already handles `NavigateMsg`; you do not need to add
anything for it.
