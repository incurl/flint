# Chapter 21 — Shipping: Configuration, Shutdown, Packaging

## 20.1 The shape of a shipped application

A program you run locally is not a finished product. A finished product
has:

- **configuration** that a user can change without recompiling;
- **graceful shutdown** that does not corrupt the data;
- **structured logs** that can be diagnosed;
- **a build** that works on the platforms your users have.

Let us work through each.

## 20.2 The configuration

### The precedence rule

```
CLI flags > environment variables > TOML file > defaults
```

This order is not arbitrary. It follows the principle of *least
surprise*:

- a **flag** is the most explicit, the most local, the most likely to
  be intentional;
- an **environment variable** is set once for a session or a CI job;
- a **config file** is a persistent, shareable, versioned setting;
- the **defaults** are the fallback.

### The structure

```go
type Config struct {
    Mode    string `toml:"mode"`
    Theme   string `toml:"theme"`
    PerPage int    `toml:"per_page"`
    LogLevel string `toml:"log_level"`

    Embedded EmbeddedConfig `toml:"embedded"`
    Remote   RemoteConfig   `toml:"remote"`
    Sync     SyncConfig     `toml:"sync"`
}

type EmbeddedConfig struct {
    DataDir       string `toml:"data_dir"`
    EncryptionEnv string `toml:"encryption_env"`
    Dev           bool   `toml:"dev"`
    HooksDir      string `toml:"hooks_dir"`
    MigrationsDir string `toml:"migrations_dir"`
    JavaScript    bool   `toml:"javascript"`
}
```

⚠️ **Keep the config flat and the sections few.** A configuration with
nested sections three deep is a configuration nobody changes.

⚠️ **The config file is not the place for every setting.** The config
file holds the **deployment configuration**: where the data lives, which
server to talk to. It cannot hold the **user preferences** — the theme,
the UI scale, the page size — because those describe the person using
the application, not the machine it runs on. The preferences live in the
PocketBase store, and the template loads them at startup.

The distinction is worth stating because it decides where a new setting
goes:

| State                                      | Where it lives |
| ------------------------------------------ | -------------- |
| Deployment configuration (mode, data dir)  | The TOML file  |
| User preferences (theme, scale, page size) | The store      |
| Runtime state (page, collection, filter)   | Memory only    |

### The loader

```go
func Load(configPath string, flags Flags) (*Config, error) {
    cfg := Default()

    // 1. TOML config file (lowest priority).
    path, err := resolveConfigPath(configPath)
    if err != nil {
        return nil, err
    }
    if path != "" {
        if err := loadFile(cfg, path); err != nil {
            return nil, err
        }
    }

    // 2. Environment variables.
    applyEnv(cfg)

    // 3. CLI flags (highest priority).
    applyFlags(cfg, flags)

    if err := cfg.Validate(); err != nil {
        return nil, err
    }
    return cfg, nil
}
```

The order of the three applications *is* the precedence rule. It is
almost too simple to see — which is why it is worth writing down.

### The environment variables

```go
const EnvPrefix = "FLINT_"

func applyEnv(cfg *Config) {
    if v := os.Getenv(EnvPrefix + "MODE"); v != "" {
        cfg.Mode = v
    }
    if v := os.Getenv(EnvPrefix + "THEME"); v != "" {
        cfg.Theme = v
    }
    // ...
}
```

⚠️ **Use a prefix.** Without one, `MODE` and `THEME` collide with
every other program on the system.

⚠️ **Only override when the variable is non-empty.** An empty
environment variable is usually "not set" in the shell's intent, and
overriding the config file with it would be surprising.

### The config file location

```go
func resolveConfigPath(explicit string) (string, error) {
    if explicit != "" {
        if _, err := os.Stat(explicit); err != nil {
            return "", fmt.Errorf("config: cannot read %q: %w", explicit, err)
        }
        return explicit, nil
    }

    candidates := []string{"flint.toml"}

    if dir, err := os.UserConfigDir(); err == nil && dir != "" {
        candidates = append(candidates, filepath.Join(dir, "flint", "config.toml"))
    }

    for _, candidate := range candidates {
        if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
            return candidate, nil
        }
    }

    return "", nil
}
```

⚠️ **The explicit path must exist.** If the user typed `--config
foo.toml` and the file is missing, that is an error — not a silent
fallback to the defaults. The user asked for a specific file; not
finding it is a mistake worth reporting.

The *implicit* paths, on the other hand, are best-effort: if
`flint.toml` is not there, that is normal.

### The flags

```go
type Flags struct {
    ConfigPath string
    Mode       string
    Theme      string
    PerPage    int
    // ...
}

func ParseFlags(args []string) (*Flags, []string, error) {
    fs := flag.NewFlagSet("flint", flag.ContinueOnError)
    // ...
    if err := fs.Parse(args); err != nil {
        return nil, nil, err
    }
    return &flags, fs.Args(), nil
}
```

⚠️ **The zero value means "not provided".** `PerPage int` with the
zero value cannot be distinguished from "the user asked for 0". That is
fine, because 0 is not a valid page size. But if a flag *can* be 0
and 0 is meaningful, use a pointer.

```go
PerPage *int   // nil means "not provided"
```

### The validation

```go
func (c *Config) Validate() error {
    switch pbstore.Mode(c.Mode) {
    case pbstore.ModeEmbedded, pbstore.ModeRemote:
    default:
        return fmt.Errorf("config: invalid mode %q (expected %q or %q)",
            c.Mode, pbstore.ModeEmbedded, pbstore.ModeRemote)
    }

    if !theme.Variant(c.Theme).Valid() {
        return fmt.Errorf("config: invalid theme %q (expected %q or %q)",
            c.Theme, theme.VariantDark, theme.VariantLight)
    }

    if c.PerPage <= 0 {
        return fmt.Errorf("config: per_page must be positive, got %d", c.PerPage)
    }

    if pbstore.Mode(c.Mode) == pbstore.ModeRemote && c.Remote.URL == "" {
        return fmt.Errorf("config: remote.url is required in the %q mode", c.Mode)
    }

    return nil
}
```

⚠️ **Validate after resolving, not before.** The mode and the theme
come from three sources; the validation must see the final value.

⚠️ **Validate the *combination*, not just the fields.** "The remote
mode requires a URL" is a cross-field rule that only makes sense after
the merge.

## 20.3 The secrets

The remote token is the only secret.

⚠️ **Never commit a token.** The template's config struct says so:

```go
// Token is the API token used to authenticate the requests.
//
// SECURITY: do not commit a token. Prefer the FLINT_REMOTE_TOKEN
// environment variable or an untracked config file.
Token string `toml:"token"`
```

And the `.gitignore`:

```gitignore
# Local configuration (may contain a token)
flint.local.toml
config.local.toml
```

### A better design

For a serious application, do not put the token in the config file at
all. Use a keyring, or an environment variable only:

```go
// The token is read from the environment, never from the file.
if token := os.Getenv("FLINT_REMOTE_TOKEN"); token != "" {
    cfg.Remote.Token = token
}
```

⚠️ **Do not log the token.** The `Flags.String()` method is careful
about this:

```go
// String renders the flags as a compact, log friendly string.
//
// The API token is never included.
func (f *Flags) String() string {
    parts := []string{}
    if f.Mode != "" {
        parts = append(parts, "mode="+f.Mode)
    }
    // ... no token
    return strings.Join(parts, " ")
}
```

## 20.4 The shutdown

The shutdown sequence has an order, and every step depends on the
previous one.

```go
func run() error {
    // 1. Parse the flags.
    flags, args, err := config.ParseFlags(os.Args[1:])
    if err != nil {
        if errors.Is(err, flag.ErrHelp) {
            return nil
        }
        return err
    }
    if len(args) > 0 {
        return fmt.Errorf("unexpected argument %q", args[0])
    }

    // 2. Resolve the configuration.
    cfg, err := config.Load(flags.ConfigPath, *flags)
    if err != nil {
        return err
    }

    // 3. Set up the logging.
    logger := newLogger(cfg.LogLevel)
    slog.SetDefault(logger)

    // 4. Open the store.
    store, err := openStore(cfg)
    if err != nil {
        return err
    }

    // 5. The deferred close: it runs after everything else.
    defer func() {
        if err := store.Close(); err != nil {
            logger.Error("failed to close the store", "error", err)
        }
    }()

    // 6. Build and run the program.
    model := app.New(store, cfg, ctx)
    program := tea.NewProgram(model)

    // 7. The signal handler.
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
    defer signal.Stop(sigCh)

    go func() {
        <-sigCh
        program.Quit()
    }()

    if _, err := program.Run(); err != nil {
        return err
    }
    return nil
}
```

### The order

1. `SIGTERM` arrives → `program.Quit()`.
2. Bubble Tea restores the terminal (the alternate screen is exited, the
   cursor is shown, the raw mode is disabled).
3. `program.Run()` returns.
4. The deferred `store.Close()` releases the database.
5. `main` returns, and the deferred `cancel()` stops the goroutines.

⚠️ **The terminal must be restored before the program exits.** Bubble
Tea handles this in `Run`, so the order is: quit the program, then close
the store. If you closed the store first, a query in flight could
panic.

⚠️ **The signal handler must not touch the store.** It runs on its own
goroutine, concurrently with the event loop. Closing the store there
would race with an in-flight query.

### The `defer` ordering

⚠️ **Go runs the deferred calls in reverse order.** In `run()`, the
`defer cancel()` (if you add it) must come *before* the `defer
store.Close()` in the source, so that it runs *after* it. Actually — you
want the store closed first, then the context cancelled. So:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()      // runs LAST

store, err := openStore(cfg)
defer store.Close()  // runs FIRST
```

Wait — `defer cancel()` is registered first, so it runs last. And
`defer store.Close()` is registered second, so it runs first. That is
the order you want: **close the store, then cancel the context.**

Hmm, actually it does not matter much: closing the store causes the
queries to fail with an error, and the cancelled context causes them
to fail with `context.Canceled`. Both are handled. But the clean order
is: quit the program → close the store → cancel the context → exit.

### The exit code

```go
func main() {
    if err := run(); err != nil {
        if isBenignShutdown(err) {
            return
        }
        fmt.Fprintf(os.Stderr, "flint: %v\n", err)
        os.Exit(1)
    }
}

// isBenignShutdown reports whether the error is one of the expected
// shutdown outcomes of a Bubble Tea program.
func isBenignShutdown(err error) bool {
    return errors.Is(err, tea.ErrInterrupted) ||
        errors.Is(err, tea.ErrProgramKilled)
}
```

⚠️ **Do not print an error for a normal exit.** A `SIGINT` is not a
failure. Printing "flint: interrupted" on every `ctrl+c` is noise.

⚠️ **Do not invent your own "interrupted" sentinel.** The obvious
mistake is to declare `var errInterrupted = errors.New("interrupted")`
and check for it. That sentinel is never returned by anything, so the
check never matches and the program exits with the status 1 on every
`ctrl+c`.

The sentinel that Bubble Tea actually returns is **`tea.ErrInterrupted`**
(it is returned when the program receives a `SIGINT`). Check for that
one, plus `tea.ErrProgramKilled`.

⚠️ **And do not install a second signal handler.** Bubble Tea already
registers its own `SIGINT`/`SIGTERM` handler, and `Program.Run` returns
when it fires. Adding a `signal.Notify` of your own races with it: both
handlers receive the signal, and the outcome depends on which one wins.
Let Bubble Tea own the signal and simply handle the error it returns.

The difference is easy to observe — run the program, press `ctrl+c`,
and check `$?`:

```console
$ ./flint
^C
$ echo $?
0
```

A non-zero status here breaks every shell script and CI job that wraps
the TUI.

## 20.5 The logging

### Structured logs to the stderr

```go
func newLogger(level string) *slog.Logger {
    var lvl slog.Level

    switch level {
    case "debug":
        lvl = slog.LevelDebug
    case "warn":
        lvl = slog.LevelWarn
    case "error":
        lvl = slog.LevelError
    default:
        lvl = slog.LevelInfo
    }

    return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
        Level: lvl,
    }))
}
```

⚠️ **The logs must go to the stderr, never the stdout.** The stdout is
the TUI's output. A log line on the stdout corrupts the display.

In the alternate-screen mode, the stderr is still visible (it is a
different file descriptor), but it scrolls the *normal* buffer, not the
alternate one. So the TUI is unaffected.

⚠️ **Keep the logs out of the render loop.** The PocketBase dev mode
prints every SQL statement:

```go
pb := pocketbase.NewWithConfig(pocketbase.Config{
    DefaultDev: cfg.Embedded.Dev,
})
```

⚠️ **Never enable the dev mode in production.** A listing of 30
records with a dev-mode log produces 30 log lines *per keystroke*.

### What to log

| Event            | Level  | Fields                          |
| ---------------- | ------ | ------------------------------- |
| The startup      | Info   | mode, data dir, collections     |
| A store error    | Error  | the error, the operation        |
| A sync           | Info   | the collection, the counts      |
| A query          | Debug  | the collection, the filter, the duration |
| A config change  | Debug  | the key, the old and new values |

```go
logger.Info("store opened",
    "mode", cfg.Mode,
    "dataDir", cfg.Embedded.DataDir,
)
```

⚠️ **Log the *filter*, not the *result*.** The filter is what the user
typed; it is useful for diagnosing. The result is potentially large.

⚠️ **Never log the token.** See §20.3.

## 20.6 The Makefile

```make
BINARY   := flint
CMD      := ./cmd/tui
BIN_DIR  := bin
DATA_DIR ?= ./pb_data

.PHONY: build
build: ## Build the standalone binary into ./bin
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags "-s -w" -o $(BIN_DIR)/$(BINARY) $(CMD)
	@echo "built $(BIN_DIR)/$(BINARY)"
```

⚠️ **`-trimpath` removes the absolute paths** from the binary. This
makes the build reproducible and hides your directory layout.

⚠️ **`-ldflags "-s -w"` strips the symbol table and the DWARF data.**
The binary is smaller, but a panic will not have a full stack trace.
For a development build, omit it.

### The cross build

```make
.PHONY: cross-build
cross-build: ## Build the binary for the supported platforms
	@mkdir -p $(BIN_DIR)
	@for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do \
		os=$${target%/*}; arch=$${target#*/}; \
		ext=""; if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch $(GO) build -trimpath \
			-o $(BIN_DIR)/$(BINARY)-$$os-$$arch$$ext $(CMD) || exit 1; \
	done
```

⚠️ **Cross-compiling pure Go is easy. Cross-compiling cgo is not.**
The template uses `modernc.org/sqlite`, which is a **pure Go** SQLite
implementation — so the cross build works with no C toolchain.

This is a significant advantage of the embedded mode: a single static
binary for every platform.

### The other targets

```make
.PHONY: test
test:
	$(GO) test ./...

.PHONY: test-race
test-race:
	$(GO) test -race ./...

.PHONY: lint
lint: vet golangci-lint

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: golangci-lint
golangci-lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint is not installed, skipping"; \
		exit 0; \
	}
	golangci-lint run
```

⚠️ **Make the optional tools optional.** The `golangci-lint` target
skips gracefully if the tool is not installed, so `make lint` works on
a fresh checkout.

## 20.7 The CI

```yaml
jobs:
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true
      - name: Check the formatting
        run: test -z "$(gofmt -l ./cmd ./config ./internal)"
      - name: Run go vet
        run: go vet ./...
      - name: Run golangci-lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: latest
          args: --timeout=5m

  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true
      - name: Run the tests
        run: go test -race -coverprofile=coverage.out ./...

  build:
    name: Build (${{ matrix.os }})
    runs-on: ${{ matrix.os }}
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true
      - name: Build
        run: go build -trimpath -o flint ./cmd/tui
      - name: Smoke test
        run: ./flint --help
```

### The three jobs

**Lint** — the formatting check, `go vet` and `golangci-lint`. The
formatting check is a one-liner that catches the most common CI
failure:

```bash
test -z "$(gofmt -l ./cmd ./config ./internal)"
```

⚠️ **The `gofmt -l` check needs the right paths.** If you add a
directory, add it to the list. Or use `gofmt -l .` and exclude the
vendor directory.

**Test** — with `-race` and a coverage report.

**Build** — on three platforms, with a smoke test.

⚠️ **The smoke test is important.** `./flint --help` verifies that the
binary *starts* — that the dependencies link, that the init code does
not panic. A build that succeeds but crashes on startup is a common and
embarrassing failure.

### The Go version

```yaml
env:
  GO_VERSION: "1.27"
```

⚠️ **Pin the version.** The PocketBase dependency requires Go 1.27, and
a floating `go-version: stable` would break the moment the CI image
changes.

## 20.8 The release

### The version

```go
var version = "dev"

func main() {
    // ...
}
```

Injected at build time:

```make
VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS := -s -w -X main.version=$(VERSION)
```

```go
go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)
```

### The release workflow

```yaml
name: Release

on:
  push:
    tags: ["v*"]

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.27"

      - name: Build
        run: |
          mkdir -p dist
          for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
            os=${target%/*}; arch=${target#*/}
            ext=""; if [ "$os" = "windows" ]; then ext=".exe"; fi
            GOOS=$os GOARCH=$arch go build -trimpath \
              -ldflags "-s -w -X main.version=${{ github.ref_name }}" \
              -o "dist/flint-$os-$arch$ext" ./cmd/tui
          done

      - uses: softprops/action-gh-release@v2
        with:
          files: dist/*
```

⚠️ **Build the binaries in CI, not locally.** The CI has every
platform; your machine does not.

## 20.9 The distribution

### The binary

The embedded mode gives you a **single, self-contained binary**:

```bash
$ ls -lh flint
-rwxr-xr-x 1 user user 38M flint
```

38 MB is mostly the pure-Go SQLite implementation. It is a fair price
for a zero-dependency binary.

⚠️ **Strip the binary for the release.** `-ldflags "-s -w"` removes the
symbol table and the DWARF data, which typically saves 25-30%.

### The data directory

The embedded mode creates a data directory next to the binary (or
wherever `--data-dir` points):

```
pb_data/
├── auxiliary.db
├── data.db
├── data.db-shm
├── data.db-wal
└── .notify/
```

⚠️ **The `-shm` and `-wal` files are part of the database.** If you
back up the data directory, copy all of them. Backing up only `data.db`
gives you a potentially inconsistent snapshot.

### The config file

```toml
mode = "embedded"
theme = "dark"

[embedded]
data_dir = "./pb_data"
```

### The systemd unit

```ini
[Unit]
Description=Flint TUI
After=network.target

[Service]
Type=simple
Environment=FLINT_REMOTE_URL=https://pocketbase.example.com
Environment=FLINT_REMOTE_TOKEN_FILE=/etc/flint/token
ExecStart=/usr/local/bin/flint --mode remote
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

⚠️ **A TUI in systemd needs a TTY.** If you run it as a service, you
need `StandardInput=tty` and a `TTYPath`, or you run it in a
`tmux`/`screen` session.

## 20.10 The template hygiene

If you are distributing this as a **GitHub template**, there are a few
extra steps.

### The marker file

```
.template-keep
```

GitHub's "Use this template" removes it automatically. Your cleanup
workflow can remove the rest.

### The cleanup workflow

```yaml
name: Template cleanup

on:
  push:
    branches: [main]

permissions:
  contents: write

jobs:
  cleanup:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Remove the template specific files
        run: |
          rm -f .template-keep
          rm -rf .github/workflows/template-cleanup.yml
      - name: Commit the cleanup
        run: |
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git add -A
          git diff --staged --quiet || git commit -m "chore: remove the template specific files"
          git push
```

### The checklist for the new project

- [ ] Rename the module path.
- [ ] Update the imports.
- [ ] Remove the template files.
- [ ] Rewrite the README (the badges, the description, the usage).
- [ ] Update the LICENSE.
- [ ] Replace the example adapter with your own domain.
- [ ] Update the `docs/` — keep the architecture guide, adapt the rest.
- [ ] Update the CI (the Go version, the build matrix).
- [ ] Update the Makefile (the binary name, the data dir).

## 20.11 The checklist for any application

### The code

- [ ] `go vet` is clean.
- [ ] `golangci-lint` is clean.
- [ ] `gofmt -l` is empty.
- [ ] The tests pass, with `-race`.
- [ ] The test coverage is reasonable.
- [ ] No `fmt.Println` in the library code.
- [ ] No `panic` in the event-loop path.

### The behaviour

- [ ] `ctrl+c` exits cleanly.
- [ ] The terminal is restored after an exit.
- [ ] The database is not corrupted after a `kill -9`.
- [ ] The resize works.
- [ ] The narrow terminal (80 columns) works.
- [ ] The errors are shown to the user, not swallowed.
- [ ] A database error does not crash the TUI.

### The operations

- [ ] The configuration is documented.
- [ ] The environment variables are documented.
- [ ] The secrets are not in the repository.
- [ ] The logs go to the stderr.
- [ ] The logs do not contain the token.
- [ ] The dev mode is off by default.
- [ ] The binary is reproducible (`-trimpath`).
- [ ] The CI builds on every target platform.
- [ ] The CI runs a smoke test.

## 20.12 Exercises

1. Add the `--migrate-only` flag to the CI: run it before the smoke
   test, so that a broken migration fails the build.
2. Add the version to the binary and print it in the settings page.
3. Write the release workflow with the cross build and the checksums.
4. Add a `--log-file` flag that writes the logs to a file instead of
   the stderr.
5. Add a token file support (`FLINT_REMOTE_TOKEN_FILE`) so the token
   never appears in the environment of a process listing.
6. Write a systemd unit that runs the TUI in a `tmux` session.
7. Audit the whole project against the §20.11 checklist.

## 20.13 What you learned

- The configuration precedence is **flags > env > TOML > defaults**, and
  it is implemented by the *order* of three applications.
- The **explicit config path must exist**; the implicit ones are
  best-effort.
- **Validate after resolving**, including the cross-field rules.
- **Secrets never go in the repository**, and never in the logs.
- The shutdown order is **quit → restore the terminal → close the
  store → cancel the context → exit**.
- The **logs go to the stderr**, and the dev mode is off in production.
- `-trimpath` and `-ldflags "-s -w"` make a smaller, reproducible
  binary.
- The **pure-Go SQLite** makes the cross build trivial.
- The CI needs three jobs: **lint, test (with -race), build (with a
  smoke test)**.
- A GitHub template needs a **marker file** and a **cleanup workflow**.

## 20.14 Further reading

- `config/config.go` — the loader.
- `config/flags.go` — the flags.
- `cmd/tui/main.go` — the entry point and the shutdown.
- `Makefile` — the targets.
- `.github/workflows/ci.yml` — the CI.

---

## Where to go next

You have reached the end of the book. Here are three directions.

**Build your domain adapter.** The `examples/wikidata` package is a
complete worked example: a custom renderer, a custom migration, a
label lookup cache and a custom filter transform, all without touching
a core file. Copy its structure.

**Read the source.** The best way to internalise the patterns is to
read the framework you are building on: `bubbletea/tea.go` for the
event loop, `core/app.go` for the PocketBase interface, `tools/search`
for the filter grammar.

**Extend the template.** The five extension points (the renderer, the
pages, the migrations, the sync, the theme) are where your application
begins. The `pbstore.Store` interface is the only contract you must
respect — and you should keep it stable, because downstream projects
depend on it.

Good luck, and may your terminal never block.
