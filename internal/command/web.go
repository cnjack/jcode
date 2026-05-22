package command

import (
	"context"
	"fmt"
	"os/signal"
	"path/filepath"
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
	internalmodel "github.com/cnjack/jcode/internal/model"
	weixin "github.com/cnjack/jcode/internal/pkg/weixin"
	"github.com/cnjack/jcode/internal/prompts"
	"github.com/cnjack/jcode/internal/runner"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/skills"
	"github.com/cnjack/jcode/internal/telemetry"
	"github.com/cnjack/jcode/internal/tools"
	util "github.com/cnjack/jcode/internal/util"
	"github.com/cnjack/jcode/internal/web"
)

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
	envInfo := util.CollectEnvInfo(pwd)

	skillLoader := skills.NewLoader()
	skillLoader.ScanProjectSkills(pwd)

	systemPrompt := prompts.GetSystemPrompt(platform, pwd, "local", envInfo, skillLoader.Descriptions())

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

	env := tools.NewEnv(pwd, platform)
	bgManager := tools.NewBackgroundManager(env)
	rec, _ := session.NewRecorder(pwd, providerName, modelName)

	// Load MCP tools.
	var mcpTools []tool.BaseTool
	if len(cfg.MCPServers) > 0 {
		mcpTools, _ = tools.LoadMCPTools(ctx, cfg.MCPServers)
	}

	planStore := tools.NewPlanStore()

	approvalState := runner.NewApprovalState(pwd, cfg.AutoApprove)

	// Create WebHandler early so subagent tool can emit events through it.
	webHandler := handler.NewWebHandler()

	// Wrap handler with NotifyingHandler for WeChat push notifications.
	// Callbacks check wechatClient.State() before sending, so this is safe
	// even when the channel is disabled or not yet configured.
	var finalHandler handler.AgentEventHandler
	wechatClient := weixin.NewClient()

	// Auto-enable if credentials exist and channel.web_enabled is true.
	if cfg.Channel != nil && cfg.Channel.WebEnabled && wechatClient.State() == channel.StateDisabled {
		if err := wechatClient.Enable(); err != nil {
			config.Logger().Printf("[wechat] web auto-enable failed: %v", err)
		} else {
			config.Logger().Printf("[wechat] web auto-enabled")
		}
	}

	// Always wrap with NotifyingHandler — the user can enable via the UI toggle.
	notifyingH := handler.NewNotifyingHandler(webHandler, 10*time.Second)
	notifyingH.SetApprovalNotifier(func(toolName, toolArgs string) {
		if wechatClient.State() == channel.StateEnabled {
			if err := wechatClient.SendText(channel.ApprovalMessage(toolName, toolArgs, "Please check the web interface")); err != nil {
				config.Logger().Printf("[wechat] failed to send approval notification: %v", err)
			}
		}
	})
	notifyingH.SetDoneNotifier(func(summary string, err error) {
		if wechatClient.State() == channel.StateEnabled {
			if sendErr := wechatClient.SendText(channel.DoneMessage(summary, err)); sendErr != nil {
				config.Logger().Printf("[wechat] failed to send done notification: %v", sendErr)
			}
		}
	})

	// Register WeChat as a notifier for working/idle status pushes.
	notifyingH.AddNotifier(channel.NewChannelNotifier(wechatClient))

	// Register BLE notifier if enabled (lazy connect — will auto-discover JCODE-* devices).
	if cfg.Channel != nil && cfg.Channel.BLEEnabled {
		bleNotifier := ble.New()
		notifyingH.AddNotifier(bleNotifier)
	}

	finalHandler = notifyingH

	// Langfuse tracer.
	var langfuseTracer *telemetry.LangfuseTracer
	if cfg.Telemetry != nil && cfg.Telemetry.Langfuse != nil {
		langfuseTracer = telemetry.NewLangfuseTracer(cfg.Telemetry.Langfuse)
	}

	buildAllTools := func(cm model.ToolCallingChatModel) []tool.BaseTool {
		all := []tool.BaseTool{
			env.NewReadTool(), env.NewEditTool(), env.NewWriteTool(),
			env.NewExecuteTool(bgManager), env.NewGrepTool(),
			env.NewTodoWriteTool(), env.NewTodoReadTool(),
			env.NewSwitchEnvTool(),
			env.NewCheckBackgroundTool(bgManager),
			env.NewSubagentTool(&tools.SubagentDeps{
				ChatModel: cm,
				Recorder:  rec,
				Notifier: func(name, agentType string, done bool, result string, err error) {
					webHandler.OnSubagentEvent(name, agentType, done, result, err)
				},
				ProgressFn: func(agentName, event, toolName, detail string) {
					webHandler.OnSubagentProgress(agentName, event, toolName, detail)
				},
			}),
			tools.NewAskUserTool(&tools.AskUserDeps{
				ResponseCh: make(chan tools.AskUserResponse, 1),
			}),
			skills.NewLoadSkillTool(skillLoader),
		}
		return append(all, mcpTools...)
	}

	createAgent := func(prov, mod string) (*adk.ChatModelAgent, error) {
		// Resolve provider config.
		// Reload config to pick up any new providers added via setup.
		currentCfg, err := config.LoadConfig()
		if err != nil {
			return nil, fmt.Errorf("config error: %w", err)
		}
		provCfg := currentCfg.GetProviders()[prov]
		if provCfg == nil {
			return nil, fmt.Errorf("provider %q not configured", prov)
		}
		bURL := provCfg.BaseURL
		if bURL == "" {
			bURL = registry.GetProviderAPI(prov)
		}
		cm, err := internalmodel.NewChatModel(ctx, &internalmodel.ChatModelConfig{
			Model: mod, APIKey: provCfg.APIKey, BaseURL: bURL,
		})
		if err != nil {
			return nil, fmt.Errorf("create model %s/%s: %w", prov, mod, err)
		}

		ctxLimit := registry.GetModelContextLimit(prov, mod)
		if ctxLimit <= 0 {
			ctxLimit = internalmodel.GetModelContextLimit(mod)
		}
		if ctxLimit <= 0 {
			ctxLimit = 200000
		}

		var middlewares []adk.AgentMiddleware
		if langfuseTracer != nil {
			middlewares = append(middlewares, langfuseTracer.AgentMiddleware())
		}

		var handlers []adk.ChatModelAgentMiddleware

		summMw, err := summarization.New(ctx, &summarization.Config{
			Model: cm,
			Trigger: &summarization.TriggerCondition{
				ContextTokens: int(float64(ctxLimit) * 0.75),
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
			MaxTokensForClear: int64(float64(ctxLimit) * 0.60),
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
			PlanStore:    planStore,
			EnvLabel:     "local",
			IsRemote:     env.IsRemote(),
			ContextLimit: ctxLimit,
		}, nil)
		handlers = append(handlers, reminderMw)

		tools := buildAllTools(cm)
		return agent.NewAgent(ctx, cm, tools, systemPrompt, approvalState.RequestApproval, middlewares, handlers)
	}

	var ag *adk.ChatModelAgent
	var agentErr error
	if !needsSetup {
		ag, agentErr = createAgent(providerName, modelName)
		if agentErr != nil {
			return fmt.Errorf("error creating agent: %w", agentErr)
		}
	}

	switchProject := func(newPwd string) (*adk.ChatModelAgent, *session.Recorder, error) {
		// 1. Update env working directory (all tools share the same *Env).
		env.ResetToLocal(newPwd, platform)

		// 2. Update approval state workpath.
		approvalState.SetWorkpath(newPwd)

		// 3. Re-scan project skills from the new directory.
		skillLoader.ScanProjectSkills(newPwd)

		// 4. Re-render system prompt with the new pwd context.
		//    Since createAgent closure captures the `systemPrompt` variable,
		//    updating it here means createAgent will use the new value.
		envInfo = util.CollectEnvInfo(newPwd)
		systemPrompt = prompts.GetSystemPrompt(platform, newPwd, "local", envInfo, skillLoader.Descriptions())

		// 5. Update the outer pwd variable (captured by createAgent closure's env).
		pwd = newPwd

		// 6. Close old recorder, create new one scoped to the new project.
		if rec != nil {
			rec.Close()
		}
		newRec, _ := session.NewRecorder(newPwd, providerName, modelName)
		rec = newRec

		// 7. Rebuild the agent with updated prompt.
		newAg, err := createAgent(providerName, modelName)
		if err != nil {
			return nil, nil, err
		}

		return newAg, newRec, nil
	}

	srv := web.NewServer(&web.ServerConfig{
		Port:          port,
		Host:          host,
		OpenBrowser:   openBrowser,
		Pwd:           pwd,
		Version:       Version,
		Agent:         ag,
		CreateAgent:   createAgent,
		SwitchProject: switchProject,
		TodoStore:     env.TodoStore,
		Recorder:      rec,
		Tracer:        langfuseTracer,
		Env:           env,
		ProviderName:  providerName,
		ModelName:     modelName,
		Config:        cfg,
		Registry:      registry,
		ApprovalState: approvalState,
		SkillLoader:   skillLoader,
		WechatClient:  wechatClient,
		WebHandler:    webHandler,
		EventHandler:  finalHandler,
		NeedsSetup:    needsSetup,
	})

	// Set handler for approval routing.
	// If WeChat channel wraps the handler, use the wrapping handler for notifications.
	approvalState.SetHandler(finalHandler)

	// Set up inbound WeChat message handler now that srv exists.
	// Always register regardless of WebEnabled — the user can enable via the UI.
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

	// Clean up WeChat on shutdown.
	defer func() {
		// Close all notifiers (BLE, etc.)
		notifyingH.CloseNotifiers()

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

	if rec != nil {
		rec.Close()
	}
	if langfuseTracer != nil {
		langfuseTracer.Flush()
	}
	return nil
}

// Ensure unused imports are used (some may be used only indirectly).
