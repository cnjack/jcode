package agent

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/prompts"
	"github.com/cnjack/jcode/internal/tools"
	utils "github.com/cnjack/jcode/internal/util"
)

// maxExternalChangesPerTurn bounds how many externally-changed files a single
// reminder reports, so one round can never flood the context.
const maxExternalChangesPerTurn = 5

// defaultEnvRefreshEvery is the env-refresh cadence (in model iterations)
// used when ReminderConfig.EnvRefreshEvery is zero.
const defaultEnvRefreshEvery = 5

// ReminderConfig holds the static configuration for the reminder middleware.
type ReminderConfig struct {
	TodoStore    *tools.TodoStore
	GoalStore    *tools.GoalStore
	PlanStore    *tools.PlanStore
	EnvLabel     string
	IsRemote     bool
	ContextLimit int
	TaskManager  *tools.SubagentTaskManager

	// FileTracker, when non-nil, is swept before each model call for files
	// modified outside the session since the agent last read them.
	FileTracker *tools.FileTracker

	// Env, when non-nil, is the live tool environment. switch_env mutates it
	// in place (local ↔ SSH/Docker) without rebuilding the agent on every
	// surface, so remote-ness is re-read from here each round: while remote,
	// the env-drift / AGENTS.md / external-file sweeps are paused — they all
	// inspect the LOCAL filesystem and would report wrong-host state.
	Env *tools.Env

	// Pwd enables the periodic environment refresh and the AGENTS.md reload
	// check. Empty disables both (remote sessions, subagents, tests).
	Pwd string
	// Platform is the platform string used when serializing env snapshots.
	Platform string
	// EnvSnapshot is the SerializeEnvInfo baseline captured at startup. When
	// empty, the first refresh cycle only establishes a baseline (no diff).
	EnvSnapshot string
	// EnvRefreshEvery is the env-refresh cadence in model iterations;
	// 0 means defaultEnvRefreshEvery.
	EnvRefreshEvery int
	// EnvCollector overrides the environment collector (for tests); nil uses
	// utils.CollectEnvInfoLight.
	EnvCollector func(pwd string) *utils.EnvInfo
}

// reminderMiddleware implements ChatModelAgentMiddleware to inject conditional
// system reminders before each model call.
type reminderMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	cfg               ReminderConfig
	tokenUsage        *internalmodel.TokenUsage
	iteration         int
	consecutiveErrors int

	// lastEnvSnapshot is the serialized env state the model last saw (system
	// prompt baseline, advanced after each reported drift so the same change
	// is only reported once).
	lastEnvSnapshot string

	// AGENTS.md reload state: one stat per round on the root file (mtime
	// gate) with an md5 dedup on the merged content. Known limitation: only
	// the root file is stat'ed, so a change in an @include child alone is
	// not caught until the root's mtime moves.
	agentsMdInit  bool
	agentsMdPath  string
	agentsMdMtime time.Time
	agentsMdHash  string
}

// NewReminderMiddleware creates a ChatModelAgentMiddleware that injects
// conditional reminders (todo check, token warning, error streak, external
// file changes, env drift, AGENTS.md reload) into the message stream before
// each model invocation.
// tokenUsage is the per-agent tracker to read from; may be nil.
func NewReminderMiddleware(cfg ReminderConfig, tokenUsage *internalmodel.TokenUsage) adk.ChatModelAgentMiddleware {
	return &reminderMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		cfg:                          cfg,
		tokenUsage:                   tokenUsage,
		lastEnvSnapshot:              cfg.EnvSnapshot,
	}
}

func (m *reminderMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	mc *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	m.iteration++
	m.updateErrorStreak(state)

	// Occupancy must mirror the LAST call's total (the live context size), not
	// the cumulative prompt ledger: the whole window is re-sent every tool
	// loop, so the ledger grows without bound and ResetContext (compaction)
	// deliberately never clears it. Same source as compaction.go's trigger.
	var contextTokens int64
	if m.tokenUsage != nil {
		contextTokens = m.tokenUsage.GetLastTotal()
	}

	var incompleteTodoN int
	var hasIncomplete bool
	if m.cfg.TodoStore != nil {
		items := m.cfg.TodoStore.Items()
		for _, item := range items {
			if item.Status != tools.TodoCompleted && item.Status != tools.TodoCancelled {
				incompleteTodoN++
			}
		}
		hasIncomplete = incompleteTodoN > 0
	}

	// Remote-ness follows the live env when available: switch_env flips the
	// shared *tools.Env in place without rebuilding this middleware.
	remote := m.cfg.IsRemote
	if m.cfg.Env != nil {
		remote = m.cfg.Env.IsRemote()
	}

	rc := &prompts.ReminderContext{
		Iteration:         m.iteration,
		TokensUsed:        contextTokens,
		ContextLimit:      m.cfg.ContextLimit,
		HasIncompleteTodo: hasIncomplete,
		IncompleteTodoN:   incompleteTodoN,
		ConsecutiveErrors: m.consecutiveErrors,
		EnvLabel:          m.cfg.EnvLabel,
		IsRemote:          remote,
	}

	// Inject approved plan context for execution mode.
	if m.cfg.PlanStore != nil && m.cfg.PlanStore.HasApprovedPlan() {
		rc.PlanContent = m.cfg.PlanStore.Content()
	}

	// Inject active goal context so the agent never loses the objective.
	if m.cfg.GoalStore != nil {
		if g := m.cfg.GoalStore.Get(); g != nil && g.Status == tools.GoalActive {
			rc.GoalActive = true
			rc.GoalObjective = g.Objective
		}
	}

	// The local-filesystem sweeps pause while the live env is remote: tracked
	// paths, git state, and AGENTS.md all belong to the local host and would
	// otherwise be reported as drift/changes of the wrong machine. They resume
	// (same baselines) when switch_env returns to local.
	if !remote {
		// Report files modified outside the session since the agent read them.
		m.scanExternalChanges(rc)
		// Periodic env refresh (git state, project type, date) and AGENTS.md
		// reload check; both are no-ops when cfg.Pwd is empty.
		m.refreshEnvDrift(rc)
		m.refreshAgentsMd(rc)
	}

	msgs := prompts.CollectReminders(rc)
	text := prompts.FormatReminders(msgs)
	if text != "" {
		state.Messages = append(state.Messages, &schema.Message{
			Role:    schema.System,
			Content: text,
		})
	}

	// Inject subagent task notifications if available.
	if m.cfg.TaskManager != nil {
		notifications := m.cfg.TaskManager.DrainNotifications()
		for _, n := range notifications {
			reminder := fmt.Sprintf(
				"<subagent-notification>\n  <task-id>%s</task-id>\n  <name>%s</name>\n  <status>%s</status>\n  <summary>%s</summary>\n</subagent-notification>",
				n.TaskID, n.Name, n.Status, truncateStr(n.Summary, 500))
			state.Messages = append(state.Messages, &schema.Message{
				Role:    schema.System,
				Content: reminder,
			})
		}
	}

	return ctx, state, nil
}

// scanExternalChanges sweeps the FileTracker for external modifications and
// fills the reminder context. Each change is reported once (the tracker
// advances its state in place); nil FileTracker disables the sweep entirely.
// The tracker is read from the live env when available so a future env
// rebuild/replacement is picked up without re-wiring this middleware.
func (m *reminderMiddleware) scanExternalChanges(rc *prompts.ReminderContext) {
	tracker := m.cfg.FileTracker
	if m.cfg.Env != nil {
		tracker = m.cfg.Env.FileTracker
	}
	if tracker == nil {
		return
	}
	for _, ch := range tracker.ScanExternalChanges(maxExternalChangesPerTurn) {
		if ch.Gone {
			rc.ExternalGoneFiles = append(rc.ExternalGoneFiles, ch.Path)
		} else {
			rc.ExternalChangedFiles = append(rc.ExternalChangedFiles, ch.Path)
		}
	}
}

// envRefreshEvery returns the configured env-refresh cadence, defaulted.
func (m *reminderMiddleware) envRefreshEvery() int {
	if m.cfg.EnvRefreshEvery > 0 {
		return m.cfg.EnvRefreshEvery
	}
	return defaultEnvRefreshEvery
}

// refreshEnvDrift re-collects the light environment info every N iterations
// and injects a diff against the last snapshot the model saw. The snapshot is
// advanced after a reported diff, so the same drift is only reported once.
// An empty starting snapshot means the first cycle only establishes a
// baseline without injecting.
func (m *reminderMiddleware) refreshEnvDrift(rc *prompts.ReminderContext) {
	if m.cfg.Pwd == "" || m.iteration%m.envRefreshEvery() != 0 {
		return
	}
	collect := m.cfg.EnvCollector
	if collect == nil {
		collect = utils.CollectEnvInfoLight
	}
	info := collect(m.cfg.Pwd)
	snap := prompts.SerializeEnvInfo(m.cfg.Platform, m.cfg.Pwd, m.cfg.EnvLabel, info)
	if m.lastEnvSnapshot == "" {
		m.lastEnvSnapshot = snap
		return
	}
	if diff := prompts.BuildEnvDiff(m.lastEnvSnapshot, m.cfg.Platform, m.cfg.Pwd, m.cfg.EnvLabel, info); diff != "" {
		rc.EnvDiff = diff
		m.lastEnvSnapshot = snap
	}
}

// refreshAgentsMd re-reads AGENTS.md when its mtime moved and injects the new
// content (hash-deduped) or a removal notice. The steady state costs one stat
// per round; when no AGENTS.md exists yet, a ReadDir re-scan runs only on the
// env-refresh cadence to catch a file created mid-session.
func (m *reminderMiddleware) refreshAgentsMd(rc *prompts.ReminderContext) {
	if m.cfg.Pwd == "" {
		return
	}
	if !m.agentsMdInit {
		// Lazy baseline on the first round: the current content is already
		// baked into the system prompt, so record state without injecting.
		m.agentsMdInit = true
		if path := prompts.HasAgentsMd(m.cfg.Pwd); path != "" {
			if info, err := os.Stat(path); err == nil {
				m.agentsMdPath = path
				m.agentsMdMtime = info.ModTime()
				m.agentsMdHash = md5Hex(prompts.LoadAgentsMdContent(m.cfg.Pwd))
			}
		}
		return
	}
	if m.agentsMdPath == "" {
		// No AGENTS.md at baseline (or it was removed): periodically re-scan
		// for one created mid-session. A newly-found file is NOT in the
		// system prompt, so it is injected immediately.
		if m.iteration%m.envRefreshEvery() != 0 {
			return
		}
		path := prompts.HasAgentsMd(m.cfg.Pwd)
		if path == "" {
			return
		}
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		content := prompts.LoadAgentsMdContent(m.cfg.Pwd)
		m.agentsMdPath = path
		m.agentsMdMtime = info.ModTime()
		m.agentsMdHash = md5Hex(content)
		rc.AgentsMdUpdate = content
		return
	}
	info, err := os.Stat(m.agentsMdPath)
	if err != nil {
		if os.IsNotExist(err) {
			rc.AgentsMdRemoved = true
			m.agentsMdPath = ""
			m.agentsMdMtime = time.Time{}
			m.agentsMdHash = ""
		}
		return
	}
	// Fast path: unchanged mtime, nothing to do.
	if info.ModTime().Equal(m.agentsMdMtime) {
		return
	}
	m.agentsMdMtime = info.ModTime()
	content := prompts.LoadAgentsMdContent(m.cfg.Pwd)
	h := md5Hex(content)
	if h == m.agentsMdHash {
		return // touch only — content identical
	}
	m.agentsMdHash = h
	rc.AgentsMdUpdate = content
}

// md5Hex returns the md5 hash of s as a hex string.
func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// updateErrorStreak scans the last tool result message in the state and
// increments or resets the consecutive error counter.
func (m *reminderMiddleware) updateErrorStreak(state *adk.ChatModelAgentState) {
	for i := len(state.Messages) - 1; i >= 0; i-- {
		msg := state.Messages[i]
		if msg.Role == schema.Tool {
			if strings.HasPrefix(msg.Content, "Tool execution failed:") ||
				strings.HasPrefix(msg.Content, "[tool error]") {
				m.consecutiveErrors++
			} else {
				m.consecutiveErrors = 0
			}
			return
		}
		// Stop scanning once we pass tool messages.
		if msg.Role == schema.Assistant || msg.Role == schema.User {
			m.consecutiveErrors = 0
			return
		}
	}
}

// truncateStr truncates s to maxLen runes, appending "..." if truncated.
func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
