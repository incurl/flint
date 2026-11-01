# Appendix C — The Complete Filter Grammar

A reference for the PocketBase filter syntax, as implemented by
`tools/search/filter.go`.

## The grammar

```
filter      := expression
expression  := term (("&&" | "||") term)*
term        := "(" expression ")" | condition
condition  := field operator value
operator   := "=" | "!=" | "~" | "!~" | ">" | ">=" | "<" | "<="
value      := string | number | boolean | null
```

## The operators

| Operator | Name        | Applies to | Example              |
| -------- | ----------- | ---------- | -------------------- |
| `=`      | equals      | all        | `key = "go"`         |
| `!=`     | not equals  | all        | `key != "go"`        |
| `~`      | like        | text       | `title ~ "go"`       |
| `!~`     | not like    | text       | `title !~ "go"`      |
| `>`      | greater     | number, date | `rank > 1`         |
| `>=`     | greater eq  | number, date | `rank >= 1`        |
| `<`      | less        | number, date | `rank < 10`         |
| `<=`     | less eq     | number, date | `rank <= 10`        |

## The logical operators

| Operator | Name | Example                            |
| -------- | ---- | ---------------------------------- |
| `&&`     | and  | `a = 1 && b = 2`                   |
| `\|\|`   | or   | `a = 1 \|\| b = 2`                 |

⚠️ **`&&` binds tighter than `||`.** `a = 1 || b = 2 && c = 3` parses
as `a = 1 || (b = 2 && c = 3)`. **Always use parentheses** when the
intent could be ambiguous.

## The values

### Strings

Double-quoted. The characters that must be escaped inside a string
literal:

| Character | Escape |
| --------- | ------ |
| `"`       | `\"`   |
| `\`       | `\\`   |

```go
filter := `title = "He said \"hi\""`
```

⚠️ **Prefer the placeholder syntax** over escaping by hand:

```go
// ✗ Fragile.
search.FilterData(fmt.Sprintf(`title = "%s"`, userInput)).BuildExpr(resolver)

// ✓ Safe.
search.FilterData(`title = {:title}`).BuildExpr(resolver, dbx.Params{"title": userInput})
```

### Numbers

Bare, with an optional fraction and an optional exponent.

```
42
-17
3.14
1e10
```

### Booleans

```
true
false
```

### Null

```
null
```

⚠️ **Comparing a field to `null` is not the same as "the field is
empty".** A text field with an empty string is `""`, not `null`.

## The `~` operator

`~` is SQL `LIKE`. It is **case-insensitive** and matches a
substring.

```
title ~ "go"      // contains "go", any case
title ~ "go%"     // starts with "go"
title ~ "%go"     // ends with "go"
title ~ "%go%"    // contains "go"
title ~ "g_o"     // "g", any char, "o"
```

⚠️ **`%` is a wildcard, not a regex.** For a literal `%`, escape it:

```
title ~ "100\\%"
```

⚠️ **`_` matches exactly one character**, not "any character".

⚠️ **The `~` operator only applies to text fields.** Using it on a
number field is an error:

```
invalid left operand "rank" - unknown field "rank"
```

## The field names

The field name can be:

- a plain schema field: `title`, `rank`;
- a relation field: `author`;
- the special fields: `id`, `created`, `updated`, `collectionId`,
  `collectionName`, `expand`;
- a dotted JSON path, **with the `SimpleFieldResolver` only**:
  `payload.lang`, `labels.en.value`.

### The JSON fields

A `JSONField` is queried through the record resolver, which wraps the
identifier in `json_extract`:

```sql
json_extract([[payload]], '')
```

So a top-level primitive in a JSON field compares correctly against its
SQL equivalent:

```
payload = 5
```

⚠️ **The dotted path syntax comes from the `SimpleFieldResolver`, not
from the record resolver.** The record resolver resolves `labels` as a
whole field; it does not interpret `labels.en.value`.

```go
// SimpleFieldResolver.Resolve, for a name with several parts:
parts := strings.Split(field, ".")

// single regular field
if len(parts) == 1 {
    return &ResolverResult{
        Identifier: "[[" + inflector.Columnify(parts[0]) + "]]",
    }, nil
}

// treat as json path
var jsonPath strings.Builder
jsonPath.WriteString("$")
for _, part := range parts[1:] {
    if _, err := strconv.Atoi(part); err == nil {
        jsonPath.WriteString("[")
        jsonPath.WriteString(inflector.Columnify(part))
        jsonPath.WriteString("]")
    } else {
        jsonPath.WriteString(".")
        jsonPath.WriteString(inflector.Columnify(part))
    }
}

return &ResolverResult{
    NullFallback: search.NullFallbackDisabled,
    Identifier: fmt.Sprintf(
        "JSON_EXTRACT([[%s]], '%s')",
        inflector.Columnify(parts[0]),
        jsonPath.String(),
    ),
}, nil
```

So `labels.en.value` becomes:

```sql
JSON_EXTRACT([[labels]], '$.en.value')
```

And `payload.tags.0` becomes:

```sql
JSON_EXTRACT([[payload]], '$.tags[0]')
```

So the dotted form requires a resolver that understands it:

```go
resolver := search.NewSimpleFieldResolver("labels.en.value", "title")
```

⚠️ **In practice, materialise the value into a schema field.** A JSON
path filter compiles to a `json_extract` on every row: it cannot use an
index, and it is opaque to the query planner. A `label_en` column with
an index is faster, and it works with every resolver.

⚠️ **A `TextField` that happens to contain JSON is treated as a plain
string.** The `json_extract` wrapping only applies to the fields
declared as `JSONField`.

### The relation fields

A relation field can be filtered by the related record's id:

```
author = "abc123"
```

And the relation's *own* fields can be reached through the `expand`
mechanism, not through the filter.

## The sort syntax

```
-created       descending by "created"
name           ascending by "name"
-created,name  descending by "created", then ascending by "name"
```

The prefix `-` means descending. The special fields `@random` and
`@rowid` are supported:

```
@random    a random order
@rowid     the SQLite rowid order
```

⚠️ **An invalid sort field is not caught at parse time.** The
`ParseSortFromString` function never fails; the error appears when the
provider builds the expression.

## The limits

| Limit                        | Value | Constant                          |
| ---------------------------- | ----- | --------------------------------- |
| The maximum filter length    | 3500  | `search.MaxFilterLength`          |
| The maximum sort field length| 255   | `search.MaxSortFieldLength`       |
| The maximum filter expressions | 200 | `search.DefaultFilterExprLimit`   |
| The maximum sort expressions   | 8   | `search.DefaultSortExprLimit`     |
| The maximum per page          | 1000  | `search.MaxPerPage`               |
| The default per page          | 30    | `search.DefaultPerPage`           |

## The examples

### The common patterns

```
key = "go"                        exact match
key != "go"                       not equal
title ~ "go"                      contains
title ~ "go%"                     starts with
title ~ "%go"                     ends with
rank > 1                          greater than
rank >= 1 && rank <= 10           between
rank > 1 || rank < 0              outside the range
active = true                     boolean
deleted = null                    null check
type = "term" && active = true    conjunction
(type = "term" || type = "prop") && active = true    with parentheses
```

### The pagination

```
page=1&perPage=50&filter=key%20%3D%20%22go%22&sort=-created
```

### The expansion

```
expand=author,editor
```

## The error messages

| Message                                                | Meaning                                   |
| ------------------------------------------------------ | ----------------------------------------- |
| `failed to resolve field "x"`                          | The field is not allowed by the resolver  |
| `invalid left operand "x" - unknown field "x"`          | The field does not exist in the schema    |
| `invalid right operand "x" - unknown field "x"`         | The same, on the right-hand side          |
| `expected ~ and not ~ operator`                        | A malformed `~` expression                |
| `invalid filter expression`                            | A general syntax error                    |
| `max filter expressions limit reached`                  | Too many `&&`/`||` terms                  |

## The implementation notes

### The placeholder substitution

The `{:name}` placeholders are replaced *before* the parsing:

```go
func (f FilterData) BuildExpr(
    fieldResolver FieldResolver,
    placeholderReplacements ...dbx.Params,
) (dbx.Expression, error)
```

The replacement is done with a string replacer, and the values are
quoted appropriately for their type:

- `nil` → `null`
- `bool`, `float64`, `int`, … → the bare literal
- anything else → `strconv.Quote(casted)`

⚠️ **A string value is quoted with `strconv.Quote`**, which uses Go
escaping. For a value containing a newline, the result is `"a\nb"` —
which the fexpr parser then reads as a literal newline. This is
correct, but worth knowing.

### The parse cache

The parsed expressions are cached:

```go
var parsedFilterData = store.New(make(map[string][]fexpr.ExprGroup, 50))
```

The cache key is the raw filter string plus the limit. It is bounded
by `SetIfLessThanLimit(..., 500)`.

⚠️ **This is a good pattern to imitate** for any cache of
user-derived values.

### The expression limit

The number of expressions is limited to prevent a filter from
generating an enormous SQL statement. The default is 200.

## The security notes

### The injection defence

The filter string is *not* SQL. It is parsed into an expression tree
and then compiled into SQL with proper quoting. The column identifiers
go through the resolver, which only allows the known fields.

So the filter language is safe against injection *by construction* —
as long as you use it as intended.

⚠️ **The danger is interpolating user input into the filter string
before the parser sees it.** If the user can inject a `"` they can
terminate the string literal and inject arbitrary filter expressions:

```
key = "x" OR 1=1 --
```

With the placeholder syntax, this is impossible:

```go
search.FilterData(`key = {:key}`).BuildExpr(resolver, dbx.Params{"key": userInput})
```

### The column allowlist

The resolver restricts which fields can be used:

```go
search.NewSimpleFieldResolver("title", "rank")
```

A user cannot filter on a hidden field or on an internal column.

⚠️ **The record resolver allows the schema fields plus the JSON paths
plus the `@request.*` pseudo-fields.** The `@request.*` ones are only
available in the REST API (they need a request context); in an
embedded application they are not.

## The quick reference

```
=    equals           !=   not equals
~    like             !~   not like
>    greater          >=   greater or equal
<    less             <=   less or equal
&&   and              ||   or
( )  grouping         " "  string literal
null                 the null value
```

```go
// The quick way.
result, err := store.List(ctx, pbstore.Query{
    Collection: "items",
    Filter:     `title ~ "go" && active = true`,
    Sort:       "-created",
    Page:       1,
    PerPage:    30,
})
```
