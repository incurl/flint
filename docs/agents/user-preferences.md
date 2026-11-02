# User preferences

Use this skill when a task involves a setting that belongs to **the
person using the application** rather than to the deployment: the
theme, the UI scale, the page size, a column layout preference.

## The three tiers of state

Getting this wrong is the most common mistake, so decide first:

| State                                       | Where it lives   | Why                          |
| ------------------------------------------- | ---------------- | ---------------------------- |
| Deployment configuration                    | The TOML file    | It says where the app runs.  |
| User preferences                            | The PocketBase   | It says how the app looks.   |
| Runtime state (page, collection, filter)    | Memory only      | It is what you are looking at.|

The deployment configuration cannot live in the database: the database's
location is itself part of the configuration. The runtime state must not
live there: it describes the current moment, not the user's choices.

**If a setting describes the person, it is a preference.**

## What already exists

The theme, the UI scale and the page size are already implemented.

```go
import "github.com/incurl/flint/internal/pbstore"

// The preferences of the current user.
type Preferences struct {
	Theme   string // "dark" or "light"
	Scale   int    // the UI scale, as a percentage
	PerPage int    // 0 means the deployment default
}

func LoadPreferences(ctx context.Context, store Store) (Preferences, error)
func SavePreferences(ctx context.Context, store Store, prefs Preferences) error
```

The collection is `_flint_preferences`. It holds a single record, and
`SavePreferences` updates it in place, so it is idempotent.

## Adding a preference

Four steps. The example adds a "compact rows" preference.

### 1. Add the field to `pbstore.Preferences`

```go
type Preferences struct {
	Theme       string
	Scale       int
	PerPage     int
	CompactRows bool
}
```

### 2. Add the field to the collection

In `internal/pbstore/preferences.go`, the `SavePreferences` function
lists the fields of the collection:

```go
if _, err := store.EnsureDictionaryCollection(ctx, PreferencesCollection, []DictionaryField{
	{Name: "theme", Type: FieldTypeText},
	{Name: "scale", Type: FieldTypeNumber},
	{Name: "per_page", Type: FieldTypeNumber},
	{Name: "compact_rows", Type: FieldTypeBool},
}); err != nil {
```

The field types are the ones of `pbstore.DictionaryField`: `text`,
`editor`, `json`, `number`, `bool`, `date`, `select`.

### 3. Read it in `LoadPreferences`

```go
if value, ok := record.Field("compact_rows"); ok {
	prefs.CompactRows = toBool(value)
}
```

You will need a `toBool` helper next to the existing `toInt`.

### 4. Apply it

The preferences are applied when the model is built, in
`internal/app/app.go`:

```go
prefs := pbstore.Preferences{
	Theme:   cfg.Theme,
	Scale:   int(theme.ScaleDefault),
	PerPage: cfg.PerPage,
}

if stored, err := pbstore.LoadPreferences(context.Background(), store); err == nil {
	if stored.Theme != "" {
		prefs.Theme = stored.Theme
	}
	// ...
}
```

And persisted when the user changes it, in
`internal/app/preferences.go`:

```go
func (m *Model) persistPreferences() {
	if m.store == nil {
		return
	}

	if err := pbstore.SavePreferences(context.Background(), m.store, m.prefs); err != nil {
		if !errors.Is(err, pbstore.ErrPreferencesUnavailable) {
			slog.Error("failed to persist the preferences", "error", err)
		}
	}
}
```

## The rules

**The remote backend cannot hold the preferences.** It cannot create a
collection, so `SavePreferences` returns `ErrPreferencesUnavailable` and
the change is not persisted. The application treats that as normal and
stays silent — a user on the remote backend can still change the theme,
it just will not survive a restart.

**Do not fail the operation when the persistence fails.** The
preferences are a convenience. A failure to store them must not break
the interaction.

**Update the settings page.** The settings page shows the effective
values, not the deployment configuration. `refreshSettings` in
`internal/app/preferences.go` merges the live preferences into the rows,
and it must be called after every change:

```go
m.prefs.Scale = int(next)
m.theme.Apply(m.theme.Variant, next)

m.persistPreferences()
m.refreshSettings()
```

## Changing the theme at runtime

The theme is shared by **pointer** and mutated **in place**:

```go
m.theme.Apply(theme.VariantLight, theme.Scale(120))
```

Every page holds the same pointer and reads the styles when it renders,
so the change appears on the next frame without rebuilding anything.

**Never replace the pointer.** If you assign a new `*theme.Theme` to the
model, the pages keep the old one and nothing changes on screen.

## Changing the UI scale

The scale is a percentage between 50 and 200. It multiplies the sizes
the theme controls: the padding of the panels and the status bar.

```go
scale := theme.Scale(m.prefs.Scale)
if !scale.Valid() {
	scale = theme.ScaleDefault
}

next := theme.ScaleZoomIn(scale)
if next == scale {
	// The scale is at its bound: say so instead of doing nothing
	// silently.
	return m.toast.Show(components.ToastInfo, "the zoom is already at "+scale.String())
}
```

The zoom hotkeys are in the global key layer of `internal/app/app.go`,
next to the theme hotkey. They are suspended while a text input has the
focus, so that a "+" typed in the filter is a "+" and not a zoom.

The `c` hotkey toggles the compact record list. It follows the same
pattern: change the preference, apply it to the runtime, persist it and
refresh the settings page.

```go
func (m *Model) toggleCompact() tea.Cmd {
	m.prefs.Compact = !m.prefs.Compact
	m.runtime.Compact = m.prefs.Compact

	m.persistPreferences()
	m.refreshSettings()

	return m.toast.Show(components.ToastSuccess, "compact rows")
}
```

## Testing

The preferences are tested at two levels.

**The store** — `internal/pbstore/preferences_test.go` covers the empty
store, the save and load round trip, the idempotence and the survival
across a reopen.

**The application** — `internal/app/preferences_test.go` covers the
hotkeys, the bounds, the theme switch and the fact that the zoom is not
applied while the user is typing.

```bash
go test ./internal/pbstore/ ./internal/app/ ./internal/theme/
```

## The gotchas

- **The settings rows accumulate.** `refreshSettings` must drop the
  previous row before appending the new one, or the row appears twice
  after every change.
- **The scale row is not a configuration row.** It is added by
  `refreshSettings`, not by `config.SettingsRows`.
- **`toInt` and `toBool` are needed.** The values arrive from the
  database as `any`, and their concrete type depends on the backend.
