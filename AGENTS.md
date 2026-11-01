# AGENTS.md

Guidance for AI coding agents working in this repository.

Flint is a **Go template** for terminal applications: Bubble Tea + Bubbles +
Lip Gloss on the front, PocketBase on the back. It runs either with an
embedded PocketBase (SQLite, in-process) or against a remote PocketBase
server over REST, behind one interface.

**Read this file first. Then read the task-specific skill in
`docs/agents/`.** The skills are short recipes; this file is the map.

> **New here?** If you are starting a *new project* from this template,
> read [`docs/agents/start-a-project.md`](docs/agents/start-a-project.md)
> first — it covers the rename, what to delete, and what to keep.

---

## 1. Orientation

### The five packages that matter

| Package          | Responsibility                                          | Rule of thumb                                  |
| ---------------- | ------------------------------------------------------- | ---------------------------------------------- |
| `internal/pbstore` | The data layer. `Store` interface + embedded/remote impls. | Never branch on the backend. Use `Store`.     |
| `internal/app`   | Root Bubble Tea model: routing, global keys, status bar, toasts. | Owns global concerns only.                |
| `internal/pages` | One file per screen. Self-contained.                    | A page never imports `internal/app`.           |
| `internal/components` | Reusable widgets: record card, JSON viewer/editor, toast, status bar, pagination. | Generic, no domain knowledge.      |
| `internal/state` | `Runtime`: volatile shared state (active page, health, sync). | Shared across pages; not persisted.   |

`internal/theme` holds the lipgloss palette. `config` loads settings
(flags > env > TOML). `cmd/tui` wires it all together.

### The dependency rule (do not break this)

```
cmd/tui → app → pages → components → theme
                 ↘         ↘
                   state   pbstore
```

- `pages` **must not** import `app` — that is an import cycle. Pages
  request navigation by emitting `pages.NavigateMsg` / `pages.OpenRecordMsg`,
  which the root model handles.
- `pbstore` must not import anything from `internal/`. It is a standalone
  data layer.
- `components` must not import `pages` or `app`.

If you find yourself wanting to import `app` from `pages`, you need a
message instead. See skill `add-a-page.md`.

### The message flow

```
keypress → app.Model.Update
              ├─ global key layer (ctrl+c, q, 1-6 digits)
              └─ updateActivePage(msg) → the active page's Update
                                            ├─ may return a tea.Cmd
                                            └─ the cmd runs off the render loop
                                                        ↓
                                              a Msg comes back
                                                        ↓
                                              app.Model.Update again
```

**Commands never return errors.** They return a message carrying an
`Err` field. `Update` never performs I/O — that is the single most
important rule of Bubble Tea and the one most often broken.

---

## 2. Non-negotiable invariants

Break one of these and you have a bug, even if the tests pass.

1. **`Update` never does I/O.** No store calls, no HTTP, no file reads, no
   `fmt.Println`. All of it goes in a `tea.Cmd` closure.
2. **`View` is pure.** It renders state and returns a string. It never
   mutates the model and never blocks.
3. **Commands capture values, not the model.** A command closure must not
   hold a pointer to the model, because the model mutates while the
   command runs. Capture the store, the collection, the filter — the
   immutable inputs.
4. **Every page owns a `Refresh()`** and the root model calls it on
   navigation. A page that shows data without a `Refresh` shows nothing.
5. **The stale-response guard.** Every list-style message carries the
   page number it was requested for, and the handler compares it to the
   current page before applying the result. See
   `docs/agents/REFERENCE.md` § *Stale response guard*.
6. **Store calls take a `context.Context`** with a timeout, and they
   return `pbstore` sentinel errors (`ErrNotFound`, `ErrInvalidFilter`,
   …) so callers can use `errors.Is`.
7. **Style comes from the theme.** Never build an inline `lipgloss.Style`
   in a page or a component. Add a field to `theme.Theme` instead.
8. **`App.Store()` is not a database.** It is PocketBase's volatile
   runtime key/value cache. Persistent dictionaries and lookup caches are
   ordinary PocketBase collections.

---

## 3. Common tasks → skill

Each file in `docs/agents/` is a self-contained recipe with a verified
recipe, the files to touch, and the checks to run.

| Skill                                                | Use it when                                            |
| ---------------------------------------------------- | ------------------------------------------------------ |
| [`start-a-project.md`](docs/agents/start-a-project.md) | You are turning the template into your own app.     |
| [`add-a-page.md`](docs/agents/add-a-page.md)         | You need a new screen.                                  |
| [`add-a-collection.md`](docs/agents/add-a-collection.md) | You need a new PocketBase collection + migration.    |
| [`add-a-record-renderer.md`](docs/agents/add-a-record-renderer.md) | You want to change how records are displayed. |
| [`add-a-command.md`](docs/agents/add-a-command.md)   | You want a key action or a background operation.        |
| [`add-a-config-option.md`](docs/agents/add-a-config-option.md) | You need a new setting.                        |
| [`add-a-sync.md`](docs/agents/add-a-sync.md)         | You want to pull remote data into the local cache.      |
| [`debug-a-tui.md`](docs/agents/debug-a-tui.md)       | The UI is frozen, blank, or unresponsive.                |
| [`verify.md`](docs/agents/verify.md)                 | Before you commit. Run this.                            |

[`REFERENCE.md`](docs/agents/REFERENCE.md) is the exact API surface —
every exported symbol, its signature, and where it lives. Use it instead
of grepping.

---

## 4. The verification loop

**Run this after every change. Do not skip it — CI runs the same checks
and will fail the build.**

```bash
make ci          # fmt-check + vet + test
make lint        # golangci-lint (must report 0 issues)
make test-race   # the race detector
```

Or directly:

```bash
gofmt -l ./cmd ./config ./internal   # must print nothing
go vet ./...
go test ./...
```

The lint configuration is strict on purpose: `errorlint`, `gocritic`,
`misspell` (UK locale), `unparam`, `unused`, `wsl_v5`, `godot`, `revive`.
Three rules catch most mistakes:

- **`errorlint`** — `fmt.Errorf("%w: %v", a, b)` is an error. Use two
  `%w` verbs to keep both errors in the chain: `fmt.Errorf("%w: %w", a, b)`.
- **`wsl_v5`** — requires a blank line between statement blocks. Run
  `golangci-lint run --fix` to apply it automatically.
- **`godot`** — every declaration's doc comment ends with a period.

### Test expectations

- New behaviour needs a test. A test that asserts the bug is fixed must
  be checked against the *unfixed* code to confirm it actually fails.
- Use `t.TempDir()` for anything touching the filesystem.
- The store is safe for concurrent use; page models are not. Do not
  share a page across goroutines.

---

## 5. Things that will trip you up

- **`tea.ErrInterrupted`, not a custom sentinel.** On `ctrl+c`,
  `Program.Run` returns `tea.ErrInterrupted`. If you invent your own
  `errInterrupted`, the check never matches and the program exits with
  status 1. Bubble Tea already installs its own `SIGINT` handler — do not
  add a second `signal.Notify`.
- **`msg.String()` for keys.** `tea.KeyPressMsg.String()` returns
  `"ctrl+c"`, `"enter"`, `"up"`, and single runes like `"j"`. Comparing
  to `tea.KeyPressMsg` values is fragile.
- **`View()` returns a `tea.View`, not a string.** Wrap the rendered
  content in `tea.NewView(...)`. When a component's view is embedded
  into another string, take its `.Content`.
- **The alternate screen is a property of the view.** Set
  `view.AltScreen = true` on every frame; there is no
  `tea.WithAltScreen()` option in Bubble Tea v2.
- **A `table` needs a width or it renders nothing.** In Bubble Tea v2 a
  table whose viewport has no width produces an empty table — no error,
  no panic. Call `SetWidth` on resize and give the table a
  `WithWidth` default at construction.
- **A `table` row must not have more cells than the table has
  columns.** Bubble Tea v2 indexes the columns with the cell position
  and panics with an index out of range otherwise.
- **PocketBase ids are 15+ characters.** A short id from a remote source
  must be padded; `normalizeID` in `pbstore` does this.
- **`PageResult.TotalItems` costs a second query.** Set
  `Query.SkipTotal = true` when you do not need the count.
- **Filters use the native PocketBase grammar**, not SQL. `title ~ "go"`,
  `status = "active"`, `-created,name` for sorting. Field names must
  exist in the collection schema.
- **`record.Fields` is `map[string]any`.** Values arrive as `string`,
  `float64`, `bool`, `[]any`, `map[string]any`. Use the helpers:
  `rec.StringField("x")`, `rec.Field("x")`.
- **The remote backend needs a token for non-public collections.** An
  unauthenticated request to a private collection returns an *empty list*,
  not an error — the failure is silent.
- **`config` precedence is flags > env > TOML.** A `FLINT_`-prefixed
  environment variable beats the config file.
- **`internal/` is not importable from outside the module.** Downstream
  projects use the template's own packages by copying them in, or by
  keeping everything in one module.

---

## 6. Working in a new project

This repository is a template. When you start a new project from it:

1. **Rename the module** in `go.mod` and every import path.
2. **Replace the example adapter.** `examples/wikidata/` is a reference
   implementation, not a product. Delete it and write your own, using
   it as the template for a custom migration, renderer, and filter.
3. **Keep the four core packages intact.** `pbstore`, `app`, `pages`,
   `components`, `state`, `theme` are the contract. Extend them; do not
   fork them.
4. **Add your migrations** in `appMigrations()` in `cmd/tui/main.go`.
5. **Wire your renderer** into `app.New(store, cfg, renderer)`.
6. **Set the module path** so `go get` works for your users.

The `book/` directory is a 25-chapter companion guide. It is the deepest
reference in the repository: chapters 21–25 cover the extension points in
detail, and chapter 16 explains the `pbstore` pattern.

---

## 7. Where things are documented

| Document                                          | What it covers                        |
| ------------------------------------------------- | ------------------------------------- |
| [`docs/agents/REFERENCE.md`](docs/agents/REFERENCE.md) | The exact API surface.             |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)   | How the pieces fit, and what is safe to change. |
| [`docs/PBSTORE_GUIDE.md`](docs/PBSTORE_GUIDE.md) | The data layer in depth.              |
| [`docs/GETTING_STARTED.md`](docs/GETTING_STARTED.md) | First run, first change.           |
| [`book/ch21-extension-points.md`](book/ch21-extension-points.md) | The extension catalogue.   |
| [`book/ch22-patterns-and-practices.md`](book/ch22-patterns-and-practices.md) | Idiomatic patterns.  |
| [`book/appendix-d-troubleshooting.md`](book/appendix-d-troubleshooting.md) | Symptom → cause → fix. |
