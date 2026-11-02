# Chapter 25 — Domain Patterns

Chapter 16 explained the *architecture*. This chapter explains the
*data modelling* decisions that the architecture enables, using the
four domains the template targets.

The four:

1. **Controlled vocabularies** — a list of terms with definitions.
2. **Lookup caches** — a local replica of a remote reference dataset.
3. **Knowledge graphs** — items with claims, properties and labels.
4. **Annotation and audit tools** — structured records with provenance.

Each is a different *shape* of data, and each shape suggests different
modelling choices. The point of this chapter is that you can pick the
shape that fits, and the template's architecture does not change.

## 24.1 The vocabulary shape

**The domain.** A list of terms: `go`, `rust`, `zig`, each with a
label, a definition, and maybe a category.

**The naive model.** One collection, flat fields.

```
terms
├── code      text
├── label     text
├── definition text
└── category  text
```

**The problem.** Real vocabularies have *metadata about the terms
themselves*: synonyms, sources, confidence levels, editorial notes,
and a version history.

**The model that scales.**

```
terms                          the canonical entry
├── code        text      (unique index)
├── label       text
├── definition  editor
├── category    select
├── status      select      (draft, approved, deprecated)
└── source_id   relation → sources

sources                        where the term came from
├── name        text
├── url         text
├── retrieved   date
└── license     text

term_aliases                   the many-to-many
├── term_id     relation → terms
├── alias       text
├── language    text
└── kind        select      (synonym, abbreviation, former label)

term_history                   the audit trail
├── term_id     relation → terms
├── changed_at  date
├── changed_by  text
└── snapshot    json        the whole record as it was
```

### Why this shape

**The aliases are many-to-many.** A term can have five synonyms; a
synonym can apply to several terms. Putting the aliases in a JSON
field would make them unqueryable — and "find every term with the
alias X" is exactly the query a vocabulary tool needs.

⚠️ **This is the rule from Chapter 10 in action:** if you need to
filter or join on it, it must be a schema field, not a JSON blob.

**The history is append-only.** It is never queried for a mutation,
only for a display, so it could be JSON — but keeping it relational
lets you ask "what changed in 2024?".

**The sources are shared.** Many terms come from the same source, so
the source is a row, not a string.

### The query patterns

```go
// The canonical entry plus its aliases.
result, err := store.List(ctx, pbstore.Query{
    Collection: "terms",
    Filter:     `code = "go"`,
    Expand:     "source_id",
})

// Every term with a given alias.
result, err := store.List(ctx, pbstore.Query{
    Collection: "term_aliases",
    Filter:     `alias ~ "golang"`,
})
```

### The renderer

```go
func (r *TermRenderer) Title(rec *pbstore.Record) string {
    return rec.StringField("label")
}

func (r *TermRenderer) Fields(rec *pbstore.Record) []components.FieldRow {
    rows := []components.FieldRow{
        {Name: "Code", Value: rec.StringField("code")},
        {Name: "Definition", Value: rec.StringField("definition")},
        {Name: "Category", Value: rec.StringField("category")},
        {Name: "Status", Value: rec.StringField("status")},
    }
    return rows
}
```

### The editorial workflow

A vocabulary usually has a **status** field and a **review queue**. The
status bar can show the count:

```go
count, err := store.Count(ctx, "terms", `status = "draft"`)
```

And a page can list the drafts:

```go
result, err := store.List(ctx, pbstore.Query{
    Collection: "terms",
    Filter:     `status = "draft"`,
})
```

### The pitfall

⚠️ **Do not over-normalise.** A vocabulary with 500 terms and no
relations between them does not need five collections. The rule of
thumb:

> **Normalise when a value is *shared* between records, or when you
> need to *query* it independently.**

If "category" is only ever displayed, keep it as a field. If you need
"list all the terms in category X, and how many there are", it can
still be a field — but the moment you want "the categories themselves,
with descriptions", it becomes a collection.

## 24.2 The lookup cache shape

**The domain.** A local replica of a remote reference dataset —
labels, ISO codes, country lists.

**The naive model.** Mirror the remote schema exactly and sync it.

**The problem.** The remote schema changes without warning. Your
local copy breaks.

**The model that survives.**

```
cache_entries                   the local replica
├── remote_id    text      (the id in the remote source)
├── source       text      (which remote collection it came from)
├── namespace    text      (the "en", "fr", … partition)
├── key          text      (the lookup key)
├── value        text      (the display value)
├── payload      json      (the full remote record, verbatim)
└── synced_at    date      (when this copy was written)
```

The **payload** holds the remote record verbatim. That is the key
insight:

> **Cache the source record verbatim, and index the few fields you
> actually query.**

This gives you three things:

1. **Forward compatibility.** When the remote schema gains a field,
   your payload already has it — you just need to expose it.
2. **Queryability.** The indexed fields (`namespace`, `key`) are
   schema fields, so they are filterable.
3. **Debuggability.** When a sync goes wrong, you can see exactly what
   the remote sent.

### The migration

```go
{
    File: "1700000001_create_cache_entries.go",
    Up: func(txApp core.App) error {
        collection := core.NewBaseCollection("cache_entries")
        collection.Fields.Add(
            &core.TextField{Name: "remote_id", Required: true, Max: 64},
            &core.TextField{Name: "source", Required: true, Max: 64},
            &core.TextField{Name: "namespace", Required: true, Max: 32},
            &core.TextField{Name: "key", Required: true, Max: 256},
            &core.TextField{Name: "value", Max: 1000},
            &core.JSONField{Name: "payload"},
            &core.DateField{Name: "synced_at"},
        )
        collection.Indexes = []string{
            "CREATE INDEX `idx_cache_lookup` ON `cache_entries` (`source`, `namespace`, `key`)",
            "CREATE UNIQUE INDEX `idx_cache_unique` ON `cache_entries` (`source`, `namespace`, `remote_id`)",
        }
        return txApp.Save(collection)
    },
    Down: func(txApp core.App) error { return nil },
}
```

### The lookup

```go
func (c *Cache) Lookup(ctx context.Context, source, namespace, key string) (string, bool) {
    rec, err := c.store.FindFirst(ctx, "cache_entries", fmt.Sprintf(
        "source = %q && namespace = %q && key = %q",
        source, namespace, key,
    ))
    if err != nil {
        return "", false
    }
    return rec.StringField("value"), true
}
```

### The sync with the local schema

The sync (§17) copies the remote fields into the local ones. But the
local schema is *not* the remote schema — it has `source` and
`namespace` and `synced_at`, which the remote does not have.

This means the built-in sync, which copies the fields verbatim, is not
enough. You need a **custom sync** that maps the remote record onto the
local shape.

```go
// CacheSync copies the remote records into the local cache, mapping the
// remote fields onto the local cache schema.
type CacheSync struct {
    store  pbstore.Store
    source string
}

// Sync performs the copy.
func (s *CacheSync) Sync(ctx context.Context, remoteCollection, namespace string) (*pbstore.SyncReport, error) {
    result, err := s.store.List(ctx, pbstore.Query{
        Collection: remoteCollection,
        Filter:     fmt.Sprintf(`namespace = %q`, namespace),
        PerPage:    500,
    })
    if err != nil {
        return nil, err
    }

    report := &pbstore.SyncReport{Collection: "cache_entries"}
    now := time.Now().UTC().Format("2006-01-02 15:04:05.000Z")

    for _, rec := range result.Records {
        payload := map[string]any{
            "remote_id": rec.ID,
            "source":    s.source,
            "namespace": namespace,
            "key":       rec.StringField("key"),
            "value":     rec.StringField("value"),
            "payload":   rec.Fields,
            "synced_at": now,
        }

        // Upsert by the unique key.
        existing, err := s.store.FindFirst(ctx, "cache_entries", fmt.Sprintf(
            "source = %q && namespace = %q && remote_id = %q",
            s.source, namespace, rec.ID,
        ))

        if err == nil {
            if _, err := s.store.Update(ctx, "cache_entries", existing.ID, payload); err != nil {
                return nil, err
            }
            report.Updated++
        } else {
            if _, err := s.store.Create(ctx, "cache_entries", payload); err != nil {
                return nil, err
            }
            report.Created++
        }
        report.Fetched++
    }

    return report, nil
}
```

⚠️ **This is the case where a custom sync is justified.** The built-in
sync copies the fields verbatim; here the local shape is *deliberately
different*.

⚠️ **But it is O(n) queries.** The `FindFirst` per record is the N+1
problem. For a large cache, fetch the existing entries once:

```go
// Fetch the existing entries once.
existing := map[string]string{}   // remote_id → local id
result, err := s.store.List(ctx, pbstore.Query{
    Collection: "cache_entries",
    Filter:     fmt.Sprintf(`source = %q && namespace = %q`, s.source, namespace),
    PerPage:    10000,
})
for _, rec := range result.Records {
    existing[rec.StringField("remote_id")] = rec.ID
}
```

### The staleness

The `synced_at` field answers "when was this copied?". The status bar
can show it:

```go
func (p *CachePage) staleness() string {
    rec, err := p.store.FindFirst(ctx, "cache_entries", `source = "wikidata"`)
    if err != nil {
        return "never synced"
    }
    return "synced " + rec.StringField("synced_at")
}
```

## 24.3 The knowledge graph shape

**The domain.** Wikidata-style items with claims and properties.

**The naive model.** One collection with a JSON blob for the claims.

```
items
├── qid        text
├── labels     json
└── claims     json
```

**The problem.** You cannot answer "which items have property P31 =
Q5?" with a JSON blob. And you cannot join across the items.

**The model that scales.**

```
wd_items
├── qid          text      (unique index)
├── label_en     text
├── label_fr     text
├── description_en text
└── payload      json      (the full dump, verbatim)

wd_properties
├── pid          text      (unique index)
├── label_en     text
├── datatype     select    (string, item, external-id, time, ...)
└── description_en text

wd_statements                  the claims, normalised
├── item_id      relation → wd_items
├── property_id  relation → wd_properties
├── value_item   relation → wd_items   (for item-valued claims)
├── value_text   text                  (for string-valued claims)
├── value_number number                (for numeric claims)
├── value_date   date                  (for time-valued claims)
├── rank         select    (normal, preferred, deprecated)
└── qualifiers   json      (the qualifiers, which stay nested)
```

### The normalisation decision

**The item-valued claims become relations.** "instance of: human" is a
row with `value_item` pointing at the `human` item.

This is what makes the graph traversable:

```go
// Every item that is an instance of human.
result, err := store.List(ctx, pbstore.Query{
    Collection: "wd_statements",
    Filter:     `property_id = "P31" && value_item = "Q5"`,
})
```

⚠️ **Not everything should be normalised.** The **qualifiers** stay as
JSON, because they are always read together with their statement and
never queried independently:

```go
// A statement with its qualifiers.
statement := &pbstore.Record{
    Fields: map[string]any{
        "item_id":     "Q42",
        "property_id": "P580",
        "value_date":  "2011-01-01",
        "qualifiers": map[string]any{
            "P585": "2011-01-01",    // a point in time
        },
    },
}
```

**The rule.**

> **Normalise what you *join* and *filter* on. Keep as JSON what you
> only ever *read* alongside its parent.**

### The labels

The `label_en` / `label_fr` columns are the **materialised** labels —
denormalised on purpose, so that the list view is a single query with
no join.

The full multilingual label set stays in the `payload` JSON.

⚠️ **Materialising a label means keeping it in sync.** When a label
changes on the remote, the sync must update it. The custom sync does
that.

⚠️ **The materialised column is also the *only* way to filter by
language efficiently.** The dotted filter syntax
(`labels.en.value = "..."`) only resolves with the `SimpleFieldResolver`
— not with the record resolver that the embedded backend uses — and even
where it does resolve, it compiles to a `json_extract` on every row,
which cannot use an index.

See Chapter 10, "The field resolver", for the details. The short
version: **if you filter on it, materialise it.**

### The renderer

```go
func (r *Renderer) Title(rec *pbstore.Record) string {
    if label := rec.StringField("label_" + r.language); label != "" {
        return label
    }
    return rec.StringField("qid")
}
```

⚠️ **The renderer with a dynamic field name is a code smell.** It
works, but it couples the renderer to the schema. A cleaner approach
is a fixed set of columns with a `languages` field:

```go
// wd_items
├── qid          text
├── label        text      (the preferred label)
├── labels       json      (all the labels)
└── descriptions json
```

⚠️ **Which is better depends on the query pattern.** If you always
filter by the *current* language, the materialised column wins. If you
filter by *any* language, the JSON path wins:

```
labels.en.value = "Douglas Adams"
```

Measure before deciding.

## 24.4 The annotation shape

**The domain.** Users attach notes, tags and corrections to records.

**The naive model.** Add the annotations to the record itself.

⚠️ **This fails immediately.** The annotations are *per-user*, and the
records are shared. Mixing them means every annotation is a record
update — a write storm, a conflict, and no history.

**The model that scales.**

```
annotations
├── target_id    text      (the id of the annotated record)
├── target_type  text      (the collection name)
├── kind         select    (note, tag, correction, rating)
├── body         editor
├── value        text      (for a tag or a rating)
├── author       text
├── created      date
├── confidence   number    (for a correction)
└── payload      json

annotation_state                the aggregated view
├── target_id    text
├── target_type  text
├── tags         json      (the materialised tag set)
├── note_count   number
├── last_updated date
```

### Why the aggregation

The **list view** wants to show, for each record, its tags and its note
count. Computing that with a join on every row is expensive.

The `annotation_state` collection **materialises** it:

```go
// Recompute the state for a target.
func (s *Annotator) recompute(ctx context.Context, targetType, targetID string) error {
    result, err := s.store.List(ctx, pbstore.Query{
        Collection: "annotations",
        Filter:     fmt.Sprintf(`target_type = %q && target_id = %q`, targetType, targetID),
    })
    if err != nil {
        return err
    }

    tags := map[string]bool{}
    notes := 0
    for _, rec := range result.Records {
        switch rec.StringField("kind") {
        case "tag":
            tags[rec.StringField("value")] = true
        case "note":
            notes++
        }
    }

    sorted := make([]string, 0, len(tags))
    for tag := range tags {
        sorted = append(sorted, tag)
    }
    sort.Strings(sorted)

    state := map[string]any{
        "target_id":    targetID,
        "target_type":  targetType,
        "tags":         sorted,
        "note_count":   notes,
        "last_updated": time.Now().UTC().Format("2006-01-02 15:04:05.000Z"),
    }

    existing, err := s.store.FindFirst(ctx, "annotation_state", fmt.Sprintf(
        "target_type = %q && target_id = %q", targetType, targetID,
    ))
    if err == nil {
        _, err = s.store.Update(ctx, "annotation_state", existing.ID, state)
        return err
    }

    _, err = s.store.Create(ctx, "annotation_state", state)
    return err
}
```

### The provenance

Every annotation has an `author` and a `created`. That is the
provenance, and it makes the tool auditable.

For a stronger guarantee, put the annotations in their own *auth*
collection, so that PocketBase's own auth machinery provides the
identity:

```go
// The annotations are auth records, so the author is the record itself.
annotations := core.NewAuthCollection("annotations")
annotations.Fields.Add(
    &core.RelationField{Name: "target", CollectionId: targetCollectionId, MaxSelect: 1},
    &core.SelectField{Name: "kind", Values: []string{"note", "tag", "correction"}},
    &core.EditorField{Name: "body"},
)
```

⚠️ **The auth collection approach is powerful but heavier.** It brings
the authentication machinery, the tokens, the password handling. For a
single-user local tool, a plain `author` text field is simpler.

## 24.5 Choosing the shape

A decision table:

| Your data has…                          | Use                                              |
| ---------------------------------------- | ------------------------------------------------ |
| Shared values that you query             | Relations                                        |
| Values you only display                  | Fields                                           |
| Values you join on                       | Relations                                        |
| A history you never mutate               | A history collection                             |
| Many-to-many                             | A join collection                                 |
| Per-user data                            | A separate collection, never a field              |
| A remote source you mirror               | The cache shape (§24.2)                           |
| Nested structure you read as a whole     | A JSON field                                      |
| Nested structure you query into          | Relations + a JSON field for the remainder        |

## 24.6 The naming convention

⚠️ **Name the collections so that the role is obvious.**

```
terms                 the canonical entities
term_aliases          the join
term_history          the audit
cache_entries         the replica
wd_items              the graph nodes
wd_statements         the graph edges
annotations           the user data
annotation_state      the materialised view
```

The `_` prefix for the internal PocketBase collections (`_collections`,
`_superusers`) is reserved. Do not use it.

⚠️ **Do not put the schema in the collection name.** `terms_v2` is a
migration waiting to happen. The schema evolves through migrations; the
name stays.

## 24.7 The domain package layout

```
internal/domain/
├── terms/
│   ├── migrations.go    the collections
│   ├── renderer.go      how a term is displayed
│   ├── service.go       the business logic
│   └── service_test.go
├── cache/
│   ├── migrations.go
│   ├── sync.go          the custom sync
│   ├── lookup.go        the lookup API
│   └── sync_test.go
└── graph/
    ├── migrations.go
    ├── renderer.go
    └── traverse.go      the graph traversal
```

⚠️ **The domain package must not import the TUI packages.** The
`service.go` should be usable from a CLI, a test or a server. It takes
a `pbstore.Store` and returns data.

```go
// The service is UI-free.
package terms

// Service provides the vocabulary operations.
type Service struct {
    store pbstore.Store
}

// New creates a service.
func New(store pbstore.Store) *Service {
    return &Service{store: store}
}

// Lookup finds a term by its code.
func (s *Service) Lookup(ctx context.Context, code string) (*pbstore.Record, error) {
    return s.store.FindFirst(ctx, "terms", fmt.Sprintf("code = %q", code))
}
```

⚠️ **This separation is what makes the domain testable and reusable.**
The TUI is a *client* of the domain, not the other way around.

## 24.8 Exercises

1. Model a **library catalogue** with the vocabulary shape: the books,
   the authors (many-to-many), the subjects, and the loans.
2. Model a **country reference** with the cache shape and write the
   custom sync.
3. Model a **chemical compound** graph with the knowledge graph shape.
   Decide which claims become relations and which stay JSON.
4. Model a **code review annotation** tool with the annotation shape.
5. Write the decision table for your own domain and check each of your
   collections against it.
6. Move a domain's service out of the TUI and write a CLI that uses it.

## 24.9 What you learned

- The four domains the template targets, and the **shape** each one
  suggests.
- The vocabulary shape: the **canonical entry**, the **many-to-many**
  join, the **sources**, the **history**.
- The lookup cache shape: **index the few fields you query, cache the
  source record verbatim** — forward compatibility for free.
- The knowledge graph shape: **normalise what you join, keep as JSON
  what you only read**.
- The annotation shape: **per-user data is never a field on a shared
  record**; aggregate it into a materialised view.
- The decision table for choosing between a field, a relation and a
  JSON blob.
- The **domain package is UI-free**: the TUI is a client of the
  service, not the other way around.

## 24.10 Further reading

- `examples/wikidata/` — the graph shape, in code.
- `internal/pbstore/embedded.go` — the sync and the dictionary.
- Chapter 10 — the filter grammar, for the query patterns above.

Next: [Chapter 26 — Creative Ideas and Geek Playgrounds](ch26-creative-ideas.md).
