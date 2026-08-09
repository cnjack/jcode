package command

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cloudwego/eino/adk"

	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/runner"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tui"
)

func TestTUIModeJournalFailureDoesNotPublishPreparedState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorder, err := session.NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recorder.Close)
	oldAgent := &adk.ChatModelAgent{}
	candidate := &adk.ChatModelAgent{}
	state := &interactiveState{
		rec: recorder, ag: oldAgent, agentMode: tui.ModeNormal,
		systemPrompt:  "approval prompt",
		approvalState: runner.NewApprovalStateWithMode(t.TempDir(), mode.Approval),
	}
	prepared := preparedTUISessionMode{
		agentMode: tui.ModePlanning, systemPrompt: "plan prompt", agent: candidate,
	}
	originalHome := os.Getenv("HOME")
	badHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(badHome, ".jcode"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("HOME", badHome); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv("HOME", originalHome) })

	if err := state.persistAndPublishTUISessionMode(mode.Plan, prepared); err == nil {
		t.Fatal("mode transaction unexpectedly succeeded")
	}
	if state.ag != oldAgent || state.ag == candidate || state.agentMode != tui.ModeNormal ||
		state.systemPrompt != "approval prompt" {
		t.Fatalf("failed transaction published agent=%p agentMode=%v prompt=%q",
			state.ag, state.agentMode, state.systemPrompt)
	}
	if state.approvalState.GetSessionMode() != mode.Approval ||
		state.approvalState.GetMode() != handler.ModeManual {
		t.Fatalf("failed transaction published session=%v approval=%v",
			state.approvalState.GetSessionMode(), state.approvalState.GetMode())
	}
}

func TestTUIApproveAllTransactionWritesFullAccessWithoutSelfSend(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := session.NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recorder.Close)
	candidate := &adk.ChatModelAgent{}
	state := &interactiveState{
		rec: recorder, ag: &adk.ChatModelAgent{}, agentMode: tui.ModeNormal,
		systemPrompt:  "approval prompt",
		approvalState: runner.NewApprovalStateWithMode(t.TempDir(), mode.Approval),
		// Program.Send blocks before Run starts. Approve All is called from
		// Model.Update, which has the same self-send constraint. Keeping this
		// non-running Program here makes any synchronous UI publish deterministic.
		p: tea.NewProgram(&tui.Model{}),
	}
	prepared := preparedTUISessionMode{
		agentMode: tui.ModeNormal, systemPrompt: "full prompt", agent: candidate,
	}
	done := make(chan error, 1)
	go func() {
		done <- state.persistAndPublishTUISessionMode(mode.FullAccess, prepared)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Approve All transaction synchronously sent to its BubbleTea Program")
	}
	if got, err := session.LoadSessionModeStrict(recorder.UUID()); err != nil || got != mode.FullAccess.String() {
		t.Fatalf("durable mode=%q err=%v", got, err)
	}
	if state.ag != candidate || state.approvalState.GetSessionMode() != mode.FullAccess ||
		state.approvalState.GetMode() != handler.ModeAuto {
		t.Fatalf("published agent=%p session=%v approval=%v",
			state.ag, state.approvalState.GetSessionMode(), state.approvalState.GetMode())
	}
}
