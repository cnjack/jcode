package computer

import (
	"strings"

	"github.com/cnjack/jcode/internal/config"
)

// FromConfig maps the persisted config into this package's Config, applying
// defaults.
//
// This is the *only* mapper between the two shapes, and it is deliberately here
// rather than in the command or web layer. Keeping one mapper and one default
// set prevents transport-specific behavior drift.
func FromConfig(c *config.ComputerConfig) Config {
	if c == nil {
		// A nil config is "not configured", which is off — not "on with
		// defaults". Computer use never turns itself on.
		return Config{MaxActionsPerBatch: defaultMaxBatch}
	}
	normalized := *c
	// HTTP/live callers do not necessarily pass through config.LoadConfig. Apply
	// the legacy-backend migration on a copy so fake/osa/unknown values fail closed
	// without mutating the caller's shared config behind its lock.
	normalized.MigrateLegacyBackend()
	c = &normalized
	cfg := Config{
		Enabled:            c.Enabled,
		Approval:           map[string]string{},
		MaxActionsPerBatch: c.MaxActionsPerBatch,
		ClipboardRead:      c.ClipboardRead,
		ClipboardWrite:     c.ClipboardWrite,
		SystemKeyCombos:    c.SystemKeyCombos,
	}
	if cfg.MaxActionsPerBatch <= 0 {
		cfg.MaxActionsPerBatch = defaultMaxBatch
	}
	for k, v := range c.Approval {
		cfg.Approval[k] = v
	}
	for _, p := range c.AppPermissions {
		cfg.AppPermissions = append(cfg.AppPermissions, AppPermission{
			BundleID: p.BundleID,
			Tier:     p.Tier,
			Launch:   p.Launch,
			Interact: p.Interact,
		})
	}
	return cfg
}

// Preapproved reports whether class ("launch"/"interact") on bundleID is
// pre-authorized, consulting the per-app override first and the class default
// second. It is the body behind ApprovalState.SetComputerPermFunc.
//
// An empty bundle id never pre-approves: if the app cannot be named, there is no
// basis for claiming the user approved it. (browserSitePreapproved makes the
// same call for an empty origin, for the same reason.)
func Preapproved(c *config.ComputerConfig, bundleID, class string) bool {
	if c == nil || strings.TrimSpace(bundleID) == "" {
		return false
	}
	normalized := *c
	normalized.MigrateLegacyBackend()
	c = &normalized
	if !c.Enabled {
		return false
	}
	for _, p := range c.AppPermissions {
		if p.BundleID != bundleID {
			continue
		}
		var v string
		switch class {
		case "launch":
			v = p.Launch
		case "interact":
			v = p.Interact
		}
		// A per-app row wins over the class default in both directions: an
		// explicit "ask" on one app is how a user carves an exception out of a
		// blanket always_allow, so an empty value (not set) is what falls
		// through, never a set-but-restrictive one.
		if v != "" {
			return v == "allow"
		}
		break
	}
	return c.Approval[class] == "always_allow"
}
