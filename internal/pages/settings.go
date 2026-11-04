package pages

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// SettingsPage shows the effective configuration of the application.
//
// It is read-only on purpose: the configuration is resolved once at
// startup (flags > env > config file) and changing it requires a restart.
type SettingsPage struct {
	// runtime holds the shared state.
	runtime *state.Runtime
	// theme is the shared style palette.
	theme *theme.Theme
	// settings holds the "name: value" rows to display.
	settings [][2]string
	// width is the available rendering width.
	width int
	// height is the available rendering height.
	height int
	// scroller shows the rows.
	//
	// The rows are as many as the configuration has settings, and the
	// page has a height: the scroller shows the part that fits.
	scroller *components.Scroller
}

// NewSettingsPage creates the settings page.
//
// The settings rows are provided by the root model so that the page stays
// independent from the configuration package.
func NewSettingsPage(runtime *state.Runtime, t *theme.Theme, settings [][2]string) *SettingsPage {
	if t == nil {
		t = theme.Default()
	}

	if runtime == nil {
		runtime = state.NewRuntime()
	}

	return &SettingsPage{
		runtime:  runtime,
		theme:    t,
		settings: settings,
		scroller: components.NewScroller(10),
	}
}

// SetRows replaces the displayed rows.
//
// The root model calls it when a value it displays changes, such as the
// UI scale after a zoom.
func (p *SettingsPage) SetRows(rows [][2]string) { p.settings = rows }

// Init implements [tea.Model].
func (p *SettingsPage) Init() tea.Cmd { return nil }

// Update implements [tea.Model].
func (p *SettingsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height

	case tea.KeyPressMsg:
		// The scroll keys belong to the list of the rows.
		if cmd, handled := p.scroller.Update(msg); handled {
			return p, cmd
		}
	}

	return p, nil
}

// View implements [tea.Model].
func (p *SettingsPage) View() tea.View {
	var b []string

	b = append(b, p.theme.Title.Render("Settings"))

	width := 0
	for _, s := range p.settings {
		if len(s[0]) > width {
			width = len(s[0])
		}
	}

	if width > 24 {
		width = 24
	}

	rows := make([]string, 0, len(p.settings))

	for _, s := range p.settings {
		label := s[0]
		if len(label) < width {
			label += strings.Repeat(" ", width-len(label))
		}

		rows = append(rows, p.theme.Label.Render(label)+p.theme.Value.Render(s[1]))
	}

	// The rows are scrolled: the configuration has as many settings as
	// it has, and the page has a height.
	//
	// The height falls back to a value that shows the settings: a page
	// that has not been sized yet still shows its content.
	height := p.height - 3
	if p.height <= 0 {
		height = len(rows)
	}

	if height < 1 {
		height = 1
	}

	p.scroller.SetSize(p.width, height)
	p.scroller.SetContent(joinLines(rows))

	b = append(b, p.scroller.View())
	b = append(b, "")
	b = append(b, p.theme.Help.Render(p.helpLine()))

	return tea.NewView(joinLines(b))
}

// helpLine returns the help of the page.
//
// The scroll keys are advertised only when the rows overflow: a key
// that does nothing is noise.
func (p *SettingsPage) helpLine() string {
	line := "configuration is resolved at startup: flags > env > config file"

	if p.scroller.AtBottom() {
		return line
	}

	return "pgup/pgdn scroll · " + line
}

// FormatSetting renders a "name: value" pair for the settings list.
func FormatSetting(name, value string) [2]string {
	if value == "" {
		value = "(not set)"
	}

	return [2]string{name, value}
}

// SettingsSummary renders a one line summary of the configuration.
//
// It is used by the status bar and the home page.
func SettingsSummary(mode string, collection string) string {
	parts := []string{fmt.Sprintf("mode=%s", mode)}
	if collection != "" {
		parts = append(parts, fmt.Sprintf("collection=%s", collection))
	}

	return strings.Join(parts, " ")
}
