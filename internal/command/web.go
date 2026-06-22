package command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/spf13/cobra"

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
	"github.com/cnjack/jcode/internal/telemetry"
	"github.com/cnjack/jcode/internal/tools"
	"github.com/cnjack/jcode/internal/usage"
	util "github.com/cnjack/jcode/internal/util"
	"github.com/cnjack/jcode/internal/web"
)

// estimateToolTokens approximates a tool's contribution to the context window
// from its serialized schema (name + description + parameters). ToolInfo's
// MarshalJSON includes the JSON-schema params, so one marshal captures it all.
func estimateToolTokens(ctx context.Context, t tool.BaseTool) int {
	if t == nil {
		return 0
	}
	info, err := t.Info(ctx)
	if err != nil || info == nil {
		return 0
	}
	raw, err := json.Marshal(info)
	if err != nil {
		return usage.EstimateBytes(len(info.Name) + len(info.Desc))
	}
	return usage.EstimateBytes(len(raw))
}

func NewWebCmd() *cobra.Command {
	var port int
	var host string
	var openBrowser bool
	cmd := &cobra.Command{
		Use:          "web",
		Short:        "Start the web server",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWebServer(port, host, openBrowser)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "HTTP server port")
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "HTTP server host")
	cmd.Flags().BoolVar(&openBrowser, "open", true, "Open browser after server starts")
	return cmd
}

func runWebServer(port int, host string, openBrowser bool) error {
	// Check if we need setup (no providers configured).
	needsSetup := config.NeedsSetup()

	var cfg *config.Config
	if !needsSetup {
		var err error
		cfg, err = config.LoadConfig()
		if err != nil {
			return fmt.Errorf("config error: %w", err)
		}
	} else {
		// Create a minimal config for setup mode.
		cfg = &config.Config{
			MaxIterations: 1000,
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pwd := util.GetWorkDir()
	platform := util.GetSystemInfo()

	skillLoader := skills.NewLoaderWithDisabled(cfg.DisabledSkills)
	skillLoader.ScanProjectSkills(pwd)

	var providerName, modelName string
	if !needsSetup {
		providerName, modelName = cfg.GetProviderModel()
		providers := cfg.GetProviders()
		providerCfg := providers[providerName]
		if providerCfg == nil {
			return fmt.Errorf("provider %q not found in config", providerName)
		}
	}

	registry := internalmodel.NewModelRegistryWithConfig(cfg)

	// Load MCP tools. mcpToolsPtr is swapped atomically by reloadMCPTools so a new
	// task (built concurrently by buildWebTask) always reads a consistent slice
	// header without a data race on hot-reload.
	var mcpToolsPtr atomic.Pointer[[]tool.BaseTool]
	var initialMCPStatuses []tools.MCPStatus
	if len(cfg.MCPServers) > 0 {
		mt, statuses := tools.LoadMCPTools(ctx, cfg.MCPServers)
		mcpToolsPtr.Store(&mt)
		initialMCPStatuses = statuses
	}
	reloadMCPTools := func(servers map[string]*config.MCPServer) ([]tools.MCPStatus, error) {
		nt, statuses := tools.LoadMCPTools(ctx, servers)
		mcpToolsPtr.Store(&nt)
		return statuses, nil
	}

	startupMode := resolveStartupMode(cfg, false)

	// Langfuse tracer (shared across tasks).
	var langfuseTracer *telemetry.LangfuseTracer
	if cfg.Telemetry != nil && cfg.Telemetry.Langfuse != nil {
		langfuseTracer = telemetry.NewLangfuseTracer(cfg.Telemetry.Langfuse)
	}

	// WeChat client + shared push notifiers (process-level, reused by every task).
	wechatClient := weixin.NewClient()
	if cfg.Channel != nil && cfg.Channel.WebEnabled && wechatClient.State() == channel.StateDisabled {
		if err := wechatClient.Enable(); err != nil {
			config.Logger().Printf("[wechat] web auto-enable failed: %v", err)
		} else {
			config.Logger().Printf("[wechat] web auto-enabled")
		}
	}
	var sharedBLE *ble.Notifier
	if cfg.Channel != nil && cfg.Channel.BLEEnabled {
		sharedBLE = ble.New()
	}

	// makeNotifyingHandler wraps a fresh per-task WebHandler with the shared push
	// notifiers (WeChat + BLE) so a backgrounded task can still surface
	// approval/done/working notifications without stealing UI focus.
	makeNotifyingHandler := func(wh *handler.WebHandler) *handler.NotifyingHandler {
		nh := handler.NewNotifyingHandler(wh, 10*time.Second)
		nh.SetApprovalNotifier(func(toolName, toolArgs string) {
			if wechatClient.State() == channel.StateEnabled {
				if err := wechatClient.SendText(channel.ApprovalMessage(toolName, toolArgs, "Please check the web interface")); err != nil {
					config.Logger().Printf("[wechat] failed to send approval notification: %v", err)
				}
			}
		})
		nh.SetDoneNotifier(func(summary string, err error) {
			if wechatClient.State() == channel.StateEnabled {
				if sendErr := wechatClient.SendText(channel.DoneMessage(summary, err)); sendErr != nil {
					config.Logger().Printf("[wechat] failed to send done notification: %v", sendErr)
				}
			}
		})
		nh.AddNotifier(channel.NewChannelNotifier(wechatClient))
		if sharedBLE != nil {
			nh.AddNotifier(sharedBLE)
		}
		return nh
	}

	// newChatModel resolves a provider/model into a live chat model + context
	// limit. Shared because it has no per-task state — each task gets its own
	// model instance from it.
	newChatModel := func(prov, mod string) (model.ToolCallingChatModel, int, error) {
		currentCfg, err := config.LoadConfig()
		if err != nil {
			return nil, 0, fmt.Errorf("config error: %w", err)
		}
		provCfg := currentCfg.GetProviders()[prov]
		if provCfg == nil {
			return nil, 0, fmt.Errorf("provider %q not configured", prov)
		}
		bURL := provCfg.BaseURL
		if bURL == "" {
			bURL = registry.GetProviderAPI(prov)
		}
		cm, err := internalmodel.NewChatModel(ctx, &internalmodel.ChatModelConfig{
			Model: mod, APIKey: provCfg.APIKey, BaseURL: bURL,
		})
		if err != nil {
			return nil, 0, fmt.Errorf("create model %s/%s: %w", prov, mod, err)
		}
		ctxLimit := internalmodel.ResolveContextLimit(registry, currentCfg, prov, mod)
		return cm, ctxLimit, nil
	}

	// buildWebTask is the per-task engine factory. It produces a fully ISOLATED
	// set of run state — its own env, background manager, recorder, token tracker,
	// approval state, plan store, and event handler — so concurrent tasks never
	// share mutable execution state. exec != nil binds the task to a remote SSH
	// target instead of a local pwd. taskID != "" resumes an existing session.
	buildWebTask := func(taskID, taskPwd, modeStr string, exec tools.RemoteExecutor) (*web.EngineConfig, error) {
		startMode := startupMode
		if modeStr != "" {
			startMode = mode.Parse(modeStr)
		}

		// Fresh execution environment for this task only.
		tenv := tools.NewEnv(taskPwd, platform)
		promptPlatform := platform
		envLabel := "local"
		projectKey := taskPwd
		var taskEnvInfo *util.EnvInfo
		// Per-task skill loader: project skills are scanned into THIS task's loader
		// so concurrent tasks in different projects don't bleed each other's project
		// skills into one shared accumulator. (The process-wide skillLoader stays
		// for the path-agnostic slash/list/toggle management UI.)
		taskLoader := skills.NewLoaderWithDisabled(cfg.DisabledSkills)
		if exec != nil {
			tenv.SetRemote(exec, taskPwd)
			promptPlatform = exec.Platform()
			envLabel = fmt.Sprintf("%s (pwd: %s)", exec.Label(), taskPwd)
			projectKey = exec.ProjectLabel(taskPwd)
		} else {
			taskLoader.ScanProjectSkills(taskPwd)
			taskEnvInfo = util.CollectEnvInfo(taskPwd)
		}

		tbg := tools.NewBackgroundManager(tenv)
		trec, _ := session.NewRecorder(projectKey, providerName, modelName)
		if taskID != "" && trec != nil {
			trec.SetUUID(taskID)
		}
		ttok := &internalmodel.TokenUsage{}
		tplan := tools.NewPlanStore()
		tappr := runner.NewApprovalStateWithMode(taskPwd, startMode)
		twh := handler.NewWebHandler()
		tnotify := makeNotifyingHandler(twh)
		tappr.SetHandler(tnotify)

		// Wire THIS task's todo/goal stores to THIS task's recorder + handler, so
		// todos persist on resume and goal changes reach the task's UI and session
		// file. (Each engine is built by this factory, including the bootstrap, so
		// this replaces the old single bootstrap-only wiring in NewServer.)
		if tenv.TodoStore != nil {
			tenv.TodoStore.OnUpdate = func(items []tools.TodoItem) {
				if trec == nil || !trec.HasRecording() {
					return
				}
				snap := make([]session.TodoSnapshotItem, len(items))
				for i, it := range items {
					snap[i] = session.TodoSnapshotItem{ID: it.ID, Title: it.Title, Status: string(it.Status)}
				}
				trec.RecordTodoSnapshot(snap)
			}
		}
		if tenv.GoalStore != nil {
			tenv.GoalStore.OnUpdate = func(g *tools.Goal) {
				if trec != nil && trec.HasRecording() {
					tools.GoalRecorderHook(trec)(g)
				}
				twh.Emit("goal_update", g)
			}
		}

		// Per-task system/plan prompts (rendered for this task's pwd).
		skillDescs := taskLoader.Descriptions()
		var systemPrompt, planPrompt string
		if exec != nil {
			systemPrompt = prompts.GetSystemPrompt(promptPlatform, taskPwd, envLabel, nil, skillDescs)
			planPrompt = prompts.GetPlanSystemPrompt(promptPlatform, taskPwd, envLabel, nil)
		} else {
			systemPrompt = prompts.GetSystemPrompt(platform, taskPwd, "local", taskEnvInfo, skillDescs)
			planPrompt = prompts.GetPlanSystemPrompt(platform, taskPwd, "local", taskEnvInfo)
		}

		buildAllTools := func(cm model.ToolCallingChatModel) []tool.BaseTool {
			all := []tool.BaseTool{
				tenv.NewReadTool(), tenv.NewEditTool(), tenv.NewWriteTool(),
				tenv.NewExecuteTool(tbg), tenv.NewGrepTool(),
				tenv.NewTodoWriteTool(), tenv.NewTodoReadTool(),
				tenv.NewGoalSetTool(), tenv.NewGoalGetTool(), tenv.NewGoalUpdateTool(),
				tenv.NewSwitchEnvTool(),
				tenv.NewCheckBackgroundTool(tbg),
				tenv.NewSubagentTool(&tools.SubagentDeps{
					ChatModel: cm,
					Recorder:  trec,
					Notifier: func(name, agentType string, done bool, result string, err error) {
						twh.OnSubagentEvent(name, agentType, done, result, err)
					},
					ProgressFn: func(agentName, event, toolName, detail string) {
						twh.OnSubagentProgress(agentName, event, toolName, detail)
					},
				}),
				tools.NewAskUserTool(&tools.AskUserDeps{
					BatchRequestFn: twh.RequestAskUser,
				}),
				skills.NewLoadSkillTool(taskLoader),
			}
			if mt := mcpToolsPtr.Load(); mt != nil {
				all = append(all, (*mt)...)
			}
			return all
		}

		buildPlanTools := func() []tool.BaseTool {
			return []tool.BaseTool{
				tenv.NewReadTool(),
				tenv.NewExecuteTool(nil),
				tenv.NewGrepTool(),
				tenv.NewTodoWriteTool(), tenv.NewTodoReadTool(),
				tools.NewAskUserTool(&tools.AskUserDeps{
					BatchRequestFn: twh.RequestAskUser,
				}),
			}
		}

		// Per-task compaction paths — transcript + reduction must be task-scoped or
		// concurrent summarization across tasks would corrupt a shared file.
		taskUUID := "task"
		if trec != nil {
			taskUUID = trec.UUID()
		}
		transcriptPath := filepath.Join(config.ConfigDir(), "transcripts", taskUUID+".txt")
		reductionRoot := filepath.Join(config.ConfigDir(), "reduction", taskUUID)
		_ = os.MkdirAll(filepath.Dir(transcriptPath), 0o755)
		_ = os.MkdirAll(reductionRoot, 0o755)

		makeAgent := func(cm model.ToolCallingChatModel, ctxLimit int, planMode bool) (*adk.ChatModelAgent, error) {
			var middlewares []adk.AgentMiddleware //nolint:staticcheck // langfuseTracer.AgentMiddleware()/agent.NewAgent still use the deprecated type
			if langfuseTracer != nil {
				middlewares = append(middlewares, langfuseTracer.AgentMiddleware())
			}

			var handlers []adk.ChatModelAgentMiddleware

			compactThreshold := cfg.CompactionThreshold()
			reductionThreshold := compactThreshold - 0.15
			if reductionThreshold < 0.1 {
				reductionThreshold = compactThreshold * 0.8
			}

			summMw, err := summarization.New(ctx, &summarization.Config{
				Model: cm,
				Trigger: &summarization.TriggerCondition{
					ContextTokens: int(float64(ctxLimit) * compactThreshold),
				},
				TranscriptFilePath: transcriptPath,
			})
			if err == nil {
				handlers = append(handlers, summMw)
			}

			reductionBackend := &agent.LocalReductionBackend{RootDir: reductionRoot}
			reductionMw, err := reduction.New(ctx, &reduction.Config{
				Backend:           reductionBackend,
				RootDir:           reductionRoot,
				MaxLengthForTrunc: 50000,
				MaxTokensForClear: int64(float64(ctxLimit) * reductionThreshold),
				ReadFileToolName:  "read",
				ToolConfig: map[string]*reduction.ToolReductionConfig{
					"read": {SkipClear: true},
				},
			})
			if err == nil {
				handlers = append(handlers, reductionMw)
			}

			reminderMw := agent.NewReminderMiddleware(agent.ReminderConfig{
				TodoStore:    tenv.TodoStore,
				GoalStore:    tenv.GoalStore,
				PlanStore:    tplan,
				EnvLabel:     "local",
				IsRemote:     tenv.IsRemote(),
				ContextLimit: ctxLimit,
			}, ttok)
			handlers = append(handlers, reminderMw)

			prompt := systemPrompt
			toolList := buildAllTools(cm)
			if planMode {
				prompt = planPrompt
				toolList = buildPlanTools()
			}
			return agent.NewAgent(ctx, cm, toolList, prompt, tappr.RequestApproval, middlewares, handlers)
		}

		// Per-task chat-model cache so a model/mode switch rebuilds only this task.
		// cmMu serializes the cache against breakdownFn (a GET handler) reading it.
		var cmMu sync.Mutex
		var currentCM model.ToolCallingChatModel
		var currentCtxLimit int
		currentPlanMode := startMode.IsPlan()

		createAgent := func(prov, mod string) (*adk.ChatModelAgent, error) {
			cm, ctxLimit, err := newChatModel(prov, mod)
			if err != nil {
				return nil, err
			}
			cmMu.Lock()
			plan := currentPlanMode
			cmMu.Unlock()
			ag, err := makeAgent(cm, ctxLimit, plan)
			if err != nil {
				return nil, err // don't poison the cache with a model whose agent failed to build
			}
			cmMu.Lock()
			currentCM = cm
			currentCtxLimit = ctxLimit
			cmMu.Unlock()
			return ag, nil
		}

		rebuildForMode := func(planMode bool) (*adk.ChatModelAgent, error) {
			cmMu.Lock()
			currentPlanMode = planMode
			cm, ctxLimit := currentCM, currentCtxLimit
			cmMu.Unlock()
			if cm == nil {
				return createAgent(providerName, modelName)
			}
			return makeAgent(cm, ctxLimit, planMode)
		}

		breakdownFn := func() usage.ContextBreakdown {
			var b usage.ContextBreakdown
			skillDesc := taskLoader.Descriptions()
			b.SkillsTokens = usage.Estimate(skillDesc)
			b.SystemPromptTokens = usage.Estimate(systemPrompt) - b.SkillsTokens
			if b.SystemPromptTokens < 0 {
				b.SystemPromptTokens = 0
			}
			if mt := mcpToolsPtr.Load(); mt != nil {
				for _, t := range *mt {
					b.MCPToolsTokens += estimateToolTokens(ctx, t)
				}
			}
			cmMu.Lock()
			cm := currentCM
			cmMu.Unlock()
			if cm != nil {
				total := 0
				for _, at := range buildAllTools(cm) {
					total += estimateToolTokens(ctx, at)
				}
				b.SystemToolsTokens = total - b.MCPToolsTokens
				if b.SystemToolsTokens < 0 {
					b.SystemToolsTokens = 0
				}
			}
			return b
		}

		var ag *adk.ChatModelAgent
		if !needsSetup {
			var err error
			ag, err = createAgent(providerName, modelName)
			if err != nil {
				return nil, fmt.Errorf("error creating agent: %w", err)
			}
		}

		return &web.EngineConfig{
			TaskID:         taskID,
			Pwd:            taskPwd,
			Mode:           startMode.String(),
			ProviderName:   providerName,
			ModelName:      modelName,
			Agent:          ag,
			Env:            tenv,
			TodoStore:      tenv.TodoStore,
			Recorder:       trec,
			TokenUsage:     ttok,
			ApprovalState:  tappr,
			Handler:        twh,
			EventHandler:   tnotify,
			BreakdownFn:    breakdownFn,
			CreateAgent:    createAgent,
			RebuildForMode: rebuildForMode,
		}, nil
	}

	// Bootstrap engine for the initial task.
	bootEC, err := buildWebTask("", pwd, startupMode.String(), nil)
	if err != nil {
		return err
	}
	bootNotifying, _ := bootEC.EventHandler.(*handler.NotifyingHandler)

	srv := web.NewServer(&web.ServerConfig{
		Port:           port,
		Host:           host,
		OpenBrowser:    openBrowser,
		Pwd:            pwd,
		Version:        Version,
		Agent:          bootEC.Agent,
		CreateAgent:    bootEC.CreateAgent,
		RebuildForMode: bootEC.RebuildForMode,
		NewEngine: func(taskID, taskPwd, modeStr string) (*web.EngineConfig, error) {
			return buildWebTask(taskID, taskPwd, modeStr, nil)
		},
		NewRemoteEngine: func(taskID string, exec tools.RemoteExecutor, remotePwd, modeStr string) (*web.EngineConfig, error) {
			return buildWebTask(taskID, remotePwd, modeStr, exec)
		},
		InitialMode:        startupMode.String(),
		TodoStore:          bootEC.TodoStore,
		Recorder:           bootEC.Recorder,
		Tracer:             langfuseTracer,
		Env:                bootEC.Env,
		ProviderName:       providerName,
		ModelName:          modelName,
		Config:             cfg,
		Registry:           registry,
		ApprovalState:      bootEC.ApprovalState,
		SkillLoader:        skillLoader,
		ReloadMCP:          reloadMCPTools,
		InitialMCPStatuses: initialMCPStatuses,
		WechatClient:       wechatClient,
		WebHandler:         bootEC.Handler,
		EventHandler:       bootEC.EventHandler,
		NeedsSetup:         needsSetup,
		TokenUsage:         bootEC.TokenUsage,
		ContextBreakdownFn: bootEC.BreakdownFn,
	})

	// Set up inbound WeChat message handler now that srv exists. Always register
	// regardless of WebEnabled — the user can enable via the UI. Inbound messages
	// target the active task (no task_id channel).
	wechatClient.SetOnMessage(func(from, text string) {
		if wechatClient.State() != channel.StateEnabled {
			return // channel disabled, silently ignore
		}
		config.Logger().Printf("[wechat] inbound message from %s: %s", from, text)
		if !srv.SubmitMessage(text, "wechat") {
			// Agent is busy, let the user know.
			_ = wechatClient.SendText(channel.BusyMessage())
		}
	})

	// Clean up WeChat + shared notifiers on shutdown.
	defer func() {
		if bootNotifying != nil {
			bootNotifying.CloseNotifiers()
		}
		if wechatClient.State() == channel.StateEnabled {
			// Best-effort, don't block shutdown
			go func() { _ = wechatClient.SendText(channel.GoodbyeMessage(time.Now())) }()
			time.Sleep(500 * time.Millisecond)
			_ = wechatClient.Disable()
		}
	}()

	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	srv.CloseAllEngines()
	if langfuseTracer != nil {
		langfuseTracer.Flush()
	}
	return nil
}
