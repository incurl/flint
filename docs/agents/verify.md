# Skill: verify

**Goal:** prove the change works before committing.

**Time:** ~5 minutes. Always do this.

---

## The gate

A change is done when **all** of these pass:

```bash
make ci          # fmt-check + vet + test
make lint        # golangci-lint, must report 0 issues
make test-race   # the race detector
```

If `golangci-lint` is not installed, `make lint` says so and skips it —
CI will still run it. Install it into `./bin` with:

```bash
make lint-install
```

The target fetches the release archive, verifies its checksum, and falls
back to `go install` when the download fails. `make lint` then uses the
local copy first, and the one in the `PATH` after it.

---

## Step 1 — format

```bash
gofmt -l ./cmd ./config ./internal ./examples
```

**Must print nothing.** If it prints a file:

```bash
gofmt -w ./cmd ./config ./internal ./examples
```

---

## Step 2 — vet

```bash
go vet ./...
```

Catches the mistakes the compiler does not: a `Printf` verb that does
not match its arguments, a lost `errcheck`, an unreachable branch.

---

## Step 3 — lint

```bash
golangci-lint run
```

**Must report `0 issues`.** The configuration enables the strict linters
on purpose. The three that catch the most:

### `errorlint`

```go
// WRONG: the second error is stringified, not wrapped.
fmt.Errorf("%w: %v", ErrInvalidFilter, err)

// RIGHT: both are in the chain, so errors.Is works for both.
fmt.Errorf("%w: %w", ErrInvalidFilter, err)
```

### `wsl_v5`

Requires a blank line between statement blocks. **Fix it automatically:**

```bash
golangci-lint run --fix
```

Then re-read the diff — the autofix only inserts blank lines, but
confirm it.

### `godot`

Every declaration's doc comment ends with a period.

```go
// Title returns the title of the record.   ← ✅
// Title returns the title of the record    ← ❌
```

---

## Step 4 — tests

```bash
go test ./...
go test -race ./...
```

The race detector matters because the store is called from command
goroutines while the render loop runs. A map read in `Update` and a
write in a command is a race that passes the ordinary test run.

---

## Step 5 — a behavioural check

Unit tests do not prove the feature works. Run the app.

```bash
# Embedded mode.
go run ./cmd/tui --mode embedded --data-dir /tmp/flint-verify

# The migrations apply cleanly.
go run ./cmd/tui --mode embedded --data-dir /tmp/flint-verify --migrate-only
```

For a change that adds a key, drive the app in a pty and send the key.
A simple check that the app starts and quits without crashing:

```bash
printf 'q' | timeout 5 go run ./cmd/tui --mode embedded --data-dir /tmp/flint-verify
echo "exit=$?"
```

A non-zero exit means the program crashed.

---

## Step 6 — write the test

**New behaviour needs a test.** If you fixed a bug, the test must fail
against the unfixed code. Verify that:

```bash
# 1. Comment out the fix.
# 2. Run the test — it MUST fail.
go test ./internal/pages/ -run TestRecordsPageDiscardsStaleResponse
# 3. Restore the fix.
# 4. Run the test — it MUST pass.
go test ./internal/pages/ -run TestRecordsPageDiscardsStaleResponse
```

The same guard exists for the detail page, where the identity is the
record id:

```bash
go test ./internal/pages/ -run TestDetailPageDiscardsStaleResponse
```

A test that passes both ways proves nothing.

### Test conventions in this repository

- `t.TempDir()` for anything touching the filesystem. Never a fixed
  path — a fixed path makes the test fail the second time.
- `t.Errorf` with the actual and the expected value:
  `t.Errorf("Title() = %q, want %q", got, want)`.
- `t.Fatalf` when the test cannot continue.
- Name the test after the behaviour: `TestRecordsPageDiscardsStaleResponse`,
  not `TestRecordsPage2`.

---

## Step 7 — coverage

```bash
make cover
```

A useful sanity check: if you added a function and the coverage did not
move, the function is not exercised.

---

## The full checklist

- [ ] `gofmt -l` prints nothing.
- [ ] `go vet ./...` is silent.
- [ ] `golangci-lint run` reports 0 issues.
- [ ] `go test ./...` passes.
- [ ] `go test -race ./...` passes.
- [ ] The app starts and exits cleanly.
- [ ] New behaviour has a test.
- [ ] A bug fix has a test that fails without the fix.
- [ ] The docs mention the new option or behaviour.
- [ ] `git status` shows only the files you intended to change.

---

## Before committing

```bash
git --no-pager diff --stat
```

Read the diff. The `wsl_v5` autofix touches many files with blank
lines — that is expected, but confirm it introduced no logic change:

```bash
git --no-pager diff -w --stat   # ignores whitespace
```

A large `-w` diff on files you did not intend to change means something
went wrong.

---

## What CI runs

`.github/workflows/ci.yml` runs, on Linux, macOS and Windows:

| Job   | Checks                                                      |
| ----- | ----------------------------------------------------------- |
| lint  | `gofmt -l`, `go vet ./...`, `golangci-lint run`             |
| test  | `go test -race -coverprofile=coverage.out ./...`            |
| build | `go build -trimpath` + a smoke test + a migration test     |

If your change passes locally, it passes CI — unless it depends on a
platform-specific behaviour. The build matrix is the reason the
`migrate-only` smoke test runs on all three operating systems.

