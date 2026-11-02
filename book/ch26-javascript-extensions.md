# Chapter 26 — The JavaScript Extension

The PocketBase **JSVM** plugin lets you write hooks and migrations in
JavaScript instead of Go. This chapter answers a narrow question: *is it
still worth using in Flint?*

The answer is **yes for the embedded backend, with one important
caveat** — and the caveat is that it only works in one of the two
deployment modes.

Everything in this chapter was verified against PocketBase v0.40.6 and
Go 1.27.

---

## 26.1 What the JSVM actually is

Since PocketBase v0.23 the JavaScript support is no longer built into
the main binary. It lives in a **plugin**:

```
github.com/pocketbase/pocketbase/plugins/jsvm
```

The plugin embeds a [goja](https://github.com/dop251/goja) JavaScript
runtime and exposes PocketBase's own APIs to it: the app, the records,
the collections, the database, the cron, the mailer, the router.

Two things are loaded from disk:

| Directory         | What it holds                    | Loaded by               |
| ----------------- | -------------------------------- | ----------------------- |
| `pb_hooks/`       | The hook files                   | `HooksDir`              |
| `pb_migrations/`  | The migration files              | `MigrationsDir`         |

The default locations are relative to the data directory:
`pb_data/../pb_hooks` and `pb_data/../pb_migrations`.

> **A note on the naming.** PocketBase's own binary is a *server*. Flint
> uses PocketBase as an *embedded library*, so there is no
> `pocketbase serve` command and no admin UI. The JSVM plugin is the
> part of the JavaScript support that survives that change, because it
> is a library, not a binary feature.

---

## 26.2 The verdict

| Question                              | Answer                                    |
| ------------------------------------- | ----------------------------------------- |
| Does it still exist?                  | Yes — `plugins/jsvm`, still maintained.   |
| Does it work with the embedded store? | **Yes** — verified below.                  |
| Does it work with the remote store?   | **No** — see §26.7.                       |
| What does it cost?                    | +10.8 MB of binary, ~2 ms of startup.     |
| What does it add to `go.mod`?         | **Nothing** — see §26.5.                  |

> **The template wires it up, but it is off by default.** The
> application registers the plugin when `embedded.javascript` is true:
>
> ```bash
> flint --mode embedded --data-dir /var/lib/flint/pb_data --javascript
> ```
>
> The flag is off by default because the plugin embeds a JavaScript
> runtime: it adds about 10 MB to the binary whether it is used or
> not. The integration adds nothing to `go.mod` beyond the
> `charm.land` module paths.

The last row is the surprise, and the reason this chapter is worth
reading.

---

## 26.3 The proof

Here is a complete, working example. Both halves are exercised: a JS
migration creates a collection, and a JS hook normalises a field.

**The migration** — `pb_migrations/1_jsitems.js`:

```js
migrate((app) => {
    const collection = new Collection({
        type: "base",
        name: "jsitems",
        listRule: "",
        fields: [
            new Field({ name: "title", type: "text", required: true }),
        ],
    })

    app.save(collection)
}, (app) => {
    // The down migration. It runs when the migration is reverted.
    const collection = app.findCollectionByNameOrId("jsitems")
    app.delete(collection)
})
```

**The hook** — `pb_hooks/1_normalise.pb.js`:

```js
onModelValidate((e) => {
    const title = e.model.get("title")

    if (typeof title === "string" && title !== title.trim()) {
        e.model.set("title", title.trim())
    }
}, "jsitems")
```

**The registration** — in `cmd/tui/main.go`:

```go
case pbstore.ModeEmbedded:
    pb := pocketbase.NewWithConfig(pocketbase.Config{
        DefaultDataDir:       cfg.Embedded.DataDir,
        DefaultEncryptionEnv: cfg.Embedded.EncryptionEnv,
        DefaultDev:           cfg.Embedded.Dev,
        HideStartBanner:      true,
    })

    // The JSVM must be registered BEFORE the store is opened, because
    // the store bootstraps the app and runs the migrations.
    if cfg.Embedded.JavaScript {
        if err := registerJavaScript(pb, cfg); err != nil {
            return nil, err
        }
    }

    return pbstore.OpenEmbedded(pb, pbstore.EmbeddedConfig{
        // ...
    })
```

The registration itself:

```go
// registerJavaScript registers the PocketBase JavaScriptVM plugin.
func registerJavaScript(app core.App, cfg *config.Config) error {
    err := jsvm.Register(app, jsvm.Config{
        HooksDir:      cfg.HooksDir(),
        MigrationsDir: cfg.MigrationsDir(),
        TypesDir:      cfg.Embedded.DataDir,
    })
    if err != nil {
        return fmt.Errorf("register the jsvm plugin: %w", err)
    }

    return nil
}
```

**The result**, verified:

```
the JS migration created the 'jsitems' collection
title after the hook = "spaced"
```

The record was created with `"  spaced  "` and the hook trimmed it to
`"spaced"`.

---

## 26.4 The API that actually works

The JS API is not the one the PocketBase documentation shows, because
the documentation describes the *server* API. The names come from the
Go methods of `core.App`, converted to camel case:

```
OnModelValidate   → onModelValidate
OnRecordCreate    → onRecordCreate
OnBootstrap       → onBootstrap
OnModelAfterCreateSuccess → onModelAfterCreateSuccess
```

### The event object

Every hook receives an event object. The fields depend on the hook:

| Hook                     | The event carries                    |
| ------------------------ | ------------------------------------ |
| `onModelValidate`        | `e.model`                            |
| `onModelCreate`          | `e.model`                            |
| `onRecordAfterCreateSuccess` | `e.record`                      |
| `onBootstrap`            | `e.app`                              |

The most common mistake is reaching for `e.record` in a model hook. It
is `e.model`. The error is:

```
TypeError: Cannot read property 'get' of undefined or null
```

### The registration call

Every hook takes a callback and an optional list of tags:

```js
onModelValidate((e) => {
    // ...
}, "jsitems")
```

The tag filters the hook to one collection. Without it, the hook runs for
every collection.

### The hook names

There is no `onRecordBeforeCreate`. The `Before*` hooks do not exist in
this API — the validation hook is where you normalise the data:

```js
// This does NOT exist:
onRecordBeforeCreate((app, record) => { ... })

// This is the one:
onModelValidate((e) => { ... })
```

### The migrations

A migration file exports an `up` and a `down` function:

```js
migrate((app) => {
    // up
}, (app) => {
    // down
})
```

The `app` passed to a migration is a **transactional** app: every write
goes through it and is committed together, or rolled back as a whole.

---

## 26.5 The cost

### The dependency cost: none

This is the surprising part. Enabling the plugin adds **nothing** to
`go.mod`:

```diff
$ diff go.mod go.mod.with-jsvm
$ echo $?
0
```

Why? Because `goja` is **already a direct dependency of PocketBase
itself**:

```
$ grep goja $(go env GOMODCACHE)/github.com/pocketbase/pocketbase@v0.40.6/go.mod
github.com/dop251/goja v0.0.0-20261021140635-64adc8bb4d51
github.com/dop251/goja_nodejs v0.0.0-20260212111938-1f56ff5bcf14
```

The runtime is already in the module graph, already downloaded, already
compiled into the build cache. Importing the plugin only changes four
lines of `go.sum`.

### The binary cost: 10.8 MB

| Build                | Size      |
| -------------------- | --------- |
| Without the plugin   | 40.2 MB   |
| With the plugin      | 51.1 MB   |
| **Difference**       | **+10.8 MB** |

That is the price of the JavaScript engine and the Node.js compatibility
layer.

### The startup cost: 2.4 ms

```
jsvm.Register took 2.37466ms
```

Negligible for a TUI that a human starts by hand.

### The runtime cost

The hook callbacks run on a pool of `goja` runtimes. With
`HooksPoolSize` at zero (the default) a new runtime is created for
every fired goroutine, which is slow. Set it if you use cron jobs:

```go
jsvm.Config{
    HooksPoolSize: 4,
}
```

---

## 26.6 The ordering constraint

This is the part that will bite you.

**The JSVM must be registered before the app is bootstrapped.**

`OpenEmbedded` does two things:

```go
// internal/pbstore/embedded.go
if err := app.Bootstrap(); err != nil {          // ← bootstraps
    return nil, fmt.Errorf("pbstore: bootstrap embedded store: %w", err)
}

if err := app.RunAllMigrations(); err != nil {    // ← runs the migrations
    _ = app.ClearBootstrap()
    return nil, fmt.Errorf("pbstore: run embedded migrations: %w", err)
}
```

The JS migrations are registered into the same `core.AppMigrations`
registry as the Go ones, and `RunAllMigrations` applies them all. But
the JSVM plugin registers its migrations when it is registered — so if
you register it after `OpenEmbedded`, the JS migrations are never
applied.

**The rule: register the plugin, then open the store.**

### Go and JS migrations coexist

Both kinds live in the same registry and both run. Verified: a Go
migration named `20240101_go` and a JS migration named
`20240102_js.js` both executed, producing `go_col` and `js_col`.

The ordering follows the **filename**, with the Go migrations using
their explicit id. A JS migration is identified by its filename, so the
naming convention does the sorting:

```
pb_migrations/
    20240101_go_col.go.js    ← runs first
    20240102_js_col.js      ← runs second
```

---

## 26.7 The remote backend cannot carry hooks

This is the caveat, and it is not a small one.

Flint has two backends. The remote one is a pure HTTP client:

```go
// internal/pbstore/remote.go
type Remote struct {
    baseURL string
    token   string
    client  *http.Client
    // ...
}
```

There is **no PocketBase app** on the other side of that HTTP
connection — there is a server, running someone else's process, with
its own hooks and its own migrations.

So:

| Capability              | Embedded | Remote                     |
| ----------------------- | -------- | -------------------------- |
| JS hooks                | ✅        | ❌ (they run on the server) |
| JS migrations           | ✅        | ❌ (they run on the server) |
| Go hooks                | ✅        | ❌                         |
| Go migrations           | ✅        | ❌                         |
| The `--migrate-only` run| ✅        | ❌                         |

**The remote backend has no local schema at all.** `EnsureDictionaryCollection`
on it returns an error, and so does `SyncCollection`:

```go
func (s *Remote) EnsureDictionaryCollection(...) (Collection, error) {
    return Collection{}, errors.New("pbstore: the remote backend cannot create collections; ...")
}
```

If you run against a remote server, the schema is the server's
business. You would write your JS hooks and migrations **for the
server**, in the server's own `pb_hooks` directory, using the
`pocketbase` CLI — not in Flint.

### The practical consequence

**The choice of backend constrains the choice of language.**

- Embedded → JS is available, and Go is available. Use either.
- Remote → the server's language is the server's business. Flint's
  code cannot influence it.

This is worth thinking about before you pick a backend, because it is
not a decision you can defer. If you might switch to a remote server
later, the hooks you write in Flint will have to move to the server.

---

## 26.8 When to use JavaScript at all

The honest comparison is not "JS versus Go". It is:

| | Go hooks and migrations | JS hooks and migrations |
| --- | --- | --- |
| **Type safety** | ✅ compile-time | ❌ runtime |
| **Refactoring** | ✅ the compiler finds the call sites | ❌ grep |
| **Startup cost** | none | 2.4 ms |
| **Binary cost** | none | +10.8 MB |
| **Learning curve** | Go | Go + JS + the PocketBase JS API |
| **Debugging** | the usual Go tools | a JS runtime inside a Go process |
| **Shared with a server** | ❌ | ✅ (same files) |
| **Hot reload** | ❌ | ✅ (`HooksWatch`) |

### Use Go when

- The logic belongs to the application. Normalising a field, deriving a
  value, enforcing an invariant — that is application logic, and Go is
  the language the application is written in.
- You want the compiler. A typo in a hook name is a startup panic, not
  a compile error.
- The team does not want two languages.

### Use JavaScript when

- **The same hooks must run on a PocketBase server.** This is the
  strongest reason: a JS hook file can be copied into a server's
  `pb_hooks` directory and it will work there unchanged. A Go hook
  cannot leave the process.
- The team is more comfortable in JavaScript.
- You need the server-side APIs that the JS bindings expose.

### Do not use JavaScript when

- You are unsure. The cost is 10.8 MB and a second language for a
  benefit you may never use.

---

## 26.9 The failure modes

The JSVM fails **loudly**, which is its main operational virtue.

### A syntax error in a hook panics at startup

```
panic: failed to execute 1_normalise.pb.js:
 - ReferenceError: onRecordBeforeCreate is not defined
```

By default (`HooksWatch: false`) a hook that fails to load **panics the
process**. The app will not start. This is deliberate: a broken hook
should not be a silent no-op.

### A hook that throws at runtime

```
TypeError: Cannot read property 'get' of undefined or null at .../pb.js:2:21
```

The error is returned to the caller of the operation, so a failed
validation surfaces as a failed write rather than a crash.

### The dangerous case: a hook that does not match

A filter that matches nothing, a hook bound to the wrong collection
name, a migration that has already been applied — these are **silent**.
The hook simply does not run. There is no "the hook did not fire"
message.

This is the failure mode to watch for. When a hook "does not work",
check the collection tag first:

```js
onModelValidate((e) => { ... }, "jsitems")
//                              ^^^^^^^ must match the collection name exactly
```

### Hot reload

`HooksWatch: true` restarts the app when a hook file changes. It does
not work on Windows, because the restart relies on `execve`.

For a template, leave it off: it is a development convenience, and in
production the app should be restarted by its supervisor.

---

## 26.10 Testing the JavaScript extensions

A JS hook is a test artefact like any other. The test creates the hook
files in a temporary directory, registers the plugin, and asserts the
behaviour:

```go
func TestJSVMHook(t *testing.T) {
    dir := t.TempDir()
    hooksDir := filepath.Join(dir, "pb_hooks")
    migrationsDir := filepath.Join(dir, "pb_migrations")

    if err := os.MkdirAll(hooksDir, 0o755); err != nil {
        t.Fatal(err)
    }
    if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
        t.Fatal(err)
    }

    hook := `
onModelValidate((e) => {
    const title = e.model.get("title")
    if (typeof title === "string" && title !== title.trim()) {
        e.model.set("title", title.trim())
    }
}, "jsitems")
`
    if err := os.WriteFile(filepath.Join(hooksDir, "1_normalise.pb.js"), []byte(hook), 0o600); err != nil {
        t.Fatal(err)
    }

    // The hook targets a collection, so the collection must exist.
    // A migration is the natural way to create it.
    migration := `
migrate((app) => {
    app.save(new Collection({
        type: "base",
        name: "jsitems",
        fields: [new Field({ name: "title", type: "text" })],
    }))
}, (app) => {})
`
    if err := os.WriteFile(filepath.Join(migrationsDir, "1_jsitems.js"), []byte(migration), 0o600); err != nil {
        t.Fatal(err)
    }

    app := pocketbase.NewWithConfig(pocketbase.Config{
        DefaultDataDir:  filepath.Join(dir, "pb_data"),
        HideStartBanner: true,
    })

    if err := jsvm.Register(app, jsvm.Config{
        HooksDir:      hooksDir,
        MigrationsDir: migrationsDir,
        TypesDir:      filepath.Join(dir, "pb_types"),
    }); err != nil {
        t.Fatalf("jsvm.Register() error = %v", err)
    }

    store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{
        DataDir: filepath.Join(dir, "pb_data"),
    })
    if err != nil {
        t.Fatalf("OpenEmbedded() error = %v", err)
    }
    defer func() { _ = store.Close() }()

    record, err := store.Create(context.Background(), "jsitems", map[string]any{
        "title": "  spaced  ",
    })
    if err != nil {
        t.Fatalf("Create() error = %v", err)
    }

    title, _ := record.Field("title")
    if title != "spaced" {
        t.Errorf("the hook did not run, title = %q", title)
    }
}
```

Note that the test writes the hook files itself. The JavaScript is
**inline in the test**, which keeps the test self-contained and makes
the expected behaviour obvious.

⚠️ **The migration in that test is not optional.** The hook is bound to
the `jsitems` collection, so the collection has to exist before the
record is created. Without it the write fails with:

```
pbstore: resolve collection "jsitems": sql: no rows in result set


---

## 26.11 The procedure, step by step

If you have decided to use the JavaScript extensions, this is the
procedure.

### 1. Add the import

```go
import "github.com/pocketbase/pocketbase/plugins/jsvm"
```

There is no new module to download: `goja` is already in the graph.

### 2. Decide where the files live

The default is `pb_data/../pb_hooks`, which means the files sit
*outside* the data directory. That is deliberate: the data directory
holds the database and is often backed up or wiped, and the hooks are
source code.

```
/var/lib/flint/
    pb_data/          ← the database
    pb_hooks/         ← the hooks
    pb_migrations/    ← the migrations
```

⚠️ **The default is relative to the data directory, not to the
current directory.** With `--data-dir /var/lib/flint/pb_data`, the
hooks are looked up in `/var/lib/flint/pb_hooks`. With a data
directory that is not named `pb_data`, the hooks end up one level
above it.

The directories can be set explicitly:

```bash
flint --mode embedded \
    --data-dir /var/lib/flint/pb_data \
    --hooks-dir /var/lib/flint/pb_hooks \
    --migrations-dir /var/lib/flint/pb_migrations \
    --javascript
```

or in the config file:

```toml
[embedded]
data_dir = "/var/lib/flint/pb_data"
hooks_dir = "/var/lib/flint/pb_hooks"
migrations_dir = "/var/lib/flint/pb_migrations"
javascript = true
```

### 3. Enable the plugin

The plugin is **off by default**. Turn it on with `--javascript`, with
`embedded.javascript = true`, or with `FLINT_JAVASCRIPT=1`.

The registration is in `cmd/tui/main.go`, and it must happen **before**
`pbstore.OpenEmbedded`. This is not optional: the store bootstraps the
application and runs the migrations, and the JS migrations are
registered by the plugin.

```go
if cfg.Embedded.JavaScript {
    if err := registerJavaScript(pb, cfg); err != nil {
        return nil, err
    }
}
```

A missing hooks directory is **not** an error: the plugin simply finds
no file. The application starts normally with the plugin enabled and
no hooks at all.

### 4. Write the hook

Start with a `onModelValidate` and a collection tag. The file must end
with `.pb.js` or `.pb.ts`.

```js
onModelValidate((e) => {
    // e.model, not e.record
}, "collection_name")
```

### 5. Write the migration

```js
migrate((app) => {
    // up
}, (app) => {
    // down
})
```

Name the file with a sortable prefix: `20240102_my_change.js`.

### 6. Test it

Write a test that creates the files in a temporary directory, as in
§26.10. Do not test the JavaScript by hand — the failure modes are
silent.

### 7. Verify the migration runs

```bash
flint --mode embedded --data-dir /var/lib/flint/pb_data --migrate-only
```

Then check the collection exists:

```bash
sqlite3 /var/lib/flint/pb_data/data.db ".tables"
```

### 8. Deploy it

The hooks and migrations are **source code**. They belong in your
deployment, next to the binary — not in the data directory.

```
/opt/flint/flint            ← the binary
/opt/flint/pb_hooks/        ← the hooks
/opt/flint/pb_migrations/   ← the migrations
```

The systemd unit must point the application at both:

```ini
[Service]
ExecStart=/opt/flint/flint \
    --mode embedded \
    --data-dir /var/lib/flint/pb_data \
    --hooks-dir /opt/flint/pb_hooks \
    --migrations-dir /opt/flint/pb_migrations \
    --javascript
```

⚠️ **A broken hook panics the process at startup.** The application
will not start. If it fails immediately after you deploy a hook, read
the panic message: it names the file and the error.

The types file is written to the data directory, so it is worth adding
to the backup: it lets an editor offer the PocketBase API
completions.

---

## 26.12 The recommendation

**Keep the Go hooks and migrations as the default.** They are typed,
they are tested by the Go toolchain, and they cost nothing.

**Enable the JSVM when you have a concrete reason**, and the best
reason is the one the template cannot solve for you: *the same hooks
must also run on a PocketBase server*. A JS hook file is portable
between Flint and a server. A Go hook is not.

If you do enable it, remember the three things that will cost you an
afternoon if nobody told you:

1. **Register the plugin before `OpenEmbedded`**, or the migrations
   never run.
2. **The event field is `e.model`** in a model hook, not `e.record`.
3. **A hook that does not fire is silent.** Check the collection tag.

---

## 26.13 What you learned

- The JSVM is a **plugin** (`plugins/jsvm`), not a built-in feature.
  It survived the move out of the PocketBase binary because it is a
  library.
- It **works with the embedded backend**, and only with it. The remote
  backend is an HTTP client with no local app, so it cannot carry
  hooks.
- It costs **+10.8 MB of binary** and **2.4 ms of startup**, and
  **nothing in `go.mod`** — the JavaScript runtime is already a
  dependency of PocketBase.
- The plugin must be registered **before** the store is opened, because
  the store bootstraps the app and runs the migrations.
- The template registers it **only when asked**: `--javascript`,
  `embedded.javascript` or `FLINT_JAVASCRIPT=1`. The binary carries the
  runtime either way.
- Go and JS migrations **coexist**, ordered by filename.
- The JS API is derived from the Go methods: `OnModelValidate` becomes
  `onModelValidate`, and the event carries `e.model`.
- A broken hook **panics at startup**; a hook that does not match is
  **silent**.
- Use Go by default. Use JavaScript when the hooks must also run on a
  server.

---

## 26.14 Exercises

1. **Measure the cost yourself.** Build the app with and without the
   plugin and compare the binary sizes. Then confirm that `go.mod` is
   unchanged.

2. **Break a hook on purpose.** Write a hook with a syntax error and
   start the app. Read the panic message. Then fix it.

3. **Write the wrong event field.** Use `e.record` in an
   `onModelValidate` hook and read the error. Find the table in §26.4
   that tells you which field each hook carries.

4. **Prove the ordering matters.** Register the plugin *after*
   `OpenEmbedded` and check that the JS migration does not run. Then
   move the registration and watch it appear.

5. **Write a hook for the remote case.** Argue, in writing, where a
   hook that must run both in Flint and on a server has to live. Then
   check whether the answer is "the server".

6. **Measure the hook cost.** Write a hook that runs on every record
   creation, create ten thousand records, and measure the difference
   with and without the hook.

---

## 26.15 Further reading

| What                              | Where                                                     |
| --------------------------------- | --------------------------------------------------------- |
| The plugin source                 | `plugins/jsvm/jsvm.go` in the PocketBase module            |
| The hook bindings                 | `plugins/jsvm/binds.go`                                     |
| The JS API reference              | <https://pocketbase.io/docs/js-sdk/>                       |
| The goja runtime                   | <https://github.com/dop251/goja>                           |
| The app migrations registry       | `core.AppMigrations` in the PocketBase module              |
