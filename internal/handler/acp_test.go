package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

func TestACPToolPresentationReadUsesFriendlyTitleAndAbsoluteLocation(t *testing.T) {
	workDir := filepath.Join(string(filepath.Separator), "tmp", "jcode-work")
	h := NewACPHandler(nil, "sess", workDir)

	p := h.presentationForTool("read", `{"file_path":"internal/handler/acp.go","offset":10,"limit":5}`)

	if p.Title != "Read internal/handler/acp.go (10-14)" {
		t.Fatalf("title = %q", p.Title)
	}
	if p.Kind != acp.ToolKindRead {
		t.Fatalf("kind = %q", p.Kind)
	}
	if len(p.Locations) != 1 {
		t.Fatalf("locations len = %d", len(p.Locations))
	}
	wantPath := filepath.Join(workDir, "internal", "handler", "acp.go")
	if p.Locations[0].Path != wantPath {
		t.Fatalf("location path = %q, want %q", p.Locations[0].Path, wantPath)
	}
	if p.Locations[0].Line == nil || *p.Locations[0].Line != 10 {
		t.Fatalf("location line = %v, want 10", p.Locations[0].Line)
	}
}

func TestACPToolPresentationSearchAndExecute(t *testing.T) {
	h := NewACPHandler(nil, "sess", "/repo")

	grep := h.presentationForTool("grep", `{"pattern":"ToolCall","path":"internal"}`)
	if grep.Title != `Search "ToolCall" in internal` {
		t.Fatalf("grep title = %q", grep.Title)
	}
	if grep.Kind != acp.ToolKindSearch {
		t.Fatalf("grep kind = %q", grep.Kind)
	}

	exec := h.presentationForTool("execute", `{"command":"go test ./...","description":"Run all tests"}`)
	if exec.Title != "Run all tests" {
		t.Fatalf("execute title = %q", exec.Title)
	}
	if exec.Kind != acp.ToolKindExecute {
		t.Fatalf("execute kind = %q", exec.Kind)
	}
}

func TestACPToolPresentationWriteIncludesDiffContent(t *testing.T) {
	h := NewACPHandler(nil, "sess", "/repo")

	p := h.presentationForTool("write", `{"file_path":"README.md","content":"hello"}`)

	if p.Title != "Write README.md" {
		t.Fatalf("title = %q", p.Title)
	}
	if len(p.Content) != 1 || p.Content[0].Diff == nil {
		t.Fatalf("expected one diff content item, got %#v", p.Content)
	}
	if p.Content[0].Diff.Path != "README.md" {
		t.Fatalf("diff path = %q", p.Content[0].Diff.Path)
	}
}

func TestACPSubagentNameFromArgs(t *testing.T) {
	if got := subagentNameFromArgs(`{"name":"scan-repo","prompt":"..."}`); got != "scan-repo" {
		t.Fatalf("name = %q, want scan-repo", got)
	}
	if got := subagentNameFromArgs(`not json`); got != "" {
		t.Fatalf("name = %q, want empty for invalid JSON", got)
	}
	if got := subagentNameFromArgs(`{"prompt":"..."}`); got != "" {
		t.Fatalf("name = %q, want empty when absent", got)
	}
}

func TestACPSubagentProgressLine(t *testing.T) {
	if got := subagentProgressLine("tool_call", "grep", `{"pattern":"foo"}`); got != `→ grep {"pattern":"foo"}` {
		t.Fatalf("tool_call line = %q", got)
	}
	if got := subagentProgressLine("tool_result", "read", "line one\nline two"); got != "← read line one line two" {
		t.Fatalf("tool_result line = %q", got)
	}
	long := strings.Repeat("x", 500)
	if got := subagentProgressLine("tool_result", "read", long); len(got) > 200 {
		t.Fatalf("long detail not truncated: len=%d", len(got))
	}
}

func TestACPSubagentDoneClearsMappingWithoutUpdate(t *testing.T) {
	// nil conn: the test passes only if the done path never touches the
	// connection (it must only clear the progress mapping).
	h := NewACPHandler(nil, "sess", "/repo")
	h.subagentCalls["scan-repo"] = "tc_1"

	h.OnSubagentEvent("scan-repo", "explore", true, "result", nil)

	if _, ok := h.subagentCalls["scan-repo"]; ok {
		t.Fatal("done event did not clear subagent mapping")
	}
	// Unknown subagent progress must be a silent no-op (no conn access).
	h.OnSubagentProgress("scan-repo", "tool_call", "grep", "{}")
}

// stubAgent satisfies acp.Agent with no-ops. Tests using it only exercise the
// outbound notification path, so none of these methods are ever invoked.
type stubAgent struct{}

func (stubAgent) Authenticate(context.Context, acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}
func (stubAgent) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{}, nil
}
func (stubAgent) Logout(context.Context, acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, nil
}
func (stubAgent) Cancel(context.Context, acp.CancelNotification) error { return nil }
func (stubAgent) CloseSession(context.Context, acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	return acp.CloseSessionResponse{}, nil
}
func (stubAgent) ListSessions(context.Context, acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, nil
}
func (stubAgent) NewSession(context.Context, acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	return acp.NewSessionResponse{}, nil
}
func (stubAgent) Prompt(context.Context, acp.PromptRequest) (acp.PromptResponse, error) {
	return acp.PromptResponse{}, nil
}
func (stubAgent) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, nil
}
func (stubAgent) SetSessionConfigOption(context.Context, acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, nil
}
func (stubAgent) SetSessionMode(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}

// toolCallUpdateWire mirrors the session/update notification shape on the wire
// for tool_call_update payloads.
type toolCallUpdateWire struct {
	Method string `json:"method"`
	Params struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate string `json:"sessionUpdate"`
			ToolCallID    string `json:"toolCallId"`
			Status        string `json:"status"`
			Content       []struct {
				Content struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"content"`
		} `json:"update"`
	} `json:"params"`
}

// newWireHandler builds an ACPHandler over a real AgentSideConnection whose
// outbound stream is captured; the returned func reads the next session/update
// notification off the wire.
func newWireHandler(t *testing.T) (*ACPHandler, func() toolCallUpdateWire) {
	t.Helper()
	outR, outW := io.Pipe()
	inR, inW := io.Pipe() // agent-side reader: held open, never written
	t.Cleanup(func() {
		_ = outW.Close()
		_ = inW.Close()
	})

	conn := acp.NewAgentSideConnection(stubAgent{}, outW, inR)
	h := NewACPHandler(conn, "sess", "/repo")

	lines := make(chan []byte, 4)
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
		for sc.Scan() {
			lines <- append([]byte(nil), sc.Bytes()...)
		}
	}()

	next := func() toolCallUpdateWire {
		t.Helper()
		select {
		case raw := <-lines:
			var msg toolCallUpdateWire
			if err := json.Unmarshal(raw, &msg); err != nil {
				t.Fatalf("bad wire JSON %s: %v", raw, err)
			}
			return msg
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for session/update notification")
			return toolCallUpdateWire{}
		}
	}
	return h, next
}

func TestACPSubagentStartSendsInProgressUpdate(t *testing.T) {
	h, next := newWireHandler(t)
	h.subagentCalls["scan-repo"] = "tc_7"

	h.OnSubagentEvent("scan-repo", "explore", false, "", nil)

	msg := next()
	if msg.Method != "session/update" {
		t.Fatalf("method = %q", msg.Method)
	}
	u := msg.Params.Update
	if u.SessionUpdate != "tool_call_update" || u.ToolCallID != "tc_7" {
		t.Fatalf("update = %+v", u)
	}
	if u.Status != string(acp.ToolCallStatusInProgress) {
		t.Fatalf("status = %q, want in_progress", u.Status)
	}
	if len(u.Content) != 1 || u.Content[0].Content.Text != "explore subagent started" {
		t.Fatalf("content = %+v", u.Content)
	}
}

func TestACPSubagentProgressSendsRollingContent(t *testing.T) {
	h, next := newWireHandler(t)
	h.subagentCalls["scan-repo"] = "tc_7"

	h.OnSubagentProgress("scan-repo", "tool_call", "grep", `{"pattern":"x"}`)

	u := next().Params.Update
	if u.SessionUpdate != "tool_call_update" || u.ToolCallID != "tc_7" {
		t.Fatalf("update = %+v", u)
	}
	if u.Status != "" {
		t.Fatalf("status = %q, progress updates must not touch status", u.Status)
	}
	if len(u.Content) != 1 || u.Content[0].Content.Text != `→ grep {"pattern":"x"}` {
		t.Fatalf("content = %+v", u.Content)
	}
}

func TestACPToolResultClearsStaleSubagentMapping(t *testing.T) {
	h := NewACPHandler(nil, "sess", "/repo")
	h.einoToACP["eino_1"] = "tc_1"
	h.subagentCalls["scan-repo"] = "tc_1"
	// Terminal status already sent (e.g. permission rejection): OnToolResult
	// returns before sending, but must still drop the stale mapping.
	h.toolTerminated["tc_1"] = true

	h.OnToolResult(ToolResultEvent{Name: "subagent", ToolCallID: "eino_1"})

	if _, ok := h.subagentCalls["scan-repo"]; ok {
		t.Fatal("tool result did not clear stale subagent mapping")
	}
}

func TestACPToolFailureOutputDetection(t *testing.T) {
	cases := []string{
		"Tool execution failed: exit status 1",
		"partial output\n\nTool execution failed: exit status 1",
		"Tool execution panicked: boom",
	}
	for _, tc := range cases {
		if !isToolFailureOutput(tc) {
			t.Fatalf("expected failure output for %q", tc)
		}
	}
	if isToolFailureOutput(strings.TrimSpace("command completed")) {
		t.Fatal("did not expect normal output to be treated as failure")
	}
}
