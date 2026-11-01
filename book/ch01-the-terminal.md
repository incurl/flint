# Chapter 1 — The Terminal Is a Program Target

## 1.1 What a terminal actually is

Before writing a line of TUI code, it is worth being precise about what
you are programming against, because most "the terminal is weird"
complaints come from an imprecise model.

A terminal emulator is a program that owns a rectangular grid of
character cells. It reads a stream of bytes, interprets most of them as
**escape sequences**, and mutates its grid accordingly. It also writes
bytes back to the host, which is how your program receives keystrokes.

Concretely, a terminal is:

- a **grid of cells**, each holding a rune plus a set of attributes
  (foreground colour, background colour, bold, underline, …);
- a **byte stream interpreter** for control sequences;
- a **bidirectional pipe** — bytes in, bytes out.

Three consequences follow, and they drive every design decision in this
book:

### Consequence 1: The screen is a string

Because the terminal is a cell grid, and because you address it by
writing text, the natural representation of a frame in Go is a `string`.
This is why Bubble Tea's `View()` returns a `string` and not a tree of
nodes.

This also means that **layout is string arithmetic**. To place two
things side by side, you concatenate them. To place them in columns, you
need to know their rendered widths — which is why the terminal is
*not* monospaced in the naive sense (wide CJK characters occupy two
cells) and why `lipgloss.Width` exists.

### Consequence 2: The screen is not a canvas you can poke

You cannot say "set the colour of cell (10, 5)". You can only append
bytes to a stream. Every library that tries to give you a canvas ends up
emulating one on top of the byte stream.

The practical consequence: **you re-render the whole frame** (or a
sufficiently large part of it) whenever something changes. Bubble Tea
diffs the previous frame against the new one to minimise the bytes it
writes, but from your perspective you always produce a complete frame.

### Consequence 3: Terminals are a moving target

The escape sequence vocabulary is huge and inconsistent. There are
"standard" sequences (ANSI), "VT100" sequences, "xterm" extensions,
and terminal-specific extensions on top. A program that assumes a
fancy feature may render as garbage elsewhere.

The practical consequence: **be conservative, and let the libraries
handle it.** `lipgloss` and the `charmbracelet/x/ansi` package know how
to degrade. Your application code should express intent
("this is a heading") rather than mechanics ("emit `\x1b[1m`").

## 1.2 What makes a terminal a good target

It is worth asking why people build TUIs at all, when there is a web.

| Property                | Terminal                            | Web                        |
| ----------------------- | ----------------------------------- | -------------------------- |
| Latency to first paint  | Milliseconds                        | Tens of milliseconds        |
| Input                   | Immediate, per keystroke             | Needs focus, per event      |
| Layout                   | Fixed grid                          | Fluid, reflows              |
| Richness                | Limited but *consistent*             | Everything                 |
| Distribution             | A single binary                     | A server + a browser       |
| Offline                  | Trivial                             | Needs a service             |
| Accessibility            | Screen readers work well             | Excellent                   |
| Text selection / copy   | Awkward but possible                | Native                      |

Two properties matter enormously for the class of application this book
is about — **data dictionaries, lookup caches, metadata browsers** — and
both of them are terminal strengths:

1. **Instantaneous input.** A lookup tool lives or dies on how fast you
   can type a query and see results. A browser round-trip per keystroke
   is not acceptable; a local process with an in-memory index is.
2. **Single-binary distribution.** A reference-data browser that
   downloads 40 MB of vocabulary data and runs offline is a *tool*. The
   same thing as a web app is an *infrastructure project*.

## 1.3 Why Bubble Tea

There are older Go TUI libraries. Bubble Tea (and the wider Charm
ecosystem) took a different approach: it borrowed the architecture of
Elm, a language whose entire value proposition is that it makes
event-loop-driven UI code boring and predictable.

The result is a library where:

- the entire program state is one value;
- all input flows through a single function;
- all I/O is expressed as values that produce messages;
- rendering is a pure function of state.

This is a **massive** win for the kind of application we are building,
because the hard part is not drawing boxes. The hard part is that you
have a database, a network, a background sync, a modal dialog, and a
status bar, and all of them want to update the UI. Bubble Tea's
one-way data flow makes "who is allowed to change what" answerable.

Let me state the central claim of this book precisely:

> **A TUI application is a state machine whose transitions are triggered
> by messages, and whose expensive work happens outside the state
> machine.**

Everything else — routing, composition, error handling, testing — falls
out of that.

## 1.4 Your first program

Let us write the smallest useful Bubble Tea program and then grow it.

```go
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

type model struct {
	presses int
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case " ":
			m.presses++
		}
	}
	return m, nil
}

func (m model) View() string {
	return fmt.Sprintf("Pressed %d times. (q to quit)", m.presses)
}

func main() {
	p := tea.NewProgram(model{})
	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
```

Run it:

```bash
go run .
```

Press space a few times, then `q`.

Now let us take it apart, because every part matters.

### `Init() tea.Cmd`

The first function called. It returns an *optional* initial command.
Returning `nil` is fine and means "nothing to do at startup".

In a real application, `Init` is where you kick off the first load:

```go
func (m model) Init() tea.Cmd {
    return tea.Batch(
        m.spinner.Tick,     // start the loading animation
        m.loadRecords(),    // fetch the first page
    )
}
```

⚠️ **A common mistake.** `Init` is called *once*. If a page is
constructed later (say, when the user navigates to it), its `Init` is
*never* called. This bites people who build multi-page applications:
the second page's spinner never starts.

The fix, used throughout the Flint template, is to give every page an
explicit `Refresh()` that starts the spinner and loads the data, and to
call it when the page becomes visible. Chapter 15 shows the pattern.

### `Update(msg tea.Msg) (tea.Model, tea.Cmd)`

The heart of the program. It receives a message and returns:

1. the (possibly modified) model;
2. an optional command to run.

Note the receiver: `func (m model) Update(...)`. It is a **value
receiver**, and it returns a new `tea.Model`. This is the Elm
influence: state is immutable in the sense that you do not mutate it in
place and rely on side effects; you return a modified copy.

That said, Go's slices and maps are reference types, so "immutable" is
a discipline you follow, not a guarantee the compiler enforces. The
discipline is: **when you change a collection, replace it, don't mutate
it in place.** Otherwise a `tea.Cmd` that captured the old slice will
observe your "change".

```go
// ✗ Mutating in place — the old slice is aliased everywhere.
m.items = append(m.items, item)

// ✓ Replacing — the old slice is untouched.
m.items = append(append([]string{}, m.items...), item)
```

In practice, Bubble Tea programs in Go mutate the model through a
pointer receiver (`func (m *Model) Update(...)`) and it works fine.
The value-receiver style is idiomatic Elm; the pointer style is
idiomatic Go. Chapter 2 discusses when each is better.

### `View() string`

Renders the current state. It is called after every `Update`.

Two rules:

1. **`View` must be fast.** It is called on every message. If your view
   does JSON marshalling or a database query, the UI will stutter.
2. **`View` must not have side effects.** It should be a pure function
   of the state.

The rule about side effects is not stylistic. A `View` that mutates
state creates a feedback loop that is very hard to debug, because the
timing depends on how fast the terminal is.

### `tea.NewProgram(model{})` and `p.Run()`

`NewProgram` wires up the terminal (raw mode, signal handling, the
renderer). `Run` starts the event loop and blocks until the program
quits.

The two options you will use constantly:

```go
p := tea.NewProgram(model{},
    tea.WithAltScreen(),        // use the alternate screen buffer
    tea.WithMouseCellMotion(),  // enable mouse reporting
)
```

`WithAltScreen` is the single most important option for a full-screen
application. Without it, your program draws over the user's shell
history and leaves the terminal in a mess on exit. With it, the
terminal's original content is restored when you quit, and your UI
occupies a separate buffer that you can scribble on freely.

## 1.5 The event loop

Let us look at what `Run` actually does, because understanding it
explains most Bubble Tea behaviour.

```
                    ┌──────────────────────────────────────┐
                    │                                      │
  terminal input ──▶│  input reader goroutine              │
                    │      │                               │
                    │      ▼                               │
                    │  msg channel                         │
                    │      │                               │
                    │      ▼                               │
                    │  ┌────────────────────────────────┐  │
                    │  │  eventLoop:                    │  │
                    │  │    for msg := range msgs {     │  │
                    │  │      model, cmd = model.Update(msg) │ │
                    │  │      cmds <- cmd                │  │
                    │  │      renderer.write(model.View()) │ │
                    │  │    }                            │  │
                    │  └────────────────────────────────┘  │
                    │      │            │                   │
                    │      │            ▼                   │
                    │      │      cmd goroutines            │
                    │      │            │                   │
                    │      │            ▼                   │
                    │      └──▶ msg channel ◀──────────────┘
                    │                                      │
                    └──────────────────────────────────────┘
```

Three consequences:

### The event loop is single-threaded

`Update` is called from one goroutine. That means **you do not need a
mutex** to protect your model state, as long as only `Update` touches
it. This is a huge simplification and one of the main reasons to adopt
Bubble Tea.

But it also means: if `Update` blocks, the entire UI freezes. No
keystrokes are processed, no rendering happens. This is the single
biggest cause of "my TUI hangs".

### Commands run in goroutines

A `tea.Cmd` is `func() Msg`. Bubble Tea runs it in a goroutine and
sends the returned message back to the event loop. This is how you do
I/O without blocking the UI.

```go
func (m model) loadRecords() tea.Cmd {
    return func() tea.Msg {
        // This runs in a goroutine. It may block.
        records, err := store.List(ctx, query)
        if err != nil {
            return loadFailedMsg{err}
        }
        return recordsLoadedMsg{records}
    }
}
```

⚠️ **The race this creates.** The command goroutine reads from the
database while the event loop mutates the model. If the command
captured a pointer to the model and read it later, you have a data
race. The rule: **a command must capture only immutable values** — the
store handle, the query parameters — and never the model itself.

### Rendering happens after every update

`View()` is called after each `Update`. If your app receives a stream of
messages (a spinner tick, a progress update), you re-render each time.
Bubble Tea's renderer diffs consecutive frames so it only writes the
bytes that changed, but `View()` itself still runs every time.

This is why `View` must be cheap.

## 1.6 A first taste of messages

The `tea.Msg` type is `any`. Any value can be a message. The
convention is to define named types for the events your application
cares about:

```go
type recordsLoadedMsg struct {
    records []*pbstore.Record
}

type loadFailedMsg struct {
    err error
}

type syncFinishedMsg struct {
    report *pbstore.SyncReport
}
```

Then handle them by type switch:

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case recordsLoadedMsg:
        m.records = msg.records
        m.loading = false
    case loadFailedMsg:
        m.err = msg.err
        m.loading = false
    }
    return m, nil
}
```

Note the shadowing: `switch msg := msg.(type)` binds `msg` to the
concrete type inside each case. This is idiomatic Go and keeps the
cases readable.

### The rule that saves you

> **Never let an error escape as a panic. Turn it into a message.**

A database call in a command can fail. The command returns a message
containing the error. `Update` turns that into state (an error banner,
a toast) and possibly another message.

If you instead panic inside a command goroutine, Bubble Tea will
recover it and quit your program with a stack trace — your UI vanishes
and the user sees nothing useful.

The Flint template centralises this in the root model:

```go
case pages.RecordsMsg:
    if msg.Err != nil {
        m.runtime.SetError(msg.Err)
        return m, m.toast.Show(components.ToastError, msg.Err.Error())
    }
    m.runtime.SetError(nil)
```

One place where errors become visible, rather than being handled
differently in each page.

## 1.7 Exercises

1. **Modify the counter program** so that `q` quits but `ctrl+c` is
   handled by the terminal (i.e. you do not intercept it). What happens
   when you press `ctrl+c`? Why might you *want* that behaviour?

2. **Add a `tea.WindowSizeMsg` handler** that stores the terminal
   dimensions in your model, and display them. Where does the message
   come from?

3. **Make `View()` slow on purpose** — add a `time.Sleep(50 *
   time.Millisecond)` inside it. Press space repeatedly. What do you
   observe? Relate it to the "rendering happens after every update"
   rule.

4. **Remove `tea.WithAltScreen()`** from a program that draws a full
   screen, run it, then quit. Describe what the terminal looks like
   afterwards. Then restore it.

5. Write a program whose `View` calls `os.Getenv` a thousand times.
   Measure how long it takes to redraw. Now write one that reads the
   environment once in `Init` and stores it in the model. Compare.

## 1.8 What you learned

- A terminal is a **cell grid** driven by a **byte stream**; layout is
  string arithmetic and the whole frame is re-rendered on change.
- Bubble Tea implements the **Elm architecture**: `Model`, `Update`,
  `View`, with all input flowing through one function.
- `Init` returns an optional command; `Update` returns the new model
  and an optional command; `View` returns a string.
- The event loop is **single-threaded**; `tea.Cmd` runs in a
  **goroutine** and reports back with a message.
- `View` must be **fast and side-effect free**.
- **Errors travel as messages**, never as panics.

## 1.9 Further reading

Read these files in the Bubble Tea source, in this order:

- `tea.go` — the `Model` interface, `Cmd`, `Msg`, and `Run`.
- `commands.go` — `Batch`, `Sequence`, `Tick`, `Every`.
- `key.go` — the `Key` struct and `String()`.
- `options.go` — `WithAltScreen` and friends.
- `standard_renderer.go` — how frames are diffed and written.

Next: [Chapter 2 — The Elm Architecture](ch02-elm-architecture.md).
