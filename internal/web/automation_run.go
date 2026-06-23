package web

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cnjack/jcode/internal/automation"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/session"
)

// AutomationRunner adapts the web Server to automation.Runner so the scheduler
// (and the CLI, via the web server) can execute a run by reusing the Engine.
func (s *Server) AutomationRunner() automation.Runner {
	return automationRunner{s: s}
}

type automationRunner struct{ s *Server }

func (r automationRunner) StartRun(ctx context.Context, a *automation.Automation, kind string) (string, error) {
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
// completion. Scheduled runs are forced to full_access (headless approvals would
// hang); ctx carries the liveness ceiling for scheduled fires.
func (s *Server) runAutomation(ctx context.Context, a *automation.Automation, kind string) (string, error) {
	if s.newEngine == nil {
		return "", fmt.Errorf("automation runs are unavailable (setup mode)")
	}
	if info, err := os.Stat(a.ProjectPath); err != nil || !info.IsDir() {
		return "", fmt.Errorf("project path is missing or not a directory: %s", a.ProjectPath)
	}

	mode := a.Mode
	if a.Trigger.Type == automation.TriggerSchedule || mode == "" {
		mode = "full_access" // headless: Ask/Plan would block forever on approvals
	}

	eng, err := s.buildLocalEngine("", a.ProjectPath, mode)
	if err != nil {
		return "", err
	}

	// Provider/model override (otherwise inherits the foreground/startup model).
	if a.Provider != "" && eng.createAgent != nil {
		_, curMdl, _ := eng.modelSnapshot()
		mdl := a.Model
		if mdl == "" {
			mdl = curMdl
		}
		if ag, agErr := eng.createAgent(a.Provider, mdl); agErr == nil {
			eng.applyModelSwitch(ag, a.Provider, mdl)
		}
	}

	// Pre-create the recorder so the run has a stable id we can register, tag,
	// and (on completion) tear down. Wrap the event handler to capture the
	// terminal error without disturbing the existing notifier chain.
	prov, mdl, _ := eng.modelSnapshot()
	rec, _ := session.NewRecorder(a.ProjectPath, prov, mdl)
	sid := rec.UUID()
	done := make(chan error, 1)
	eng.emu.Lock()
	eng.recorder = rec
	eng.taskID = sid
	eng.eventHandler = &doneCapture{AgentEventHandler: eng.eventHandler, done: done}
	eng.emu.Unlock()
	// Register so the pump starts (a watching client sees live output). On the
	// cap-exceeded error the engine is NOT in the map, so tear it down here
	// (closing the pre-created recorder) instead of leaking it — mirrors
	// buildLocalEngine's error handling.
	if err := s.registerEngine(eng); err != nil {
		eng.teardown()
		return "", err
	}

	if !eng.running.CompareAndSwap(false, true) {
		s.deleteEngine(sid)
		return sid, fmt.Errorf("engine busy")
	}
	_ = s.submitMessage(eng, a.Prompt, mode, "automation", sid, nil)
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
