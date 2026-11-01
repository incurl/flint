# Skill: add a config option

**Goal:** add a setting the user can control from a file, an environment
variable or a flag.

**Time:** ~10 minutes. Touches 3 files.

---

## The precedence rule

```
CLI flag  >  environment variable  >  TOML file  >  default
```

A new option must work at **all three levels**. An option that only
works in the TOML file is a half-implemented option.

---

## Step 1 — the config struct

In `config/config.go`, add the field to `Config` or to the relevant
sub-struct:

```go
type Config struct {
	// Mode is the store mode: "embedded" or "remote".
	Mode string `toml:"mode"`
	// … the existing fields

	// Display selects the record renderer.
	//
	// It is one of "default" or "articles".
	Display string `toml:"display"`
}
```

The `toml` tag is the key in the config file. Use `snake_case`.

---

## Step 2 — the default

In `config.Default()`:

```go
func Default() *Config {
	return &Config{
		Mode:    string(pbstore.ModeEmbedded),
		Theme:   string(theme.VariantDark),
		PerPage: 30,
		Display: "default",
		// …
	}
}
```

---

## Step 3 — the environment variable

In `applyEnv()`:

```go
if v := os.Getenv(EnvPrefix + "DISPLAY"); v != "" {
	cfg.Display = v
}
```

The prefix is `FLINT_` (the `EnvPrefix` constant). The variable name is
the uppercased option name.

---

## Step 4 — the flag

In `config/flags.go`:

```go
// Display overrides the record renderer.
Display string
```

```go
fs.StringVar(&flags.Display, "display", "", "record renderer: default or articles")
```

Then in `applyFlags()`:

```go
if flags.Display != "" {
	cfg.Display = flags.Display
}
```

**The flag default must be the empty string.** A non-zero default would
override the TOML file on every run, because the loader cannot tell
"not provided" from "provided with this value".

---

## Step 5 — validate it

In `Config.Validate()`:

```go
switch c.Display {
case "default", "articles":
default:
	return fmt.Errorf("config: invalid display %q (expected %q or %q)",
		c.Display, "default", "articles")
}
```

**Validate early.** A bad value should fail at startup with a clear
message, not deep inside a renderer.

---

## Step 6 — use it

In `cmd/tui/main.go`:

```go
var renderer components.RecordRenderer

switch cfg.Display {
case "articles":
	renderer = domain.NewArticleRenderer(true)
default:
	renderer = nil // the default renderer
}

model := app.New(store, cfg, renderer)
```

---

## Step 7 — document it

In `README.md`, add a row to the flags table:

```markdown
| `--display`          | `FLINT_DISPLAY`            | `default`        |
```

And in the config example:

```toml
display = "articles"
```

---

## Step 8 — test it

```go
// TestConfigDisplay verifies that the display option is loaded.
func TestConfigDisplay(t *testing.T) {
	cfg, err := config.Load("", config.Flags{Display: "articles"})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Display != "articles" {
		t.Errorf("Display = %q, want %q", cfg.Display, "articles")
	}
}

// TestConfigInvalidDisplay verifies that an invalid value is rejected.
func TestConfigInvalidDisplay(t *testing.T) {
	_, err := config.Load("", config.Flags{Display: "nonsense"})
	if err == nil {
		t.Error("Load() accepted an invalid display value")
	}
}
```

The existing `config/config_test.go` shows the pattern for each level.

---

## Verify

```bash
gofmt -w ./config
go build ./... && go test ./...
golangci-lint run

# Try it end to end.
FLINT_DISPLAY=articles flint --mode embedded
```

---

## Checklist

- [ ] The field is in the config struct with a `toml` tag.
- [ ] `Default()` returns a sane value.
- [ ] `applyEnv()` reads the `FLINT_`-prefixed variable.
- [ ] The flag default is `""` (or `0` / `false`).
- [ ] `applyFlags()` applies it only when set.
- [ ] `Validate()` rejects invalid values.
- [ ] The README documents the flag, the variable and the default.
- [ ] A test covers each level.
