package command

import (
	"context"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/runner"
	"github.com/cnjack/jcode/internal/tui"
)

func TestHandlePlanCompletionSkipsInterruptedRun(t *testing.T) {
	state := &interactiveState{agentMode: tui.ModePlanning}
	returned := make(chan struct{})
	go func() {
		state.handlePlanCompletion(runner.RunResult{
			Response: "partial plan",
			Err:      context.Canceled,
		})
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("interrupted plan entered the approval flow")
	}
}
