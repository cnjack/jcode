package web

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cnjack/jcode/internal/automation"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/handler"
	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/session"
)

// AutomationRunner adapts the web Server to automation.Runner so the scheduler
// (and the CLI, via the web server) can execute a run by reusing the Engine.
func (s *Server) AutomationRunner() automation.Runner {
	return automationRunner{s: s}
}

type automationRunner struct{ s *Server }

func (r automationRunner) CanStart(a *automation.Automation) bool {
	if a.ContextPolicy != automation.ContextConversation || a.OwnerSessionID == "" {
		return true
	}
	eng := r.s.resolveEngine(a.OwnerSessionID)
	return eng == nil || !eng.running.Load()
}

func (r automationRunner) StartRun(ctx context.Context, a *automation.Automation, kind string) (string, error) {
	if r.s.automations != nil {
		current := r.s.automations.Get(a.ID)
		if current == nil {
			return "", fmt.Errorf("automation %q no longer exists", a.ID)
		}
		a = current
	}
	return r.s.runAutomation(ctx, a, kind)
}

// doneCapture wraps an event handler to capture the run's terminal error while
// preserving the wrapped handler's behavior (it still drives WS broadcast and
// WeChat/BLE completion notifications). The terminal error is delivered exactly
// once on the buffered channel.
type doneCapture struct {
	handler.AgentEventHandler
	done chan error
}

func (d *doneCapture) OnAgentDone(err error) {
	d.AgentEventHandler.OnAgentDone(err)
	select {
	case d.done <- err:
	default:
	}
}

// runAutomation executes one automation run to completion by building a fresh,
// throwaway headless Engine, injecting the prompt, and blocking until the agent
// is done. The run is recorded as a normal session tagged with the automation id
// and trigger kind. Because there is no idle-evict, the engine is torn down on
// completion. Auto-fired schedule and once definitions are forced to full_access
// (headless approvals would hang); ctx carries the liveness ceiling.
func (s *Server) runAutomation(ctx context.Context, a *automation.Automation, kind string) (string, error) {
	if a.ContextPolicy == automation.ContextConversation {
		return s.runConversationAutomation(ctx, a, kind)
	}
	if s.newEngine == nil {
		return "", fmt.Errorf("automation runs are unavailable (setup mode)")
	}
	if info, err := os.Stat(a.ProjectPath); err != nil || !info.IsDir() {
		return "", fmt.Errorf("project path is missing or not a directory: %s", a.ProjectPath)
	}

	mode := automationRunMode(a)

	// Automation runs are unattended, so they must use a headless engine that
	// drops interactive tools (ask_user). An agent calling ask_user in a run with
	// no watching client would otherwise block on the WS channel forever, stalling
	// the run until the liveness ceiling cancels it. Falls back to the regular
	// local-engine factory when the dedicated headless one isn't wired (setup mode).
	eng, err := s.buildLocalEngineWith("", a.ProjectPath, mode,
		func(taskID, pwd, modeStr string) (*EngineConfig, error) {
			factory := s.newAutomationEngine
			if factory == nil {
				factory = s.newEngine
			}
			return factory(taskID, pwd, modeStr)
		})
	if err != nil {
		return "", err
	}
	sid := eng.taskID

	// Provider/model override (otherwise inherits the foreground/startup model).
	// The "small" alias resolves to config.small_model so recurring mechanical
	// automations can ride the lightweight model like subagents do; unset or
	// invalid small_model degrades to inheriting the current model.
	prov, mdl := a.Provider, a.Model
	if mdl == internalmodel.SmallModelAlias {
		smallRef := ""
		s.cfgMu.Lock()
		if s.cfg != nil {
			smallRef = s.cfg.SmallModel
		}
		s.cfgMu.Unlock()
		if sp, sm, err := internalmodel.ParseProviderModel(smallRef); err == nil {
			prov, mdl = sp, sm
		} else {
			config.Logger().Printf("[automation] %s: small model alias unavailable (small_model=%q); inheriting current model", a.ID, smallRef)
			prov, mdl = "", ""
		}
	}
	if prov != "" && eng.createAgent != nil {
		_, curMdl, _ := eng.modelSnapshot()
		if mdl == "" {
			mdl = curMdl
		}
		eng.rebuildMu.Lock()
		if ag, agErr := eng.createAgent(prov, mdl); agErr == nil {
			eng.applyModelSwitch(ag, prov, mdl)
		}
		eng.rebuildMu.Unlock()
	}

	// Wrap the event handler to capture the run's terminal error without
	// disturbing the existing notifier chain.
	done := make(chan error, 1)
	eng.emu.Lock()
	eng.eventHandler = &doneCapture{AgentEventHandler: eng.eventHandler, done: done}
	eng.emu.Unlock()

	if !s.tryStartEngine(eng) {
		s.deleteEngine(sid)
		return sid, fmt.Errorf("engine busy")
	}
	if _, err := s.submitMessage(eng, a.Prompt, mode, "automation", sid, nil); err != nil {
		s.deleteEngine(sid)
		return sid, err
	}
	s.stampAutomationMeta(sid, a, kind)

	var runErr error
	completed := false
	select {
	case runErr = <-done:
		completed = true
	case <-ctx.Done():
		// Liveness ceiling hit or server shutting down: cancel the in-flight run,
		// then give it a moment to flush a terminal record.
		eng.emu.Lock()
		cancel := eng.runCancel
		eng.emu.Unlock()
		if cancel != nil {
			cancel()
		}
		select {
		case runErr = <-done:
			completed = true
		case <-time.After(3 * time.Second):
			runErr = ctx.Err()
		}
	}

	s.finalizeAutomationMeta(sid, runErr)
	if completed {
		s.deleteEngine(sid)
	} else {
		// The run goroutine is still live after the cancel; tearing down now would
		// Close the recorder under a live writer and truncate the session. Drain in
		// the background and reclaim the engine only once the run actually finishes.
		go func() {
			<-done
			s.deleteEngine(sid)
		}()
	}
	return sid, runErr
}

// runConversationAutomation resumes the owning conversation and injects the
// automation prompt as a synthetic user turn. It uses a one-turn headless agent
// over the conversation's current history, leaving the interactive agent and
// saved mode untouched. If the conversation is busy, the claimed automation
// waits for that turn to finish instead of opening a parallel history writer.
func (s *Server) runConversationAutomation(
	ctx context.Context,
	a *automation.Automation,
	kind string,
) (string, error) {
	if a.OwnerSessionID == "" {
		return "", fmt.Errorf("conversation automation %q has no owner session", a.ID)
	}

	var eng *Engine
	for {
		// Activation and the running claim share taskCreateMu with session deletion.
		// Without this lock span, deletion could clear the recorder after activation
		// but before the claim, and this run would recreate the deleted session.
		s.taskCreateMu.Lock()
		result, err := s.ensureConversationLocked(ctx, a.OwnerSessionID, a.ProjectPath, "automation", "")
		if err != nil {
			s.taskCreateMu.Unlock()
			return "", fmt.Errorf("resume automation conversation %q: %w", a.OwnerSessionID, err)
		}
		if result.Project != a.ProjectPath {
			s.taskCreateMu.Unlock()
			return "", fmt.Errorf(
				"automation conversation %q belongs to project %q, not %q",
				a.OwnerSessionID, result.Project, a.ProjectPath,
			)
		}
		eng = s.resolveEngine(a.OwnerSessionID)
		if eng == nil {
			s.taskCreateMu.Unlock()
			return "", fmt.Errorf("automation conversation %q has no runtime", a.OwnerSessionID)
		}
		claimed := s.tryStartEngine(eng)
		s.taskCreateMu.Unlock()
		if claimed {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	submitted := false
	defer func() {
		if !submitted {
			eng.running.Store(false)
		}
	}()
	if eng.rebuildForAutomation == nil {
		return "", fmt.Errorf("conversation automation agent is unavailable")
	}
	eng.rebuildMu.Lock()
	automationAgent, err := eng.rebuildForAutomation()
	eng.rebuildMu.Unlock()
	if err != nil {
		return "", fmt.Errorf("build conversation automation agent: %w", err)
	}

	done := make(chan error, 1)
	eng.emu.Lock()
	baseHandler := eng.eventHandler
	eng.emu.Unlock()
	if baseHandler == nil {
		return "", fmt.Errorf("conversation automation handler is unavailable")
	}
	capture := &doneCapture{AgentEventHandler: baseHandler, done: done}
	sid, err := s.submitMessageWithOptions(
		eng,
		conversationAutomationPrompt(a, kind),
		"full_access",
		"automation",
		a.OwnerSessionID,
		nil,
		submitMessageOptions{Agent: automationAgent, EventHandler: capture},
	)
	if err != nil {
		return sid, err
	}
	submitted = true

	select {
	case runErr := <-done:
		return sid, runErr
	case <-ctx.Done():
		eng.emu.Lock()
		cancel := eng.runCancel
		eng.emu.Unlock()
		if cancel != nil {
			cancel()
		}
		select {
		case runErr := <-done:
			return sid, runErr
		case <-time.After(3 * time.Second):
			return sid, ctx.Err()
		}
	}
}

func conversationAutomationPrompt(a *automation.Automation, kind string) string {
	return fmt.Sprintf(
		"<automation-fire id=%q kind=%q>\n%s\n</automation-fire>",
		a.ID, kind, a.Prompt,
	)
}

func automationRunMode(a *automation.Automation) string {
	mode := a.Mode
	if a.Trigger.Type == automation.TriggerSchedule || a.Trigger.Type == automation.TriggerOnce || mode == "" {
		return "full_access" // headless: Ask/Plan would block forever on approvals
	}
	return mode
}

// stampAutomationMeta tags a run's session with its automation id, trigger kind
// and a title, so it surfaces in "Recent runs" and is excluded from the main
// task list.
func (s *Server) stampAutomationMeta(sessionID string, a *automation.Automation, kind string) {
	_, _ = session.UpdateSessionMeta(sessionID, func(m *session.SessionMeta) {
		m.AutomationID = a.ID
		m.TriggerKind = kind
		if m.Title == "" {
			m.Title = a.Name
		}
		m.UpdatedAt = time.Now().Format(time.RFC3339)
	})
}

// finalizeAutomationMeta records the run-outcome audit fields used by the Status
// (Success/Failed) filter.
func (s *Server) finalizeAutomationMeta(sessionID string, runErr error) {
	_, _ = session.UpdateSessionMeta(sessionID, func(m *session.SessionMeta) {
		m.EndTime = time.Now().Format(time.RFC3339)
		m.UpdatedAt = m.EndTime
		if runErr != nil {
			m.TerminalStatus = automation.StatusError
			m.ErrorReason = truncateReason(runErr.Error(), 300)
		} else {
			m.TerminalStatus = automation.StatusSuccess
		}
	})
}

func truncateReason(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
