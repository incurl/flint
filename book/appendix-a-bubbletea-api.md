# Appendix A — Bubble Tea API Reference

A quick reference for the Bubble Tea v1.3.x API. Every signature here
was verified against the source of `github.com/charmbracelet/bubbletea@v1.3.10`.

## The core types

```go
// Msg contains data from the result of an IO operation.
type Msg interface{}

// Model contains the program's state and its core functions.
type Model interface {
    Init() Cmd
    Update(Msg) (Model, Cmd)
    View() string
}

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
func WithAltScreen() ProgramOption
func WithMouseCellMotion() ProgramOption
func WithMouseAllMotion() ProgramOption
func WithMouseMode(mouseMode MouseMode) ProgramOption
func WithInput(input io.Reader) ProgramOption
func WithInputTTY() ProgramOption
func WithOutput(output io.Writer) ProgramOption
func WithContext(ctx context.Context) ProgramOption
func WithoutSignalHandler() ProgramOption
func WithoutCatchPanics() ProgramOption
func WithFPS(fps int) ProgramOption
```

### The recommended set

```go
p := tea.NewProgram(model,
    tea.WithAltScreen(),
    tea.WithMouseCellMotion(),
)
```

⚠️ **The mouse takes the selection away from the user.** If copying
text matters, omit `WithMouseCellMotion`.

## The messages

### The built-in ones

```go
type QuitMsg struct{}

type WindowSizeMsg struct {
    Width  int
    Height int
}

type KeyMsg Key

type MouseMsg struct {
    X      int
    Y      int
    Shift  bool
    Alt    bool
    Ctrl   bool
    Action MouseAction
    Button MouseButton
}

type FocusMsg struct{}
type BlurMsg struct{}
type PasteMsg string
```

## The key type

```go
type Key struct {
    Type  KeyType
    Runes []rune
    Alt   bool
    Paste bool
}

func (k Key) String() string
```

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

### The paste marker

```go
if k.Paste {
    buf.WriteByte('[')
}
buf.WriteString(string(k.Runes))
if k.Paste {
    buf.WriteByte(']')
}
```

A pasted `q` becomes `"[q]"`, so a string comparison against `"q"`
fails. This is intentional.

## The key types

The `KeyType` constants:

```go
KeyRunes, KeyUp, KeyDown, KeyRight, KeyLeft, KeyShiftTab,
KeyHome, KeyEnd, KeyPgUp, KeyPgDown, KeyDelete, KeyInsert,
KeySpace, KeyCtrlA … KeyCtrlZ, KeyCtrlSpace, KeyCtrlOpenBracket,
KeyCtrlCloseBracket, KeyCtrlCaret, KeyCtrlUnderscore,
KeyCtrlBackslash, KeyCtrlSquareBracket,
KeyEnter, KeyBackspace, KeyEsc, KeyEscape, KeyTab, KeyDelete,
KeyShiftUp, KeyShiftDown, KeyShiftRight, KeyShiftLeft,
KeyCtrlC, KeyCtrlD, KeyCtrlR, KeyCtrlS, ...
```

## The Bubbles packages

| Package        | Type                        | Key methods                       |
| -------------- | --------------------------- | --------------------------------- |
| `textinput`    | `textinput.Model`           | `Focus`, `Blur`, `Value`, `SetValue`, `Reset`, `CursorEnd` |
| `textarea`     | `textarea.Model`            | `Focus`, `Blur`, `Value`, `SetValue`, `SetWidth`, `SetHeight` |
| `table`        | `table.Model`               | `SetRows`, `SetColumns`, `SetStyles`, `MoveUp`, `MoveDown`, `Cursor`, `SetHeight` |
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

	tea "github.com/charmbracelet/bubbletea"
)

type model struct {
	choices  []string
	cursor   int
	selected string
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
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

func (m model) View() string {
	s := "Choose an option:\n\n"
	for i, choice := range m.choices {
		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}
		s += fmt.Sprintf("%s %s\n", cursor, choice)
	}
	return s + "\n(enter to select, q to quit)"
}

func main() {
	p := tea.NewProgram(model{choices: []string{"one", "two", "three"}})
	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
```
