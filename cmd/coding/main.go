package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/coding/internal/agent"
	"github.com/cnjack/coding/internal/config"
	internalmodel "github.com/cnjack/coding/internal/model"
	"github.com/cnjack/coding/internal/prompts"
	"github.com/cnjack/coding/internal/runner"
	"github.com/cnjack/coding/internal/session"
	"github.com/cnjack/coding/internal/skills"
	"github.com/cnjack/coding/internal/telemetry"
	"github.com/cnjack/coding/internal/tools"
	"github.com/cnjack/coding/internal/tui"
	util "github.com/cnjack/coding/internal/util"
)

var (
	Version   = "0.2.0"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	// Handle mcp subcommand before standard flag parsing.
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		handleMCPSubcommand(os.Args[2:])
		return
	}

	var prompt string
	flag.StringVar(&prompt, "prompt", "", "The prompt to send to the coding assistant")
	flag.StringVar(&prompt, "p", "", "The prompt to send to the coding assistant (shorthand)")
	var isDoctor bool
	flag.BoolVar(&isDoctor, "doctor", false, "Run system check to test model and MCP connections")
	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "Show version information")
	var resumeUUID string
	flag.StringVar(&resumeUUID, "resume", "", "Resume a previous session by UUID")
	var listSessions bool
	flag.BoolVar(&listSessions, "session", false, "List sessions for the current project and exit")
	flag.Parse()
	prompt = strings.TrimSpace(prompt)
	hasPrompt := prompt != ""

	if showVersion {
		printVersion()
		return
	}
	if isDoctor {
		runDoctorMode()
		return
	}
	if listSessions {
		handleListSessions()
		return
	}

	// Redirect default log output to the app error log so library diagnostics
	// (e.g. Langfuse upload errors) are visible without corrupting the TUI.
	log.SetOutput(config.Logger().Writer())

	// Setup wizard if config is missing
	if config.NeedsSetup() {
		ok, err := tui.RunSetupTUI()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Setup error: %v\n", err)
			os.Exit(1)
		}
		if !ok {
			os.Exit(0)
		}
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Config file: %s\n", config.ConfigPath())
		os.Exit(1)
	}

	ctx := context.Background()
	pwd := util.GetWorkDir()
	platform := util.GetSystemInfo()
	envInfo := util.CollectEnvInfo(pwd)

	// Initialize skill loader: discovers built-in + user + project skills.
	skillLoader := skills.NewLoader()
	skillLoader.ScanProjectSkills(pwd)

	systemPrompt := prompts.GetSystemPrompt(platform, pwd, "local", envInfo, skillLoader.Descriptions())

	providerCfg := cfg.Models[cfg.Provider]
	if providerCfg == nil {
		fmt.Fprintf(os.Stderr, "Provider %q not found in config\n", cfg.Provider)
		os.Exit(1)
	}

	chatModel, err := internalmodel.NewChatModel(ctx, &internalmodel.ChatModelConfig{
		Model: cfg.Model, APIKey: providerCfg.APIKey, BaseURL: providerCfg.BaseURL,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating model: %v\n", err)
		os.Exit(1)
	}

	env := tools.NewEnv(pwd, platform)
	bgManager := tools.NewBackgroundManager(env)

	// tuiProgram holds the TUI program reference, set after RunTUI.
	// Used by closures that need to send messages to the TUI.
	var tuiProgram *tea.Program

	subagentNotifier := func(name, agentType string, done bool, result string, err error) {
		if tuiProgram == nil {
			return
		}
		if !done {
			tuiProgram.Send(tui.SubagentStartMsg{Name: name, Type: agentType})
		} else {
			tuiProgram.Send(tui.SubagentDoneMsg{Name: name, Result: result, Err: err})
		}
	}

	subagentProgress := func(agentName, event, toolName, detail string) {
		if tuiProgram == nil {
			return
		}
		tuiProgram.Send(tui.SubagentProgressMsg{
			AgentName: agentName,
			Event:     event,
			ToolName:  toolName,
			Detail:    detail,
		})
	}

	// mcpTools holds MCP tools loaded at startup, preserved across mode switches.
	var mcpTools []tool.BaseTool
	var mcpStatuses []tui.MCPStatusItem
	if len(cfg.MCPServers) > 0 {
		var internalStatuses []tools.MCPStatus
		mcpTools, internalStatuses = tools.LoadMCPTools(ctx, cfg.MCPServers)
		for _, st := range internalStatuses {
			errMsg := ""
			if st.Error != nil {
				errMsg = st.Error.Error()
			}
			mcpStatuses = append(mcpStatuses, tui.MCPStatusItem{
				Name: st.Name, ToolCount: st.ToolCount, Running: st.Running, ErrMsg: errMsg,
			})
		}
	}

	// PlanStore holds the active plan content across mode transitions.
	planStore := tools.NewPlanStore()

	// Session recorder — created early so closures (subagent, todo callback)
	// can reference it. Lazy file creation means no disk I/O until first message.
	rec, _ := session.NewRecorder(pwd, cfg.Provider, cfg.Model)

	// Wire TodoStore → session recording: each todowrite update is persisted.
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

	// Channel for ask_user tool ← TUI user answers.
	askUserCh := make(chan tools.AskUserResponse, 1)

	// askUserDeps are shared across both normal and plan modes.
	askUserDeps := &tools.AskUserDeps{
		ResponseCh: askUserCh,
		// NotifyFn is set after tuiProgram is available.
	}

	// buildAllTools returns the full tool set for Agent (normal) mode.
	buildAllTools := func() []tool.BaseTool {
		all := []tool.BaseTool{
			env.NewReadTool(), env.NewEditTool(), env.NewWriteTool(),
			env.NewExecuteTool(bgManager), env.NewGrepTool(),
			env.NewTodoWriteTool(), env.NewTodoReadTool(),
			env.NewSwitchEnvTool(),
			env.NewCheckBackgroundTool(bgManager),
			env.NewSubagentTool(&tools.SubagentDeps{
				ChatModel:  chatModel,
				Notifier:   subagentNotifier,
				ProgressFn: subagentProgress,
				Recorder:   rec,
			}),
			tools.NewAskUserTool(askUserDeps),
			skills.NewLoadSkillTool(skillLoader),
		}
		return append(all, mcpTools...)
	}

	// buildPlanTools returns the read-only tool set for Plan mode.
	buildPlanTools := func() []tool.BaseTool {
		return []tool.BaseTool{
			env.NewReadTool(),
			env.NewExecuteTool(nil), // no background in plan mode
			env.NewGrepTool(),
			env.NewTodoWriteTool(), env.NewTodoReadTool(),
			tools.NewAskUserTool(askUserDeps),
		}
	}

	agentMode := tui.ModeNormal
	toolList := buildAllTools()

	// summCapture is shared between the Finalize callback (inside Eino's
	// summarization middleware) and the prompt-handling goroutine. It is
	// single-threaded: the Finalize callback runs synchronously inside
	// runner.Run, and the drain happens right after runner.Run returns.
	summCapture := &summarizationCapture{}

	approvalState := runner.NewApprovalState(pwd)

	// Setup Langfuse tracer if telemetry is configured.
	var langfuseTracer *telemetry.LangfuseTracer
	if cfg.Telemetry != nil && cfg.Telemetry.Langfuse != nil {
		langfuseTracer = telemetry.NewLangfuseTracer(cfg.Telemetry.Langfuse)
	}

	p, _ := tui.RunTUI(hasPrompt, pwd, env.TodoStore)
	tuiProgram = p
	bgManager.SetNotifier(func(taskID, command, status string) {
		p.Send(tui.BgTaskDoneMsg{TaskID: taskID, Command: command, Status: status})
	})
	approvalState.SetProgram(p)

	// Wire NotifyFn callbacks now that tuiProgram is available.
	askUserDeps.NotifyFn = func(question string, options []string) {
		p.Send(tui.AskUserQuestionMsg{Question: question, Options: options})
	}

	// Bridge TUI response channels → tool channels.
	go func() {
		tuiAskCh := tui.GetAskUserResponseChannel()
		for resp := range tuiAskCh {
			askUserCh <- tools.AskUserResponse{Answer: resp.Answer}
		}
	}()

	createAgent := func() (*adk.ChatModelAgent, error) {
		var middlewares []adk.AgentMiddleware
		if langfuseTracer != nil {
			middlewares = append(middlewares, langfuseTracer.AgentMiddleware())
		}

		// Build ChatModelAgentMiddleware handlers (new v0.8 interface).
		// Order: [summarization, reduction, approval+safeTool]
		// Outermost handler is first; approval is always innermost (added by NewAgent).
		var handlers []adk.ChatModelAgentMiddleware

		// Summarization middleware: compresses conversation history when tokens
		// exceed the threshold, preventing context overflow in long sessions.
		contextLimit := internalmodel.GetModelContextLimit(cfg.Model)
		if contextLimit <= 0 {
			contextLimit = 200000 // conservative default
		}
		summMw, err := summarization.New(ctx, &summarization.Config{
			Model: chatModel,
			Trigger: &summarization.TriggerCondition{
				ContextTokens: int(float64(contextLimit) * 0.75),
			},
			TranscriptFilePath: filepath.Join(config.ConfigDir(), "transcript.txt"),
			Finalize: func(ctx context.Context, originalMsgs []adk.Message, summary adk.Message) ([]adk.Message, error) {
				// Default behavior: keep system messages + summary.
				var systemMsgs []adk.Message
				var contextN int
				for _, msg := range originalMsgs {
					if msg.Role == schema.System {
						systemMsgs = append(systemMsgs, msg)
					} else {
						contextN++
					}
				}
				// Capture so the prompt loop can sync history.
				summCapture.capture(summary.Content, contextN)
				config.Logger().Printf("[summarization] Finalize: compacted %d context messages", contextN)
				return append(systemMsgs, summary), nil
			},
		})
		if err != nil {
			config.Logger().Printf("[agent] summarization middleware init error: %v", err)
		} else {
			handlers = append(handlers, summMw)
		}

		// ToolReduction middleware: truncates large tool outputs and clears old
		// tool results when total tokens exceed the threshold.
		reductionBackend := &localReductionBackend{rootDir: config.ConfigDir()}
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
		if err != nil {
			config.Logger().Printf("[agent] reduction middleware init error: %v", err)
		} else {
			handlers = append(handlers, reductionMw)
		}

		// Reminder middleware: injects conditional system reminders
		// (todo check, token warning, error streak, plan execution) before each model call.
		reminderMw := agent.NewReminderMiddleware(agent.ReminderConfig{
			TodoStore:    env.TodoStore,
			PlanStore:    planStore,
			EnvLabel:     "local",
			IsRemote:     env.IsRemote(),
			ContextLimit: contextLimit,
		})
		handlers = append(handlers, reminderMw)

		return agent.NewAgent(ctx, chatModel, toolList, systemPrompt, approvalState.RequestApproval, middlewares, handlers)
	}

	ag, err := createAgent()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating agent: %v\n", err)
		os.Exit(1)
	}

	env.OnEnvChange = func(envLabel string, isLocal bool, err error) {
		if err != nil {
			p.Send(tui.SSHStatusMsg{
				Success: false,
				Err:     err,
			})
			return
		}
		if isLocal {
			approvalState.SetWorkpath(pwd)
			if agentMode == tui.ModePlanning {
				systemPrompt = prompts.GetPlanSystemPrompt(platform, pwd, "local", envInfo)
			} else {
				systemPrompt = prompts.GetSystemPrompt(platform, pwd, "local", envInfo, skillLoader.Descriptions())
			}
			if newAg, err := createAgent(); err == nil {
				ag = newAg
			}
			p.Send(tui.SSHCancelMsg{})
			return
		}
		approvalState.SetWorkpath(env.Pwd())
		if agentMode == tui.ModePlanning {
			systemPrompt = prompts.GetPlanSystemPrompt(platform, pwd, envLabel, nil)
		} else {
			systemPrompt = prompts.GetSystemPrompt(platform, pwd, envLabel, nil, skillLoader.Descriptions())
		}
		if newAg, err := createAgent(); err == nil {
			ag = newAg
		}
		p.Send(tui.SSHStatusMsg{
			Success: true,
			Label:   envLabel,
		})
	}

	var sessionResumeWarning string
	attemptSSHResume := func(target string) string {
		if target == "local" || target == "" {
			return ""
		}
		var alias *config.SSHAlias
		for _, a := range cfg.SSHAliases {
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

		env.SetSSH(sshExec, env.Pwd())
		label := sshExec.Label()
		if env.OnEnvChange != nil {
			env.OnEnvChange(label, false, nil)
		}
		return ""
	}

	// Load a previous session if --resume was requested.
	var initialHistory []adk.Message
	var initialResumeUUID string
	var initialResumeEntries []tui.SessionEntry
	if resumeUUID != "" {
		entries, loadErr := session.LoadSession(resumeUUID)
		if loadErr != nil {
			fmt.Fprintf(os.Stderr, "Cannot load session: %v\n", loadErr)
			os.Exit(1)
		}
		state := session.ReconstructState(entries)
		initialHistory = state.History
		initialResumeUUID = resumeUUID
		initialResumeEntries = tui.ConvertSessionEntries(entries)
		hasPrompt = false // treat as interactive, not one-shot

		// Restore plan state.
		if state.Plan != nil {
			switch state.Plan.Status {
			case "approved":
				planStore.Submit(state.Plan.Title, state.Plan.Content)
				planStore.Approve()
			case "submitted":
				planStore.Submit(state.Plan.Title, state.Plan.Content)
			case "rejected":
				planStore.SetDraft(state.Plan.Title, state.Plan.Content)
			}
		}

		// Restore todo items.
		if len(state.Todos) > 0 {
			todoItems := make([]tools.TodoItem, len(state.Todos))
			for i, t := range state.Todos {
				todoItems[i] = tools.TodoItem{
					ID: t.ID, Title: t.Title, Status: tools.TodoStatus(t.Status),
				}
			}
			env.TodoStore.Update(todoItems)
		}

		targetEnv := state.EnvTarget
		if targetEnv != "local" {
			sessionResumeWarning = attemptSSHResume(targetEnv)
		}
	}

	go func() {
		defer func() {
			if rec != nil {
				rec.Close()
			}
			if langfuseTracer != nil {
				langfuseTracer.Flush()
			}
		}()

		if len(mcpStatuses) > 0 {
			p.Send(tui.MCPStatusMsg{Statuses: mcpStatuses})
		}
		if agentsMdPath := prompts.HasAgentsMd(pwd); agentsMdPath != "" {
			p.Send(tui.AgentsMdMsg{Found: true, Path: agentsMdPath})
		}

		// Notify TUI about available skill slash commands.
		if slashSkills := skillLoader.SlashCommands(); len(slashSkills) > 0 {
			var slashInfos []tui.SkillSlashInfo
			for _, sk := range slashSkills {
				slashInfos = append(slashInfos, tui.SkillSlashInfo{
					Slash:       sk.Slash,
					Description: sk.Description,
				})
			}
			p.Send(tui.SkillsLoadedMsg{SlashCommands: slashInfos})
		}

		history := initialHistory
		if initialResumeUUID != "" {
			p.Send(tui.SessionResumedMsg{UUID: initialResumeUUID, Entries: initialResumeEntries})
		}

		if hasPrompt {
			p.Send(tui.UserPromptMsg{Prompt: prompt})
			if rec != nil {
				rec.RecordUser(prompt)
			}
			history = append(history, schema.UserMessage(prompt))
			resp := runner.Run(ctx, ag, history, p, rec, env.TodoStore, langfuseTracer)
			if resp != "" {
				history = append(history, &schema.Message{Role: schema.Assistant, Content: resp})
			}
			history = syncSummarization(summCapture, history, rec)
		}

		promptCh := tui.GetPromptChannel()
		pendingPromptCh := tui.GetPendingPromptChannel()
		sshCh := tui.GetSSHChannel()
		configCh := tui.GetConfigChannel()
		addModelCh := tui.GetAddModelChannel()
		resumeCh := tui.GetResumeChannel()
		autoApproveCh := tui.GetAutoApproveChannel()
		compactCh := tui.GetCompactChannel()
		planModeCh := tui.GetPlanModeChannel()

		// applyModeSwitch rebuilds the agent for the given mode.
		applyModeSwitch := func(newMode tui.AgentMode) {
			agentMode = newMode
			config.Logger().Printf("[plan] mode switch to %d (0=normal, 1=plan)", newMode)

			// Record mode transition to session.
			if rec != nil {
				rec.RecordModeChange(agentModeString(newMode))
			}

			if agentMode == tui.ModePlanning {
				systemPrompt = prompts.GetPlanSystemPrompt(platform, pwd, env.Exec.Label(), envInfo)
				toolList = buildPlanTools()
				config.Logger().Printf("[plan] built plan tools: %d tools", len(toolList))
			} else {
				systemPrompt = prompts.GetSystemPrompt(platform, pwd, env.Exec.Label(), envInfo, skillLoader.Descriptions())
				toolList = buildAllTools()
				config.Logger().Printf("[plan] built all tools: %d tools", len(toolList))
			}
			if newAg, err := createAgent(); err == nil {
				ag = newAg
				config.Logger().Printf("[plan] agent recreated successfully")
			} else {
				config.Logger().Printf("[plan] agent creation failed: %v", err)
			}
		}

		// drainModeSwitch applies any pending mode switch before processing a prompt.
		drainModeSwitch := func() {
			for {
				select {
				case newMode := <-planModeCh:
					applyModeSwitch(newMode)
				default:
					return
				}
			}
		}

		// handlePlanCompletion is called after runner.Run() returns in plan mode.
		// It submits the agent's response as the plan, shows the review dialog,
		// and blocks until the user approves or rejects.
		var handlePlanCompletion func(resp string)
		handlePlanCompletion = func(resp string) {
			if agentMode != tui.ModePlanning || resp == "" {
				return
			}

			// Store the agent's response as the submitted plan.
			planStore.Submit("Plan", resp)
			config.Logger().Printf("[plan] plan submitted for review (%d chars)", len(resp))
			if rec != nil {
				rec.RecordPlanUpdate("submitted", "Plan", resp, "")
			}

			// Show plan review dialog in TUI.
			p.Send(tui.PlanApprovalMsg{PlanContent: resp, PlanPath: "Plan"})

			// Block waiting for user response.
			planRespCh := tui.GetPlanResponseChannel()
			planResp := <-planRespCh

			if !planResp.Approved {
				feedback := planResp.Feedback
				planStore.Reject(feedback)
				config.Logger().Printf("[plan] plan rejected: %s", feedback)
				if rec != nil {
					rec.RecordPlanUpdate("rejected", "", "", feedback)
				}

				// Re-run with feedback so the agent can revise.
				revisePrompt := "Your plan was rejected."
				if feedback != "" {
					revisePrompt += " Feedback: " + feedback
				}
				revisePrompt += "\nPlease revise your plan based on this feedback."
				p.Send(tui.UserPromptMsg{Prompt: revisePrompt})
				if rec != nil {
					rec.RecordUser(revisePrompt)
				}
				history = append(history, schema.UserMessage(revisePrompt))
				newResp := runner.Run(ctx, ag, history, p, rec, env.TodoStore, langfuseTracer)
				if newResp != "" {
					history = append(history, &schema.Message{Role: schema.Assistant, Content: newResp})
				}
				history = syncSummarization(summCapture, history, rec)
				// Recurse to handle the revised plan.
				handlePlanCompletion(newResp)
				return
			}

			planStore.Approve()
			config.Logger().Printf("[plan] plan approved, transitioning to execution mode")
			if rec != nil {
				rec.RecordPlanUpdate("approved", planStore.Title(), planStore.Content(), "")
			}

			// Extract todos from plan steps.
			todos := tools.ExtractTodosFromPlan(planStore.Content())
			if len(todos) > 0 {
				env.TodoStore.Update(todos)
				p.Send(tui.TodoUpdateMsg{})
				config.Logger().Printf("[plan] populated %d todos from plan", len(todos))
			}

			// Switch to executing mode (full tools + normal prompt).
			applyModeSwitch(tui.ModeExecuting)

			// Auto-send execution prompt.
			execPrompt := "Your plan has been approved. Execute it step by step, tracking progress with the todo list. Mark each step complete as you finish it."
			p.Send(tui.UserPromptMsg{Prompt: execPrompt})
			if rec != nil {
				rec.RecordUser(execPrompt)
			}
			history = append(history, schema.UserMessage(execPrompt))
			execResp := runner.Run(ctx, ag, history, p, rec, env.TodoStore, langfuseTracer)
			if execResp != "" {
				history = append(history, &schema.Message{Role: schema.Assistant, Content: execResp})
			}
			history = syncSummarization(summCapture, history, rec)

			// Check if all todos are done → transition back to normal.
			if env.TodoStore.HasItems() && !env.TodoStore.HasIncomplete() {
				config.Logger().Printf("[plan] all todos complete, switching to normal mode")
				planStore.Clear()
				applyModeSwitch(tui.ModeNormal)
			}
		}

		for {
			select {
			case enabled := <-autoApproveCh:
				approvalState.SetSessionApproval(enabled)

			case newMode := <-planModeCh:
				applyModeSwitch(newMode)

			case cfgMsg := <-configCh:
				providerCfg := cfgMsg.Models[cfgMsg.Provider]
				if providerCfg != nil {
					newChatModel, err := internalmodel.NewChatModel(ctx, &internalmodel.ChatModelConfig{
						Model: cfgMsg.Model, APIKey: providerCfg.APIKey, BaseURL: providerCfg.BaseURL,
					})
					if err == nil {
						chatModel = newChatModel
						if newAg, err := createAgent(); err == nil {
							ag = newAg
						}
					}
				}

			case userPrompt := <-promptCh:
				drainModeSwitch()
				config.Logger().Printf("[plan] processing prompt, agentMode=%d, toolCount=%d", agentMode, len(toolList))
				if sessionResumeWarning != "" {
					userPrompt = sessionResumeWarning + "\n\n" + userPrompt
					sessionResumeWarning = ""
				}
				if rec != nil {
					rec.RecordUser(userPrompt)
				}
				history = append(history, schema.UserMessage(userPrompt))
				history = drainBgNotifications(bgManager, history)
				resp := runner.Run(ctx, ag, history, p, rec, env.TodoStore, langfuseTracer)
				if resp != "" {
					history = append(history, &schema.Message{Role: schema.Assistant, Content: resp})
				}
				history = syncSummarization(summCapture, history, rec)
				handlePlanCompletion(resp)

			case pendingPrompt := <-pendingPromptCh:
				drainModeSwitch()
				p.Send(tui.UserPromptMsg{Prompt: pendingPrompt})
				if sessionResumeWarning != "" {
					pendingPrompt = sessionResumeWarning + "\n\n" + pendingPrompt
					sessionResumeWarning = ""
				}
				if rec != nil {
					rec.RecordUser(pendingPrompt)
				}
				history = append(history, schema.UserMessage(pendingPrompt))
				history = drainBgNotifications(bgManager, history)
				resp := runner.Run(ctx, ag, history, p, rec, env.TodoStore, langfuseTracer)
				if resp != "" {
					history = append(history, &schema.Message{Role: schema.Assistant, Content: resp})
				}
				history = syncSummarization(summCapture, history, rec)
				handlePlanCompletion(resp)

			case uuid := <-resumeCh:
				entries, loadErr := session.LoadSession(uuid)
				if loadErr != nil {
					p.Send(tui.AgentDoneMsg{Err: fmt.Errorf("load session: %w", loadErr)})
					break
				}
				state := session.ReconstructState(entries)
				history = state.History
				approvalState.SetSessionApproval(false)
				p.Send(tui.SessionResumedMsg{UUID: uuid, Entries: tui.ConvertSessionEntries(entries)})

				// Restore plan state.
				if state.Plan != nil {
					switch state.Plan.Status {
					case "approved":
						planStore.Submit(state.Plan.Title, state.Plan.Content)
						planStore.Approve()
					case "submitted":
						planStore.Submit(state.Plan.Title, state.Plan.Content)
					case "rejected":
						planStore.SetDraft(state.Plan.Title, state.Plan.Content)
					}
				}

				// Restore todos.
				if len(state.Todos) > 0 {
					todoItems := make([]tools.TodoItem, len(state.Todos))
					for i, t := range state.Todos {
						todoItems[i] = tools.TodoItem{
							ID: t.ID, Title: t.Title, Status: tools.TodoStatus(t.Status),
						}
					}
					env.TodoStore.Update(todoItems)
					p.Send(tui.TodoUpdateMsg{})
				}

				targetEnv := state.EnvTarget
				if targetEnv != "local" {
					sessionResumeWarning = attemptSSHResume(targetEnv)
				}

			case connMsg := <-sshCh:
				switch msg := connMsg.(type) {
				case tui.SSHConnectMsg:
					handleSSHConnect(ctx, env, msg.Addr, msg.Path, p, &systemPrompt, &ag, chatModel, createAgent, skillLoader.Descriptions())
				case tui.SSHListDirReqMsg:
					handleSSHListDir(ctx, env, msg.Path, p)
				case tui.SSHCancelMsg:
					_ = msg
					env.ResetToLocal(pwd, platform)
					if agentMode == tui.ModePlanning {
						systemPrompt = prompts.GetPlanSystemPrompt(platform, pwd, "local", envInfo)
					} else {
						systemPrompt = prompts.GetSystemPrompt(platform, pwd, "local", envInfo, skillLoader.Descriptions())
					}
					if newAg, err := createAgent(); err == nil {
						ag = newAg
					}
				}

			case <-compactCh:
				_, _, oldTokens := internalmodel.TokenTracker.Get()
				oldLen := len(history)
				history = compactHistory(ctx, chatModel, history)
				_, _, newTokens := internalmodel.TokenTracker.Get()
				// Record compact event if history was actually compacted.
				if rec != nil && len(history) < oldLen && len(history) > 0 {
					rec.RecordCompact(history[0].Content, oldLen-len(history))
				}
				p.Send(tui.CompactDoneMsg{
					OldTokens: oldTokens,
					NewTokens: newTokens,
				})

			case <-addModelCh:
				p.ReleaseTerminal()
				ok, setupErr := tui.RunSetupTUI()
				p.RestoreTerminal()
				if setupErr != nil {
					p.Send(tui.AgentDoneMsg{Err: fmt.Errorf("Setup error: %w", setupErr)})
				} else if ok {
					newCfg, loadErr := config.LoadConfig()
					if loadErr == nil {
						providerCfg := newCfg.Models[newCfg.Provider]
						if providerCfg != nil {
							newChatModel, cmErr := internalmodel.NewChatModel(ctx, &internalmodel.ChatModelConfig{
								Model: newCfg.Model, APIKey: providerCfg.APIKey, BaseURL: newCfg.Models[newCfg.Provider].BaseURL,
							})
							if cmErr == nil {
								chatModel = newChatModel
								if newAg, agErr := createAgent(); agErr == nil {
									ag = newAg
								}
							}
						}
						p.Send(tui.ConfigUpdatedMsg{
							Provider: newCfg.Provider,
							Model:    newCfg.Model,
							Message:  fmt.Sprintf("✅ Added model: %s - %s\n", newCfg.Provider, newCfg.Model),
						})
					}
				}
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		if langfuseTracer != nil {
			langfuseTracer.Flush()
		}
		os.Exit(1)
	}
	if langfuseTracer != nil {
		langfuseTracer.Flush()
	}
}

// localReductionBackend implements reduction.Backend by writing files to a
// local directory. Used by the ToolReduction middleware to persist truncated
// tool output so the agent can re-read it via the read tool.
type localReductionBackend struct {
	rootDir string
}

func (b *localReductionBackend) Write(_ context.Context, req *filesystem.WriteRequest) error {
	fp := req.FilePath
	if !filepath.IsAbs(fp) {
		fp = filepath.Join(b.rootDir, fp)
	}
	if err := os.MkdirAll(filepath.Dir(fp), 0o700); err != nil {
		return err
	}
	return os.WriteFile(fp, []byte(req.Content), 0o600)
}

// compactHistory summarizes the conversation history using the model,
// replacing all messages with a system summary + the last few messages.
func compactHistory(ctx context.Context, cm einomodel.BaseChatModel, history []adk.Message) []adk.Message {
	if len(history) < 4 {
		return history // too short to compact
	}

	// Keep last 2 messages (most recent context).
	keepCount := 2
	if keepCount > len(history) {
		keepCount = len(history)
	}
	toSummarize := history[:len(history)-keepCount]
	kept := history[len(history)-keepCount:]

	// Build a summarization prompt from the older messages.
	var sb strings.Builder
	sb.WriteString("Summarize this conversation history concisely. Focus on:\n")
	sb.WriteString("- Key decisions made\n- Files modified and why\n- Current task status\n- Important context needed to continue\n\n")
	sb.WriteString("Conversation:\n")
	for _, msg := range toSummarize {
		if msg == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, truncateStr(msg.Content, 500)))
	}

	resp, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage("You are a conversation summarizer. Produce a concise summary of the conversation history provided. Output only the summary, no preamble."),
		schema.UserMessage(sb.String()),
	})
	if err != nil {
		config.Logger().Printf("[compact] summarization failed: %v", err)
		return history // return original on error
	}

	var compacted []adk.Message
	compacted = append(compacted, schema.SystemMessage(
		"[Context Summary — previous conversation was compacted]\n\n"+resp.Content,
	))
	compacted = append(compacted, kept...)

	config.Logger().Printf("[compact] %d messages → %d messages", len(history), len(compacted))
	return compacted
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// agentModeString converts an AgentMode to its string representation for session recording.
func agentModeString(m tui.AgentMode) string {
	switch m {
	case tui.ModePlanning:
		return "planning"
	case tui.ModeExecuting:
		return "executing"
	default:
		return "normal"
	}
}

// summarizationCapture captures the result when Eino's summarization middleware
// fires, so that the application-level history can be synced afterwards.
type summarizationCapture struct {
	fired      bool
	summary    string
	compactedN int
}

// capture records a summarization event. Called from the Finalize callback.
func (c *summarizationCapture) capture(summary string, compactedN int) {
	c.fired = true
	c.summary = summary
	c.compactedN = compactedN
}

// drain returns and resets the captured state.
func (c *summarizationCapture) drain() (fired bool, summary string, compactedN int) {
	fired = c.fired
	summary = c.summary
	compactedN = c.compactedN
	c.fired = false
	c.summary = ""
	c.compactedN = 0
	return
}

// syncSummarization checks whether Eino's summarization middleware fired
// during the last runner.Run() and, if so, replaces history with the
// compacted version so the next turn starts from the summarized state.
func syncSummarization(cap *summarizationCapture, history []adk.Message, rec *session.Recorder) []adk.Message {
	fired, summary, compactedN := cap.drain()
	if !fired {
		return history
	}
	// Keep the most recent messages (typically latest user + assistant).
	keepCount := 2
	if keepCount > len(history) {
		keepCount = len(history)
	}
	kept := make([]adk.Message, keepCount)
	copy(kept, history[len(history)-keepCount:])

	var newHistory []adk.Message
	newHistory = append(newHistory, schema.SystemMessage(
		"[Context Summary — conversation was auto-summarized]\n\n"+summary,
	))
	newHistory = append(newHistory, kept...)

	if rec != nil {
		rec.RecordCompact(summary, compactedN)
	}
	config.Logger().Printf("[summarization] synced history: %d → %d messages", len(history), len(newHistory))
	return newHistory
}

// drainBgNotifications injects any completed background task results into
// the conversation history so the agent is aware of them on the next turn.
func drainBgNotifications(bm *tools.BackgroundManager, history []adk.Message) []adk.Message {
	notifs := bm.DrainNotifications()
	if len(notifs) == 0 {
		return history
	}
	var sb strings.Builder
	sb.WriteString("<background-results>\n")
	for _, n := range notifs {
		sb.WriteString(fmt.Sprintf("[%s] %s — %s\n", n.TaskID, n.Status, truncateStr(n.Output, 500)))
	}
	sb.WriteString("</background-results>")

	history = append(history, schema.UserMessage(sb.String()))
	history = append(history, &schema.Message{Role: schema.Assistant, Content: "Noted background results."})
	return history
}
