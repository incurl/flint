# Chapter 4 — Keys, Hotkeys, and the Routing Problem

## 4.1 The problem

A single-screen program has one set of key bindings. A multi-screen
program has several, and they overlap.

Consider a records list with a filter input:

| Key   | Meaning in the list        | Meaning in the filter input |
| ----- | -------------------------- | -------------------------- |
| `j`   | move down                  | type "j"                   |
| `q`   | quit                       | type "q"                   |
| `/`   | focus the filter           | (nothing)                  |
| `esc` | clear the filter           | unfocus                    |

If the application handles keys globally, typing "jq" in a filter
quits the application. If it handles them only locally, there is no
consistent way to switch pages.

This is the **key routing problem**, and solving it well is a large
part of what separates a usable TUI from a frustrating one.

## 4.2 How Bubble Tea reports keys

Every keystroke arrives as a `tea.KeyMsg`:

```go
type KeyMsg Key

type Key struct {
    Type  KeyType
    Runes []rune
    Alt   bool
    Paste bool
}
```

The `String()` method gives you a canonical name:

```go
func (k Key) String() (str string)
```

The names are stable and are what you should compare against:

| Category     | Names                                                            |
| ------------ | ---------------------------------------------------------------- |
| Printable    | the character itself: `"a"`, `"Z"`, `"7"`, `" "`                  |
| Named keys   | `"enter"`, `"esc"`, `"tab"`, `"shift+tab"`, `"backspace"`         |
| Arrows       | `"up"`, `"down"`, `"left"`, `"right"`                             |
| Home / End   | `"home"`, `"end"`                                                |
| Page         | `"pgup"`, `"pgdown"`                                             |
| Delete       | `"delete"`                                                       |
| Control      | `"ctrl+a"` … `"ctrl+z"`, `"ctrl+space"`                           |
| Alt          | `"alt+left"`, `"alt+f"` (the `Alt` flag is folded in)            |

⚠️ **Two important subtleties.**

**Control keys have two names.** `ctrl+i` is the same byte as `tab`,
`ctrl+j` is `enter`, `ctrl+m` is `enter` again, `ctrl+h` is
`backspace`. The `keyNames` map resolves them to the *friendly* name:

```go
keyHT: "tab",       // also ctrl+i
keyLF: "ctrl+j",
keyVT: "ctrl+k",
keyFF: "ctrl+l",
keyCR: "enter",     // also ctrl+m
keyDEL: "backspace",
```

So `msg.String()` for the Tab key is `"tab"`, not `"ctrl+i"`. If you
want to detect the raw control character, you must compare
`msg.Type` against the constants, not the string.

**Pastes are marked.** When the user pastes text, `Key.Paste` is true
and `String()` wraps the value in brackets:

```go
if k.Paste {
    buf.WriteByte('[')
}
buf.WriteString(string(k.Runes))
if k.Paste {
    buf.WriteByte(']')
}
```

The purpose is that `bubbles/keys` does string comparison, so a pasted
`q` would otherwise look exactly like a typed `q` — and would quit your
application. The brackets make the comparison fail.

⚠️ **But** if you compare `msg.String() == "q"` directly, a pasted "q"
also matches, because the string is `"[q]"`, not `"q"`. That is
accidentally safe. But if you compare against a *prefix* or use
`strings.Contains`, you have reintroduced the bug. Prefer
`key.Matches(msg, keyMap)` from `bubbles/keys`, which understands
pastes.

## 4.3 The `bubbles/keys` package

Bubbles provides a declarative binding system:

```go
import "github.com/charmbracelet/bubbles/key"

// Define a binding.
type keyMap struct {
    Up      key.Binding
    Down    key.Binding
    Filter  key.Binding
    Save    key.Binding
    Quit    key.Binding
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
        Filter: key.NewBinding(
            key.WithKeys("/"),
            key.WithHelp("/", "filter"),
        ),
        Save: key.NewBinding(
            key.WithKeys("ctrl+s"),
            key.WithHelp("ctrl+s", "save"),
        ),
        Quit: key.NewBinding(
            key.WithKeys("q", "ctrl+c"),
            key.WithHelp("q", "quit"),
        ),
    }
}
```

and matching:

```go
if key.Matches(msg, m.keys.Quit) {
    return m, tea.Quit
}
if key.Matches(msg, m.keys.Filter) {
    // ...
}
```

The `key.Binding` carries both the keys and the help text, so the
help and the behaviour cannot drift apart:

```go
// The help is generated from the bindings.
help := keyMap.ShortHelpView(m.keys)
```

This is the right way to do it. The Flint template uses a simpler
string comparison for its global layer (because it needs precise
control over routing) but a `keyMap`-style structure for the hint
display.

## 4.4 The layered routing model

The solution used by the Flint template is a **layered** approach:

```
key press
   │
   ├─▶ LAYER 0: the interrupt key
   │      ctrl+c → quit, always
   │
   ├─▶ LAYER 1: text-input capture
   │      if a page owns a focused text input:
   │          the input consumes the key
   │          (except the page-switch digits, see §4.7)
   │
   ├─▶ LAYER 2: the global hotkeys
   │      1…6 → page switch
   │      s   → health check
   │      y   → trigger a sync
   │      esc → go back
   │
   └─▶ LAYER 3: the active page
          / → focus the filter
          enter → open the record
          …
```

Each layer gets the key only if the layer above declined it.

### Implementing the layers

```go
func (m *Model) handleGlobalKey(msg tea.KeyMsg) (tea.Cmd, bool) {
    // Layer 1: a focused text input captures the key.
    if m.inputFocused() {
        switch msg.String() {
        case "ctrl+c":
            m.quitting = true
            return tea.Quit, true
        case "1":
            return m.navigate(state.PageHome), true
        // ... the other digits
        }
        return nil, false      // the page gets it
    }

    // Layer 2: the global hotkeys.
    switch msg.String() {
    case "ctrl+c", "q":
        m.quitting = true
        return tea.Quit, true
    case "1":
        return m.navigate(state.PageHome), true
    // ...
    case "esc":
        m.runtime.GoBack()
        m.refreshStatusBar()
        return m.loadPage(m.runtime.Page), true
    }

    return nil, false          // the page gets it
}
```

And the root model's `Update`:

```go
case tea.KeyMsg:
    if cmd, handled := m.handleGlobalKey(msg); handled {
        return m, cmd
    }
    // ... fall through to updateActivePage
```

⚠️ **The `q` problem.** `q` quits globally. But if the user is typing
in a filter, `q` must be a literal "q".

The fix is `inputFocused()`:

```go
// inputFocused reports whether a page currently owns a focused text
// input.
//
// When it returns true the global hotkey layer is suspended.
func (m *Model) inputFocused() bool {
    switch m.runtime.Page {
    case state.PageSearch:
        return true
    case state.PageRecords:
        return m.records.FilterFocused()
    default:
        return false
    }
}
```

⚠️ **This is a whack-a-mole design.** Every new page with a text input
must be added to `inputFocused()`, or it will have the same bug. The
alternative is to have each page report whether it wants the global
layer, via an interface:

```go
// FocusAware is implemented by the pages that own a focused input.
type FocusAware interface {
    InputFocused() bool
}
```

```go
func (m *Model) inputFocused() bool {
    switch page := m.activePage().(type) {
    case FocusAware:
        return page.InputFocused()
    default:
        return false
    }
}
```

This is better: the page owns the knowledge, and adding a page cannot
break the global layer.

## 4.5 The page-local shortcut problem

The inverse problem: a page wants a key that the global layer also
wants.

In the template, `r` means "refresh" globally, but on the detail page
it means "toggle the raw JSON view". The global layer must not steal
it.

The template solves this with an explicit exception:

```go
// The "r" hotkey refreshes the active page, but it is not available
// on the pages that use it for a local toggle (eg. the raw JSON view).
if msg.String() == "r" && !m.hasLocalRBinding() {
    return m.reloadActivePage(), true
}

return nil, false
```

```go
// hasLocalRBinding reports whether the active page uses the "r" key for
// a page local action.
func (m *Model) hasLocalRBinding() bool {
    return m.runtime.Page == state.PageDetail
}
```

This is explicit and readable, but it does not scale: every page that
rebinds a global key must be registered here.

⚠️ **A better design** is to invert the priority: let the *page* get
first refusal on any key it explicitly binds, and only fall back to the
global layer if the page declines. The Bubbles ecosystem supports this
naturally, because each component exposes its own `KeyMap`.

A pragmatic middle ground used by many applications: the global layer
handles only keys that are *unambiguous* (page-switch digits, `ctrl+c`,
`?`), and page-specific keys live with the page. The `s` (status) and
`y` (sync) keys in the template are examples of "global but rarely
rebound".

## 4.6 Key hints: telling the user what is possible

A TUI has no menus. The status bar is the only affordance the user has
for discovering what they can do.

The template renders contextual hints per page:

```go
func (m *Model) hints() []string {
    switch m.runtime.Page {
    case state.PageRecords:
        return components.FormatHints([][2]string{
            {"/", "filter"}, {"↑↓", "move"}, {"enter", "open"}, {"←→", "page"},
        })
    case state.PageDetail:
        return components.FormatHints([][2]string{
            {"j", "json"}, {"r", "raw"}, {"e", "edit"},
        })
    // ...
    }
}
```

And the status bar renders them, dropping the trailing ones when the
terminal is too narrow:

```go
// Drop the trailing hints until everything fits.
//
// The number of displayed hints is computed locally so that the
// component state is never mutated by a render.
visible := len(s.info.Hints)
for visible > 0 && s.width > 0 &&
    lipgloss.Width(left)+lipgloss.Width(right)+lipgloss.Width(s.hintsSectionN(visible))+sepWidth > s.width {
    visible--
}
hints := s.hintsSectionN(visible)
```

⚠️ **Do not mutate the state during a render.** An earlier version of
this code trimmed `s.info.Hints` in place, which meant that once the
terminal had been narrow, the hints were permanently gone. The
`hintsSectionN(n)` helper takes the count as an argument instead.

## 4.7 The digit conflict

There is a subtle conflict between "page switch digits" and "typing
digits".

If the search page has a focused input and the global layer grabs
`1`–`6` to switch pages, the user cannot type a number.

The template resolves it by allowing the digits **even when an input
has focus**:

```go
if m.inputFocused() {
    switch msg.String() {
    case "ctrl+c":
        m.quitting = true
        return tea.Quit, true
    case "1":
        return m.navigate(state.PageHome), true
    case "2":
        return m.navigate(state.PageRecords), true
    // ...
    }
    return nil, false
}
```

This is a deliberate trade-off: the digits are reserved for navigation
everywhere, and a user who needs to type a number uses the
`Filter`-by-field approach or a dedicated "insert literal" mode.

⚠️ **This is a design decision, not a law.** If your application needs
to type numbers into a filter, do *not* reserve the digits globally.
Instead, use `ctrl+1`…`ctrl+6`, or `g` followed by a digit (a
"goto" prefix), or `alt+1`…`alt+6`.

The general principle:

> **Reserve keys that users type rarely, and make the reservation
> explicit and documented.**

A status bar that shows the page-switch digits is the user's only
warning. Put them in the hints.

## 4.8 Key repeat and fast typists

A user holding down `j` generates a stream of key messages faster than
your program can render. Bubble Tea will queue them all.

This is usually fine, but it has two consequences:

1. **The cursor can lag behind the input.** If each keypress triggers a
   database query, the queue builds up. This is why the debounce from
   Chapter 3 matters.
2. **A "quit" key pressed during a burst can be delayed.** If the user
   holds `j` and then hits `q`, the `q` sits behind 30 `j`s.

If this matters, you can coalesce repeated keys in `Update`:

```go
case tea.KeyMsg:
    if m.lastKey == msg.String() && time.Since(m.lastKeyAt) < 50*time.Millisecond {
        // Ignore the repeated key.
        return m, nil
    }
    m.lastKey = msg.String()
    m.lastKeyAt = time.Now()
```

⚠️ Be careful with this: it will break a text input that legitimately
receives repeated characters (holding down "a" to type "aaaa"). Only
apply it to navigation keys, and only when the input does not have
focus.

## 4.9 Mouse support

Bubble Tea can enable mouse reporting:

```go
p := tea.NewProgram(model{},
    tea.WithAltScreen(),
    tea.WithMouseCellMotion(),
)
```

Then `tea.MouseMsg` arrives:

```go
case tea.MouseMsg:
    switch msg.Button {
    case tea.MouseButtonLeft:
        // ...
    }
```

⚠️ **Mouse support is a trade-off in a terminal.** Enabling it means
the terminal no longer lets the user select and copy text with the
mouse — the application captures it. For a data browser where copying
values matters, that is a regression.

The template enables it, because the table interaction feels natural.
If your users need to copy text, leave it off.

There is a middle path: `tea.WithMouseCellMotion()` only reports
motion while a button is pressed; `tea.WithMouseAllMotion()` reports
all motion. Neither avoids the copy problem, though.

## 4.10 A complete key handling example

Here is the records page's key handling in full, with comments:

```go
case tea.KeyMsg:
    // Layer 1: the focused filter input owns the keyboard.
    if p.filterFocused {
        switch msg.String() {
        case "enter":
            p.filterFocused = false
            p.filter.Blur()
            return p, tea.Batch(
                func() tea.Msg { return RecordsFilterMsg{Filter: p.filter.Value()} },
                p.Load(),
            )
        case "esc":
            p.filterFocused = false
            p.filter.Blur()
            p.filter.SetValue(p.runtime.Filter)
            return p, nil
        }

        // The focused input consumes the key.
        var cmd tea.Cmd
        p.filter, cmd = p.filter.Update(msg)
        return p, cmd
    }

    // Layer 2: the page's own shortcuts.
    switch msg.String() {
    case "/":
        p.filterFocused = true
        p.filter.Focus()
        p.filter.SetValue(p.runtime.Filter)
        return p, nil

    case "enter":
        if rec := p.SelectedRecord(); rec != nil {
            return p, func() tea.Msg {
                return OpenRecordMsg{ID: rec.ID}
            }
        }
        return p, nil

    case "up", "k":
        p.table.MoveUp(1)
        return p, nil

    case "down", "j":
        p.table.MoveDown(1)
        return p, nil

    case "left", "h":
        if p.pagination.Page() > 1 {
            p.pagination.SetPage(p.pagination.Page() - 1)
            p.runtime.PageNumber = p.pagination.Page()
            p.loading = true
            return p, tea.Batch(p.spinner.Tick, p.Load())
        }
        return p, nil

    case "right", "l":
        if p.pagination.Page() < p.pagination.TotalPages() {
            p.pagination.SetPage(p.pagination.Page() + 1)
            p.runtime.PageNumber = p.pagination.Page()
            p.loading = true
            return p, tea.Batch(p.spinner.Tick, p.Load())
        }
        return p, nil
    }
```

Notice the structure: the focused-input branch returns early, so the
page's own shortcuts are unreachable while typing. That is the correct
priority — typing wins.

⚠️ **And notice what is missing: the `default` case.** Because the
switch handles some keys and returns, other keys fall through to the
code after the switch, where the table gets its update:

```go
var cmd tea.Cmd
p.table, cmd = p.table.Update(msg)
cmds = append(cmds, cmd)
```

This is how the table's own bindings (like `g` for "go to top") still
work, and it is why the spinner keeps ticking — the spinner is updated
in the same fall-through path.

## 4.11 Exercises

1. Add a `g` binding to the records page that jumps the table back to
   the first row. Verify that the table's own `g` binding does not
   conflict.

2. Demonstrate the paste problem: write a test that sends a
   `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q"), Paste: true}`
   and show that `msg.String()` is `"[q]"`. Then show that your quit
   handler does not trigger.

3. Change the global page-switch keys to `ctrl+1`…`ctrl+6` and remove
   the digit exception from the `inputFocused()` branch. Verify that
   digits can now be typed into the filter.

4. Implement the `FocusAware` interface from §4.4 and refactor
   `inputFocused()` to use it. Add a new page with a text input and
   verify that the global layer suspends correctly without touching
   `app.go`.

5. Add key repeat coalescing for the arrow keys only, and verify that
   holding down "j" in a text input still types "jjjj".

6. Measure: write a program that logs every `tea.KeyMsg` it receives,
   then hold down a key for one second. Count the messages. Compare
   with the number of renders.

## 4.12 What you learned

- Keystrokes arrive as `tea.KeyMsg`; `String()` gives a stable name
  (`"enter"`, `"esc"`, `"ctrl+s"`, `"alt+f"`).
- **Control keys have aliases**: `ctrl+i` is `tab`, `ctrl+m` is
  `enter`, `ctrl+h` is `backspace`.
- Pasted text is marked with `Paste` and bracketed in `String()`.
- Multi-screen applications need **layered key routing**: interrupt →
  text-input capture → global hotkeys → page shortcuts.
- The **global layer must suspend** when a text input has focus, or
  typing "q" quits the application.
- When a page rebinds a global key, the exception must be **explicit**.
- **Reserve rarely-typed keys** for global actions, and document the
  reservation in the status bar.
- **Never mutate state during a render.**

## 4.13 Further reading

- `key.go` in the Bubble Tea source — `Key`, `String()`, the
  `keyNames` map.
- `bubbles/key` — the declarative binding package.
- `internal/app/app.go` — `handleGlobalKey` and `inputFocused`.
- `internal/pages/records.go` — the layered page key handling.
- `internal/components/statusbar.go` — the hint rendering and the
  graceful degradation.

Next: [Chapter 5 — Rendering: lipgloss, Layout, and the Frame](ch05-rendering.md).
