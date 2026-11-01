package components

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/theme"
)

// JSONNode is a single node of the collapsible JSON tree.
//
// It is a generic structure: it knows nothing about the domain schema, so
// it can render any nested payload stored in a PocketBase record.
type JSONNode struct {
	// Key is the name of the node.
	//
	// It is empty for the root node.
	Key string
	// Value is the scalar value of the node (nil for objects/arrays).
	Value any
	// Children holds the nested objects/arrays.
	Children []*JSONNode
	// IsContainer reports whether the node has children.
	IsContainer bool
	// depth is the nesting level of the node.
	depth int
	// path is the unique path of the node, used as the expansion key.
	path string
}

// JSONViewer renders a collapsible JSON tree.
//
// The tree is built once from the raw payload and the component only
// tracks which nodes are expanded, which keeps the rendering cheap even
// for large payloads.
type JSONViewer struct {
	// theme is the shared style palette.
	theme *theme.Theme
	// viewport scrolls the rendered tree.
	viewport viewport.Model
	// root is the tree root.
	root *JSONNode
	// expanded holds the keys of the expanded containers.
	expanded map[string]bool
	// width is the available rendering width.
	width int
	// height is the available rendering height.
	height int
	// focused reports whether the component handles the key input.
	focused bool
	// cursor is the index of the highlighted line.
	cursor int
}

// NewJSONViewer creates a JSON viewer component.
func NewJSONViewer(t *theme.Theme) *JSONViewer {
	if t == nil {
		t = theme.Default()
	}

	v := viewport.New(viewport.WithWidth(20), viewport.WithHeight(10))
	v.KeyMap.Up.SetEnabled(true)
	v.KeyMap.Down.SetEnabled(true)

	return &JSONViewer{
		theme:    t,
		viewport: v,
		expanded: map[string]bool{},
	}
}

// SetContent replaces the displayed payload.
//
// The payload is expected to be a JSON document. A non JSON value is
// rendered as a single scalar node.
func (v *JSONViewer) SetContent(raw string) error {
	var data any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return fmt.Errorf("components: invalid json: %w", err)
	}

	v.root = buildJSONNode("", data, 0, "")
	v.expanded = map[string]bool{}
	v.cursor = 0

	// Expand the root level by default so that the viewer is not empty.
	if v.root != nil && v.root.IsContainer {
		v.expanded[v.root.path] = true
	}

	v.render()

	return nil
}

// SetContentValue replaces the displayed payload with an arbitrary value.
func (v *JSONViewer) SetContentValue(data any) {
	v.root = buildJSONNode("", data, 0, "")
	v.expanded = map[string]bool{}
	v.cursor = 0

	if v.root != nil && v.root.IsContainer {
		v.expanded[v.root.path] = true
	}

	v.render()
}

// Focus gives the keyboard focus to the viewer.
func (v *JSONViewer) Focus() { v.focused = true }

// Blur removes the keyboard focus from the viewer.
func (v *JSONViewer) Blur() { v.focused = false }

// Focused reports whether the component handles the key input.
func (v *JSONViewer) Focused() bool { return v.focused }

// SetSize sets the available rendering area.
func (v *JSONViewer) SetSize(width, height int) {
	v.width = width
	v.height = height
	v.viewport.SetWidth(width)
	v.viewport.SetHeight(height)
	v.render()
}

// Init implements [tea.Model].
func (v *JSONViewer) Init() tea.Cmd { return nil }

// Update implements [tea.Model].
func (v *JSONViewer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height-2)
		return v, nil

	case tea.KeyPressMsg:
		if !v.focused {
			return v, nil
		}

		switch msg.String() {
		case "up", "k":
			v.moveCursor(-1)
			return v, nil
		case "down", "j":
			v.moveCursor(1)
			return v, nil
		case "enter", " ":
			v.toggleCurrent()
			return v, nil
		}
	}

	var cmd tea.Cmd

	v.viewport, cmd = v.viewport.Update(msg)

	return v, cmd
}

// View implements [tea.Model].
func (v *JSONViewer) View() tea.View {
	if v.root == nil {
		return tea.NewView(v.theme.Muted.Render("no data"))
	}

	return tea.NewView(v.viewport.View())
}

// visibleNodes returns the flattened list of the currently visible nodes.
func (v *JSONViewer) visibleNodes() []*JSONNode {
	out := make([]*JSONNode, 0, 32)
	if v.root == nil {
		return out
	}

	collectVisible(v.root, v.expanded, &out)

	return out
}

// moveCursor moves the highlighted line by the given offset.
func (v *JSONViewer) moveCursor(delta int) {
	nodes := v.visibleNodes()
	if len(nodes) == 0 {
		return
	}

	v.cursor += delta
	if v.cursor < 0 {
		v.cursor = 0
	}

	if v.cursor >= len(nodes) {
		v.cursor = len(nodes) - 1
	}
}

// toggleCurrent expands or collapses the highlighted container.
func (v *JSONViewer) toggleCurrent() {
	nodes := v.visibleNodes()
	if v.cursor < 0 || v.cursor >= len(nodes) {
		return
	}

	node := nodes[v.cursor]
	if !node.IsContainer {
		return
	}

	if v.expanded[node.path] {
		delete(v.expanded, node.path)
		v.render()

		return
	}

	v.expanded[node.path] = true
	v.render()
}

// render rebuilds the viewport content from the current tree state.
func (v *JSONViewer) render() {
	if v.root == nil {
		v.viewport.SetContent("")
		return
	}

	nodes := v.visibleNodes()

	lines := make([]string, 0, len(nodes))
	for i, node := range nodes {
		lines = append(lines, v.renderNode(node, i == v.cursor))
	}

	v.viewport.SetContent(strings.Join(lines, "\n"))
}

// renderNode renders a single tree line.
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

// collectVisible flattens the tree, keeping only the visible nodes.
func collectVisible(node *JSONNode, expanded map[string]bool, out *[]*JSONNode) {
	*out = append(*out, node)

	if !node.IsContainer || !expanded[node.path] {
		return
	}

	for _, child := range node.Children {
		collectVisible(child, expanded, out)
	}
}

// buildJSONNode converts a decoded JSON value into a [JSONNode] tree.
//
// Object keys are sorted so that the rendering is stable across runs.
func buildJSONNode(key string, value any, depth int, parentPath string) *JSONNode {
	path := key
	if parentPath != "" {
		path = parentPath + "/" + key
	}

	node := &JSONNode{Key: key, Value: value, depth: depth, path: path}

	switch v := value.(type) {
	case map[string]any:
		node.IsContainer = true

		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}

		sort.Strings(keys)

		for _, k := range keys {
			node.Children = append(node.Children, buildJSONNode(k, v[k], depth+1, path))
		}

	case []any:
		node.IsContainer = true

		for i, item := range v {
			node.Children = append(node.Children, buildJSONNode(fmt.Sprintf("[%d]", i), item, depth+1, path))
		}
	}

	return node
}

// Depth returns the nesting level of the node.
func (n *JSONNode) Depth() int { return n.depth }

// formatScalar renders a scalar value as a short string.
func formatScalar(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		return strconv.Quote(v)
	case bool:
		return fmt.Sprintf("%t", v)
	case float64:
		// Render whole numbers without the trailing ".0".
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}

		return fmt.Sprintf("%g", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
