package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestGoalStore_SetAndGet(t *testing.T) {
	s := NewGoalStore()
	if s.Has() {
		t.Fatal("new store should have no goal")
	}
	if s.StatusLine() != "" {
		t.Fatal("empty store should have empty status line")
	}

	g := s.Set("Build the thing")
	if g.Status != GoalActive {
		t.Fatalf("new goal should be active, got %q", g.Status)
	}
	if !s.IsActive() {
		t.Fatal("store should report active")
	}
	got := s.Get()
	if got.Objective != "Build the thing" {
		t.Fatalf("unexpected goal: %+v", got)
	}

	// Returned snapshot must be a copy — mutating it must not affect the store.
	got.Objective = "mutated"
	if s.Get().Objective != "Build the thing" {
		t.Fatal("Get must return a defensive copy")
	}
}

func TestGoalStore_StatusTransitions(t *testing.T) {
	s := NewGoalStore()
	s.Set("obj")

	if !s.Complete() {
		t.Fatal("Complete should succeed when goal set")
	}
	if s.IsActive() {
		t.Fatal("completed goal must not be active")
	}
	if s.Get().Status != GoalComplete {
		t.Fatal("status should be complete")
	}
	if s.ContinuationPrompt() != "" {
		t.Fatal("non-active goal must not produce a continuation prompt")
	}

	s2 := NewGoalStore()
	if s2.Complete() {
		t.Fatal("Complete should fail when no goal set")
	}

	s.Set("again")
	if !s.Block() {
		t.Fatal("Block should succeed")
	}
	if s.IsActive() || s.Get().Status != GoalBlocked {
		t.Fatal("goal should be blocked and inactive")
	}
}

func TestGoalStore_RecordTokensUsageOnly(t *testing.T) {
	s := NewGoalStore()
	s.Set("obj")

	s.RecordTokens(400)
	if got := s.Get().TokensUsed; got != 400 {
		t.Fatalf("tokens used = %d, want 400", got)
	}
	// Only positive deltas counted (context total can shrink after summarization).
	s.RecordTokens(300)
	if got := s.Get().TokensUsed; got != 400 {
		t.Fatalf("tokens used = %d, want still 400", got)
	}
	s.RecordTokens(1300)
	if got := s.Get().TokensUsed; got != 1400 {
		t.Fatalf("tokens used = %d, want 1400", got)
	}
	// Usage is informational only — the goal never leaves Active because of tokens.
	if !s.IsActive() {
		t.Fatal("goal must stay active regardless of token usage")
	}
}

func TestGoalStore_OnUpdateFires(t *testing.T) {
	s := NewGoalStore()
	var calls []string
	s.OnUpdate = func(g *Goal) {
		if g == nil {
			calls = append(calls, "cleared")
		} else {
			calls = append(calls, string(g.Status))
		}
	}
	s.Set("obj") // active
	s.Complete() // complete
	s.Clear()    // cleared
	want := []string{"active", "complete", "cleared"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("OnUpdate calls = %v, want %v", calls, want)
	}
}

func TestGoalStore_ContinuationPromptIncludesObjective(t *testing.T) {
	s := NewGoalStore()
	s.Set("Migrate the database")
	p := s.ContinuationPrompt()
	if !strings.Contains(p, "Migrate the database") {
		t.Fatal("continuation prompt should include the objective")
	}
	if !strings.Contains(p, "goal_update") {
		t.Fatal("continuation prompt should instruct calling goal_update")
	}
}

func TestGoalTools_SetGetUpdate(t *testing.T) {
	env := NewEnv("/tmp", "darwin/arm64")
	ctx := context.Background()

	setTool := env.NewGoalSetTool()
	out, err := setTool.InvokableRun(ctx, `{"objective":"Do the work"}`)
	if err != nil {
		t.Fatalf("goal_set failed: %v", err)
	}
	if !strings.Contains(out, "Do the work") {
		t.Fatalf("goal_set output missing objective: %s", out)
	}
	if !env.GoalStore.IsActive() {
		t.Fatal("goal_set should create an active goal")
	}

	// Empty objective is rejected.
	if _, err := setTool.InvokableRun(ctx, `{"objective":"   "}`); err == nil {
		t.Fatal("empty objective should be rejected")
	}

	getTool := env.NewGoalGetTool()
	gout, err := getTool.InvokableRun(ctx, ``)
	if err != nil {
		t.Fatalf("goal_get failed: %v", err)
	}
	var g Goal
	if err := json.Unmarshal([]byte(gout), &g); err != nil {
		t.Fatalf("goal_get should return JSON: %v (%s)", err, gout)
	}
	if g.Objective != "Do the work" {
		t.Fatalf("goal_get objective = %q", g.Objective)
	}

	updTool := env.NewGoalUpdateTool()
	if _, err := updTool.InvokableRun(ctx, `{"status":"complete"}`); err != nil {
		t.Fatalf("goal_update complete failed: %v", err)
	}
	if env.GoalStore.IsActive() {
		t.Fatal("goal should be inactive after complete")
	}
	// Invalid status rejected.
	env.GoalStore.Set("x")
	if _, err := updTool.InvokableRun(ctx, `{"status":"bogus"}`); err == nil {
		t.Fatal("invalid status should be rejected")
	}
}

func TestGoalGet_NoGoal(t *testing.T) {
	env := NewEnv("/tmp", "darwin/arm64")
	out, err := env.NewGoalGetTool().InvokableRun(context.Background(), ``)
	if err != nil {
		t.Fatal(err)
	}
	if out != "No goal set." {
		t.Fatalf("expected 'No goal set.', got %q", out)
	}
}

func TestParseGoalCommand(t *testing.T) {
	cases := []struct {
		in        string
		kind      string
		objective string
	}{
		{"", "status", ""},
		{"status", "status", ""},
		{"  status  ", "status", ""},
		{"clear", "clear", ""},
		{"Refactor the parser", "set", "Refactor the parser"},
		{"  keep all tests green  ", "set", "keep all tests green"},
	}
	for _, c := range cases {
		got := ParseGoalCommand(c.in)
		if got.Kind != c.kind {
			t.Errorf("ParseGoalCommand(%q).Kind = %q, want %q", c.in, got.Kind, c.kind)
		}
		if got.Objective != c.objective {
			t.Errorf("ParseGoalCommand(%q).Objective = %q, want %q", c.in, got.Objective, c.objective)
		}
	}
}

func TestValidateGoalObjective(t *testing.T) {
	if _, err := ValidateGoalObjective("   "); err == nil {
		t.Fatal("blank objective should be rejected")
	}
	if _, err := ValidateGoalObjective(strings.Repeat("x", goalMaxObjectiveLen+1)); err == nil {
		t.Fatal("over-long objective should be rejected")
	}
	got, err := ValidateGoalObjective("  do the thing  ")
	if err != nil || got != "do the thing" {
		t.Fatalf("ValidateGoalObjective = %q, %v", got, err)
	}
}
