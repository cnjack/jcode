package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/session"
)

func newModeRestoreRecorder(t *testing.T) *session.Recorder {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	recorder, err := session.NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recorder.Close)
	return recorder
}

func appendMalformedApprovalMode(t *testing.T, sessionID string) {
	t.Helper()
	dir, err := config.SessionsDir()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, sessionID+".json"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"type\":\"mode_change\",\"mode\":\"approval\"\n"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoredSessionModeFailsClosedAfterMalformedRevoke(t *testing.T) {
	recorder := newModeRestoreRecorder(t)
	if err := recorder.RecordModeChangeStrict(mode.FullAccess.String()); err != nil {
		t.Fatal(err)
	}
	appendMalformedApprovalMode(t, recorder.UUID())

	if got := restoredSessionMode(recorder.UUID(), "test"); got != mode.Approval {
		t.Fatalf("restored mode=%v, want fail-closed Approval", got)
	}
}

func TestRestoredSessionModeNormalizesPlanAndOverridesGlobalFullAccess(t *testing.T) {
	recorder := newModeRestoreRecorder(t)
	if err := recorder.RecordModeChangeStrict(mode.Approval.String()); err != nil {
		t.Fatal(err)
	}
	global := resolveStartupMode(&config.Config{DefaultMode: mode.FullAccess.String()}, false)
	if global != mode.FullAccess {
		t.Fatalf("test global mode=%v", global)
	}
	global = restoredSessionMode(recorder.UUID(), "test")
	if global != mode.Approval {
		t.Fatalf("resumed Approval task inherited global Full access: %v", global)
	}

	if err := recorder.RecordModeChangeStrict(mode.Plan.String()); err != nil {
		t.Fatal(err)
	}
	if got := restoredSessionMode(recorder.UUID(), "test"); got != mode.Approval {
		t.Fatalf("saved Plan restored as %v, want Approval", got)
	}
}
