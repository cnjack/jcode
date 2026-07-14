package command

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	acp "github.com/coder/acp-go-sdk"
	"github.com/spf13/cobra"

	"github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/flow"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/hooks"
	mempipeline "github.com/cnjack/jcode/internal/memory/pipeline"
	"github.com/cnjack/jcode/internal/mode"
	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/prompts"
	"github.com/cnjack/jcode/internal/runner"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/skills"
	"github.com/cnjack/jcode/internal/telemetry"
	"github.com/cnjack/jcode/internal/tools"
	util "github.com/cnjack/jcode/internal/util"
)

// acpSession bundles all per-session state created in NewSession.
// ACP mode IDs match mode.SessionMode.String().
const (
	acpModeApproval   acp.SessionModeId = "approval"
	acpModePlan       acp.SessionModeId = "plan"
	acpModeFullAccess acp.SessionModeId = "full_access"
)

// acpModeID maps a unified mode to its advertised ACP wire id.
func acpModeID(m mode.SessionMode) acp.SessionModeId {
	return acp.SessionModeId(m.String())
}

// acpModes returns the mode list advertised in NewSession/ResumeSession responses.
func acpModes(current acp.SessionModeId) *acp.SessionModeState {
	return &acp.SessionModeState{
		CurrentModeId: current,
		AvailableModes: []acp.SessionMode{
			{Id: acpModeApproval, Name: "Ask for approval", Description: acp.Ptr("Ask before each restricted tool call")},
			{Id: acpModePlan, Name: "Plan", Description: acp.Ptr("Read-only planning mode for analysis")},
			{Id: acpModeFullAccess, Name: "Full access", Description: acp.Ptr("Unrestricted execution without approval prompts")},
		},
	}
}

type acpSession struct {
	h             *handler.ACPHandler
	ag            *adk.ChatModelAgent
	env           *tools.Env
	approvalState *runner.ApprovalState
	history       []adk.Message
	rec           *session.Recorder
	todoStore     *tools.TodoStore
	tracer        *telemetry.LangfuseTracer
	tokenUsage    *internalmodel.TokenUsage
	cancel        context.CancelFunc
	mu            sync.Mutex

	// Mode switching support.
	mode         acp.SessionModeId
	createAgent  func(sysPrompt string, toolList []tool.BaseTool) (*adk.ChatModelAgent, error)
	allTools     []tool.BaseTool
	planTools    []tool.BaseTool
	normalPrompt string
	planPrompt   string
	skillLoader  *skills.Loader
	flowLoader   *flow.Loader
}

// Close releases resources held by the session (recorder file handle, tracer).
func (s *acpSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec != nil {
		s.rec.Close()
		s.rec = nil
	}
	if s.tracer != nil {
		s.tracer.Flush()
		s.tracer = nil
	}
}

// acpAgent implements the acp.Agent and acp.AgentLoader interfaces, exposing jcode as an ACP server.
type acpAgent struct {
	conn *acp.AgentSideConnection

	mu       sync.Mutex
	sessions map[acp.SessionId]*acpSession
}

// Ensure acpAgent implements acp.AgentLoader interface.
var _ acp.AgentLoader = (*acpAgent)(nil)

func NewACPCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "acp",
		Short:        "Start ACP JSON-RPC server (headless agent protocol)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			handleACPSubcommand()
			return nil
		},
	}
}

func handleACPSubcommand() {
	// Redirect default log to debug log so nothing corrupts stdio JSON-RPC.
	log.SetOutput(config.Logger().Writer())

	a := &acpAgent{
		sessions: make(map[acp.SessionId]*acpSession),
	}
	conn := acp.NewAgentSideConnection(a, os.Stdout, os.Stdin)
	a.conn = conn

	config.Logger().Printf("[acp] ACP server started on stdio")
	<-conn.Done()

	// Clean up all session recorders on connection close.
	a.mu.Lock()
	for id, sess := range a.sessions {
		sess.Close()
		delete(a.sessions, id)
	}
	a.mu.Unlock()

	config.Logger().Printf("[acp] ACP connection closed")
}

// availableCommandList builds the slash commands advertised to ACP clients:
// the built-in /goal command plus any skill-based commands.
func availableCommandList(skillLoader *skills.Loader, flowLoader *flow.Loader) []acp.AvailableCommand {
	cmds := []acp.AvailableCommand{
		{
			Name:        "goal",
			Description: "Set a persistent objective the agent pursues across turns (or 'clear' / status)",
			Input: &acp.AvailableCommandInput{
				Unstructured: &acp.UnstructuredCommandInput{
					Hint: "<objective> | clear | status",
				},
			},
		},
	}
	if skillLoader != nil {
		for _, sk := range skillLoader.SlashCommands() {
			name := strings.TrimPrefix(sk.Slash, "/")
			// "/goal" is consumed by Prompt() before the skill lookup, so a
			// skill with that name could never run — don't advertise it.
			if name == "goal" {
				continue
			}
			cmds = append(cmds, acp.AvailableCommand{
				Name:        name,
				Description: sk.Description,
				Input: &acp.AvailableCommandInput{
					Unstructured: &acp.UnstructuredCommandInput{
						Hint: "additional instructions",
					},
				},
			})
		}
	}
	if flowLoader != nil {
		for _, fc := range flowLoader.SlashCommands() {
			name := strings.TrimPrefix(fc.Slash, "/")
			if name == "goal" {
				continue
			}
			// ACP has no "type" field, so mark workflows in the description so
			// editor command palettes distinguish them from skills.
			cmds = append(cmds, acp.AvailableCommand{
				Name:        name,
				Description: "workflow — " + fc.Description,
				Input: &acp.AvailableCommandInput{
					Unstructured: &acp.UnstructuredCommandInput{
						Hint: "args / context",
					},
				},
			})
		}
	}
	return cmds
}

// broadcastSlashCommands sends the available slash commands to the client via
// an available_commands_update session notification.
func (a *acpAgent) broadcastSlashCommands(sessionID acp.SessionId, sess *acpSession) {
	cmds := availableCommandList(sess.skillLoader, sess.flowLoader)
	if len(cmds) == 0 {
		return
	}

	if err := a.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: sessionID,
		Update: acp.SessionUpdate{
			AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
				AvailableCommands: cmds,
			},
		},
	}); err != nil {
		config.Logger().Printf("[acp] broadcastSlashCommands error: %v", err)
	}
}

// scheduleSlashCommandsBroadcast sends slash commands after the session
// response has had a chance to reach the client. Some ACP clients ignore
// session updates that arrive before the new/load/resume response is fully
// processed, which makes slash commands appear only after a later message.
func (a *acpAgent) scheduleSlashCommandsBroadcast(sessionID acp.SessionId, sess *acpSession) {
	go func() {
		time.Sleep(50 * time.Millisecond)

		a.mu.Lock()
		current, ok := a.sessions[sessionID]
		a.mu.Unlock()
		if !ok || current != sess {
			return
		}

		a.broadcastSlashCommands(sessionID, sess)
	}()
}

// --- acp.Agent interface ---

func (a *acpAgent) Initialize(_ context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	config.Logger().Printf("[acp] Initialize: client=%v, protocol=%d",
		params.ClientInfo, params.ProtocolVersion)

	// Check if the configured model supports image input.
	imageSupport := false
	if cfg, err := config.LoadConfig(); err == nil {
		providerName, modelName := cfg.GetProviderModel()
		registry := internalmodel.NewModelRegistryWithConfig(cfg)
		if _, m, ok := registry.LookupModel(providerName, modelName); ok && m != nil && m.Modalities != nil {
			for _, mod := range m.Modalities.Input {
				if mod == "image" {
					imageSupport = true
					break
				}
			}
		}
	}

	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{
			PromptCapabilities: acp.PromptCapabilities{
				EmbeddedContext: true,
				Image:           imageSupport,
			},
			LoadSession: true,
			SessionCapabilities: acp.SessionCapabilities{
				List:   &acp.SessionListCapabilities{},
				Close:  &acp.SessionCloseCapabilities{},
				Resume: &acp.SessionResumeCapabilities{},
			},
		},
		AgentInfo: &acp.Implementation{
			Name:    "jcode",
			Title:   acp.Ptr("JCODE — Coding Assistant"),
			Version: Version, // set from internal/command/version.go
		},
	}, nil
}

func (a *acpAgent) Authenticate(_ context.Context, _ acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

func (a *acpAgent) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	config.Logger().Printf("[acp] NewSession: cwd=%s, mcpServers=%d", params.Cwd, len(params.McpServers))

	cfg, err := config.LoadConfig()
	if err != nil {
		return acp.NewSessionResponse{}, fmt.Errorf("config error: %w", err)
	}

	pwd := params.Cwd
	if pwd == "" {
		pwd = util.GetWorkDir()
	}

	providerName, modelName := cfg.GetProviderModel()
	rec, _ := session.NewRecorder(pwd, providerName, modelName)
	sessionID := acp.SessionId(fmt.Sprintf("sess_%s", rec.UUID()))

	sess, err := a.buildAgentSession(ctx, cfg, pwd, sessionID, rec, nil)
	if err != nil {
		return acp.NewSessionResponse{}, err
	}

	// Background memory distillation on session start (gates inside).
	mempipeline.MaybeStartBackground(cfg, pwd)

	a.mu.Lock()
	a.sessions[sessionID] = sess
	a.mu.Unlock()

	config.Logger().Printf("[acp] Session created: %s", sessionID)

	// Broadcast available slash commands for this session after the response
	// is returned so clients have registered the session before the update.
	a.scheduleSlashCommandsBroadcast(sessionID, sess)

	return acp.NewSessionResponse{
		SessionId: sessionID,
		Modes:     acpModes(sess.mode),
	}, nil
}

// buildAgentSession creates the env, tools, middlewares, and agent shared by
// NewSession and LoadSession. The caller is responsible for creating the
// Recorder (so the session ID can be derived from it) and for storing the
// returned session in a.sessions.
func (a *acpAgent) buildAgentSession(
	ctx context.Context,
	cfg *config.Config,
	pwd string,
	sessionID acp.SessionId,
	rec *session.Recorder,
	history []*schema.Message,
) (*acpSession, error) {
	platform := util.GetSystemInfo()
	envInfo := util.CollectEnvInfo(pwd)

	skillLoader := skills.NewLoader()
	skillLoader.ScanProjectSkills(pwd)

	flowLoader := flow.NewLoader()
	flowLoader.LoadProject(pwd)

	providerName, modelName := cfg.GetProviderModel()
	providers := cfg.GetProviders()
	providerCfg := providers[providerName]
	if providerCfg == nil {
		return nil, fmt.Errorf("provider %q not found in config", providerName)
	}

	registry := internalmodel.NewModelRegistryWithConfig(cfg)
	baseURL := providerCfg.BaseURL
	if baseURL == "" {
		baseURL = registry.GetProviderAPI(providerName)
	}

	// Apply a per-model reasoning-effort override (set from the chat picker)
	// over the provider-level default before constructing the model.
	acpEffortCfg := *providerCfg
	acpEffortCfg.ReasoningEffort = config.ResolveEffort(providerName, modelName, providerCfg.ReasoningEffort)
	chatModel, err := internalmodel.NewChatModelFromProvider(ctx, modelName, baseURL, &acpEffortCfg)
	if err != nil {
		return nil, fmt.Errorf("error creating model: %w", err)
	}

	env := tools.NewEnv(pwd, platform)
	config.Logger().Printf("[acp] using LocalExecutor for session %s", sessionID)
	bgManager := tools.NewBackgroundManager(env)

	// Per-session token tracker for usage display (goal status, reminders,
	// token updates).
	tokenUsage := &internalmodel.TokenUsage{}

	// Load MCP tools from config.
	var mcpTools []tool.BaseTool
	if len(cfg.MCPServers) > 0 {
		mcpTools, _ = tools.LoadMCPTools(ctx, cfg.MCPServers)
	}

	// The ACPHandler is created before the tool list so the subagent tool can
	// bridge its lifecycle/progress callbacks into ACP tool_call_update
	// notifications (parity with the TUI notifier and the web SSE events).
	acpHandler := handler.NewACPHandler(a.conn, sessionID, pwd)

	// Langfuse tracer — created before the tools so subagent runs nest child
	// traces under the session trace.
	var tracer *telemetry.LangfuseTracer
	if cfg.Telemetry != nil && cfg.Telemetry.Langfuse != nil {
		tracer = telemetry.NewLangfuseTracer(cfg.Telemetry.Langfuse)
	}

	// One factory serves subagent + workflow model overrides (incl. the
	// "small" alias); fallback is this session's current model.
	factory := internalmodel.NewModelFactory(cfg, chatModel)
	allTools := []tool.BaseTool{
		env.NewReadTool(), env.NewEditTool(), env.NewWriteTool(),
		env.NewExecuteTool(bgManager), env.NewGrepTool(),
		env.NewTodoWriteTool(), env.NewTodoReadTool(),
		env.NewGoalSetTool(), env.NewGoalGetTool(), env.NewGoalUpdateTool(),
		env.NewAutomationCreateTool(),
		env.NewSwitchEnvTool(),
		env.NewCheckBackgroundTool(bgManager),
		env.NewSubagentTool(&tools.SubagentDeps{
			ChatModel:    chatModel,
			ModelFactory: factory,
			Recorder:     rec,
			Tracer:       tracer,
			Notifier:     acpHandler.OnSubagentEvent,
			ProgressFn:   acpHandler.OnSubagentProgress,
		}),
		env.NewWorkflowRunTool(&tools.WorkflowToolDeps{
			ModelFactory: factory,
			Recorder:     rec,
			Tracer:       tracer,
			Loader:       flowLoader,
		}),
	}
	if config.MemoryEnabled(cfg) {
		allTools = append(allTools, env.NewMemoryNoteTool(&tools.MemoryNoteDeps{
			SessionIDFn: func() string {
				if rec != nil {
					return rec.UUID()
				}
				return ""
			},
		}))
	}
	allTools = append(allTools, mcpTools...)

	// Plan mode tools: read-only subset. Goal tools are included — like the
	// todo tools they only mutate session metadata, and the continuation
	// guard runs in every mode, so the agent must be able to inspect and
	// complete/block an active goal here too.
	planTools := []tool.BaseTool{
		env.NewReadTool(),
		env.NewExecuteTool(nil),
		env.NewGrepTool(),
		env.NewTodoWriteTool(), env.NewTodoReadTool(),
		env.NewGoalSetTool(), env.NewGoalGetTool(), env.NewGoalUpdateTool(),
	}

	normalPrompt := prompts.GetSystemPrompt(platform, pwd, "local", envInfo, skillLoader.Descriptions())
	planPrompt := prompts.GetPlanSystemPrompt(platform, pwd, "local", envInfo)
	startupMode := resolveStartupMode(cfg, false)
	approvalState := runner.NewApprovalStateWithMode(pwd, startupMode)

	approvalState.SetHandler(acpHandler)

	// Wire up environment change callback so switch_env properly restores
	// the original local executor when switching back from SSH.
	env.OnEnvChange = func(label string, isLocal bool, envErr error) {
		if envErr != nil {
			config.Logger().Printf("[acp] env change error: %v", envErr)
			return
		}
		if isLocal {
			env.ResetToLocal(pwd, platform)
			config.Logger().Printf("[acp] env reset to original executor: %s", env.Exec.Label())
			return
		}
		config.Logger().Printf("[acp] env changed to: %s", label)
	}

	if rec != nil {
		env.TodoStore.OnUpdate = func(items []tools.TodoItem) {
			snapItems := make([]session.TodoSnapshotItem, len(items))
			for i, it := range items {
				snapItems[i] = session.TodoSnapshotItem{
					ID: it.ID, Title: it.Title, Status: string(it.Status),
				}
			}
			rec.RecordTodoSnapshot(snapItems)
		}
		env.GoalStore.OnUpdate = tools.GoalRecorderHook(rec)
	}

	// Build agent with middlewares
	contextLimit := internalmodel.ResolveContextLimit(registry, cfg, providerName, modelName)
	compactThreshold := cfg.CompactionThreshold()

	var handlers []adk.ChatModelAgentMiddleware

	summMw, err := summarization.New(ctx, &summarization.Config{
		Model: chatModel,
		Trigger: &summarization.TriggerCondition{
			ContextTokens: int(float64(contextLimit) * compactThreshold),
		},
		TranscriptFilePath: filepath.Join(config.ConfigDir(), "transcript.txt"),
	})
	if err == nil {
		handlers = append(handlers, summMw)
	}

	reductionMw, err := reduction.New(ctx, agent.BuildReductionConfig(
		filepath.Join(config.ConfigDir(), "reduction"),
		contextLimit,
		compactThreshold,
		internalmodel.NewCalibratedCounter(tokenUsage).Count,
	))
	if err != nil {
		config.Logger().Printf("[acp] reduction middleware init error: %v", err)
	} else {
		handlers = append(handlers, reductionMw)
	}
	// Aggregate cap on one turn's NEW tool results: reduction only caps each
	// result individually (50k), so N parallel calls could still flood a single
	// request. Registered after reduction so per-result truncation runs first.
	handlers = append(handlers, agent.NewTurnToolResultBudgetMiddleware(0))

	reminderMw := agent.NewReminderMiddleware(agent.ReminderConfig{
		TodoStore:    env.TodoStore,
		GoalStore:    env.GoalStore,
		EnvLabel:     "local",
		IsRemote:     env.IsRemote(),
		ContextLimit: contextLimit,
		FileTracker:  env.FileTracker,
		Env:          env,
		Pwd:          pwd,
		Platform:     platform,
		EnvSnapshot:  prompts.SerializeEnvInfo(platform, pwd, "local", envInfo),
	}, tokenUsage)
	handlers = append(handlers, reminderMw)

	// Seed the initial agent from the resolved startup mode (Plan uses the
	// read-only tool/prompt set; Approval/Full access share the full set and differ only
	// on the approval axis, already seeded in approvalState above).
	initialPrompt, initialTools := normalPrompt, allTools
	if startupMode.IsPlan() {
		initialPrompt, initialTools = planPrompt, planTools
	}
	ag, err := agent.NewAgent(ctx, chatModel, initialTools, initialPrompt, approvalState.RequestApproval, nil, handlers)
	if err != nil {
		return nil, fmt.Errorf("error creating agent: %w", err)
	}

	// createAgent closure for mode switching — rebuilds agent with different prompt/tools.
	// Uses context.Background() so the agent survives beyond the original request context.
	makeAgent := func(sysPrompt string, toolList []tool.BaseTool) (*adk.ChatModelAgent, error) {
		return agent.NewAgent(context.Background(), chatModel, toolList, sysPrompt, approvalState.RequestApproval, nil, handlers)
	}

	sess := &acpSession{
		h:             acpHandler,
		ag:            ag,
		env:           env,
		approvalState: approvalState,
		rec:           rec,
		todoStore:     env.TodoStore,
		tracer:        tracer,
		tokenUsage:    tokenUsage,
		history:       history,
		mode:          acpModeID(startupMode),
		createAgent:   makeAgent,
		allTools:      allTools,
		planTools:     planTools,
		normalPrompt:  normalPrompt,
		planPrompt:    planPrompt,
		skillLoader:   skillLoader,
		flowLoader:    flowLoader,
	}

	// Reconcile the session's advertised mode when the handler promotes to
	// Full access via "Allow All", so sess.mode never drifts from the approval
	// state's source of truth.
	acpHandler.SetModeChangeCallback(func(m mode.SessionMode) {
		sess.mu.Lock()
		sess.mode = acpModeID(m)
		sess.mu.Unlock()
	})

	return sess, nil
}

func (a *acpAgent) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	config.Logger().Printf("[acp] Prompt: session=%s, blocks=%d", params.SessionId, len(params.Prompt))

	a.mu.Lock()
	sess, ok := a.sessions[params.SessionId]
	a.mu.Unlock()
	if !ok {
		return acp.PromptResponse{}, fmt.Errorf("unknown session: %s", params.SessionId)
	}

	// Extract text and images from prompt content blocks.
	var userText strings.Builder
	var imageParts []schema.MessageInputPart
	for _, block := range params.Prompt {
		if block.Text != nil {
			userText.WriteString(block.Text.Text)
		}
		if block.Image != nil && block.Image.Data != "" {
			data := block.Image.Data
			imageParts = append(imageParts, schema.MessageInputPart{
				Type: schema.ChatMessagePartTypeImageURL,
				Image: &schema.MessageInputImage{
					MessagePartCommon: schema.MessagePartCommon{
						MIMEType:   block.Image.MimeType,
						Base64Data: &data,
					},
				},
			})
		}
		if block.ResourceLink != nil {
			fmt.Fprintf(&userText, "\n[Resource: %s (%s)]", block.ResourceLink.Name, block.ResourceLink.Uri)
		}
	}

	prompt := userText.String()
	if prompt == "" && len(imageParts) == 0 {
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}

	// Handle slash commands.
	if strings.HasPrefix(prompt, "/") {
		cmd := strings.TrimPrefix(prompt, "/")
		parts := strings.SplitN(cmd, " ", 2)
		cmdName := parts[0]

		// Built-in /goal command. Status and clear are answered locally —
		// the goal state lives in GoalStore, so no agent run is needed (and
		// running one would let the continuation guard auto-continue an
		// active goal from a mere status query).
		if cmdName == "goal" {
			rest := ""
			if len(parts) > 1 {
				rest = parts[1]
			}
			var goalStore *tools.GoalStore
			if sess.env != nil {
				goalStore = sess.env.GoalStore
			}
			action := tools.ParseGoalCommand(rest)
			switch action.Kind {
			case "status":
				line := "🎯 No goal set. Use /goal <objective> to set one."
				if goalStore != nil && goalStore.Has() {
					line = goalStore.StatusLine()
				}
				sess.h.OnAgentText(line)
				return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
			case "clear":
				if goalStore != nil {
					goalStore.Clear()
				}
				sess.h.OnAgentText("🎯 Goal cleared.")
				return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
			default: // "set"
				objective, err := tools.ValidateGoalObjective(action.Objective)
				if err != nil {
					sess.h.OnAgentText("🎯 " + err.Error())
					return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
				}
				if goalStore != nil {
					goalStore.Set(objective)
				}
				prompt = tools.GoalKickoffPrompt(objective)
			}
		}

		// Check if it's a skill slash command.
		matchedSkill := false
		if sess.skillLoader != nil {
			if sk := sess.skillLoader.GetBySlash("/" + cmdName); sk != nil {
				userInput := ""
				if len(parts) > 1 {
					userInput = parts[1]
				}
				prompt = fmt.Sprintf("Use the load_skill tool with name=%q and follow its instructions. %s", sk.Name, userInput)
				matchedSkill = true
			}
		}

		// Otherwise check workflow slash commands (e.g. /repo-audit).
		if !matchedSkill && sess.flowLoader != nil {
			if wf, ok := sess.flowLoader.GetBySlash("/" + cmdName); ok {
				userInput := ""
				if len(parts) > 1 {
					userInput = parts[1]
				}
				prompt = flow.SlashRunPrompt(wf.Meta.Name, userInput)
			}
		}
	}

	sess.mu.Lock()
	if sess.rec != nil {
		var entryImages []session.EntryImage
		for _, p := range imageParts {
			if p.Image != nil && p.Image.Base64Data != nil {
				entryImages = append(entryImages, session.EntryImage{
					MimeType: p.Image.MIMEType,
					Data:     *p.Image.Base64Data,
				})
			}
		}
		sess.rec.RecordUser(prompt, entryImages...)
	}

	// Build user message — include images as multimodal content if provided.
	var userMsg *schema.Message
	if len(imageParts) > 0 {
		parts := make([]schema.MessageInputPart, 0, len(imageParts)+1)
		if prompt != "" {
			parts = append(parts, schema.MessageInputPart{
				Type: schema.ChatMessagePartTypeText,
				Text: prompt,
			})
		}
		parts = append(parts, imageParts...)
		userMsg = &schema.Message{
			Role:                  schema.User,
			Content:               prompt,
			UserInputMultiContent: parts,
		}
	} else {
		userMsg = schema.UserMessage(prompt)
	}
	sess.history = append(sess.history, userMsg)

	// Create a cancellable context for this prompt turn.
	promptCtx, cancel := context.WithCancel(ctx)
	sess.cancel = cancel
	history := make([]adk.Message, len(sess.history))
	copy(history, sess.history)
	sess.mu.Unlock()

	// Inject the hook dispatcher so PreToolUse/PostToolUse/Stop hooks run on ACP
	// too (parity with the TUI); reloaded per turn so hooks.json edits hot-apply.
	// The recorder is optional here, so fall back to the ACP session id.
	hookSessionID := string(params.SessionId)
	if sess.rec != nil {
		hookSessionID = sess.rec.UUID()
	}
	promptCtx = hooks.WithDispatcher(promptCtx, hooks.NewSessionDispatcher(config.ConfigDir(), sess.env.Pwd(), hookSessionID, config.Logger().Printf))
	resp := runner.Run(promptCtx, sess.ag, history, sess.h, sess.rec, sess.todoStore, sess.env.GoalStore, sess.tracer, sess.tokenUsage)

	sess.mu.Lock()
	if resp != "" {
		sess.history = append(sess.history, &schema.Message{Role: schema.Assistant, Content: resp})
	}
	sess.cancel = nil
	sess.mu.Unlock()

	if promptCtx.Err() != nil {
		return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
	}

	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (a *acpAgent) Cancel(_ context.Context, params acp.CancelNotification) error {
	config.Logger().Printf("[acp] Cancel: session=%s", params.SessionId)

	a.mu.Lock()
	sess, ok := a.sessions[params.SessionId]
	a.mu.Unlock()
	if !ok {
		return nil
	}

	sess.mu.Lock()
	if sess.cancel != nil {
		sess.cancel()
	}
	sess.mu.Unlock()
	return nil
}

func (a *acpAgent) SetSessionMode(_ context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	config.Logger().Printf("[acp] SetSessionMode: session=%s, mode=%s", params.SessionId, params.ModeId)

	a.mu.Lock()
	sess, ok := a.sessions[params.SessionId]
	a.mu.Unlock()
	if !ok {
		return acp.SetSessionModeResponse{}, fmt.Errorf("unknown session: %s", params.SessionId)
	}

	sess.mu.Lock()

	// Accept only the three canonical ids.
	switch params.ModeId {
	case acpModeApproval, acpModePlan, acpModeFullAccess:
	default:
		sess.mu.Unlock()
		return acp.SetSessionModeResponse{}, fmt.Errorf("unknown mode: %s", params.ModeId)
	}
	sm := mode.Parse(string(params.ModeId))
	newMode := acpModeID(sm)
	if newMode == sess.mode {
		sess.mu.Unlock()
		return acp.SetSessionModeResponse{}, nil
	}

	sess.mode = newMode
	// Apply the approval axis: Full access flips to auto-approve; Approval and Plan use manual approval.
	sess.approvalState.SetSessionMode(sm)
	if sess.rec != nil {
		sess.rec.RecordModeChange(sm.String())
	}

	// Apply the tool/prompt axis: Plan uses the read-only set; Approval and Full access
	// share the full set (they differ only on the approval axis above).
	var sysPrompt string
	var toolList []tool.BaseTool
	if sm.IsPlan() {
		sysPrompt = sess.planPrompt
		toolList = sess.planTools
	} else {
		sysPrompt = sess.normalPrompt
		toolList = sess.allTools
	}

	if sess.createAgent != nil {
		if newAg, err := sess.createAgent(sysPrompt, toolList); err == nil {
			sess.ag = newAg
			config.Logger().Printf("[acp] agent recreated for mode %s", newMode)
		} else {
			config.Logger().Printf("[acp] agent recreation failed: %v", err)
		}
	}

	sess.mu.Unlock()

	// Notify the client of the mode change (outside of lock).
	if err := a.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: params.SessionId,
		Update: acp.SessionUpdate{
			CurrentModeUpdate: &acp.SessionCurrentModeUpdate{
				CurrentModeId: newMode,
			},
		},
	}); err != nil {
		config.Logger().Printf("[acp] SetSessionMode update error: %v", err)
	}

	return acp.SetSessionModeResponse{}, nil
}

func (a *acpAgent) ListSessions(_ context.Context, params acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	cwd := util.GetWorkDir()
	if params.Cwd != nil && *params.Cwd != "" {
		cwd = *params.Cwd
	}
	config.Logger().Printf("[acp] ListSessions: cwd=%s", cwd)

	metas, err := session.ListSessions(cwd)
	if err != nil {
		config.Logger().Printf("[acp] ListSessions error: %v", err)
		return acp.ListSessionsResponse{Sessions: []acp.SessionInfo{}}, nil
	}

	sessions := make([]acp.SessionInfo, 0, len(metas))
	for _, m := range metas {
		var title *string
		if m.Title != "" {
			title = &m.Title
		}
		updatedAt := m.StartTime
		sessions = append(sessions, acp.SessionInfo{
			SessionId: acp.SessionId(fmt.Sprintf("sess_%s", m.UUID)),
			Title:     title,
			Cwd:       cwd,
			UpdatedAt: &updatedAt,
		})
	}
	return acp.ListSessionsResponse{Sessions: sessions}, nil
}

func (a *acpAgent) SetSessionConfigOption(_ context.Context, params acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	config.Logger().Printf("[acp] SetSessionConfigOption")
	return acp.SetSessionConfigOptionResponse{}, nil
}

// CloseSession implements the session/close method.
// It cancels any ongoing work and releases session resources.
func (a *acpAgent) CloseSession(_ context.Context, params acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	config.Logger().Printf("[acp] CloseSession: session=%s", params.SessionId)

	a.mu.Lock()
	sess, ok := a.sessions[params.SessionId]
	if ok {
		delete(a.sessions, params.SessionId)
	}
	a.mu.Unlock()

	if ok {
		// Cancel any ongoing prompt, then release resources.
		sess.mu.Lock()
		if sess.cancel != nil {
			sess.cancel()
		}
		sess.mu.Unlock()
		sess.Close()
	}

	return acp.CloseSessionResponse{}, nil
}

func (a *acpAgent) LoadSession(ctx context.Context, params acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	config.Logger().Printf("[acp] LoadSession: sessionId=%s", params.SessionId)

	// Extract the internal session UUID from the ACP session ID.
	resumeUUID := strings.TrimPrefix(string(params.SessionId), "sess_")

	cfg, err := config.LoadConfig()
	if err != nil {
		return acp.LoadSessionResponse{}, fmt.Errorf("config error: %w", err)
	}

	// Load session history from disk.
	entries, err := session.LoadSession(resumeUUID)
	if err != nil {
		return acp.LoadSessionResponse{}, fmt.Errorf("failed to load session: %w", err)
	}

	pwd := params.Cwd
	if pwd == "" {
		pwd = util.GetWorkDir()
	}

	providerName, modelName := cfg.GetProviderModel()
	rec, _ := session.NewRecorder(pwd, providerName, modelName)
	if rec != nil {
		rec.SetUUID(resumeUUID)
	}

	// Reconstruct full message history (including tool calls/results).
	resumeState := session.ReconstructState(entries)
	history := session.PruneOldToolOutputs(resumeState.History, 2)

	sess, err := a.buildAgentSession(ctx, cfg, pwd, params.SessionId, rec, history)
	if err != nil {
		return acp.LoadSessionResponse{}, err
	}
	sess.env.GoalStore.RestoreFromSnapshot(resumeState.Goal)

	a.mu.Lock()
	if old, ok := a.sessions[params.SessionId]; ok {
		old.Close()
	}
	a.sessions[params.SessionId] = sess
	a.mu.Unlock()

	config.Logger().Printf("[acp] Session loaded: %s, messages=%d", params.SessionId, len(sess.history))

	// Broadcast available slash commands for the loaded session after the
	// response is returned so clients have registered the session first.
	a.scheduleSlashCommandsBroadcast(params.SessionId, sess)

	return acp.LoadSessionResponse{}, nil
}

// ResumeSession implements the session/resume method.
// It resumes an existing session without returning previous messages.
func (a *acpAgent) ResumeSession(ctx context.Context, params acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	config.Logger().Printf("[acp] ResumeSession: session=%s", params.SessionId)

	// Extract the internal session UUID from the ACP session ID.
	resumeUUID := strings.TrimPrefix(string(params.SessionId), "sess_")

	a.mu.Lock()
	if sess, ok := a.sessions[params.SessionId]; ok {
		// Session already in memory — broadcast slash commands after the
		// response so the reconnecting client receives the update.
		s := sess
		a.mu.Unlock()
		a.scheduleSlashCommandsBroadcast(params.SessionId, s)
		return acp.ResumeSessionResponse{Modes: acpModes(s.mode)}, nil
	}
	a.mu.Unlock()

	// Session not in memory — reload from disk so the agent has conversation context.
	cfg, err := config.LoadConfig()
	if err != nil {
		return acp.ResumeSessionResponse{}, fmt.Errorf("config error: %w", err)
	}

	pwd := params.Cwd
	if pwd == "" {
		pwd = util.GetWorkDir()
	}

	providerName, modelName := cfg.GetProviderModel()
	rec, _ := session.NewRecorder(pwd, providerName, modelName)
	// Reuse the original session UUID so transcript entries are written to
	// the same session file (same pattern as LoadSession).
	if rec != nil {
		rec.SetUUID(resumeUUID)
	}

	// Load history from disk so the agent has conversation context.
	var history []*schema.Message
	var goalSnap *session.GoalSnapshot
	restoredMode := mode.Approval
	if entries, err := session.LoadSession(resumeUUID); err == nil {
		resumeState := session.ReconstructState(entries)
		history = session.PruneOldToolOutputs(resumeState.History, 2)
		goalSnap = resumeState.Goal
		// Restore the saved mode (Full access as-is; Plan normalized to Approval so the
		// reloaded full-tool agent is not stranded in read-only plan tools).
		restoredMode = mode.Parse(resumeState.Mode)
		if restoredMode == mode.Plan {
			restoredMode = mode.Approval
		}
		config.Logger().Printf("[acp] ResumeSession: loaded %d history messages for %s", len(history), params.SessionId)
	} else {
		config.Logger().Printf("[acp] ResumeSession: could not load history for %s: %v", params.SessionId, err)
	}

	sess, err := a.buildAgentSession(ctx, cfg, pwd, params.SessionId, rec, history)
	if err != nil {
		return acp.ResumeSessionResponse{}, err
	}
	sess.env.GoalStore.RestoreFromSnapshot(goalSnap)
	// Apply the restored approval axis; Approval/Full access both use the full-tool agent
	// already built above, so no rebuild is needed.
	sess.approvalState.SetSessionMode(restoredMode)
	sess.mode = acpModeID(restoredMode)

	a.mu.Lock()
	a.sessions[params.SessionId] = sess
	a.mu.Unlock()

	config.Logger().Printf("[acp] Session resumed: %s", params.SessionId)
	a.scheduleSlashCommandsBroadcast(params.SessionId, sess)

	return acp.ResumeSessionResponse{Modes: acpModes(sess.mode)}, nil
}

// Logout implements the logout method.
// Terminates the current authenticated session (no-op for jcode, which doesn't require auth).
func (a *acpAgent) Logout(_ context.Context, _ acp.LogoutRequest) (acp.LogoutResponse, error) {
	config.Logger().Printf("[acp] Logout")
	return acp.LogoutResponse{}, nil
}
