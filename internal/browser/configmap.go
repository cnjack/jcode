package browser

import "github.com/cnjack/jcode/internal/config"

const defaultViewport = "1280x720"

// FromConfig maps the persisted browser settings into the runtime shape and
// applies defaults in one place. A nil config means browser use has not been
// enabled yet, so it remains off while the non-sensitive runtime defaults are
// still populated for the settings/status UI.
func FromConfig(c *config.BrowserConfig) Config {
	if c == nil {
		return Config{Backend: "auto", Viewport: defaultViewport}
	}
	backend := c.Backend
	if backend == "" {
		backend = "auto"
	}
	viewport := c.Viewport
	if viewport == "" {
		viewport = defaultViewport
	}
	return Config{
		Enabled:    c.Enabled,
		Backend:    backend,
		ChromePath: c.ChromePath,
		Headless:   c.Headless,
		Viewport:   viewport,
		DevMode:    c.DevMode,
	}
}
