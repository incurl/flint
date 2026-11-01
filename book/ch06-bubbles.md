# Chapter 6 — Bubbles: Composition over Reinvention

## 6.1 What Bubbles is

Bubbles is a collection of small, focused TUI components — each one a
self-contained `tea.Model`:

| Package        | Component                     | What it does                        |
| -------------- | ----------------------------- | ----------------------------------- |
| `textinput`    | `textinput.Model`             | A single-line text field            |
| `textarea`     | `textarea.Model`              | A multi-line text field             |
| `table`        | `table.Model`                 | A scrollable, sortable table         |
| `list`         | `list.Model`                  | A filterable list with a delegate   |
| `viewport`     | `viewport.Model`              | A scrollable pane of content        |
| `spinner`      | `spinner.Model`               | An animation frame                  |
| `paginator`    | `paginator.Model`             | Page navigation                     |
| `progress`     | `progress.Model`              | A progress bar                      |
| `help`         | `help.Model`                  | A key hint bar                      |
| `key`          | `key.Binding`                 | Declarative key bindings            |
| `cursor`       | `cursor.Model`                | A blinking cursor                   |
| `filepicker`   | `filepicker.Model`            | A file browser                      |

The value proposition is not that these components exist — it is that
they are **composable**. Each one is a `tea.Model` with `Init`, `Update`
and `View`, so you nest them exactly like you nest your own models.

## 6.2 The composition pattern

A Bubbles component is used in three steps:

1. **Construct** it.
2. **Forward messages** to it in your `Update`.
3. **Render** it in your `View`.

```go
type recordsPage struct {
    table   table.Model
    filter  textinput.Model
    spinner spinner.Model
}

func newRecordsPage() *recordsPage {
    tbl := table.New(
        table.WithColumns([]table.Column{
            {Title: "Title", Width: 32},
            {Title: "Summary", Width: 36},
            {Title: "ID", Width: 20},
        }),
        table.WithFocused(true),
        table.WithHeight(10),
    )
    tbl.SetStyles(tableStyles(t))

    filter := textinput.New()
    filter.Placeholder = "filter, eg. title ~ \"go\""
    filter.Prompt = "filter: "
    filter.CharLimit = 200

    return &recordsPage{
        table:   tbl,
        filter:  filter,
        spinner: spinner.New(),
    }
}
```

```go
func (p *recordsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case recordsLoadedMsg:
        p.table.SetRows(rowsFor(msg.records))
        return p, nil
    }

    var cmds []tea.Cmd
    var cmd tea.Cmd

    p.table, cmd = p.table.Update(msg)
    cmds = append(cmds, cmd)

    p.spinner, cmd = p.spinner.Update(msg)
    cmds = append(cmds, cmd)

    return p, tea.Batch(cmds...)
}
```

```go
func (p *recordsPage) View() string {
    return p.filter.View() + "\n\n" + p.table.View()
}
```

⚠️ **Order matters in `Update`.** Bubbles components generally consume
the messages they care about. If you forward a key to both the
`textinput` and the `table`, and both handle it, you get double
handling.

The `textinput` has a `Focus`/`Blur` mechanism precisely for this: an
unfocused input ignores most keys, so you can forward everything and
only the focused one reacts.

## 6.3 The table

The `table` component is the workhorse for a record browser.

### Columns

```go
type Column struct {
    Title string
    Width int
}
```

### Rows

```go
type Row []string

tbl.SetRows([]table.Row{
    {"Go", "vocab · 28vntlshlihnmum", "28vntlshlihnmum"},
})
```

⚠️ **The row must have as many cells as there are columns.** If it has
fewer, the table panics or renders oddly. If it has more, the extras
are ignored.

### Styles

```go
tbl.SetStyles(table.Styles{
    Selected: t.TableSelected,
    Header:   t.TableHeader.Inherit(t.TableHeaderBorder),
    Cell:     t.TableCell,
})
```

The styles come from the theme — never inline.

### Sizing and cursor

```go
tbl.SetHeight(h)
tbl.SetWidth(w)
tbl.MoveUp(1)
tbl.MoveDown(1)
tbl.Cursor()      // the index of the selected row
```

### The selection → record mapping

⚠️ **The table knows about rows, not about your records.** You must
keep the mapping yourself:

```go
// records holds the records of the current page.
records []*pbstore.Record

func (p *RecordsPage) applyResult(result *pbstore.PageResult) {
    rows := make([]table.Row, 0, len(result.Records))
    for _, rec := range result.Records {
        rows = append(rows, table.Row{
            p.renderer.Title(rec),
            p.renderer.Summary(rec),
            rec.ID,
        })
    }

    p.records = result.Records
    p.table.SetRows(rows)
    p.table.SetCursor(0)
}
```

```go
func (p *RecordsPage) SelectedRecord() *pbstore.Record {
    cursor := p.table.Cursor()
    if cursor < 0 || cursor >= len(p.records) {
        return nil
    }
    return p.records[cursor]
}
```

⚠️ **Keep `records` in sync with the rows.** An earlier version of the
template tried to read the records from `runtime.Records`, which was a
shared slice that was never populated — so `SelectedRecord()` always
returned nil and the "open record" hotkey did nothing.

The rule: **the page owns the data behind its table.**

## 6.4 The textinput

```go
input := textinput.New()
input.Placeholder = "filter, eg. title ~ \"go\""
input.Prompt = "filter: "
input.CharLimit = 200
input.Width = 40
```

### Focus

```go
input.Focus()
input.Blur()
input.Focused()
```

⚠️ **An unfocused `textinput` still renders its prompt and value.** It
just ignores the keys. If you want it to look inactive, change the
style or the prompt.

### The value

```go
input.Value()          // the current text
input.SetValue(text)   // replace it
input.Reset()          // clear it
input.CursorEnd()      // move the cursor to the end
```

⚠️ **`SetValue` moves the cursor to the end** and **resets the
selection**. If you call it on every keystroke to "restore" the value,
you will fight the user's cursor. Only call it when you are
*replacing* the content deliberately.

### The `CharLimit`

`CharLimit = 0` means unlimited. Setting a limit is good hygiene for a
filter box (the native PocketBase filter has a maximum length of 3500
characters anyway).

## 6.5 The textarea

The `textarea` is the multi-line text input. The template uses it for
the raw JSON editor:

```go
ta := textarea.New()
ta.Placeholder = "{\n  \"key\": \"value\"\n}"
ta.ShowLineNumbers = false
ta.CharLimit = 0
```

```go
ta.SetValue(raw)
ta.SetWidth(w)
ta.SetHeight(h)
ta.Value()
ta.CursorEnd()
```

### Validation before saving

The editor's whole purpose is to produce valid JSON:

```go
func (e *JSONEditor) Validate() (map[string]any, error) {
    raw := strings.TrimSpace(e.textarea.Value())
    if raw == "" {
        return nil, errors.New("the document is empty")
    }

    var doc map[string]any
    if err := json.Unmarshal([]byte(raw), &doc); err != nil {
        return nil, err
    }
    return doc, nil
}
```

⚠️ **Validate before sending to the store.** A malformed document
would be rejected by the server anyway, but validating locally gives a
better error message and avoids a round trip.

⚠️ **Note the `map[string]any` target.** Unmarshalling into a map
rejects a valid JSON *array* or *scalar*. If your records can have
array roots, use `any`:

```go
var doc any
if err := json.Unmarshal([]byte(raw), &doc); err != nil {
    return nil, err
}
```

## 6.6 The viewport

```go
vp := viewport.New(20, 10)
vp.SetContent(text)
vp.SetY(0)
vp.GotoTop()
vp.GotoBottom()
vp.LineDown(1)
```

The viewport handles scrolling, including the mouse wheel if mouse
support is enabled.

⚠️ **Always size it.** `viewport.New(20, 10)` is a guess. When the
terminal size is known, call `SetSize`:

```go
case tea.WindowSizeMsg:
    vp.SetWidth(msg.Width)
    vp.SetHeight(msg.Height - 4)
```

## 6.7 The spinner

```go
sp := spinner.New()
sp.Spinner = spinner.Dot
sp.Style = t.Accent
```

The available animations are `spinner.Dot`, `spinner.MiniDot`,
`spinner.Jump`, `spinner.Pulse`, `spinner.Points`, `spinner.Globe`,
`spinner.Moon`, `spinner.Meteor`, `spinner.Ellipsis`, and more.

### How the spinner ticks

As described in Chapter 3, the spinner is driven by a self-rescheduling
tick:

```go
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
    switch msg := msg.(type) {
    case TickMsg:
        // Advance the frame and schedule the next tick.
        m.frame++
        ...
        return m, m.tick(m.id, m.tag)
    default:
        return m, nil
    }
}
```

So the usage pattern is:

```go
// Start it.
cmd := sp.Tick

// Keep it alive by forwarding the tick messages.
updated, cmd := sp.Update(msg)
```

⚠️ **The spinner only advances when it receives its tick message.** If
your page's `Update` has an early `return` for a message type, and that
message type is what the spinner needs, the animation freezes. This is
why the template forwards to the spinner in the `default` branch.

## 6.8 The list

The `list` is a filterable, paginated list of items with a delegate
that renders them.

```go
l := list.New([]list.Item{}, list.NewDefaultDelegate(), 40, 12)
l.Title = "Search results"
l.SetShowStatusBar(false)
l.SetShowHelp(false)
l.SetShowPagination(false)
l.SetShowTitle(false)
l.SetFilteringEnabled(false)
l.SetFilterState(list.Unfiltered)
```

### The delegate contract

⚠️ **This is the single most common Bubbles mistake.** The default
delegate only renders items that implement `list.DefaultItem`:

```go
type DefaultItem interface {
    Item
    Title() string
    Description() string
}
```

If your item only implements `FilterValue()`, the list will show
**"No items."** even though `VisibleItems()` returns them.

```go
// ✗ Not rendered.
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

⚠️ **And note the field/method name collision.** You cannot have a
field `Title` and a method `Title()` on the same struct. The template
names the field `Name` and the method `Title()`.

This bug cost real debugging time in the template: the search results
were empty, `VisibleItems()` returned 3 items, and the delegate was
silently rendering nothing.

### The filtering state

The list has three filter states: `Unfiltered`, `Filtering` and
`FilterApplied`. `SetItems` behaves differently in each:

```go
func (m *Model) SetItems(i []Item) tea.Cmd {
    m.items = i

    if m.filterState != Unfiltered {
        m.filteredItems = nil
        cmd = filterItems(*m)
    }
    // ...
}
```

So if the list is in the `Filtering` state when you set the items, the
items get filtered against the (empty) filter input and disappear.

The template sets the state explicitly:

```go
// The results are never filtered by the list itself: the page filter
// input is the single source of truth.
p.list.SetFilterState(list.Unfiltered)
p.list.SetItems(items)
```

### The list's own key bindings

⚠️ **The list binds `/` to its filter.** If your page also uses `/` for
something, the list will steal it and enter the `Filtering` state.

The template resolves this by **not forwarding the key presses to the
list at all** — the page's own filter is the only filter:

```go
// The page owns a dedicated PocketBase filter input, so the key
// presses are never forwarded to the bubbles list: its own filter
// binding ("/") would otherwise shadow the page filter.
var cmd tea.Cmd
p.input, cmd = p.input.Update(msg)
return p, cmd
```

The consequence is that the list's scrolling keys (`j`/`k`) stop
working. If you need them, forward the navigation keys explicitly:

```go
switch msg.String() {
case "up", "k":
    p.list.CursorUp()
    return p, nil
case "down", "j":
    p.list.CursorDown()
    return p, nil
default:
    var cmd tea.Cmd
    p.input, cmd = p.input.Update(msg)
    return p, cmd
}
```

### Size and pagination

```go
l.SetSize(width, height)
```

⚠️ **The list's height determines its page size.** In `updatePagination`:

```go
m.Paginator.PerPage = max(1, availHeight/(m.delegate.Height()+m.delegate.Spacing()))
```

The `availHeight` is the height minus the title, the status bar, the
pagination and the help — whatever you have enabled. If you want more
items visible, turn the extras off:

```go
l.SetShowTitle(false)
l.SetShowStatusBar(false)
l.SetShowHelp(false)
l.SetShowPagination(false)
```

With all four off, `availHeight == height` and the list shows
`height / 2` items (because the default delegate has a spacing of 1).

## 6.9 The paginator

The `paginator` is the small "•••" widget. The template wraps it to
drive the PocketBase pagination:

```go
type Pagination struct {
    theme     *theme.Theme
    paginator paginator.Model
    totalItems int
    perPage    int
}

func (p *Pagination) SetTotal(totalItems, page, perPage int) int {
    if perPage > 0 {
        p.perPage = perPage
    }
    p.totalItems = totalItems

    pages := 0
    if p.perPage > 0 {
        pages = (totalItems + p.perPage - 1) / p.perPage
    }

    p.paginator.TotalPages = pages
    p.paginator.Page = page

    return pages
}
```

⚠️ **The bubbles v1.0.0 paginator uses exported fields, not accessor
methods.** It has `paginator.Page`, `paginator.PerPage` and
`paginator.TotalPages` as plain fields. Earlier versions had
`SetPage()`/`Page()` methods. If you see a compile error here, check
your bubbles version.

### Driving it from the page

```go
case "left", "h":
    if p.pagination.Page() > 1 {
        p.pagination.SetPage(p.pagination.Page() - 1)
        p.runtime.PageNumber = p.pagination.Page()
        p.loading = true
        return p, tea.Batch(p.spinner.Tick, p.Load())
    }
    return p, nil
```

## 6.10 The help component

```go
h := help.New()
h.ShortKeyView(m.keys)
```

The `help` component renders a key hint bar from a `key.Binding` map.
The template renders its own hints instead, because it wants the hints
to be context-sensitive and to degrade gracefully when the terminal is
narrow — which the `help` component does not do.

## 6.11 Writing your own component

The pattern is the same as for a page:

```go
// A component is a self-contained tea.Model.
type RecordCard struct {
    theme    *theme.Theme
    renderer RecordRenderer
    record   *pbstore.Record
    width    int
}

func (c *RecordCard) Init() tea.Cmd { return nil }

func (c *RecordCard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        c.width = msg.Width
    }
    return c, nil
}

func (c *RecordCard) View() string { /* ... */ }
```

The rules for a good component:

1. **It owns its state.** No reaching into the parent.
2. **It exposes intent, not implementation.** `SetRecord`, `Focus`,
   `Blur` — not `SetState(map[string]any)`.
3. **It is domain-agnostic.** Inject the domain knowledge.
4. **It handles its own messages.** If it schedules a message, the
   parent must forward it (Chapter 2).

## 6.12 Exercises

1. Build a table with 10,000 rows and measure `View()`. Then compare
   with the same table where `View` is only called for the visible
   rows.

2. Implement a custom delegate that renders a row with a coloured
   status badge. Then implement a `list.DefaultItem` and confirm the
   default delegate renders it.

3. Create a `textinput`, focus it, type "hello", then call
   `SetValue("world")`. Where is the cursor? Then call `CursorEnd()`
   and check again.

4. Write a list that shows "No items." even though `SetItems` was
   called with 5 items. Diagnose the cause. (Hint: check the filter
   state.)

5. Add a `paginator` to a list and observe how the visible item count
   changes with the height. Explain the arithmetic.

6. Write a custom component with a self-scheduling timer (like the
   toast) and wire it into the root model so that the expiry message
   reaches it.

## 6.13 What you learned

- Bubbles components are **composable `tea.Model`s**: construct,
  forward messages, render.
- The `table` knows about **rows, not records** — you keep the
  mapping.
- The `textinput` ignores keys when **not focused**; `SetValue` moves
  the cursor.
- The `viewport` must be **sized** from the terminal dimensions.
- The `spinner` advances only when it receives its **tick message**.
- The `list` delegate renders only items implementing
  **`list.DefaultItem`** — the most common Bubbles bug.
- The `list` binds `/` to its **own filter** and can steal the key.
- The `paginator` exposes **exported fields** in bubbles v1.0.0.
- A good component is **self-contained, intent-exposing and
  domain-agnostic**.

## 6.14 Further reading

- The Bubbles source: `table/table.go`, `list/list.go`,
  `list/defaultitem.go`, `textinput/textinput.go`.
- `internal/components/recordcard.go` — a custom component.
- `internal/components/jsoneditor.go` — a component built on the
  `textarea`.
- `internal/components/pagination.go` — the `paginator` wrapper.

Next: [Chapter 7 — Concurrency: The Purity of Update](ch07-concurrency.md).
