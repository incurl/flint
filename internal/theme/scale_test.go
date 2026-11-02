package theme_test

import (
	"testing"

	"github.com/incurl/flint/internal/theme"
)

func TestScaleString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scale theme.Scale
		want  string
	}{
		{theme.ScaleDefault, "100%"},
		{theme.Scale(110), "110%"},
		{theme.ScaleMin, "50%"},
		{theme.ScaleMax, "200%"},
	}

	for _, tt := range tests {
		if got := tt.scale.String(); got != tt.want {
			t.Errorf("Scale(%d).String() = %q, want %q", int(tt.scale), got, tt.want)
		}
	}
}

func TestScaleZoom(t *testing.T) {
	t.Parallel()

	// Zooming in and out is symmetric.
	if got := theme.ScaleDefault.ZoomIn(); got != 110 {
		t.Errorf("ZoomIn() = %d, want 110", int(got))
	}

	if got := theme.Scale(110).ZoomOut(); got != 100 {
		t.Errorf("ZoomOut() = %d, want 100", int(got))
	}

	// The bounds are respected.
	if got := theme.ScaleMax.ZoomIn(); got != theme.ScaleMax {
		t.Errorf("ZoomIn() at the max = %d, want %d", int(got), int(theme.ScaleMax))
	}

	if got := theme.ScaleMin.ZoomOut(); got != theme.ScaleMin {
		t.Errorf("ZoomOut() at the min = %d, want %d", int(got), int(theme.ScaleMin))
	}
}

func TestScaleClamp(t *testing.T) {
	t.Parallel()

	if got := theme.Scale(10).Clamp(); got != theme.ScaleMin {
		t.Errorf("Clamp() = %d, want %d", int(got), int(theme.ScaleMin))
	}

	if got := theme.Scale(999).Clamp(); got != theme.ScaleMax {
		t.Errorf("Clamp() = %d, want %d", int(got), int(theme.ScaleMax))
	}

	if got := theme.Scale(120).Clamp(); got != 120 {
		t.Errorf("Clamp() = %d, want 120", int(got))
	}
}

func TestScaleValid(t *testing.T) {
	t.Parallel()

	if theme.Scale(10).Valid() {
		t.Error("Scale(10).Valid() = true, want false")
	}

	if theme.Scale(999).Valid() {
		t.Error("Scale(999).Valid() = true, want false")
	}

	if !theme.ScaleDefault.Valid() {
		t.Error("ScaleDefault.Valid() = false, want true")
	}
}

func TestParseScale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  theme.Scale
	}{
		{"", theme.ScaleDefault},
		{"110", 110},
		{"110%", 110},
		{"1.1x", 110},
		{" 120 ", 120},
	}

	for _, tt := range tests {
		got, err := theme.ParseScale(tt.value)
		if err != nil {
			t.Errorf("ParseScale(%q) error = %v", tt.value, err)

			continue
		}

		if got != tt.want {
			t.Errorf("ParseScale(%q) = %d, want %d", tt.value, int(got), int(tt.want))
		}
	}
}

func TestParseScaleErrors(t *testing.T) {
	t.Parallel()

	// A value that is not a number.
	if _, err := theme.ParseScale("abc"); err == nil {
		t.Error("ParseScale(\"abc\") returned no error")
	}

	// A value out of the accepted range.
	if _, err := theme.ParseScale("500"); err == nil {
		t.Error("ParseScale(\"500\") returned no error")
	}
}

func TestScalePadding(t *testing.T) {
	t.Parallel()

	// The normal scale keeps the default padding of one cell.
	if got := theme.ScaleDefault.Padding(); got != 1 {
		t.Errorf("Padding() at the default = %d, want 1", got)
	}

	// A larger scale pads more.
	if got := theme.Scale(200).Padding(); got != 2 {
		t.Errorf("Padding() at 200%% = %d, want 2", got)
	}
}

func TestApplyChangesTheThemeInPlace(t *testing.T) {
	t.Parallel()

	th := theme.New(theme.VariantDark, theme.ScaleDefault)

	dark := th.Base.GetForeground()

	th.Apply(theme.VariantLight, theme.ScaleDefault)

	if th.Variant != theme.VariantLight {
		t.Errorf("Variant = %q, want %q", th.Variant, theme.VariantLight)
	}

	if th.Base.GetForeground() == dark {
		t.Error("the foreground did not change with the variant")
	}
}

func TestApplyKeepsTheSamePointer(t *testing.T) {
	t.Parallel()

	// The pages hold a pointer to the theme and read the styles when
	// they render. The theme must therefore be updated in place, so
	// that every page sees the change without being rebuilt.
	th := theme.New(theme.VariantDark, theme.ScaleDefault)

	before := th.Panel.GetPaddingLeft()

	th.Apply(th.Variant, theme.Scale(200))

	if th.Panel.GetPaddingLeft() <= before {
		t.Errorf("the panel padding did not grow: %d -> %d",
			before, th.Panel.GetPaddingLeft())
	}

	if th.Scale != 200 {
		t.Errorf("Scale = %d, want 200", int(th.Scale))
	}
}

func TestApplyClampsTheScale(t *testing.T) {
	t.Parallel()

	th := theme.New(theme.VariantDark, theme.ScaleDefault)

	th.Apply(th.Variant, theme.Scale(9999))

	if th.Scale != theme.ScaleMax {
		t.Errorf("Scale = %d, want %d", int(th.Scale), int(theme.ScaleMax))
	}
}

func TestApplyFallsBackToADarkTheme(t *testing.T) {
	t.Parallel()

	th := theme.New(theme.VariantDark, theme.ScaleDefault)

	th.Apply(theme.Variant("nonsense"), theme.ScaleDefault)

	if th.Variant != theme.VariantDark {
		t.Errorf("Variant = %q, want %q", th.Variant, theme.VariantDark)
	}
}
