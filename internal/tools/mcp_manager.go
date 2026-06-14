package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// Connection State
// ---------------------------------------------------------------------------

// MCPServerState represents the connection state of an MCP server.
type MCPServerState string

const (
	MCPDisconnected MCPServerState = "disconnected"
	MCPConnecting   MCPServerState = "connecting"
	MCPConnected    MCPServerState = "connected"
	MCPReconnecting MCPServerState = "reconnecting"
	MCPFailed       MCPServerState = "failed"
)

// ---------------------------------------------------------------------------
// Info / Status types
// ---------------------------------------------------------------------------

// MCPToolInfo describes a tool exposed by an MCP server.
type MCPToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	ServerName  string          `json:"server_name"`
}

// MCPCapabilities summarises what a server supports.
type MCPCapabilities struct {
	Tools     bool `json:"tools"`
	Resources bool `json:"resources"`
	Prompts   bool `json:"prompts"`
}

// MCPServerStatus is a snapshot of one server connection.
type MCPServerStatus struct {
	Name      string         `json:"name"`
	State     MCPServerState `json:"state"`
	ToolCount int            `json:"tool_count"`
	Error     string         `json:"error,omitempty"`
}

// MCPResource describes a resource exposed by an MCP server.
type MCPResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ---------------------------------------------------------------------------
// ReconnectPolicy
// ---------------------------------------------------------------------------

// ReconnectPolicy configures exponential backoff for reconnection attempts.
type ReconnectPolicy struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxRetries   int
	Multiplier   float64
}

// DefaultReconnectPolicy returns a sensible default policy.
func DefaultReconnectPolicy() *ReconnectPolicy {
	return &ReconnectPolicy{
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		MaxRetries:   5,
		Multiplier:   2.0,
	}
}

// Delay returns the backoff duration for the given retry count (0-based),
// capped at MaxDelay, with ±10 % jitter.
func (p *ReconnectPolicy) Delay(retryCount int) time.Duration {
	d := float64(p.InitialDelay) * math.Pow(p.Multiplier, float64(retryCount))
	if d > float64(p.MaxDelay) {
		d = float64(p.MaxDelay)
	}
	// ±10 % jitter
	jitter := d * 0.1 * (2*rand.Float64() - 1) // range [-0.1d, +0.1d]
	d += jitter
	if d < 0 {
		d = 0
	}
	return time.Duration(d)
}

// ---------------------------------------------------------------------------
// MCPConnection
// ---------------------------------------------------------------------------

// MCPConnection tracks a single MCP server.
type MCPConnection struct {
	Name          string
	Config        *config.MCPServer
	State         MCPServerState
	Client        *client.Client
	Tools         []MCPToolInfo
	Capabilities  *MCPCapabilities
	RetryCount    int
	LastError     error
	LastConnected time.Time
	mu            sync.RWMutex
}

func (c *MCPConnection) setState(s MCPServerState) {
	c.State = s
}

// ---------------------------------------------------------------------------
// MCPManager
// ---------------------------------------------------------------------------

// MCPManager manages multiple MCP server connections with reconnection,
// dynamic add/remove, resource discovery and tool invocation.
type MCPManager struct {
	connections map[string]*MCPConnection
	tokenStore  *TokenStore
	policy      *ReconnectPolicy
	onState     func(name string, state MCPServerState)
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewMCPManager creates an MCPManager.
// tokenStore may be nil. onState may be nil (no callbacks).
func NewMCPManager(tokenStore *TokenStore, onState func(string, MCPServerState)) *MCPManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &MCPManager{
		connections: make(map[string]*MCPConnection),
		tokenStore:  tokenStore,
		policy:      DefaultReconnectPolicy(),
		onState:     onState,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// notifyState fires the optional state callback.
func (m *MCPManager) notifyState(name string, state MCPServerState) {
	if m.onState != nil {
		m.onState(name, state)
	}
}

// Connect creates a client for the given server config, starts it,
// initialises the MCP session and discovers tools and capabilities.
func (m *MCPManager) Connect(ctx context.Context, name string, cfg *config.MCPServer) error {
	if cfg == nil {
		return fmt.Errorf("mcp_manager: nil config for server %q", name)
	}

	conn := &MCPConnection{
		Name:   name,
		Config: cfg,
		State:  MCPConnecting,
	}

	m.mu.Lock()
	m.connections[name] = conn
	m.mu.Unlock()

	m.notifyState(name, MCPConnecting)

	if err := m.doConnect(ctx, conn); err != nil {
		conn.mu.Lock()
		conn.setState(MCPDisconnected)
		conn.LastError = err
		conn.mu.Unlock()
		m.notifyState(name, MCPDisconnected)
		return fmt.Errorf("mcp_manager: connect %q: %w", name, err)
	}

	conn.mu.Lock()
	conn.setState(MCPConnected)
	conn.LastConnected = time.Now()
	conn.RetryCount = 0
	conn.LastError = nil
	conn.mu.Unlock()

	m.notifyState(name, MCPConnected)
	config.Logger().Printf("mcp_manager: connected to %q (%d tools)", name, len(conn.Tools))
	return nil
}

// doConnect performs the low-level create → start → init → discover sequence.
func (m *MCPManager) doConnect(ctx context.Context, conn *MCPConnection) error {
	cfg := conn.Config

	cli, err := buildMCPClient(conn.Name, cfg)
	if err != nil {
		return fmt.Errorf("client create: %w", err)
	}

	// Ensure the client is closed on any subsequent failure.
	success := false
	defer func() {
		if !success {
			_ = cli.Close()
		}
	}()

	if err := cli.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "jcode",
		Version: "2.0.0",
	}

	initResult, err := cli.Initialize(ctx, initReq)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// Parse capabilities.
	caps := &MCPCapabilities{
		Tools:     initResult.Capabilities.Tools != nil,
		Resources: initResult.Capabilities.Resources != nil,
		Prompts:   initResult.Capabilities.Prompts != nil,
	}

	// Discover tools.
	var tools []MCPToolInfo
	if caps.Tools {
		toolsResult, err := cli.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			config.Logger().Printf("mcp_manager: list tools for %q: %v", conn.Name, err)
		} else {
			for _, t := range toolsResult.Tools {
				schema, _ := json.Marshal(t.InputSchema)
				tools = append(tools, MCPToolInfo{
					Name:        t.Name,
					Description: t.Description,
					InputSchema: schema,
					ServerName:  conn.Name,
				})
			}
		}
	}

	conn.mu.Lock()
	conn.Client = cli
	conn.Tools = tools
	conn.Capabilities = caps
	conn.mu.Unlock()

	success = true
	return nil
}

// Disconnect gracefully closes a server connection and removes it.
func (m *MCPManager) Disconnect(name string) error {
	m.mu.Lock()
	conn, ok := m.connections[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("mcp_manager: server %q not found", name)
	}
	delete(m.connections, name)
	m.mu.Unlock()

	conn.mu.Lock()
	cli := conn.Client
	conn.Client = nil
	conn.Tools = nil
	conn.setState(MCPDisconnected)
	conn.mu.Unlock()

	m.notifyState(name, MCPDisconnected)

	if cli != nil {
		if err := cli.Close(); err != nil {
			config.Logger().Printf("mcp_manager: close %q: %v", name, err)
			return err
		}
	}
	return nil
}

// AddServer adds and connects a new MCP server at runtime.
func (m *MCPManager) AddServer(ctx context.Context, name string, cfg *config.MCPServer) error {
	m.mu.RLock()
	_, exists := m.connections[name]
	m.mu.RUnlock()
	if exists {
		return fmt.Errorf("mcp_manager: server %q already exists", name)
	}
	return m.Connect(ctx, name, cfg)
}

// RemoveServer disconnects and removes a server.
func (m *MCPManager) RemoveServer(name string) error {
	return m.Disconnect(name)
}

// GetAllTools returns tools from all connected servers.
func (m *MCPManager) GetAllTools() []MCPToolInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []MCPToolInfo
	for _, conn := range m.connections {
		conn.mu.RLock()
		if conn.State == MCPConnected {
			all = append(all, conn.Tools...)
		}
		conn.mu.RUnlock()
	}
	return all
}

// GetServerStatus returns a snapshot of all server statuses.
func (m *MCPManager) GetServerStatus() []MCPServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]MCPServerStatus, 0, len(m.connections))
	for _, conn := range m.connections {
		conn.mu.RLock()
		s := MCPServerStatus{
			Name:      conn.Name,
			State:     conn.State,
			ToolCount: len(conn.Tools),
		}
		if conn.LastError != nil {
			s.Error = conn.LastError.Error()
		}
		conn.mu.RUnlock()
		out = append(out, s)
	}
	return out
}

// InvokeMCPTool calls a tool on the named server. If the call fails it
// attempts one reconnection before returning an error.
func (m *MCPManager) InvokeMCPTool(ctx context.Context, serverName, toolName string, args json.RawMessage) (string, error) {
	conn, err := m.getConnection(serverName)
	if err != nil {
		return "", err
	}

	result, err := m.callTool(ctx, conn, toolName, args)
	if err == nil {
		return result, nil
	}

	// Attempt one reconnection and retry.
	config.Logger().Printf("mcp_manager: tool %q on %q failed, reconnecting: %v", toolName, serverName, err)

	if reconnErr := m.reconnectOnce(ctx, conn); reconnErr != nil {
		return "", fmt.Errorf("mcp_manager: reconnect %q failed: %w (original: %v)", serverName, reconnErr, err)
	}

	return m.callTool(ctx, conn, toolName, args)
}

func (m *MCPManager) callTool(ctx context.Context, conn *MCPConnection, toolName string, args json.RawMessage) (string, error) {
	conn.mu.RLock()
	cli := conn.Client
	state := conn.State
	conn.mu.RUnlock()

	if cli == nil || state != MCPConnected {
		return "", fmt.Errorf("server %q not connected (state=%s)", conn.Name, state)
	}

	var arguments any
	if len(args) > 0 {
		var m map[string]any
		if err := json.Unmarshal(args, &m); err != nil {
			return "", fmt.Errorf("invalid arguments JSON: %w", err)
		}
		arguments = m
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = arguments

	result, err := cli.CallTool(ctx, req)
	if err != nil {
		return "", err
	}

	if result.IsError {
		return "", fmt.Errorf("tool %q returned error: %s", toolName, contentToString(result.Content))
	}

	return contentToString(result.Content), nil
}

// contentToString serialises MCP Content slices to a single string.
func contentToString(contents []mcp.Content) string {
	var parts []string
	for _, c := range contents {
		switch v := c.(type) {
		case mcp.TextContent:
			parts = append(parts, v.Text)
		default:
			b, _ := json.Marshal(c)
			parts = append(parts, string(b))
		}
	}
	return strings.Join(parts, "\n")
}

// reconnectOnce performs a single reconnection attempt for a connection.
func (m *MCPManager) reconnectOnce(ctx context.Context, conn *MCPConnection) error {
	conn.mu.Lock()
	if conn.Client != nil {
		_ = conn.Client.Close()
		conn.Client = nil
	}
	conn.setState(MCPReconnecting)
	conn.mu.Unlock()
	m.notifyState(conn.Name, MCPReconnecting)

	if err := m.doConnect(ctx, conn); err != nil {
		conn.mu.Lock()
		conn.LastError = err
		conn.setState(MCPDisconnected)
		conn.mu.Unlock()
		m.notifyState(conn.Name, MCPDisconnected)
		return err
	}

	conn.mu.Lock()
	conn.setState(MCPConnected)
	conn.LastConnected = time.Now()
	conn.RetryCount = 0
	conn.LastError = nil
	conn.mu.Unlock()
	m.notifyState(conn.Name, MCPConnected)
	return nil
}

// getConnection looks up a connection by name.
func (m *MCPManager) getConnection(name string) (*MCPConnection, error) {
	m.mu.RLock()
	conn, ok := m.connections[name]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("mcp_manager: server %q not found", name)
	}
	return conn, nil
}

// ---------------------------------------------------------------------------
// Resource Discovery
// ---------------------------------------------------------------------------

// ListResources returns the resources advertised by the named server.
func (m *MCPManager) ListResources(ctx context.Context, serverName string) ([]MCPResource, error) {
	conn, err := m.getConnection(serverName)
	if err != nil {
		return nil, err
	}

	conn.mu.RLock()
	cli := conn.Client
	caps := conn.Capabilities
	conn.mu.RUnlock()

	if caps == nil || !caps.Resources {
		return nil, fmt.Errorf("server %q does not support resources", serverName)
	}
	if cli == nil {
		return nil, fmt.Errorf("server %q not connected", serverName)
	}

	result, err := cli.ListResources(ctx, mcp.ListResourcesRequest{})
	if err != nil {
		return nil, fmt.Errorf("list resources on %q: %w", serverName, err)
	}

	out := make([]MCPResource, 0, len(result.Resources))
	for _, r := range result.Resources {
		out = append(out, MCPResource{
			URI:         r.URI,
			Name:        r.Name,
			Description: r.Description,
			MimeType:    r.MIMEType,
		})
	}
	return out, nil
}

// ReadResource reads a single resource from the named server.
func (m *MCPManager) ReadResource(ctx context.Context, serverName, uri string) (string, error) {
	conn, err := m.getConnection(serverName)
	if err != nil {
		return "", err
	}

	conn.mu.RLock()
	cli := conn.Client
	caps := conn.Capabilities
	conn.mu.RUnlock()

	if caps == nil || !caps.Resources {
		return "", fmt.Errorf("server %q does not support resources", serverName)
	}
	if cli == nil {
		return "", fmt.Errorf("server %q not connected", serverName)
	}

	req := mcp.ReadResourceRequest{}
	req.Params.URI = uri

	result, err := cli.ReadResource(ctx, req)
	if err != nil {
		return "", fmt.Errorf("read resource %q on %q: %w", uri, serverName, err)
	}

	var parts []string
	for _, c := range result.Contents {
		switch v := c.(type) {
		case mcp.TextResourceContents:
			parts = append(parts, v.Text)
		case mcp.BlobResourceContents:
			parts = append(parts, v.Blob)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

// Close disconnects all servers and cancels the manager context.
func (m *MCPManager) Close() error {
	m.cancel()

	m.mu.Lock()
	names := make([]string, 0, len(m.connections))
	for name := range m.connections {
		names = append(names, name)
	}
	m.mu.Unlock()

	var firstErr error
	for _, name := range names {
		if err := m.Disconnect(name); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
