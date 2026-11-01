package theme_test

import (
	"testing"

	"github.com/incurl/flint/internal/theme"
)

// TestNewResolvesVariants verifies that every known variant is resolved and
// that an unknown one falls back to the dark palette.
func TestNewResolvesVariants(t *testing.T) {
	tests := []struct {
		name    string
		variant theme.Variant
		want    theme.Variant
	}{
		{name: "dark", variant: theme.VariantDark, want: theme.VariantDark},
		{name: "light", variant: theme.VariantLight, want: theme.VariantLight},
		{name: "unknown", variant: theme.Variant("neon"), want: theme.VariantDark},
		{name: "empty", variant: theme.Variant(""), want: theme.VariantDark},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := theme.New(tt.variant)
			if got.Variant != tt.want {
				t.Errorf("New(%q).Variant = %q, want %q", tt.variant, got.Variant, tt.want)
			}
			if got.Palette.Foreground == "" {
				t.Error("New() returned an empty foreground colour")
			}
		})
	}
}

// TestVariantValid verifies the variant validation.
func TestVariantValid(t *testing.T) {
	if !theme.VariantDark.Valid() {
		t.Error("VariantDark.Valid() = false, want true")
	}
	if !theme.VariantLight.Valid() {
		t.Error("VariantLight.Valid() = false, want true")
	}
	if theme.Variant("other").Valid() {
		t.Error("an unknown variant reported itself as valid")
	}
}

// TestStatusColor verifies that the connection status maps to a style.
func TestStatusColor(t *testing.T) {
	tm := theme.Default()

	for _, status := range []string{"ok", "degraded", "unreachable", "unknown"} {
		style := tm.StatusColor(status)
		if style.GetForeground() == nil && status != "unknown" {
			t.Errorf("StatusColor(%q) returned no foreground colour", status)
		}
	}
}

// TestLayoutModeFor verifies the responsive breakpoints.
func TestLayoutModeFor(t *testing.T) {
	bp := theme.DefaultBreakpoints()

	tests := []struct {
		width int
		want  theme.LayoutMode
	}{
		{width: 40, want: theme.LayoutNarrow},
		{width: 80, want: theme.LayoutNarrow},
		{width: 120, want: theme.LayoutMedium},
		{width: 200, want: theme.LayoutWide},
	}

	for _, tt := range tests {
		if got := theme.LayoutModeFor(tt.width, bp); got != tt.want {
			t.Errorf("LayoutModeFor(%d) = %q, want %q", tt.width, got, tt.want)
		}
	}
}

// TestSplitWidth verifies the two column width computation.
func TestSplitWidth(t *testing.T) {
	tests := []struct {
		name      string
		total     int
		left      int
		gap       int
		wantLeft  int
		wantRight int
	}{
		{name: "even split", total: 100, left: 0, gap: 2, wantLeft: 49, wantRight: 49},
		{name: "fixed left", total: 100, left: 60, gap: 2, wantLeft: 60, wantRight: 38},
		{name: "too narrow", total: 3, left: 10, gap: 2, wantLeft: 3, wantRight: 0},
		{name: "no gap", total: 80, left: 40, gap: 0, wantLeft: 40, wantRight: 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			left, right := theme.SplitWidth(tt.total, tt.left, tt.gap)
			if left != tt.wantLeft {
				t.Errorf("SplitWidth() left = %d, want %d", left, tt.wantLeft)
			}
			if right != tt.wantRight {
				t.Errorf("SplitWidth() right = %d, want %d", right, tt.wantRight)
			}
		})
	}
}

// TestTruncate verifies the width aware truncation.
func TestTruncate(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
	}{
		{name: "shorter than the limit", in: "abc", width: 10},
		{name: "exactly the limit", in: "abc", width: 3},
		{name: "longer than the limit", in: "abcdefghij", width: 5},
		{name: "zero width", in: "abc", width: 0},
		{name: "negative width", in: "abc", width: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := theme.Truncate(tt.in, tt.width)
			if tt.width <= 0 {
				if got != "" {
					t.Errorf("Truncate() = %q, want an empty string", got)
				}
				return
			}
			if len(got) > tt.width && tt.width > 1 {
				t.Errorf("Truncate(%q, %d) = %q, which is too long", tt.in, tt.width, got)
			}
		})
	}
}
