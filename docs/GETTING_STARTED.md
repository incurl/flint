# Getting started

This guide walks you through the first hour with Flint: how to run it,
how the pieces fit together, and how to turn the scaffold into your own
application.

## 1. Run it

```bash
git clone https://github.com/you/your-project.git
cd your-project

# The embedded mode needs no server: PocketBase runs inside the binary.
make run
```

The first start creates `./pb_data` with a SQLite database and applies the
PocketBase system migrations. You land on the **home** page, which lists
the available collections.

## 2. Drive the UI

| Key     | Action                                        |
| ------- | --------------------------------------------- |
| `↑`/`↓` | Move the selection                            |
| `enter` | Open the highlighted collection / record      |
| `/`     | Focus the filter input (records page)         |
| `1`–`6` | Switch page                                   |
| `r`     | Refresh                                       |
| `s`     | Health check                                  |
| `y`     | Trigger a sync (embedded mode)                |
| `esc`   | Go back                                       |
| `q`     | Quit                                          |

The status bar at the bottom always shows the store mode, the connection
health, the active collection and the sync status.

## 3. Understand the layers

The application is a strict Bubble Tea program. The chain is:

```
key press
   → internal/app        (the global hotkey layer)
      → internal/pages   (the active screen)
         → pbstore       (the data layer, only inside a tea.Cmd)
```

Two rules keep it healthy:

1. **`Update` never blocks.** A database call always happens inside a
   `tea.Cmd`, which Bubble Tea runs in a goroutine.
2. **Errors are messages, never panics.** A failing query becomes a
   `RecordsMsg{Err: ...}`, which the root model turns into a toast.

## 4. Make it yours

### 4.1 Rename the module

```bash
go mod edit -module github.com/you/your-project
```

Then update the imports:

```bash
grep -rl "github.com/incurl/flint" --include="*.go" . \
  | xargs sed -i 's|github.com/incurl/flint|github.com/you/your-project|g'
```

### 4.2 Create your collections

Add a migration in `cmd/tui/main.go`:

```go
func appMigrations() []core.Migration {
	return []core.Migration{
		{
			File: "1700000000_create_terms.go",
			Up: func(txApp core.App) error {
				collection := core.NewBaseCollection("terms")
				collection.Fields.Add(
					&core.TextField{Name: "key", Required: true},
					&core.TextField{Name: "label"},
					&core.JSONField{Name: "payload"},
				)
				return txApp.Save(collection)
			},
			Down: func(txApp core.App) error { return nil },
		},
	}
}
```

The migration runs automatically at the next start.

> A directory called `migrations/` with one file per migration is the
> PocketBase convention. You can move the definitions there — just keep
> returning them from `appMigrations()`.

### 4.3 Write a record renderer

A renderer decides how your records are summarised. It is the main
customisation point:

```go
package myapp

import (
	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
)

// TermRenderer renders a controlled vocabulary entry.
type TermRenderer struct{}

var _ components.RecordRenderer = (*TermRenderer)(nil)

func (r *TermRenderer) Title(rec *pbstore.Record) string {
	return rec.StringField("label")
}

func (r *TermRenderer) Summary(rec *pbstore.Record) string {
	return rec.StringField("key")
}

func (r *TermRenderer) Fields(rec *pbstore.Record) []components.FieldRow {
	return []components.FieldRow{
		{Name: "Key", Value: rec.StringField("key")},
		{Name: "Label", Value: rec.StringField("label")},
	}
}
```

Wire it in `cmd/tui/main.go`:

```go
model := app.New(store, cfg, &myapp.TermRenderer{})
```

The renderer is used by the record table, the record card and the search
results. You never touch the core packages.

### 4.4 Style it

Everything visual lives in `internal/theme`. Add a variant:

```go
func SolarizedPalette() Palette {
	return Palette{
		Background: "#002b36",
		Foreground: "#93a1a1",
		Primary:    "#268bd2",
		// ...
	}
}
```

and register it in `theme.New`:

```go
case VariantSolarized:
	palette = SolarizedPalette()
```

Never write an inline lipgloss style outside the theme package.

## 5. Add a page

A page is a self-contained `tea.Model`:

```go
package myapp

import (
	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/theme"
)

// StatsPage shows some statistics about the collection.
type StatsPage struct {
	theme *theme.Theme
}

// NewStatsPage creates the statistics page.
func NewStatsPage(t *theme.Theme) *StatsPage {
	return &StatsPage{theme: t}
}

// Init implements tea.Model.
func (p *StatsPage) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (p *StatsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return p, nil }

// View implements tea.Model.
func (p *StatsPage) View() tea.View {
	return tea.NewView(p.theme.Title.Render("Statistics"))
}
```

Register it in the root model (`internal/app/app.go`):

1. add a field to `Model`;
2. create it in `New`;
3. add a case to `updateActivePage` and to `View`;
4. add the hotkey in `handleGlobalKey`.

See the [`examples/wikidata/`](../examples/wikidata/) package for a
complete example.

## 6. Work with a remote server

```bash
# Start a local PocketBase server (requires the pocketbase CLI).
make pb-serve

# Point the TUI at it.
make run-remote REMOTE_URL=http://127.0.0.1:8090
```

Or use the environment:

```bash
export FLINT_MODE=remote
export FLINT_REMOTE_URL=http://127.0.0.1:8090
export FLINT_REMOTE_TOKEN=your-token
./bin/flint
```

> Keep the token out of your shell history and out of the repository.

## 7. Populate a local cache

The **sync** page (`6`) copies a remote collection into the local
embedded database:

- `remote collection` — the source collection on the server;
- `local collection` — the destination (defaults to the source name);
- `filter` — an optional native PocketBase filter.

Press `ctrl+r` to run it. The records are matched by id, so repeating the
sync refreshes them instead of duplicating them.

## 8. Before you publish

```bash
make fmt-check   # the formatting must be clean
make lint        # go vet + golangci-lint
make test        # the test suite
make cross-build # the release binaries
```

Then update:

- the **README** — the badges, the description and the usage;
- the **module path**;
- the **LICENSE**;
- the `docs/` folder — keep the architecture guide, adapt the rest.

## Where to go next

| Topic                        | Document                                        |
| ---------------------------- | ----------------------------------------------- |
| The TEA flow, the boundaries | [`ARCHITECTURE.md`](ARCHITECTURE.md)            |
| The store, the params        | [`PBSTORE_GUIDE.md`](PBSTORE_GUIDE.md)          |
| A complete domain adapter    | [`examples/wikidata/`](../examples/wikidata/)   |
