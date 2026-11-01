# Example: a Wikidata adapter

This guide builds a **Wikidata item and property browser** on top of
Flint, without modifying a single core file.

It is the original motivating use case of the template, and it exercises
every extension point:

- a custom **record renderer**,
- a custom **migration** creating the collections,
- a **label lookup cache** populated by a sync,
- a **custom filter transform** turning a human query into a native
  PocketBase filter.

> The adapter lives in `examples/wikidata`. It is an *example*, not a core
> package: the core never mentions Wikidata.

## The data model

Two collections are enough:

| Collection    | Fields                                     | Purpose                       |
| ------------- | ------------------------------------------ | ----------------------------- |
| `wd_items`    | `qid`, `labels`, `descriptions`, `claims`  | The items (Q42, Q31, ...)      |
| `wd_labels`   | `qid`, `language`, `label`                 | The label lookup cache        |

The `claims` field is a `JSON` field: it holds the nested claim structure
verbatim, which is exactly what the collapsible JSON viewer is made for.

## 1. The migration

```go
// examples/wikidata/migrations.go
package wikidata

import (
	"github.com/pocketbase/pocketbase/core"
)

// Migrations returns the collections required by the Wikidata adapter.
func Migrations() []core.Migration {
	return []core.Migration{
		{
			File: "1700000001_create_wikidata.go",
			Up: func(txApp core.App) error {
				items := core.NewBaseCollection("wd_items")
				items.Fields.Add(
					&core.TextField{Name: "qid", Required: true, Max: 32},
					&core.JSONField{Name: "labels"},
					&core.JSONField{Name: "descriptions"},
					&core.JSONField{Name: "claims"},
				)
				if err := txApp.Save(items); err != nil {
					return err
				}

				labels := core.NewBaseCollection("wd_labels")
				labels.Fields.Add(
					&core.TextField{Name: "qid", Required: true, Max: 32},
					&core.TextField{Name: "language", Required: true, Max: 16},
					&core.TextField{Name: "label", Max: 500},
				)
				return txApp.Save(labels)
			},
			Down: func(txApp core.App) error {
				return nil
			},
		},
	}
}
```

Register it in `cmd/tui/main.go`:

```go
func appMigrations() []core.Migration {
	return wikidata.Migrations()
}
```

## 2. The record renderer

The renderer decides how a record is summarised. It implements
`components.RecordRenderer`:

```go
// examples/wikidata/renderer.go
package wikidata

import (
	"fmt"
	"strings"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
)

// Renderer renders a Wikidata item.
//
// It implements components.RecordRenderer, so it plugs into the record
// card and into the record table without touching the core packages.
type Renderer struct {
	// language is the preferred label language.
	language string
}

// Compile-time check that Renderer satisfies the interface.
var _ components.RecordRenderer = (*Renderer)(nil)

// NewRenderer creates a renderer for the given label language.
func NewRenderer(language string) *Renderer {
	if language == "" {
		language = "en"
	}
	return &Renderer{language: language}
}

// Title returns the label of the item, falling back to the QID.
func (r *Renderer) Title(rec *pbstore.Record) string {
	if rec == nil {
		return ""
	}

	if label := r.label(rec); label != "" {
		return label
	}
	return rec.StringField("qid")
}

// Summary returns the description of the item.
func (r *Renderer) Summary(rec *pbstore.Record) string {
	if rec == nil {
		return ""
	}

	description := r.description(rec)
	qid := rec.StringField("qid")

	if description == "" {
		return qid
	}
	return fmt.Sprintf("%s — %s", qid, description)
}

// Fields returns the displayed rows.
func (r *Renderer) Fields(rec *pbstore.Record) []components.FieldRow {
	if rec == nil {
		return nil
	}

	rows := []components.FieldRow{
		{Name: "QID", Value: rec.StringField("qid")},
		{Name: "Label", Value: r.label(rec)},
		{Name: "Description", Value: r.description(rec)},
	}

	if claims := rec.StringField("claims"); claims != "" {
		rows = append(rows, components.FieldRow{
			Name:  "Claims",
			Value: fmt.Sprintf("%d bytes of JSON", len(claims)),
		})
	}

	return rows
}

// label extracts the label for the preferred language.
//
// The `labels` field holds a JSON object shaped like
// {"en": {"language": "en", "value": "Douglas Adams"}, ...}.
func (r *Renderer) label(rec *pbstore.Record) string {
	labels, ok := rec.Field("labels")
	if !ok {
		return ""
	}

	byLang, ok := labels.(map[string]any)
	if !ok {
		return ""
	}

	entry, ok := byLang[r.language].(map[string]any)
	if !ok {
		return ""
	}

	value, _ := entry["value"].(string)
	return value
}

// description extracts the description for the preferred language.
func (r *Renderer) description(rec *pbstore.Record) string {
	descriptions, ok := rec.Field("descriptions")
	if !ok {
		return ""
	}

	byLang, ok := descriptions.(map[string]any)
	if !ok {
		return ""
	}

	entry, ok := byLang[r.language].(map[string]any)
	if !ok {
		return ""
	}

	value, _ := entry["value"].(string)
	return strings.TrimSpace(value)
}
```

Wire it into the root model:

```go
// cmd/tui/main.go
renderer := wikidata.NewRenderer("en")
model := app.New(store, cfg, renderer)
```

The renderer is now used by the record table, the record card and the
search results.

## 3. The label lookup cache

The label cache is a plain collection queried through `pbstore`:

```go
// examples/wikidata/labels.go
package wikidata

import (
	"context"
	"errors"
	"fmt"

	"github.com/incurl/flint/internal/pbstore"
)

// ErrUnknownLabel is returned when a QID is not in the cache.
var ErrUnknownLabel = errors.New("wikidata: unknown label")

// LabelStore resolves the labels from the local cache.
type LabelStore struct {
	// store is the data layer.
	store pbstore.Store
	// language is the preferred label language.
	language string
}

// NewLabelStore creates a label store.
func NewLabelStore(store pbstore.Store, language string) *LabelStore {
	return &LabelStore{store: store, language: language}
}

// Label returns the label of a QID.
func (l *LabelStore) Label(ctx context.Context, qid string) (string, error) {
	rec, err := l.store.FindFirst(ctx, "wd_labels", fmt.Sprintf(
		"qid = %q && language = %q", qid, l.language,
	))
	if err != nil {
		if errors.Is(err, pbstore.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", ErrUnknownLabel, qid)
		}
		return "", err
	}

	return rec.StringField("label"), nil
}

// Sync refreshes the label cache from the remote server.
//
// It is an explicit operation: the TUI triggers it from the sync page.
func (l *LabelStore) Sync(ctx context.Context, batchSize int) (*pbstore.SyncReport, error) {
	return l.store.SyncCollection(ctx, pbstore.SyncRequest{
		RemoteCollection: "wd_labels",
		LocalCollection:  "wd_labels",
		BatchSize:        batchSize,
	})
}
```

The filter `qid = "Q42" && language = "en"` is a **native PocketBase
filter**, parsed by PocketBase itself — no custom query language.

## 4. A custom filter transform

A friendly search box can be translated into a native filter:

```go
// examples/wikidata/search.go
package wikidata

import (
	"fmt"
	"strings"
)

// BuildFilter translates a human query into a native PocketBase filter.
//
// A query such as "Q42" or "human instance of" is turned into a filter
// that PocketBase parses. The output is always a valid native filter,
// so the embedded and the remote backends behave identically.
func BuildFilter(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	// A QID is an exact match.
	if strings.HasPrefix(strings.ToUpper(query), "Q") {
		return fmt.Sprintf("qid = %q", strings.ToUpper(query))
	}

	// Otherwise search the label cache.
	return fmt.Sprintf(`label ~ %q`, query)
}
```

The result is used exactly like any other filter:

```go
result, err := store.List(ctx, pbstore.Query{
	Collection: "wd_items",
	Filter:     wikidata.BuildFilter(userInput),
})
```

## 5. A custom page

A dedicated Wikidata page can reuse the core components without touching
them:

```go
// examples/wikidata/page.go
package wikidata

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/theme"
)

// Page is a Wikidata specific screen.
//
// It owns its own state and only talks to the store through the
// pbstore.Store interface.
type Page struct {
	// store is the data layer.
	store pbstore.Store
	// labels is the label lookup cache.
	labels *LabelStore
	// theme is the shared style palette.
	theme *theme.Theme
	// renderer formats the items.
	renderer *Renderer

	// query is the search input.
	query textinput.Model
	// card renders the selected item.
	card *components.RecordCard
	// loading reports whether a query is in flight.
	loading bool
	// err holds the last error.
	err error
}

// NewPage creates the Wikidata page.
func NewPage(store pbstore.Store, labels *LabelStore, t *theme.Theme) *Page {
	query := textinput.New()
	query.Placeholder = "Q42 or a label"
	query.Prompt = "query: "

	return &Page{
		store:    store,
		labels:   labels,
		theme:    t,
		renderer: NewRenderer("en"),
		query:    query,
		card:     components.NewRecordCard(t, NewRenderer("en")),
	}
}

// Init implements tea.Model.
func (p *Page) Init() tea.Cmd {
	p.query.Focus()
	return p.search()
}

// Update implements tea.Model.
func (p *Page) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			p.loading = true
			return p, tea.Batch(p.search())
		}
	}

	var cmd tea.Cmd
	p.query, cmd = p.query.Update(msg)
	return p, cmd
}

// View implements tea.Model.
func (p *Page) View() string {
	header := p.theme.Title.Render("Wikidata") + "\n" + p.query.View()

	if p.loading {
		return header + "\n\n" + p.theme.Muted.Render("searching…")
	}
	if p.err != nil {
		return header + "\n\n" + p.theme.Error.Render(p.err.Error())
	}
	return header + "\n\n" + p.card.View()
}

// search returns the command that runs the query.
func (p *Page) search() tea.Cmd {
	store, filter := p.store, BuildFilter(p.query.Value())

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		result, err := store.List(ctx, pbstore.Query{
			Collection: "wd_items",
			Filter:     filter,
			PerPage:    20,
		})
		if err != nil {
			return searchErrMsg{Err: err}
		}

		if len(result.Records) > 0 {
			p.card.SetRecord(result.Records[0])
		}
		return searchErrMsg{}
	}
}

// searchErrMsg carries the result of a Wikidata query.
type searchErrMsg struct {
	Err error
}
```

## What this example demonstrates

| Extension point            | Where it is used                            |
| -------------------------- | ------------------------------------------- |
| Custom record renderer     | `Renderer` — the title, summary and fields  |
| Custom migration           | `Migrations()` — the two collections        |
| Custom lookup cache        | `LabelStore` — the `wd_labels` collection   |
| Custom filter transform    | `BuildFilter()` — a human query → a filter  |
| Custom page                | `Page` — a self-contained screen            |

**Not a single core file was modified.** The core packages (`pbstore`,
`app`, `theme`, `components`) stayed untouched, which is exactly the
point of the template.

## Next steps

- Add a `claims` browser that expands the nested JSON with the
  `components.JSONViewer`.
- Add a property browser on top of the `wd_properties` collection.
- Sync the label cache into the embedded database to browse the items
  offline.
