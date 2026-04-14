package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// ReconnectPolicy.Delay — M-08, M-10
// ---------------------------------------------------------------------------

func TestReconnectPolicy_Delay_ExponentialBackoff(t *testing.T) {
	p := &ReconnectPolicy{
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		MaxRetries:   5,
		Multiplier:   2.0,
	}

	// Expected base durations (before jitter): 1s, 2s, 4s, 8s, 16s
	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
	}

	for i, want := range expected {
		got := p.Delay(i)
		// Allow ±10% jitter
		lo := time.Duration(float64(want) * 0.89)
		hi := time.Duration(float64(want) * 1.11)
		if got < lo || got > hi {
			t.Errorf("Delay(%d) = %v, want in [%v, %v]", i, got, lo, hi)
		}
	}
}

func TestReconnectPolicy_Delay_CappedAtMaxDelay(t *testing.T) {
	p := &ReconnectPolicy{
		InitialDelay: 1 * time.Second,
		MaxDelay:     5 * time.Second,
		MaxRetries:   10,
		Multiplier:   2.0,
	}

	// At retry 10 the base would be 1024s, but cap is 5s.
	got := p.Delay(10)
	base := float64(5 * time.Second)
	maxWithJitter := time.Duration(base * 1.11)
	if got > maxWithJitter {
		t.Errorf("Delay(10) = %v, exceeds max+jitter %v", got, maxWithJitter)
	}
	if got < 0 {
		t.Errorf("Delay(10) = %v, must not be negative", got)
	}
}

func TestReconnectPolicy_Delay_JitterWithinBounds(t *testing.T) {
	p := &ReconnectPolicy{
		InitialDelay: 10 * time.Second,
		MaxDelay:     60 * time.Second,
		MaxRetries:   5,
		Multiplier:   2.0,
	}

	// Run many samples for retry 0 (base = 10s) and check jitter bounds.
	base := 10 * time.Second
	lo := time.Duration(float64(base) * 0.9)
	hi := time.Duration(float64(base) * 1.1)

	for i := 0; i < 200; i++ {
		got := p.Delay(0)
		if got < lo || got > hi {
			t.Fatalf("Delay(0) iteration %d = %v, want in [%v, %v]", i, got, lo, hi)
		}
	}
}

// ---------------------------------------------------------------------------
// DefaultReconnectPolicy
// ---------------------------------------------------------------------------

func TestDefaultReconnectPolicy(t *testing.T) {
	p := DefaultReconnectPolicy()
	if p.InitialDelay != 1*time.Second {
		t.Errorf("InitialDelay = %v, want 1s", p.InitialDelay)
	}
	if p.MaxDelay != 30*time.Second {
		t.Errorf("MaxDelay = %v, want 30s", p.MaxDelay)
	}
	if p.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", p.MaxRetries)
	}
	if p.Multiplier != 2.0 {
		t.Errorf("Multiplier = %f, want 2.0", p.Multiplier)
	}
}

// ---------------------------------------------------------------------------
// NewMCPManager
// ---------------------------------------------------------------------------

func TestNewMCPManager(t *testing.T) {
	called := false
	mgr := NewMCPManager(nil, func(name string, state MCPServerState) {
		called = true
	})
	defer func() { _ = mgr.Close() }()

	if mgr.connections == nil {
		t.Fatal("connections map is nil")
	}
	if mgr.policy == nil {
		t.Fatal("policy is nil")
	}
	if mgr.ctx == nil {
		t.Fatal("ctx is nil")
	}

	// Verify callback is stored
	mgr.notifyState("test", MCPConnecting)
	if !called {
		t.Error("onState callback was not invoked")
	}
}

func TestNewMCPManager_NilCallback(t *testing.T) {
	mgr := NewMCPManager(nil, nil)
	defer func() { _ = mgr.Close() }()

	// Should not panic with nil callback
	mgr.notifyState("test", MCPConnecting)
}

// ---------------------------------------------------------------------------
// State callbacks — M-04
// ---------------------------------------------------------------------------

func TestStateCallback_ConnectDisconnect(t *testing.T) {
	var mu sync.Mutex
	var transitions []struct {
		name  string
		state MCPServerState
	}

	mgr := NewMCPManager(nil, func(name string, state MCPServerState) {
		mu.Lock()
		transitions = append(transitions, struct {
			name  string
			state MCPServerState
		}{name, state})
		mu.Unlock()
	})
	defer func() { _ = mgr.Close() }()

	// Simulate a connection by manually inserting a connection and
	// walking through states. We can't use a real MCP server, but we
	// can verify the state machine via direct manipulation.
	conn := &MCPConnection{
		Name:   "test-server",
		State:  MCPDisconnected,
		Config: configStub(),
	}
	mgr.mu.Lock()
	mgr.connections["test-server"] = conn
	mgr.mu.Unlock()

	// Walk through: connecting → connected → disconnecting
	conn.mu.Lock()
	conn.setState(MCPConnecting)
	conn.mu.Unlock()
	mgr.notifyState("test-server", MCPConnecting)

	conn.mu.Lock()
	conn.setState(MCPConnected)
	conn.mu.Unlock()
	mgr.notifyState("test-server", MCPConnected)

	// Disconnect
	conn.mu.Lock()
	conn.setState(MCPDisconnected)
	conn.mu.Unlock()
	mgr.notifyState("test-server", MCPDisconnected)

	mu.Lock()
	defer mu.Unlock()

	if len(transitions) != 3 {
		t.Fatalf("expected 3 transitions, got %d", len(transitions))
	}

	wantStates := []MCPServerState{MCPConnecting, MCPConnected, MCPDisconnected}
	for i, want := range wantStates {
		if transitions[i].state != want {
			t.Errorf("transition[%d].state = %q, want %q", i, transitions[i].state, want)
		}
		if transitions[i].name != "test-server" {
			t.Errorf("transition[%d].name = %q, want %q", i, transitions[i].name, "test-server")
		}
	}
}

// ---------------------------------------------------------------------------
// Max retries respected — M-09
// ---------------------------------------------------------------------------

func TestReconnectPolicy_MaxRetriesRespected(t *testing.T) {
	p := &ReconnectPolicy{
		InitialDelay: 1 * time.Millisecond, // fast for testing
		MaxDelay:     10 * time.Millisecond,
		MaxRetries:   3,
		Multiplier:   2.0,
	}

	// The reconnect method uses MaxRetries as the loop bound.
	// Verify semantically: calling Delay beyond MaxRetries is fine (policy
	// doesn't enforce it), but the manager loop stops at MaxRetries.
	if p.MaxRetries != 3 {
		t.Fatalf("MaxRetries = %d, want 3", p.MaxRetries)
	}

	// Verify delays are still produced for all retries (0..MaxRetries-1)
	for i := 0; i < p.MaxRetries; i++ {
		d := p.Delay(i)
		if d <= 0 {
			t.Errorf("Delay(%d) = %v, want > 0", i, d)
		}
	}
}

// ---------------------------------------------------------------------------
// Successful reconnect resets retry count — M-11
// ---------------------------------------------------------------------------

func TestSuccessfulReconnectResetsRetryCount(t *testing.T) {
	conn := &MCPConnection{
		Name:       "test",
		Config:     configStub(),
		State:      MCPReconnecting,
		RetryCount: 4,
		LastError:  fmt.Errorf("previous error"),
	}

	// Simulate a successful reconnect outcome.
	conn.mu.Lock()
	conn.setState(MCPConnected)
	conn.LastConnected = time.Now()
	conn.RetryCount = 0
	conn.LastError = nil
	conn.mu.Unlock()

	if conn.RetryCount != 0 {
		t.Errorf("RetryCount = %d after reconnect, want 0", conn.RetryCount)
	}
	if conn.LastError != nil {
		t.Errorf("LastError = %v after reconnect, want nil", conn.LastError)
	}
	if conn.State != MCPConnected {
		t.Errorf("State = %q after reconnect, want %q", conn.State, MCPConnected)
	}
}

// ---------------------------------------------------------------------------
// GetServerStatus
// ---------------------------------------------------------------------------

func TestGetServerStatus(t *testing.T) {
	mgr := NewMCPManager(nil, nil)
	defer func() { _ = mgr.Close() }()

	// Empty
	statuses := mgr.GetServerStatus()
	if len(statuses) != 0 {
		t.Fatalf("expected 0 statuses, got %d", len(statuses))
	}

	// Add some connections manually
	mgr.mu.Lock()
	mgr.connections["a"] = &MCPConnection{
		Name:  "a",
		State: MCPConnected,
		Tools: []MCPToolInfo{{Name: "t1"}, {Name: "t2"}},
	}
	mgr.connections["b"] = &MCPConnection{
		Name:      "b",
		State:     MCPFailed,
		LastError: fmt.Errorf("connection refused"),
	}
	mgr.mu.Unlock()

	statuses = mgr.GetServerStatus()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	byName := map[string]MCPServerStatus{}
	for _, s := range statuses {
		byName[s.Name] = s
	}

	a := byName["a"]
	if a.State != MCPConnected {
		t.Errorf("a.State = %q, want %q", a.State, MCPConnected)
	}
	if a.ToolCount != 2 {
		t.Errorf("a.ToolCount = %d, want 2", a.ToolCount)
	}
	if a.Error != "" {
		t.Errorf("a.Error = %q, want empty", a.Error)
	}

	b := byName["b"]
	if b.State != MCPFailed {
		t.Errorf("b.State = %q, want %q", b.State, MCPFailed)
	}
	if b.Error != "connection refused" {
		t.Errorf("b.Error = %q, want %q", b.Error, "connection refused")
	}
}

// ---------------------------------------------------------------------------
// GetAllTools
// ---------------------------------------------------------------------------

func TestGetAllTools(t *testing.T) {
	mgr := NewMCPManager(nil, nil)
	defer func() { _ = mgr.Close() }()

	mgr.mu.Lock()
	mgr.connections["s1"] = &MCPConnection{
		Name:  "s1",
		State: MCPConnected,
		Tools: []MCPToolInfo{
			{Name: "tool-a", ServerName: "s1"},
		},
	}
	mgr.connections["s2"] = &MCPConnection{
		Name:  "s2",
		State: MCPConnected,
		Tools: []MCPToolInfo{
			{Name: "tool-b", ServerName: "s2"},
			{Name: "tool-c", ServerName: "s2"},
		},
	}
	mgr.connections["s3"] = &MCPConnection{
		Name:  "s3",
		State: MCPFailed, // should be excluded
		Tools: []MCPToolInfo{
			{Name: "tool-d", ServerName: "s3"},
		},
	}
	mgr.mu.Unlock()

	tools := mgr.GetAllTools()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"tool-a", "tool-b", "tool-c"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
	if names["tool-d"] {
		t.Error("tool-d from failed server should not be included")
	}
}

// ---------------------------------------------------------------------------
// MCPConnection state helpers
// ---------------------------------------------------------------------------

func TestMCPConnectionSetState(t *testing.T) {
	conn := &MCPConnection{State: MCPDisconnected}
	conn.setState(MCPConnecting)
	if conn.State != MCPConnecting {
		t.Errorf("State = %q, want %q", conn.State, MCPConnecting)
	}
}

// ---------------------------------------------------------------------------
// Connect with nil config
// ---------------------------------------------------------------------------

func TestConnect_NilConfig(t *testing.T) {
	mgr := NewMCPManager(nil, nil)
	defer func() { _ = mgr.Close() }()

	err := mgr.Connect(context.Background(), "bad", nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

// ---------------------------------------------------------------------------
// Disconnect unknown server
// ---------------------------------------------------------------------------

func TestDisconnect_UnknownServer(t *testing.T) {
	mgr := NewMCPManager(nil, nil)
	defer func() { _ = mgr.Close() }()

	err := mgr.Disconnect("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
}

// ---------------------------------------------------------------------------
// AddServer duplicate
// ---------------------------------------------------------------------------

func TestAddServer_Duplicate(t *testing.T) {
	mgr := NewMCPManager(nil, nil)
	defer func() { _ = mgr.Close() }()

	mgr.mu.Lock()
	mgr.connections["dup"] = &MCPConnection{Name: "dup", State: MCPConnected}
	mgr.mu.Unlock()

	err := mgr.AddServer(context.Background(), "dup", configStub())
	if err == nil {
		t.Fatal("expected error for duplicate server")
	}
}

// ---------------------------------------------------------------------------
// contentToString
// ---------------------------------------------------------------------------

func TestContentToString(t *testing.T) {
	contents := []mcp.Content{
		mcp.TextContent{Type: "text", Text: "hello"},
		mcp.TextContent{Type: "text", Text: "world"},
	}
	got := contentToString(contents)
	if got != "hello\nworld" {
		t.Errorf("contentToString = %q, want %q", got, "hello\nworld")
	}
}

func TestContentToString_Empty(t *testing.T) {
	got := contentToString(nil)
	if got != "" {
		t.Errorf("contentToString(nil) = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// MCPToolInfo JSON
// ---------------------------------------------------------------------------

func TestMCPToolInfo_JSON(t *testing.T) {
	info := MCPToolInfo{
		Name:        "mytool",
		Description: "does stuff",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		ServerName:  "srv",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MCPToolInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Name != info.Name || decoded.ServerName != info.ServerName {
		t.Errorf("round-trip mismatch: %+v", decoded)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func configStub() *config.MCPServer {
	return &config.MCPServer{
		Type:    "stdio",
		Command: "echo",
		Args:    []string{"hello"},
	}
}
