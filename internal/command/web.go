package command

import (
	"context"
	"fmt"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/tool"
	"github.com/spf13/cobra"

	"github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
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
	cmd := &cobra.Command{
		Use:          "web",
		Short:        "Start the web server",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWebServer(port, host)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "HTTP server port")
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "HTTP server host")
	return cmd
}

func runWebServer(port int, host string) error {

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pwd := util.GetWorkDir()
	platform := util.GetSystemInfo()
	envInfo := util.CollectEnvInfo(pwd)

	skillLoader := skills.NewLoader()
	skillLoader.ScanProjectSkills(pwd)

	systemPrompt := prompts.GetSystemPrompt(platform, pwd, "local", envInfo, skillLoader.Descriptions())
	providerName, modelName := cfg.GetProviderModel()

	providers := cfg.GetProviders()
	providerCfg := providers[providerName]
	if providerCfg == nil {
		return fmt.Errorf("provider %q not found in config", providerName)
	}

	registry := internalmodel.NewModelRegistry()
	baseURL := providerCfg.BaseURL
	if baseURL == "" {
		baseURL = registry.GetProviderAPI(providerName)
	}

	// Verify the default model can be created.
	if _, err := internalmodel.NewChatModel(ctx, &internalmodel.ChatModelConfig{
		Model: modelName, APIKey: providerCfg.APIKey, BaseURL: baseURL,
	}); err != nil {
		return fmt.Errorf("error creating model: %w", err)
	}

	env := tools.NewEnv(pwd, platform)
	bgManager := tools.NewBackgroundManager(env)
	rec, _ := session.NewRecorder(pwd, providerName, modelName)

	// Wire TodoStore → session recording.
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

	// Load MCP tools.
	var mcpTools []tool.BaseTool
	if len(cfg.MCPServers) > 0 {
		mcpTools, _ = tools.LoadMCPTools(ctx, cfg.MCPServers)
	}

	planStore := tools.NewPlanStore()

	approvalState := runner.NewApprovalState(pwd, cfg.AutoApprove)

	// Langfuse tracer.
	var langfuseTracer *telemetry.LangfuseTracer
	if cfg.Telemetry != nil && cfg.Telemetry.Langfuse != nil {
		langfuseTracer = telemetry.NewLangfuseTracer(cfg.Telemetry.Langfuse)
	}

	buildAllTools := func() []tool.BaseTool {
		all := []tool.BaseTool{
			env.NewReadTool(), env.NewEditTool(), env.NewWriteTool(),
			env.NewExecuteTool(bgManager), env.NewGrepTool(),
			env.NewTodoWriteTool(), env.NewTodoReadTool(),
			env.NewSwitchEnvTool(),
			env.NewCheckBackgroundTool(bgManager),
			tools.NewAskUserTool(&tools.AskUserDeps{
				ResponseCh: make(chan tools.AskUserResponse, 1),
			}),
			skills.NewLoadSkillTool(skillLoader),
		}
		return append(all, mcpTools...)
	}

	toolList := buildAllTools()

	createAgent := func(prov, mod string) (*adk.ChatModelAgent, error) {
		// Resolve provider config.
		provCfg := providers[prov]
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
		})
		handlers = append(handlers, reminderMw)

		return agent.NewAgent(ctx, cm, toolList, systemPrompt, approvalState.RequestApproval, middlewares, handlers)
	}

	ag, err := createAgent(providerName, modelName)
	if err != nil {
		return fmt.Errorf("error creating agent: %w", err)
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
		Pwd:           pwd,
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
	})

	// Set handler for approval routing.
	approvalState.SetHandler(srv.Handler())

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
