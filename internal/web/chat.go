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
	eng, err := s.engineForChat(req.SessionID, modeStr)
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

	sessionID := s.submitMessage(eng, req.Message, modeStr, "", req.SessionID, req.Images)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "processing", "session_id": sessionID})
}

// engineForChat resolves the engine a chat request targets. An empty task id (or
// one matching the active task) uses the active engine; a known live task uses
// its engine; an unknown id lazily spins up a fresh engine for it (a new task or
// the first message of a not-yet-live task), rooted at the active task's pwd.
func (s *Server) engineForChat(taskID, modeStr string) (*Engine, error) {
	if eng := s.resolveEngine(taskID); eng != nil {
		return eng, nil
	}
	pwd := ""
	if a := s.activeEngine(); a != nil {
		pwd = a.pwd
	}
	return s.buildLocalEngine(taskID, pwd, modeStr)
}

// chatImage represents a base64-encoded image in a chat request.
type chatImage struct {
	Data     string `json:"data"`       // base64 data (without data: prefix)
	MimeType string `json:"media_type"` // e.g. "image/png", "image/jpeg"
}

// SubmitMessage submits a message for agent processing from an external source
// (e.g. WeChat inbound message). Returns false if the agent is busy.
func (s *Server) SubmitMessage(message, source string) bool {
	eng := s.activeEngine()
	if eng == nil {
		return false
	}
	if !eng.running.CompareAndSwap(false, true) {
		return false
	}
	s.submitMessage(eng, message, eng.curMode(), source, "", nil)
	return true
}

// submitMessage is the shared implementation for starting an agent run.
// source is an optional label (e.g. "wechat") for the user_message event.
// sessionID is an optional session identifier from the client to ensure
// continuity — if the current recorder has a different UUID, resume the
// correct session instead of creating a new one.
// images is an optional list of base64-encoded images to include in the message.
// The caller must have already set eng.running to true (via CompareAndSwap).
// Returns the session_id of the recorder used.
func (s *Server) submitMessage(eng *Engine, message, mode, source, sessionID string, images []chatImage) string {
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

	// Emit user_message event for external sources (e.g. WeChat) so web clients see it.
	// Web-originated messages are already added by the frontend's sendMessage().
	if source != "" {
		eng.handler.Emit("user_message", map[string]string{
			"content": message,
			"source":  source,
		})
	}

	// Ensure a recorder exists (lazy creation on first message).
	// If the client provided a session_id and the current recorder differs,
	// resume the client's session to prevent creating a duplicate.
	eng.emu.Lock()
	if eng.recorder == nil {
		rec, _ := session.NewRecorder(eng.pwd, eng.providerName, eng.modelName)
		if sessionID != "" {
			rec.SetUUID(sessionID)
		}
		if rec != nil && eng.recorderInit != nil {
			eng.recorderInit(rec)
		}
		eng.recorder = rec
	} else if sessionID != "" && eng.recorder.UUID() != sessionID {
		// Client is continuing a session that doesn't match the current recorder.
		// Resume the client's session to keep all messages together.
		eng.recorder.Close()
		rec, _ := session.NewRecorder(eng.pwd, eng.providerName, eng.modelName)
		rec.SetUUID(sessionID)
		if rec != nil && eng.recorderInit != nil {
			eng.recorderInit(rec)
		}
		eng.recorder = rec
	}
	recorder := eng.recorder
	eng.emu.Unlock()

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
		resp := runner.Run(hookCtx, agent, history, eng.eventHandler, recorder, eng.todoStore, eng.env.GoalStore, s.tracer, eng.tokenUsage)
		if resp != "" {
			eng.emu.Lock()
			eng.history = append(eng.history, &schema.Message{Role: schema.Assistant, Content: resp})
			eng.emu.Unlock()
		}
	}()

	return recorder.UUID()
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
