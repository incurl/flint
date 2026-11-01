# Chapter 25 — Creative Ideas and Geek Playgrounds

This chapter is deliberately different. The previous chapters were
about building things correctly. This one is about **what you could
build** — some practical, some speculative, some just for the fun of
it.

Every idea here is buildable with the template as it stands. Some need
one new package; some need a new page; none need to modify the core.

## 25.1 The time machine

**The idea.** A tool that shows you how a dataset *changes over time*.

Every PocketBase record has an `updated` timestamp. If you keep the
history (the `term_history` collection from Chapter 24), you can
render a diff between any two points in time.

```
┌─ 2023-06-01 ──────────┐   ┌─ 2024-01-15 ──────────┐
│ label:      Go         │   │ label:      Golang      │
│ definition: A lang...  │   │ definition: A systems.. │
└────────────────────────┘   └────────────────────────┘
```

**How to build it.** Store the snapshot on every write, with a hook:

```go
app.OnRecordAfterUpdateSuccess().BindFunc(func(e *core.RecordEvent) error {
    if e.Record.Collection().Name != "terms" {
        return e.Next()
    }

    history := core.NewRecord(e.Record.Collection())
    // ... build the history record ...
    return e.Next()
})
```

Then a page that takes two timestamps and renders the diff with
`github.com/sergi/go-diff` — or, more simply, by comparing the two
`map[string]any` payloads field by field.

**Why it is interesting.** It turns a *browsing* tool into a *time
machine* for the data. For a vocabulary that is being curated, it shows
exactly what the curators changed and when.

## 25.2 The diff viewer

**The idea.** Two records side by side, with the differences
highlighted.

This is a small component and a very reusable one:

```go
// DiffField is a single compared field.
type DiffField struct {
    Name     string
    Before   string
    After    string
    Changed  bool
}

// DiffRows compares two field maps.
func DiffRows(before, after map[string]any) []DiffField {
    names := map[string]bool{}
    for k := range before {
        names[k] = true
    }
    for k := range after {
        names[k] = true
    }

    sorted := make([]string, 0, len(names))
    for k := range names {
        sorted = append(sorted, k)
    }
    sort.Strings(sorted)

    rows := make([]DiffField, 0, len(sorted))
    for _, name := range sorted {
        b := fmt.Sprintf("%v", before[name])
        a := fmt.Sprintf("%v", after[name])
        rows = append(rows, DiffField{
            Name:    name,
            Before:  b,
            After:   a,
            Changed: b != a,
        })
    }
    return rows
}
```

⚠️ **The rendering is the fun part.** Use the theme's `Success` for
the additions, `Error` for the removals, and `Muted` for the
unchanged. It reads instantly.

**The extension.** A page that diffs the *remote* copy against the
*local* copy — so you can see what the sync is about to do before you
do it. That is the "preview" mode from Chapter 23.

## 25.3 The graph explorer

**The idea.** A TUI for exploring a knowledge graph.

The knowledge graph shape from Chapter 24 gives you the edges. Now
render them.

```
        ┌─────────────┐
        │  human  Q5  │
        └──────┬──────┘
               │ instance of (P31)
      ┌────────┴────────┐
      ▼                 ▼
┌───────────┐     ┌───────────┐
│ writer    │     │ software  │
│   Q49757  │     │   Q7397   │
└───────────┘     └───────────┘
```

**The traversal.** A breadth-first walk over the statements:

```go
// Neighbours returns the entities related to an item.
func (g *Graph) Neighbours(ctx context.Context, itemID string, depth int) ([]Edge, error) {
    edges := make([]Edge, 0)

    frontier := []string{itemID}
    seen := map[string]bool{itemID: true}

    for d := 0; d < depth && len(frontier) > 0; d++ {
        next := make([]string, 0)

        for _, id := range frontier {
            result, err := g.store.List(ctx, pbstore.Query{
                Collection: "wd_statements",
                Filter:     fmt.Sprintf(`item_id = %q`, id),
                PerPage:    200,
            })
            if err != nil {
                return nil, err
            }

            for _, rec := range result.Records {
                edge := Edge{
                    From:    id,
                    To:      rec.StringField("value_item"),
                    Label:   rec.StringField("property_id"),
                }
                edges = append(edges, edge)

                if edge.To != "" && !seen[edge.To] {
                    seen[edge.To] = true
                    next = append(next, edge.To)
                }
            }
        }

        frontier = next
    }

    return edges, nil
}
```

**The rendering.** This is where the terminal earns its keep. A
depth-limited tree, drawn with box-drawing characters:

```
▾ human (Q5)
  ├─ instance of → writer (Q49757)
  ├─ instance of → software (Q7397)
  └─ occupation → author (Q482980)
```

⚠️ **Use `lipgloss.Width` for the alignment.** The tree characters
are one column each, but a label may contain wide CJK characters.

**The interaction.** `enter` on a node expands it. `/` searches the
graph for an entity. `esc` collapses.

## 25.4 The REPL

**The idea.** Give the user a query language, live.

The native PocketBase filter grammar is already a query language. Put
it in a prompt, evaluate it on every keystroke, show the results.

```go
// The REPL page.
type REPLPage struct {
    store   pbstore.Store
    runtime *state.Runtime
    theme   *theme.Theme

    input   textinput.Model
    history []string
    results []*pbstore.Record
    err     error
}
```

**The evaluation.** The filter is the native grammar, so the store
does the parsing:

```go
result, err := p.store.List(ctx, pbstore.Query{
    Collection: p.runtime.Collection,
    Filter:     p.input.Value(),
    PerPage:    50,
})
```

**Why this is powerful.** The user learns one grammar, and it works
against the REST API too. The REPL is a *teaching tool* for the filter
syntax.

⚠️ **Show the parse errors prominently.** They are the most valuable
output of the REPL:

```
✗ invalid left operand "ran" - unknown field "ran"
```

**The extension.** A REPL that accepts more than filters:

```
> terms where label ~ "go"
> count terms where status = "draft"
> schema terms
> .help
```

That is a small command language on top of the filter grammar. It is
the same idea as `psql` or `sqlite3`, but for a domain.

## 25.5 The database inspector

**The idea.** A `sqlite3`-style tool for PocketBase, in the terminal.

The features of `sqlite3` that make it useful:

```
sqlite> .tables
sqlite> .schema terms
sqlite> .dump
sqlite> .mode json
sqlite> select * from terms;
```

The TUI equivalents:

| The SQLite command | The TUI action                  |
| ------------------ | ------------------------------- |
| `.tables`          | The collection list (the home page) |
| `.schema terms`    | The schema inspector             |
| `.dump`            | The export                       |
| `.mode json`       | A view mode                      |

**The schema inspector** is the interesting one. PocketBase's schema is
data, so you can render it beautifully:

```go
// The schema page.
func (p *SchemaPage) View() tea.View {
    collection, err := p.store.FindCollection(p.runtime.Collection)
    // ...

    var b strings.Builder
    b.WriteString(p.theme.Title.Render(collection.Name))
    b.WriteString("\n\n")

    b.WriteString(p.theme.Subtitle.Render("Fields"))
    b.WriteString("\n")

    for _, field := range collection.Fields {
        name := field.GetName()
        required := ""
        if field.GetRequired() {
            required = p.theme.Error.Render(" required")
        }
        b.WriteString(fmt.Sprintf("  %-24s %s%s\n",
            name,
            p.theme.Muted.Render(field.GetType()),
            required,
        ))
    }

    b.WriteString("\n")
    b.WriteString(p.theme.Subtitle.Render("Indexes"))
    b.WriteString("\n")

    for _, index := range collection.Indexes {
        b.WriteString("  " + p.theme.Accent.Render(index) + "\n")
    }

    return tea.NewView(b.String())
}
```

⚠️ **This needs a `FindCollection` on the `Store` interface** — or an
optional interface, as in Chapter 21:

```go
// SchemaInspector is an optional interface for the stores that can
// expose the collection schema.
type SchemaInspector interface {
    Schema(ctx context.Context, collection string) (*CollectionSchema, error)
}
```

## 25.6 The TUI dashboard

**The idea.** A live dashboard of a PocketBase instance, in the
terminal.

`htop` for a database. The panels:

```
┌─ Collections ─────────┐ ┌─ Records ──────────────┐
│ vocab      base  1.2k │ │ Recent: Go             │
│ items      base  8.4k │ │ Recent: Rust           │
│ users      auth   12  │ │ ...                    │
└───────────────────────┘ └────────────────────────┘
┌─ Sync ─────────────────┐ ┌─ Health ───────────────┐
│ last: 3 minutes ago    │ │ ok (12 collections)    │
│ 1,204 fetched          │ │ latency: 2ms           │
└───────────────────────┘ └────────────────────────┘
```

**The layout.** `lipgloss.JoinHorizontal` with fixed widths:

```go
left := lipgloss.NewStyle().Width(30).Render(leftPanel)
right := lipgloss.NewStyle().Width(50).Render(rightPanel)
row := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
```

**The data.** The counts come from `store.Count`:

```go
counts := make([][2]string, 0, len(collections))
for _, c := range collections {
    n, err := store.Count(ctx, c.Name, "")
    if err != nil {
        continue
    }
    counts = append(counts, [2]string{c.Name, strconv.Itoa(n)})
}
```

⚠️ **The counts are expensive.** One query per collection. Cache them
and refresh on a timer (§23.10).

**The extension.** A `sparkline` of the record count over time, using
the Unicode block characters:

```
▁▂▃▅▆▇█▇▆▅▃▂▁
```

## 25.7 The TUI log viewer

**The idea.** `less`, but for a PocketBase instance.

PocketBase has an auxiliary database with the request logs:

```go
// The logs collection.
logs, err := app.FindCollectionByNameOrId("_logs")
```

**The view.** A scrollable list of the recent requests:

```
15:04:05  GET  /api/collections/vocab/records  200   12ms
15:04:07  POST /api/collections/items/records    201   34ms
15:04:12  GET  /api/health                       200    1ms
```

**The filters.** PocketBase's filter grammar works on the logs
collection too:

```go
result, err := store.List(ctx, pbstore.Query{
    Collection: "_logs",
    Filter:     `url ~ "/records" && created > "2024-01-01 00:00:00.000Z"`,
})
```

⚠️ **The logs are in the auxiliary database.** The `Store` interface
does not expose it, so you would need an optional interface or the
embedded app directly.

⚠️ **The logs grow.** PocketBase has a cron job that cleans them up
(`__pbLogsCleanup__`, every 6 hours). Do not re-implement it.

## 25.8 The template engine

**The idea.** Let the user define *how* a record is rendered.

The renderer interface (Chapter 21) is a Go interface. But a user
might want to change the layout without recompiling. A tiny template
language does it:

```
{{ .label }} ({{ .code }})
```

```go
// A tiny template renderer.
type TemplateRenderer struct {
    template string
}

// Title renders the template with the record's fields.
func (r *TemplateRenderer) Title(rec *pbstore.Record) string {
    return r.render(r.template, rec)
}

// render substitutes the {{ .field }} placeholders.
func (r *TemplateRenderer) render(tmpl string, rec *pbstore.Record) string {
    out := tmpl
    for name, value := range rec.Fields {
        out = strings.ReplaceAll(out, "{{ ."+name+" }}", fmt.Sprintf("%v", value))
    }
    return out
}
```

⚠️ **This is a toy.** It does not handle escaping, nesting, or errors.

⚠️ **If you do this properly, use `text/template`.** But then the
template can call methods, which is a security consideration if the
templates come from an untrusted source.

**The real use.** It lets a *user* customise the display without a
developer. For a team tool, that is sometimes exactly what is needed.

## 25.9 The keyboard-driven interface

**The idea.** Hide the mouse entirely; make the keyboard the whole
interface.

This is the philosophy of `lazygit`, `k9s`, `delta` and `fzf`. The
template already leans this way.

**The principles.**

1. **Every action has a key.** No "click here".
2. **The keys are shown.** In the status bar, in a help overlay.
3. **The keys are consistent.** `j`/`k` everywhere, `/` to search,
   `enter` to open, `esc` to go back.
4. **A key prefix for the rare actions.** `g` for "go to", `:` for a
   command palette.

**A prefix-key implementation.**

```go
// The pending prefix.
type prefixState int

const (
    prefixNone prefixState = iota
    prefixG          // "g" pressed, waiting for the second key
)

// The key handling.
case tea.KeyPressMsg:
    switch m.prefix {
    case prefixG:
        m.prefix = prefixNone
        switch msg.String() {
        case "h":
            return m, m.navigate(state.PageHome)
        case "t":
            return m, m.navigate(state.PageRecords)
        case "s":
            return m, m.navigate(state.PageSettings)
        }
        return m, nil

    case prefixNone:
        switch msg.String() {
        case "g":
            m.prefix = prefixG
            return m, nil
        }
    }
```

⚠️ **The prefix must time out**, or a stray `g` swallows the next
key.

```go
case prefixG:
    // ...
case prefixExpiredMsg:
    m.prefix = prefixNone
```

**The payoff.** A tool that a power user never needs the mouse for is
a tool they use ten times faster.

## 25.10 The multi-pane layout

**The idea.** The screen is split; each pane has its own focus; the
keys are routed to the focused pane.

```
┌─ Records ──────────┬─ Detail ───────────────────────┐
│ Go                 │ Go                              │
│ Rust               │ A statically typed language     │
│ Zig                │                                 │
│                    │ ▾ root (4)                      │
│                    │   key: "go"                     │
└────────────────────┴─────────────────────────────────┘
```

**The focus model.** The root model keeps a focus index:

```go
type Model struct {
    // ...
    panes  []Pane
    focused int
}

// Pane is a focusable region.
type Pane interface {
    tea.Model
    Focused() bool
    Focus()
    Blur()
}
```

**The routing.** The focused pane gets the message first; if it does
not consume it, the next pane tries.

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    if len(m.panes) > 0 {
        pane := m.panes[m.focused]
        updated, cmd := pane.Update(msg)
        m.panes[m.focused] = updated.(Pane)

        if consumed(msg) {
            return m, cmd
        }
    }
    // ... the global layer ...
}
```

**The trap.** "Consumed" is hard to define. A key that a pane
*ignores* is not the same as a key it *handled*.

⚠️ **A pragmatic approach:** give each pane an explicit set of keys it
owns, and route by that set rather than by trying to infer the
consumption.

```go
// The keys a pane owns.
func (p *RecordsPane) Keys() []string {
    return []string{"up", "down", "j", "k", "enter", "/"}
}
```

**The alternative that avoids the problem entirely.** Two panes, two
*explicit* modes, toggled by a key. `tab` switches the focus. The
detail pane is read-only, so it only handles `j`/`k` for scrolling.

## 25.11 The themable status bar

**The idea.** The status bar is the most valuable real estate in the
application. Make it configurable.

```toml
[statusbar]
mode = true
health = true
collection = true
sync = true
page = true
error = true
hints = true
```

```go
type StatusBarConfig struct {
    Mode       bool `toml:"mode"`
    Health     bool `toml:"health"`
    Collection bool `toml:"collection"`
    Sync       bool `toml:"sync"`
    Page       bool `toml:"page"`
    Error      bool `toml:"error"`
    Hints      bool `toml:"hints"`
}
```

Then the status bar renders only what is enabled:

```go
func (s *StatusBar) leftSection() string {
    parts := make([]string, 0, 4)

    if s.info.ShowMode {
        parts = append(parts, s.theme.StatusMode.Render(strings.ToUpper(s.info.Mode)))
    }
    if s.info.ShowHealth {
        parts = append(parts, s.theme.StatusColor(s.info.Connection).Render("● "+s.info.Connection))
    }
    // ...

    return strings.Join(parts, s.theme.Muted.Render(" │ "))
}
```

⚠️ **The zero value must be the "everything on" default.** A
`bool` field defaults to `false`, so an unset config would hide
everything. Use a pointer, or invert the fields:

```go
type StatusBarConfig struct {
    HideMode       bool `toml:"hide_mode"`
    HideHealth     bool `toml:"hide_health"`
    // ...
}
```

## 25.12 The self-documenting UI

**The idea.** The application explains itself.

A `--help` that describes the *data model*, not just the flags:

```
$ flint --describe

Collection: terms (base)

  Fields:
    code        text      required
    label       text
    definition  editor
    category    select    values: language, unit, concept
    status      select    values: draft, approved, deprecated

  Indexes:
    CREATE UNIQUE INDEX idx_terms_code ON terms (code)

  Example filter:
    terms where status = "approved" && category = "language"
```

This is a **great onboarding tool** and it is about twenty lines:

```go
func (s *Embedded) Describe(ctx context.Context, collection string) (string, error) {
    model, err := s.app.FindCachedCollectionByNameOrId(collection)
    if err != nil {
        return "", err
    }

    var b strings.Builder
    fmt.Fprintf(&b, "Collection: %s (%s)\n\n", model.Name, model.Type)

    b.WriteString("  Fields:\n")
    for _, field := range model.Fields {
        required := ""
        if field.GetRequired() {
            required = " required"
        }
        fmt.Fprintf(&b, "    %-16s %s%s\n", field.GetName(), field.Type(), required)
    }

    if len(model.Indexes) > 0 {
        b.WriteString("\n  Indexes:\n")
        for _, index := range model.Indexes {
            fmt.Fprintf(&b, "    %s\n", index)
        }
    }

    return b.String(), nil
}
```

## 25.13 The live preview

**The idea.** As you type a filter, show the matching records
*without* waiting for a round trip — by evaluating the filter against
the **already-loaded** page.

This is possible when the data is local:

```go
// A local evaluation of the filter against the loaded records.
func localMatch(rec *pbstore.Record, filter string) bool {
    if filter == "" {
        return true
    }
    // A simplified matcher for the common cases.
    // ...
}
```

⚠️ **This is a lie.** The local evaluation is an *approximation* of
the database's semantics. It can disagree with the server.

⚠️ **Use it only as a preview, clearly labelled.** Show the local
count as "≈ 3 matches" and the server count as the truth once the
query completes.

The value: the UI responds instantly, and the user gets a feel for
the filter before committing.

## 25.14 The terminal as a spreadsheet

**The idea.** A grid that behaves like a spreadsheet: arrow keys move
between cells, `enter` edits, and the changes are written back.

**The model.**

```go
type Cell struct {
    Record *pbstore.Record
    Field  string
    Value  string
}

type Grid struct {
    rows    [][]Cell
    cursorX int
    cursorY int
    editing bool
    input   textinput.Model
}
```

**The editing.** `enter` puts the cell in edit mode; `enter` again
writes it back:

```go
func (g *Grid) commitEdit() tea.Cmd {
    cell := g.currentCell()
    value := g.input.Value()

    store, collection, id, field := g.store, g.collection, cell.Record.ID, cell.Field

    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()

        if _, err := store.Update(ctx, collection, id, map[string]any{field: value}); err != nil {
            return cellErrorMsg{err}
        }
        return cellSavedMsg{}
    }
}
```

⚠️ **The write is per cell.** A spreadsheet implies a transaction over
the whole edit session. With the per-cell writes, a half-finished edit
leaves the data half-updated.

⚠️ **For a real spreadsheet, you need a batch update.** Add it as an
optional interface (Chapter 23, §23.13):

```go
type BulkStore interface {
    UpdateMany(ctx context.Context, collection string, updates map[string]map[string]any) error
}
```

⚠️ **The UI is the easy part; the transaction is the hard part.**

## 25.15 The offline-first PWA in a terminal

**The idea.** The ultimate version of the template: a terminal
application that is *also* a web application, with the same codebase.

PocketBase already ships a web dashboard (`ui/`). If you enable it in
the embedded mode:

```go
pb := pocketbase.NewWithConfig(pocketbase.Config{
    DefaultDataDir: dir,
})
```

...and run the API server, you get the full PocketBase dashboard on
`http://127.0.0.1:8090/_/` — backed by the same SQLite database as
your TUI.

```go
// Serve the PocketBase API alongside the TUI.
go func() {
    router, err := apis.NewRouter(pb)
    if err != nil {
        return
    }
    mux, err := router.BuildMux()
    if err != nil {
        return
    }
    _ = http.ListenAndServe("127.0.0.1:8090", mux)
}()
```

⚠️ **The TUI and the server share the database.** SQLite allows one
writer at a time, so the TUI's writes and the server's writes contend.
It works (SQLite retries), but it is not ideal.

⚠️ **The dashboard is for superusers.** It exposes the whole
database. Do not bind it to a public interface.

**The result.** A TUI for the keyboard-first workflow, and a web
dashboard for the occasional administrator. The same data, the same
schema, the same migrations.

## 25.16 The plugin system

**The idea.** Let the user extend the application with their own code,
at runtime, without recompiling.

Go's `plugin` package supports this (on Linux and macOS):

```go
// The plugin interface.
type Plugin interface {
    Name() string
    Commands() []Command
}

// Load the plugins from a directory.
func LoadPlugins(dir string) ([]Plugin, error) {
    entries, err := os.ReadDir(dir)
    if err != nil {
        return nil, err
    }

    var plugins []Plugin
    for _, entry := range entries {
        if entry.IsDir() || filepath.Ext(entry.Name()) != ".so" {
            continue
        }

        p, err := plugin.Open(filepath.Join(dir, entry.Name()))
        if err != nil {
            continue
        }

        symbol, err := p.Lookup("Plugin")
        if err != nil {
            continue
        }

        instance, ok := symbol.(Plugin)
        if !ok {
            continue
        }
        plugins = append(plugins, instance)
    }

    return plugins, nil
}
```

⚠️ **`plugin.Open` only works on Linux, macOS and FreeBSD.** Not
Windows. And the plugin must be built with the *exact same* version of
the shared packages — a version mismatch is a runtime crash, not a
compile error.

⚠️ **A plugin is arbitrary code.** Loading one is equivalent to
running a binary. Only load plugins from a directory you trust.

**The pragmatic alternative.** Use the **command palette** (§23.4) with
the commands registered by the *application*, and let the user add
their own commands through a **configuration file**:

```toml
[[commands]]
name = "Open in browser"
description = "Open the current record in the default browser"
url = "https://example.org/records/{{ .id }}"
```

That gives 80% of the value with 0% of the risk.

## 25.17 The build-your-own-UI generator

**The idea.** Generate a TUI from a schema.

Given a collection schema, generate a page that lists the records and
shows their fields. It is the "batteries included" version of the
renderer.

```go
// The generated page.
func GeneratePage(store pbstore.Store, runtime *state.Runtime, t *theme.Theme, collection string) *GeneratedPage {
    // Build the columns from the schema.
    columns := []table.Column{{Title: "ID", Width: 20}}
    for _, field := range fieldsOf(collection) {
        columns = append(columns, table.Column{
            Title: field.GetName(),
            Width: 20,
        })
    }
    // ...
}
```

**The value.** A "quick look" mode that works on *any* collection,
including the ones you did not plan for. The template's home page
already lists the collections; this would let the user browse any of
them immediately.

⚠️ **The columns must be computed at runtime**, because the schema is
data. That means the page cannot use a static `table.Columns` list —
it must rebuild it when the collection changes.

## 25.18 The TUI that explains itself

**The idea.** A `--debug` mode that shows the message flow.

Every Bubble Tea application has a message stream. A debug overlay can
show it:

```
Messages (last 10):
  15:04:05.123  tea.KeyPressMsg     "j"
  15:04:05.124  pages.RecordsMsg    {Page: 1}
  15:04:05.130  spinner.TickMsg     {ID: 3, tag: 1}
  ...
```

**The implementation.** A ring buffer in the root model:

```go
type debugLog struct {
    entries []entry
    limit   int
}

func (l *debugLog) add(msg tea.Msg) {
    l.entries = append(l.entries, entry{
        at:   time.Now(),
        kind: fmt.Sprintf("%T", msg),
        text: fmt.Sprintf("%+v", msg),
    })
    if len(l.entries) > l.limit {
        l.entries = l.entries[1:]
    }
}
```

And the hook:

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    if m.debug {
        m.debugLog.add(msg)
    }
    // ...
}
```

⚠️ **The debug log must be cheap.** `fmt.Sprintf("%+v", msg)` on every
message is not free. Only do it when the debug mode is on.

**The value.** It is the single most useful tool when the application
behaves unexpectedly. "What messages actually arrived?" is the
question you always end up asking.

## 25.19 The idea list

Here is a grab-bag of smaller ideas, for inspiration.

| Idea                | The hook                                            |
| ------------------- | --------------------------------------------------- |
| **Fuzzy jump**      | `g` then a fuzzy search over the records            |
| **Bulk edit**       | A multi-select mode with a batch update              |
| **Undo history**    | The undo stack (§23.6)                              |
| **Export/import**   | CSV and JSON round trips (§23.8, §23.9)              |
| **Watch mode**      | Re-run the query when the data changes               |
| **Query history**   | The `↑` in the filter recalls the previous query     |
| **Snippets**        | Saved filters as named, reusable snippets            |
| **Compare**         | Diff two records side by side                        |
| **Merge**           | Merge two vocabulary entries                         |
| **Validate**        | A page that reports every schema violation           |
| **Profile**         | The query timings, per collection                    |
| **Archive**         | Move the old records to an `archive` collection      |
| **Tag cloud**       | The most-used tags, rendered as a cloud              |
| **Heatmap**         | The record activity over the last year                |
| **Fuzzy finder**    | A `fzf`-like search over the whole database          |
| **TUI + web**       | The embedded mode with the dashboard enabled (§25.15) |
| **Plugin**          | The runtime-loaded extensions (§25.16)                |
| **Schema-driven**   | The generated pages (§25.17)                         |
| **Debug overlay**   | The message stream (§25.18)                          |

## 25.20 How to pick one

⚠️ **Do not start with the geek ideas.** They are the *last* thing you
build, not the first.

The order:

```
1. The migration       (the data)
2. The renderer        (the display)
3. The core pages      (the workflow)
4. The quality         (the tests, the errors, the performance)
5. THEN the geek ideas (the polish)
```

An application with a beautiful command palette and a broken filter is
worse than an application with a plain interface that works.

## 25.21 Exercises

1. Build the **diff viewer** (§25.2). It is about fifty lines and it
   is immediately useful.
2. Build the **REPL** (§25.4). It teaches the filter grammar.
3. Build the **schema inspector** (§25.5).
4. Build the **TUI dashboard** (§25.6).
5. Build the **graph explorer** (§25.3) on top of the Wikidata example.
6. Build the **debug overlay** (§25.18). You will use it the next time
   something misbehaves.
7. Build the **self-documenting `--describe`** (§25.12).
8. Pick three ideas from §25.19 and implement them.

## 25.22 What you learned

- The **time machine**: the history collection turns a browser into a
  time machine.
- The **diff viewer**: a small, reusable component with a big payoff.
- The **graph explorer**: the normalised statements make the traversal
  possible.
- The **REPL**: the native filter grammar *is* a query language.
- The **database inspector**: PocketBase's schema is data, so it can be
  rendered beautifully.
- The **dashboard**, the **log viewer** and the **describe** command:
  the schema is the documentation.
- The **keyboard-driven interface** and the **multi-pane layout**: the
  consistency and the focus model.
- The **themable status bar** and the **live preview**.
- The **terminal as a spreadsheet**: the UI is easy, the transaction is
  hard.
- The **offline-first PWA in a terminal**: the embedded mode with the
  dashboard enabled.
- The **plugin system**: possible, but the configuration-file approach
  is safer and nearly as good.
- **Build the core first, the geek ideas last.**

## 25.23 Further reading

- `charmbracelet/gum` — the "tunable" helpers for lists and tables.
- `charm.land/lipgloss/v2` — the layout and the borders.
- `charm.land/bubbles/v2` — the components.
- `pocketbase/pocketbase/examples/base` — a full application.
- The `fzf` source — the fuzzy matching algorithm, if you want to
  reimplement it.
- `sergi/go-diff` — the diffing, if you want the real thing.

---

## A closing thought

You have now read about twenty-five chapters covering two libraries and
the space between them. The most important ideas, in order:

1. **The screen is a string.** Everything follows from that.
2. **One state, one update, one view.** Every problem becomes
   tractable.
3. **Effects are values.** I/O belongs in commands, and commands capture
   values, never the model.
4. **Reuse the native primitives.** The filter grammar, the record
   model, the validation — PocketBase's, not yours.
5. **The database is the source of truth.** The runtime store is a
   cache.
6. **The layering is the architecture.** `app → pages → components →
   pbstore`, and the arrows point down.
7. **Explicit beats clever.** The explicit sync, the explicit allowlist,
   the explicit error messages.

The template gives you the plumbing. What you build with it is yours.
