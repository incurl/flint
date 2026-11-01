package pages

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

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
	}
}

// Init implements [tea.Model].
func (p *SettingsPage) Init() tea.Cmd { return nil }

// Update implements [tea.Model].
func (p *SettingsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		p.width = msg.Width
		p.height = msg.Height
	}

	return p, nil
}

// View implements [tea.Model].
func (p *SettingsPage) View() tea.View {
	var b []string

	b = append(b, p.theme.Title.Render("Settings"))
	b = append(b, "")

	width := 0
	for _, s := range p.settings {
		if len(s[0]) > width {
			width = len(s[0])
		}
	}

	if width > 24 {
		width = 24
	}

	for _, s := range p.settings {
		label := s[0]
		if len(label) < width {
			label += strings.Repeat(" ", width-len(label))
		}

		b = append(b, p.theme.Label.Render(label)+p.theme.Value.Render(s[1]))
	}

	b = append(b, "")
	b = append(b, p.theme.Help.Render("configuration is resolved at startup: flags > env > config file"))

	return tea.NewView(joinLines(b))
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
