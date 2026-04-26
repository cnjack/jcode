package command

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	acp "github.com/coder/acp-go-sdk"
	"github.com/spf13/cobra"

	"github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/handler"
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
// ACP mode IDs.
const (
	acpModeAgent acp.SessionModeId = "agent"
	acpModePlan  acp.SessionModeId = "plan"
)

// acpModes returns the mode list advertised in NewSession responses.
func acpModes(current acp.SessionModeId) *acp.SessionModeState {
	return &acp.SessionModeState{
		CurrentModeId: current,
		AvailableModes: []acp.SessionMode{
			{Id: acpModeAgent, Name: "Agent", Description: acp.Ptr("Full agent mode with all tools")},
			{Id: acpModePlan, Name: "Plan", Description: acp.Ptr("Read-only planning mode for analysis")},
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

// broadcastSlashCommands sends the available slash commands to the client via
// an available_commands_update session notification.
func (a *acpAgent) broadcastSlashCommands(sessionID acp.SessionId, sess *acpSession) {
	var cmds []acp.AvailableCommand

	// Skill-based slash commands.
	if sess.skillLoader != nil {
		for _, sk := range sess.skillLoader.SlashCommands() {
			name := strings.TrimPrefix(sk.Slash, "/")
			cmd := acp.AvailableCommand{
				Name:        name,
				Description: sk.Description,
			}
			cmd.Input = &acp.AvailableCommandInput{
				Unstructured: &acp.UnstructuredCommandInput{
					Hint: "additional instructions",
				},
			}
			cmds = append(cmds, cmd)
		}
	}

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

// --- acp.Agent interface ---

func (a *acpAgent) Initialize(_ context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	config.Logger().Printf("[acp] Initialize: client=%v, protocol=%d",
		params.ClientInfo, params.ProtocolVersion)

	// Check if the configured model supports image input.
	imageSupport := false
	if cfg, err := config.LoadConfig(); err == nil {
		providerName, modelName := cfg.GetProviderModel()
		registry := internalmodel.NewModelRegistry()
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
				List:  &acp.SessionListCapabilities{},
				Close: &acp.SessionCloseCapabilities{},
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

	a.mu.Lock()
	a.sessions[sessionID] = sess
	a.mu.Unlock()

	config.Logger().Printf("[acp] Session created: %s", sessionID)

	// Broadcast available slash commands for this session.
	a.broadcastSlashCommands(sessionID, sess)

	return acp.NewSessionResponse{
		SessionId: sessionID,
		Modes:     acpModes(acpModeAgent),
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

	providerName, modelName := cfg.GetProviderModel()
	providers := cfg.GetProviders()
	providerCfg := providers[providerName]
	if providerCfg == nil {
		return nil, fmt.Errorf("provider %q not found in config", providerName)
	}

	registry := internalmodel.NewModelRegistry()
	baseURL := providerCfg.BaseURL
	if baseURL == "" {
		baseURL = registry.GetProviderAPI(providerName)
	}

	chatModel, err := internalmodel.NewChatModel(ctx, &internalmodel.ChatModelConfig{
		Model: modelName, APIKey: providerCfg.APIKey, BaseURL: baseURL,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating model: %w", err)
	}

	env := tools.NewEnv(pwd, platform)
	bgManager := tools.NewBackgroundManager(env)

	// Load MCP tools from config.
	var mcpTools []tool.BaseTool
	if len(cfg.MCPServers) > 0 {
		mcpTools, _ = tools.LoadMCPTools(ctx, cfg.MCPServers)
	}

	allTools := []tool.BaseTool{
		env.NewReadTool(), env.NewEditTool(), env.NewWriteTool(),
		env.NewExecuteTool(bgManager), env.NewGrepTool(),
		env.NewTodoWriteTool(), env.NewTodoReadTool(),
		env.NewSwitchEnvTool(),
		env.NewCheckBackgroundTool(bgManager),
	}
	allTools = append(allTools, mcpTools...)

	// Plan mode tools: read-only subset.
	planTools := []tool.BaseTool{
		env.NewReadTool(),
		env.NewExecuteTool(nil),
		env.NewGrepTool(),
		env.NewTodoWriteTool(), env.NewTodoReadTool(),
	}

	normalPrompt := prompts.GetSystemPrompt(platform, pwd, "local", envInfo, skillLoader.Descriptions())
	planPrompt := prompts.GetPlanSystemPrompt(platform, pwd, "local", envInfo)
	approvalState := runner.NewApprovalState(pwd, cfg.AutoApprove)

	// Create ACPHandler
	acpHandler := handler.NewACPHandler(a.conn, sessionID)
	approvalState.SetHandler(acpHandler)

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
	}

	// Langfuse tracer
	var tracer *telemetry.LangfuseTracer
	if cfg.Telemetry != nil && cfg.Telemetry.Langfuse != nil {
		tracer = telemetry.NewLangfuseTracer(cfg.Telemetry.Langfuse)
	}

	// Build agent with middlewares
	contextLimit := registry.GetModelContextLimit(providerName, modelName)
	if contextLimit <= 0 {
		contextLimit = internalmodel.GetModelContextLimit(modelName)
	}
	if contextLimit <= 0 {
		contextLimit = 200000
	}

	var handlers []adk.ChatModelAgentMiddleware

	summMw, err := summarization.New(ctx, &summarization.Config{
		Model: chatModel,
		Trigger: &summarization.TriggerCondition{
			ContextTokens: int(float64(contextLimit) * 0.75),
		},
		TranscriptFilePath: filepath.Join(config.ConfigDir(), "transcript.txt"),
	})
	if err == nil {
		handlers = append(handlers, summMw)
	}

	reductionBackend := &agent.LocalReductionBackend{RootDir: config.ConfigDir()}
	reductionMw, err := reduction.New(ctx, &reduction.Config{
		Backend:           reductionBackend,
		RootDir:           filepath.Join(config.ConfigDir(), "reduction"),
		MaxLengthForTrunc: 50000,
		MaxTokensForClear: int64(float64(contextLimit) * 0.60),
		ReadFileToolName:  "read",
		ToolConfig: map[string]*reduction.ToolReductionConfig{
			"read": {SkipClear: true},
		},
	})
	if err == nil {
		handlers = append(handlers, reductionMw)
	}

	reminderMw := agent.NewReminderMiddleware(agent.ReminderConfig{
		TodoStore:    env.TodoStore,
		EnvLabel:     "local",
		IsRemote:     env.IsRemote(),
		ContextLimit: contextLimit,
	}, nil)
	handlers = append(handlers, reminderMw)

	ag, err := agent.NewAgent(ctx, chatModel, allTools, normalPrompt, approvalState.RequestApproval, nil, handlers)
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
		history:       history,
		mode:          acpModeAgent,
		createAgent:   makeAgent,
		allTools:      allTools,
		planTools:     planTools,
		normalPrompt:  normalPrompt,
		planPrompt:    planPrompt,
		skillLoader:   skillLoader,
	}

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

		// Check if it's a skill slash command.
		if sess.skillLoader != nil {
			if sk := sess.skillLoader.GetBySlash("/" + cmdName); sk != nil {
				userInput := ""
				if len(parts) > 1 {
					userInput = parts[1]
				}
				prompt = fmt.Sprintf("Use the load_skill tool with name=%q and follow its instructions. %s", sk.Name, userInput)
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

	resp := runner.Run(promptCtx, sess.ag, history, sess.h, sess.rec, sess.todoStore, sess.tracer, nil)

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

	newMode := params.ModeId
	if newMode != acpModeAgent && newMode != acpModePlan {
		sess.mu.Unlock()
		return acp.SetSessionModeResponse{}, fmt.Errorf("unknown mode: %s", newMode)
	}
	if newMode == sess.mode {
		sess.mu.Unlock()
		return acp.SetSessionModeResponse{}, nil
	}

	sess.mode = newMode
	if sess.rec != nil {
		sess.rec.RecordModeChange(string(newMode))
	}

	var sysPrompt string
	var toolList []tool.BaseTool
	if newMode == acpModePlan {
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

func (a *acpAgent) ListSessions(_ context.Context, _ acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	config.Logger().Printf("[acp] ListSessions")

	allSessions, err := session.ListAllSessions()
	if err != nil {
		config.Logger().Printf("[acp] ListSessions error: %v", err)
		return acp.ListSessionsResponse{Sessions: []acp.SessionInfo{}}, nil
	}

	var sessions []acp.SessionInfo
	for project, metas := range allSessions {
		for _, m := range metas {
			var title *string
			if m.Title != "" {
				title = &m.Title
			}
			sessions = append(sessions, acp.SessionInfo{
				SessionId: acp.SessionId(fmt.Sprintf("sess_%s", m.UUID)),
				Title:     title,
				Cwd:       project,
			})
		}
	}
	return acp.ListSessionsResponse{Sessions: sessions}, nil
}

func (a *acpAgent) SetSessionConfigOption(_ context.Context, params acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	config.Logger().Printf("[acp] SetSessionConfigOption")
	return acp.SetSessionConfigOptionResponse{}, nil
}

// UnstableCloseSession implements the experimental session/close method.
// It cancels any ongoing work and releases session resources.
func (a *acpAgent) UnstableCloseSession(_ context.Context, params acp.UnstableCloseSessionRequest) (acp.UnstableCloseSessionResponse, error) {
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

	return acp.UnstableCloseSessionResponse{}, nil
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
	history := session.ReconstructHistory(entries)

	sess, err := a.buildAgentSession(ctx, cfg, pwd, params.SessionId, rec, history)
	if err != nil {
		return acp.LoadSessionResponse{}, err
	}

	a.mu.Lock()
	if old, ok := a.sessions[params.SessionId]; ok {
		old.Close()
	}
	a.sessions[params.SessionId] = sess
	a.mu.Unlock()

	config.Logger().Printf("[acp] Session loaded: %s, messages=%d", params.SessionId, len(sess.history))

	// Broadcast available slash commands for the loaded session.
	a.broadcastSlashCommands(params.SessionId, sess)

	return acp.LoadSessionResponse{}, nil
}
