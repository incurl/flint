package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/theme"
)

// Theme returns the shared style palette.
//
// It is exposed so that a caller — a test, or a component that needs to
// build a style — can read the current variant and scale.
func (m *Model) Theme() *theme.Theme { return m.theme }

// Preferences returns the user preferences.
func (m *Model) Preferences() pbstore.Preferences { return m.prefs }

// zoom changes the UI scale and persists the new preferences.
//
// The scale is applied to the shared theme in place, so every page picks
// it up on the next frame without being rebuilt.
//
// It returns the command that shows the confirmation toast.
func (m *Model) zoom(direction func(theme.Scale) theme.Scale) tea.Cmd {
	current := theme.Scale(m.prefs.Scale)
	if !current.Valid() {
		current = theme.ScaleDefault
	}

	next := direction(current)
	if next == current {
		// The scale is at its bound: say so instead of doing nothing
		// silently.
		return m.toast.Show(components.ToastInfo, fmt.Sprintf(
			"the zoom is already at %s", current))
	}

	m.prefs.Scale = int(next)
	m.theme.Apply(m.theme.Variant, next)

	m.refreshSettings()

	return tea.Batch(
		m.persistPreferences(),
		m.toast.Show(components.ToastSuccess, fmt.Sprintf("zoom %s", next)),
	)
}

// toggleTheme switches between the dark and the light palette.
//
// It returns the command that shows the confirmation toast.
func (m *Model) toggleTheme() tea.Cmd {
	next := theme.VariantLight
	if m.theme.Variant == theme.VariantLight {
		next = theme.VariantDark
	}

	m.prefs.Theme = next.String()
	m.theme.Apply(next, theme.Scale(m.prefs.Scale))

	m.refreshSettings()

	return tea.Batch(
		m.persistPreferences(),
		m.toast.Show(components.ToastSuccess, fmt.Sprintf("theme %s", next)),
	)
}

// toggleCompact switches the compact record list.
//
// It returns the command that shows the confirmation toast.
func (m *Model) toggleCompact() tea.Cmd {
	m.prefs.Compact = !m.prefs.Compact
	m.runtime.Compact = m.prefs.Compact

	m.refreshSettings()

	label := "compact rows"
	if !m.prefs.Compact {
		label = "comfortable rows"
	}

	return tea.Batch(
		m.persistPreferences(),
		m.toast.Show(components.ToastSuccess, label),
	)
}

// refreshSettings updates the settings page with the live preferences.
//
// The configuration rows describe the deployment, which never changes
// while the application runs. The preferences describe the current
// state, so they are replaced in place.
func (m *Model) refreshSettings() {
	rows := make([][2]string, 0, len(m.settings)+1)

	for _, row := range m.settings {
		// The scale and compact rows are rebuilt below, so the previous
		// ones are dropped: the rows are refreshed on every change and
		// must not accumulate.
		if row[0] == "scale" || row[0] == "compact" {
			continue
		}

		switch row[0] {
		case "theme":
			row[1] = m.theme.Variant.String()
		case "per_page":
			row[1] = strconv.Itoa(m.prefs.PerPage)
		}

		rows = append(rows, row)
	}

	rows = append(rows, [2]string{"scale", theme.Scale(m.prefs.Scale).String()})
	rows = append(rows, [2]string{"compact", strconv.FormatBool(m.prefs.Compact)})

	m.settings = rows
	m.settingsPage.SetRows(rows)
}

// persistPreferences returns the command that writes the preferences to
// the store.
//
// The write happens in the command, and not in the update loop: a
// store call blocks, and the render loop must never block. The
// preferences are a convenience, so a failure to store them does not
// break the application: the error is only logged.
//
// The preferences are captured by value: the model keeps mutating
// while the command runs, and the command must write the ones the
// change was about.
func (m *Model) persistPreferences() tea.Cmd {
	store := m.store
	if store == nil {
		return nil
	}

	prefs := m.prefs

	return func() tea.Msg {
		if err := pbstore.SavePreferences(context.Background(), store, prefs); err != nil {
			// The remote backend cannot hold the preferences. That is
			// not an error the user can act on, so it stays silent.
			if !errors.Is(err, pbstore.ErrPreferencesUnavailable) {
				slog.Error("failed to persist the preferences", "error", err)
			}
		}

		return nil
	}
}
