package command

import (
	"github.com/cnjack/jcode/internal/computer"
	"github.com/cnjack/jcode/internal/config"
)

// newComputerManager is the single production composition point for Computer
// Use. A normal binary only constructs it on macOS. The eval build tag can opt
// into a deterministic injected backend on other platforms without adding a
// mock selection path to the shipping config schema.
func newComputerManager(cfg *config.Config, home string) *computer.Manager {
	if !computer.Supported() && !computerEvalEnabled() {
		return nil
	}
	var cc *config.ComputerConfig
	if cfg != nil {
		cc = cfg.Computer
	}
	m := computer.NewManager(computer.FromConfig(cc), home)
	if err := installEvalComputerBackend(m, cfg); err != nil {
		// An eval binary must never fall through from a missing/corrupt scripted
		// screen to the real macOS helper. Disable the manager and make the eval
		// fail visibly without touching the user's desktop.
		m.SetConfig(computer.Config{})
		config.Logger().Printf("[computer/eval] disabled after fixture setup failure: %v", err)
	}
	return m
}
