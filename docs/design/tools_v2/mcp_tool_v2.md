# MCP Tool V2 — 完整设计

## 概述

基于 Claude Code 的完整 MCP 协议支持（7种传输 + OAuth + 资源发现 + 重连机制 + 权限），为 jcode 设计等价的 MCP 集成升级方案。

---

## 1. Claude Code 实现深度分析

### 1.1 传输方式

| 传输 | 用途 |
|------|------|
| stdio | 本地子进程 |
| SSE | HTTP Server-Sent Events |
| SSE-IDE | VS Code 扩展代理 |
| HTTP | Streamable HTTP |
| WebSocket | 双向实时通信 |
| SDK | 同进程直接调用 |
| Claude.ai | Claude.ai 代理 |

### 1.2 配置层级

7层配置作用域: local > user > project > dynamic > enterprise > claudeai > managed

### 1.3 OAuth 认证

- RFC 9728 + RFC 8414 授权服务器发现
- OAuth2 授权码流程（PKCE）
- 本地回调服务器 (localhost:port/callback)
- 令牌自动刷新
- Keychain/文件安全存储

### 1.4 资源机制

- `ListMcpResourcesTool`: LRU 缓存，自动重连
- `ReadMcpResourceTool`: 文本/二进制自动处理

### 1.5 连接管理

- 指数退避重连 (1s → 30s, 最多5次)
- 会话恢复
- 实时状态同步
- 能力检测 (capabilities)

---

## 2. jcode 当前实现分析

### 2.1 现状 (internal/tools/mcp.go)

```go
// MCPServer 配置
type MCPServer struct {
    Type    string            // "http", "sse", "stdio"
    Command string
    Args    []string
    Env     []string
    URL     string
    Headers map[string]string
}
```

**局限**:
- 3 种传输 (stdio/SSE/HTTP)
- 启动时加载一次，无重连
- 无 OAuth 认证
- 无资源机制
- 无能力检测
- 无动态连接管理

---

## 3. jcode MCP V2 设计

### 3.1 连接状态机

```go
// MCPServerState 连接状态
type MCPServerState string
const (
    MCPDisconnected  MCPServerState = "disconnected"
    MCPConnecting    MCPServerState = "connecting"
    MCPConnected     MCPServerState = "connected"
    MCPReconnecting  MCPServerState = "reconnecting"
    MCPAuthPending   MCPServerState = "auth_pending"   // OAuth 等待认证
    MCPFailed        MCPServerState = "failed"
)
```

### 3.2 连接管理器

```go
// MCPConnection 单个服务器连接
type MCPConnection struct {
    Name          string
    Config        MCPServerConfig
    State         MCPServerState
    Client        MCPClient          // MCP 协议客户端
    Tools         []MCPToolInfo      // 已发现的工具
    Capabilities  *MCPCapabilities   // 服务器能力
    RetryCount    int
    LastError     error
    LastConnected time.Time
    mu            sync.RWMutex
}

// MCPToolInfo MCP 工具元信息
type MCPToolInfo struct {
    Name        string
    Description string
    InputSchema json.RawMessage  // JSON Schema
    ServerName  string           // 所属服务器
}

// MCPCapabilities 服务器能力声明
type MCPCapabilities struct {
    Tools     bool `json:"tools"`
    Resources bool `json:"resources"`
    Prompts   bool `json:"prompts"`
}

// MCPManager 多服务器连接池
type MCPManager struct {
    connections map[string]*MCPConnection
    tokenStore  *TokenStore
    policy      *ReconnectPolicy
    onState     func(name string, state MCPServerState) // 状态回调
    mu          sync.RWMutex
}

func NewMCPManager(tokenStore *TokenStore, onState func(string, MCPServerState)) *MCPManager {
    return &MCPManager{
        connections: make(map[string]*MCPConnection),
        tokenStore:  tokenStore,
        policy:      DefaultReconnectPolicy(),
        onState:     onState,
    }
}
```

### 3.3 连接生命周期

```go
// Connect 连接到 MCP 服务器
func (m *MCPManager) Connect(ctx context.Context, name string, config MCPServerConfig) error {
    m.mu.Lock()
    conn := &MCPConnection{
        Name:   name,
        Config: config,
        State:  MCPConnecting,
    }
    m.connections[name] = conn
    m.mu.Unlock()
    m.notifyState(name, MCPConnecting)

    // 创建传输层
    transport, err := m.createTransport(config)
    if err != nil {
        conn.State = MCPFailed
        conn.LastError = err
        m.notifyState(name, MCPFailed)
        return err
    }

    // 创建 MCP 客户端
    client, err := NewMCPClient(transport)
    if err != nil {
        conn.State = MCPFailed
        conn.LastError = err
        m.notifyState(name, MCPFailed)
        return err
    }

    // 初始化握手
    caps, err := client.Initialize(ctx)
    if err != nil {
        // 检查是否需要 OAuth
        if isAuthRequired(err) {
            conn.State = MCPAuthPending
            m.notifyState(name, MCPAuthPending)
            return m.handleOAuth(ctx, conn)
        }
        conn.State = MCPFailed
        conn.LastError = err
        m.notifyState(name, MCPFailed)
        return err
    }

    conn.mu.Lock()
    conn.Client = client
    conn.Capabilities = caps
    conn.State = MCPConnected
    conn.LastConnected = time.Now()
    conn.RetryCount = 0
    conn.mu.Unlock()
    m.notifyState(name, MCPConnected)

    // 发现工具
    if caps.Tools {
        tools, _ := client.ListTools(ctx)
        conn.mu.Lock()
        conn.Tools = tools
        conn.mu.Unlock()
    }

    return nil
}

func (m *MCPManager) createTransport(config MCPServerConfig) (Transport, error) {
    switch config.Type {
    case "stdio":
        return NewStdioTransport(config.Command, config.Args, config.Env)
    case "sse":
        return NewSSETransport(config.URL, config.Headers)
    case "http":
        return NewHTTPTransport(config.URL, config.Headers)
    default:
        return nil, fmt.Errorf("unsupported transport type: %s", config.Type)
    }
}
```

### 3.4 重连机制

```go
// ReconnectPolicy 重连策略
type ReconnectPolicy struct {
    InitialDelay time.Duration // 1s
    MaxDelay     time.Duration // 30s
    MaxRetries   int           // 5
    Multiplier   float64       // 2.0
}

func DefaultReconnectPolicy() *ReconnectPolicy {
    return &ReconnectPolicy{
        InitialDelay: 1 * time.Second,
        MaxDelay:     30 * time.Second,
        MaxRetries:   5,
        Multiplier:   2.0,
    }
}

func (p *ReconnectPolicy) Delay(retryCount int) time.Duration {
    delay := float64(p.InitialDelay) * math.Pow(p.Multiplier, float64(retryCount))
    if delay > float64(p.MaxDelay) {
        delay = float64(p.MaxDelay)
    }
    // 添加 jitter (±10%)
    jitter := delay * 0.1 * (2*rand.Float64() - 1)
    return time.Duration(delay + jitter)
}

// reconnect 自动重连
func (m *MCPManager) reconnect(ctx context.Context, conn *MCPConnection) {
    for conn.RetryCount < m.policy.MaxRetries {
        conn.mu.Lock()
        conn.State = MCPReconnecting
        conn.RetryCount++
        conn.mu.Unlock()
        m.notifyState(conn.Name, MCPReconnecting)

        delay := m.policy.Delay(conn.RetryCount - 1)
        config.Logger().Printf("MCP %s: reconnecting in %s (attempt %d/%d)",
            conn.Name, delay, conn.RetryCount, m.policy.MaxRetries)

        select {
        case <-time.After(delay):
        case <-ctx.Done():
            return
        }

        if err := m.Connect(ctx, conn.Name, conn.Config); err == nil {
            return // 重连成功
        }
    }

    conn.mu.Lock()
    conn.State = MCPFailed
    conn.mu.Unlock()
    m.notifyState(conn.Name, MCPFailed)
    config.Logger().Printf("MCP %s: reconnection failed after %d attempts", conn.Name, m.policy.MaxRetries)
}

func (m *MCPManager) notifyState(name string, state MCPServerState) {
    if m.onState != nil {
        m.onState(name, state)
    }
}
```

### 3.5 OAuth 认证

```go
// OAuthFlow OAuth2 PKCE 认证流程
type OAuthFlow struct {
    tokenStore *TokenStore
    callbackPort int
}

func (f *OAuthFlow) Authenticate(ctx context.Context, serverURL string) (*OAuthToken, error) {
    // 1. 发现授权端点
    authMeta, err := discoverAuthServer(serverURL)
    if err != nil {
        return nil, fmt.Errorf("OAuth discovery failed: %w", err)
    }

    // 2. 生成 PKCE code verifier + challenge
    verifier := generateCodeVerifier()
    challenge := generateCodeChallenge(verifier)

    // 3. 启动本地回调服务器
    callbackCh := make(chan string, 1)
    server := startCallbackServer(f.callbackPort, callbackCh)
    defer server.Shutdown(ctx)

    redirectURI := fmt.Sprintf("http://localhost:%d/callback", f.callbackPort)

    // 4. 构建授权 URL
    authURL := buildAuthURL(authMeta.AuthorizationEndpoint, &AuthParams{
        ClientID:            "jcoding",
        RedirectURI:         redirectURI,
        Scope:               "tools resources",
        CodeChallenge:       challenge,
        CodeChallengeMethod: "S256",
        State:               generateState(),
    })

    // 5. 打开浏览器
    fmt.Printf("Opening browser for authentication:\n%s\n", authURL)
    openBrowser(authURL)

    // 6. 等待回调
    var code string
    select {
    case code = <-callbackCh:
    case <-time.After(2 * time.Minute):
        return nil, fmt.Errorf("OAuth timeout: no callback received in 2 minutes")
    case <-ctx.Done():
        return nil, ctx.Err()
    }

    // 7. 交换令牌
    token, err := exchangeToken(authMeta.TokenEndpoint, &TokenParams{
        Code:         code,
        RedirectURI:  redirectURI,
        ClientID:     "jcoding",
        CodeVerifier: verifier,
    })
    if err != nil {
        return nil, fmt.Errorf("token exchange failed: %w", err)
    }

    // 8. 存储令牌
    oauthToken := &OAuthToken{
        AccessToken:  token.AccessToken,
        RefreshToken: token.RefreshToken,
        ExpiresAt:    time.Now().Add(time.Duration(token.ExpiresIn) * time.Second),
        Provider:     serverURL,
    }
    if err := f.tokenStore.Save(serverURL, *oauthToken); err != nil {
        config.Logger().Printf("Failed to save OAuth token: %v", err)
    }

    return oauthToken, nil
}

// refreshToken 刷新过期令牌
func (f *OAuthFlow) refreshToken(ctx context.Context, serverURL string, refreshToken string) (*OAuthToken, error) {
    authMeta, err := discoverAuthServer(serverURL)
    if err != nil {
        return nil, err
    }

    token, err := refreshOAuthToken(authMeta.TokenEndpoint, &RefreshParams{
        RefreshToken: refreshToken,
        ClientID:     "jcoding",
    })
    if err != nil {
        return nil, err
    }

    oauthToken := &OAuthToken{
        AccessToken:  token.AccessToken,
        RefreshToken: token.RefreshToken,
        ExpiresAt:    time.Now().Add(time.Duration(token.ExpiresIn) * time.Second),
        Provider:     serverURL,
    }
    f.tokenStore.Save(serverURL, *oauthToken)
    return oauthToken, nil
}

func isAuthRequired(err error) bool {
    // 检查 401/403 或 MCP 认证错误
    return strings.Contains(err.Error(), "401") ||
        strings.Contains(err.Error(), "authentication required") ||
        strings.Contains(err.Error(), "-32042") // MCP auth error code
}
```

### 3.6 资源发现与读取

```go
// MCPResource MCP 资源
type MCPResource struct {
    URI         string `json:"uri"`
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    MimeType    string `json:"mimeType,omitempty"`
}

// ListResources 列出服务器资源
func (conn *MCPConnection) ListResources(ctx context.Context) ([]MCPResource, error) {
    if conn.Capabilities == nil || !conn.Capabilities.Resources {
        return nil, fmt.Errorf("server does not support resources")
    }
    return conn.Client.ListResources(ctx)
}

// ReadResource 读取资源内容
func (conn *MCPConnection) ReadResource(ctx context.Context, uri string) (string, error) {
    return conn.Client.ReadResource(ctx, uri)
}
```

### 3.7 动态连接管理

```go
// AddServer 运行时添加服务器
func (m *MCPManager) AddServer(ctx context.Context, name string, config MCPServerConfig) error {
    return m.Connect(ctx, name, config)
}

// RemoveServer 运行时移除服务器
func (m *MCPManager) RemoveServer(name string) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    conn, ok := m.connections[name]
    if !ok {
        return fmt.Errorf("server %s not found", name)
    }

    if conn.Client != nil {
        conn.Client.Close()
    }
    delete(m.connections, name)
    m.notifyState(name, MCPDisconnected)
    return nil
}

// GetAllTools 获取所有已连接服务器的工具
func (m *MCPManager) GetAllTools() []MCPToolInfo {
    m.mu.RLock()
    defer m.mu.RUnlock()

    var tools []MCPToolInfo
    for _, conn := range m.connections {
        conn.mu.RLock()
        if conn.State == MCPConnected {
            tools = append(tools, conn.Tools...)
        }
        conn.mu.RUnlock()
    }
    return tools
}

// GetServerStatus 获取所有服务器状态
func (m *MCPManager) GetServerStatus() []MCPServerStatus {
    m.mu.RLock()
    defer m.mu.RUnlock()

    var status []MCPServerStatus
    for name, conn := range m.connections {
        conn.mu.RLock()
        status = append(status, MCPServerStatus{
            Name:      name,
            State:     conn.State,
            ToolCount: len(conn.Tools),
            Error:     conn.LastError,
        })
        conn.mu.RUnlock()
    }
    return status
}

type MCPServerStatus struct {
    Name      string
    State     MCPServerState
    ToolCount int
    Error     error
}
```

### 3.8 MCP 工具调用增强

```go
// InvokeMCPTool 调用 MCP 工具（带重连和认证）
func (m *MCPManager) InvokeMCPTool(ctx context.Context, serverName, toolName string, args json.RawMessage) (string, error) {
    conn, ok := m.connections[serverName]
    if !ok {
        return "", fmt.Errorf("MCP server %s not found", serverName)
    }

    // 检查连接状态
    conn.mu.RLock()
    state := conn.State
    conn.mu.RUnlock()

    switch state {
    case MCPConnected:
        // 正常调用
    case MCPDisconnected, MCPFailed:
        // 尝试重连
        if err := m.Connect(ctx, serverName, conn.Config); err != nil {
            return "", fmt.Errorf("MCP server %s unavailable: %w", serverName, err)
        }
    case MCPReconnecting:
        // 等待重连完成（with timeout）
        if err := m.waitForConnect(ctx, conn, 10*time.Second); err != nil {
            return "", err
        }
    case MCPAuthPending:
        return "", fmt.Errorf("MCP server %s requires authentication. Run 'coding mcp auth %s'", serverName, serverName)
    default:
        return "", fmt.Errorf("MCP server %s in state: %s", serverName, state)
    }

    // 调用工具
    result, err := conn.Client.CallTool(ctx, toolName, args)
    if err != nil {
        // 检查是否为连接错误（触发重连）
        if isConnectionError(err) {
            go m.reconnect(ctx, conn)
            return "", fmt.Errorf("MCP server %s connection lost, reconnecting...", serverName)
        }
        return "", err
    }

    return result, nil
}
```

### 3.9 能力检测

```go
// ensureCapability 检查服务器是否支持特定能力
func (conn *MCPConnection) ensureCapability(cap string) error {
    if conn.Capabilities == nil {
        return fmt.Errorf("server capabilities not available")
    }
    switch cap {
    case "tools":
        if !conn.Capabilities.Tools { return fmt.Errorf("server does not support tools") }
    case "resources":
        if !conn.Capabilities.Resources { return fmt.Errorf("server does not support resources") }
    case "prompts":
        if !conn.Capabilities.Prompts { return fmt.Errorf("server does not support prompts") }
    }
    return nil
}
```

---

## 4. 配置扩展

```go
// MCPServerConfig V2 配置（扩展现有 MCPServer）
type MCPServerConfig struct {
    // V1 字段（保留）
    Type    string            `json:"type"`
    Command string            `json:"command,omitempty"`
    Args    []string          `json:"args,omitempty"`
    Env     []string          `json:"env,omitempty"`
    URL     string            `json:"url,omitempty"`
    Headers map[string]string `json:"headers,omitempty"`

    // V2 新增
    Auth         *MCPAuthConfig `json:"auth,omitempty"`          // 认证配置
    Reconnect    bool           `json:"reconnect,omitempty"`     // 启用自动重连（默认 true）
    MaxRetries   int            `json:"max_retries,omitempty"`   // 最大重试次数
    Capabilities []string       `json:"capabilities,omitempty"`  // 期望的能力
}

type MCPAuthConfig struct {
    Type     string `json:"type"`      // "oauth2", "bearer", "apikey"
    TokenURL string `json:"token_url,omitempty"` // OAuth token endpoint
    ClientID string `json:"client_id,omitempty"`
}
```

---

## 5. 对比矩阵

| 维度 | Claude Code | jcode V1 | jcode V2 (设计) |
|------|-------------|----------|-----------------|
| **传输方式** | 7 种 | 3 种 | 3 种 (stdio/SSE/HTTP) |
| **配置层级** | 7 层 | 1 层 | 1 层 (保留) |
| **OAuth 认证** | RFC 标准 PKCE | 无 | OAuth2 PKCE |
| **令牌存储** | Keychain + 文件 | 无 | 文件 (0o600) |
| **令牌刷新** | 自动 | 无 | 自动 |
| **资源发现** | ListResources + ReadResource | 无 | ListResources + ReadResource |
| **重连机制** | 指数退避 (1s→30s) | 无 | 指数退避 (1s→30s) + jitter |
| **能力检测** | 完整 capabilities | 无 | tools/resources/prompts |
| **动态连接** | 运行时增删 | 启动时固定 | 运行时增删 |
| **连接状态** | 实时状态同步 | 无 | 状态机 + 回调 |
| **权限体系** | Channel + 用户交互 | 无 | 无 (scope out) |
| **MCP 技能** | 自动转换 | 无 | 无 (scope out) |
| **错误类型** | 5+ 专门类型 | 通用 | 连接/认证/能力分类 |

---

## 6. CLI 子命令扩展

```go
// 现有: coding mcp add/list
// V2 新增:
// coding mcp auth <server>   — 手动触发 OAuth 认证
// coding mcp status          — 查看所有服务器连接状态
// coding mcp remove <server> — 移除服务器
// coding mcp reconnect <server> — 手动重连

func mcpStatusCmd() {
    mgr := getMCPManager()
    statuses := mgr.GetServerStatus()
    for _, s := range statuses {
        icon := "●"
        color := "green"
        switch s.State {
        case MCPConnected: icon = "●"; color = "green"
        case MCPConnecting, MCPReconnecting: icon = "◐"; color = "yellow"
        case MCPFailed: icon = "●"; color = "red"
        case MCPAuthPending: icon = "🔒"; color = "yellow"
        default: icon = "○"; color = "gray"
        }
        fmt.Printf("%s %s [%s] (%d tools)\n", icon, s.Name, s.State, s.ToolCount)
        if s.Error != nil {
            fmt.Printf("  Error: %s\n", s.Error)
        }
    }
}
```
