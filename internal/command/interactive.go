package command

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/channel"
	"github.com/cnjack/jcode/internal/channel/ble"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/mode"
	internalmodel "github.com/cnjack/jcode/internal/model"
	weixin "github.com/cnjack/jcode/internal/pkg/weixin"
	"github.com/cnjack/jcode/internal/prompts"
	"github.com/cnjack/jcode/internal/runner"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/skills"
	"github.com/cnjack/jcode/internal/team"
	"github.com/cnjack/jcode/internal/telemetry"
	"github.com/cnjack/jcode/internal/tools"
	"github.com/cnjack/jcode/internal/tui"
	util "github.com/cnjack/jcode/internal/util"
)

// interactiveState holds all shared state for the interactive TUI event loop.
type interactiveState struct {
	ctx             context.Context
	p               *tea.Program
	cfg             *config.Config
	chatModel       einomodel.ToolCallingChatModel
	ag              *adk.ChatModelAgent
	history         []adk.Message
	env             *tools.Env
	bgManager       *tools.BackgroundManager
	approvalState   *runner.ApprovalState
	rec             *session.Recorder
	planStore       *tools.PlanStore
	summCapture     *agent.SummarizationCapture
	systemPrompt    string
	toolList        []tool.BaseTool
	agentMode       tui.AgentMode
	envInfo         *util.EnvInfo
	pwd             string
	platform        string
	registry        *internalmodel.ModelRegistry
	skillLoader     *skills.Loader
	langfuseTracer  *telemetry.LangfuseTracer
	h               handler.AgentEventHandler
	askUserDeps     *tools.AskUserDeps
	teamManager     *team.Manager
	mcpTools        []tool.BaseTool
	agentTokenUsage *internalmodel.TokenUsage

	sessionResumeWarning  string
	sessionBaselineCommit string

	// WeChat channel
	wechatClient *weixin.Client
	agentRunning atomic.Bool

	// Agent cancellation
	cancelFunc context.CancelFunc // used to cancel a running agent job
	runCtx     context.Context    // per-run context, non-nil while agent is running
}

func (s *interactiveState) buildAllTools() []tool.BaseTool {
	all := []tool.BaseTool{
		s.env.NewReadTool(), s.env.NewEditTool(), s.env.NewWriteTool(),
		s.env.NewExecuteTool(s.bgManager), s.env.NewGrepTool(),
		s.env.NewTodoWriteTool(), s.env.NewTodoReadTool(),
		s.env.NewGoalSetTool(), s.env.NewGoalGetTool(), s.env.NewGoalUpdateTool(),
		s.env.NewCheckBackgroundTool(s.bgManager),
		s.env.NewSubagentTool(&tools.SubagentDeps{
			ChatModel:  s.chatModel,
			Notifier:   s.subagentNotifier,
			ProgressFn: s.subagentProgress,
			TokenFn:    s.subagentTokenFn,
			Recorder:   s.rec,
			Tracer:     s.langfuseTracer,
		}),
		tools.NewAskUserTool(s.askUserDeps),
		skills.NewLoadSkillTool(s.skillLoader),
		tools.NewTeamCreateTool(s.teamManager),
		tools.NewTeamSpawnTool(s.teamManager),
		tools.NewTeamSendMessageTool(s.teamManager),
		tools.NewTeamListTool(s.teamManager),
		tools.NewTeamDeleteTool(s.teamManager),
	}
	// Only add switch_env tool if SSH aliases are configured
	if s.cfg != nil && len(s.cfg.SSHAliases) > 0 {
		all = append(all, s.env.NewSwitchEnvTool())
	}
	return append(all, s.mcpTools...)
}

func (s *interactiveState) buildPlanTools() []tool.BaseTool {
	return []tool.BaseTool{
		s.env.NewReadTool(),
		s.env.NewExecuteTool(nil),
		s.env.NewGrepTool(),
		s.env.NewTodoWriteTool(), s.env.NewTodoReadTool(),
		tools.NewAskUserTool(s.askUserDeps),
	}
}

func (s *interactiveState) subagentNotifier(name, agentType string, done bool, result string, err error) {
	if s.p == nil {
		return
	}
	if !done {
		s.p.Send(tui.SubagentStartMsg{Name: name, Type: agentType})
	} else {
		s.p.Send(tui.SubagentDoneMsg{Name: name, Result: result, Err: err})
	}
}

func (s *interactiveState) subagentProgress(agentName, event, toolName, detail string) {
	if s.p == nil {
		return
	}
	s.p.Send(tui.SubagentProgressMsg{
		AgentName: agentName,
		Event:     event,
		ToolName:  toolName,
		Detail:    detail,
	})
}

func (s *interactiveState) subagentTokenFn(totalTokens int64) {
	if s.p != nil {
		s.p.Send(tui.SubagentTokenUpdateMsg{TotalTokens: totalTokens})
	}
}

func (s *interactiveState) createAgent() (*adk.ChatModelAgent, error) {
	var middlewares []adk.AgentMiddleware
	if s.langfuseTracer != nil {
		middlewares = append(middlewares, s.langfuseTracer.AgentMiddleware())
	}

	var handlers []adk.ChatModelAgentMiddleware

	providerName, modelName := s.cfg.GetProviderModel()
	contextLimit := internalmodel.ResolveContextLimit(s.registry, s.cfg, providerName, modelName)
	// compactThreshold drives summarization + compaction; reductionThreshold (the
	// lighter, earlier tool-output clearing) sits just below it.
	compactThreshold := s.cfg.CompactionThreshold()
	reductionThreshold := compactThreshold - 0.15
	if reductionThreshold < 0.1 {
		reductionThreshold = compactThreshold * 0.8
	}

	summMw, err := summarization.New(s.ctx, &summarization.Config{
		Model: s.chatModel,
		Trigger: &summarization.TriggerCondition{
			ContextTokens: int(float64(contextLimit) * compactThreshold),
		},
		TranscriptFilePath: filepath.Join(config.ConfigDir(), "transcript.txt"),
		Finalize: func(ctx context.Context, originalMsgs []adk.Message, summary adk.Message) ([]adk.Message, error) {
			var systemMsgs []adk.Message
			var contextN int
			for _, msg := range originalMsgs {
				if msg.Role == schema.System {
					systemMsgs = append(systemMsgs, msg)
				} else {
					contextN++
				}
			}
			s.summCapture.Capture(summary.Content, contextN)
			config.Logger().Printf("[summarization] Finalize: compacted %d context messages", contextN)
			if s.agentTokenUsage != nil {
				s.agentTokenUsage.Reset()
			}
			return append(systemMsgs, summary), nil
		},
	})
	if err != nil {
		config.Logger().Printf("[agent] summarization middleware init error: %v", err)
	} else {
		handlers = append(handlers, summMw)
	}

	reductionBackend := &agent.LocalReductionBackend{RootDir: config.ConfigDir()}
	reductionMw, err := reduction.New(s.ctx, &reduction.Config{
		Backend:           reductionBackend,
		RootDir:           filepath.Join(config.ConfigDir(), "reduction"),
		MaxLengthForTrunc: 50000,
		MaxTokensForClear: int64(float64(contextLimit) * reductionThreshold),
		ReadFileToolName:  "read",
		TruncExcludeTools: []string{"ask_user", "load_skill"},
		ToolConfig: map[string]*reduction.ToolReductionConfig{
			"read": {SkipClear: true},
		},
	})
	if err != nil {
		config.Logger().Printf("[agent] reduction middleware init error: %v", err)
	} else {
		handlers = append(handlers, reductionMw)
	}

	reminderMw := agent.NewReminderMiddleware(agent.ReminderConfig{
		TodoStore:    s.env.TodoStore,
		GoalStore:    s.env.GoalStore,
		PlanStore:    s.planStore,
		EnvLabel:     "local",
		IsRemote:     s.env.IsRemote(),
		ContextLimit: contextLimit,
	}, s.agentTokenUsage)
	handlers = append(handlers, reminderMw)

	// Wire up budget middleware (per-agent tracker).
	if s.cfg.Budget != nil {
		providerName, modelName := s.cfg.GetProviderModel()
		inputPer1M, outputPer1M := s.registry.GetModelCost(providerName, modelName)
		pricing := internalmodel.ModelPricing{InputPer1M: inputPer1M, OutputPer1M: outputPer1M}
		budgetManager := agent.NewBudgetManager(s.cfg.Budget, pricing)
		budgetMw := agent.NewBudgetMiddleware(budgetManager, s.agentTokenUsage, func(status agent.BudgetStatus) {
			config.Logger().Printf("[budget] warning level=%d cost=%.4f", status.WarningLevel, status.EstimatedCost)
		})
		handlers = append([]adk.ChatModelAgentMiddleware{budgetMw}, handlers...)
	}

	// Wire up compaction middleware (per-agent tracker).
	compactionStrategy := agent.NewThresholdCompactionStrategy(compactThreshold, s.chatModel, 6)
	compactionMw := agent.NewCompactionMiddleware(compactionStrategy, contextLimit, s.agentTokenUsage, func(savedTokens int) {
		if s.agentTokenUsage != nil {
			s.agentTokenUsage.Reset()
		}
		if s.p != nil {
			s.p.Send(tui.CompactDoneMsg{OldTokens: 0, NewTokens: 0})
		}
	})
	handlers = append([]adk.ChatModelAgentMiddleware{compactionMw}, handlers...)

	return agent.NewAgent(s.ctx, s.chatModel, s.toolList, s.systemPrompt, s.approvalState.RequestApproval, middlewares, handlers)
}

// mcpStatusItems converts internal MCP statuses to TUI status items.
func mcpStatusItems(internalStatuses []tools.MCPStatus) []tui.MCPStatusItem {
	items := make([]tui.MCPStatusItem, 0, len(internalStatuses))
	for _, st := range internalStatuses {
		errMsg := ""
		if st.Error != nil {
			errMsg = st.Error.Error()
		}
		items = append(items, tui.MCPStatusItem{
			Name: st.Name, ToolCount: st.ToolCount, Running: st.Running,
			ErrMsg: errMsg, NeedsAuth: st.NeedsAuth,
		})
	}
	return items
}

// reloadMCP reloads MCP servers from the current config, rebuilds the agent
// tool set in place, and pushes a fresh status to the TUI. Used after an
// OAuth login or MCP config change so tools become usable without a restart.
func (s *interactiveState) reloadMCP() {
	latest, err := config.LoadConfig()
	if err != nil {
		config.Logger().Printf("[mcp] reload: config load failed: %v", err)
		return
	}
	s.cfg = latest
	mcpTools, statuses := tools.LoadMCPTools(s.ctx, latest.MCPServers)
	s.mcpTools = mcpTools
	if s.agentMode != tui.ModePlanning {
		s.toolList = s.buildAllTools()
		if newAg, err := s.createAgent(); err == nil {
			s.ag = newAg
		} else {
			config.Logger().Printf("[mcp] reload: agent rebuild failed: %v", err)
		}
	}
	if s.p != nil {
		s.p.Send(tui.MCPStatusMsg{Statuses: mcpStatusItems(statuses)})
	}
}

func (s *interactiveState) applyModeSwitch(newMode tui.AgentMode) {
	s.agentMode = newMode
	config.Logger().Printf("[plan] mode switch to %d (0=normal, 1=plan)", newMode)

	if s.rec != nil {
		// Record the unified session mode derived from both axes (tool/prompt +
		// approval), not just the tool axis, so resume round-trips the selector.
		s.rec.RecordModeChange(sessionModeFrom(newMode, s.approvalState.GetMode()).String())
	}

	if s.agentMode == tui.ModePlanning {
		s.systemPrompt = prompts.GetPlanSystemPrompt(s.platform, s.pwd, s.env.Exec.Label(), s.envInfo)
		s.toolList = s.buildPlanTools()
		config.Logger().Printf("[plan] built plan tools: %d tools", len(s.toolList))
	} else {
		s.systemPrompt = prompts.GetSystemPrompt(s.platform, s.pwd, s.env.Exec.Label(), s.envInfo, s.skillLoader.Descriptions())
		s.toolList = s.buildAllTools()
		config.Logger().Printf("[plan] built all tools: %d tools", len(s.toolList))
	}
	if newAg, err := s.createAgent(); err == nil {
		s.ag = newAg
		config.Logger().Printf("[plan] agent recreated successfully")
	} else {
		config.Logger().Printf("[plan] agent creation failed: %v", err)
	}
	if s.agentTokenUsage != nil {
		s.agentTokenUsage.Reset()
	}
	// Sync the TUI mode pill with the resulting unified mode (covers the
	// plan-completion revert to Normal, which the user did not trigger directly).
	if s.p != nil {
		s.p.Send(tui.ModeSelectedMsg{Mode: sessionModeFrom(newMode, s.approvalState.GetMode())})
	}
}

// applySessionMode applies a unified selector mode to BOTH axes: the approval
// axis (via ApprovalState) and the tool/prompt axis (rebuilding the agent). It
// is the single entry point for a mode change driven by the TUI selector.
// SetSessionMode runs first so applyModeSwitch records the correct unified mode.
func (s *interactiveState) applySessionMode(m mode.SessionMode) {
	s.approvalState.SetSessionMode(m)
	if m.IsPlan() {
		s.applyModeSwitch(tui.ModePlanning)
	} else {
		s.applyModeSwitch(tui.ModeNormal)
	}
}

func (s *interactiveState) drainModeSwitch(modeSelectCh <-chan mode.SessionMode) {
	for {
		select {
		case sm := <-modeSelectCh:
			s.applySessionMode(sm)
		default:
			return
		}
	}
}

func (s *interactiveState) handlePrompt(userPrompt string) {
	s.agentRunning.Store(true)
	defer s.agentRunning.Store(false)

	// Create a per-run cancellable context so cancelling one run
	// does not prevent future runs.
	runCtx, runCancel := context.WithCancel(s.ctx)
	s.cancelFunc = runCancel
	s.runCtx = runCtx
	defer func() {
		runCancel()
		s.runCtx = nil
	}()

	if s.sessionResumeWarning != "" {
		userPrompt = s.sessionResumeWarning + "\n\n" + userPrompt
		s.sessionResumeWarning = ""
	}
	if s.rec != nil {
		s.rec.RecordUser(userPrompt)
	}
	if s.agentTokenUsage == nil {
		s.agentTokenUsage = &internalmodel.TokenUsage{}
	}
	s.history = append(s.history, schema.UserMessage(userPrompt))
	s.history = agent.DrainBgNotifications(s.bgManager, s.history)
	resp := runner.Run(runCtx, s.ag, s.history, s.h, s.rec, s.env.TodoStore, s.env.GoalStore, s.langfuseTracer, s.agentTokenUsage)
	if resp != "" {
		s.history = append(s.history, &schema.Message{Role: schema.Assistant, Content: resp})
	}
	s.history = agent.SyncSummarization(s.summCapture, s.history, s.rec)
	s.handlePlanCompletion(resp)
}

func (s *interactiveState) handlePlanCompletion(resp string) {
	if s.agentMode != tui.ModePlanning || resp == "" {
		return
	}

	s.planStore.Submit("Plan", resp)
	config.Logger().Printf("[plan] plan submitted for review (%d chars)", len(resp))
	if s.rec != nil {
		s.rec.RecordPlanUpdate("submitted", "Plan", resp, "")
	}

	s.p.Send(tui.PlanApprovalMsg{PlanContent: resp, PlanPath: "Plan"})

	planRespCh := tui.GetPlanResponseChannel()
	planResp := <-planRespCh

	if !planResp.Approved {
		feedback := planResp.Feedback
		s.planStore.Reject(feedback)
		config.Logger().Printf("[plan] plan rejected: %s", feedback)
		if s.rec != nil {
			s.rec.RecordPlanUpdate("rejected", "", "", feedback)
		}

		revisePrompt := "Your plan was rejected."
		if feedback != "" {
			revisePrompt += " Feedback: " + feedback
		}
		revisePrompt += "\nPlease revise your plan based on this feedback."
		s.p.Send(tui.UserPromptMsg{Prompt: revisePrompt})
		if s.rec != nil {
			s.rec.RecordUser(revisePrompt)
		}
		s.history = append(s.history, schema.UserMessage(revisePrompt))
		newResp := runner.Run(s.runCtx, s.ag, s.history, s.h, s.rec, s.env.TodoStore, s.env.GoalStore, s.langfuseTracer, s.agentTokenUsage)
		if newResp != "" {
			s.history = append(s.history, &schema.Message{Role: schema.Assistant, Content: newResp})
		}
		s.history = agent.SyncSummarization(s.summCapture, s.history, s.rec)
		s.handlePlanCompletion(newResp)
		return
	}

	s.planStore.Approve()
	config.Logger().Printf("[plan] plan approved, transitioning to execution mode")
	if s.rec != nil {
		s.rec.RecordPlanUpdate("approved", s.planStore.Title(), s.planStore.Content(), "")
	}

	todos := tools.ExtractTodosFromPlan(s.planStore.Content())
	if len(todos) > 0 {
		s.env.TodoStore.Update(todos)
		s.p.Send(tui.TodoUpdateMsg{})
		config.Logger().Printf("[plan] populated %d todos from plan", len(todos))
	}

	s.applyModeSwitch(tui.ModeExecuting)

	execPrompt := "Your plan has been approved. Execute it step by step, tracking progress with the todo list. Mark each step complete as you finish it."
	s.p.Send(tui.UserPromptMsg{Prompt: execPrompt})
	if s.rec != nil {
		s.rec.RecordUser(execPrompt)
	}
	s.history = append(s.history, schema.UserMessage(execPrompt))
	execResp := runner.Run(s.ctx, s.ag, s.history, s.h, s.rec, s.env.TodoStore, s.env.GoalStore, s.langfuseTracer, s.agentTokenUsage)
	if execResp != "" {
		s.history = append(s.history, &schema.Message{Role: schema.Assistant, Content: execResp})
	}
	s.history = agent.SyncSummarization(s.summCapture, s.history, s.rec)

	if s.env.TodoStore.HasItems() && !s.env.TodoStore.HasIncomplete() {
		config.Logger().Printf("[plan] all todos complete, switching to normal mode")
		s.planStore.Clear()
		s.applyModeSwitch(tui.ModeNormal)
	}
}

func (s *interactiveState) attemptSSHResume(target string) string {
	if target == "local" || target == "" {
		return ""
	}
	var alias *config.SSHAlias
	for _, a := range s.cfg.SSHAliases {
		if a.Name == target {
			alias = &a
			break
		}
	}
	if alias == nil {
		return fmt.Sprintf("[System Note: The session was previously connected to SSH alias '%s', but it no longer exists in config. Environment dropped to 'local'.]", target)
	}

	authMethods := tools.BuildSSHAuthMethods()
	user := ""
	host := alias.Addr
	if idx := strings.Index(host, "@"); idx > 0 {
		user = host[:idx]
		host = host[idx+1:]
	}

	sshExec, err := tools.NewSSHExecutor(host, user, authMethods)
	if err != nil {
		return fmt.Sprintf("[System Note: The session attempted to reconnect to SSH alias '%s' (%s) but failed: %v. Environment dropped to 'local'.]", target, alias.Addr, err)
	}

	s.env.SetSSH(sshExec, s.env.Pwd())
	label := sshExec.Label()
	if s.env.OnEnvChange != nil {
		s.env.OnEnvChange(label, false, nil)
	}
	return ""
}

func (s *interactiveState) handleResume(uuid string) {
	entries, loadErr := session.LoadSession(uuid)
	if loadErr != nil {
		s.p.Send(tui.AgentDoneMsg{Err: fmt.Errorf("load session: %w", loadErr)})
		return
	}
	st := session.ReconstructState(entries)
	s.history = session.PruneOldToolOutputs(st.History, 2)
	// Restore the unified session mode. The approval axis is restored as-is, so a
	// session saved in Full access resumes auto-approving (accept-all-risk policy).
	// A saved Plan is normalized to Approval on resume: we keep full tools and restore
	// the saved plan into planStore below rather than stranding the user in the
	// read-only plan tool set with no execution trigger.
	restoredMode := mode.Parse(st.Mode)
	if restoredMode == mode.Plan {
		restoredMode = mode.Approval
	}
	s.approvalState.SetSessionMode(restoredMode)
	s.agentMode = tui.ModeNormal
	s.rec.SetUUID(uuid)
	s.p.Send(tui.SessionResumedMsg{UUID: uuid, Entries: tui.ConvertSessionEntries(entries)})
	// Sync the mode pill with the restored mode (SessionResumedMsg resets the
	// pill to Approval; this overrides it with what the session was actually saved in).
	s.p.Send(tui.ModeSelectedMsg{Mode: restoredMode})

	// Restore stored system prompt for KV-cache-friendly resume.
	if st.SystemPrompt != "" {
		s.systemPrompt = st.SystemPrompt
		envDiff := prompts.BuildEnvDiff(st.EnvInfo, s.platform, s.pwd, s.env.Exec.Label(), s.envInfo)
		if envDiff != "" {
			s.history = append(s.history, &schema.Message{
				Role:    schema.System,
				Content: envDiff,
			})
		}
	}

	if st.Plan != nil {
		switch st.Plan.Status {
		case "approved":
			s.planStore.Submit(st.Plan.Title, st.Plan.Content)
			s.planStore.Approve()
		case "submitted":
			s.planStore.Submit(st.Plan.Title, st.Plan.Content)
		case "rejected":
			s.planStore.SetDraft(st.Plan.Title, st.Plan.Content)
		}
	}

	if len(st.Todos) > 0 {
		todoItems := make([]tools.TodoItem, len(st.Todos))
		for i, t := range st.Todos {
			todoItems[i] = tools.TodoItem{
				ID: t.ID, Title: t.Title, Status: tools.TodoStatus(t.Status),
			}
		}
		s.env.TodoStore.Update(todoItems)
		s.p.Send(tui.TodoUpdateMsg{})
	}

	// Restore the resumed session's goal — or reset the store when it has
	// none, so a goal from the previously open session does not leak across.
	s.env.GoalStore.RestoreFromSnapshot(st.Goal)

	if targetEnv := st.EnvTarget; targetEnv != "local" {
		s.sessionResumeWarning = s.attemptSSHResume(targetEnv)
	}
}

func (s *interactiveState) handleConfig(cfgMsg *config.Config) {
	// Update stored config
	s.cfg = cfgMsg

	newProvName, newModelName := cfgMsg.GetProviderModel()
	newProviders := cfgMsg.GetProviders()
	newProvCfg := newProviders[newProvName]
	if newProvCfg == nil {
		return
	}

	// Refresh registry so new custom models / providers are available.
	s.registry = internalmodel.NewModelRegistryWithConfig(cfgMsg)

	newBaseURL := newProvCfg.BaseURL
	if newBaseURL == "" {
		newBaseURL = s.registry.GetProviderAPI(newProvName)
	}
	newChatModel, err := internalmodel.NewChatModel(s.ctx, &internalmodel.ChatModelConfig{
		Model: newModelName, APIKey: newProvCfg.APIKey, BaseURL: newBaseURL,
	})
	if err != nil {
		return
	}
	s.chatModel = newChatModel

	// Rebuild system prompt and tools to reflect config changes (e.g., SSH aliases)
	if s.agentMode == tui.ModePlanning {
		s.systemPrompt = prompts.GetPlanSystemPrompt(s.platform, s.pwd, s.env.Exec.Label(), s.envInfo)
		s.toolList = s.buildPlanTools()
	} else {
		s.systemPrompt = prompts.GetSystemPrompt(s.platform, s.pwd, s.env.Exec.Label(), s.envInfo, s.skillLoader.Descriptions())
		s.toolList = s.buildAllTools()
	}

	if newAg, err := s.createAgent(); err == nil {
		s.ag = newAg
	}
}

func (s *interactiveState) handleCompact() {
	var oldTokens int64
	if s.agentTokenUsage != nil {
		_, _, oldTokens = s.agentTokenUsage.Get()
	}
	oldLen := len(s.history)
	s.history = agent.CompactHistory(s.ctx, s.chatModel, s.history)
	var newTokens int64
	if s.agentTokenUsage != nil {
		_, _, newTokens = s.agentTokenUsage.Get()
	}
	if s.rec != nil && len(s.history) < oldLen && len(s.history) > 0 {
		s.rec.RecordCompact(s.history[0].Content, oldLen-len(s.history))
	}
	if s.agentTokenUsage != nil {
		s.agentTokenUsage.Reset()
	}
	s.p.Send(tui.CompactDoneMsg{
		OldTokens: oldTokens,
		NewTokens: newTokens,
	})
}

func (s *interactiveState) handleAddModel() {
	_ = s.p.ReleaseTerminal()
	ok, setupErr := tui.RunSetupTUI()
	_ = s.p.RestoreTerminal()
	if setupErr != nil {
		s.p.Send(tui.AgentDoneMsg{Err: fmt.Errorf("setup error: %w", setupErr)})
		return
	}
	if !ok {
		return
	}
	newCfg, loadErr := config.LoadConfig()
	if loadErr != nil {
		return
	}
	newProvName, newModelName := newCfg.GetProviderModel()
	newProviders := newCfg.GetProviders()
	newProvCfg := newProviders[newProvName]
	if newProvCfg == nil {
		return
	}
	// Refresh registry so new custom models / providers are available.
	s.registry = internalmodel.NewModelRegistryWithConfig(newCfg)

	newBaseURL := newProvCfg.BaseURL
	if newBaseURL == "" {
		newBaseURL = s.registry.GetProviderAPI(newProvName)
	}
	newChatModel, cmErr := internalmodel.NewChatModel(s.ctx, &internalmodel.ChatModelConfig{
		Model: newModelName, APIKey: newProvCfg.APIKey, BaseURL: newBaseURL,
	})
	if cmErr != nil {
		return
	}
	s.chatModel = newChatModel
	if newAg, agErr := s.createAgent(); agErr == nil {
		s.ag = newAg
	}
	s.p.Send(tui.ConfigUpdatedMsg{
		Provider: newProvName,
		Model:    newModelName,
		Message:  fmt.Sprintf("✅ Added model: %s/%s\n", newProvName, newModelName),
	})
}

func (s *interactiveState) handleSSH(connMsg interface{}) {
	switch msg := connMsg.(type) {
	case tui.SSHConnectMsg:
		HandleSSHConnect(s.ctx, s.env, msg.Addr, msg.Path, s.p, &s.systemPrompt,
			&s.ag, s.chatModel, s.createAgent, s.skillLoader.Descriptions())
	case tui.SSHListDirReqMsg:
		HandleSSHListDir(s.ctx, s.env, msg.Path, s.p)
	case tui.SSHCancelMsg:
		s.env.ResetToLocal(s.pwd, s.platform)
		if s.agentMode == tui.ModePlanning {
			s.systemPrompt = prompts.GetPlanSystemPrompt(s.platform, s.pwd, "local", s.envInfo)
		} else {
			s.systemPrompt = prompts.GetSystemPrompt(s.platform, s.pwd, "local", s.envInfo, s.skillLoader.Descriptions())
		}
		if newAg, err := s.createAgent(); err == nil {
			s.ag = newAg
		}
	}
}

// runEventLoop is the main goroutine that processes TUI events and drives
// the agent loop. It is started from RunInteractive after TUI setup.
func (s *interactiveState) runEventLoop(initialHistory []adk.Message, initialResumeUUID string,
	initialResumeEntries []tui.SessionEntry, hasPrompt bool, prompt string, mcpStatuses []tui.MCPStatusItem) {
	defer func() {
		if s.rec != nil {
			s.rec.Close()
		}
		if s.langfuseTracer != nil {
			s.langfuseTracer.Flush()
		}
	}()

	s.p.Send(team.SetTeamManagerMsg{Manager: s.teamManager})

	if len(mcpStatuses) > 0 {
		s.p.Send(tui.MCPStatusMsg{Statuses: mcpStatuses})
	}
	if agentsMdPath := prompts.HasAgentsMd(s.pwd); agentsMdPath != "" {
		s.p.Send(tui.AgentsMdMsg{Found: true, Path: agentsMdPath})
	}

	if slashSkills := s.skillLoader.SlashCommands(); len(slashSkills) > 0 {
		var slashInfos []tui.SkillSlashInfo
		for _, sk := range slashSkills {
			slashInfos = append(slashInfos, tui.SkillSlashInfo{
				Slash:       sk.Slash,
				Description: sk.Description,
			})
		}
		s.p.Send(tui.SkillsLoadedMsg{SlashCommands: slashInfos})
	}

	s.history = initialHistory
	if initialResumeUUID != "" {
		s.p.Send(tui.SessionResumedMsg{UUID: initialResumeUUID, Entries: initialResumeEntries})
	}

	if hasPrompt {
		s.p.Send(tui.UserPromptMsg{Prompt: prompt})
		if s.rec != nil {
			s.rec.RecordUser(prompt)
		}
		runCtx, runCancel := context.WithCancel(s.ctx)
		s.cancelFunc = runCancel
		s.runCtx = runCtx
		s.agentRunning.Store(true)
		s.history = append(s.history, schema.UserMessage(prompt))
		resp := runner.Run(runCtx, s.ag, s.history, s.h, s.rec, s.env.TodoStore, s.env.GoalStore, s.langfuseTracer, s.agentTokenUsage)
		runCancel()
		s.runCtx = nil
		s.agentRunning.Store(false)
		if resp != "" {
			s.history = append(s.history, &schema.Message{Role: schema.Assistant, Content: resp})
		}
		s.history = agent.SyncSummarization(s.summCapture, s.history, s.rec)
	}

	promptCh := tui.GetPromptChannel()
	pendingPromptCh := tui.GetPendingPromptChannel()
	sshCh := tui.GetSSHChannel()
	configCh := tui.GetConfigChannel()
	addModelCh := tui.GetAddModelChannel()
	resumeCh := tui.GetResumeChannel()
	compactCh := tui.GetCompactChannel()
	modeSelectCh := tui.GetModeSelectChannel()
	channelActionCh := tui.GetChannelActionChannel()
	mcpLoginCh := tui.GetMCPLoginChannel()
	cancelAgentCh := tui.GetCancelAgentChannel()

	// Background goroutine to handle agent cancellation requests.
	// This is necessary because the main event loop blocks on handlePrompt/runner.Run,
	// so the cancel channel must be consumed independently.
	go func() {
		for range cancelAgentCh {
			if s.cancelFunc != nil {
				config.Logger().Printf("[interactive] cancelling agent job via Ctrl+C")
				s.cancelFunc()
			}
		}
	}()

	// Send initial WeChat state to TUI
	s.p.Send(tui.ChannelStateMsg{
		ChannelID: "wechat",
		State:     s.wechatClient.State().String(),
	})

	for {
		select {
		case sm := <-modeSelectCh:
			s.applySessionMode(sm)

		case cfgMsg := <-configCh:
			s.handleConfig(cfgMsg)

		case userPrompt := <-promptCh:
			s.drainModeSwitch(modeSelectCh)
			s.handlePrompt(userPrompt)

		case pendingPrompt := <-pendingPromptCh:
			s.drainModeSwitch(modeSelectCh)
			s.p.Send(tui.UserPromptMsg{Prompt: pendingPrompt})
			s.handlePrompt(pendingPrompt)

		case uuid := <-resumeCh:
			s.handleResume(uuid)

		case connMsg := <-sshCh:
			s.handleSSH(connMsg)

		case <-compactCh:
			s.handleCompact()

		case <-addModelCh:
			s.handleAddModel()

		case action := <-channelActionCh:
			s.handleChannelAction(action)

		case name := <-mcpLoginCh:
			s.handleMCPLogin(name)
		}
	}
}

// handleMCPLogin runs an OAuth login for the named server in the background so
// the event loop is not blocked while the user completes the browser flow. On
// success it persists config and hot-reloads MCP tools.
func (s *interactiveState) handleMCPLogin(name string) {
	srv := s.cfg.MCPServers[name]
	if srv == nil {
		s.p.Send(tui.MCPNoticeMsg{Text: "server not found: " + name})
		return
	}
	if srv.URL == "" || (srv.Type != "http" && srv.Type != "sse") {
		s.p.Send(tui.MCPNoticeMsg{Text: "OAuth login only applies to http/sse servers"})
		return
	}
	if srv.OAuth == nil {
		srv.OAuth = &config.MCPOAuthConfig{Enabled: true}
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
		defer cancel()
		err := tools.PerformMCPOAuthLogin(ctx, name, srv, func(authURL string) {
			s.p.Send(tui.MCPNoticeMsg{Text: "opening browser…"})
			if err := util.OpenURL(authURL); err != nil {
				config.Logger().Printf("[mcp] open browser: %v", err)
			}
		})
		if err != nil {
			s.p.Send(tui.MCPNoticeMsg{Text: "login failed: " + err.Error()})
			return
		}
		if err := config.SaveConfig(s.cfg); err != nil {
			config.Logger().Printf("[mcp] save config after login: %v", err)
		}
		s.p.Send(tui.MCPNoticeMsg{Text: name + " authorized — reloading tools"})
		s.reloadMCP()
	}()
}

// RunInteractive starts the interactive TUI session.
// The unsafe flag enables auto-approve for all tool calls and takes precedence over config.
func RunInteractive(prompt, resumeUUID string, unsafe bool) error {
	prompt = strings.TrimSpace(prompt)
	hasPrompt := prompt != ""

	// Redirect default log output to the app error log so library diagnostics
	// (e.g. Langfuse upload errors) are visible without corrupting the TUI.
	log.SetOutput(config.Logger().Writer())

	// Setup wizard if config is missing.
	if config.NeedsSetup() {
		ok, err := tui.RunSetupTUI()
		if err != nil {
			return fmt.Errorf("setup error: %w", err)
		}
		if !ok {
			return nil
		}
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("config error: %v\nconfig file: %s", err, config.ConfigPath())
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	pwd := util.GetWorkDir()
	platform := util.GetSystemInfo()
	envInfo := util.CollectEnvInfo(pwd)

	skillLoader := skills.NewLoaderWithDisabled(cfg.DisabledSkills)
	skillLoader.ScanProjectSkills(pwd)

	systemPrompt := prompts.GetSystemPrompt(platform, pwd, "local", envInfo, skillLoader.Descriptions())

	providerName, modelName := cfg.GetProviderModel()

	providers := cfg.GetProviders()
	providerCfg := providers[providerName]
	if providerCfg == nil {
		return fmt.Errorf("provider %q not found in config", providerName)
	}

	registry := internalmodel.NewModelRegistryWithConfig(cfg)
	baseURL := providerCfg.BaseURL
	if baseURL == "" {
		baseURL = registry.GetProviderAPI(providerName)
	}

	chatModel, err := internalmodel.NewChatModel(ctx, &internalmodel.ChatModelConfig{
		Model: modelName, APIKey: providerCfg.APIKey, BaseURL: baseURL,
	})
	if err != nil {
		return fmt.Errorf("error creating model: %w", err)
	}

	env := tools.NewEnv(pwd, platform)
	bgManager := tools.NewBackgroundManager(env)

	var mcpTools []tool.BaseTool
	var mcpStatuses []tui.MCPStatusItem
	if len(cfg.MCPServers) > 0 {
		var internalStatuses []tools.MCPStatus
		mcpTools, internalStatuses = tools.LoadMCPTools(ctx, cfg.MCPServers)
		mcpStatuses = mcpStatusItems(internalStatuses)
	}

	planStore := tools.NewPlanStore()

	rec, _ := session.NewRecorder(pwd, providerName, modelName)

	env.TodoStore.OnUpdate = func(items []tools.TodoItem) {
		if rec != nil {
			snapItems := make([]session.TodoSnapshotItem, len(items))
			for i, it := range items {
				snapItems[i] = session.TodoSnapshotItem{
					ID: it.ID, Title: it.Title, Status: string(it.Status),
				}
			}
			rec.RecordTodoSnapshot(snapItems)
		}
	}

	env.GoalStore.OnUpdate = tools.GoalRecorderHook(rec)

	askUserCh := make(chan tools.AskUserResponse, 1)
	askUserDeps := &tools.AskUserDeps{
		ResponseCh: askUserCh,
	}

	st := &interactiveState{
		ctx:          ctx,
		cancelFunc:   cancelFunc,
		cfg:          cfg,
		chatModel:    chatModel,
		env:          env,
		bgManager:    bgManager,
		planStore:    planStore,
		summCapture:  &agent.SummarizationCapture{},
		systemPrompt: systemPrompt,
		agentMode:    tui.ModeNormal,
		envInfo:      envInfo,
		pwd:          pwd,
		platform:     platform,
		registry:     registry,
		skillLoader:  skillLoader,
		askUserDeps:  askUserDeps,
		mcpTools:     mcpTools,
		rec:          rec,
	}

	// Initialize WeChat channel
	st.wechatClient = weixin.NewClient()

	// Auto-enable WeChat if credentials exist
	if st.wechatClient.State() == channel.StateDisabled {
		st.wechatClient.SetOnMessage(func(from, text string) {
			if st.p != nil {
				// Notify user if agent is busy
				if st.agentRunning.Load() {
					_ = st.wechatClient.SendText(channel.BusyMessage())
				}
				st.p.Send(tui.ChannelInboundMsg{
					ChannelID: "wechat",
					From:      from,
					Text:      text,
				})
			}
		})
		if err := st.wechatClient.Enable(); err != nil {
			config.Logger().Printf("[wechat] auto-enable failed: %v", err)
		} else {
			config.Logger().Printf("[wechat] auto-enabled on startup")
			go func() {
				if err := st.wechatClient.SendText(channel.WelcomeMessage(time.Now())); err != nil {
					config.Logger().Printf("[wechat] failed to send welcome: %v", err)
				}
			}()
		}
	}

	if cfg.Telemetry != nil && cfg.Telemetry.Langfuse != nil {
		st.langfuseTracer = telemetry.NewLangfuseTracer(cfg.Telemetry.Langfuse)
	}

	teamManager := team.NewManager(&team.ManagerDeps{
		DefaultModel: chatModel,
		EnvFactory: func(cwd string) any {
			return tools.NewEnv(cwd, platform)
		},
		ToolBuilder: func(childEnv any, agentType string) []tool.BaseTool {
			e, ok := childEnv.(*tools.Env)
			if !ok {
				return nil
			}
			return []tool.BaseTool{
				e.NewReadTool(), e.NewEditTool(), e.NewWriteTool(),
				e.NewExecuteTool(nil), e.NewGrepTool(),
				e.NewTodoWriteTool(), e.NewTodoReadTool(),
			}
		},
		ModelFactory: func(mCtx context.Context, mName string) (any, error) {
			parts := strings.SplitN(mName, "/", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid model format %q, expected 'provider/model'", mName)
			}
			pName, modelID := parts[0], parts[1]
			pProviders := cfg.GetProviders()
			pCfg := pProviders[pName]
			if pCfg == nil {
				return nil, fmt.Errorf("unknown provider %q", pName)
			}
			bURL := pCfg.BaseURL
			if bURL == "" {
				bURL = registry.GetProviderAPI(pName)
			}
			return internalmodel.NewChatModel(mCtx, &internalmodel.ChatModelConfig{
				Model: modelID, APIKey: pCfg.APIKey, BaseURL: bURL,
			})
		},
		PromptBuilder: func(agentType, agentPwd, agentPlatform string) string {
			return prompts.GetSystemPrompt(agentPlatform, agentPwd, "local", nil, "")
		},
		LeaderSessionUUID: rec.UUID(),
		Tracer:            st.langfuseTracer,
	})
	st.teamManager = teamManager
	st.toolList = st.buildAllTools()

	// Resolve the startup session mode. CLI --unsafe forces Full access and takes
	// precedence over config. Otherwise DefaultMode wins, falling back to the
	// legacy AutoApprove bool (true → Full access) when DefaultMode is unset.
	startupMode := resolveStartupMode(cfg, unsafe)
	approvalState := runner.NewApprovalStateWithMode(pwd, startupMode)
	st.approvalState = approvalState

	p, _ := tui.RunTUI(hasPrompt, pwd, env.TodoStore, tui.WithVersion(Version), tui.WithGoalStore(env.GoalStore), tui.WithStartupMode(startupMode), tui.WithTheme(cfg.Theme), tui.WithApprovalModeChange(func(enabled bool) {
		approvalState.SetSessionApproval(enabled)
	}))
	st.p = p
	bgManager.SetNotifier(func(taskID, cmd, status string) {
		p.Send(tui.BgTaskDoneMsg{TaskID: taskID, Command: cmd, Status: status})
	})
	teamManager.SetTuiProgram(p)

	h := handler.NewTUIHandler(p)

	// Wrap with notifying handler for WeChat push notifications
	notifyingH := handler.NewNotifyingHandler(h, 10*time.Second)
	notifyingH.SetApprovalNotifier(func(toolName, toolArgs string) {
		if st.wechatClient.State() == channel.StateEnabled {
			if err := st.wechatClient.SendText(channel.ApprovalMessage(toolName, toolArgs, "Please return to terminal")); err != nil {
				config.Logger().Printf("[wechat] failed to send approval notification: %v", err)
			}
		}
	})
	notifyingH.SetDoneNotifier(func(summary string, err error) {
		if st.wechatClient.State() == channel.StateEnabled {
			if sendErr := st.wechatClient.SendText(channel.DoneMessage(summary, err)); sendErr != nil {
				config.Logger().Printf("[wechat] failed to send done notification: %v", sendErr)
			}
		}
	})

	// Register WeChat as a notifier for working/idle status pushes.
	notifyingH.AddNotifier(channel.NewChannelNotifier(st.wechatClient))

	// Register BLE notifier if enabled (lazy connect — will auto-discover JCODE-* devices).
	if cfg.Channel != nil && cfg.Channel.BLEEnabled {
		bleNotifier := ble.New()
		notifyingH.AddNotifier(bleNotifier)
		// Push initial idle status (triggers BLE discovery in background).
		bleNotifier.Notify(channel.NotifyEvent{Type: channel.EventIdle})

		// Forward BLE inbound commands to TUI.
		if bleCh := bleNotifier.Receive(); bleCh != nil {
			go func() {
				for cmd := range bleCh {
					p.Send(tui.BLECommandMsg{Cmd: cmd.Cmd, Val: cmd.Val})
				}
			}()
		}
	}

	st.h = notifyingH
	approvalState.SetHandler(notifyingH)

	teamManager.SetHandlersFactory(func(workerName, workerColor string) []adk.ChatModelAgentMiddleware {
		return agent.NewTeammateHandlers(approvalState.NewTeammateApprovalFunc(workerName, workerColor))
	})

	askUserDeps.NotifyFn = func(question string, options []string) {
		p.Send(tui.AskUserQuestionMsg{Question: question, Options: options})
	}

	go func() {
		tuiAskCh := tui.GetAskUserResponseChannel()
		for resp := range tuiAskCh {
			askUserCh <- tools.AskUserResponse{Answer: resp.Answer}
		}
	}()

	ag, err := st.createAgent()
	if err != nil {
		return fmt.Errorf("error creating agent: %w", err)
	}
	st.ag = ag

	// Record the system prompt and environment snapshot for KV-cache-friendly resume.
	if rec != nil {
		envSnapshot := prompts.SerializeEnvInfo(platform, pwd, "local", envInfo)
		rec.RecordSystemPrompt(systemPrompt, envSnapshot)
	}

	env.OnEnvChange = func(envLabel string, isLocal bool, envErr error) {
		if envErr != nil {
			p.Send(tui.SSHStatusMsg{Success: false, Err: envErr})
			return
		}
		if isLocal {
			approvalState.SetWorkpath(pwd)
			if st.agentMode == tui.ModePlanning {
				st.systemPrompt = prompts.GetPlanSystemPrompt(platform, pwd, "local", envInfo)
			} else {
				st.systemPrompt = prompts.GetSystemPrompt(platform, pwd, "local", envInfo, skillLoader.Descriptions())
			}
			if newAg, agErr := st.createAgent(); agErr == nil {
				st.ag = newAg
			}
			p.Send(tui.SSHCancelMsg{})
			return
		}
		approvalState.SetWorkpath(env.Pwd())
		if st.agentMode == tui.ModePlanning {
			st.systemPrompt = prompts.GetPlanSystemPrompt(platform, pwd, envLabel, nil)
		} else {
			st.systemPrompt = prompts.GetSystemPrompt(platform, pwd, envLabel, nil, skillLoader.Descriptions())
		}
		if newAg, agErr := st.createAgent(); agErr == nil {
			st.ag = newAg
		}
		p.Send(tui.SSHStatusMsg{Success: true, Label: envLabel})
	}

	// Load a previous session if --resume was requested.
	var initialHistory []adk.Message
	var initialResumeUUID string
	var initialResumeEntries []tui.SessionEntry
	if resumeUUID != "" {
		entries, loadErr := session.LoadSession(resumeUUID)
		if loadErr != nil {
			return fmt.Errorf("cannot load session: %w", loadErr)
		}
		resumeState := session.ReconstructState(entries)
		initialHistory = session.PruneOldToolOutputs(resumeState.History, 2)
		initialResumeUUID = resumeUUID
		initialResumeEntries = tui.ConvertSessionEntries(entries)
		hasPrompt = false

		// Restore stored system prompt for KV-cache-friendly resume.
		if resumeState.SystemPrompt != "" {
			systemPrompt = resumeState.SystemPrompt
			st.systemPrompt = systemPrompt
			// Inject environment diff as an additional system message.
			envDiff := prompts.BuildEnvDiff(resumeState.EnvInfo, platform, pwd, "local", envInfo)
			if envDiff != "" {
				initialHistory = append(initialHistory, &schema.Message{
					Role:    schema.System,
					Content: envDiff,
				})
			}
		}

		if resumeState.Plan != nil {
			switch resumeState.Plan.Status {
			case "approved":
				planStore.Submit(resumeState.Plan.Title, resumeState.Plan.Content)
				planStore.Approve()
			case "submitted":
				planStore.Submit(resumeState.Plan.Title, resumeState.Plan.Content)
			case "rejected":
				planStore.SetDraft(resumeState.Plan.Title, resumeState.Plan.Content)
			}
		}

		if len(resumeState.Todos) > 0 {
			todoItems := make([]tools.TodoItem, len(resumeState.Todos))
			for i, t := range resumeState.Todos {
				todoItems[i] = tools.TodoItem{
					ID: t.ID, Title: t.Title, Status: tools.TodoStatus(t.Status),
				}
			}
			env.TodoStore.Update(todoItems)
		}

		env.GoalStore.RestoreFromSnapshot(resumeState.Goal)

		if targetEnv := resumeState.EnvTarget; targetEnv != "local" {
			st.sessionResumeWarning = st.attemptSSHResume(targetEnv)
		}

		// Reuse the existing session UUID so new messages are appended to the same file
		if st.rec != nil {
			st.rec.SetUUID(resumeUUID)
		}
	}

	// Capture a git baseline so the exit summary only shows changes from this session.
	st.sessionBaselineCommit = computeGitBaseline()

	go st.runEventLoop(initialHistory, initialResumeUUID, initialResumeEntries, hasPrompt, prompt, mcpStatuses)

	if _, err := p.Run(); err != nil {
		if st.langfuseTracer != nil {
			st.langfuseTracer.Flush()
		}
		return fmt.Errorf("TUI error: %w", err)
	}

	// Send goodbye via WeChat if enabled
	if st.wechatClient.State() == channel.StateEnabled {
		// Best-effort, don't block exit
		go func() { _ = st.wechatClient.SendText(channel.GoodbyeMessage(time.Now())) }()
		time.Sleep(500 * time.Millisecond)
		_ = st.wechatClient.Disable()
	}

	// Close all notifiers (BLE, etc.)
	notifyingH.CloseNotifiers()

	if st.langfuseTracer != nil {
		st.langfuseTracer.Flush()
	}

	// Print session summary on exit.
	fmt.Println()
	fmt.Println("Session Summary")
	fmt.Println("===============")
	added, deleted := computeGitDiffStats(st.sessionBaselineCommit)
	fmt.Printf("Files changed: +%d/-%d lines\n", added, deleted)
	promptTokens, completionTokens, totalTokens := internalmodel.TokenTracker.Get()
	byModel := internalmodel.TokenTracker.GetByModel()
	if len(byModel) > 0 {
		for model, tokens := range byModel {
			fmt.Printf("Total tokens (%s): %d\n", model, tokens)
		}
	} else {
		fmt.Printf("Total tokens: %d (prompt: %d, completion: %d)\n", totalTokens, promptTokens, completionTokens)
	}
	if st.rec != nil && st.rec.HasRecording() {
		fmt.Printf("Resume: jcode --resume %s\n", st.rec.UUID())
	}
	fmt.Println()

	return nil
}

// sessionModeFrom derives the unified selector mode from the two low-level axes
// (tool/prompt + approval). Plan is determined purely by the tool axis; among the
// non-plan agent modes (Normal/Executing) the approval axis decides Approval vs
// Full access. This is the inverse of mode.IsPlan()/AutoApprove().
func sessionModeFrom(am tui.AgentMode, apm handler.ApprovalMode) mode.SessionMode {
	if am == tui.ModePlanning {
		return mode.Plan
	}
	if apm == handler.ModeAuto {
		return mode.FullAccess
	}
	return mode.Approval
}

// resolveStartupMode picks the initial session mode from CLI flags and config.
// Precedence: --unsafe (forces Full access) > DefaultMode > legacy AutoApprove.
func resolveStartupMode(cfg *config.Config, unsafe bool) mode.SessionMode {
	if unsafe {
		return mode.FullAccess
	}
	if cfg != nil && cfg.DefaultMode != "" {
		return mode.Parse(cfg.DefaultMode)
	}
	if cfg != nil && cfg.AutoApprove { //nolint:staticcheck // intentional fallback to the deprecated field when DefaultMode is unset
		return mode.FullAccess
	}
	return mode.Approval
}

// computeGitBaseline creates a transient stash commit of the current working tree
// without modifying it. Returns the commit hash or empty string if there is nothing to stash.
func computeGitBaseline() string {
	cmd := exec.Command("git", "stash", "create", "jcode session baseline")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// computeGitDiffStats returns the total added and deleted lines since the session started.
// If baseline is non-empty, it diffs against that baseline commit; otherwise it falls back to git diff.
func computeGitDiffStats(baseline string) (added, deleted int) {
	var args []string
	if baseline != "" {
		args = []string{"diff", baseline, "--numstat"}
	} else {
		args = []string{"diff", "--numstat"}
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			a, _ := strconv.Atoi(fields[0])
			d, _ := strconv.Atoi(fields[1])
			added += a
			deleted += d
		}
	}
	return added, deleted
}
