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
	config.Logger().Printf("[acp] ACP connection closed")
}

// --- acp.Agent interface ---

func (a *acpAgent) Initialize(_ context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	config.Logger().Printf("[acp] Initialize: client=%v, protocol=%d",
		params.ClientInfo, params.ProtocolVersion)
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{
			PromptCapabilities: acp.PromptCapabilities{
				EmbeddedContext: true,
			},
			LoadSession: true,
		},
		AgentInfo: &acp.Implementation{
			Name:    "jcode",
			Title:   acp.Ptr("Little Jack — Coding Assistant"),
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
	return acp.NewSessionResponse{SessionId: sessionID}, nil
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

	systemPrompt := prompts.GetSystemPrompt(platform, pwd, "local", envInfo, skillLoader.Descriptions())
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

	ag, err := agent.NewAgent(ctx, chatModel, allTools, systemPrompt, approvalState.RequestApproval, nil, handlers)
	if err != nil {
		return nil, fmt.Errorf("error creating agent: %w", err)
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

	// Extract text from prompt content blocks.
	var userText strings.Builder
	for _, block := range params.Prompt {
		if block.Text != nil {
			userText.WriteString(block.Text.Text)
		}
		if block.ResourceLink != nil {
			fmt.Fprintf(&userText, "\n[Resource: %s (%s)]", block.ResourceLink.Name, block.ResourceLink.Uri)
		}
	}

	prompt := userText.String()
	if prompt == "" {
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}

	sess.mu.Lock()
	if sess.rec != nil {
		sess.rec.RecordUser(prompt)
	}
	sess.history = append(sess.history, schema.UserMessage(prompt))

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
	return acp.SetSessionModeResponse{}, nil
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
	a.sessions[params.SessionId] = sess
	a.mu.Unlock()

	config.Logger().Printf("[acp] Session loaded: %s, messages=%d", params.SessionId, len(sess.history))
	return acp.LoadSessionResponse{}, nil
}
