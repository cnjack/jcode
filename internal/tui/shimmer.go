package tui

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// ─── Status-line shimmer (P3-14) ───
//
// A cosine highlight band sweeps across the "Working" verb by blending each
// character's foreground between the theme's dim and primary text colors —
// the codex shimmer.rs approach. Only existing theme colors are mixed, so no
// new hue can appear under any theme. The repaint cadence rides the spinner
// tick that is already active while thinking; no extra timer is introduced.

const (
	// shimmerPeriod is the time of one full left-to-right sweep.
	shimmerPeriod = 2 * time.Second
	// shimmerBand is the highlight half-width, in characters.
	shimmerBand = 3.0
)

// shimmerDisabled turns the sweep into plain static text. Evaluated once at
// startup: JCODE_REDUCED_MOTION is the dedicated switch, and the conventional
// NO_COLOR implies reduced decoration too. Tests may set it directly.
var shimmerDisabled = os.Getenv("JCODE_REDUCED_MOTION") != "" || os.Getenv("NO_COLOR") != ""

// blendColor linearly interpolates a→b at t∈[0,1] per sRGB channel.
func blendColor(a, b color.Color, t float64) color.Color {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	lerp := func(x, y uint32) uint8 {
		return uint8(float64(x>>8) + (float64(y>>8)-float64(x>>8))*t)
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", lerp(ar, br), lerp(ag, bg), lerp(ab, bb)))
}

// shimmerText renders "Working" with the highlight band positioned by now: dim
// base characters, with a cosine-shaped bright band sweeping across each period.
// Under reduced motion (or missing palette) it returns "Working" unstyled.
func shimmerText(now time.Time) string {
	const s = "Working"
	if shimmerDisabled || colorText == nil || colorDimText == nil {
		return s
	}
	runes := []rune(s)
	n := len(runes)
	if n == 0 {
		return s
	}

	// The band center travels from -band to n+band over one period, so the
	// highlight fully enters and exits instead of wrapping mid-glyph.
	frac := float64(now.UnixMilli()%shimmerPeriod.Milliseconds()) / float64(shimmerPeriod.Milliseconds())
	center := frac*(float64(n)+2*shimmerBand) - shimmerBand

	var sb strings.Builder
	for i, r := range runes {
		d := math.Abs(float64(i) - center)
		t := 0.0
		if d < shimmerBand {
			// Cosine falloff: 1 at the band center, 0 at its edge.
			t = 0.5 * (1 + math.Cos(math.Pi*d/shimmerBand))
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(blendColor(colorDimText, colorText, t)).Render(string(r)))
	}
	return sb.String()
}
