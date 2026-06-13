package tui

import (
	"testing"

	"github.com/cnjack/jcode/internal/theme"
)

// TestApplyThemeSwitchesPalette verifies the de-freeze refactor: switching
// themes at runtime actually changes the package-level color values (and thus
// the derived styles). The original code baked colors into package vars at
// import time, so this guards against regressing back to frozen styles.
func TestApplyThemeSwitchesPalette(t *testing.T) {
	defer ApplyTheme(theme.DefaultDark) // restore for other tests

	ApplyTheme("jcode-dark")
	if currentTheme.Name != "jcode-dark" {
		t.Fatalf("currentTheme = %q, want jcode-dark", currentTheme.Name)
	}
	darkR, _, _, _ := colorText.RGBA()
	darkPlan, _, _, _ := colorPlanMode.RGBA()

	ApplyTheme("jcode-light")
	if currentTheme.Name != "jcode-light" {
		t.Fatalf("currentTheme = %q, want jcode-light", currentTheme.Name)
	}
	lightR, _, _, _ := colorText.RGBA()
	if darkR == lightR {
		t.Error("colorText did not change between dark and light themes")
	}

	// The status-icon strings are pre-rendered; they must be rebuilt too.
	if toolIconSuccess == "" {
		t.Error("toolIconSuccess not rebuilt after ApplyTheme")
	}

	// Unknown names fall back to the dark default rather than a zero palette.
	ApplyTheme("totally-unknown")
	if currentTheme.Name != theme.DefaultDark {
		t.Errorf("unknown theme should fall back to %q, got %q", theme.DefaultDark, currentTheme.Name)
	}
	fallbackPlan, _, _, _ := colorPlanMode.RGBA()
	if fallbackPlan != darkPlan {
		t.Error("fallback theme palette does not match jcode-dark")
	}
}

// TestGlamourStyleTracksAppearance ensures markdown rendering follows the
// active theme's light/dark appearance.
func TestGlamourStyleTracksAppearance(t *testing.T) {
	defer ApplyTheme(theme.DefaultDark)

	ApplyTheme("jcode-light")
	if got := currentTheme.GlamourStyle(); got != "light" {
		t.Errorf("light theme GlamourStyle = %q, want light", got)
	}
	ApplyTheme("jcode-dark")
	if got := currentTheme.GlamourStyle(); got != "dark" {
		t.Errorf("dark theme GlamourStyle = %q, want dark", got)
	}
}
