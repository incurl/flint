# Skill: add a record renderer

**Goal:** change how records are displayed — the table title, the
summary line and the detail rows.

**Time:** ~15 minutes. Touches 2 files.

This is the highest-leverage extension point: one interface drives the
record table, the record card and the search results at once.

---

## How it fits together

```
components.RecordRenderer
        │
        ├── RecordCard.Fields()   → the detail page rows
        ├── RecordsPage table      → Title and Summary columns
        └── SearchPage results     → Title and Summary columns
```

Implement the interface and every view that shows a record follows.

---

## Step 1 — implement the interface

Create `internal/domain/<name>_renderer.go`, or in your own package.

```go
package domain

import (
	"fmt"
	"strings"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
)

// ArticleRenderer renders an article record.
//
// It implements [components.RecordRenderer], so it plugs into the record
// card, the record table and the search results without touching the
// core packages.
type ArticleRenderer struct {
	// showBody controls whether the body preview is included in the
	// summary.
	showBody bool
}

// Compile-time check that ArticleRenderer satisfies the interface.
//
// This line turns a signature mismatch into a compile error at the
// definition site, not at the call site.
var _ components.RecordRenderer = (*ArticleRenderer)(nil)

// NewArticleRenderer creates a renderer.
func NewArticleRenderer(showBody bool) *ArticleRenderer {
	return &ArticleRenderer{showBody: showBody}
}

// Title returns the article title.
func (r *ArticleRenderer) Title(rec *pbstore.Record) string {
	if rec == nil {
		return ""
	}

	return rec.StringField("title")
}

// Summary returns the one line description of the article.
func (r *ArticleRenderer) Summary(rec *pbstore.Record) string {
	if rec == nil {
		return ""
	}

	parts := make([]string, 0, 3)

	if status := rec.StringField("status"); status != "" {
		parts = append(parts, status)
	}

	if author := rec.StringField("author_name"); author != "" {
		parts = append(parts, author)
	}

	if r.showBody {
		if body := rec.StringField("body"); body != "" {
			parts = append(parts, truncate(body, 60))
		}
	}

	return strings.Join(parts, " · ")
}

// Fields returns the ordered list of the displayed rows.
func (r *ArticleRenderer) Fields(rec *pbstore.Record) []components.FieldRow {
	if rec == nil {
		return nil
	}

	rows := []components.FieldRow{
		{Name: "Title", Value: rec.StringField("title")},
		{Name: "Status", Value: rec.StringField("status")},
	}

	if v := rec.StringField("body"); v != "" {
		rows = append(rows, components.FieldRow{Name: "Body", Value: v})
	}

	return rows
}

// truncate shortens a string to the given number of runes.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}

	return string(runes[:max]) + "…"
}
```

### The three methods

| Method   | Used by                     | Should return                        |
| -------- | --------------------------- | ------------------------------------ |
| `Title`  | the table column, the card  | A short label. Never empty if you can avoid it. |
| `Summary`| the table column, the card  | One line. No newlines.              |
| `Fields` | the detail rows             | Ordered rows, most important first.  |

**Always handle `rec == nil`.** The default renderer does, and the
component calls these methods during layout, when a record may not be
loaded yet.

---

## Step 2 — wire it in

In `cmd/tui/main.go`, `openStore` is not the place — the renderer goes
to `app.New`:

```go
model := app.New(store, cfg, domain.NewArticleRenderer(true))
```

`app.New` accepts `nil` for the renderer, which selects
`components.DefaultRecordRenderer`.

To choose the renderer from the config, add a setting (see
[`add-a-config-option.md`](add-a-config-option.md)) and switch on it:

```go
var renderer components.RecordRenderer

switch cfg.Display {
case "articles":
	renderer = domain.NewArticleRenderer(true)
default:
	renderer = nil // the default renderer
}

model := app.New(store, cfg, renderer)
```

---

## Step 3 — test it

```go
// TestArticleRenderer verifies the title and the summary.
func TestArticleRenderer(t *testing.T) {
	rec := &pbstore.Record{
		ID:             "rec1",
		CollectionName: "articles",
		Fields: map[string]any{
			"title":  "Hello",
			"status": "published",
		},
	}

	r := domain.NewArticleRenderer(false)

	if got := r.Title(rec); got != "Hello" {
		t.Errorf("Title() = %q, want %q", got, "Hello")
	}

	if got := r.Summary(rec); got != "published" {
		t.Errorf("Summary() = %q, want %q", got, "published")
	}
}
```

Put the test in your own package's `_test.go` file, next to the
renderer.

---

## Step 4 — verify

```bash
gofmt -w ./internal
go build ./... && go test ./...
golangci-lint run
```

---

## Using the generic renderer instead

If you do not need a custom implementation, configure the generic one:

```go
renderer := &components.DefaultRecordRenderer{
	TitleFields: []string{"title", "name", "label"},
	MaxFields:   8,
}

model := app.New(store, cfg, renderer)
```

`TitleFields` is tried in order; the first non-empty field becomes the
title. When it is empty, the default renderer uses a heuristic over the
record's fields.

This is often enough — reach for a full implementation only when you
need to compute a value, format nested JSON, or show a domain-specific
fallback.

---

## Gotchas

| Mistake                                                | Consequence                                              |
| ------------------------------------------------------ | -------------------------------------------------------- |
| Forgetting the `var _ components.RecordRenderer = ...` check | The mismatch surfaces far from the definition.      |
| Not handling `rec == nil`                              | A panic when the component renders before loading.        |
| Returning a multi-line `Summary`                      | The table row breaks. Keep it to one line.                |
| Building a `lipgloss.Style` in the renderer            | The renderer should return plain text. Style it in the view. |
| Reading a field that does not exist                    | `StringField` returns `""`. It does not panic.            |
| Expecting `Fields` to be called on every record        | It is called for the detail page only.                   |

---

## Rendering nested JSON

Field values are `any`. To read a nested value, type-assert:

```go
func (r *ArticleRenderer) meta(rec *pbstore.Record) string {
	raw, ok := rec.Field("meta")
	if !ok {
		return ""
	}

	byKey, ok := raw.(map[string]any)
	if !ok {
		return ""
	}

	value, _ := byKey["source"].(string)

	return value
}
```

Use `rec.JSON()` when you want to show the whole record as pretty JSON —
that is what the JSON viewer component does.
