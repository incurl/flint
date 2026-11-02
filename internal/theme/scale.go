package theme

import (
	"strconv"
	"strings"
)

// Scale is a UI scale factor.
//
// The scale multiplies the sizes that the theme controls: the padding
// of the panels and the status bar, and the width of the borders. The
// colours are not affected: a scaled UI is the same UI, only bigger.
//
// The scale is a percentage, so 100 is the normal size.
type Scale int

const (
	// ScaleMin is the smallest scale the theme accepts.
	ScaleMin Scale = 50
	// ScaleMax is the largest scale the theme accepts.
	ScaleMax Scale = 200
	// ScaleDefault is the normal scale.
	ScaleDefault Scale = 100
	// ScaleStep is the amount by which a single zoom key changes the
	// scale.
	ScaleStep Scale = 10
)

// String implements [fmt.Stringer].
//
// The scale is rendered as a percentage, eg. "110%".
func (s Scale) String() string {
	return strconv.Itoa(int(s)) + "%"
}

// Valid reports whether the scale is within the accepted range.
func (s Scale) Valid() bool {
	return s >= ScaleMin && s <= ScaleMax
}

// Clamp returns the scale bounded to the accepted range.
func (s Scale) Clamp() Scale {
	if s < ScaleMin {
		return ScaleMin
	}

	if s > ScaleMax {
		return ScaleMax
	}

	return s
}

// ScaleZoomIn returns the scale increased by one step.
//
// It is a package level helper, so that a caller does not need to hold
// a scale to zoom in.
func ScaleZoomIn(s Scale) Scale { return s.ZoomIn() }

// ScaleZoomOut returns the scale decreased by one step.
//
// It is a package level helper, so that a caller does not need to hold
// a scale to zoom out.
func ScaleZoomOut(s Scale) Scale { return s.ZoomOut() }

// ZoomIn returns the scale increased by one step.
//
// The scale is bounded: zooming in at the maximum returns the maximum.
func (s Scale) ZoomIn() Scale {
	next := s + ScaleStep
	if next > ScaleMax {
		return ScaleMax
	}

	return next
}

// ZoomOut returns the scale decreased by one step.
//
// The scale is bounded: zooming out at the minimum returns the minimum.
func (s Scale) ZoomOut() Scale {
	next := s - ScaleStep
	if next < ScaleMin {
		return ScaleMin
	}

	return next
}

// ParseScale parses a scale from its string form.
//
// It accepts a plain number ("110"), a number with a percent sign
// ("110%") and a multiplier ("1.1"). An empty string yields
// [ScaleDefault].
//
// It returns an error when the value cannot be parsed or is out of
// range.
func ParseScale(value string) (Scale, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return ScaleDefault, nil
	}

	multiplier := false

	switch {
	case strings.HasSuffix(trimmed, "%"):
		trimmed = strings.TrimSuffix(trimmed, "%")
	case strings.HasSuffix(trimmed, "x"):
		trimmed = strings.TrimSuffix(trimmed, "x")
		multiplier = true
	}

	number, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return ScaleDefault, err
	}

	if multiplier {
		number *= 100
	}

	scale := Scale(number)
	if !scale.Valid() {
		return ScaleDefault, errScaleOutOfRange
	}

	return scale, nil
}

// errScaleOutOfRange is returned when a scale is outside the accepted
// range.
var errScaleOutOfRange = scaleError("theme: the scale is out of range")

// scaleError is the error type of the scale parsing.
type scaleError string

// Error implements the error interface.
func (e scaleError) Error() string { return string(e) }
