package tui

import (
	"image/color"
	"testing"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
)

// TestShimmerTextPreservesContent asserts the sweep only ever recolors: the
// plain text survives ANSI stripping, and the band actually moves over time.
func TestShimmerTextPreservesContent(t *testing.T) {
	old := shimmerDisabled
	defer func() { shimmerDisabled = old }()
	shimmerDisabled = false

	now := time.UnixMilli(500)
	out := shimmerText(now)
	if got := xansi.Strip(out); got != "Working" {
		t.Fatalf("shimmer altered text content: %q", got)
	}
	if out2 := shimmerText(now.Add(shimmerPeriod / 2)); out == out2 {
		t.Fatal("shimmer band did not move between frames")
	}
}

// TestShimmerReducedMotion pins the opt-out: plain, unstyled text.
func TestShimmerReducedMotion(t *testing.T) {
	old := shimmerDisabled
	defer func() { shimmerDisabled = old }()
	shimmerDisabled = true

	if got := shimmerText(time.UnixMilli(500)); got != "Working" {
		t.Fatalf("reduced motion must render plain text, got %q", got)
	}
}

// TestBlendColor checks the channel lerp at both ends and the midpoint.
func TestBlendColor(t *testing.T) {
	black := color.RGBA{0, 0, 0, 255}
	white := color.RGBA{255, 255, 255, 255}

	if c := blendColor(black, white, 0); c != color.Color(black) {
		t.Errorf("t=0 should return the base color, got %v", c)
	}
	if c := blendColor(black, white, 1); c != color.Color(white) {
		t.Errorf("t=1 should return the highlight color, got %v", c)
	}
	r, g, b, _ := blendColor(black, white, 0.5).RGBA()
	for name, v := range map[string]uint32{"r": r >> 8, "g": g >> 8, "b": b >> 8} {
		if v < 126 || v > 129 {
			t.Errorf("midpoint channel %s out of range: %d", name, v)
		}
	}
}
