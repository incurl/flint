# Chapter 10 — Querying: The Native Filter Grammar

## 10.1 Why the native grammar matters

A template for data browsers faces a choice:

1. **Invent a query language.** `title contains go`, `rank > 5`,
   `type = term`. Nice to design, but you must implement the parser,
   the operator set, the escaping, the error messages, the pagination
   semantics, and the field resolution. And then you must implement it
   *twice* if you support two backends.

2. **Reuse an existing grammar.** PocketBase already has one: the
   filter syntax used by its REST API, its admin UI, and its SDKs. It
   is implemented by the `tools/search` package, it is battle-tested,
   and it is documented.

The Flint template takes option 2, and it is the single most important
design decision in the data layer:

> **The filter grammar is PocketBase's. Not ours.**

The consequence is that a filter typed in the TUI means exactly the
same thing as the same filter typed into the PocketBase dashboard, and
the embedded and the remote backends cannot disagree about it.

## 10.2 The grammar

```
filter      := expression
expression  := term (("&&" | "||") term)*
term        := "(" expression ")" | condition
condition  := field operator value
operator   := "=" | "!=" | "~" | "!~" | ">" | ">=" | "<" | "<="
value      := string | number | boolean | null
```

### The operators

| Operator | Meaning                | Example                     |
| -------- | ---------------------- | --------------------------- |
| `=`      | equals                 | `key = "go"`                |
| `!=`     | not equals             | `key != "go"`               |
| `~`      | like (case insensitive)| `title ~ "go"`              |
| `!~`     | not like               | `title !~ "go"`             |
| `>`      | greater than           | `rank > 1`                  |
| `>=`     | greater or equal       | `rank >= 1`                 |
| `<`      | less than              | `rank < 10`                 |
| `<=`     | less or equal          | `rank <= 10`                |

### The logical operators

`&&` (and) and `||` (or), with parentheses for grouping:

```
rank > 1 && rank < 10
type = "term" || type = "property"
(active = true) && (rank > 1 || rank < 0)
```

### The values

- **Strings** are double-quoted: `key = "go"`.
- **Numbers** are bare: `rank = 42`, `ratio > 0.5`.
- **Booleans** are `true` / `false`.
- **Null** is `null`.

### The `~` operator

`~` is a "like" match. It is case-insensitive and matches a
substring:

```
title ~ "go"        // contains "go", any case
title ~ "go%"       // starts with "go"
title ~ "%go"       // ends with "go"
```

⚠️ **The `%` is a wildcard, not a regex.** `~` is SQL `LIKE`, so `%`
means "any sequence" and `_` means "any single character".

⚠️ **The `~` operator only applies to text fields.** Applying it to a
number field is an error:

```
"invalid left operand \"rank\" - unknown field \"rank\""
```

That error is what the template's test caught when it tried to filter
on a field that was not in the schema.

## 10.3 The `FilterData` type

The filter grammar is implemented by `search.FilterData`, which is a
`string` type with a `BuildExpr` method:

```go
type FilterData string

func (f FilterData) BuildExpr(
    fieldResolver FieldResolver,
    placeholderReplacements ...dbx.Params,
) (dbx.Expression, error)
```

```go
import "github.com/pocketbase/pocketbase/tools/search"

var filter search.FilterData = `title = {:title}`

expr, err := filter.BuildExpr(
    search.NewSimpleFieldResolver("title"),
    dbx.Params{"title": "Go"},
)
```

The result is a `dbx.Expression` — a native SQL WHERE clause that you
can attach to any query:

```go
query := app.RecordQuery("items")
query.AndWhere(expr)
```

### The placeholder syntax

The filter string can contain `{:name}` placeholders, which are
replaced with properly quoted values:

```go
// This is safe:
search.FilterData(`name = {:name}`).BuildExpr(resolver, dbx.Params{"name": userInput})

// This is NOT:
search.FilterData(fmt.Sprintf(`name = "%s"`, userInput)).BuildExpr(resolver)
```

⚠️ **Never interpolate user input into a filter string.** The
placeholder mechanism handles the quoting and escaping. If you build
the string by hand, a filter like `key = "x" OR 1=1 --` becomes an
injection.

The template's helper does this correctly:

```go
// FilterExpr builds a native PocketBase WHERE expression out of the
// provided filter string.
func FilterExpr(filter string, resolver search.FieldResolver, params ...dbx.Params) (dbx.Expression, error) {
    if filter == "" {
        return nil, nil
    }
    if resolver == nil {
        resolver = search.NewSimpleFieldResolver(`^\w+[\w\.]*$`)
    }
    expr, err := search.FilterData(filter).BuildExpr(resolver, params...)
    if err != nil {
        return nil, fmt.Errorf("%w: %v", ErrInvalidFilter, err)
    }
    return expr, nil
}
```

Note the `ErrInvalidFilter` wrapping: the caller can use `errors.Is`
instead of string-matching the PocketBase error.

## 10.4 The field resolver

The resolver decides which field names are legal and how they map to
SQL columns.

### The simple resolver

```go
resolver := search.NewSimpleFieldResolver("name", "rank", "status")
```

Only the listed fields are allowed. Anything else is an error:

```
failed to resolve field "unknown"
```

### The regex form

```go
// Allow any word-character field name.
resolver := search.NewSimpleFieldResolver(`^\w+[\w\.]*$`)
```

The arguments can be plain names or regular expressions:

```go
// From the PocketBase source:
func NewSimpleFieldResolver(allowedFields ...string) *SimpleFieldResolver
```

⚠️ **The default resolver in the template allows any field name.** That
is convenient for a generic browser, but it means a user can filter on
a field that does not exist and get a runtime error instead of a
friendly message. For a fixed-schema application, list the fields
explicitly.

### The record field resolver

PocketBase has a richer resolver for records:

```go
resolver := core.NewRecordFieldResolver(app, collection, requestInfo, allowHiddenFields)
```

This one understands:
- the schema fields;
- the relation fields;
- the `@request.*` pseudo-fields.

For a field declared as `JSONField`, it wraps the identifier in
`json_extract` so that a top-level JSON primitive compares correctly
against its SQL equivalent:

```go
// wrap in json_extract to ensure that top-level primitives
// stored as json work correctly when compared to their SQL equivalent
if field.Type() == FieldTypeJSON {
    result.NullFallback = search.NullFallbackDisabled
    result.Identifier = dbutils.JSONExtract(r.activeTableAlias+"."+cleanFieldName, "")
}
```

⚠️ **The *dotted path* syntax (`labels.en.value`) comes from the
`SimpleFieldResolver`, not from the record resolver.** The record
resolver resolves `labels` as a whole field. The dotted form is the
`SimpleFieldResolver`'s interpretation of a name with several dots:

```go
// SimpleFieldResolver.Resolve, for a name with several parts:
entries, ok := entries[r.language].(map[string]any)
```

So:

| You want to filter…                     | Use                                        |
| ---------------------------------------- | ------------------------------------------ |
| A JSON field as a whole (`payload = 5`) | The record resolver                        |
| Inside a JSON field (`labels.en.value`)  | The `SimpleFieldResolver`, or a dedicated field |

⚠️ **The dotted form only works with the `SimpleFieldResolver`.** The
template's embedded `List` uses the *record* resolver, so
`labels.en.value` will **not** resolve there — it will report an
unknown field.

If you need to query inside a JSON field, the practical options are:

1. **Materialise the value into a schema field** (the `label_en`
   column from Chapter 24) — the fastest and the most reliable;
2. **Use the `SimpleFieldResolver`** with an explicit field list, and
   accept that you lose the relation resolution;
3. **Add a computed field** to the collection that extracts the value.

**Materialising is usually the right answer.** A JSON path filter is
a `json_extract` on every row — it cannot use an index, and it is
opaque to the query planner.

```go
// Resolve parses the provided field and returns a properly formatted db
// identifier (eg. NULL, quoted column, placeholder parameter, etc).
func (r *SimpleFieldResolver) Resolve(field string) (*ResolverResult, error)
```

For a JSON field, the resolver produces:

```sql
JSON_EXTRACT([[payload]], '$.en.value')
```

which is how you filter *inside* a JSON document:

```
payload.en.value = "Douglas Adams"
```

⚠️ **This only works for fields declared as `JSONField`.** For a
`TextField` that happens to contain JSON, the resolver treats it as a
plain string.

The template uses the record resolver for the listings, because it
gives the full native behaviour:

```go
resolver := core.NewRecordFieldResolver(s.app, collection, nil, true)
```

⚠️ **The `requestInfo` parameter is `nil`.** That means the
`@request.*` pseudo-fields (which depend on the HTTP request) are not
available — which is correct for an embedded application, where there
is no HTTP request.

## 10.5 The search provider

The provider ties the filter, the sort and the pagination together. It
is what the REST API uses internally.

```go
provider := search.NewProvider(resolver).Query(query)

provider.Page(2)
provider.PerPage(50)
provider.AddFilter(search.FilterData(`key = "go"`))
provider.AddSort(search.SortField{Name: "created", Direction: search.SortDesc})
provider.SkipTotal(false)

records := []*core.Record{}
result, err := provider.Exec(&records)
```

### The result

```go
type Result struct {
    Items      any `json:"items"`
    Page       int `json:"page"`
    PerPage    int `json:"perPage"`
    TotalItems int `json:"totalItems"`
    TotalPages int `json:"totalPages"`
}
```

`TotalItems` comes from a separate `COUNT(DISTINCT ...)` query, run
**concurrently** with the page query:

```go
if !s.skipTotal {
    // execute the 2 queries concurrently
    errg := new(errgroup.Group)
    errg.Go(routine.SafeWrap(countExec))
    errg.Go(routine.SafeWrap(modelsExec))
    if err := errg.Wait(); err != nil {
        return nil, err
    }
}
```

⚠️ **`SkipTotal` is a real performance lever.** It removes the count
query entirely. For an infinite-scroll list where you never show the
total, use it.

### The URL parameter parsing

The provider can parse the native URL query string:

```go
if err := provider.Parse(urlQuery); err != nil {
    return nil, err
}
```

where `urlQuery` is the raw query string:

```
page=2&perPage=50&filter=key%20%3D%20%22go%22&sort=-created
```

This parses:
- `page` → `Page()`
- `perPage` → `PerPage()`
- `filter` → `AddFilter()`
- `sort` → `AddSort()`
- `skipTotal` → `SkipTotal()`

⚠️ **This is how the template guarantees the two backends agree.**
The embedded backend parses the *same* URL parameters that the remote
backend sends over the wire:

```go
// Embedded.
provider := search.NewProvider(resolver).Query(query)
if err := provider.Parse(QueryParams(q).Encode()); err != nil {
    return nil, fmt.Errorf("%w: %v", ErrInvalidFilter, err)
}

// Remote.
endpoint := SearchURL(q.Collection) + "?" + QueryParams(q).Encode()
```

Both go through `QueryParams`:

```go
func QueryParams(q Query) url.Values {
    values := url.Values{}

    if q.Filter != "" {
        values.Set(search.FilterQueryParam, q.Filter)
    }
    if q.Sort != "" {
        values.Set(search.SortQueryParam, q.Sort)
    }
    if q.Expand != "" {
        values.Set("expand", q.Expand)
    }
    if q.Fields != "" {
        values.Set("fields", q.Fields)
    }
    if q.SkipTotal {
        values.Set(search.SkipTotalQueryParam, "true")
    }

    values.Set(search.PageQueryParam, strconv.Itoa(NormalizePage(q.Page)))
    values.Set(search.PerPageQueryParam, strconv.Itoa(NormalizePerPage(q.PerPage)))

    return values
}
```

⚠️ **Note the normalisation.** `NormalizePage` clamps to ≥ 1 and
`NormalizePerPage` clamps to the PocketBase range (1..1000). Doing it
in one place means the two backends cannot disagree about what
"perPage=0" means.

## 10.6 Sorting

The sort syntax is the PocketBase one:

```
-created      → descending by "created"
name          → ascending by "name"
-created,name → descending by "created", then ascending by "name"
```

```go
fields := search.ParseSortFromString("-created,name")
// fields[0] = {Name: "created", Direction: search.SortDesc}
// fields[1] = {Name: "name", Direction: search.SortAsc}
```

The template exposes this:

```go
// SortFields parses the provided sort expression into native PocketBase
// [search.SortField] values.
func SortFields(sort string) []search.SortField {
    if sort == "" {
        return nil
    }
    return search.ParseSortFromString(sort)
}
```

⚠️ **The `ParseSortFromString` function never returns an error.** An
invalid field name is caught later, when the provider builds the
expression:

```go
expr, err := sortField.BuildExpr(fieldResolver)
if err != nil {
    return "", fmt.Errorf("invalid sort field %q", s.Name)
}
```

So a bad sort field produces an error at execution time, not at parse
time. The template's `ErrInvalidSort` sentinel is reserved for this.

## 10.7 The complete embedded listing

Here is the template's `List` method, which is the heart of the data
layer:

```go
func (s *Embedded) List(ctx context.Context, q Query) (*PageResult, error) {
    if err := ctx.Err(); err != nil {
        return nil, err
    }
    if s.isClosed() {
        return nil, ErrClosed
    }
    if q.Collection == "" {
        return nil, errors.New("pbstore: collection is required")
    }
    if !s.isAllowed(q.Collection) {
        return nil, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, q.Collection)
    }

    collection, err := s.app.FindCachedCollectionByNameOrId(q.Collection)
    if err != nil {
        return nil, fmt.Errorf("pbstore: resolve collection %q: %w", q.Collection, err)
    }

    resolver := core.NewRecordFieldResolver(s.app, collection, nil, true)

    query := s.app.RecordQuery(collection)
    provider := search.NewProvider(resolver).Query(query)

    // Reuse the native URL parameter parsing so that the embedded backend
    // and the remote backend interpret the very same query strings.
    if err := provider.Parse(QueryParams(q).Encode()); err != nil {
        return nil, fmt.Errorf("%w: %v", ErrInvalidFilter, err)
    }
    provider.SkipTotal(q.SkipTotal)

    records := []*core.Record{}
    result, err := provider.Exec(&records)
    if err != nil {
        return nil, fmt.Errorf("pbstore: list %q: %w", q.Collection, err)
    }

    // Resolve the relation expansions.
    if expands := ExpandList(q.Expand); len(expands) > 0 {
        if expandErrs := s.app.ExpandRecords(records, expands, nil); err != nil {
            return nil, fmt.Errorf("pbstore: expand %q: %w", q.Collection, err)
        }
    }

    out := &PageResult{
        Records:    make([]*Record, 0, len(records)),
        Page:       result.Page,
        PerPage:    result.PerPage,
        TotalItems: result.TotalItems,
        TotalPages: result.TotalPages,
    }
    for _, rec := range records {
        out.Records = append(out.Records, recordFromCore(rec))
    }
    return out, nil
}
```

Read it as a sequence of decisions:

1. **Validate the inputs.** The context, the closed flag, the
   collection, the allowlist.
2. **Resolve the collection** (cached).
3. **Build the resolver** — the record resolver, so JSON paths work.
4. **Build the query** — `RecordQuery` sets up the table, the joins and
   the record scanning.
5. **Parse the parameters** — the *same* parameters the remote backend
   sends.
6. **Execute** — the provider applies the filter, the sort, the limit
   and the offset.
7. **Expand** the relations.
8. **Convert** to the backend-agnostic view.

## 10.8 Expanding relations

`Expand` resolves the relation fields:

```go
result, err := store.List(ctx, pbstore.Query{
    Collection: "articles",
    Expand:     "author,editor",
})
```

The expanded data lands in `record.Expand`:

```go
record.Expand["author"]   // map[string]any with the related record
```

⚠️ **`ExpandRecords` returns a map of errors, not an error.** Each
relation can fail independently:

```go
func (app *BaseApp) ExpandRecords(records []*Record, expands []string, optFetchFunc ExpandFetchFunc) map[string]error
```

The template joins them:

```go
if expandErrs := s.app.ExpandRecords(records, expands, nil); len(expandErrs) > 0 {
    return nil, fmt.Errorf("pbstore: expand %q: %w", q.Collection, joinErrors(expandErrs))
}
```

## 10.9 The count query

```go
func (s *Embedded) Count(ctx context.Context, collection, filter string) (int, error) {
    result, err := s.List(ctx, Query{
        Collection: collection,
        Filter:     filter,
        PerPage:    1,
    })
    if err != nil {
        return 0, err
    }
    return result.TotalItems, nil
}
```

⚠️ **This is a slightly wasteful implementation.** It fetches one
record and throws it away, just to get the count. It is correct and
simple, which is the right trade-off for a template — but for a hot
path, write the count query directly.

## 10.10 Errors from queries

The error taxonomy you should expose:

```go
var (
    ErrNotFound           = errors.New("pbstore: not found")
    ErrCollectionNotAllowed = errors.New("pbstore: collection is not allowed")
    ErrInvalidFilter      = errors.New("pbstore: invalid filter")
    ErrInvalidSort        = errors.New("pbstore: invalid sort")
    ErrClosed             = errors.New("pbstore: store is closed")
    ErrNotConnected       = errors.New("pbstore: not connected")
)
```

And always wrap the underlying error so that `errors.Is` works and
the message keeps the context:

```go
return nil, fmt.Errorf("pbstore: list %q: %w", q.Collection, err)
```

⚠️ **Use `%w`, not `%v`.** With `%v` the chain is broken and
`errors.Is(err, sql.ErrNoRows)` stops working.

## 10.11 The filter grammar in the UI

Now the payoff: exposing the native grammar to the user.

```go
// internal/pages/records.go
case "/":
    p.filterFocused = true
    p.filter.Focus()
    p.filter.SetValue(p.runtime.Filter)
    return p, nil

case "enter":
    p.filterFocused = false
    p.filter.Blur()
    return p, tea.Batch(
        func() tea.Msg { return RecordsFilterMsg{Filter: p.filter.Value()} },
        p.Load(),
    )
```

And the placeholder teaches the syntax:

```go
filter.Placeholder = "filter, eg. title ~ \"go\""
```

### Reporting a filter error

```go
case pages.RecordsMsg:
    if msg.Err != nil {
        m.runtime.SetError(msg.Err)
        return m, m.toast.Show(components.ToastError, msg.Err.Error())
    }
```

The PocketBase error is reasonably informative:

```
pbstore: list "items": invalid left operand "rank" - unknown field "rank"
```

⚠️ **Do not show the raw error without context.** Prefix it with what
the user was trying to do:

```go
text := fmt.Sprintf("invalid filter: %v", err)
```

## 10.12 Exercises

1. Write a program that lists the records matching each of these
   filters, and explain the result:
   - `key = "go"`
   - `key ~ "g"`
   - `key ~ "g%"`
   - `rank > 1 && rank < 10`
   - `rank > 1 || label = "Zig"`

2. Write a filter with a syntax error. Capture the error and turn it
   into a user-friendly message.

3. Use the placeholder syntax to filter with a value that contains a
   double quote. Verify the escaping is correct. Then try to break it
   with a value like `x" OR 1=1 --`.

4. Time a listing of 10,000 records with and without `SkipTotal`.

5. Filter *inside* a JSON field using a path like `payload.lang = "en"`.

6. Write a program that uses the `search.Provider` directly, with a
   custom `FieldResolver` that maps `title` to a SQL expression of your
   choosing.

7. Implement the `Count` method with a direct `dbx` count query
   instead of the `List` trick. Measure the difference.

## 10.13 What you learned

- PocketBase's **filter grammar is the native one**, and reusing it is
  the central design decision of the data layer.
- The operators are `=`, `!=`, `~`, `!~`, `>`, `>=`, `<`, `<=`, with
  `&&` and `||` and parentheses.
- `~` is SQL `LIKE`: case-insensitive substring, with `%` and `_`
  wildcards.
- `search.FilterData.BuildExpr` compiles a filter into a **native
  `dbx.Expression`**.
- The **`{:param}` placeholders** are the defence against injection —
  never interpolate user input into a filter string.
- The **field resolver** decides which fields are legal and how they
  map to columns; the record resolver also handles JSON paths.
- The **`search.Provider`** ties filter, sort and pagination together,
  and can parse the native URL parameters — which is how the two
  backends are guaranteed to agree.
- **`SkipTotal`** removes the count query.
- Errors must be **wrapped with `%w`** so that `errors.Is` works.

## 10.14 Further reading

- `tools/search/filter.go` — the grammar implementation.
- `tools/search/provider.go` — the filter/sort/pagination engine.
- `tools/search/sort.go` — the sort syntax.
- `tools/search/simple_field_resolver.go` — the resolver.
- `core/record_field_resolver.go` — the record resolver.
- `internal/pbstore/queries.go` — the template's parameter builders.
- `internal/pbstore/embedded.go` — the `List` implementation.

Next: [Chapter 11 — App.Store(): What It Really Is](ch11-app-store.md).
