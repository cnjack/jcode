package computer

import (
	"testing"

	"github.com/cnjack/jcode/internal/config"
)

func TestFromConfigIgnoresSafeLegacyBackend(t *testing.T) {
	for _, backend := range []string{"", "auto", " helper "} {
		c := &config.ComputerConfig{
			Enabled: true, Backend: backend, //nolint:staticcheck // compatibility migration input
			Approval:        map[string]string{"launch": "always_allow"},
			AppPermissions:  []config.ComputerAppPermission{{BundleID: notesID, Tier: "read"}},
			ClipboardRead:   true,
			ClipboardWrite:  true,
			SystemKeyCombos: true,
		}
		got := FromConfig(c)
		if !got.Enabled || got.Backend != "" || len(got.Approval) != 1 || len(got.AppPermissions) != 1 ||
			!got.ClipboardRead || !got.ClipboardWrite || !got.SystemKeyCombos {
			t.Errorf("FromConfig backend=%q changed safe policy: %+v", backend, got)
		}
	}
}

func TestFromConfigFailsClosedForUnsafeLegacyBackend(t *testing.T) {
	for _, backend := range []string{"fake", "osa", "unknown"} {
		c := &config.ComputerConfig{
			Enabled: true, Backend: backend, //nolint:staticcheck // compatibility migration input
			Approval:        map[string]string{"launch": "always_allow"},
			AppPermissions:  []config.ComputerAppPermission{{BundleID: notesID, Tier: "read", Launch: "allow"}},
			ClipboardRead:   true,
			ClipboardWrite:  true,
			SystemKeyCombos: true,
		}
		got := FromConfig(c)
		if got.Enabled || len(got.Approval) != 0 || len(got.AppPermissions) != 0 ||
			got.ClipboardRead || got.ClipboardWrite || got.SystemKeyCombos {
			t.Errorf("FromConfig backend=%q did not fail closed: %+v", backend, got)
		}
		if Preapproved(c, notesID, "launch") {
			t.Errorf("Preapproved accepted unsafe legacy backend %q", backend)
		}
		// Mapping must not mutate the shared config; the caller publishes its own
		// migrated copy under its config lock.
		if !c.Enabled || c.Backend != backend { //nolint:staticcheck // intentional legacy migration assertion
			t.Errorf("FromConfig mutated caller config: %+v", *c)
		}
	}
}
