package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/session"
)

// GoalStatus is the lifecycle state of a session goal.
type GoalStatus string

const (
	// GoalActive means the agent should keep working toward the objective,
	// auto-continuing across turns until it can prove completion.
	GoalActive GoalStatus = "active"
	// GoalComplete is a terminal status set once the objective is verifiably done.
	GoalComplete GoalStatus = "complete"
	// GoalBlocked is a terminal status for an obstacle the agent cannot clear.
	GoalBlocked GoalStatus = "blocked"
)

// Goal is a persistent, cross-turn objective attached to a session. It mirrors
// codex's ThreadGoal: the agent keeps working toward Objective, auto-continuing
// when it would otherwise stop, until it can prove the goal Complete or Blocked.
type Goal struct {
	Objective  string     `json:"objective"`
	Status     GoalStatus `json:"status"`
	TokensUsed int64      `json:"tokens_used"` // informational only; goals are bounded by the turn cap, not tokens
	CreatedAt  int64      `json:"created_at"`
	UpdatedAt  int64      `json:"updated_at"`
}

// clone returns a copy so callers never mutate store-owned state.
func (g *Goal) clone() *Goal {
	if g == nil {
		return nil
	}
	cp := *g
	return &cp
}

// GoalStore is a concurrency-safe holder for the single active session goal.
// It mirrors TodoStore: an OnUpdate callback fires (with a snapshot copy) after
// every mutation so the session recorder can persist goal_update entries.
type GoalStore struct {
	mu        sync.RWMutex
	goal      *Goal
	tokenSeen int64 // last observed cumulative token total, for delta accounting
	OnUpdate  func(g *Goal)
	nowFn     func() int64 // injectable clock for tests
}

// NewGoalStore creates an empty GoalStore.
func NewGoalStore() *GoalStore {
	return &GoalStore{nowFn: func() int64 { return time.Now().Unix() }}
}

func (s *GoalStore) now() int64 {
	if s.nowFn != nil {
		return s.nowFn()
	}
	return time.Now().Unix()
}

// fireUpdate invokes OnUpdate with a snapshot copy. Must be called without the lock held.
func (s *GoalStore) fireUpdate(snapshot *Goal) {
	if s.OnUpdate != nil {
		s.OnUpdate(snapshot)
	}
}

// Set creates or replaces the active goal with a fresh objective. Token
// accounting resets. Returns a snapshot copy.
func (s *GoalStore) Set(objective string) *Goal {
	now := s.now()
	s.mu.Lock()
	g := &Goal{
		Objective:  objective,
		Status:     GoalActive,
		TokensUsed: 0,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	s.goal = g
	s.tokenSeen = 0
	snap := g.clone()
	s.mu.Unlock()
	s.fireUpdate(snap)
	return snap
}

// Restore loads a goal verbatim (used when resuming a session). A nil goal
// resets the store. No OnUpdate fires, so nothing is re-recorded.
func (s *GoalStore) Restore(g *Goal) {
	s.mu.Lock()
	s.goal = g.clone()
	s.tokenSeen = 0
	s.mu.Unlock()
}

// RestoreFromSnapshot loads a goal snapshot produced by
// session.ReconstructState. A nil snapshot resets the store. Shared by every
// resume path (TUI /resume, CLI --resume, ACP load/resume, web resume).
func (s *GoalStore) RestoreFromSnapshot(snap *session.GoalSnapshot) {
	if snap == nil {
		s.Restore(nil)
		return
	}
	s.Restore(&Goal{
		Objective:  snap.Objective,
		Status:     GoalStatus(snap.Status),
		TokensUsed: snap.TokensUsed,
	})
}

// GoalRecorderHook returns an OnUpdate callback that persists goal changes via
// rec.RecordGoalUpdate, including the nil-goal "cleared" marker. Shared by all
// frontends so the persistence encoding lives in one place.
func GoalRecorderHook(rec *session.Recorder) func(*Goal) {
	return func(g *Goal) {
		if rec == nil {
			return
		}
		if g == nil {
			rec.RecordGoalUpdate("", "", 0)
			return
		}
		rec.RecordGoalUpdate(g.Objective, string(g.Status), g.TokensUsed)
	}
}

// Get returns a snapshot copy of the current goal, or nil if none.
func (s *GoalStore) Get() *Goal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.goal.clone()
}

// Has reports whether any goal is set (in any status).
func (s *GoalStore) Has() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.goal != nil
}

// IsActive reports whether a goal exists and is in the Active status.
func (s *GoalStore) IsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.goal != nil && s.goal.Status == GoalActive
}

// setStatus transitions the goal to a terminal status. Returns the snapshot and
// whether a goal existed to update.
func (s *GoalStore) setStatus(status GoalStatus) (*Goal, bool) {
	s.mu.Lock()
	if s.goal == nil {
		s.mu.Unlock()
		return nil, false
	}
	s.goal.Status = status
	s.goal.UpdatedAt = s.now()
	snap := s.goal.clone()
	s.mu.Unlock()
	s.fireUpdate(snap)
	return snap, true
}

// Complete marks the goal complete. Returns false if no goal is set.
func (s *GoalStore) Complete() bool {
	_, ok := s.setStatus(GoalComplete)
	return ok
}

// Block marks the goal blocked. Returns false if no goal is set.
func (s *GoalStore) Block() bool {
	_, ok := s.setStatus(GoalBlocked)
	return ok
}

// Clear removes the goal entirely.
func (s *GoalStore) Clear() {
	s.mu.Lock()
	had := s.goal != nil
	s.goal = nil
	s.tokenSeen = 0
	s.mu.Unlock()
	if had {
		s.fireUpdate(nil)
	}
}

// RecordTokens feeds an observed cumulative token total into the goal's
// informational usage counter. Only positive deltas are added (the context
// total can shrink after summarization). Usage is display-only — goals are
// bounded by the runner's turn cap, never by tokens.
func (s *GoalStore) RecordTokens(cumulativeTotal int64) {
	s.mu.Lock()
	if s.goal != nil {
		if delta := cumulativeTotal - s.tokenSeen; delta > 0 {
			s.goal.TokensUsed += delta
		}
	}
	s.tokenSeen = cumulativeTotal
	s.mu.Unlock()
}

// StatusLine returns a compact one-line human summary, e.g.
// "🎯 active — Build the thing (1.2k tokens)". Empty if no goal.
func (s *GoalStore) StatusLine() string {
	g := s.Get()
	if g == nil {
		return ""
	}
	obj := g.Objective
	if len(obj) > 60 {
		obj = strings.TrimSpace(string([]rune(obj)[:60])) + "…"
	}
	usage := ""
	if g.TokensUsed > 0 {
		usage = fmt.Sprintf(" (%s tokens)", humanTokens(g.TokensUsed))
	}
	return fmt.Sprintf("🎯 %s — %s%s", g.Status, obj, usage)
}

// ContinuationPrompt is the steering message injected when the agent would
// otherwise stop but the goal is still Active. It mirrors codex's
// continuation.md: keep working, but only mark complete with real evidence.
func (s *GoalStore) ContinuationPrompt() string {
	g := s.Get()
	if g == nil || g.Status != GoalActive {
		return ""
	}
	var b strings.Builder
	b.WriteString("[Goal continuation]\n")
	b.WriteString("You have an active session goal and have not yet proven it complete.\n\n")
	b.WriteString("Objective: ")
	b.WriteString(g.Objective)
	b.WriteString("\n")
	b.WriteString("\nContinue working toward the objective. When — and only when — it is")
	b.WriteString(" verifiably done (checked against the actual files/commands/tests, not your")
	b.WriteString(" intent), call goal_update with status=\"complete\". If you are genuinely and")
	b.WriteString(" repeatedly blocked by an obstacle you cannot clear, call goal_update with")
	b.WriteString(" status=\"blocked\" and explain why. Do not mark complete just to stop.")
	return b.String()
}

// humanTokens formats a token count compactly (e.g. 1234 -> "1.2k").
func humanTokens(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// ---------------------------------------------------------------------------
// /goal slash command — shared grammar and prompts for all frontends
// ---------------------------------------------------------------------------

// GoalCommand is the parsed intent of a "/goal ..." slash command.
type GoalCommand struct {
	Kind      string // "status", "clear", or "set"
	Objective string // non-empty when Kind == "set"
}

// ParseGoalCommand parses the argument portion of a "/goal ..." command. The
// grammar is shared by the TUI, ACP, and web frontends: no argument or
// "status" reports the goal, "clear" removes it, anything else sets it as the
// objective.
func ParseGoalCommand(rest string) GoalCommand {
	rest = strings.TrimSpace(rest)
	switch rest {
	case "", "status":
		return GoalCommand{Kind: "status"}
	case "clear":
		return GoalCommand{Kind: "clear"}
	default:
		return GoalCommand{Kind: "set", Objective: rest}
	}
}

// GoalKickoffPrompt is the prompt sent to the agent immediately after a goal
// is set, so work starts without a separate user message.
func GoalKickoffPrompt(objective string) string {
	return "A persistent session goal has been set. Begin working toward it now, and keep going until it is verifiably complete:\n\n" + objective
}

// ValidateGoalObjective normalizes and validates a goal objective. It is the
// single validation rule for every entry point (goal_set tool, HTTP API,
// slash commands).
func ValidateGoalObjective(objective string) (string, error) {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return "", fmt.Errorf("objective must not be empty")
	}
	if len([]rune(objective)) > goalMaxObjectiveLen {
		return "", fmt.Errorf("objective exceeds %d characters", goalMaxObjectiveLen)
	}
	return objective, nil
}

// ---------------------------------------------------------------------------
// Tools: goal_set, goal_get, goal_update
// ---------------------------------------------------------------------------

const goalMaxObjectiveLen = 4000

type goalSetInput struct {
	Objective string `json:"objective"`
}

// NewGoalSetTool creates the goal_set tool: set or replace the session goal.
func (e *Env) NewGoalSetTool() tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "goal_set",
		Desc: `Set (or replace) the persistent objective for this session. A goal keeps you working across turns: when you would otherwise stop, you are automatically reminded to continue until the objective is verifiably complete.

Use this when the user gives you a substantial, multi-step objective they want pursued to completion. Provide a clear, self-contained objective describing the desired end state.

Setting a new goal replaces any existing one and resets accounting.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"objective": {
				Type:     schema.String,
				Desc:     "A clear, self-contained description of the desired end state (max 4000 chars).",
				Required: true,
			},
		}),
	}
	return &goalSetTool{env: e, info: info}
}

type goalSetTool struct {
	env  *Env
	info *schema.ToolInfo
}

func (t *goalSetTool) Info(_ context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *goalSetTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var in goalSetInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("failed to parse goal_set input: %w", err)
	}
	objective, err := ValidateGoalObjective(in.Objective)
	if err != nil {
		return "", err
	}
	if t.env.GoalStore == nil {
		return "", fmt.Errorf("goals are not enabled in this session")
	}
	g := t.env.GoalStore.Set(objective)
	out, _ := json.Marshal(g)
	return fmt.Sprintf("Goal set. Keep working until it is verifiably complete, then call goal_update.\n%s", string(out)), nil
}

// NewGoalGetTool creates the goal_get tool: read the current goal.
func (e *Env) NewGoalGetTool() tool.InvokableTool {
	info := &schema.ToolInfo{
		Name:        "goal_get",
		Desc:        "Read the current session goal: its objective, status, and token usage. Takes no parameters. Returns \"No goal set.\" when none exists.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}
	return &goalGetTool{env: e, info: info}
}

type goalGetTool struct {
	env  *Env
	info *schema.ToolInfo
}

func (t *goalGetTool) Info(_ context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *goalGetTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	if t.env.GoalStore == nil {
		return "No goal set.", nil
	}
	g := t.env.GoalStore.Get()
	if g == nil {
		return "No goal set.", nil
	}
	out, _ := json.Marshal(g)
	return string(out), nil
}

type goalUpdateInput struct {
	Status string `json:"status"`
}

// NewGoalUpdateTool creates the goal_update tool: mark the goal complete or blocked.
func (e *Env) NewGoalUpdateTool() tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "goal_update",
		Desc: `Update the status of the current session goal. Use status="complete" only when the objective is verifiably done — checked against the real state of files, command output, or tests, not your intent or memory. Use status="blocked" only when you are genuinely and repeatedly blocked by an obstacle you cannot clear. Marking the goal complete or blocked stops the automatic continuation.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"status": {
				Type:     schema.String,
				Desc:     `New status: "complete" (objective verifiably achieved) or "blocked" (cannot proceed).`,
				Required: true,
				Enum:     []string{"complete", "blocked"},
			},
		}),
	}
	return &goalUpdateTool{env: e, info: info}
}

type goalUpdateTool struct {
	env  *Env
	info *schema.ToolInfo
}

func (t *goalUpdateTool) Info(_ context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *goalUpdateTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var in goalUpdateInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("failed to parse goal_update input: %w", err)
	}
	if t.env.GoalStore == nil || !t.env.GoalStore.Has() {
		return "", fmt.Errorf("no goal is set; nothing to update")
	}
	switch strings.ToLower(strings.TrimSpace(in.Status)) {
	case "complete":
		t.env.GoalStore.Complete()
		return "Goal marked complete. Automatic continuation stopped.", nil
	case "blocked":
		t.env.GoalStore.Block()
		return "Goal marked blocked. Automatic continuation stopped.", nil
	default:
		return "", fmt.Errorf("invalid status %q, must be \"complete\" or \"blocked\"", in.Status)
	}
}
