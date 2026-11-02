# Filter JSON fields

Use this skill when a task involves filtering records by a **JSON field**,
especially when the JSON holds an array.

## The problem

The native PocketBase filter grammar reaches into the nested **objects**
of a JSON field:

```
meta.lang = "go"              // equality
meta.level > 2                // numeric comparison
meta.nested.deep.value = "x"  // any depth
```

It **cannot** reach into an **array**. The expression parser rejects
the `[` character, and an array field compared with `=` matches nothing:

```
meta.tags = "cli"       // matches nothing, silently
meta.tags[0] = "cli"    // error: unexpected character '['
```

The silent case is the dangerous one: a filter that matches nothing looks
like a filter that matched no records.

## The solution

The `hybridstore.Hybrid` wrapper routes the expressions the native grammar
cannot evaluate to a local evaluation:

```
meta.tags.#(=="cli")      // any element equals "cli"
meta.items.0.name = "x"   // the "name" of the first element
meta.items.#.qty > 5      // a numeric comparison across the elements
not (meta.lang = "go")    // a negation
```

The application already builds the hybrid in `cmd/tui/main.go`, so **you
do not have to change anything to use these expressions**.

## What to do

1. Write the expression with the array syntax.
2. Test it against a collection that holds the data.
3. Check the record count. A count of zero on a filter you expect to
   match usually means the expression still holds a native construct.

## The bound

The local evaluation fetches at most `max_local_records` records
(default 1000, flag `--max-local-records`, env `FLINT_MAX_LOCAL_RECORDS`).

When the bound is reached:

- `PageResult.Truncated` is set.
- The pagination shows `(partial)`.

Never raise the bound without checking the memory cost: the local
evaluation holds the whole fetched set in memory.

## Writing a local filter

When you need to evaluate an expression yourself — in a test, or in a
custom component — use the package directly:

```go
import "github.com/incurl/flint/internal/jsonfilter"

filter, err := jsonfilter.Parse(`meta.tags.#(=="cli")`)
if err != nil {
	return err
}

if filter.Match(document) {
	// ...
}
```

`document` is the raw JSON of the record. The path of the expression is
relative to the record itself.

To decide whether an expression needs the local evaluation:

```go
if jsonfilter.NeedsLocal(expr) {
	// evaluate it locally
}
```

## The syntax

The supported syntax mirrors the PocketBase grammar, plus the gjson
array extensions.

| Expression                | Meaning                            |
| ------------------------- | ---------------------------------- |
| `path = "v"`              | equality                           |
| `path != "v"`             | inequality                         |
| `path > 2`                | numeric comparison                 |
| `path >= 2`               | numeric comparison                 |
| `path < 2`                | numeric comparison                 |
| `path <= 2`               | numeric comparison                 |
| `path ~ "v"`              | contains                           |
| `a = "x" && b > 1`        | conjunction                        |
| `a = "x" \|\| b > 1`      | disjunction                        |
| `not (a = "x")`           | negation                           |
| `tags.#(=="v")`           | an array contains a value          |
| `items.0.name = "x"`      | an array index                     |
| `items.#.qty > 5`         | a comparison across the elements   |

An array matches when **any** of its elements matches.

## The gotchas

- **The operators need the surrounding spaces.** `meta.level>2` is not a
  comparison; `meta.level > 2` is.
- **A quoted value is not a path.** `meta.label = "a[0]b"` compares a
  string; the bracket is a literal.
- **The path is relative to the record.** `meta.lang`, not
  `fields.meta.lang`.
- **A missing path never matches**, and neither does a comparison
  against a non-numeric value with a numeric operator.

## Verifying

```bash
go test ./internal/jsonfilter/ ./internal/pbstore/
```

The `pbstore` tests compare the hybrid against the unwrapped store, so
a change that makes the two layers disagree fails the suite.
