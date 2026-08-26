package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/flow"
	"github.com/cnjack/jcode/internal/hooks"
	"github.com/cnjack/jcode/internal/runner"
	"github.com/cnjack/jcode/internal/session"
)

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if s.needsSetup {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "setup required: please configure a provider first"})
		return
	}

	var req struct {
		Message   string      `json:"message"`
		Images    []chatImage `json:"images,omitempty"`     // optional: base64-encoded images
		Mode      string      `json:"mode,omitempty"`       // "build" or "plan"
		SessionID string      `json:"session_id,omitempty"` // optional: the task (session) to run
		// Source is an optional channel label (e.g. "console"/"mobile" from the
		// cloud relay connector) propagated to the user_message event, exactly
		// like SubmitMessage's source ("wechat"). Empty = web-originated.
		Source string `json:"source,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 20<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}

	modeStr := req.Mode
	if modeStr == "" {
		modeStr = s.activeMode()
	}

	// Resolve (or lazily create) the engine for this task. Different tasks run
	// concurrently; the per-task running flag only blocks double-running the SAME
	// task.
	eng, err := s.engineForChatContext(r.Context(), req.SessionID, modeStr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !eng.running.CompareAndSwap(false, true) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "this task is already processing a request",
		})
		return
	}

	sessionID, err := s.submitMessage(eng, req.Message, modeStr, req.Source, req.SessionID, req.Images)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "processing", "session_id": sessionID})
}

// engineForChat resolves the engine a chat request targets. An empty task id (or
// one matching the active task) uses the active engine; a known live task uses
// its engine; an unknown id lazily spins up a fresh engine for it (a new task or
// the first message of a not-yet-live task), rooted at the active task's pwd.
func (s *Server) engineForChat(taskID, modeStr string) (*Engine, error) {
	return s.engineForChatContext(context.Background(), taskID, modeStr)
}

func (s *Server) engineForChatContext(ctx context.Context, taskID, modeStr string) (*Engine, error) {
	if taskID != "" {
		meta, err := session.FindSessionMeta(taskID)
		if err != nil {
			return nil, fmt.Errorf("load conversation metadata: %w", err)
		}
		if meta != nil {
			result, err := s.ensureConversation(ctx, taskID, "", "")
			if err != nil {
				return nil, err
			}
			eng := s.resolveEngine(result.SessionID)
			if eng == nil {
				return nil, fmt.Errorf("activated task %s is unavailable", result.SessionID)
			}
			return eng, nil
		}
	}
	if eng := s.resolveEngine(taskID); eng != nil {
		return eng, nil
	}
	// The first two requests for an explicit client-provided task ID can arrive
	// concurrently. Re-check under a creation mutex so exactly one canonical
	// Engine (and therefore one running flag/Recorder/usage ledger) is published.
	if taskID != "" {
		s.taskCreateMu.Lock()
		defer s.taskCreateMu.Unlock()
		if eng := s.resolveEngine(taskID); eng != nil {
			return eng, nil
		}
	}
	active := s.activeEngine()
	project := engineProject(active)
	workspaceKind := session.WorkspaceProject
	if active != nil {
		workspaceKind = session.NormalizeWorkspaceKind(active.workspaceKind)
	}
	if project == "" {
		// Setup-focused tests and embedders may have a bootstrap engine without a
		// workspace yet. This branch is only for a genuinely new, non-indexed task;
		// persisted remote ids were resolved above and can never reach it.
		if s.newEngine == nil {
			return nil, fmt.Errorf("task creation is not supported")
		}
		eng, err := s.assembleLocalEngine(taskID, "", modeStr, s.newEngine)
		if err != nil {
			return nil, err
		}
		if err := s.publishEngineCandidate(eng, nil); err != nil {
			eng.teardown()
			return nil, err
		}
		return eng, nil
	}
	target, err := parseConversationTarget(project)
	if err != nil {
		return nil, fmt.Errorf("resolve active conversation target: %w", err)
	}
	eng, err := s.assembleConversationEngine(ctx, taskID, target, modeStr, workspaceKind)
	if err != nil {
		return nil, err
	}
	if err := s.publishEngineCandidate(eng, nil); err != nil {
		eng.teardown()
		return nil, err
	}
	return eng, nil
}

// chatImage represents a base64-encoded image in a chat request.
type chatImage struct {
	Data     string `json:"data"`       // base64 data (without data: prefix)
	MimeType string `json:"media_type"` // e.g. "image/png", "image/jpeg"
}

// SubmitMessage submits a message for agent processing from an external source
// (e.g. WeChat inbound message). accepted is false only when no active engine is
// available or the targeted engine is already busy; agent recovery failures are
// returned separately so channel callers can surface the real error.
func (s *Server) SubmitMessage(message, source string) (accepted bool, err error) {
	eng := s.activeEngine()
	if eng == nil {
		return false, nil
	}
	if !eng.running.CompareAndSwap(false, true) {
		return false, nil
	}
	_, err = s.submitMessage(eng, message, eng.curMode(), source, "", nil)
	if err != nil {
		return false, err
	}
	return true, nil
}

// submitMessage is the shared implementation for starting an agent run.
// source is an optional label (e.g. "wechat") for the user_message event.
// sessionID is an optional session identifier from the client to ensure
// continuity — if the current recorder has a different UUID, resume the
// correct session instead of creating a new one.
// images is an optional list of base64-encoded images to include in the message.
// The caller must have already set eng.running to true (via CompareAndSwap).
// Returns the session_id of the recorder used. If a degraded Engine is
// still unavailable, agent construction is retried before any history or
// recorder mutation and the caller's running claim is released.
func (s *Server) submitMessage(eng *Engine, message, mode, source, sessionID string, images []chatImage) (string, error) {
	if err := eng.ensureAgentAvailable(); err != nil {
		eng.running.Store(false)
		return "", err
	}

	// Slash command rewrite: if the original message starts with "/", check for
	// skill slash commands and rewrite to load_skill instruction (same pattern as
	// ACP/TUI). This must happen BEFORE the plan-mode prefix is applied, otherwise
	// HasPrefix("/"…) would fail against the prefixed string.
	agentMsg := message
	if strings.HasPrefix(message, "/") {
		cmd := strings.TrimPrefix(message, "/")
		parts := strings.SplitN(cmd, " ", 2)
		cmdName := parts[0]
		userInput := ""
		if len(parts) > 1 {
			userInput = parts[1]
		}
		matchedSkill := false
		if s.skillLoader != nil {
			if sk := s.skillLoader.GetBySlash("/" + cmdName); sk != nil {
				var sb strings.Builder
				fmt.Fprintf(&sb, "Use the load_skill tool with name=%q and follow its instructions.", sk.Name)
				if userInput != "" {
					sb.WriteString("\n\nAdditional context: ")
					sb.WriteString(userInput)
				}
				agentMsg = sb.String()
				matchedSkill = true
			}
		}
		// Otherwise check workflow slash commands (e.g. /repo-audit) against this
		// task's project loader so its .jcode/workflows resolve.
		if fl := s.flowLoaderFor(eng); !matchedSkill && fl != nil {
			if wf, ok := fl.GetBySlash("/" + cmdName); ok {
				agentMsg = flow.SlashRunPrompt(wf.Meta.Name, userInput)
			}
		}
	}

	// Plan mode no longer needs an inline prompt prefix: the agent is rebuilt with
	// the read-only plan system prompt + tool set on mode switch (handleSwitchMode),
	// matching TUI/ACP. The mode arg is retained for the recorder/event context.
	_ = mode

	// M19: stamp the receiving session's sync opt-in BEFORE the user_message
	// Emit below — the connector's event pump gates uploads on the opt-in, so
	// a cloud-originated first message would otherwise race uplink and be
	// dropped (J8-S3: user_message missing from the durable log). Keyed on
	// eng.taskID, the WS event task_id the gate checks; it coincides with the
	// recorder UUID for local engines, and the recorder-based stamp below
	// covers the resume/replace branch. SetIfAbsent makes repeat stamps
	// (every message lands here) harmless.
	s.stampCloudSync(eng.taskID, source, sessionID == "")

	// Every durable user turn is emitted so the Cloud event mirror cannot miss
	// messages composed on Desktop. Desktop already renders its own optimistic
	// message, so local_echo lets that frontend ignore only this echoed event;
	// remote/channel messages carry their source and render normally. One call
	// emits exactly once, including Cloud-originated turns.
	userEvent := map[string]any{"content": message, "source": source}
	if source == "" {
		userEvent["local_echo"] = true
	}
	eng.handler.Emit("user_message", userEvent)

	// Ensure a recorder exists (lazy creation on first message).
	// If the client provided a session_id and the current recorder differs,
	// resume the client's session to prevent creating a duplicate.
	// stampNew captures whether THIS call minted the recording session (for
	// the M19 sync stamp below; the store write happens after eng.emu is
	// released).
	var stampNew bool
	eng.toolOverrideMu.Lock()
	eng.emu.Lock()
	if eng.recorder == nil {
		rec, err := session.NewRecorder(eng.pwd, eng.providerName, eng.modelName)
		if err != nil {
			eng.emu.Unlock()
			eng.toolOverrideMu.Unlock()
			eng.running.Store(false)
			return "", fmt.Errorf("create session recorder: %w", err)
		}
		if rec == nil {
			eng.emu.Unlock()
			eng.toolOverrideMu.Unlock()
			eng.running.Store(false)
			return "", fmt.Errorf("create session recorder: returned nil recorder")
		}
		rec.SetWorkspaceKind(eng.workspaceKind)
		rec.SetAgent(eng.agentRole)
		if sessionID != "" {
			rec.SetUUID(sessionID)
		}
		if eng.recorderInit != nil {
			eng.recorderInit(rec)
		}
		eng.recorder = rec
		// sessionID == "" means the recorder just minted a new UUID.
		stampNew = sessionID == ""
	} else if sessionID != "" && eng.recorder.UUID() != sessionID {
		// Client is continuing a session that doesn't match the current recorder.
		// Build the replacement before closing the current recorder so a creation
		// failure cannot discard the live session.
		rec, err := session.NewRecorder(eng.pwd, eng.providerName, eng.modelName)
		if err != nil {
			eng.emu.Unlock()
			eng.toolOverrideMu.Unlock()
			eng.running.Store(false)
			return "", fmt.Errorf("create recorder for session %s: %w", sessionID, err)
		}
		if rec == nil {
			eng.emu.Unlock()
			eng.toolOverrideMu.Unlock()
			eng.running.Store(false)
			return "", fmt.Errorf("create recorder for session %s: returned nil recorder", sessionID)
		}
		rec.SetWorkspaceKind(eng.workspaceKind)
		rec.SetAgent(eng.agentRole)
		rec.SetUUID(sessionID)
		if eng.recorderInit != nil {
			eng.recorderInit(rec)
		}
		eng.recorder.Close()
		eng.recorder = rec
	}
	recorder := eng.recorder
	eng.emu.Unlock()
	eng.toolOverrideMu.Unlock()
	// M19: stamp the receiving session's initial cloud-sync state on EVERY
	// submitted message, not only when this call created the recorder — the
	// bootstrap engine already has a recorder at startup, and a cloud
	// chat.send landing on it (the J8 regression) must still opt the session
	// in. stampCloudSync is a no-op for local sources unless the session is
	// brand-new (then cloud.sync_default applies), and never overwrites an
	// explicit user toggle.
	if recorder != nil {
		s.stampCloudSync(recorder.UUID(), source, stampNew)
	}

	// Record user message.
	if recorder != nil {
		var entryImages []session.EntryImage
		for _, img := range images {
			entryImages = append(entryImages, session.EntryImage{
				MimeType: img.MimeType,
				Data:     img.Data,
			})
		}
		recorder.RecordUser(agentMsg, entryImages...)
		if recorder.HasRecording() {
			session.SaveLastSession(engineProject(eng), recorder.UUID())
		}
	}

	// Build the user message — include images as multimodal content if provided.
	var userMsg *schema.Message
	if len(images) > 0 {
		parts := make([]schema.MessageInputPart, 0, len(images)+1)
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeText,
			Text: agentMsg,
		})
		for _, img := range images {
			data := img.Data
			parts = append(parts, schema.MessageInputPart{
				Type: schema.ChatMessagePartTypeImageURL,
				Image: &schema.MessageInputImage{
					MessagePartCommon: schema.MessagePartCommon{
						MIMEType:   img.MimeType,
						Base64Data: &data,
					},
				},
			})
		}
		userMsg = &schema.Message{
			Role:                  schema.User,
			Content:               agentMsg,
			UserInputMultiContent: parts,
		}
	} else {
		userMsg = schema.UserMessage(agentMsg)
	}

	eng.emu.Lock()
	eng.history = append(eng.history, userMsg)
	history := make([]adk.Message, len(eng.history))
	copy(history, eng.history)
	agent := eng.agent
	eng.emu.Unlock()

	// Reset the per-turn approval-reviewer denial breaker.
	if eng.approvalState != nil {
		eng.approvalState.OnTurnStart()
	}

	// Stream response via WebSocket — run agent in background. Each task derives
	// its own cancellable context so /stop cancels only that task. Fall back to
	// Background if a run is somehow submitted before Start set the root context.
	base := s.rootCtx()
	if base == nil {
		base = context.Background()
	}
	runCtx, runCancel := context.WithCancel(base)
	eng.emu.Lock()
	eng.runGen++
	gen := eng.runGen
	eng.runCancel = runCancel
	eng.emu.Unlock()

	go func() {
		s.setTaskStatus(eng, true)
		defer func() {
			// Tear down only if this run is still the current one. If a newer turn
			// on the same engine has already taken over (runGen advanced) it now
			// owns running/runCancel — leave them so /stop still reaches the live
			// run and we don't broadcast a spurious idle for it. Releasing running
			// inside the same emu section that clears runCancel also closes the
			// gate↔cancel interleave window the run-start CAS relies on.
			eng.emu.Lock()
			superseded := eng.runGen != gen
			if !superseded {
				eng.runCancel = nil
				eng.running.Store(false)
			}
			eng.emu.Unlock()
			if !superseded {
				s.setTaskStatus(eng, false)
			}
		}()

		// Take a git snapshot before the agent run for session diff tracking.
		s.takeSessionSnapshot(eng)

		// Inject the hook dispatcher so PreToolUse/PostToolUse/Stop hooks run on the
		// Web surface too (parity with the TUI); reloaded per turn for hot-apply.
		hookCtx := hooks.WithDispatcher(runCtx, hooks.NewSessionDispatcher(config.ConfigDir(), eng.env.Pwd(), recorder.UUID(), config.Logger().Printf))
		result := runner.Run(hookCtx, agent, history, eng.eventHandler, recorder, eng.todoStore, eng.env.GoalStore, s.tracer, eng.tokenUsage)
		if len(result.Messages) > 0 {
			eng.emu.Lock()
			eng.history = append(eng.history, result.Messages...)
			eng.emu.Unlock()
		}
	}()

	return recorder.UUID(), nil
}

// --- Stop handler ---

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	// Cancel only the targeted task. task_id comes via query or JSON body; absent,
	// fall back to the active task (legacy clients).
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		var req struct {
			TaskID string `json:"task_id"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
		taskID = req.TaskID
	}

	eng := s.resolveEngine(taskID)
	if eng == nil || !eng.running.Load() {
		writeJSON(w, http.StatusOK, map[string]string{"status": "not_running"})
		return
	}

	eng.emu.Lock()
	cancel := eng.runCancel
	eng.emu.Unlock()
	if cancel != nil {
		cancel()
	}

	// The runner owns the run lifecycle: it observes the cancellation and emits
	// the single OnAgentDone(context.Canceled), which the web handler surfaces
	// as a calm "stopped" notice. Emitting one here too would double-report.

	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}
