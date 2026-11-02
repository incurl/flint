# Skill: start a new project from this template

**Goal:** turn the template into your own application.

**Time:** ~30 minutes.

---

## Step 1 — copy and rename

```bash
# From your new project's parent directory.
git clone <the template url> myapp
cd myapp
rm -rf .git && git init
```

Then rename the module. The module path appears in `go.mod` and in
every import.

```bash
# Replace the module path in go.mod.
go mod edit -module github.com/you/myapp

# Update the imports across the tree.
grep -rl "github.com/incurl/flint" --include="*.go" . \
  | xargs sed -i 's|github.com/incurl/flint|github.com/you/myapp|g'
```

Verify:

```bash
go build ./... && go test ./...
```

---

## Step 2 — decide what the app does

Write one sentence: **"My app lets the user ______."**

Then answer:

| Question                                | Where it lands                                  |
| --------------------------------------- | ----------------------------------------------- |
| Which collections does it show?         | migrations + the allowlist                       |
| How are records displayed?              | a `components.RecordRenderer` implementation     |
| Does it need remote data?               | `remote.url` + `remote.token` + a sync          |
| What extra screens does it need?        | new pages                                        |
| What extra key actions?                 | commands in the pages or the root model          |

Write the answers down before you write code. Most mistakes in a new
project come from building the plumbing before deciding the shape.

---

## Step 3 — replace the example adapter

`examples/wikidata/` is a **reference implementation**, not a product.
Delete it and write your own.

```bash
rm -rf examples/wikidata
```

Keep the structure though — it demonstrates the four extension points:

1. **A custom migration** — your collections.
2. **A custom renderer** — how your records look.
3. **A lookup cache** — a collection used as a dictionary.
4. **A custom filter** — how a search term becomes a PocketBase filter.

Replace it with your own package:

```
internal/domain/
  migrations.go   the collections
  renderer.go     the RecordRenderer
  filter.go       the filter transform
```

The package must not import `internal/app` or `internal/pages`. It
imports `pbstore`, `components` and `state` at most.

---

## Step 4 — your migrations

```go
// cmd/tui/main.go
func appMigrations() []core.Migration {
	return domain.Migrations()
}
```

See [`add-a-collection.md`](add-a-collection.md).

---

## Step 5 — your renderer

```go
// cmd/tui/main.go
model := app.New(store, cfg, domain.NewRenderer())
```

See [`add-a-record-renderer.md`](add-a-record-renderer.md).

---

## Step 6 — trim the template

The template ships six pages: home, records, detail, search, settings,
sync. Most applications need a subset.

**To remove a page:**

1. Delete `internal/pages/<name>.go`.
2. Remove the field from `Model` in `internal/app/app.go`.
3. Remove the construction in `New`.
4. Remove the cases in `updateActivePage`, `View` and `loadPage`.
5. Remove the constant from `internal/state/state.go`, and from
   `Page.Title()` and `AllPages()`.
6. Remove the hotkey in `handleGlobalKey` and the hint in `hints()`.

**Keep the home page** — it is the collection picker, and the whole app
depends on `runtime.Collection` being set.

---

## Step 7 — the config

`config/config.go` holds the settings. Add your own — see
[`add-a-config-option.md`](add-a-config-option.md).

Rename `FLINT_` to your own prefix if you like, but update
`EnvPrefix` and the README together.

---

## Step 8 — the identity

Update:

- `README.md` — the name, the description, the install instructions.
- `config/flags.go` — the `Usage` text.
- `LICENSE` — keep it if you want MIT, replace it otherwise.
- `Makefile` — the `BINARY` variable.

---

## Step 9 — the book

The `book/` directory is a 26-chapter guide to the architecture. It is
excellent onboarding material for a new maintainer, and it stays
accurate because it is checked against the code.

**Keep it.** It is the single best artefact in the repository.

If you want to shorten it for your users, move it to a `docs/book/`
directory and link it from the README. Do not delete it — it explains
*why* the code is shaped the way it is, and an agent (or a new
maintainer) will need that.

---

## Step 10 — remove the template-specific bits

```bash
# The .idea directory is a JetBrains IDE project.
rm -rf .idea
```

Keep `.github/workflows/ci.yml` — it is a good CI.

---

## Step 11 — verify

```bash
make ci
make lint
make test-race
```

Then run it:

```bash
go run ./cmd/tui --mode embedded --data-dir ./pb_data
```

---

## Step 12 — a first commit

```bash
git add -A
git commit -m "chore: initialise the application from the Flint template"
```

---

## A note on `internal/`

`internal/` packages are **not importable from another module**. If you
want to publish a reusable store package, move `internal/pbstore` to a
top-level `pbstore/` directory and update the imports. Everything else
can stay internal.

---

## Checklist

- [ ] The module path is renamed and the build passes.
- [ ] `examples/wikidata` is replaced with your own domain package.
- [ ] Your migrations are registered in `appMigrations()`.
- [ ] Your renderer is wired into `app.New`.
- [ ] The unused pages are removed (or kept deliberately).
- [ ] Your config options work at all three levels.
- [ ] The README describes *your* app.
- [ ] `make ci`, `make lint` and `make test-race` pass.
- [ ] The app starts and quits cleanly.
