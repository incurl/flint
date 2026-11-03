package components

import (
	"encoding/json"
	"errors"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/theme"
)

// JSONEditor is the raw JSON editing form.
//
// The content is validated before it is handed back to the pbstore layer,
// so an invalid document never reaches the database.
type JSONEditor struct {
	// theme is the shared style palette.
	theme *theme.Theme
	// textarea holds the edited document.
	textarea textarea.Model
	// original holds the document as it was loaded.
	original string
	// err holds the last validation error.
	err error
	// width is the available rendering width.
	width int
	// height is the available rendering height.
	height int
	// focused reports whether the component handles the key input.
	focused bool
}

// NewJSONEditor creates a JSON editor component.
func NewJSONEditor(t *theme.Theme) *JSONEditor {
	if t == nil {
		t = theme.Default()
	}

	ta := textarea.New()
	ta.Placeholder = "{\n  \"key\": \"value\"\n}"
	ta.ShowLineNumbers = false
	ta.CharLimit = 0

	return &JSONEditor{
		theme:    t,
		textarea: ta,
	}
}

// SetContent replaces the edited document.
//
// The content is not validated here: the user is expected to be in the
// middle of an edit.
func (e *JSONEditor) SetContent(raw string) {
	e.original = raw
	e.err = nil
	e.textarea.SetValue(raw)
	e.textarea.CursorEnd()
}

// Content returns the current document.
func (e *JSONEditor) Content() string { return e.textarea.Value() }

// Dirty reports whether the document was modified.
func (e *JSONEditor) Dirty() bool { return e.textarea.Value() != e.original }

// Err returns the last validation error, if any.
func (e *JSONEditor) Err() error { return e.err }

// Validate checks that the current document is a valid JSON object.
//
// It returns the decoded document on success.
func (e *JSONEditor) Validate() (map[string]any, error) {
	raw := strings.TrimSpace(e.textarea.Value())
	if raw == "" {
		return nil, errors.New("the document is empty")
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}

	return doc, nil
}

// SetSize sets the available rendering area.
func (e *JSONEditor) SetSize(width, height int) {
	e.width = width
	e.height = height
	e.textarea.SetWidth(width)
	e.textarea.SetHeight(height)
}

// Focus gives the keyboard focus to the editor.
//
// The focus has to reach the textarea: it drops every message while it
// is blurred, so setting the wrapper's flag alone left a field that
// looked focused, accepted no input and gave no sign of the problem.
func (e *JSONEditor) Focus() tea.Cmd {
	e.focused = true

	return e.textarea.Focus()
}

// Blur removes the keyboard focus from the editor.
func (e *JSONEditor) Blur() {
	e.focused = false
	e.textarea.Blur()
}

// Focused reports whether the component handles the key input.
func (e *JSONEditor) Focused() bool { return e.focused }

// Init implements [tea.Model].
func (e *JSONEditor) Init() tea.Cmd { return nil }

// Update implements [tea.Model].
func (e *JSONEditor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		e.SetSize(msg.Width, msg.Height-4)
		return e, nil

	case tea.KeyPressMsg:
		if !e.focused {
			return e, nil
		}

		// The save is handled by the owning page, which owns the
		// store access, so the key is simply swallowed here.
		if msg.String() == "ctrl+s" {
			return e, nil
		}
	}

	var cmd tea.Cmd

	e.textarea, cmd = e.textarea.Update(msg)

	return e, cmd
}

// View implements [tea.Model].
func (e *JSONEditor) View() tea.View {
	var b strings.Builder

	b.WriteString(e.textarea.View())
	b.WriteString("\n")

	if e.err != nil {
		b.WriteString(e.theme.Error.Render("✗ " + e.err.Error()))
	} else {
		b.WriteString(e.theme.Muted.Render("ctrl+s save · esc cancel"))
	}

	return tea.NewView(b.String())
}
