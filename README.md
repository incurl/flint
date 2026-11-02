# Flint

A **production-ready GitHub template** for building terminal user interface
(TUI) applications with **Bubble Tea**, **Bubbles** and **PocketBase**.

Flint is not an application: it is the foundation of one. After clicking
*Use this template* you only write the domain specific parts — your
vocabulary schemas, your renderers, your pages — while the store plumbing
and the TUI scaffolding are already in place.

```
┌──────────────────────────────────────────────────────────────┐
│  Flint                                                       │
│                                                              │
│   Bubble Tea (root model)  ──▶  pages  ──▶  components       │
│                                    │                         │
│                                    ▼                         │
│                              pbstore.Store                   │
│                                    │                         │
│                    ┌───────────────┴───────────────┐        │
│                    ▼                               ▼        │
│            Embedded PocketBase              Remote PocketBase │
│            (App.Store + SQLite)              (REST API)      │
└──────────────────────────────────────────────────────────────┘
```

## Highlights

- **Two backends, one interface.** `pbstore.Store` hides the difference
  between an in-process PocketBase instance and a remote PocketBase
  server. Domain code never branches on the backend.
- **Native PocketBase primitives.** Filters, sorting, pagination,
  validation and the record model are PocketBase's own — the same
  machinery the admin UI and the REST API use. No custom query language,
  no re-implemented pagination.
- **Strict Bubble Tea architecture.** One-way data flow, all I/O inside
  `tea.Cmd`, pure `Update` functions, a global hotkey layer and a
  persistent status bar.
- **Bubbles everywhere.** `table`, `textinput`, `textarea`, `viewport`,
  `list`, `spinner`, `paginator`.
- **One theme package.** Every style lives in `internal/theme`. Inline
  lipgloss literals are forbidden.
- **Extension points.** Custom record renderers, custom pages, custom
  sync strategies and custom migrations — none of them require touching
  the core packages.

## Use this template

1. Click the **Use this template** button at the top of this page (or
   clone it with `--depth 1`).
2. Rename the module path:

   ```bash
   go mod edit -module github.com/you/your-project
   ```

3. Remove the template specific files:

   ```bash
   rm -f .template-keep
   rm -rf .github/workflows/template-cleanup.yml
   ```

4. Run it:

   ```bash
   make run
   ```

> The `.github/workflows/template-cleanup.yml` workflow automates step 3
> for you on the first push.

## Quick start

```bash
# Embedded mode — a full PocketBase instance inside the binary.
make run

# Remote mode — talk to a PocketBase server over HTTP.
make run-remote REMOTE_URL=http://127.0.0.1:8090

# Build a standalone binary.
make build          # -> ./bin/flint
```

The TUI starts on the **home** page, listing the collections exposed by
the store. Pick one with `↑`/`↓` and `enter` to browse its records.

### Global hotkeys

| Key     | Action                                             |
| ------- | -------------------------------------------------- |
| `1`–`6` | Switch page (home, records, detail, search, settings, sync) |
| `r`     | Refresh the active page                            |
| `s`     | Run a store health check                           |
| `y`     | Trigger a remote → local sync (embedded mode)      |
| `esc`   | Go back to the previous page                       |
| `q`     | Quit                                               |

Page local shortcuts are namespaced and never conflict with the global
layer: `/` filters the record list, `enter` opens a record, `r` toggles
the raw JSON view on the detail page, `e` opens the JSON editor.

## Embedded vs remote

|                     | Embedded                            | Remote                             |
| ------------------- | ----------------------------------- | ---------------------------------- |
| Storage             | SQLite in `./pb_data`               | PocketBase server                  |
| Data model          | Native PocketBase collections        | Native PocketBase collections      |
| Filter / sort       | Native PocketBase grammar            | Native PocketBase grammar          |
| Writes              | Direct, with PocketBase validation  | Through the REST API               |
| Lookup cache        | **Yes** — synced into the local db   | No local cache (queries go remote) |
| Offline usage       | Yes                                 | No                                 |
| Setup               | None                                | A running PocketBase server        |

The **sync** page copies a remote collection into the local embedded
database, so an offline-first lookup tool can keep a local replica of a
reference dataset. The sync is always **explicit**: nothing runs in the
background.

## Configuration

The resolution order is **CLI flags > environment variables > TOML config
file > defaults**.

```toml
# flint.toml
mode = "embedded"
theme = "dark"
per_page = 30
log_level = "info"

[embedded]
data_dir = "./pb_data"

[remote]
url = "http://127.0.0.1:8090"
collections = ["items", "vocab"]

[sync]
collections = ["vocab"]
batch_size = 200
```

| Flag                  | Environment variable        | Default          |
| --------------------- | --------------------------- | ---------------- |
| `--mode`              | `FLINT_MODE`                | `embedded`       |
| `--theme`             | `FLINT_THEME`               | `dark`           |
| `--per-page`          | `FLINT_PER_PAGE`            | `30`             |
| `--log-level`         | `FLINT_LOG_LEVEL`           | `info`           |
| `--data-dir`          | `FLINT_DATA_DIR`            | `./pb_data`      |
| `--remote-url`        | `FLINT_REMOTE_URL`          | —                |
| `--remote-token`      | `FLINT_REMOTE_TOKEN`        | —                |
| `--collections`       | `FLINT_REMOTE_COLLECTIONS`  | all              |
| `--sync-collections`  | `FLINT_SYNC_COLLECTIONS`    | none             |
| `--sync-batch-size`   | `FLINT_SYNC_BATCH_SIZE`     | `100`            |
| `--migrate-only`      | —                           | `false`          |

> **Security:** never commit an API token. Use `FLINT_REMOTE_TOKEN` or an
> untracked config file.

> **`--migrate-only`** applies the embedded migrations and exits without
> starting the TUI. It is what `make pb-migrate` runs, and it is handy in a
> deployment pipeline that separates the migration step from the run step.

## Project layout

```
cmd/tui/           entry point: wires the store, the config and the model
config/            configuration loader (flags > env > TOML)
internal/
  app/             root Bubble Tea model, global routing, status bar wiring
  pbstore/         the Store interface + the embedded and remote backends
  components/      reusable components: record card, JSON viewer/editor, toast, status bar
  pages/           the isolated screens: home, records, detail, search, settings, sync
  theme/           the lipgloss palette and the layout helpers
  state/           the volatile runtime state (active page, health, sync)
examples/
  wikidata/        a complete domain adapter built on the core packages
docs/              the architecture and the extension guides
```

The `examples/wikidata` package is a **working reference implementation**:
it shows how to add a custom renderer, a custom migration, a label lookup
cache and a custom filter transform — without touching a single core
file.

## Use cases

The scaffold is deliberately generic. It fits projects such as:

- **Data dictionary and controlled vocabulary editors** — the dictionary
  collection is a plain PocketBase collection queried through `pbstore`.
- **Reference metadata lookup tools** — sync a remote reference dataset
  into the local database and browse it offline.
- **Wikidata / RDF item and property browsers** — see
  [`examples/wikidata/`](examples/wikidata/).
- **Audit and annotation tools** over structured JSON records.

## Documentation

- [`AGENTS.md`](AGENTS.md) — **for AI coding agents**: the architecture, the
  invariants, the gotchas and the verification loop. Start here.
- [`docs/agents/`](docs/agents/) — task recipes for agents: add a page, add a
  collection, add a renderer, add a command, add a config option, add a sync,
  debug a TUI, verify.
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — the TEA flow, the
  package boundaries, what you may modify.
- [`docs/GETTING_STARTED.md`](docs/GETTING_STARTED.md) — a guided tour.
- [`docs/PBSTORE_GUIDE.md`](docs/PBSTORE_GUIDE.md) — how to use the store,
  compose the native params, build dictionaries and caches.
- [`examples/wikidata/`](examples/wikidata/) — a complete domain adapter
  example: a custom migration, a custom renderer, a label lookup cache and
  a custom filter transform.

### 📖 The companion book

**[`book/`](book/)** — *Building Terminal Applications with Bubble Tea and
PocketBase*.

Twenty-six chapters and five appendices (~82,000 words), from the
terminal basics to shipping a production TUI:

- **Part I — Bubble Tea**: the Elm architecture, messages and commands,
  the key routing problem, rendering with lipgloss, the Bubbles
  components, and the concurrency rules.
- **Part II — PocketBase as a library**: the mental model, collections
  and records, the native filter grammar, `App.Store()` (and what it
  really is), hooks and validation, migrations, and embedded vs. server.
- **Part III — Putting them together**: the layering, the `pbstore`
  pattern, sync and offline-first caching, testing, performance, and
  shipping.
- **Part IV — Extending the template**: the extension points and their
  costs, the pattern library, the advanced patterns (background workers,
  modals, command palettes, undo, virtualised lists), the four domain
  shapes, and a playground of ideas you could build.
- **Part V — The JavaScript extension**: the PocketBase JSVM plugin, when
  it is worth using, and the procedure to enable it.

Start at [`book/README.md`](book/README.md).

## Development

```bash
make help      # list the targets
make test      # run the tests
make lint      # go vet + golangci-lint
make cross-build
```

## Requirements

- Go **1.27** or later
- A POSIX-like terminal (the UI is tested on Linux and macOS)

## License

MIT — see [LICENSE](LICENSE).
