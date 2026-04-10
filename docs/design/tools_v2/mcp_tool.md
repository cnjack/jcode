# MCP 工具 V2 设计

## 1. MCPManager — 连接管理核心

```go
// internal/tools/mcp_manager.go

// MCPServerState MCP 服务器连接状态
type MCPServerState string

const (
    MCPStateDisconnected MCPServerState = "disconnected"
    MCPStateConnecting   MCPServerState = "connecting"
    MCPStateConnected    MCPServerState = "connected"
    MCPStateReconnecting MCPServerState = "reconnecting"
    MCPStateAuthPending  MCPServerState = "auth_pending"
    MCPStateFailed       MCPServerState = "failed"
)

// MCPConnection 单个 MCP 服务器连接
type MCPConnection struct {
    Name         string
    Config       *config.MCPServer
    Client       *client.Client
    State        MCPServerState
    Tools        []tool.BaseTool
    Capabilities *mcp.ServerCapabilities
    RetryCount   int
    LastError    error
}

// MCPManager 管理所有 MCP 服务器连接
type MCPManager struct {
    mu          sync.RWMutex
    connections map[string]*MCPConnection
    authStore   OAuthTokenStore
    onStateChange func(name string, state MCPServerState)
}

// NewMCPManager 创建管理器
func NewMCPManager(authStore OAuthTokenStore) *MCPManager

// Connect 连接到一个 MCP 服务器（支持 OAuth）
func (m *MCPManager) Connect(ctx context.Context, name string, cfg *config.MCPServer) error

// Disconnect 断开指定服务器
func (m *MCPManager) Disconnect(name string) error

// Reconnect 重连指定服务器（指数退避）
func (m *MCPManager) Reconnect(ctx context.Context, name string) error

// AllTools 返回所有已连接服务器的工具列表
func (m *MCPManager) AllTools() []tool.BaseTool

// ListResources 列出指定服务器的资源
func (m *MCPManager) ListResources(ctx context.Context, server string) ([]mcp.Resource, error)

// ReadResource 读取指定资源
func (m *MCPManager) ReadResource(ctx context.Context, server, uri string) ([]byte, string, error)
```

## 2. OAuth 认证组件

```go
// internal/tools/mcp_oauth.go

// OAuthTokenStore Token 安全存储接口
type OAuthTokenStore interface {
    // GetToken 获取指定服务器的 OAuth Token
    GetToken(serverName string) (*OAuthToken, error)
    // SaveToken 保存 Token
    SaveToken(serverName string, token *OAuthToken) error
    // DeleteToken 删除 Token
    DeleteToken(serverName string) error
}

// OAuthToken OAuth2 令牌
type OAuthToken struct {
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token,omitempty"`
    TokenType    string    `json:"token_type"`
    Expiry       time.Time `json:"expiry"`
}

// FileTokenStore 基于加密文件的 Token 存储
// 路径: ~/.jcoding/oauth/<server_name>.json (加密)
type FileTokenStore struct {
    baseDir string
}

func NewFileTokenStore() *FileTokenStore

// OAuthFlow 执行完整 OAuth2 PKCE 授权流程
// 1. 启动本地回调服务器
// 2. 打开浏览器进行授权
// 3. 接收回调获取 code
// 4. 交换 code 获得 token
func OAuthFlow(ctx context.Context, serverURL string) (*OAuthToken, error)

// RefreshToken 令牌刷新
func RefreshToken(ctx context.Context, serverURL string, token *OAuthToken) (*OAuthToken, error)
```

## 3. 重连策略

```go
// internal/tools/mcp_reconnect.go

// ReconnectPolicy 重连策略配置
type ReconnectPolicy struct {
    InitialDelay time.Duration // 1s
    MaxDelay     time.Duration // 30s
    MaxRetries   int           // 5
    BackoffMult  float64       // 2.0
}

var DefaultReconnectPolicy = ReconnectPolicy{
    InitialDelay: 1 * time.Second,
    MaxDelay:     30 * time.Second,
    MaxRetries:   5,
    BackoffMult:  2.0,
}

// reconnectWithBackoff 指数退避重连
func (m *MCPManager) reconnectWithBackoff(ctx context.Context, conn *MCPConnection) error
```

## 4. 数据流：MCP Connect + OAuth

```
Agent 调用 mcp_connect(server_name, url)
    │
    ▼
MCPManager.Connect(name, config)
    │
    ▼
创建 Client (stdio/SSE/HTTP) → Start()
    │
    ├── 成功 → Initialize() → 获取 Capabilities
    │                │
    │                ▼
    │          tools/list → 注册新工具 → State=Connected
    │
    └── 401 Unauthorized → State=AuthPending
                │
                ▼
          OAuthTokenStore.GetToken() → 有缓存?
                │                        │
                │ 无                      │ 有
                ▼                        ▼
          OAuthFlow() ← TUI 浏览器交互  RefreshToken()
                │                        │ 过期
                ▼                        ▼
          SaveToken() → 重试 Connect    OAuthFlow()
                │
                ▼
          连接成功 → State=Connected

连接断开事件:
    │
    ▼
State=Reconnecting → reconnectWithBackoff()
    │
    ├── 重试 1: 等待 1s → Connect()
    ├── 重试 2: 等待 2s → Connect()
    ├── 重试 3: 等待 4s → Connect()
    ├── 重试 4: 等待 8s → Connect()
    └── 重试 5: 等待 16s → Connect()
         │ 全部失败
         ▼
    State=Failed → 通知 TUI → 日志记录
```
