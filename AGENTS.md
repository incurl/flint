# AGENTS.md

Guidance for AI coding agents working in this repository.

Flint is a **Go template** for terminal applications: Bubble Tea + Bubbles +
Lip Gloss on the front, PocketBase on the back. It runs either with an
embedded PocketBase (in-process, local database) or against a remote
PocketBase server over REST, behind one interface.

**Read this file first. Then read the task-specific skill in
`docs/agents/`.** The skills are short recipes; this file is the map.

> **New here?** If you are starting a *new project* from this template,
> read [`docs/agents/start-a-project.md`](docs/agents/start-a-project.md)
> first — it covers the rename, what to delete, and what to keep.

---

## 1. Orientation

### The packages that matter

| Package          | Responsibility                                          | Rule of thumb                                  |
| ---------------- | ------------------------------------------------------- | ---------------------------------------------- |
| `internal/pbstore` | The data layer. `Store` interface + embedded/remote impls. | Never branch on the backend. Use `Store`.     |
| `internal/hybridstore` | `Hybrid`: wraps a `Store` to evaluate the JSON filters the native grammar cannot express. | The only package allowed to import both `pbstore` and `jsonfilter`. |
| `internal/jsonfilter` | The local evaluator for array and `not` filters.      | Pure functions over a record map; no state.     |
| `internal/agent`  | The agentic layer: tool registry, run lifecycle, permission gate. | Knows nothing about the UI. Publishes events. |
| `internal/session` | The conversations of the agent, stored in PocketBase. | Reads and writes through `pbstore`.            |
| `internal/app`   | Root Bubble Tea model: routing, global keys, status bar, toasts. | Owns global concerns only.                |
| `internal/pages` | One file per screen. Self-contained.                    | A page never imports `internal/app`.           |
| `internal/components` | Reusable widgets: record card, JSON viewer/editor, toast, status bar, pagination. | Generic, no domain knowledge.      |
| `internal/state` | `Runtime`: volatile shared state (active page, health, sync). | Shared across pages; not persisted.   |

`internal/theme` holds the lipgloss palette. `config` loads settings
(flags > env > TOML). `cmd/tui` wires it all together.

### The dependency rule (do not break this)

```
cmd/tui → app → pages → components → theme
             ↘     ↘         ↘
         hybridstore  state   pbstore → (nothing)
             ↘                 ↑
           jsonfilter           │
             ↘                 │
             agent ─────────────┘
             ↕
          session
```

- `pages` **must not** import `app` — that is an import cycle. Pages
  request navigation by emitting `pages.NavigateMsg` / `pages.OpenRecordMsg`,
  which the root model handles.
- `pbstore` must not import anything from `internal/`. It is a standalone
  data layer. Anything that needs to sit *above* it — the hybrid filter is
  the example — goes in its own package (`internal/hybridstore`), never
  inside `pbstore`.
- `components` must not import `pages` or `app`.

If you find yourself wanting to import `app` from `pages`, you need a
message instead. See skill `add-a-page.md`.

### The message flow

```
keypress → app.Model.Update
              ├─ global key layer (ctrl+c, q, 1-7 digits, +/- zoom, T theme, c compact, ctrl+b panel)
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
4. **Every page that shows data owns a `Refresh()`**, and the root model
   calls it on navigation. A page that shows data without a `Refresh`
   shows nothing. The two pages that fetch nothing — `settings` and
   `sync` — have none, and that is correct: they render the `Runtime`
   and the configuration they were given.
5. **The stale-response guard.** Every asynchronous message carries the
   identity it was requested for — the page number for a list, the
   record id for a detail — and the handler compares it to the current
   selection before applying the result. Without it, a slow response for
   something the user has already left overwrites what they are looking
   at. See `docs/agents/REFERENCE.md` § *Stale response guard*.
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
| [`add-an-agent-tool.md`](docs/agents/add-an-agent-tool.md) | You want the model to do something new.             |
| [`AGENT_SETUP.md`](docs/AGENT_SETUP.md) | You want to *use* the agent: setup, providers, usage. |
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

- **A script renderer must never run in `View()`.** The record renderer
  is called while a frame is drawn, so a JavaScript renderer precomputes
  its output in a command ([components.Preload]) and the render methods
  only read the cache. A renderer that evaluated the script in `View()`
  would block the loop for as long as the script takes.
- **The JavaScriptVM plugin is off by default.** `--javascript` (or
  `embedded.javascript`) registers it in `cmd/tui/main.go`, **before**
  `pbstore.OpenEmbedded`: the store bootstraps the app and runs the
  migrations, and the plugin registers the JS migrations. A missing
  hooks directory is not an error.
- **The theme is shared by pointer and mutated in place.** Every page
  holds the same `*theme.Theme` and reads the styles when it renders, so
  a runtime change is one call: `m.theme.Apply(variant, scale)`. Never
  swap the pointer — the pages would keep the old one.
- **The UI scale is a percentage between 50 and 200.** `+`/`-` change it
  by 10, `T` switches the palette, `c` toggles the compact rows. All
  are persisted in the embedded store (the `_flint_preferences`
  collection) and restored at startup. The remote backend cannot hold
  them.
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
- **The agent panel is width-gated.** `ctrl+b` only works on a
  terminal of at least 121 columns: the record table is 88 wide, the
  panel takes 32, and a table narrower than its columns renders
  *empty*. When the panel is open the root model forwards the resize
  with the width of the *page*, not the width of the terminal.
- **The panel toggle is unreachable where an input has the focus.**
  It is a global hotkey, and the global layer is suspended while a text
  input has the focus — the agent and the search pages. The status bar
  must not advertise it there, and a panel left open when the user
  navigates to such a page has to be closed: the key that closes it
  does not work there.
- **Two columns have to add up.** `theme.SplitWidth` returns the two
  widths *without* the gap, so the body is padded to
  `bodyWidth + gap`. Lip Gloss does not pad the lines of a bordered
  block either: pad them yourself, or the border is ragged.
- **PocketBase ids are 15+ characters.** A short id from a remote source
  must be padded; `normalizeID` in `pbstore` does this.
- **`PageResult.TotalItems` costs a second query.** Set
  `Query.SkipTotal = true` when you do not need the count.
- **Filters use the native PocketBase grammar**, not SQL. `title ~ "go"`,
  `status = "active"`, `-created,name` for sorting. Field names must
  exist in the collection schema.
- **The native grammar cannot reach into a JSON array.** The expression
  parser rejects `[`, and an array field compared with `=` matches
  nothing. The `hybridstore.Hybrid` wrapper evaluates those expressions
  locally with `internal/jsonfilter`: `meta.tags.#(=="cli")`,
  `meta.items.0.name = "x"`, `meta.items.#.qty > 5`. The detection is
  syntactic, so the two layers never disagree.
- **A local filter is bounded.** `max_local_records` (default 1000) caps
  the number of records fetched for the local evaluation. When the bound
  is reached, `PageResult.Truncated` is set and the pagination shows
  `(partial)`. Never raise the bound without checking the memory cost.
- **`record.Fields` is `map[string]any`.** Values arrive as `string`,
  `float64`, `bool`, `[]any`, `map[string]any`. Use the helpers:
  `rec.StringField("x")`, `rec.Field("x")`.
- **The remote backend needs a token for non-public collections.** An
  unauthenticated request to a private collection returns an *empty list*,
  not an error — the failure is silent.
- **`config` precedence is flags > env > TOML.** A `FLINT_`-prefixed
  environment variable beats the config file.
- **A written config holds the values the *file* had.** `Config.Save`
  and `Config.SaveAgent` write `FileValues()`, the snapshot taken
  before the environment and the flags were applied: a setting that
  came from `FLINT_AGENT_PROVIDER` does not belong in a file. The file
  is written with `0600` and atomically, and the API key is never in
  it — only the name of the variable that holds it.
- **The provider's "carry the API secret key" message is useless.**
  It is the same for a key that is empty, one that is invalid, one
  that belongs to the other platform and one with a stray space. The
  key is trimmed before it is sent — a pasted key carries a trailing
  space, and the header is compared byte for byte — and
  `flint --check-agent` tells the causes apart.
- **A provider that needs a key is refused one that has none.** An
  empty key produces a request with **no `Authorization` header at
  all**, and the provider answers with a message about the header —
  which says nothing about the variable that holds the key, and sends
  the user looking for a header that was never there. `agent.NewModel`
  refuses the empty key, and the error names the variable to export.
- **A page that renders more lines than the terminal has is cut off,
  and the lines at the end are the ones that are lost.** The agent
  conversation, the settings rows and the record fields all grow
  without a bound, so they are scrolled. `components.Scroller` is a
  viewport that **follows the end** while the reader is there: a
  conversation is read from its last line. Scrolling up takes over,
  and scrolling back gives it back.
- **The content of a scroller is prepared before the keys are
  handled.** A scroll has to act on the conversation as it is now, not
  on the one of the last frame.
- **The runner's model can be replaced while the application runs.**
  `Runner.SetModel` installs it, and `Run` reads the current one at the
  start of every turn — a run already in flight keeps the model it
  started with. This is what lets the guided setup make the agent work
  without a restart. The key it uses is held in memory and never
  written to a file.
- **The plan mode is a restriction, not a request.** In `ModePlan` the
  tools that change the data are not in the tool list at all, so the
  model cannot call them — a prompt would only ask it not to. A tool
  the template does not know is withheld too, so a downstream project
  is safe by default.
- **A stuck run is stopped early.** `MaxSteps` is the budget, and the
  loop detection is the diagnosis: the same call, with the same
  arguments, returning the same answer more than five times in ten
  steps ends the run with `ErrLoopDetected`.
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
3. **Keep the core packages intact.** `pbstore`, `hybridstore`,
   `jsonfilter`, `app`, `pages`, `components`, `state`, `theme` are the
   contract. Extend them; do not fork them.
4. **Add your migrations** in `appMigrations()` in `cmd/tui/main.go`.
5. **Wire your renderer** into `app.New(store, cfg, renderer)`.
6. **Set the module path** so `go get` works for your users.

The `book/` directory is a 26-chapter companion guide. It is the deepest
reference in the repository: chapters 22–26 cover the extension points in
detail, and chapter 17 explains the `pbstore` pattern.

---

## 7. Where things are documented

| Document                                          | What it covers                        |
| ------------------------------------------------- | ------------------------------------- |
| [`docs/agents/REFERENCE.md`](docs/agents/REFERENCE.md) | The exact API surface.             |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)   | How the pieces fit, and what is safe to change. |
| [`docs/PBSTORE_GUIDE.md`](docs/PBSTORE_GUIDE.md) | The data layer in depth.              |
| [`docs/GETTING_STARTED.md`](docs/GETTING_STARTED.md) | First run, first change.           |
| [`docs/AGENT_SETUP.md`](docs/AGENT_SETUP.md) | The agentic layer: setup, configuration, usage, removal. |
| [`book/ch22-agentic-layer.md`](book/ch22-agentic-layer.md) | The agentic layer: the tools, the gate, the conversations. |
| [`book/ch23-extension-points.md`](book/ch23-extension-points.md) | The extension catalogue.   |
| [`book/ch24-patterns-and-practices.md`](book/ch24-patterns-and-practices.md) | Idiomatic patterns.  |
| [`book/appendix-d-troubleshooting.md`](book/appendix-d-troubleshooting.md) | Symptom → cause → fix. |
