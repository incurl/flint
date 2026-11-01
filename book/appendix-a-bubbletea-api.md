# Appendix A — Bubble Tea API Reference

A quick reference for the Bubble Tea v2 API. Every signature here
was verified against the source of `charm.land/bubbletea/v2@v2.1.1`.

⚠️ **Bubble Tea v2 is a major rewrite.** The module path is now
`charm.land/bubbletea/v2` (not `github.com/charmbracelet/bubbletea`),
and the `View` method, the key messages and the screen options all
changed shape. See the "Migrating from v1" section at the end of this
appendix.

## The core types

```go
// Msg contains data from the result of an IO operation.
type Msg interface{}

// Model contains the program's state and its core functions.
type Model interface {
    Init() Cmd
    Update(Msg) (Model, Cmd)
    View() View
}

// View represents a terminal view: styled content plus the screen
// options that apply to it.
type View struct {
    Content         string
    AltScreen       bool
    MouseMode       MouseMode
    Cursor          *Cursor
    BackgroundColor color.Color
    ForegroundColor color.Color
}

// NewView builds a View from a styled string.
func NewView(s string) View

// Cmd is an IO operation that returns a message when it's complete.
type Cmd func() Msg
```

## The commands

```go
func Quit() Msg
func Batch(cmds ...Cmd) Cmd
func Sequence(cmds ...Cmd) Cmd
func Tick(d time.Duration, fn func(time.Time) Msg) Cmd
func Every(duration time.Duration, fn func(time.Time) Msg) Cmd
func Println(args ...interface{}) Cmd
func Printf(format string, args ...interface{}) Cmd
```

### Batch vs Sequence

| | Order | Concurrency |
| --- | --- | --- |
| `Batch` | unordered | concurrent |
| `Sequence` | ordered | sequential |

Use `Batch` for independent work, `Sequence` when there is a
dependency.

## The program

```go
func NewProgram(model Model, opts ...ProgramOption) *Program

func (p *Program) Run() (returnModel Model, returnErr error)
func (p *Program) Start() error
func (p *Program) StartReturningModel() (Model, error)
func (p *Program) Send(msg Msg)
func (p *Program) Quit()
func (p *Program) Kill()
func (p *Program) Wait()
func (p *Program) Println(args ...interface{})
func (p *Program) Printf(format string, args ...interface{})
func (p *Program) ReleaseTerminal() error
func (p *Program) RestoreTerminal() error
```

## The options

```go
func WithContext(ctx context.Context) ProgramOption
func WithInput(input io.Reader) ProgramOption
func WithOutput(output io.Writer) ProgramOption
func WithEnvironment(env []string) ProgramOption
func WithFilter(filter func(Model, Msg) Msg) ProgramOption
func WithFPS(fps int) ProgramOption
func WithColorProfile(profile colorprofile.Profile) ProgramOption
func WithWindowSize(width, height int) ProgramOption
func WithoutSignalHandler() ProgramOption
func WithoutCatchPanics() ProgramOption
func WithoutSignals() ProgramOption
func WithoutRenderer() ProgramOption
```

### The screen options moved to the view

In v2 the alternate screen and the mouse mode are **properties of the
rendered view**, not options of the program. Set them on every frame:

```go
func (m model) View() tea.View {
    v := tea.NewView(m.render())
    v.AltScreen = true
    v.MouseMode = tea.MouseModeCellMotion

    return v
}

p := tea.NewProgram(model)
```

⚠️ **The mouse takes the selection away from the user.** If copying
text matters, leave `MouseMode` at `tea.MouseModeNone`.

## The messages

### The built-in ones

```go
type QuitMsg struct{}
type SuspendMsg struct{}
type ResumeMsg struct{}
type InterruptMsg struct{}

type WindowSizeMsg struct {
    Width  int
    Height int
}

// KeyMsg is an interface satisfied by both the press and the release
// events.
type KeyMsg interface {
    fmt.Stringer
    Key() Key
}

type KeyPressMsg Key
type KeyReleaseMsg Key

type MouseMsg interface {
    fmt.Stringer
    Mouse() Mouse
}

type FocusMsg struct{}
type BlurMsg struct{}
type PasteMsg string
```

## The key type

```go
type Key struct {
    Text        string
    Mod         KeyMod
    Code        rune
    ShiftedCode rune
    BaseCode    rune
    IsRepeat    bool
}

func (k Key) String() string
func (k Key) Keystroke() string
```

`String()` returns the text of the key when there is one ("a", "2",
"/"), and the canonical name of the special key otherwise ("enter",
"ctrl+c", "up"). It is the stable way to match a key.

### The key names

`String()` returns these stable names:

| Category   | Names                                                             |
| ---------- | ----------------------------------------------------------------- |
| Printable  | the character: `"a"`, `"Z"`, `"7"`, `" "`                       |
| Control    | `"ctrl+a"` … `"ctrl+z"`, `"ctrl+space"`, `"ctrl+backslash"`       |
| Named      | `"enter"`, `"esc"`, `"tab"`, `"shift+tab"`, `"backspace"`, `"delete"` |
| Arrows     | `"up"`, `"down"`, `"left"`, `"right"`                             |
| Navigation | `"home"`, `"end"`, `"pgup"`, `"pgdown"`                           |
| Alt        | `"alt+left"`, `"alt+f"`                                           |

### The control key aliases

⚠️ **Some control characters are aliases of the named keys.** The
`keyNames` map resolves them to the friendly name:

| Character | `String()` returns | Also |
| --------- | ------------------ | ---- |
| `ctrl+i`  | `"tab"`            | `0x09` |
| `ctrl+j`  | `"ctrl+j"`         | `0x0a` |
| `ctrl+k`  | `"ctrl+k"`         | `0x0b` |
| `ctrl+l`  | `"ctrl+l"`         | `0x0c` |
| `ctrl+m`  | `"enter"`          | `0x0d` |
| `ctrl+h`  | `"backspace"`      | `0x08` |
| `esc`     | `"esc"`            | `0x1b` |

⚠️ **A terminal in raw mode sends `0x0d` (CR) for Enter.** Some
terminals send `0x0a` (LF) instead. Bubble Tea normalises both to
`"enter"`.

### Pasted text

In v2 a paste is **not** a key event. It arrives as its own message:

```go
type PasteMsg struct {
    Text string
}

type PasteStartMsg struct{}
type PasteEndMsg struct{}
```

Handle it separately from the key handling:

```go
case tea.PasteMsg:
    m.input += msg.Text
```

The v1 approach — flagging a key with `Paste` and wrapping it in
brackets so that it does not match a plain key — is gone.

## The key constants

The `KeyType` enum is gone: the key is now described by its `Code`
and its `Text`. The constants that remain:

```go
KeyUp, KeyDown, KeyRight, KeyLeft, KeyShiftTab,
KeyHome, KeyEnd, KeyPgUp, KeyPgDown, KeyDelete, KeyInsert,
KeySpace, KeyEnter, KeyBackspace, KeyEsc, KeyEscape, KeyTab,
KeyF1 … KeyF12,
KeyCtrlA … KeyCtrlZ,
...
```

Use `msg.Code` to compare against them:

```go
case tea.KeyPressMsg:
    if msg.Code == tea.KeyEnter {
        ...
    }
```

Matching on `msg.String()` remains the simplest option for the common
cases ("enter", "ctrl+c", "up", "a").

## The Bubbles packages

| Package        | Type                        | Key methods                       |
| -------------- | --------------------------- | --------------------------------- |
| `textinput`    | `textinput.Model`           | `Focus`, `Blur`, `Value`, `SetValue`, `Reset`, `CursorEnd` |
| `textarea`     | `textarea.Model`            | `Focus`, `Blur`, `Value`, `SetValue`, `SetWidth`, `SetHeight` |
| `table`        | `table.Model`               | `SetRows`, `SetColumns`, `SetStyles`, `MoveUp`, `MoveDown`, `Cursor`, `SetWidth`, `SetHeight` |
| `list`         | `list.Model`                | `SetItems`, `SetItem`, `SetSize`, `SetFilterState`, `VisibleItems` |
| `viewport`     | `viewport.Model`            | `SetContent`, `SetWidth`, `SetHeight`, `GotoTop`, `GotoBottom`, `LineDown` |
| `spinner`      | `spinner.Model`             | `Tick`, `Update`, `SetSpinner`     |
| `paginator`    | `paginator.Model`           | `TotalPages`, `Page`, `PerPage` (exported fields in v1.0.0) |
| `progress`     | `progress.Model`            | `SetPercent`, `ViewAs`            |
| `help`         | `help.Model`                | `ShortKeyView`, `FullHelpView`    |
| `key`          | `key.Binding`               | `NewBinding`, `Matches`           |
| `cursor`       | `cursor.Model`              | `Focus`, `Blink`                  |
| `filepicker`   | `filepicker.Model`          | `Dir`, `Selected`                 |

## The `key` package

```go
type Binding struct {
    keys         []string
    help         [2]string
    disabled     bool
    withHelpText bool
}

func NewBinding(keys ...string) Binding
func (b Binding) WithKeys(keys ...string) Binding
func (b Binding) WithHelp(key, desc string) Binding
func (b Binding) WithHelp(key string, desc string) Binding
func (b Binding) Disabled() Binding
func (b Binding) Enabled() bool
func (b Binding) Help() [2]string
func (b Binding) Keys() []string
func (b Binding) FullHelp() [][]string
func (b Binding) ShortHelp() []string

func Matches(msg tea.KeyMsg, b ...Binding) bool
```

### The example

```go
type keyMap struct {
    Up    key.Binding
    Down  key.Binding
    Quit  key.Binding
}

func defaultKeyMap() keyMap {
    return keyMap{
        Up: key.NewBinding(
            key.WithKeys("up", "k"),
            key.WithHelp("↑/k", "up"),
        ),
        Down: key.NewBinding(
            key.WithKeys("down", "j"),
            key.WithHelp("↓/j", "down"),
        ),
        Quit: key.NewBinding(
            key.WithKeys("q", "ctrl+c"),
            key.WithHelp("q", "quit"),
        ),
    }
}
```

## The lipgloss essentials

```go
// Styles.
func NewStyle() Style
func (s Style) Foreground(c TerminalColor) Style
func (s Style) Background(c TerminalColor) Style
func (s Style) Bold(v bool) Style
func (s Style) Italic(v bool) Style
func (s Style) Underline(v bool) Style
func (s Style) Width(i int) Style
func (s Style) Height(i int) Style
func (s Style) MaxWidth(i int) Style
func (s Style) MaxHeight(i int) Style
func (s Style) Padding(i ...int) Style
func (s Style) Margin(i ...int) Style
func (s Style) Border(b Border, sides ...bool) Style
func (s Style) BorderForeground(c TerminalColor) Style
func (s Style) Align(p ...Position) Style
func (s Style) Render(strs ...string) string
func (s Style) Inherit(s Style) Style

// Colours.
func Color(s string) TerminalColor
func AdaptiveColor{Light, Dark string}

// Layout.
func JoinHorizontal(pos Position, strs ...string) string
func JoinVertical(pos Position, strs ...string) string
func Width(s string) int
func Height(s string) int
func Place(w, h int, pos Position, str string) string
```

### The style rules order

Margin → Border → Padding → Alignment → Sizing.

### The positions

```go
Position(0)  // Top / Left
Position(1)  // Center / Right
Position(2)  // Bottom
```

## The `bubbles/key` help

```go
func (m Model) ShortHelpView(bindings []key.Binding) string
func (m Model) FullHelpView(groups [][]key.Binding) string
```

## A complete skeleton

```go
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

type model struct {
	choices  []string
	cursor   int
	selected string
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "enter":
			m.selected = m.choices[m.cursor]
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	s := "Choose an option:\n\n"
	for i, choice := range m.choices {
		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}
		s += fmt.Sprintf("%s %s\n", cursor, choice)
	}

	v := tea.NewView(s + "\n(enter to select, q to quit)")
	v.AltScreen = true

	return v
}

func main() {
	p := tea.NewProgram(model{choices: []string{"one", "two", "three"}})
	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
```

## Migrating from v1

The changes that actually bite, in the order you meet them.

### 1. The module path

```go
// Before
import tea "github.com/charmbracelet/bubbletea"
import "github.com/charmbracelet/bubbles/table"
import "github.com/charmbracelet/lipgloss"

// After
import tea "charm.land/bubbletea/v2"
import "charm.land/bubbles/v2/table"
import "charm.land/lipgloss/v2"
```

The v2 modules live under Charm's own domain, not under
`github.com/charmbracelet/`. Asking for the GitHub path fails with a
"module declares its path as" error.

### 2. `View()` returns a `tea.View`

```go
// Before
func (m model) View() string {
	return m.render()
}

// After
func (m model) View() tea.View {
	return tea.NewView(m.render())
}
```

When a component's view is embedded into another string, take its
`Content`:

```go
b = append(b, p.table.View().Content)
```

### 3. The screen options moved to the view

```go
// Before
p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

// After
func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	return v
}

p := tea.NewProgram(model)
```

### 4. The key messages

`tea.KeyMsg` is now an interface. Use `tea.KeyPressMsg` in a type
switch:

```go
case tea.KeyPressMsg:
	switch msg.String() {
	case "enter":
		...
	}
```

`tea.KeyMsg` still works as a type switch case and matches both the
press and the release events. Release events are only delivered if
the program asks for them
(`v.KeyboardEnhancements.ReportEventTypes = true`), so the default
behaviour matches v1.

To build a key in a test:

```go
// Before
tea.KeyPressMsg{Code: 'a', Text: "a"}

// After
tea.KeyPressMsg{Code: 'a', Text: "a"}
```

### 5. The Bubbles components

```go
// Before
v := viewport.New(20, 10)
v.Width = 20
v.Height = 10

// After
v := viewport.New(viewport.WithWidth(20), viewport.WithHeight(10))
v.SetWidth(20)
v.SetHeight(10)
```

The `Width` and `Height` fields are unexported in v2.

### 6. The tables need a width

⚠️ **This one fails silently.** A v2 table whose viewport has no width
renders nothing at all: no error, no panic, just an empty table. If
the rows disappear after the migration, call `SetWidth` (and give the
table a `WithWidth` default at construction, before the first resize).

### 7. The tables panic on ragged rows

A row with **more** cells than the table has columns panics with an
index out of range, because v2 indexes the columns with the cell
position. A row with fewer cells is fine.
