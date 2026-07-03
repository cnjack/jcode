// Command jcode-acp-harness is a headless ACP client that drives a single
// `jcode acp` subprocess through one prompt turn, records the entire streamed
// event trajectory to a JSONL log, auto-approves permission requests, and
// prints a compact JSON result summary on stdout.
//
// It exists to test jcode's autonomous execution unattended. Each invocation is
// meant to run with an isolated HOME (so it reads a throwaway config instead of
// the operator's real ~/.jcode with live keys) and an isolated sandbox cwd (so
// file/exec side effects are contained). The orchestrator sets those up.
//
// Usage:
//
//	jcode-acp-harness -bin /path/to/jcode -cwd /sandbox \
//	    -promptfile prompt.txt -out events.jsonl -model glm-5.1 -timeout 300
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// recorder writes one JSON object per line to the event log and keeps running
// summary counters extracted from the ACP stream.
type recorder struct {
	mu sync.Mutex
	w  *os.File

	agentTextLen  int
	agentChunks   int
	thoughtChunks int
	toolCalls     int
	toolUpdates   int
	permissionN   int
	plans         int
	toolNames     map[string]int
	toolKind      map[string]string // toolCallId -> kind
	toolStatusEnd map[string]string // toolCallId -> last seen status
	toolTitles    map[string]string // toolCallId -> title (tool name)
	lastUsage     json.RawMessage
	finalText     []byte
}

func newRecorder(w *os.File) *recorder {
	return &recorder{
		w:             w,
		toolNames:     map[string]int{},
		toolKind:      map[string]string{},
		toolStatusEnd: map[string]string{},
		toolTitles:    map[string]string{},
	}
}

func (r *recorder) line(kind string, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		raw, _ = json.Marshal(map[string]string{"marshal_error": err.Error()})
	}
	rec := struct {
		TS   int64           `json:"ts"`
		Kind string          `json:"kind"`
		Data json.RawMessage `json:"data"`
	}{TS: time.Now().UnixMilli(), Kind: kind, Data: raw}
	b, _ := json.Marshal(rec)
	r.mu.Lock()
	_, _ = r.w.Write(b)
	_, _ = r.w.Write([]byte("\n"))
	r.mu.Unlock()
}

// client implements acp.Client (the editor/host side of ACP). jcode's agent
// runs its own tools server-side, so the fs/terminal callbacks here are never
// exercised in practice; they are implemented defensively.
type client struct {
	rec     *recorder
	sandbox string
}

func (c *client) SessionUpdate(_ context.Context, params acp.SessionNotification) error {
	u := params.Update
	c.rec.line("session_update", u) // lossless: SessionUpdate has a custom MarshalJSON

	c.rec.mu.Lock()
	defer c.rec.mu.Unlock()
	switch {
	case u.AgentMessageChunk != nil:
		c.rec.agentChunks++
		if u.AgentMessageChunk.Content.Text != nil {
			t := u.AgentMessageChunk.Content.Text.Text
			c.rec.agentTextLen += len(t)
			c.rec.finalText = append(c.rec.finalText, t...)
		}
	case u.AgentThoughtChunk != nil:
		c.rec.thoughtChunks++
	case u.ToolCall != nil:
		tc := u.ToolCall
		c.rec.toolCalls++
		c.rec.toolNames[tc.Title]++
		id := string(tc.ToolCallId)
		c.rec.toolTitles[id] = tc.Title
		c.rec.toolKind[id] = string(tc.Kind)
		c.rec.toolStatusEnd[id] = string(tc.Status)
	case u.ToolCallUpdate != nil:
		tu := u.ToolCallUpdate
		c.rec.toolUpdates++
		if tu.Status != nil {
			c.rec.toolStatusEnd[string(tu.ToolCallId)] = string(*tu.Status)
		}
	case u.Plan != nil:
		c.rec.plans++
	case u.UsageUpdate != nil:
		raw, _ := json.Marshal(u.UsageUpdate)
		c.rec.lastUsage = raw
	}
	return nil
}

// RequestPermission auto-approves by selecting an allow option. This simulates
// an unattended full-access operator and also exercises the permission plumbing.
func (c *client) RequestPermission(_ context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	c.rec.mu.Lock()
	c.rec.permissionN++
	c.rec.mu.Unlock()

	var chosen acp.PermissionOptionId
	for _, o := range params.Options { // prefer allow_always to reduce round-trips
		if o.Kind == acp.PermissionOptionKindAllowAlways {
			chosen = o.OptionId
		}
	}
	if chosen == "" {
		for _, o := range params.Options {
			if o.Kind == acp.PermissionOptionKindAllowOnce {
				chosen = o.OptionId
			}
		}
	}
	if chosen == "" && len(params.Options) > 0 {
		chosen = params.Options[0].OptionId
	}
	c.rec.line("permission_request", map[string]any{"chosen": string(chosen), "toolCall": params.ToolCall})
	return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeSelected(chosen)}, nil
}

func (c *client) ReadTextFile(_ context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	b, err := os.ReadFile(params.Path)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	return acp.ReadTextFileResponse{Content: string(b)}, nil
}

func (c *client) WriteTextFile(_ context.Context, params acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	if err := os.MkdirAll(filepath.Dir(params.Path), 0o755); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if err := os.WriteFile(params.Path, []byte(params.Content), 0o644); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	return acp.WriteTextFileResponse{}, nil
}

var errUnsupported = fmt.Errorf("client capability not supported by test harness")

func (c *client) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, errUnsupported
}
func (c *client) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, errUnsupported
}
func (c *client) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, errUnsupported
}
func (c *client) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, errUnsupported
}
func (c *client) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, errUnsupported
}
func (c *client) UnstableCompleteElicitation(context.Context, acp.UnstableCompleteElicitationNotification) error {
	return nil
}
func (c *client) UnstableCreateElicitation(context.Context, acp.UnstableCreateElicitationRequest) (acp.UnstableCreateElicitationResponse, error) {
	return acp.NewUnstableCreateElicitationResponseDecline(), nil
}
func (c *client) UnstableConnectMcp(context.Context, acp.UnstableConnectMcpRequest) (acp.UnstableConnectMcpResponse, error) {
	return acp.UnstableConnectMcpResponse{}, errUnsupported
}
func (c *client) UnstableDisconnectMcp(context.Context, acp.UnstableDisconnectMcpRequest) (acp.UnstableDisconnectMcpResponse, error) {
	return acp.UnstableDisconnectMcpResponse{}, nil
}

func main() {
	var (
		bin        = flag.String("bin", "", "path to jcode binary")
		cwd        = flag.String("cwd", "", "session working directory (sandbox, absolute)")
		promptStr  = flag.String("prompt", "", "prompt text (or use -promptfile)")
		promptFile = flag.String("promptfile", "", "file containing the prompt text")
		outPath    = flag.String("out", "events.jsonl", "event log output path")
		model      = flag.String("model", "", "model label recorded in the result")
		modeStr    = flag.String("mode", "full_access", "session mode: approval|plan|full_access")
		timeoutSec = flag.Int("timeout", 300, "prompt turn timeout in seconds")
		setupSec   = flag.Int("setup-timeout", 90, "initialize+new-session timeout in seconds")
	)
	flag.Parse()

	if *promptFile != "" {
		b, err := os.ReadFile(*promptFile)
		if err != nil {
			die(map[string]any{"stop_reason": "HARNESS_ERROR", "error": "read promptfile: " + err.Error(), "model": *model})
		}
		*promptStr = string(b)
	}
	if *bin == "" || *cwd == "" || *promptStr == "" {
		die(map[string]any{"stop_reason": "HARNESS_ERROR", "error": "missing -bin/-cwd/-prompt", "model": *model})
	}

	f, err := os.Create(*outPath)
	if err != nil {
		die(map[string]any{"stop_reason": "HARNESS_ERROR", "error": "create out: " + err.Error(), "model": *model})
	}
	defer f.Close()
	rec := newRecorder(f)

	result := map[string]any{"model": *model, "cwd": *cwd, "mode": *modeStr}
	start := time.Now()

	// Launch the jcode ACP server. HOME/env is inherited from the caller, which
	// is expected to have already pointed HOME at an isolated throwaway dir.
	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	cmd := exec.CommandContext(subCtx, *bin, "acp")
	cmd.Env = os.Environ()
	cmd.Dir = *cwd
	stdin, err := cmd.StdinPipe()
	if err != nil {
		finish(rec, result, "HARNESS_ERROR", "stdin pipe: "+err.Error(), start)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		finish(rec, result, "HARNESS_ERROR", "stdout pipe: "+err.Error(), start)
		return
	}
	if stderrF, ferr := os.Create(*outPath + ".stderr"); ferr == nil {
		cmd.Stderr = stderrF
		defer stderrF.Close()
	}
	if err := cmd.Start(); err != nil {
		finish(rec, result, "HARNESS_ERROR", "start jcode: "+err.Error(), start)
		return
	}

	conn := acp.NewClientSideConnection(&client{rec: rec, sandbox: *cwd}, stdin, stdout)

	// Phase 1: initialize + new session, under a setup timeout.
	setupCtx, setupCancel := context.WithTimeout(context.Background(), time.Duration(*setupSec)*time.Second)
	defer setupCancel()

	initResp, err := conn.Initialize(setupCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientInfo:      &acp.Implementation{Name: "jcode-acp-harness", Version: "0.1.0"},
	})
	if err != nil {
		subCancel()
		_ = cmd.Wait()
		finish(rec, result, "INIT_ERROR", "initialize: "+err.Error(), start)
		return
	}
	rec.line("initialized", initResp)

	ns, err := conn.NewSession(setupCtx, acp.NewSessionRequest{Cwd: *cwd, McpServers: []acp.McpServer{}})
	if err != nil {
		subCancel()
		_ = cmd.Wait()
		finish(rec, result, "NEWSESSION_ERROR", "new session: "+err.Error(), start)
		return
	}
	sid := ns.SessionId
	result["sessionId"] = string(sid)
	rec.line("session_new", map[string]any{"sessionId": string(sid)})

	if *modeStr != "" {
		if _, merr := conn.SetSessionMode(setupCtx, acp.SetSessionModeRequest{SessionId: sid, ModeId: acp.SessionModeId(*modeStr)}); merr != nil {
			rec.line("setmode_error", map[string]any{"err": merr.Error()})
		}
	}

	// Phase 2: run the prompt turn under a wall-clock timeout.
	promptCtx, promptCancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	defer promptCancel()

	presp, perr := conn.Prompt(promptCtx, acp.PromptRequest{
		SessionId: sid,
		Prompt:    []acp.ContentBlock{acp.TextBlock(*promptStr)},
	})
	elapsed := time.Since(start)

	// Attach summary counters gathered from the stream.
	rec.mu.Lock()
	result["tool_calls"] = rec.toolCalls
	result["tool_updates"] = rec.toolUpdates
	result["tool_names"] = rec.toolNames
	result["tool_kind"] = rec.toolKind
	result["tool_titles"] = rec.toolTitles
	result["tool_status_end"] = rec.toolStatusEnd
	result["thought_chunks"] = rec.thoughtChunks
	result["agent_chunks"] = rec.agentChunks
	result["agent_text_len"] = rec.agentTextLen
	result["permission_reqs"] = rec.permissionN
	result["plans"] = rec.plans
	result["final_text"] = string(rec.finalText)
	if rec.lastUsage != nil {
		result["usage_update"] = json.RawMessage(rec.lastUsage)
	}
	rec.mu.Unlock()

	result["elapsed_ms"] = elapsed.Milliseconds()

	switch {
	case perr != nil && promptCtx.Err() == context.DeadlineExceeded:
		result["stop_reason"] = "TIMEOUT"
		result["error"] = fmt.Sprintf("prompt turn exceeded %ds wall-clock", *timeoutSec)
		// best-effort graceful cancel before killing
		_ = conn.Cancel(context.Background(), acp.CancelNotification{SessionId: sid})
	case perr != nil:
		result["stop_reason"] = "PROMPT_ERROR"
		result["error"] = perr.Error()
	default:
		result["stop_reason"] = string(presp.StopReason)
		if presp.Usage != nil {
			result["prompt_usage"] = presp.Usage
		}
	}

	rec.line("result", result)

	// Tear down the subprocess.
	subCancel()
	_ = cmd.Wait()

	out, _ := json.Marshal(result)
	fmt.Println(string(out))
}

// finish attaches a terminal status + elapsed to result, logs it, and prints it.
func finish(rec *recorder, result map[string]any, stop, errMsg string, start time.Time) {
	result["stop_reason"] = stop
	if errMsg != "" {
		result["error"] = errMsg
	}
	result["elapsed_ms"] = time.Since(start).Milliseconds()
	rec.line("result", result)
	out, _ := json.Marshal(result)
	fmt.Println(string(out))
}

// die prints a result JSON to stdout and exits 0 (status travels in the JSON).
func die(result map[string]any) {
	out, _ := json.Marshal(result)
	fmt.Println(string(out))
	os.Exit(0)
}
