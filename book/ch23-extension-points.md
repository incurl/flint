# Chapter 23 — Extension Points: A Catalogue

## 23.1 What an extension point is

An extension point is a **seam** in the architecture where a downstream
project can substitute its own behaviour without modifying the core.

This is a precise definition, and the precision matters. A "hook" that
requires you to edit `internal/app/app.go` is not an extension point;
it is a patch that happens to be called a hook.

> **An extension point must be reachable without editing the core
> files.**

The Flint template has seven. This chapter catalogues them, shows what
each one costs, and explains when to use which.

## 23.2 The extension points

| # | Extension point          | Lives in                    | Reached by                    |
| - | ------------------------ | --------------------------- | ----------------------------- |
| 1 | The **record renderer**  | `internal/components`       | `app.New(store, cfg, renderer)` |
| 2 | The **pages**            | `internal/pages`            | Registering in `internal/app`  |
| 3 | The **migrations**       | `cmd/tui`                   | `appMigrations()`             |
| 4 | The **sync strategy**    | `internal/pbstore`          | Implementing `Store`           |
| 5 | The **theme**            | `internal/theme`            | `theme.New(variant, scale)`   |

There is a sixth, unofficial one that turns out to matter a lot:

| # | Extension point          | Lives in                    | Reached by                    |
| - | ------------------------ | --------------------------- | ----------------------------- |
| 6 | The **store**           | `internal/pbstore`          | `openStore(cfg)`              |

And a seventh, added by the agentic layer:

| # | Extension point          | Lives in                    | Reached by                    |
| - | ------------------------ | --------------------------- | ----------------------------- |
| 7 | The **agent tools**     | `internal/agent`            | `registry.Register(tool)`     |

The first one has a runtime variant worth calling out: the record
renderer can also be **overridden by a script** in `flint_hooks/`,
without a Go rebuild. See §23.3.

Let us examine each.

## 23.3 The record renderer

### The interface

```go
// RecordRenderer renders a record into a human readable summary.
//
// Downstream projects inject their own renderer to display dictionary
// entries, Wikidata items, reference metadata, ... without touching the
// core packages.
type RecordRenderer interface {
    // Title returns the short title of the record.
    Title(rec *pbstore.Record) string
    // Summary returns the one line description of the record.
    Summary(rec *pbstore.Record) string
    // Fields returns the ordered list of the displayed fields.
    //
    // Each entry is a "name: value" pair.
    Fields(rec *pbstore.Record) []FieldRow
}
```

### Where it is used

The renderer is injected into the root model and passed down to every
component that displays records:

```go
func New(store pbstore.Store, cfg *config.Config, renderer components.RecordRenderer) *Model {
    if renderer == nil {
        renderer = &components.DefaultRecordRenderer{}
    }
    // ...
    records: pages.NewRecordsPage(store, runtime, t, renderer),
    detail:  pages.NewDetailPage(store, runtime, t, renderer),
    search:  pages.NewSearchPage(store, runtime, t, renderer),
}
```

Three places consume it:

| Consumer           | Uses                     | For                             |
| ------------------ | ------------------------ | ------------------------------- |
| The record table   | `Title` and `Summary`    | The list rows                   |
| The record card    | `Title`, `Summary`, `Fields` | The detail view            |
| The search results | `Title` and `Summary`    | The result rows                 |

### The cost

**Low.** One interface, three methods, no generics, no reflection. A
renderer is a plain struct.

### The example

```go
// examples/wikidata/renderer.go
type Renderer struct {
    language string
}

var _ components.RecordRenderer = (*Renderer)(nil)

func NewRenderer(language string) *Renderer {
    if language == "" {
        language = "en"
    }
    return &Renderer{language: language}
}

func (r *Renderer) Title(rec *pbstore.Record) string {
    if rec == nil {
        return ""
    }
    if label := r.label(rec); label != "" {
        return label
    }
    return rec.StringField("qid")
}
```

### When to use it

**Always**, if your domain has a notion of "how a record should be
summarised". It is the cheapest possible customisation and it applies
everywhere the records appear.

⚠️ **The renderer must be cheap.** `View` calls it on every render. A
renderer that does a database lookup will make the UI crawl.

⚠️ **The renderer must not mutate the record.** It receives a pointer;
treat it as read-only.

### Extending the renderer

If you need more than a title, a summary and a list of fields, you
have two options.

**Option 1: a richer interface.** Change `RecordRenderer` to return a
structured result:

```go
type RecordRenderer interface {
    Render(rec *pbstore.Record) Rendered
}

type Rendered struct {
    Title    string
    Summary  string
    Fields   []FieldRow
    Sections []Section   // richer, grouped content
}
```

⚠️ **This is a breaking change to the core.** The template's
`RecordRenderer` is documented as a stable extension point. If you
change it, downstream projects break.

**Option 2: a separate interface, detected by assertion.**

```go
// SectionedRenderer is an optional interface that a renderer can
// implement to provide richer, grouped content.
type SectionedRenderer interface {
    RecordRenderer
    Sections(rec *pbstore.Record) []Section
}

type Section struct {
    Title string
    Rows  []FieldRow
}
```

And in the record card:

```go
if sr, ok := c.renderer.(components.SectionedRenderer); ok {
    // Render the sections.
}
```

⚠️ **Option 2 is the right way to extend a published interface.** It is
additive: the existing renderers keep working, and the new capability
is opt-in.

### Overriding it from JavaScript

There is a third way, and it does not need a Go rebuild at all: a
script in `flint_hooks/` can **override** the renderer.

```js
// <data-dir>/../flint_hooks/1_render.pb.js
function flintRender(record) {
    return {
        title:   record.label || record.id,
        summary: record.description || "",
        fields:  [{ name: "Key", value: String(record.key) }],
    }
}
```

Enabled with `--js-renderer`. It **overrides** rather than replaces:
when the script returns a value, that value is used, and the Go
renderer is the fallback. A missing, broken or slow script degrades to
the default rendering instead of breaking the screen.

Three things make it safe.

**1. It is a separate directory from `pb_hooks/`.** The PocketBase hooks
extend the data layer and can run unchanged on a server. The renderer
is local to the application: the server has no TUI to render. Keeping
the directories apart keeps that portability honest.

**2. The script cannot reach the database.** It runs in its own goja
runtime, not the PocketBase one, so there is no `$app`, no `$dbx`, no
`$os`. A renderer is display logic; it must not be able to write.

**3. The script never runs while a frame is drawn.** This is the part
that matters for the `RecordRenderer` contract above.

The render methods are called from `View`. Evaluating a script there
would block the render loop for as long as the script takes — on every
keystroke. So the render methods are pure cache lookups, and the script
runs only in a preload that the pages call from the commands that fetch
the records:

```go
// internal/pages/records.go, inside the load command
components.Preload(p.renderer, result.Records)
```

```go
// The hook the pages call.
type Preloader interface {
    RecordRenderer
    Preload(records []*pbstore.Record) int
}
```

The consequence is visible: the **first frame of a new record shows the
Go rendering**, and the next frame shows the script rendering. That is
the price of keeping `View` pure, and it is the same reason the card
renderer must be cheap.

A single evaluation is bounded to two seconds and is interrupted with
`Runtime.Interrupt`, so a script that loops forever cannot leave the
runtime holding the lock.

| Property | Go renderer | JavaScript override |
| -------- | ----------- | ------------------- |
| Changes need a rebuild | yes | no |
| Type checked | yes | no |
| Runs in `View` | yes | **no** (preloaded) |
| Can reach the database | yes | no |
| Runs on a server | no | no |

The last row is the one to keep in mind: **neither** renderer is
portable to a server, because rendering is a client concern. Only the
data hooks in `pb_hooks/` are.

## 23.4 The pages

### What a page is

A page is a self-contained `tea.Model` with its own view state.

```go
type StatsPage struct {
    store   pbstore.Store
    runtime *state.Runtime
    theme   *theme.Theme

    stats   Stats
    loading bool
    err     error
    width   int
    height  int
}
```

### What it costs to add one

Four edits in `internal/app/app.go`:

```go
type Model struct {
    // ...
    stats *pages.StatsPage
}

func New(...) *Model {
    // ...
    stats: pages.NewStatsPage(store, runtime, t),
}

func (m *Model) updateActivePage(msg tea.Msg) []tea.Cmd {
    switch m.runtime.Page {
    // ...
    case state.PageStats:
        updated, cmd := m.stats.Update(msg)
        m.stats = updated.(*pages.StatsPage)
        cmds = append(cmds, cmd)
    }
}

func (m *Model) View() tea.View {
    switch m.runtime.Page {
    // ...
    case state.PageStats:
        body = m.stats.View()
    }
}
```

Plus the hotkey:

```go
case "7":
    return m.navigate(state.PageStats), true
```

Plus the `Page` constant:

```go
// internal/state/state.go
const (
    PageHome Page = "home"
    PageRecords Page = "records"
    // ...
    PageStats Page = "stats"
)
```

### The cost

**Medium.** Five touch points, one of them in a core file.

⚠️ **This is the weakest extension point in the template.** Adding a
page means editing `internal/app/app.go` and `internal/state/state.go`.

### How to make it weaker-free

The five touch points can be reduced to one with a **page registry**:

```go
// internal/app/registry.go

// PageFactory creates a page.
type PageFactory func(deps Deps) Page

// Page is the contract every page must satisfy.
type Page interface {
    tea.Model
    // Key returns the hotkey that activates the page.
    Key() string
    // Title returns the human readable name.
    Title() string
}

// Deps are the dependencies every page receives.
type Deps struct {
    Store   pbstore.Store
    Runtime *state.Runtime
    Theme   *theme.Theme
    Renderer components.RecordRenderer
}

// Registry holds the pages of the application.
type Registry struct {
    pages map[state.Page]PageFactory
    order []state.Page
}

func NewRegistry(deps Deps) *Registry {
    r := &Registry{pages: map[state.Page]PageFactory{}}

    r.Register(state.PageHome, func(d Deps) Page {
        return pages.NewHomePage(d.Store, d.Runtime, d.Theme)
    })
    r.Register(state.PageRecords, func(d Deps) Page {
        return pages.NewRecordsPage(d.Store, d.Runtime, d.Theme, d.Renderer)
    })
    // ...

    return r
}

// Register adds a page to the registry.
func (r *Registry) Register(page state.Page, factory PageFactory) {
    if _, ok := r.pages[page]; !ok {
        r.order = append(r.order, page)
    }
    r.pages[page] = factory
}
```

Now adding a page is:

```go
registry.Register(state.PageStats, func(d Deps) Page {
    return pages.NewStatsPage(d.Store, d.Runtime, d.Theme)
})
```

— **one line, in a file you own.**

⚠️ **The `state.Page` constant still has to be added.** Unless you make
the page identifier a `string`:

```go
type Registry struct {
    pages map[string]PageFactory
    order []string
}
```

Then a downstream project can register `"stats"` without touching
`internal/state` at all.

⚠️ **The trade-off.** The registry is more flexible but less
discoverable: a reader of `internal/app` no longer sees the list of
pages. For a template whose *purpose* is to be extended, that trade is
worth making. But it is a change to the core, so it is your call.

## 23.5 The migrations

### The interface

```go
func appMigrations() []core.Migration {
    return myapp.Migrations()
}
```

That is the whole extension point. One function, in `cmd/tui/main.go`
— a file the downstream project owns.

### What a downstream project can do

**Create the domain collections.**

```go
func Migrations() []core.Migration {
    return []core.Migration{
        {
            File: "1700000001_create_terms.go",
            Up: func(txApp core.App) error {
                collection := core.NewBaseCollection("terms")
                collection.Fields.Add(
                    &core.TextField{Name: "key", Required: true},
                    &core.TextField{Name: "label"},
                    &core.JSONField{Name: "payload"},
                )
                return txApp.Save(collection)
            },
            Down: func(txApp core.App) error { return nil },
        },
    }
}
```

**Add a field to an existing collection.**

```go
{
    File: "1700000002_add_terms_notes.go",
    Up: func(txApp core.App) error {
        collection, err := txApp.FindCollectionByNameOrId("terms")
        if err != nil {
            return err
        }
        collection.Fields.Add(&core.TextField{Name: "notes", Max: 2000})
        return txApp.Save(collection)
    },
    Down: func(txApp core.App) error { return nil },
}
```

**Seed reference data.**

```go
{
    File: "1700000003_seed_units.go",
    Up: func(txApp core.App) error {
        collection, err := txApp.FindCollectionByNameOrId("terms")
        if err != nil {
            return err
        }
        for _, unit := range []struct{ Key, Label string }{
            {"m", "metre"}, {"km", "kilometre"},
        } {
            rec := core.NewRecord(collection)
            rec.Set("key", unit.Key)
            rec.Set("label", unit.Label)
            if err := txApp.Save(rec); err != nil {
                return err
            }
        }
        return nil
    },
    Down: func(txApp core.App) error { return nil },
}
```

### The cost

**Zero.** `cmd/tui/main.go` is the downstream project's file.

⚠️ **The migration filenames must sort after the system ones.** Use a
timestamp prefix (Chapter 13).

## 23.6 The sync strategy

### The interface

```go
// SyncCollection copies records from a remote PocketBase server into the
// local embedded store.
//
// It is an explicit, user-triggered operation: nothing syncs in the
// background. Records are matched by id so that a repeated sync updates
// the existing local copy instead of duplicating it.
SyncCollection(ctx context.Context, req SyncRequest) (*SyncReport, error)
```

### The three ways to customise it

**Option 1: a different filter.** The `SyncRequest` already carries
one:

```go
req := pbstore.SyncRequest{
    RemoteCollection: "labels",
    Filter:           `namespace = "en"`,
    BatchSize:        500,
    MaxRecords:       100000,
}
```

**Option 2: a different merge policy.** Implement your own `Store`
that wraps the embedded one:

```go
// ConflictStore adds a conflict resolution policy to the embedded store.
type ConflictStore struct {
    *pbstore.Embedded
    // PreferLocal keeps the local copy when it is newer.
    PreferLocal bool
}
```

⚠️ **Embedding `*pbstore.Embedded` and overriding `SyncCollection`**
works because Go's method sets allow it. But the override must call the
embedded implementation for the parts it does not replace — and the
embedded implementation's `upsertSynced` is unexported, so you cannot
override just the merge.

**Option 3: a different sync entirely.** Implement `Store` from
scratch:

```go
// GraphStore syncs a knowledge graph.
type GraphStore struct {
    // ...
}
```

### The cost

**Medium to high.** The `Store` interface has twelve methods.

⚠️ **This is the argument for keeping the interface minimal.** The
smaller it is, the easier it is to implement a custom store. The
template's interface is deliberately small for exactly this reason.

### The pragmatic recommendation

For 95% of the extensions, you do **not** need a custom store. You
need:

1. a **migration** that creates the collections;
2. a **renderer** that displays them;
3. the **built-in sync**, with the filter and the batch size tuned.

Reach for a custom `Store` only when you need a fundamentally
different data flow — a different backend, a merge policy the built-in
one cannot express, or a sync that is not record-oriented.

## 23.7 The theme

### The interface

```go
func New(variant Variant, scale Scale) *Theme
func (t *Theme) Apply(variant Variant, scale Scale)
```

### The theme is mutated in place

Every page holds the **same** `*theme.Theme` pointer and reads the styles
when it renders. That is what makes a runtime change possible:

```go
m.theme.Apply(theme.VariantLight, theme.Scale(120))
```

Every page picks the change up on the next frame, without being
rebuilt. **Never replace the pointer** — the pages would keep the old
theme and nothing would change on screen.

### The scale

The `Scale` is a percentage between 50 and 200. It multiplies the sizes
the theme controls: the padding of the panels and the status bar. The
colours are not affected.

```go
const (
    ScaleMin     Scale = 50
    ScaleMax     Scale = 200
    ScaleDefault Scale = 100
    ScaleStep    Scale = 10
)
```

The `+` and `-` hotkeys change it by one step, and the value is
persisted with the other user preferences.

### Adding a variant

```go
// internal/theme/theme.go
const (
    VariantDark Variant = "dark"
    VariantLight Variant = "light"
    VariantSolarized Variant = "solarized"   // ← new
)

func SolarizedPalette() Palette {
    return Palette{
        Background:    "#002b36",
        Foreground:    "#93a1a1",
        Muted:         "#586e75",
        Primary:       "#268bd2",
        Secondary:     "#d33682",
        Accent:        "#2aa198",
        Success:       "#859900",
        Warning:       "#b58900",
        Error:         "#dc322f",
        Border:        "#073642",
        BorderFocus:   "#268bd2",
        Selection:     "#073642",
        SelectionText: "#fdf6e3",
    }
}
```

The styles are built in `Apply`, so a new variant only needs a palette
and a case in the switch:

```go
func (t *Theme) Apply(variant Variant, scale Scale) {
    if !variant.Valid() {
        variant = VariantDark
    }

    palette := DarkPalette()
    switch variant {
    case VariantLight:
        palette = LightPalette()
    case VariantSolarized:
        palette = SolarizedPalette()
    }

    t.Variant = variant
    t.Palette = palette
    // ... the styles
}
```

```go
func (v Variant) Valid() bool {
    switch v {
    case VariantDark, VariantLight, VariantSolarized:
        return true
    default:
        return false
    }
}
```

### The cost

**Low.** Three small edits in `internal/theme/theme.go`.

⚠️ **The `Palette` struct is the contract.** Adding a colour to it is
additive and safe. Changing an existing one changes the whole
application's look — which is the point, but be deliberate.

⚠️ **Add the variant to the configuration validation**, or the user
will get a silent fallback to dark:

```go
if !theme.Variant(c.Theme).Valid() {
    return fmt.Errorf("config: invalid theme %q", c.Theme)
}
```

### Beyond the palette

The theme can also carry **semantic styles** that the components use:

```go
type Theme struct {
    // ...
    // DiffAdded is the style of an added line in a diff view.
    DiffAdded lipgloss.Style
    // DiffRemoved is the style of a removed line.
    DiffRemoved lipgloss.Style
    // DiffChanged is the style of a changed line.
    DiffChanged lipgloss.Style
}
```

This is the mechanism for adding a *new kind of content* to the
application without touching the components that render it.

## 23.8 The store

### The interface

`openStore` is the seam:

```go
func openStore(cfg *config.Config) (pbstore.Store, error) {
    switch pbstore.Mode(cfg.Mode) {
    case pbstore.ModeRemote:
        return pbstore.OpenRemote(pbstore.RemoteConfig{ /* ... */ })

    case pbstore.ModeEmbedded:
        pb := pocketbase.NewWithConfig(pocketbase.Config{ /* ... */ })
        return pbstore.OpenEmbedded(pb, pbstore.EmbeddedConfig{ /* ... */ })

    default:
        return nil, fmt.Errorf("unsupported mode %q", cfg.Mode)
    }
}
```

### What a downstream project can do

**Add a mode.** A third mode — a read-only "demo" mode backed by a
bundled dataset, or a "memory" mode for tests:

```go
case pbstore.ModeMemory:
    return pbstore.OpenMemory(pbstore.MemoryConfig{ /* ... */ })
```

**Add a configuration option** that reaches the store:

```go
type Config struct {
    // ...
    // Demo enables the read-only demo dataset.
    Demo bool `toml:"demo"`
}
```

**Swap the whole store** for a domain-specific one.

### The cost

**Low to medium.** `cmd/tui/main.go` is yours.

⚠️ **The `Store` interface is a contract.** Implementing it means
implementing twelve methods, with the right semantics (the contexts,
the error wrapping, the allowlist). Read the interface documentation
carefully.

## 23.9 The agent tools

The agentic layer (Chapter 22) added a seventh extension point: the
**tools** the model can call.

### The interface

The seam is the `Tool` interface, and it is small on purpose:

```go
type Tool interface {
    Name() string
    Description() string
    Run(ctx context.Context, input string) (string, error)
}
```

### What a downstream project can do

**Add a tool** that reaches a part of the domain the template does not
know about:

```go
registry.Register(agent.NewGuardedTool(
    NewResolveTermTool(store),
    gate,
    "Resolve a term against the external vocabulary.",
))
```

**Replace a tool** by registering one with the same name: the registry
keeps the registration order and replaces the tool.

**Remove a tool** by not registering it. The registry is what the
model sees, so a tool that is not in it does not exist for the model.

### The cost

**Low.** A tool is one file and one registration.

⚠️ **A mutating tool goes behind the gate.** The `NewGuardedTool`
wrapper is what makes the user approve the call. A mutating tool
registered without it writes without asking, and the agent page will
never show the request.

⚠️ **The description is the contract with the model.** A tool whose
description is vague will be called at the wrong moment, and the
symptom is a model that ignores your data — not an error.

## 23.10 The extension matrix

| Extension point | Cost  | Touches the core? | Reversible? |
| --------------- | ----- | ----------------- | ----------- |
| The renderer    | Low   | No                | Yes         |
| The migrations  | Zero  | No                | Yes         |
| The theme       | Low   | Yes (one file)    | Yes         |
| The agent tools | Low   | No                | Yes         |
| The pages       | Medium| Yes (two files)   | Yes         |
| The sync        | High  | No (implement)    | Yes         |
| The store       | High  | No (implement)    | Yes         |

## 23.11 The order to extend in

⚠️ **Do not start with the store.** The temptation is to build the
data layer first, because it feels like the "real" work. But the store
is the part you are least likely to change.

The order that minimises rework:

```
1. The migration        (what is the data?)
2. The renderer         (how is it shown?)
3. The configuration    (what is tunable?)
4. The theme            (how does it feel?)
5. The pages            (what extra screens?)
6. The store            (only if 1-5 are not enough)
```

⚠️ **Rebuilding the store after you have written five pages is
expensive**, because every page that touches it has to be revisited.
Rebuilding the renderer is cheap, because it is a single injected
object.

## 23.12 A worked example: adding a domain

Let us walk through adding a "measurement units" domain to the
template, in the order above.

### Step 1 — the migration

```go
// internal/domain/units/migrations.go
package units

import "github.com/pocketbase/pocketbase/core"

// Migrations returns the collections required by the units domain.
func Migrations() []core.Migration {
    return []core.Migration{
        {
            File: "1700000001_create_units.go",
            Up: func(txApp core.App) error {
                collection := core.NewBaseCollection("units")
                collection.Fields.Add(
                    &core.TextField{Name: "code", Required: true, Max: 16},
                    &core.TextField{Name: "name", Required: true, Max: 100},
                    &core.TextField{Name: "symbol", Max: 16},
                    &core.NumberField{Name: "factor", OnlyInt: false},
                    &core.SelectField{Name: "dimension", Values: []string{"length", "mass", "time", "temperature"}},
                    &core.JSONField{Name: "aliases"},
                )
                collection.Indexes = []string{
                    "CREATE UNIQUE INDEX `idx_units_code` ON `units` (`code`)",
                }
                return txApp.Save(collection)
            },
            Down: func(txApp core.App) error {
                collection, err := txApp.FindCollectionByNameOrId("units")
                if err != nil {
                    return nil
                }
                return txApp.Delete(collection)
            },
        },
    }
}
```

### Step 2 — the renderer

```go
// internal/domain/units/renderer.go
package units

import (
    "fmt"
    "strings"

    "github.com/incurl/flint/internal/components"
    "github.com/incurl/flint/internal/pbstore"
)

// Renderer renders a measurement unit.
type Renderer struct{}

var _ components.RecordRenderer = (*Renderer)(nil)

func (r *Renderer) Title(rec *pbstore.Record) string {
    if rec == nil {
        return ""
    }
    name := rec.StringField("name")
    symbol := rec.StringField("symbol")
    switch {
    case name != "" && symbol != "":
        return fmt.Sprintf("%s (%s)", name, symbol)
    case name != "":
        return name
    default:
        return rec.StringField("code")
    }
}

func (r *Renderer) Summary(rec *pbstore.Record) string {
    if rec == nil {
        return ""
    }
    parts := []string{rec.StringField("code")}
    if d := rec.StringField("dimension"); d != "" {
        parts = append(parts, d)
    }
    return strings.Join(parts, " · ")
}

func (r *Renderer) Fields(rec *pbstore.Record) []components.FieldRow {
    if rec == nil {
        return nil
    }
    return []components.FieldRow{
        {Name: "Code", Value: rec.StringField("code")},
        {Name: "Name", Value: rec.StringField("name")},
        {Name: "Symbol", Value: rec.StringField("symbol")},
        {Name: "Dimension", Value: rec.StringField("dimension")},
        {Name: "Factor", Value: fmt.Sprintf("%v", rec.Fields["factor"])},
    }
}
```

### Step 3 — the configuration

```go
type Config struct {
    // ...
    // DefaultCollection is the collection opened at startup.
    DefaultCollection string `toml:"default_collection"`
}
```

### Step 4 — the wiring

```go
// cmd/tui/main.go
func main() {
    // ...
    model := app.New(store, cfg, &units.Renderer{})
    // ...
}

func appMigrations() []core.Migration {
    return units.Migrations()
}
```

### Step 5 — the default collection

```go
// In app.New, after the runtime is created:
if cfg.DefaultCollection != "" {
    runtime.Collection = cfg.DefaultCollection
}
```

That is the whole extension. **No core file was modified.**

### Step 6 — the tests

```go
// internal/domain/units/renderer_test.go
func TestRenderer(t *testing.T) {
    r := &Renderer{}

    rec := &pbstore.Record{
        Fields: map[string]any{
            "code":      "m",
            "name":      "metre",
            "symbol":    "m",
            "dimension": "length",
        },
    }

    if got := r.Title(rec); got != "metre (m)" {
        t.Errorf("Title() = %q, want %q", got, "metre (m)")
    }
}
```

## 23.13 Exercises

1. Implement the `SectionedRenderer` optional interface from §23.3 and
   render grouped sections in the record card.

2. Implement the page registry from §23.4 and move the existing pages
   into it. Measure how many lines of `internal/app/app.go` you
   removed.

3. Add a `demo` mode: a read-only store backed by a bundled JSON file,
   loaded into an in-memory collection at startup.

4. Write a custom `Store` implementation that wraps the embedded one
   and adds a "preview" mode to the sync (it reports what *would*
   happen without writing).

5. Add a theme variant and a `--theme` flag value for it. Verify the
   configuration validation accepts it.

6. Extend the template with a domain of your own, following the
   six-step procedure in §23.12.

## 23.14 What you learned

- An extension point must be **reachable without editing the core**.
- The template has **seven**: the renderer, the migrations, the theme,
  the pages, the sync, the store and the agent tools.
- The **renderer** is the cheapest and the most widely used.
- The **agent tools** are cheap too, and a mutating one belongs behind
  the permission gate.
- The **pages** are the most awkward; a registry makes them one line.
- The **migrations** are free — `cmd/tui/main.go` is yours.
- The **sync** and the **store** are expensive, and you rarely need them.
- Extend in the order **migration → renderer → config → theme → pages →
  store**, because the early ones are cheap to change later.
- Add capabilities with **optional interfaces** (type assertions), not
  by breaking the published ones.

## 23.15 Further reading

- `internal/components/recordcard.go` — the `RecordRenderer` interface.
- `internal/app/app.go` — the injection points.
- `examples/wikidata/` — a complete worked extension.
- `internal/pbstore/store.go` — the `Store` contract.

Next: [Chapter 24 — Patterns and Practices](ch24-patterns-and-practices.md).
