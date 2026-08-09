package command

import (
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/session"
)

// restoredSessionMode reads authorization state through the strict journal
// reader, independently from tolerant conversation reconstruction. A damaged
// line may be a newer revoke, so ambiguity always restores Approval. Saved
// Plan also restores Approval because resumed sessions use the normal tool set
// and retain the submitted plan separately in session state.
func restoredSessionMode(sessionID, surface string) mode.SessionMode {
	saved, err := session.LoadSessionModeStrict(sessionID)
	if err != nil {
		config.Logger().Printf(
			"[%s] mode journal unavailable for %s; restoring approval: %v",
			surface, sessionID, err,
		)
		return mode.Approval
	}
	restored := mode.Parse(saved)
	if restored == mode.Plan {
		return mode.Approval
	}
	return restored
}
