package browser

import (
	"testing"

	"github.com/cnjack/jcode/internal/config"
)

func TestFromConfigDefaultsToDisabledAutoManagedBrowser(t *testing.T) {
	got := FromConfig(nil)
	if got.Enabled {
		t.Fatal("nil browser config must remain disabled")
	}
	if got.Backend != "auto" || got.Viewport != defaultViewport {
		t.Fatalf("defaults = %+v, want backend=auto viewport=%s", got, defaultViewport)
	}
}

func TestFromConfigAppliesDefaultsAndPreservesSettings(t *testing.T) {
	got := FromConfig(&config.BrowserConfig{
		Enabled:    true,
		ChromePath: "/opt/chrome",
		Headless:   true,
		DevMode:    true,
	})
	if !got.Enabled || got.Backend != "auto" || got.Viewport != defaultViewport ||
		got.ChromePath != "/opt/chrome" || !got.Headless || !got.DevMode {
		t.Fatalf("mapped config = %+v", got)
	}

	got = FromConfig(&config.BrowserConfig{Backend: "extension", Viewport: "1440x900"})
	if got.Backend != "extension" || got.Viewport != "1440x900" {
		t.Fatalf("explicit settings were not preserved: %+v", got)
	}
}
