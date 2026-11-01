# Chapter 9 — The App: Collections, Fields, Records

This chapter is the hands-on companion to Chapter 8. We will create a
collection, write records, read them back, and deal with the
peculiarities of PocketBase's dynamic record model.

## 9.1 Creating a collection

```go
collection := core.NewBaseCollection("vocab")

collection.Fields.Add(
    &core.TextField{Name: "key", Required: true, Max: 200},
    &core.TextField{Name: "label", Max: 500},
    &core.TextField{Name: "description", Max: 5000},
)

if err := app.Save(collection); err != nil {
    return err
}
```

`NewBaseCollection` returns a collection pre-populated with the
default configuration for the type: the system fields (`id`,
`created`, `updated`), the default indexes, and the type-specific
options.

### The three collection types

```go
core.NewBaseCollection("vocab")    // ordinary records
core.NewAuthCollection("users")    // with email/password
core.NewViewCollection("active")   // a read-only SQL view
```

Or the generic constructor:

```go
core.NewCollection(core.CollectionTypeAuth, "users")
```

### The fields

Every field type has the same core shape:

```go
&core.TextField{
    Name:     "key",
    Required: true,
    Max:      200,
    Help:     "The stable identifier of the term.",
    Hidden:   false,
}
```

Let us look at the field types you will use most.

**Text** — a string with an optional pattern:

```go
&core.TextField{
    Name:     "qid",
    Required: true,
    Max:      32,
    Pattern:  `^Q[0-9]+$`,   // a Wikidata QID
}
```

⚠️ **The `Pattern` is a regular expression applied to the whole
value.** Anchor it with `^` and `$` if you mean the whole string.

**Bool**:

```go
&core.BoolField{Name: "active"}
```

**Number**:

```go
&core.NumberField{
    Name:     "weight",
    Min:      0,          // optional
    Max:      100,        // optional
    OnlyInt:  true,       // reject floats
}
```

**Select** — a closed set of values:

```go
&core.SelectField{
    Name:   "kind",
    Values: []string{"noun", "verb", "adjective"},
    MaxSelect: 1,        // how many can be picked
}
```

**JSON** — arbitrary structured data:

```go
&core.JSONField{Name: "claims", MaxSize: 2000000}
```

⚠️ **A JSON field is stored as a JSON string** and is validated as
valid JSON on write. It is the right type for nested, schema-less
payloads — exactly what a Wikidata claims blob is.

**Editor** — rich text, stored as HTML:

```go
&core.EditorField{Name: "body"}
```

**Date**:

```go
&core.DateField{Name: "published_at"}
```

**Autodate** — managed by the framework:

```go
collection.Fields.Add(
    &core.AutodateField{Name: "created", OnCreate: true, OnUpdate: false},
    &core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
)
```

⚠️ **Do not add `created`/`updated` yourself.** `NewBaseCollection`
already includes them. Adding a second field with the same name is
rejected by the validation.

**Relation** — a link to another collection:

```go
&core.RelationField{
    Name:          "author",
    CollectionId:  "abc123",
    MaxSelect:     1,
    CascadeDelete: false,
}
```

⚠️ **The relation needs the target collection's id**, which you only
know after creating it. So you create the collection, then add the
relation field and save again — or use the collection's `Id` directly
if you know it.

## 9.2 The FieldsList

`collection.Fields` is a `FieldsList`, which is a slice of `Field`
interfaces with some conveniences:

```go
collection.Fields.Add(
    &core.TextField{Name: "a"},
    &core.TextField{Name: "b"},
)

collection.Fields.AddAt(0, &core.TextField{Name: "first"})

field, _ := collection.Fields.GetByName("a")
fields, _ := collection.Fields.GetById("xyz")
```

⚠️ **`Add` does not return an error.** The field's `Id` is generated
when it is added to the list. If you need the id, add it first, then
read `field.Id`.

⚠️ **Field names must be unique within a collection** and must not
clash with the system fields. The validation catches this at save
time.

## 9.3 Saving a collection

```go
if err := app.Save(collection); err != nil {
    return fmt.Errorf("create collection: %w", err)
}
```

`Save` runs the full validation:
- the name is valid and unique;
- the fields are valid;
- the indexes are consistent;
- the rules are valid filter expressions.

⚠️ **Validation errors are aggregated.** The error may describe
several problems at once. For a TUI that creates collections at
runtime, showing the raw error to the user is usually fine — the
messages are reasonably specific.

### Updating a collection

```go
collection, err := app.FindCollectionByNameOrId("vocab")
if err != nil {
    return err
}

collection.Fields.Add(&core.TextField{Name: "note"})

if err := app.Save(collection); err != nil {
    return err
}
```

⚠️ **Saving a collection with a new field alters the table.** SQLite
supports `ALTER TABLE ... ADD COLUMN`, so this is cheap. But adding a
`NOT NULL` field with a default to a table with existing rows requires
a table rebuild, which PocketBase does for you — at the cost of a
lock.

### Deleting a collection

```go
collection, err := app.FindCollectionByNameOrId("vocab")
if err != nil {
    return err
}

if err := app.Delete(collection); err != nil {
    return err
}
```

⚠️ **This drops the table and all its data.** There is no undo.

## 9.4 Creating a record

```go
record := core.NewRecord(collection)

record.Load(map[string]any{
    "key":         "go",
    "label":       "Go",
    "description": "A statically typed language",
})

if err := app.Save(record); err != nil {
    return err
}

fmt.Println(record.Id)   // the generated id
```

`NewRecord` requires a `*Collection`, so you resolve it first:

```go
collection, err := app.FindCachedCollectionByNameOrId("vocab")
if err != nil {
    return err
}
```

⚠️ **Use `FindCachedCollectionByNameOrId`, not `FindCollectionByNameOrId`,
in a hot path.** The cached variant reads from the in-memory registry
(`app.Store()`); the uncached one hits the database on every call. For
a record listing that resolves the collection once per query, this does
not matter. For a loop over 10,000 records, it does.

### `Load` versus `Set`

```go
record.Load(map[string]any{...})   // bulk load
record.Set("key", "go")            // set one field
```

`Load` calls `Set` for each entry. `Set` looks up the field and uses
its *setter*, which normalises the value:

```go
func (m *Record) Set(key string, value any) {
    switch key {
    case FieldNameExpand:
        m.SetExpand(cast.ToStringMap(value))
    default:
        field := m.SetIfFieldExists(key, value)
        if field == nil {
            // custom key - set it without any transformations
            m.SetRaw(key, value)
        }
    }
}
```

⚠️ **Setting an unknown key stores it as custom data.** It is not
rejected; it is just not part of the schema. This is how the template
handles arbitrary JSON payloads that do not have a schema field.

### The validation on save

```go
if err := app.Save(record); err != nil {
    // The error describes which field failed and why.
}
```

The validation runs the field rules: `Required`, `Min`, `Max`,
`Pattern`, the select values, the JSON validity, the relation
existence.

⚠️ **`Save` runs the hooks too.** If a hook rejects the record, `Save`
returns that error. This is a feature (Chapter 12), but it means an
error from `Save` is not necessarily a schema problem.

## 9.5 Reading a record

### By id

```go
record, err := app.FindRecordById("vocab", "abc123")
if err != nil {
    if errors.Is(err, sql.ErrNoRows) {
        // not found
    }
    return err
}
```

### All records

```go
records, err := app.FindAllRecords("vocab")
```

⚠️ **This loads everything into memory.** For a table with a million
rows, it will exhaust your heap. Always use a filtered or paginated
query in production code.

### With a filter

```go
records, err := app.FindRecordsByFilter("vocab", `key = "go"`, "-created", 10, 0)
```

The signature is:

```go
FindRecordsByFilter(
    collectionModelOrIdentifier any,
    filter string,
    sort string,
    limit int,
    offset int,
    params ...dbx.Params,
) ([]*Record, error)
```

⚠️ **`limit = 0` means no limit.** The template always sets an explicit
limit.

### The first match

```go
record, err := app.FindFirstRecordByFilter("vocab", `key = "go"`)
```

⚠️ **It returns `sql.ErrNoRows` when there is no match**, not a nil
record with a nil error. The template wraps it:

```go
rec, err := s.app.FindFirstRecordByFilter(collection, filter)
if err != nil {
    if errors.Is(err, sql.ErrNoRows) {
        return nil, fmt.Errorf("%w: %s", ErrNotFound, collection)
    }
    return nil, fmt.Errorf("pbstore: find first %q: %w", collection, err)
}
```

That wrapping is what lets the caller use `errors.Is(err,
pbstore.ErrNotFound)` instead of knowing about `database/sql`.

## 9.6 Reading the field values

A record is a dynamic map. The accessors are:

```go
record.GetString("key")          // string
record.GetInt("count")           // int
record.GetInt64("count")         // int64
record.GetFloat("ratio")         // float64
record.GetBool("active")         // bool
record.GetStringSlice("tags")    // []string
record.GetDateTime("created")    // types.DateTime
record.GetGeoPoint("location")   // types.GeoPoint
record.GetRaw("payload")         // the raw value, no normalisation
record.Get("anything")           // any
```

Each accessor does a **type coercion**, so `GetString` on a number
field returns its string form.

```go
func (m *Record) GetString(key string) string {
    return cast.ToString(m.Get(key))
}
```

⚠️ **`GetString` on a number returns the number as a string.** If you
need to know the actual type, use `GetRaw` and type-assert.

### The two data sets

A record has two kinds of data:

```go
// The schema fields only.
fields := record.FieldsData()

// The custom (non-schema) fields only.
custom := record.CustomData()
```

```go
func (m *Record) FieldsData() map[string]any {
    result := make(map[string]any, len(m.collection.Fields))
    for _, f := range m.collection.Fields {
        fieldName := f.GetName()
        result[fieldName] = m.Get(fieldName)
    }
    return result
}
```

⚠️ **`PublicExport()` returns only the schema fields** (plus the
`id`, `collectionId`, `collectionName` and `expand` helpers). Custom
data is excluded unless you call `WithCustomData(true)`.

The template merges both, because it wants the JSON viewer to show
everything:

```go
// PublicExport only returns schema fields, so the raw field data is
// merged in to also expose custom (non-schema) values.
for k, v := range rec.FieldsData() {
    out.Fields[k] = v
}
for k, v := range rec.CustomData() {
    if _, ok := out.Fields[k]; !ok {
        out.Fields[k] = v
    }
}
```

## 9.7 Updating and deleting

```go
record, err := app.FindRecordById("vocab", "abc123")
if err != nil {
    return err
}

record.Set("label", "Golang")

if err := app.Save(record); err != nil {
    return err
}
```

⚠️ **`Save` on an existing record does an UPDATE, not a replace.**
Fields you do not touch keep their values. If you want to clear a
field, set it to the zero value explicitly.

### Deleting

```go
if err := app.Delete(record); err != nil {
    return err
}
```

### `SaveNoValidate`

```go
if err := app.SaveNoValidate(record); err != nil {
    return err
}
```

Skips the validation *and* the hooks. Use it only for bulk imports
where you have already validated the data yourself.

⚠️ **`SaveNoValidate` bypasses your own hooks.** If a hook enforces a
business rule, skipping it can corrupt your data.

## 9.8 The transaction

```go
err := app.RunInTransaction(func(txApp core.App) error {
    if err := txApp.Save(record1); err != nil {
        return err
    }
    if err := txApp.Save(record2); err != nil {
        return err
    }
    return nil
})
```

The `txApp` is a transactional view of the app. If the function returns
an error, everything is rolled back.

⚠️ **Inside a transaction, `txApp` is not `app`.** Using the outer
`app` inside the closure writes outside the transaction, which defeats
the purpose.

## 9.9 A complete example

Here is the whole lifecycle, which is roughly what the template's
`EnsureDictionaryCollection` does:

```go
func ensureDictionary(app core.App, name string, extra []core.Field) (*core.Collection, error) {
    // Reuse the existing collection when it is already there.
    if existing, err := app.FindCachedCollectionByNameOrId(name); err == nil && existing != nil {
        return existing, nil
    }

    collection := core.NewBaseCollection(name)
    collection.Fields.Add(
        &core.TextField{Name: "key", Required: true, Max: 200},
        &core.TextField{Name: "label", Max: 500},
        &core.TextField{Name: "description", Max: 5000},
    )
    collection.Fields.Add(extra...)

    if err := app.Save(collection); err != nil {
        return nil, fmt.Errorf("create dictionary collection %q: %w", name, err)
    }

    return collection, nil
}
```

⚠️ **The idempotency check comes first.** A function that creates a
collection should be safe to call on every startup, because that is
when you call it.

## 9.10 Inspecting the schema at runtime

A TUI that browses arbitrary collections needs to introspect the
schema:

```go
collections, err := app.FindAllCollections()
for _, c := range collections {
    fmt.Printf("%s (%s)\n", c.Name, c.Type)
    for _, f := range c.Fields {
        fmt.Printf("  %s: %s (required=%t)\n", f.GetName(), f.Type(), f.GetRequired())
    }
}
```

The `Field` interface:

```go
type Field interface {
    GetId() string
    GetName() string
    GetType() string
    GetRequired() bool
    GetHidden() bool
    GetSystem() bool
    GetOptions() map[string]any
    SetId(id string)
    SetName(name string)
    SetRequired(required bool)
    SetHidden(hidden bool)
    SetSystem(system bool)
    SetOptions(options map[string]any)
}
```

⚠️ **`GetType()` returns the field type string** ("text", "number",
"json", …). This is how you can build a generic editor that adapts to
the schema.

## 9.11 Exercises

1. Create a collection with a text field, a number field, a select
   field and a JSON field. Insert one record into each and print the
   result of `FieldsData()` and `CustomData()`.

2. Write a record with an unknown field name. Observe that it is
   stored as custom data. Read it back with `Get` and with
   `PublicExport()`. Explain the difference.

3. Try to save a record that violates the `Required` rule. Read the
   error. Then try to save one that violates a `Pattern` rule.

4. Write a function that lists all the fields of a collection with
   their types, and use it to build a generic "add record" form.

5. Write a transaction that inserts two records, and make the second
   one fail. Verify that the first one was rolled back.

6. Benchmark `FindCollectionByNameOrId` versus
   `FindCachedCollectionByNameOrId` in a loop of 10,000 iterations.

7. Add a `RelationField` to a collection and use `ExpandRecords` to
   resolve the related records.

## 9.12 What you learned

- `NewBaseCollection` / `NewAuthCollection` / `NewViewCollection`
  create the collections; `Add` the fields; `Save` validates and
  persists.
- The field types are `Text`, `Bool`, `Number`, `Date`, `Select`,
  `JSON`, `Editor`, `File`, `Relation`, `GeoPoint`, `Password`,
  `Email` and `Autodate`.
- `NewRecord` + `Load` + `Save` creates; `FindRecordById` + `Set` +
  `Save` updates.
- The accessors (`GetString`, `GetInt`, …) **coerce**; `GetRaw` does
  not.
- `FieldsData()` is the schema fields; `CustomData()` is the rest;
  `PublicExport()` is the schema fields plus the helpers.
- `Save` runs **both** the validation and the hooks.
- `FindFirstRecordByFilter` returns **`sql.ErrNoRows`** when there is
  no match — wrap it if you want a domain-neutral error.
- `SaveNoValidate` skips the validation **and** the hooks — use it
  carefully.
- Transactions take a `txApp`; using the outer `app` inside defeats
  them.
- Use **`FindCachedCollectionByNameOrId`** in hot paths.

## 9.13 Further reading

- `core/collection_model.go` — `NewBaseCollection` and the defaults.
- `core/field_*.go` — the field types.
- `core/fields_list.go` — the `FieldsList` API.
- `core/record_model.go` — `NewRecord`, `Load`, `Set`, the accessors.
- `core/record_query.go` — the query methods.
- `core/db.go` — `Save`, `Delete`, `Validate`, the transactions.
- `internal/pbstore/embedded.go` — the template's implementation of
  these operations.

Next: [Chapter 10 — Querying: The Native Filter Grammar](ch10-querying.md).
