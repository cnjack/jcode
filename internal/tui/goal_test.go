package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cnjack/jcode/internal/tools"
)

// TestHandleGoalInput exercises the /goal slash command dispatch in the TUI
// without a running BubbleTea program (m.ready stays false, so the viewport
// refresh is skipped).
func TestHandleGoalInput(t *testing.T) {
	// Set with a plain objective.
	m := Model{goalStore: tools.NewGoalStore()}
	m.handleGoalInput("/goal Build the feature")
	if !m.goalStore.IsActive() {
		t.Fatal("/goal <obj> should create an active goal")
	}
	if g := m.goalStore.Get(); g.Objective != "Build the feature" {
		t.Fatalf("objective = %q", g.Objective)
	}
	if !m.thinking {
		t.Fatal("/goal <obj> should start the agent (thinking=true)")
	}

	m2 := Model{goalStore: tools.NewGoalStore()}
	m2.handleGoalInput("/goal Migrate DB")
	g := m2.goalStore.Get()
	if g == nil || g.Objective != "Migrate DB" {
		t.Fatalf("goal = %+v", g)
	}

	// Clear.
	m2.handleGoalInput("/goal clear")
	if m2.goalStore.Has() {
		t.Fatal("/goal clear should remove the goal")
	}

	// Status on an empty store must not panic and must not set a goal.
	m3 := Model{goalStore: tools.NewGoalStore()}
	m3.handleGoalInput("/goal")
	if m3.goalStore.Has() {
		t.Fatal("/goal status should not create a goal")
	}

	// While the agent is running, /goal sets the goal but must NOT submit a
	// kickoff prompt (the in-flight run's continuation guard picks it up);
	// a blocking channel send here would freeze the UI.
	m4 := Model{goalStore: tools.NewGoalStore(), thinking: true, agentDone: false}
	_, cmd := m4.handleGoalInput("/goal Fix the tests")
	if !m4.goalStore.IsActive() {
		t.Fatal("/goal while running should still set the goal")
	}
	if msg := drainForPromptSubmit(cmd); msg != nil {
		t.Fatalf("no PromptSubmitMsg should be emitted while the agent runs, got %+v", msg)
	}
}

// drainForPromptSubmit executes a (possibly batched) command tree and returns
// the first PromptSubmitMsg it produces, or nil.
func drainForPromptSubmit(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case PromptSubmitMsg:
		return msg
	case tea.BatchMsg:
		for _, c := range msg {
			if found := drainForPromptSubmit(c); found != nil {
				return found
			}
		}
	}
	return nil
}

func TestGoalCommandInSlashMenu(t *testing.T) {
	m := Model{}
	found := false
	for _, c := range m.getAllCommands() {
		if c.cmd == "/goal" {
			found = true
		}
	}
	if !found {
		t.Fatal("/goal should appear in the slash command menu")
	}
}
