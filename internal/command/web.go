package command

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	"github.com/cnjack/jcode/internal/automation"
	"github.com/cnjack/jcode/internal/browser"
	"github.com/cnjack/jcode/internal/channel"
	"github.com/cnjack/jcode/internal/channel/ble"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/feature"
	"github.com/cnjack/jcode/internal/flow"
	"github.com/cnjack/jcode/internal/handler"
	mempipeline "github.com/cnjack/jcode/internal/memory/pipeline"
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
	var authToken string
	cmd := &cobra.Command{
		Use:          "web",
		Short:        "Start the web server",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWebServer(cmd.Context(), port, host, openBrowser, authToken)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "HTTP server port")
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "HTTP server host")
	cmd.Flags().BoolVar(&openBrowser, "open", true, "Open browser after server starts")
	cmd.Flags().StringVar(&authToken, "auth-token", "", "Bearer token required when bound to a non-loopback host (auto-generated if empty). Can also be set via JCODE_WEB_TOKEN.")
	return cmd
}

// interactiveToolNames are the tools that require a live human to answer — they
// cannot run unattended. Automation runs (scheduled, and manual runs that may be
// headless) drop them via dropInteractiveTools so an agent calling ask_user in a
// run with no watching client can't block on the WS channel forever, stalling
// the run until the liveness ceiling cancels it.
var interactiveToolNames = map[string]struct{}{"ask_user": {}}

// dropInteractiveTools returns tools minus any whose name is in
// interactiveToolNames. Tools whose Info() can't be read are kept (best-effort).
func dropInteractiveTools(tools []tool.BaseTool) []tool.BaseTool {
	out := make([]tool.BaseTool, 0, len(tools))
	for _, t := range tools {
		if info, err := t.Info(context.Background()); err == nil {
			if _, drop := interactiveToolNames[info.Name]; drop {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// browserSitePreapproved reports whether an origin is pre-authorized for a
// browser action class ("navigate"/"interact") via config.browser.approval
// defaults or a per-site override. Empty origin never pre-approves.
func browserSitePreapproved(cfg *config.Config, origin, class string) bool {
	if cfg == nil || cfg.Browser == nil || origin == "" {
		return false
	}
	bc := cfg.Browser
	// Per-site override wins over the class default.
	for _, sp := range bc.SitePermissions {
		if sp.Origin != origin {
			continue
		}
		val := sp.Navigate
		if class == "interact" {
			val = sp.Interact
		}
		return val == "allow"
	}
	if bc.Approval != nil && bc.Approval[class] == "always_allow" {
		return true
	}
	return false
}

// resolveWebToken decides the web auth token and whether auth must be enforced.
//
// Auth is required when the bind host is non-loopback (exposed to the network),
// or when a token was explicitly supplied. Token source priority:
//  1. --auth-token flag / JCODE_WEB_TOKEN env — session-scoped, never written to disk
//  2. ~/.jcode/web_token — persisted (0600), reused across restarts
//  3. auto-generated (32 random bytes, base64url) when exposed and none of the
//     above; persisted to ~/.jcode/web_token so the token is stable across restarts
func resolveWebToken(host, flagToken string) (token string, requireAuth bool, err error) {
	explicit := flagToken
	if explicit == "" {
		explicit = os.Getenv("JCODE_WEB_TOKEN")
	}
	// Explicit token (flag/env): enforce auth, never touch disk (session-scoped).
	if explicit != "" {
		if !web.IsValidWSSubprotocolToken(explicit) {
			return "", true, fmt.Errorf("auth token must be printable ASCII with no spaces or separators (it is sent as a WebSocket subprotocol)")
		}
		return explicit, true, nil
	}
	// Loopback bind with no explicit token: keep the existing no-auth behaviour.
	if web.IsLoopbackBind(host) {
		return "", false, nil
	}
	// Exposed bind, no explicit token: reuse a persisted token or generate one.
	dir := config.ConfigDir()
	path := filepath.Join(dir, "web_token")
	if b, rerr := os.ReadFile(path); rerr == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, true, nil
		}
	}
	gen, gerr := generateWebToken()
	if gerr != nil {
		return "", true, fmt.Errorf("generate web token: %w", gerr)
	}
	// Ensure ~/.jcode exists before writing, otherwise a first remote start would
	// fail with ENOENT and silently fall back to a session-scoped token.
	if merr := os.MkdirAll(dir, 0o700); merr != nil {
		config.Logger().Printf("[web] could not create config dir %s: %v", dir, merr)
	} else if werr := os.WriteFile(path, []byte(gen), 0o600); werr != nil {
		// Non-fatal: fall back to a session-scoped token (auth still enforced).
		config.Logger().Printf("[web] could not persist web token to %s: %v", path, werr)
	}
	return gen, true, nil
}

// generateWebToken returns 32 cryptographically-random bytes as a URL-safe
// base64 string (no padding).
func generateWebToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func runWebServer(parent context.Context, port int, host string, openBrowser bool, authToken string) error {
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

	ctx, cancel := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pwd := util.GetWorkDir()
	platform := util.GetSystemInfo()

	skillLoader := skills.NewLoaderWithDisabled(cfg.DisabledSkills)
	skillLoader.ScanProjectSkills(pwd)

	flowLoader := flow.NewLoader()
	flowLoader.LoadProject(pwd)

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

	// MCP tools are loaded asynchronously after the web server starts listening.
	// A slow remote MCP server must not block /api/health and make desktop launch
	// look hung. mcpToolsPtr is swapped atomically by reloadMCPTools so a new task
	// (built concurrently by buildWebTask) always reads a consistent slice header
	// without a data race on hot-reload.
	var mcpToolsPtr atomic.Pointer[[]tool.BaseTool]
	reloadMCPTools := func(servers map[string]*config.MCPServer) ([]tools.MCPStatus, error) {
		nt, statuses := tools.LoadMCPTools(ctx, servers)
		mcpToolsPtr.Store(&nt)
		return statuses, nil
	}

	startupMode := resolveStartupMode(cfg, false)

	// Publish the developer toggles (Settings → Developer) before the first
	// logger / tracer is built. Both take effect on this startup; runtime
	// toggling is not supported and the UI marks them as "restart required".
	config.SetLoggingEnabled(config.LoggingEnabled(cfg))

	// Langfuse tracer (shared across tasks). Respects Settings → Developer →
	// Enable Langfuse tracing; an absent block keeps the historical behavior.
	var langfuseTracer *telemetry.LangfuseTracer
	if config.TracingEnabled(cfg) && cfg.Telemetry != nil && cfg.Telemetry.Langfuse != nil {
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
	// BLE is a desktop-only feature (compiled out of plain `jcode web`). The proxy
	// is added to every task's notifier chain; enabling/disabling it via the
	// settings toggle takes effect live (no restart). It stays a no-op until
	// Enable() is called — at startup here if configured, and on the toggle.
	bleProxy := &ble.Proxy{}
	if feature.BLE && cfg.Channel != nil && cfg.Channel.BLEEnabled {
		bleProxy.Enable()
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
		if feature.BLE {
			nh.AddNotifier(bleProxy)
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
		// Apply a per-model reasoning-effort override (set from the chat picker)
		// over the provider-level default before constructing the model.
		pcEffort := config.ResolveEffort(prov, mod, provCfg.ReasoningEffort)
		effortCfg := *provCfg
		effortCfg.ReasoningEffort = pcEffort
		cm, err := internalmodel.NewChatModelFromProvider(ctx, prov, mod, bURL, &effortCfg)
		if err != nil {
			return nil, 0, fmt.Errorf("create model %s/%s: %w", prov, mod, err)
		}
		ctxLimit := internalmodel.ResolveContextLimit(registry, currentCfg, prov, mod)
		return cm, ctxLimit, nil
	}

	// Browser-use manager (extension bridge + managed Chrome), process-wide and
	// shared with every per-task Env so the settings UI and the agent's browser_*
	// tools operate the same Chrome. Created regardless of needsSetup so the
	// settings page works before providers are configured.
	browserMgr := browser.NewManager(browser.FromConfig(cfg.Browser))

	// Computer-use manager (native desktop app control), process-wide like the
	// browser one so the settings UI and the agent's computer_* tools share one
	// backend and one view of what is granted. Off unless config enables it.
	computerMgr := newComputerManager(cfg, "")
	if computerMgr != nil {
		defer func() { _ = computerMgr.Close() }()
	}

	// Automation store (definitions + scheduler state). Skipped in setup mode.
	// Created before buildWebTask so every per-task Env shares this one live
	// store — the automation_create tool must write through it (not a throwaway)
	// so created automations are visible to the REST API and scheduler.
	var autoStore *automation.Store
	if !needsSetup {
		var aerr error
		if autoStore, aerr = automation.NewStore(); aerr != nil {
			config.Logger().Printf("[automation] store unavailable: %v", aerr)
			autoStore = nil
		}
	}

	// buildWebTask is the per-task engine factory. It produces a fully ISOLATED
	// set of run state — its own env, background manager, recorder, token tracker,
	// approval state, plan store, and event handler — so concurrent tasks never
	// share mutable execution state. exec != nil binds the task to a remote SSH
	// target instead of a local pwd. taskID != "" resumes an existing session.
	// interactiveTools are the tool names that require a live human to answer —
	// they cannot run unattended, so automation runs (scheduled, and manual runs
	// that may be headless) exclude them. An agent in an automation run that calls
	// ask_user would otherwise block on the WS channel forever (no client resolves
	// it) and stall the run until the liveness ceiling cancels it.
	buildWebTask := func(taskID, taskPwd, modeStr string, exec tools.RemoteExecutor, excludeInteractive bool) (*web.EngineConfig, error) {
		// Per-task config snapshot, so a live task is insulated from mid-run
		// edits (the shared copy in internal/web is guarded by cfgMu). In setup
		// mode there is no valid config on disk yet — LoadConfig always fails —
		// so fall back to the minimal startup config. Setup mode exists to serve
		// the settings UI so the user can finish setup; hard-failing here aborts
		// the bootstrap engine before the server ever listens, making first-run
		// setup unreachable.
		taskCfg, err := config.LoadConfig()
		if err != nil {
			if !needsSetup {
				return nil, fmt.Errorf("load task config: %w", err)
			}
			taskCfg = cfg
		}
		startMode := startupMode
		if modeStr != "" {
			startMode = mode.Parse(modeStr)
		}

		// Fresh execution environment for this task only.
		tenv := tools.NewEnv(taskPwd, platform)
		tenv.AutomationStore = autoStore
		tenv.Browser = browserMgr
		tenv.Computer = computerMgr
		promptPlatform := platform
		envLabel := "local"
		projectKey := taskPwd
		var taskEnvInfo *util.EnvInfo
		// Per-task skill loader: project skills are scanned into THIS task's loader
		// so concurrent tasks in different projects don't bleed each other's project
		// skills into one shared accumulator. (The process-wide skillLoader stays
		// for the path-agnostic slash/list/toggle management UI.)
		taskLoader := skills.NewLoaderWithDisabled(taskCfg.DisabledSkills)
		if exec != nil {
			tenv.SetRemote(exec, taskPwd)
			promptPlatform = exec.Platform()
			envLabel = fmt.Sprintf("%s (pwd: %s)", exec.Label(), taskPwd)
			projectKey = exec.ProjectLabel(taskPwd)
		} else {
			taskLoader.ScanProjectSkills(taskPwd)
			taskEnvInfo = util.CollectEnvInfo(taskPwd)
		}

		// Per-task flow loader (builtin + user + this task's project workflows),
		// shared with the workflow_run tool so slash triggers and inline runs
		// resolve the same set. Project workflows only apply to a local exec.
		taskFlowLoader := flow.NewLoader()
		if exec == nil {
			taskFlowLoader.LoadProject(taskPwd)
		}

		tbg := tools.NewBackgroundManager(tenv)
		trec, _ := session.NewRecorder(projectKey, providerName, modelName)
		if taskID != "" && trec != nil {
			trec.SetUUID(taskID)
		}
		// LLM session titles ride the small model (checked at fire time).
		// Resumed tasks (existing session file) never re-trigger titling.
		attachTitleRefiner(ctx, trec)
		ttok := &internalmodel.TokenUsage{}
		tplan := tools.NewPlanStore()
		tappr := runner.NewApprovalStateWithMode(taskPwd, startMode)
		twh := handler.NewWebHandler()
		tnotify := makeNotifyingHandler(twh)
		tappr.SetHandler(tnotify)
		// Provide the config/platform needed to lazily build the LLM reviewer when
		// this task enters Auto mode. The engine installs the transcript provider.
		tappr.SetReviewerConfig(taskCfg, platform)
		// Site-permission lookup for browser tools: an origin marked "allow" for a
		// class (navigate/interact) is auto-approved. Reads the live config each
		// call so settings changes take effect without rebuilding the task.
		tappr.SetBrowserPermFunc(func(origin, class string) bool {
			currentCfg, loadErr := config.LoadConfig()
			return loadErr == nil && browserSitePreapproved(currentCfg, origin, class)
		})
		// browser_act's args carry no URL, so its per-site permission check needs
		// the active tab's origin from THIS task's session.
		tappr.SetBrowserOriginFunc(tenv.CurrentBrowserOrigin)

		// Same shape for computer use: origin ↔ bundle id, and computer_act's
		// args carry no app identity, so the frontmost app must come from THIS
		// task's session.
		tappr.SetComputerPermFunc(func(bundleID, class string) bool {
			return computerMgr != nil && computerMgr.Preapproved(bundleID, class)
		})
		tappr.SetComputerAppFunc(tenv.CurrentComputerApp)

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

		// Background memory distillation per task session (gates inside).
		// Local sessions only: for remote (SSH/Docker) tasks taskPwd is a path
		// on the remote host — the memory store and session index are keyed to
		// the local machine, so a remote path would just create a junk scope
		// and never match any sessions.
		if exec == nil {
			mempipeline.MaybeStartBackground(taskCfg, taskPwd)
		}

		// Re-render prompts on every agent build. Memory settings and curated
		// summaries can change while the web server is running; rebuilding an agent
		// must therefore refresh both schema and prompt, not reuse a startup string.
		renderPrompts := func() (string, string) {
			skillDescs := taskLoader.Descriptions()
			if exec != nil {
				return prompts.GetSystemPrompt(promptPlatform, taskPwd, envLabel, nil, skillDescs),
					prompts.GetPlanSystemPrompt(promptPlatform, taskPwd, envLabel, nil)
			}
			return prompts.GetSystemPrompt(platform, taskPwd, "local", taskEnvInfo, skillDescs),
				prompts.GetPlanSystemPrompt(platform, taskPwd, "local", taskEnvInfo)
		}

		toolSearchCounts := func(plan agent.ToolPlan) web.ToolSearchCounts {
			mcpDeferred := 0
			for _, descriptor := range plan.Deferred {
				if descriptor.Source == "mcp" || strings.HasPrefix(descriptor.Source, "mcp:") {
					mcpDeferred++
				}
			}
			return web.ToolSearchCounts{
				DirectCount:      len(plan.Direct),
				DeferredCount:    len(plan.Deferred),
				MCPDeferredCount: mcpDeferred,
			}
		}

		buildAllTools := func(cm model.ToolCallingChatModel, agentCfg *config.Config) []tool.BaseTool {
			// One factory serves subagent + workflow model overrides (incl.
			// the "small" alias); fallback is this task's current model.
			factory := internalmodel.NewModelFactory(agentCfg, cm)
			all := []tool.BaseTool{
				tenv.NewReadTool(), tenv.NewEditTool(), tenv.NewWriteTool(),
				tenv.NewExecuteTool(tbg), tenv.NewGrepTool(),
				tenv.NewTodoWriteTool(), tenv.NewTodoReadTool(),
				tenv.NewGoalSetTool(), tenv.NewGoalGetTool(), tenv.NewGoalUpdateTool(),
				tenv.NewAutomationCreateTool(),
				tenv.NewSwitchEnvTool(),
				tenv.NewCheckBackgroundTool(tbg),
				tenv.NewSubagentTool(&tools.SubagentDeps{
					ChatModel:    cm,
					ModelFactory: factory,
					Recorder:     trec,
					Notifier: func(name, agentType string, done bool, result string, err error) {
						twh.OnSubagentEvent(name, agentType, done, result, err)
					},
					ProgressFn: func(agentName, event, toolName, detail string) {
						twh.OnSubagentProgress(agentName, event, toolName, detail)
					},
				}),
				tenv.NewWorkflowRunTool(&tools.WorkflowToolDeps{
					ModelFactory: factory,
					Recorder:     trec,
					Loader:       taskFlowLoader,
				}),
				tools.NewAskUserTool(&tools.AskUserDeps{
					BatchRequestFn: twh.RequestAskUser,
				}),
				skills.NewLoadSkillTool(taskLoader),
			}
			if config.MemoryEnabled(agentCfg) {
				all = append(all, tenv.NewMemoryNoteTool(&tools.MemoryNoteDeps{
					SessionIDFn: func() string {
						if trec != nil {
							return trec.UUID()
						}
						return ""
					},
				}))
			}
			all = append(all, tenv.NewBrowserTools()...)
			all = append(all, tenv.NewComputerTools()...)
			// Automation runs are unattended — drop interactive tools that would
			// otherwise block on a human who isn't there (see dropInteractiveTools).
			if excludeInteractive {
				all = dropInteractiveTools(all)
			}
			return all
		}

		buildPlanTools := func() []tool.BaseTool {
			plan := []tool.BaseTool{
				tenv.NewReadTool(),
				tenv.NewPlanExecuteTool(),
				tenv.NewGrepTool(),
				tenv.NewTodoWriteTool(), tenv.NewTodoReadTool(),
				tenv.NewGoalSetTool(), tenv.NewGoalGetTool(), tenv.NewGoalUpdateTool(),
				tools.NewAskUserTool(&tools.AskUserDeps{
					BatchRequestFn: twh.RequestAskUser,
				}),
			}
			// Plan mode gets the read-only browser subset (look, don't change).
			plan = append(plan, tenv.NewBrowserPlanTools()...)
			plan = append(plan, tenv.NewComputerPlanTools()...)
			return plan
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
			agentCfg, loadErr := config.LoadConfig()
			if loadErr != nil {
				return nil, fmt.Errorf("reload agent config: %w", loadErr)
			}
			var middlewares []adk.ChatModelAgentMiddleware
			if langfuseTracer != nil {
				middlewares = append(middlewares, langfuseTracer.AgentMiddleware())
			}

			var handlers []adk.ChatModelAgentMiddleware

			compactThreshold := agentCfg.CompactionThreshold()

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

			reductionMw, err := reduction.New(ctx, agent.BuildReductionConfig(
				reductionRoot,
				ctxLimit,
				compactThreshold,
				internalmodel.NewCalibratedCounter(ttok).Count,
			))
			if err != nil {
				config.Logger().Printf("[web] reduction middleware init error: %v", err)
			} else {
				handlers = append(handlers, reductionMw)
			}
			// Aggregate cap on one turn's NEW tool results: reduction only caps
			// each result individually (50k), so N parallel calls could still
			// flood a single request. Registered after reduction so per-result
			// truncation runs first.
			handlers = append(handlers, agent.NewTurnToolResultBudgetMiddleware(0))

			// Env-drift/AGENTS.md refresh only applies to local tasks: for a
			// remote executor taskPwd lives on the remote host, so collecting
			// local state for it would be meaningless. Empty Pwd = feature off.
			reminderPwd, reminderSnapshot := "", ""
			if exec == nil {
				reminderPwd = taskPwd
				reminderSnapshot = prompts.SerializeEnvInfo(platform, taskPwd, "local", taskEnvInfo)
			}
			reminderMw := agent.NewReminderMiddleware(agent.ReminderConfig{
				TodoStore:    tenv.TodoStore,
				GoalStore:    tenv.GoalStore,
				PlanStore:    tplan,
				EnvLabel:     "local",
				IsRemote:     tenv.IsRemote(),
				ContextLimit: ctxLimit,
				FileTracker:  tenv.FileTracker,
				Env:          tenv,
				Pwd:          reminderPwd,
				Platform:     platform,
				EnvSnapshot:  reminderSnapshot,
			}, ttok)
			handlers = append(handlers, reminderMw)

			systemPrompt, planPrompt := renderPrompts()
			prompt := systemPrompt
			toolList := buildAllTools(cm, agentCfg)
			if planMode {
				prompt = planPrompt
				toolList = buildPlanTools()
			}

			// Snapshot MCP exactly once so the candidate catalog and runtime plan
			// cannot observe different reload generations while an agent is built.
			var currentMCPTools []tool.BaseTool
			if mt := mcpToolsPtr.Load(); mt != nil {
				currentMCPTools = append([]tool.BaseTool(nil), (*mt)...)
			}
			if excludeInteractive {
				currentMCPTools = dropInteractiveTools(currentMCPTools)
			}

			if !config.ToolSearchEnabled(agentCfg) {
				// Preserve the eager/static path. Plan mode has never exposed MCP
				// tools, while normal mode appends the captured MCP generation.
				if !planMode {
					toolList = append(toolList, currentMCPTools...)
				}
				return agent.NewAgent(ctx, cm, toolList, prompt, tappr.RequestApproval, middlewares, handlers)
			}

			toolMode := agent.ToolModeNormal
			if planMode {
				toolMode = agent.ToolModePlan
			}
			toolPlan, err := buildCommandToolPlan(
				ctx,
				toolList,
				currentMCPTools,
				agent.ToolTransportWeb,
				toolMode,
			)
			if err != nil {
				return nil, fmt.Errorf("build web tool plan: %w", err)
			}
			return agent.NewAgentWithToolPlan(
				ctx,
				cm,
				toolPlan,
				prompt,
				tappr.RequestApproval,
				middlewares,
				handlers,
			)
		}

		// Per-task chat-model cache so a model/mode switch rebuilds only this task.
		// cmMu serializes the cache against breakdownFn (a GET handler) reading it.
		var cmMu sync.Mutex
		var currentCM model.ToolCallingChatModel
		var currentCtxLimit int
		currentPlanMode := startMode.IsPlan()

		// ToolSearch counts are derived on demand from the latest persisted policy,
		// MCP catalog and task mode. Candidate agents can be discarded by revision
		// checks during concurrent model/settings rebuilds; publishing counts while
		// building such a candidate would make Settings describe an agent that was
		// never installed.
		toolSearchStats := func() web.ToolSearchCounts {
			cmMu.Lock()
			cm, planMode := currentCM, currentPlanMode
			cmMu.Unlock()
			if cm == nil {
				return web.ToolSearchCounts{}
			}
			agentCfg, loadErr := config.LoadConfig()
			if loadErr != nil {
				return web.ToolSearchCounts{}
			}
			toolList := buildAllTools(cm, agentCfg)
			toolMode := agent.ToolModeNormal
			if planMode {
				toolList = buildPlanTools()
				toolMode = agent.ToolModePlan
			}
			var currentMCPTools []tool.BaseTool
			if mt := mcpToolsPtr.Load(); mt != nil {
				currentMCPTools = append([]tool.BaseTool(nil), (*mt)...)
			}
			if excludeInteractive {
				currentMCPTools = dropInteractiveTools(currentMCPTools)
			}
			plan, planErr := buildCommandToolPlan(
				ctx,
				toolList,
				currentMCPTools,
				agent.ToolTransportWeb,
				toolMode,
			)
			if planErr != nil {
				return web.ToolSearchCounts{}
			}
			return toolSearchCounts(plan)
		}

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
			previousPlanMode := currentPlanMode
			currentPlanMode = planMode
			cm, ctxLimit := currentCM, currentCtxLimit
			cmMu.Unlock()
			if cm == nil {
				ag, createErr := createAgent(providerName, modelName)
				if createErr != nil {
					cmMu.Lock()
					currentPlanMode = previousPlanMode
					cmMu.Unlock()
				}
				return ag, createErr
			}
			ag, makeErr := makeAgent(cm, ctxLimit, planMode)
			if makeErr != nil {
				cmMu.Lock()
				currentPlanMode = previousPlanMode
				cmMu.Unlock()
			}
			return ag, makeErr
		}

		breakdownFn := func() usage.ContextBreakdown {
			var b usage.ContextBreakdown
			skillDesc := taskLoader.Descriptions()
			b.SkillsTokens = usage.Estimate(skillDesc)
			systemPrompt, _ := renderPrompts()
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
				currentCfg, loadErr := config.LoadConfig()
				if loadErr != nil {
					return b
				}
				total := 0
				for _, at := range buildAllTools(cm, currentCfg) {
					total += estimateToolTokens(ctx, at)
				}
				b.SystemToolsTokens = total
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
			TaskID:          taskID,
			Pwd:             taskPwd,
			Mode:            startMode.String(),
			ProviderName:    providerName,
			ModelName:       modelName,
			Agent:           ag,
			Env:             tenv,
			TodoStore:       tenv.TodoStore,
			Recorder:        trec,
			TokenUsage:      ttok,
			ApprovalState:   tappr,
			Handler:         twh,
			EventHandler:    tnotify,
			BreakdownFn:     breakdownFn,
			ToolSearchStats: toolSearchStats,
			CreateAgent:     createAgent,
			RebuildForMode:  rebuildForMode,
			FlowLoader:      taskFlowLoader,
			// Recorders the engine creates later (lazy create / session switch
			// in chat.go) get the same title hook as trec above.
			RecorderInit: func(r *session.Recorder) {
				attachTitleRefiner(ctx, r)
			},
		}, nil
	}

	// Resolve the web auth token. Auth is enforced when bound to a non-loopback
	// host (exposed to the network) or when a token was explicitly provided.
	webToken, requireAuth, err := resolveWebToken(host, authToken)
	if err != nil {
		return err
	}
	if requireAuth {
		fmt.Printf("\n🔐 Web access token (required when reaching %s):\n   %s\n", host, webToken)
		fmt.Printf("   Open http://%s:%d/ and paste this token to sign in.\n\n", host, port)
		config.Logger().Printf("[web] token auth enabled for non-loopback bind %q", host)
	}

	// The cloud relay supervisor is constructed before the server so the cloud
	// status/config API can reach it; it is started below, once the rest of the
	// server is wired.
	cloudSup := newCloudSupervisor(cfg, port, webToken)

	// Bootstrap engine for the initial task.
	bootEC, err := buildWebTask("", pwd, startupMode.String(), nil, false)
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
			return buildWebTask(taskID, taskPwd, modeStr, nil, false)
		},
		NewRemoteEngine: func(taskID string, exec tools.RemoteExecutor, remotePwd, modeStr string) (*web.EngineConfig, error) {
			return buildWebTask(taskID, remotePwd, modeStr, exec, false)
		},
		// NewAutomationEngine builds a headless task engine for automation runs.
		// Same as NewEngine but drops interactive tools (ask_user) so an unattended
		// run can't stall waiting for a human to answer a question no one is watching.
		NewAutomationEngine: func(taskID, taskPwd, modeStr string) (*web.EngineConfig, error) {
			return buildWebTask(taskID, taskPwd, modeStr, nil, true)
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
		FlowLoader:         flowLoader,
		ReloadMCP:          reloadMCPTools,
		WechatClient:       wechatClient,
		WebHandler:         bootEC.Handler,
		EventHandler:       bootEC.EventHandler,
		NeedsSetup:         needsSetup,
		TokenUsage:         bootEC.TokenUsage,
		ContextBreakdownFn: bootEC.BreakdownFn,
		ToolSearchStats:    bootEC.ToolSearchStats,
		Automations:        autoStore,
		AuthToken:          webToken,
		RequireAuth:        requireAuth,
		BrowserManager:     browserMgr,
		ComputerManager:    computerMgr,
		MemoryStart: func(runCtx context.Context, project string) (<-chan error, error) {
			currentCfg, loadErr := config.LoadConfig()
			if loadErr != nil {
				return nil, fmt.Errorf("load memory config: %w", loadErr)
			}
			return mempipeline.Start(runCtx, currentCfg, project, mempipeline.Options{
				IncludeRecent:  true,
				IgnoreCooldown: true,
				Log: func(format string, args ...any) {
					config.Logger().Printf("[memory] "+format, args...)
				},
			})
		},
		BLEController:   bleProxy,
		CloudSupervisor: cloudSup,
	})

	// Start the periodic automation scheduler. A single process owns periodic
	// firing (elected via flock); others return immediately. Manual runs work in
	// any process regardless of ownership. The flock is OS-released on exit, so a
	// crashed owner never deadlocks the election.
	if autoStore != nil {
		sched := automation.NewScheduler(autoStore, srv.AutomationRunner())
		go sched.Run(ctx)
	}

	if len(cfg.MCPServers) > 0 {
		srv.ReloadMCPInBackground()
	}

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

	// Wire native-messaging auto-connect: write the endpoint discovery file and
	// install the browser native-host manifest (best-effort, only when browser
	// use is enabled). Lets the extension connect with zero manual steps.
	srv.SetupNativeMessaging()

	// Start the jcloud relay connector in the background (best-effort): it runs
	// only when logged in with cloud.auto_connect enabled, and any failure is a
	// logged warning — the local web server is never affected. Its context is
	// the server's shutdown context, so Ctrl+C tears it down with everything
	// else.
	cloudSup.Start(ctx)

	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	srv.CloseAllEngines()
	// The managed Chrome is owned by the Manager and persists across tasks (task
	// teardown only releases per-task tabs), so it must be torn down here on
	// server exit or it leaks as an orphan process holding the profile lock.
	_ = browserMgr.Close()
	if langfuseTracer != nil {
		langfuseTracer.Flush()
	}
	return nil
}
