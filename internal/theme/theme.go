// Package theme holds the single source of truth for every visual style
// used by the TUI.
//
// Inline lipgloss styles are forbidden across the code base: components and
// pages must compose the styles exposed here so that a downstream project
// can restyle the whole application by swapping a theme variant.
package theme

import (
	"charm.land/lipgloss/v2"
)

// Variant selects a colour palette.
type Variant string

const (
	// VariantDark is the default palette, tuned for dark terminals.
	VariantDark Variant = "dark"
	// VariantLight is a palette tuned for light terminals.
	VariantLight Variant = "light"
)

// String implements [fmt.Stringer].
func (v Variant) String() string { return string(v) }

// Valid reports whether the variant is one of the known palettes.
func (v Variant) Valid() bool {
	switch v {
	case VariantDark, VariantLight:
		return true
	default:
		return false
	}
}

// Palette holds the raw colours of a theme variant.
type Palette struct {
	// Background is the app background colour.
	Background string
	// Foreground is the default text colour.
	Foreground string
	// Muted is the colour of the secondary/dimmed text.
	Muted string
	// Primary is the accent colour used for the active elements.
	Primary string
	// Secondary is the accent colour used for the secondary elements.
	Secondary string
	// Accent is the highlight colour.
	Accent string
	// Success is the colour of the positive states.
	Success string
	// Warning is the colour of the cautionary states.
	Warning string
	// Error is the colour of the error states.
	Error string
	// Border is the default border colour.
	Border string
	// BorderFocus is the border colour of the focused element.
	BorderFocus string
	// Selection is the background colour of the selected row.
	Selection string
	// SelectionText is the text colour of the selected row.
	SelectionText string
}

// DarkPalette returns the palette used on dark terminals.
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

// LightPalette returns the palette used on light terminals.
func LightPalette() Palette {
	return Palette{
		Background:    "#fafafa",
		Foreground:    "#1c1c1c",
		Muted:         "#6c6c6c",
		Primary:       "#2f5fd0",
		Secondary:     "#7b3fbf",
		Accent:        "#0f6f8f",
		Success:       "#2f7d32",
		Warning:       "#a06a00",
		Error:         "#c62839",
		Border:        "#d0d0d0",
		BorderFocus:   "#2f5fd0",
		Selection:     "#d6e0f7",
		SelectionText: "#101010",
	}
}

// Theme is the resolved set of styles used by the application.
type Theme struct {
	// Variant identifies the palette in use.
	Variant Variant
	// Scale is the UI scale factor.
	Scale Scale
	// Palette holds the raw colours.
	Palette Palette

	// Base is the default text style.
	Base lipgloss.Style
	// Muted is the dimmed text style.
	Muted lipgloss.Style
	// Title is the style of the page titles.
	Title lipgloss.Style
	// Subtitle is the style of the secondary page headers.
	Subtitle lipgloss.Style
	// Accent is the highlighted text style.
	Accent lipgloss.Style
	// Success is the style of the positive messages.
	Success lipgloss.Style
	// Warning is the style of the cautionary messages.
	Warning lipgloss.Style
	// Error is the style of the error messages.
	Error lipgloss.Style
	// Selected is the style of the selected table row.
	Selected lipgloss.Style
	// Normal is the style of a regular table row.
	Normal lipgloss.Style
	// Help is the style of the key hints.
	Help lipgloss.Style
	// StatusBar is the style of the persistent status bar.
	StatusBar lipgloss.Style
	// StatusMode is the style of the store mode indicator.
	StatusMode lipgloss.Style
	// Panel is the style of a bordered panel.
	Panel lipgloss.Style
	// PanelFocused is the style of a focused bordered panel.
	PanelFocused lipgloss.Style
	// Label is the style of the form labels.
	Label lipgloss.Style
	// Value is the style of the form values.
	Value lipgloss.Style
	// Toast is the style of the toast notification frame.
	Toast lipgloss.Style
	// TableHeader is the style of the table column headers.
	TableHeader lipgloss.Style
	// TableCell is the style of the regular table cells.
	TableCell lipgloss.Style
	// TableSelected is the style of the selected table row.
	TableSelected lipgloss.Style
	// ListSelectedTitle is the style of the selected list item title.
	ListSelectedTitle lipgloss.Style
	// ListSelectedDesc is the style of the selected list item description.
	ListSelectedDesc lipgloss.Style
	// TableHeaderBorder is the border style of the table header.
	TableHeaderBorder lipgloss.Style
	// ListSelectedBorder is the border style of the selected list item.
	ListSelectedBorder lipgloss.Style
}

// New resolves the styles of the requested variant.
//
// An unknown variant falls back to [VariantDark] so that a typo in the
// configuration never crashes the application.
// New creates a theme with the provided variant and scale.
//
// The scale defaults to [ScaleDefault] when it is out of the accepted
// range.
func New(variant Variant, scale Scale) *Theme {
	if !variant.Valid() {
		variant = VariantDark
	}

	t := &Theme{}
	t.Apply(variant, scale)

	return t
}

// Default returns the default theme.
func Default() *Theme { return New(VariantDark, ScaleDefault) }

// Apply rebuilds the theme in place with the provided variant and scale.
//
// The styles are replaced in place rather than returning a new theme, so
// that every component holding a pointer to the theme sees the change
// immediately. This is what makes a runtime theme switch possible: the
// pages read the styles when they render, not when they are created.
//
// The scale is clamped to the accepted range.
func (t *Theme) Apply(variant Variant, scale Scale) {
	if t == nil {
		return
	}

	if !variant.Valid() {
		variant = VariantDark
	}

	scale = scale.Clamp()

	palette := DarkPalette()
	if variant == VariantLight {
		palette = LightPalette()
	}

	t.Variant = variant
	t.Palette = palette
	t.Scale = scale

	// The padding follows the scale. The border width does not: lipgloss
	// renders a border in a single cell, whatever the scale.
	padding := scale.Padding()

	t.Base = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.Foreground))
	t.Muted = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.Muted))
	t.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(palette.Primary))
	t.Subtitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(palette.Secondary))
	t.Accent = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.Accent))
	t.Success = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.Success))
	t.Warning = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.Warning))
	t.Error = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.Error))
	t.Selected = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.SelectionText)).
		Background(lipgloss.Color(palette.Selection))
	t.Normal = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.Foreground))
	t.Help = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.Muted))
	t.StatusBar = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.Foreground)).
		Background(lipgloss.Color(palette.Background)).
		Padding(0, padding)
	t.StatusMode = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(palette.Primary))
	t.Panel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(palette.Border)).
		Padding(0, padding)
	t.PanelFocused = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(palette.BorderFocus)).
		Padding(0, padding)
	t.Label = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(palette.Muted))
	t.Value = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.Foreground))
	t.Toast = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(palette.Border)).
		Padding(0, padding)
	t.TableHeader = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(palette.Secondary))
	t.TableCell = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.Foreground))
	t.TableSelected = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.SelectionText)).
		Background(lipgloss.Color(palette.Selection))
	t.ListSelectedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.SelectionText)).
		Background(lipgloss.Color(palette.Selection))
	t.ListSelectedDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(palette.SelectionText)).
		Background(lipgloss.Color(palette.Selection))
	t.TableHeaderBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(palette.Border)).
		BorderBottom(true)
	t.ListSelectedBorder = lipgloss.NewStyle().
		BorderForeground(lipgloss.Color(palette.Selection))
}

// Padding returns the horizontal padding of the panels, for the scale.
//
// A scale of 100 keeps the default padding of one cell.
func (s Scale) Padding() int {
	return int(float64(s) / 100)
}

// StatusColor returns the style associated with a connection status.
//
// The status values are the ones produced by the store health checks.
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
