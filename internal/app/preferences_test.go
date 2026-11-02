package app_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/config"
	"github.com/incurl/flint/internal/app"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/theme"
)

// TestZoomChangesTheScale verifies that the zoom hotkey changes the UI
// scale of the shared theme.
func TestZoomChangesTheScale(t *testing.T) {
	m := newTestModel(t)

	before := m.Theme().Scale

	updated, _ := m.Update(tea.KeyPressMsg{Code: '+', Text: "+"})
	m = updated.(*app.Model)

	after := m.Theme().Scale
	if after <= before {
		t.Errorf("the scale did not grow: %d -> %d", int(before), int(after))
	}

	if after != theme.ScaleDefault+theme.ScaleStep {
		t.Errorf("scale = %d, want %d", int(after), int(theme.ScaleDefault+theme.ScaleStep))
	}
}

// TestZoomOutShrinksTheScale verifies the opposite direction.
func TestZoomOutShrinksTheScale(t *testing.T) {
	m := newTestModel(t)

	// Zoom in first, so that there is room to zoom out.
	updated, _ := m.Update(tea.KeyPressMsg{Code: '+', Text: "+"})
	m = updated.(*app.Model)

	grown := m.Theme().Scale

	updated, _ = m.Update(tea.KeyPressMsg{Code: '-', Text: "-"})
	m = updated.(*app.Model)

	if m.Theme().Scale >= grown {
		t.Errorf("the scale did not shrink: %d -> %d", int(grown), int(m.Theme().Scale))
	}
}

// TestZoomIsBounded verifies that the scale cannot leave the accepted
// range.
func TestZoomIsBounded(t *testing.T) {
	m := newTestModel(t)

	for i := 0; i < 40; i++ {
		updated, _ := m.Update(tea.KeyPressMsg{Code: '+', Text: "+"})
		m = updated.(*app.Model)
	}

	if m.Theme().Scale != theme.ScaleMax {
		t.Errorf("scale = %d, want the maximum %d", int(m.Theme().Scale), int(theme.ScaleMax))
	}

	for i := 0; i < 40; i++ {
		updated, _ := m.Update(tea.KeyPressMsg{Code: '-', Text: "-"})
		m = updated.(*app.Model)
	}

	if m.Theme().Scale != theme.ScaleMin {
		t.Errorf("scale = %d, want the minimum %d", int(m.Theme().Scale), int(theme.ScaleMin))
	}
}

// TestToggleTheme verifies that the theme hotkey switches the palette.
func TestToggleTheme(t *testing.T) {
	m := newTestModel(t)

	before := m.Theme().Variant

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'T', Text: "T"})
	m = updated.(*app.Model)

	if m.Theme().Variant == before {
		t.Errorf("the variant did not change: %q", m.Theme().Variant)
	}

	// A second press switches it back.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'T', Text: "T"})
	m = updated.(*app.Model)

	if m.Theme().Variant != before {
		t.Errorf("the variant = %q, want %q", m.Theme().Variant, before)
	}
}

// TestTheSettingsPageShowsTheScale verifies that the effective scale is
// visible on the settings page.
func TestTheSettingsPageShowsTheScale(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(tea.KeyPressMsg{Code: '+', Text: "+"})
	m = updated.(*app.Model)

	// Navigate to the settings page.
	updated, _ = m.Update(tea.KeyPressMsg{Code: '5', Text: "5"})
	m = updated.(*app.Model)

	view := m.View().Content
	if !strings.Contains(view, "scale") {
		t.Errorf("View() = %q, want it to contain the scale", view)
	}

	if !strings.Contains(view, "110%") {
		t.Errorf("View() = %q, want it to contain the current scale", view)
	}
}

// TestToggleCompact verifies that the compact hotkey switches the
// record list layout.
func TestToggleCompact(t *testing.T) {
	m := newTestModel(t)

	before := m.Preferences().Compact

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m = updated.(*app.Model)

	if m.Preferences().Compact == before {
		t.Error("the compact flag did not change")
	}

	// A second press switches it back.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m = updated.(*app.Model)

	if m.Preferences().Compact != before {
		t.Error("the compact flag did not switch back")
	}
}

// TestEveryStoredPreferenceIsRestored verifies that every preference
// survives a restart.
//
// The regression this guards against is a preference that is stored
// but never read back at startup: the value is written to the store
// and then silently ignored.
func TestEveryStoredPreferenceIsRestored(t *testing.T) {
	// The stub store returns the stored preferences.
	store := &stubStore{
		hasPreferences: true,
		preferences: pbstore.Preferences{
			Theme:   "light",
			Scale:   120,
			PerPage: 50,
			Compact: true,
		},
	}

	m := app.New(store, config.Default(), nil)

	if got := m.Theme().Variant; got != theme.VariantLight {
		t.Errorf("the variant = %q, want %q", got, theme.VariantLight)
	}

	if got := m.Theme().Scale; got != 120 {
		t.Errorf("the scale = %d, want 120", int(got))
	}

	if got := m.Preferences().PerPage; got != 50 {
		t.Errorf("the page size = %d, want 50", got)
	}

	if got := m.Preferences().Compact; !got {
		t.Error("the compact flag was not restored")
	}
}

// TestTheZoomIsNotAppliedWhileTyping verifies that the global hotkey
// layer stays out of the way of a text input.
func TestTheZoomIsNotAppliedWhileTyping(t *testing.T) {
	m := newTestModel(t)

	before := m.Theme().Scale

	// Focus the filter of the records page.
	updated, _ := m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m = updated.(*app.Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(*app.Model)

	// The "+" must be typed, not treated as a zoom.
	updated, _ = m.Update(tea.KeyPressMsg{Code: '+', Text: "+"})
	m = updated.(*app.Model)

	if m.Theme().Scale != before {
		t.Errorf("the scale changed to %d while typing, want %d",
			int(m.Theme().Scale), int(before))
	}
}
