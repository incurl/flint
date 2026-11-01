# Appendix E — Further Reading

## The primary sources

The best documentation for these libraries is their source code. It is
well written, well commented, and it is the ground truth.

### Bubble Tea

| File                    | What it holds                                  |
| ----------------------- | ---------------------------------------------- |
| `tea.go`                | The `Model` interface, `Cmd`, `Msg`, `Run`      |
| `commands.go`           | `Batch`, `Sequence`, `Tick`, `Every`            |
| `key.go`                | The `Key` type, the key names, the paste marker |
| `options.go`            | The program options                             |
| `standard_renderer.go`  | The frame diffing and the writing               |
| `exec.go`               | The command execution                           |
| `focus.go`              | The focus reporting                            |

Repository: <https://github.com/charmbracelet/bubbletea>

Module path: `charm.land/bubbletea/v2`

### Bubbles

| Package      | File                    | What it holds                |
| ------------ | ----------------------- | ---------------------------- |
| `table`      | `table/table.go`        | The table model and styles   |
| `list`       | `list/list.go`          | The list model and the states |
| `list`       | `list/defaultitem.go`   | The `DefaultItem` contract   |
| `textinput`  | `textinput/textinput.go`| The text input model         |
| `textarea`   | `textarea/textarea.go`  | The text area model          |
| `viewport`   | `viewport/viewport.go`  | The scrollable viewport      |
| `spinner`    | `spinner/spinner.go`    | The spinner and the tick     |
| `paginator`  | `paginator/paginator.go`| The page dots                 |
| `key`        | `key/key.go`            | The binding system           |
| `help`       | `help/help.go`          | The key hint bar             |

Repository: <https://github.com/charmbracelet/bubbles>

Module path: `charm.land/bubbles/v2`

### Lipgloss

| File              | What it holds                                |
| ----------------- | -------------------------------------------- |
| `style.go`        | The `Style` type and the rules               |
| `position.go`     | `JoinHorizontal`, `JoinVertical`, `Place`     |
| `color.go`        | The colour types                             |
| `border.go`       | The border definitions                       |
| `set.go`          | The setters                                  |
| `get.go`          | The getters                                  |

Repository: <https://github.com/charmbracelet/lipgloss>

Module path: `charm.land/lipgloss/v2`

### PocketBase

| File                              | What it holds                          |
| --------------------------------- | -------------------------------------- |
| `core/app.go`                    | The `App` interface                    |
| `core/base.go`                    | `BaseApp`, `Bootstrap`, the hooks      |
| `core/collection_model.go`        | The `Collection` model                 |
| `core/record_model.go`            | The `Record` model                     |
| `core/field_*.go`                 | The field types                        |
| `core/fields_list.go`             | The `FieldsList`                       |
| `core/record_query.go`            | The query methods                      |
| `core/record_field_resolver.go`   | The record field resolver              |
| `core/db.go`                      | `Save`, `Delete`, `Validate`           |
| `core/migrations_list.go`         | The migration types                    |
| `tools/search/filter.go`          | The filter grammar                     |
| `tools/search/provider.go`        | The filter/sort/pagination engine      |
| `tools/search/sort.go`            | The sort syntax                        |
| `tools/store/store.go`            | The runtime key/value store            |
| `tools/hook/hook.go`              | The hook chain                         |
| `tools/dbutils`                   | The query helpers                      |
| `migrations/`                     | The system migrations, as examples      |

Repository: <https://github.com/pocketbase/pocketbase>

## The official documentation

| Resource                                     | URL                                              |
| -------------------------------------------- | ------------------------------------------------ |
| The PocketBase documentation                  | <https://pocketbase.io/docs/>                    |
| The PocketBase API reference                  | <https://pocketbase.io/docs/api-records/>         |
| The PocketBase filter syntax                  | <https://pocketbase.io/docs/api-records/#filter-syntax> |
| The PocketBase Go SDK                         | <https://pocketbase.io/docs/go-sdk/>              |
| The PocketBase JavaScript SDK                | <https://pocketbase.io/docs/js-sdk/>              |
| The PocketBase changelog                     | <https://github.com/pocketbase/pocketbase/blob/master/CHANGELOG.md> |
| The Bubble Tea tutorial                       | <https://github.com/charmbracelet/bubbletea/tree/main/examples/tutorial> |
| The Bubbles README                            | <https://github.com/charmbracelet/bubbles>        |
| The Lipgloss README                           | <https://github.com/charmbracelet/lipgloss>       |
| The Charm documentation                       | <https://charm.sh>                               |

## The tutorials worth doing

### The Bubble Tea tutorial

The official tutorial in `bubbletea/examples/tutorial` builds a
counter, then a list, then a text input, then a table. It is about
400 lines in total and it teaches the architecture properly.

**Do it in order.** Each part builds on the previous one.

### The PocketBase "everything" example

`pocketbase/examples/base` shows a full application: the migrations,
the hooks, the custom routes.

## The books and articles

| Title                                     | Where                                          |
| ----------------------------------------- | ---------------------------------------------- |
| *Elm's Architecture: the one that stuck*  | <https://blog.izettle.com/2015/01/26/teaching-elm-the-one-that-stuck/> |
| *The Elm Architecture* (the original)     | <https://guide.elm-lang.org/architecture/>     |
| *How Elm calls a function*                | <https://www.youtube.com/watch?v=5eVBO0zZ_28> |
| *The Effects of Architecture on Developer Productivity* | <https://medium.com/@hbr/startup-architecture-considerations-for-long-lived-code-4e27c0f3f0a9> |

⚠️ **The Elm architecture articles are worth reading even if you
never write Elm.** They explain *why* the architecture works, which is
what you need to know when you deviate from it.

## The TUI design resources

| Resource                                | Where                                    |
| --------------------------------------- | ---------------------------------------- |
| *The True History of the Text Terminal* | <https://thereal.info/>                   |
| *Terminal graphics with ANSI escape sequences* | <https://gist.github.com/XVilka/8346728> |
| *A Modern Terminal Emulator*            | <https://sw.kovidgoyal.net/2018/10/06/Modern-Terminal-Emulator.html> |
| The VT100 user guide                    | <https://vt100.net/docs/vt100-ug/chapter3.html> |

⚠️ **Read the escape sequence reference once.** It makes the terminal
feel like a real target rather than a magic box.

## The PocketBase deployment resources

| Resource                       | Where                                        |
| ------------------------------ | -------------------------------------------- |
| The PocketBase Go SDK          | <https://pocketbase.io/docs/go-sdk/>         |
| The superuser guide            | <https://pocketbase.io/docs/going-into-prod/> |
| The `pocketbase/tools` packages | <https://pkg.go.dev/github.com/pocketbase/pocketbase/tools> |

## The books on Go

| Title                                        | Author            |
| -------------------------------------------- | ----------------- |
| *The Go Programming Language*                | Alan A. A. Donovan and Brian W. Kernighan |
| *The Go Programming Language* (the site)      | <https://go.dev/book/> |
| *Concurrency in Go*                          | Katherine Cox-Buday |
| *The Go Programming Language* (the site)      | <https://go.dev/book/> |
| *Effective Go*                               | <https://go.dev/doc/effective_go.html> |
| *Go Code Review Comments*                    | <https://go.dev/wiki/CodeReviewComments>     |

⚠️ **Read "Effective Go"** — especially the sections on concurrency
and on error handling. They are short and they change how you write.

## The code to read

If you want to see a well-built TUI application, these are good
references:

| Project                        | What it shows                        |
| ------------------------------ | ------------------------------------ |
| `charmbracelet/glow`           | A markdown renderer for the terminal  |
| `charmbracelet/bubbletea/examples/tutorial` | The architecture, step by step |
| `charmbracelet/wish`           | The Bubble Tea server framework       |
| `pocketbase/pocketbase/examples/base` | A full PocketBase application   |
| `charmbracelet/log`            | Structured logging for a TUI          |

## The communities

| Community                | Where                                     |
| ------------------------ | ----------------------------------------- |
| The Charm Discord        | <https://charm.sh>                        |
| The PocketBase Discord   | <https://discord.gg/pocketbase>           |
| The Go Forum              | <https://forum.golangbridge.org>           |
| r/golang                 | <https://reddit.com/r/golang>             |

## The glossary

| Term                | Meaning                                                    |
| ------------------- | ---------------------------------------------------------- |
| **Alt screen**      | The separate terminal buffer a full-screen app draws on     |
| **ANSI escape**     | The control sequence that moves the cursor or sets a colour |
| **Raw mode**        | The terminal mode where the keys are delivered unprocessed  |
| **Cell**            | One character position on the screen                        |
| **Frame**           | One complete rendering of the screen                        |
| **Elm architecture**| Model / Update / View with effects as values                |
| **Effect**          | An action requested by the update function                  |
| **Message**         | An event delivered to the update function                   |
| **Command**         | A function that performs an effect and returns a message    |
| **Bubbles**         | The Charm component library                                 |
| **View**            | The string the model renders                                |
| **Daemon**          | A background process                                        |
| **Sentinel error**  | A package-level error value used with `errors.Is`           |

## The chapters of this book, at a glance

| # | Chapter                                     | The one idea                                        |
| - | ------------------------------------------- | --------------------------------------------------- |
| 1 | The Terminal Is a Program Target            | The screen is a string                              |
| 2 | The Elm Architecture                        | One state, one update, one view                    |
| 3 | Messages, Commands, and the Event Loop     | Effects are values                                  |
| 4 | Keys, Hotkeys, and the Routing Problem     | The global layer must suspend for text inputs       |
| 5 | Rendering: lipgloss, Layout, and the Frame | All styling lives in one package                    |
| 6 | Bubbles: Composition over Reinvention     | The delegate contract                               |
| 7 | Concurrency: The Purity of Update          | Commands capture values, never the model            |
| 8 | PocketBase's Mental Model                   | It is a Go library first                            |
| 9 | The App: Collections, Fields, Records      | The schema is data                                  |
| 10 | Querying: The Native Filter Grammar        | Reuse the grammar, do not invent one               |
| 11 | App.Store(): What It Really Is             | It is a cache, not a database                        |
| 12 | Hooks, Validation, and the Life Cycle      | Always call `e.Next()`                              |
| 13 | Migrations                                   | Filename order is execution order                   |
| 14 | Embedded or Server                          | Same library, two deployments                      |
| 15 | Architecture for TUI + Database Apps       | The layering and its rules                          |
| 16 | The pbstore Pattern                         | One interface, two backends                         |
| 17 | Sync, Caching, and Offline-First           | The explicit sync and its invariants                |
| 18 | Testing Terminal Applications              | No terminal required                                |
| 19 | Performance                                 | I/O in the render path is the #1 bug                |
| 20 | Shipping                                    | Configuration, shutdown, packaging                  |
