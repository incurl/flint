# The JavaScript record renderer

Use this skill when a task involves **how a record is displayed**, and
the change should not need a Go rebuild: the user wants their own
titles, summaries or field rows.

## What it is

`RecordRenderer` is the template's main extension point. It drives the
record table, the record card and the search results at once.

The JavaScript renderer **overrides** it: when the script returns a
value, that value is used. The Go renderer is the fallback, so a
missing or broken script degrades to the default rendering.

This is the one place where JavaScript extends the **TUI**. The
PocketBase hooks in `pb_hooks/` extend the **data layer** and can run on
a server; the renderer is local to the application.

## The file

```
<data-dir>/../flint_hooks/1_render.pb.js
```

The files are concatenated in name order, so a project can split the
script. Only files ending in `.pb.js` are read.

## The contract

The script defines exactly one function:

```js
function flintRender(record) {
    return {
        title:   record.label || record.id,
        summary: record.description || "",
        fields: [
            { name: "Key",  value: String(record.key) },
            { name: "Lang", value: String(record.lang) },
        ],
    }
}
```

`record` carries the record's fields, plus the system fields:

| Property         | Meaning                          |
| ---------------- | -------------------------------- |
| `record.fields`  | The raw field map                |
| `record.id`      | The record id                    |
| `record.collectionName` | The collection name       |
| `record.created` | The creation timestamp           |
| `record.updated` | The update timestamp             |

Each field row is `{ name, value }`. A plain `"name: value"` string is
also accepted.

## The rule that must not be broken

**The script never runs while a frame is drawn.**

`Title`, `Summary` and `Fields` are called from `View()`. Evaluating
the script there would block the render loop for as long as the script
takes — every keystroke.

So the script runs only in `Preload`, which the pages call from the
commands that fetch the records:

```go
// internal/pages/records.go, inside the load command
components.Preload(p.renderer, result.Records)
```

The render methods are pure cache lookups. A record that is not cached
falls back to the Go renderer, which is why the **first frame of a new
record shows the Go rendering** and the next frame shows the script
rendering. That is intended, not a bug.

If you add a new place that fetches records, call `components.Preload`
there too, from the command — never from `Update` or `View`.

## Enabling it

```bash
flint --mode embedded --data-dir /var/lib/flint/pb_data --js-renderer
```

or `js_renderer = true` in the config file, or `FLINT_JS_RENDERER=1`.
The directory is set with `--renderer-dir`, `FLINT_RENDERER_DIR` or
`renderer_dir`.

## A broken script fails at startup

The script is compiled when the application starts:

```
flint: the renderer script "/var/lib/flint/flint_hooks": components:
evaluate the script: SyntaxError: Line 4:1 Unexpected end of input
```

The application exits with status 1. That is deliberate: a typo in the
renderer must be reported once, not hidden behind a silent fallback.

A script that **compiles but throws at runtime** is different: the
record falls back to the Go rendering and the application keeps
running.

## The timeout

A single evaluation is bounded by `components.ScriptTimeout` (2
seconds). A script that loops forever is interrupted, and the record
falls back to the Go renderer.

The interrupt is real: `Preload` calls `vm.Interrupt`, so the runtime
is not left mid-execution holding the lock.

## What the script cannot do

**It cannot reach the database.** The script runs in its own goja
runtime, not the PocketBase one: there is no `$app`, no `$dbx`, no
`$os`. That is the main safety property — a renderer is display logic,
and it must not be able to write.

If a task needs to read related records, do it in Go and put the result
in the record's fields before the preload runs.

## Testing

```bash
go test ./internal/components/ ./config/
```

The interesting tests:

- `TestScriptRendererUsesTheCurrentRecord` — guards the regression
  where the record was bound once, so every record rendered as the
  first one.
- `TestScriptRendererIsNotEvaluatedInTheRenderPath` — guards the
  regression where the script ran in `View()`.
- `TestScriptRendererTimesOut` — the interrupt actually stops the
  script.
- `TestCachedRendererReRendersAChangedRecord` — the cache is keyed by
  the record's update time.

## The gotchas

- **The cache is keyed by `id` + `updated`.** A record that changes is
  rendered again; a record that does not is not.
- **`Invalidate` and `InvalidateAll`** drop the cache, for a page that
  knows a record changed without a new timestamp.
- **The fallback is always available.** Never make the renderer return
  an empty string: `View()` would render a blank row.
- **Do not put `flint_hooks/` in `pb_hooks/`.** The data hooks run on a
  server; the renderer does not. Keeping them apart keeps the
  portability honest.
