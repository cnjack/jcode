package session

import "testing"

func TestReconstructState_Goal(t *testing.T) {
	entries := []Entry{
		{Type: EntrySessionStart},
		{Type: EntryUser, Content: "hi"},
		{Type: EntryGoalUpdate, GoalObjective: "Ship it", GoalStatus: "active", GoalTokensUsed: 100},
		{Type: EntryGoalUpdate, GoalObjective: "Ship it", GoalStatus: "active", GoalTokensUsed: 900},
	}
	st := ReconstructState(entries)
	if st.Goal == nil {
		t.Fatal("expected goal to be reconstructed")
	}
	if st.Goal.Objective != "Ship it" || st.Goal.Status != "active" {
		t.Fatalf("unexpected goal: %+v", st.Goal)
	}
	// Last snapshot wins.
	if st.Goal.TokensUsed != 900 {
		t.Fatalf("tokens used = %d, want 900 (last snapshot)", st.Goal.TokensUsed)
	}
}

func TestReconstructState_GoalCleared(t *testing.T) {
	entries := []Entry{
		{Type: EntryGoalUpdate, GoalObjective: "x", GoalStatus: "active"},
		{Type: EntryGoalUpdate, GoalStatus: "cleared"},
	}
	st := ReconstructState(entries)
	if st.Goal != nil {
		t.Fatalf("goal should be nil after a cleared marker, got %+v", st.Goal)
	}
}
