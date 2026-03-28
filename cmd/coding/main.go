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
	systemPrompt := prompts.GetSystemPrompt(platform, pwd, "local", envInfo)

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

	toolList := []tool.BaseTool{
		env.NewReadTool(), env.NewEditTool(), env.NewWriteTool(),
		env.NewExecuteTool(), env.NewGrepTool(),
		env.NewTodoWriteTool(), env.NewTodoReadTool(),
		env.NewSwitchEnvTool(),
		env.NewBackgroundRunTool(bgManager),
		env.NewCheckBackgroundTool(bgManager),
		env.NewSubagentTool(&tools.SubagentDeps{
			ChatModel: chatModel,
			Notifier: func(name, agentType string, done bool, result string, err error) {
				if tuiProgram == nil {
					return
				}
				if !done {
					tuiProgram.Send(tui.SubagentStartMsg{Name: name, Type: agentType})
				} else {
					tuiProgram.Send(tui.SubagentDoneMsg{Name: name, Result: result, Err: err})
				}
			},
		}),
	}

	var mcpStatuses []tui.MCPStatusItem
	if len(cfg.MCPServers) > 0 {
		var mcpTools []tool.BaseTool
		var internalStatuses []tools.MCPStatus
		mcpTools, internalStatuses = tools.LoadMCPTools(ctx, cfg.MCPServers)
		toolList = append(toolList, mcpTools...)
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
		// (todo check, token warning, error streak) before each model call.
		reminderMw := agent.NewReminderMiddleware(agent.ReminderConfig{
			TodoStore:    env.TodoStore,
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
			systemPrompt = prompts.GetSystemPrompt(platform, pwd, "local", envInfo)
			if newAg, err := createAgent(); err == nil {
				ag = newAg
			}
			p.Send(tui.SSHCancelMsg{})
			return
		}
		approvalState.SetWorkpath(env.Pwd())
		systemPrompt = prompts.GetSystemPrompt(platform, pwd, envLabel, nil)
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
		initialHistory = session.ReconstructHistory(entries)
		initialResumeUUID = resumeUUID
		initialResumeEntries = tui.ConvertSessionEntries(entries)
		hasPrompt = false // treat as interactive, not one-shot

		targetEnv := session.GetLastEnvironment(entries)
		if targetEnv != "local" {
			sessionResumeWarning = attemptSSHResume(targetEnv)
		}
	}

	go func() {
		rec, _ := session.NewRecorder(pwd, cfg.Provider, cfg.Model)
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
		}

		promptCh := tui.GetPromptChannel()
		pendingPromptCh := tui.GetPendingPromptChannel()
		sshCh := tui.GetSSHChannel()
		configCh := tui.GetConfigChannel()
		addModelCh := tui.GetAddModelChannel()
		resumeCh := tui.GetResumeChannel()
		autoApproveCh := tui.GetAutoApproveChannel()
		compactCh := tui.GetCompactChannel()
		for {
			select {
			case enabled := <-autoApproveCh:
				approvalState.SetSessionApproval(enabled)

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

			case pendingPrompt := <-pendingPromptCh:
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

			case uuid := <-resumeCh:
				entries, loadErr := session.LoadSession(uuid)
				if loadErr != nil {
					p.Send(tui.AgentDoneMsg{Err: fmt.Errorf("load session: %w", loadErr)})
					break
				}
				history = session.ReconstructHistory(entries)
				approvalState.SetSessionApproval(false)
				p.Send(tui.SessionResumedMsg{UUID: uuid, Entries: tui.ConvertSessionEntries(entries)})

				targetEnv := session.GetLastEnvironment(entries)
				if targetEnv != "local" {
					sessionResumeWarning = attemptSSHResume(targetEnv)
				}

			case connMsg := <-sshCh:
				switch msg := connMsg.(type) {
				case tui.SSHConnectMsg:
					handleSSHConnect(ctx, env, msg.Addr, msg.Path, p, &systemPrompt, &ag, chatModel, createAgent)
				case tui.SSHListDirReqMsg:
					handleSSHListDir(ctx, env, msg.Path, p)
				case tui.SSHCancelMsg:
					_ = msg
					env.ResetToLocal(pwd, platform)
					systemPrompt = prompts.GetSystemPrompt(platform, pwd, "local", envInfo)
					if newAg, err := createAgent(); err == nil {
						ag = newAg
					}
				}

			case <-compactCh:
				_, _, oldTokens := internalmodel.TokenTracker.Get()
				history = compactHistory(ctx, chatModel, history)
				_, _, newTokens := internalmodel.TokenTracker.Get()
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
