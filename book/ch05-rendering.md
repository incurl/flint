# Chapter 5 — Rendering: lipgloss, Layout, and the Frame

## 5.1 The rendering contract

Bubble Tea calls `View()` after every update and writes the result to
the terminal. Three properties follow, and violating any of them causes
visible bugs:

1. **`View()` returns the complete frame.** Not a diff, not a patch —
   the whole thing.
2. **`View()` must be fast.** It runs on every message.
3. **`View()` must be deterministic given the state.** The same state
   must always produce the same frame, or the terminal flickers.

Bubble Tea's renderer diffs consecutive frames so it only writes the
changed bytes, but from your side the contract is "produce the whole
frame, cheaply".

## 5.2 lipgloss: styles as values

`lipgloss` is the styling library of the Charm ecosystem. Its core idea
is that a style is a **value**, not a function call that mutates
something.

```go
style := lipgloss.NewStyle().
    Bold(true).
    Foreground(lipgloss.Color("#7aa2f7")).
    Padding(0, 1)

rendered := style.Render("hello")
```

`Render` returns a string with ANSI escape sequences embedded. The
important subtlety:

> **`Render` returns a string whose *visible* width is not its byte
> length.**

```go
s := style.Render("hello")
len(s)      // 18 — the escape sequences are counted
lipgloss.Width(s)  // 5 — the visible width
```

Every layout calculation must use `lipgloss.Width`, never `len`. This
matters as soon as you use colours, which is immediately.

### Style inheritance

Styles can be combined:

```go
base := lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff"))
bold := base.Bold(true)
```

`Bold` returns a **new** style; it does not modify `base`. But there
is a subtlety with `Inherit`:

```go
// Bold adds bold to the base style.
bold := base.Bold(true)          // foreground + bold

// Inherit layers one style's *set* properties onto another.
combo := bold.Inherit(other)     // other wins for the properties it sets
```

`Inherit` is how the template composes the table styles:

```go
Header: t.TableHeader.Inherit(t.TableHeaderBorder),
```

`TableHeader` sets the foreground and bold; `TableHeaderBorder` sets
the border style and colour. The result has both.

### The rules of the style

Lipgloss styles have *rules* — constraints that are applied at render
time:

```go
lipgloss.NewStyle().
    Width(40).          // the total width is 40
    MaxHeight(10).      // at most 10 lines
    MaxWidth(40).       // at most 40 columns
    Padding(1, 2).      // 1 line, 2 columns of padding
    Margin(1).          // outer spacing
    Border(lipgloss.RoundedBorder()).
    Align(lipgloss.Center)
```

The rules are applied in a fixed order: margin → border → padding →
alignment → sizing. Understanding the order is what lets you predict
the output.

⚠️ **`Width` includes padding and border.** A style with `Width(40)` and
`Padding(0, 2)` has 36 columns of content.

## 5.3 Colours and the palette

A theme is a set of colours plus a set of styles built from them. The
template's theme package is the model:

```go
// internal/theme/theme.go
type Palette struct {
    Background    string
    Foreground    string
    Muted         string
    Primary       string
    Secondary     string
    Accent        string
    Success       string
    Warning       string
    Error         string
    Border        string
    BorderFocus   string
    Selection     string
    SelectionText string
}

func DarkPalette() Palette {
    return Palette{
        Background:    "#1c1c1c",
        Foreground:    "#e6e6e6",
        Muted:         "#8a8a8a",
        Primary:       "#7aa2f7",
        Secondary:     "#bb9af7",
        Accent:        "#7dcfff",
        Success:       "#9ece6a",
        Warning:       "#e0af68",
        Error:         "#f7768e",
        Border:        "#3b3b3b",
        BorderFocus:   "#7aa2f7",
        Selection:     "#283457",
        SelectionText: "#ffffff",
    }
}
```

### Why a palette and not raw colours

Because a terminal's background is unknown. The application cannot know
whether it is running on a black background, a white one, or a photo of
a cat. Two strategies handle this:

**Absolute colours** (what the template does): you pick colours and
commit to them. Simple, predictable, and fine when you know your users'
terminals.

**Adaptive colours** (the `lipgloss/adaptive` package): you define a
light and a dark variant, and lipgloss picks based on the detected
terminal background.

```go
light := lipgloss.AdaptiveColor{Light: "#d0d0d0", Dark: "#3b3b3b"}
style := lipgloss.NewStyle().Foreground(light)
```

The template uses absolute colours but organises them into named
palettes, so switching to adaptive is a change to `DarkPalette` and
`LightPalette` only.

⚠️ **Always provide a `NoColor` fallback.** Some terminals (and pipes)
do not support colour. Lipgloss's `termenv` layer detects this and
strips the sequences, which is the correct behaviour — but only if you
let it. Do not hand-roll ANSI codes.

## 5.4 The theme package rule

The template enforces one rule: **all styling lives in `internal/theme`**.

```go
// ✗ Forbidden.
lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))

// ✓ Correct.
m.theme.Error.Render("boom")
```

The reason is not pedantry. Consider what happens when a user wants a
different colour scheme:

- With the rule: you edit `theme.go`, and every component follows.
- Without it: you grep for `#ff0000` and hope you find them all.

The theme exposes a resolved `*Theme` with named styles:

```go
type Theme struct {
    Variant Variant
    Palette Palette

    Base     lipgloss.Style
    Muted    lipgloss.Style
    Title    lipgloss.Style
    Subtitle lipgloss.Style
    Accent   lipgloss.Style
    Success  lipgloss.Style
    Warning  lipgloss.Style
    Error    lipgloss.Style
    Selected lipgloss.Style
    Normal   lipgloss.Style
    Help     lipgloss.Style
    StatusBar lipgloss.Style
    // ...
}
```

and a helper for the semantic colours:

```go
// StatusColor returns the style associated with a connection status.
func (t *Theme) StatusColor(status string) lipgloss.Style {
    switch status {
    case "ok":
        return t.Success
    case "degraded", "connecting", "syncing":
        return t.Warning
    case "unreachable", "unhealthy", "error":
        return t.Error
    default:
        return t.Muted
    }
}
```

⚠️ **The one exception: measurement.** `lipgloss.Width` is not a style,
it is a measurement, and it is fine to call from any package:

```go
visible := lipgloss.Width(s)
```

## 5.5 Layout: joining strings

Because the terminal is a grid and `View` returns a string, layout is
string arithmetic.

### Joining lines

```go
func joinLines(lines []string) string {
    return strings.Join(lines, "\n")
}
```

That is the whole vertical layout.

### Joining columns

```go
lipgloss.JoinHorizontal(lipgloss.Top, left, right)
lipgloss.JoinVertical(lipgloss.Left, top, bottom)
```

`JoinHorizontal` pads the shorter block with spaces so the columns
align. `Top` is the alignment for the *tops* of the blocks; `Left`
centres the block vertically.

⚠️ **`JoinHorizontal` uses the visible width**, so it is safe with
styled content.

### Sizing the columns yourself

For a responsive layout you usually want to compute the widths:

```go
// SplitWidth divides the available width between two columns.
func SplitWidth(total, left, gap int) (int, int) {
    if gap < 0 {
        gap = 0
    }

    available := total - gap
    if available < 2 {
        return max(1, total), 0
    }

    if left <= 0 {
        left = available / 2
    }
    if left > available {
        left = available
    }

    right := available - left
    return left, max(0, right)
}
```

The template's version is in `internal/theme/layout.go`.

### The responsive breakpoints

```go
type Breakpoints struct {
    Narrow int
    Medium int
}

func LayoutModeFor(width int, bp Breakpoints) LayoutMode {
    switch {
    case width < bp.Narrow:
        return LayoutNarrow
    case width < bp.Medium:
        return LayoutMedium
    default:
        return LayoutWide
    }
}
```

The template uses `Narrow: 100, Medium: 160`: below 100 columns the
content stacks, between 100 and 160 it is two columns, above 160 it is
two columns with more space.

⚠️ **Test at 80 columns.** That is the classic terminal width, and it
is where layouts break. If your application is unusable at 80 columns,
half your users cannot use it.

### The style-based layout

Lipgloss also offers a declarative approach:

```go
horizontal := lipgloss.JoinHorizontal(
    lipgloss.Top,
    lipgloss.NewStyle().Width(30).Render(left),
    lipgloss.NewStyle().Width(50).Render(right),
)
```

The `Width` on the style is what reserves the space. This is simpler
than manual arithmetic but less flexible when the widths depend on the
terminal size.

## 5.6 Padding and truncation

Two operations come up constantly.

### Padding to a width

```go
// padRight pads a string with spaces up to the requested width.
//
// The padding is applied on the visible characters only, so ANSI escape
// sequences are not counted.
func padRight(s string, width int) string {
    visible := lipgloss.Width(s)
    if visible >= width {
        return s
    }
    return s + strings.Repeat(" ", width-visible)
}
```

The template uses this to align the label column of the record card:

```go
label := c.theme.Label.Render(padRight(row.Name, 14))
b.WriteString(label + c.theme.Value.Render(row.Value))
```

⚠️ **`fmt.Sprintf("%-14s", s)` does not work with styled strings.** It
pads based on byte length, so a coloured label gets the wrong width.
Always use `lipgloss.Width`.

### Truncating to a width

```go
// Truncate shortens a string to the provided width.
func Truncate(s string, width int) string {
    if width <= 0 {
        return ""
    }
    if lipgloss.Width(s) <= width {
        return s
    }
    if width == 1 {
        return "…"
    }
    return lipgloss.NewStyle().MaxWidth(width).Render(s)
}
```

`MaxWidth` truncates and adds an ellipsis. It is width-aware.

⚠️ **Truncation can cut an escape sequence.** Lipgloss handles this
correctly for styled content, but if you build ANSI sequences by hand
you will produce garbage.

## 5.7 The record card

Now let us look at a complete rendering component, because it
exercises everything above.

```go
// internal/components/recordcard.go
type RecordRenderer interface {
    Title(rec *pbstore.Record) string
    Summary(rec *pbstore.Record) string
    Fields(rec *pbstore.Record) []FieldRow
}

type FieldRow struct {
    Name  string
    Value string
}
```

The card renders a record through an injected renderer:

```go
func (c *RecordCard) View() tea.View {
    if c.record == nil {
        return tea.NewView(c.theme.Muted.Render("no record selected"))
    }

    var b strings.Builder

    b.WriteString(c.theme.Title.Render(c.renderer.Title(c.record)))
    b.WriteString("\n")
    b.WriteString(c.theme.Muted.Render(c.renderer.Summary(c.record)))
    b.WriteString("\n\n")

    rows := c.renderer.Fields(c.record)
    stacked := c.width > 0 && c.width < 48

    for _, row := range rows {
        if stacked {
            b.WriteString(c.theme.Label.Render(row.Name))
            b.WriteString("\n")
            b.WriteString("  " + c.theme.Value.Render(row.Value))
            b.WriteString("\n")
            continue
        }

        label := c.theme.Label.Render(padRight(row.Name, 14))
        b.WriteString(label + c.theme.Value.Render(row.Value))
        b.WriteString("\n")
    }

    return tea.NewView(b.String())
}
```

Note the **adaptive layout**: below 48 columns the label and value are
stacked instead of side by side, because a 14-column label column plus
a value does not fit in a narrow terminal.

### The renderer interface is the extension point

The card knows nothing about the domain. A downstream project supplies
a renderer:

```go
type WikidataRenderer struct {
    language string
}

func (r *WikidataRenderer) Title(rec *pbstore.Record) string {
    // The label for the preferred language, falling back to the QID.
}
```

This is the pattern that keeps the template generic. The card, the
table and the search results all use the same renderer, so a domain
adapter gets a consistent look everywhere without touching the core.

⚠️ **The renderer must be cheap.** `View` calls it on every render.
If `Title` does a database lookup, the UI crawls. The renderer should
read from the record it is given.

## 5.8 Rendering nested JSON

Nested JSON is the hard case. A Wikidata item's claims look like this:

```json
{
  "P31": [
    { "mainsnak": { "datavalue": { "value": { "id": "Q5" } } } }
  ]
}
```

A flat dump is unreadable. The template's `JSONViewer` builds a tree:

```go
type JSONNode struct {
    Key         string
    Value       any
    Children    []*JSONNode
    IsContainer bool
    depth       int
    path        string
}
```

and renders it with indentation and expand/collapse markers:

```go
func (v *JSONViewer) renderNode(node *JSONNode, selected bool) string {
    indent := strings.Repeat("  ", node.Depth())

    marker := "  "
    if node.IsContainer {
        if v.expanded[node.path] {
            marker = "▾ "
        } else {
            marker = "▸ "
        }
    }

    label := node.Key
    if label == "" {
        label = "root"
    }

    prefix := indent + v.theme.Muted.Render(marker) + v.theme.Accent.Render(label)
    if node.IsContainer {
        prefix += v.theme.Muted.Render(fmt.Sprintf(" (%d)", len(node.Children)))
    } else {
        prefix += v.theme.Muted.Render(": ") + v.theme.Value.Render(formatScalar(node.Value))
    }

    if selected {
        return v.theme.Selected.Render(prefix)
    }
    return prefix
}
```

### The `path` field

⚠️ **Do not key the expansion map by the node's own key.** Sibling
nodes can share a key:

```json
{ "a": { "x": 1 }, "b": { "x": 2 } }
```

If the expansion map is keyed by `"x"`, expanding `a.x` also expands
`b.x`. The template uses a **path** — the chain of keys from the root:

```go
func buildJSONNode(key string, value any, depth int, parentPath string) *JSONNode {
    path := key
    if parentPath != "" {
        path = parentPath + "/" + key
    }
    node := &JSONNode{Key: key, Value: value, depth: depth, path: path}
    // ...
}
```

### The visible-node flattening

The tree is stored once; only the *visible* nodes are computed at render
time:

```go
func collectVisible(node *JSONNode, expanded map[string]bool, out *[]*JSONNode) {
    *out = append(*out, node)

    if !node.IsContainer || !expanded[node.path] {
        return
    }
    for _, child := range node.Children {
        collectVisible(child, expanded, out)
    }
}
```

⚠️ **Remember to re-render after a toggle.** This was a bug in the
template: `toggleCurrent` updated the expansion map but did not call
`render()`, so the viewport kept showing the old tree.

### Sorting the keys

```go
keys := make([]string, 0, len(v))
for k := range v {
    keys = append(keys, k)
}
sort.Strings(keys)
```

Map iteration order in Go is *random*. Without sorting, the JSON tree
would reshuffle on every render — a flickering display. This is a
general rule:

> **Never iterate a map to produce output.** Sort the keys first.

## 5.9 Rendering large text

For long text (a log, a document), you need a scrollable viewport. That
is what `bubbles/viewport` is for:

```go
viewport := viewport.New(20, 10)
viewport.SetContent(longText)
```

and the rendered frame is `viewport.View()`.

⚠️ **Size the viewport when the terminal size is known**, and re-size it
on `tea.WindowSizeMsg`. A viewport created with a default size will
either clip the content or waste space.

The template's detail page does this:

```go
func (p *DetailPage) resize() {
    // The header, the titles and the help line take up roughly 8 lines.
    viewerHeight := p.height - 8
    if viewerHeight < 3 {
        viewerHeight = 3
    }
    p.viewer.SetSize(p.width, viewerHeight)
    p.editor.SetSize(p.width, p.height-6)
    p.card.SetWidth(p.width)
}
```

⚠️ **Do not hardcode the chrome height.** "roughly 8 lines" is a
guess that breaks when the header wraps. A more robust approach
measures the header:

```go
headerHeight := lipgloss.Height(header)
viewerHeight := p.height - headerHeight - footerHeight
```

The template uses the estimate because it keeps the code simple, but
the measurement is more correct.

## 5.10 The status bar

The status bar is the most custom-rendered component, because it has
to fit a variable amount of information into a fixed width.

```go
func (s *StatusBar) View() tea.View {
    left := s.leftSection()
    right := s.rightSection()

    const minGap = 1
    const sepWidth = 2

    visible := len(s.info.Hints)
    for visible > 0 && s.width > 0 &&
        lipgloss.Width(left)+lipgloss.Width(right)+lipgloss.Width(s.hintsSectionN(visible))+sepWidth > s.width {
        visible--
    }
    hints := s.hintsSectionN(visible)

    if s.width > 0 && lipgloss.Width(left)+lipgloss.Width(right)+minGap > s.width {
        right = ""
    }

    separator := ""
    if hints != "" && right != "" {
        separator = strings.Repeat(" ", sepWidth)
    }

    gap := minGap
    if s.width > 0 {
        gap = s.width - lipgloss.Width(left) - lipgloss.Width(right) -
            lipgloss.Width(hints) - lipgloss.Width(separator)
        if gap < minGap {
            gap = minGap
        }
    }

    return tea.NewView(s.theme.StatusBar.Render(left + strings.Repeat(" ", gap) + hints + separator + right))
}
```

The logic is a **priority-based degradation**:

1. The left section (mode, health, collection, sync) is essential —
   never dropped.
2. The right section (page name, last error) is dropped first.
3. The hints are dropped one at a time, from the right.
4. What remains is padded to the full width.

This is the right general pattern for a status bar: **decide what is
essential, and degrade the rest**.

⚠️ **Always compare with `lipgloss.Width`, never `len`.**

## 5.11 Rendering performance

`View()` runs on every message. If it is slow, the UI stutters.

Common causes:

| Cause                          | Fix                                        |
| ------------------------------ | ------------------------------------------ |
| Rebuilding a large string      | Cache it, invalidate on state change       |
| `lipgloss.Width` in a loop     | Measure once, reuse                        |
| JSON marshalling in `View`     | Marshal in `Update`, store the string      |
| A database call in `View`      | Never. Load in a command.                  |
| `fmt.Sprintf` in a hot path    | Precompute the constant parts              |

The JSON viewer is a good example of something to watch: it rebuilds the
entire visible tree on every render. For a document of a few thousand
nodes that is fine. For a hundred thousand it is not — and the fix is
to render into a viewport (which the template does), so only the visible
lines are produced.

## 5.12 Exercises

1. Write a program that renders a 100×100 grid of characters and
   measures how long `View()` takes. Now make it render only the
   visible 24 lines (as a viewport would) and compare.

2. Build a theme with an adaptive colour and verify it changes with
   the terminal background.

3. Implement a `padRight` that truncates as well as pads, and verify
   it is correct for a styled string.

4. Render a JSON document with duplicate sibling keys and verify that
   collapsing one does not affect the other.

5. Make the status bar's left section too long to fit (simulate a very
   long collection name) and verify that the degradation works.

6. Write a benchmark for `View()` of your own application's root model.

## 5.13 What you learned

- `View()` returns the **complete frame**, must be **fast**, and must
  be **deterministic**.
- Lipgloss styles are **values**; `Render` embeds ANSI sequences, so
  measure with `lipgloss.Width`, never `len`.
- Styles have **rules** applied in a fixed order (margin → border →
  padding → alignment → sizing).
- **All styling lives in the theme package**; only `lipgloss.Width`
  (a measurement) is allowed elsewhere.
- Layout is **string arithmetic**: `JoinHorizontal`, `JoinVertical`,
  and your own width arithmetic.
- **Never iterate a map to produce output** — sort the keys first, or
  the display flickers.
- Large text needs a **viewport**, sized from the terminal dimensions.
- Status bars should **degrade by priority**: drop the least important
  information first.
- The **renderer interface** is how a domain adapter customises the
  look without touching the core.

## 5.14 Further reading

- `internal/theme/theme.go` — the palette and the resolved styles.
- `internal/theme/layout.go` — the breakpoints and the width helpers.
- `internal/components/recordcard.go` — a complete rendering component.
- `internal/components/jsonviewer.go` — the tree, the paths, the
  flattening.
- `internal/components/statusbar.go` — priority-based degradation.
- The lipgloss source: `style.go` for the rules, `position.go` for
  `JoinHorizontal`.

Next: [Chapter 6 — Bubbles: Composition over Reinvention](ch06-bubbles.md).
