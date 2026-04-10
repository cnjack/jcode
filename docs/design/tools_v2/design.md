# 工具系统 V2 技术设计文档

> 基于 PRD (`docs/prd/tools.md`) 的技术实现方案。

---

## 1. 架构概述

### 1.1 当前架构

```
┌─────────────────────────────────────────┐
│              Agent (Eino)               │
│         ChatModelAgent + 中间件          │
└─────────────┬───────────────────────────┘
              │ tool.InvokableTool
┌─────────────▼───────────────────────────┐
│            工具注册层                     │
│  edit │ read │ write │ grep │ execute   │
│  ask_user │ todo │ mcp │ subagent      │
└─────────────┬───────────────────────────┘
              │ Executor 接口
┌─────────────▼───────────────────────────┐
│     Env (执行环境抽象)                    │
│  LocalExecutor │ SSHExecutor            │
│  TodoStore │ PlanStore │ BackgroundMgr  │
└─────────────────────────────────────────┘
```

### 1.2 V2 架构演进

```
┌─────────────────────────────────────────┐
│              Agent (Eino)               │
│         ChatModelAgent + 中间件          │
└─────────────┬───────────────────────────┘
              │ tool.InvokableTool
┌─────────────▼───────────────────────────┐
│            工具注册层 (V2)                │
│  edit_v2 │ read_v2 │ write_v2 │ glob   │
│  grep_v2 │ execute_v2 │ ask_user_v2    │
│  todo_v2 │ mcp_v2 │ subagent           │
│  mcp_connect │ mcp_list_resources      │
└─────────────┬───────────────────────────┘
              │
┌─────────────▼───────────────────────────┐
│     Env V2 (增强执行环境)                 │
│  ┌──────────────────────────────┐       │
│  │  FileTracker (冲突检测)       │       │
│  │  mtime + hash 管理           │       │
│  └──────────────────────────────┘       │
│  ┌──────────────────────────────┐       │
│  │  MCPManager (连接管理)        │       │
│  │  OAuth + 重连 + 资源发现      │       │
│  └──────────────────────────────┘       │
│  ┌──────────────────────────────┐       │
│  │  TodoStore V2 (持久化)        │       │
│  │  磁盘存储 + 增量API + 依赖    │       │
│  └──────────────────────────────┘       │
│  ┌──────────────────────────────┐       │
│  │  BackgroundManager V2         │       │
│  │  自适应 + 流式 + Stall检测    │       │
│  └──────────────────────────────┘       │
│  LocalExecutor │ SSHExecutor            │
└─────────────────────────────────────────┘
```

### 1.3 核心设计原则

1. **接口兼容**：V2 工具在同一 `tool.InvokableTool` 接口上扩展，新增参数有默认值
2. **Executor 透明**：所有增强同时支持 Local 和 SSH，通过 `Executor` 接口统一
3. **组合优于继承**：新增能力以独立组件（FileTracker、MCPManager 等）注入 Env
4. **故障降级**：外部依赖失败（磁盘写入、MCP 连接）时降级而非崩溃

---

## 2. Edit/Read/Write 工具组件设计

### 2.1 FileTracker — 冲突检测组件

```go
// internal/tools/file_tracker.go

// FileSnapshot 记录文件快照用于冲突检测
type FileSnapshot struct {
    Path       string
    ModTime    time.Time
    ContentMD5 [16]byte
    Size       int64
}

// FileTracker 管理文件快照，支持冲突检测
type FileTracker struct {
    mu        sync.RWMutex
    snapshots map[string]*FileSnapshot // path → snapshot
}

// Track 记录文件当前状态（读取或编辑后调用）
func (ft *FileTracker) Track(ctx context.Context, exec Executor, path string) error

// Check 检测文件是否被外部修改
// 返回 nil 表示未冲突，返回 ConflictError 表示冲突
func (ft *FileTracker) Check(ctx context.Context, exec Executor, path string) error

// ConflictError 冲突详情
type ConflictError struct {
    Path        string
    TrackedTime time.Time
    CurrentTime time.Time
    TrackedMD5  [16]byte
    CurrentMD5  [16]byte
}

func (e *ConflictError) Error() string
```

### 2.2 MultiEdit — 多编辑支持

```go
// internal/tools/edit.go （扩展 EditInput）

// EditOperation 单次编辑操作
type EditOperation struct {
    OldString string `json:"old_string"`
    NewString string `json:"new_string"`
    StartLine int    `json:"start_line,omitempty"`
    EndLine   int    `json:"end_line,omitempty"`
}

// EditInputV2 扩展编辑输入，支持单编辑和批量编辑
type EditInputV2 struct {
    FilePath   string          `json:"file_path"`
    OldString  string          `json:"old_string,omitempty"`   // 兼容模式
    NewString  string          `json:"new_string,omitempty"`   // 兼容模式
    ReplaceAll bool            `json:"replace_all,omitempty"`
    StartLine  int             `json:"start_line,omitempty"`
    EndLine    int             `json:"end_line,omitempty"`
    Edits      []EditOperation `json:"edits,omitempty"`        // 多编辑模式
}

// EditResult 编辑结果，包含 diff 摘要
type EditResult struct {
    Path         string `json:"path"`
    LinesChanged int    `json:"lines_changed"`
    Diff         string `json:"diff"`          // unified diff 格式
    Created      bool   `json:"created"`
}

// applyMultiEdits 原子应用多个编辑操作
// 从上到下排序后逐个应用，自动调整行偏移
// 任一失败则全部回滚
func applyMultiEdits(content string, edits []EditOperation) (string, string, error)

// generateUnifiedDiff 生成 unified diff 输出
func generateUnifiedDiff(path, before, after string) string
```

### 2.3 BinaryDetector — 二进制/编码检测

```go
// internal/tools/binary_detect.go

// FileEncoding 文件编码类型
type FileEncoding string

const (
    EncodingUTF8    FileEncoding = "utf-8"
    EncodingUTF16LE FileEncoding = "utf-16le"
    EncodingUTF16BE FileEncoding = "utf-16be"
    EncodingBinary  FileEncoding = "binary"
    EncodingUnknown FileEncoding = "unknown"
)

// DetectEncoding 检测文件编码
// 通过 BOM + 内容启发式判断
func DetectEncoding(header []byte) FileEncoding

// IsBinaryFile 判断文件是否为二进制
// 使用 magic bytes（前 512 字节）+ 扩展名联合判断
func IsBinaryFile(path string, header []byte) bool

// MaxFileSize 文件大小上限（默认 10MB）
const MaxFileSize = 10 * 1024 * 1024

// CheckFileSize 检查文件大小是否超限
func CheckFileSize(size int64) error
```

### 2.4 数据流：Edit V2

```
Agent 调用 edit_v2(file_path, edits=[...])
    │
    ▼
解析输入 → 兼容路由（单编辑 or 多编辑）
    │
    ▼
FileTracker.Check(path) ── 冲突? ──→ 返回 ConflictError
    │ 无冲突
    ▼
ReadFile(path) → DetectEncoding → IsBinaryFile?
    │                                   │ 是
    │ 否                                ▼
    ▼                            返回 "binary file, cannot edit"
按顺序应用 edits（applyMultiEdits）
    │ 任一失败
    │──────────→ 全部回滚，返回错误
    │ 全部成功
    ▼
WriteFile(path, result) → FileTracker.Track(path)
    │
    ▼
generateUnifiedDiff → 返回 EditResult{diff, lines_changed}
```

---

## 3. Ask User 工具组件设计

### 3.1 扩展问题模型

```go
// internal/tools/ask_user.go （扩展）

// AskUserOptionV2 带描述的选项
type AskUserOptionV2 struct {
    Label       string `json:"label"`
    Description string `json:"description,omitempty"` // 选项说明
}

// AskUserQuestion 单个问题
type AskUserQuestion struct {
    Question    string             `json:"question"`
    Options     []AskUserOptionV2  `json:"options,omitempty"`
    MultiSelect bool               `json:"multi_select,omitempty"` // 多选模式
}

// AskUserInputV2 支持批量提问
type AskUserInputV2 struct {
    Question    string             `json:"question,omitempty"`     // 兼容单问题
    Options     []AskUserOptionV2  `json:"options,omitempty"`      // 兼容单问题
    Questions   []AskUserQuestion  `json:"questions,omitempty"`    // 批量模式（1-4 个）
    MultiSelect bool               `json:"multi_select,omitempty"` // 兼容单问题多选
}

// AskUserResponseV2 结构化多答案
type AskUserResponseV2 struct {
    Answer  string            `json:"answer,omitempty"`   // 兼容单答案
    Answers []AskUserAnswer   `json:"answers,omitempty"`  // 批量答案
}

// AskUserAnswer 单个问题的答案
type AskUserAnswer struct {
    Question string   `json:"question"`
    Selected []string `json:"selected"` // 选中项（多选时多个）
    FreeText string   `json:"free_text,omitempty"` // 自由输入
}
```

### 3.2 TUI 交互流程

```
批量提问模式:
┌─────────────────────────────────────────┐
│ 问题 1/3: 选择目标框架                    │
│                                         │
│  ● Gin (推荐)                            │
│    高性能 HTTP 框架，社区活跃              │
│  ○ Echo                                  │
│    轻量级，中间件灵活                      │
│  ○ Fiber                                 │
│    Express 风格，适合 Node 开发者          │
│                                         │
│ [Enter] 确认  [Tab] 下一题  [n] 跳过     │
└─────────────────────────────────────────┘

多选模式:
┌─────────────────────────────────────────┐
│ 启用哪些中间件？（多选）                   │
│                                         │
│  ☑ Logger — 请求日志记录                  │
│  ☑ CORS — 跨域资源共享                   │
│  ☐ RateLimit — 速率限制                  │
│  ☑ Recovery — panic 恢复                 │
│                                         │
│ [Space] 切换  [Enter] 确认               │
└─────────────────────────────────────────┘
```

---

## 4. MCP 工具组件设计

### 4.1 MCPManager — 连接管理核心

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

### 4.2 OAuth 认证组件

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

### 4.3 重连策略

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

### 4.4 数据流：MCP Connect + OAuth

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

---

## 5. Grep 工具组件设计

### 5.1 扩展参数

```go
// internal/tools/grep.go （扩展）

// GrepOutputMode 输出模式
type GrepOutputMode string

const (
    GrepModeContent GrepOutputMode = "content" // 默认：行内容
    GrepModeFiles   GrepOutputMode = "files"   // 仅文件列表
    GrepModeCount   GrepOutputMode = "count"   // 计数统计
)

// GrepInputV2 扩展搜索输入
type GrepInputV2 struct {
    Pattern         string         `json:"pattern"`
    Path            string         `json:"path"`
    Include         string         `json:"include,omitempty"`
    CaseInsensitive bool           `json:"case_insensitive,omitempty"`
    MaxResults      int            `json:"max_results,omitempty"`      // 默认 50
    Offset          int            `json:"offset,omitempty"`           // 分页偏移
    BeforeContext   int            `json:"before_context,omitempty"`   // -B
    AfterContext    int            `json:"after_context,omitempty"`    // -A
    Context         int            `json:"context,omitempty"`          // -C (优先于 B/A)
    OutputMode      GrepOutputMode `json:"output_mode,omitempty"`     // 默认 content
    Multiline       bool           `json:"multiline,omitempty"`       // 多行匹配
}

// GrepResult 搜索结果
type GrepResult struct {
    Matches      []GrepMatch `json:"matches,omitempty"`       // content 模式
    Files        []string    `json:"files,omitempty"`         // files 模式
    Counts       map[string]int `json:"counts,omitempty"`     // count 模式
    TotalMatches int         `json:"total_matches"`
    Truncated    bool        `json:"truncated"`
}

type GrepMatch struct {
    File    string `json:"file"`
    Line    int    `json:"line"`
    Content string `json:"content"`
    Before  string `json:"before,omitempty"` // 上文
    After   string `json:"after,omitempty"`  // 下文
}
```

### 5.2 Glob 工具

```go
// internal/tools/glob.go

// GlobInput 文件查找参数
type GlobInput struct {
    Pattern  string `json:"pattern"`           // glob 模式
    Path     string `json:"path"`              // 搜索根路径
    MaxDepth int    `json:"max_depth,omitempty"` // 递归深度限制
    Limit    int    `json:"limit,omitempty"`     // 返回上限，默认 100
}

// GlobResult 文件查找结果
type GlobResult struct {
    Files     []string `json:"files"`
    Total     int      `json:"total"`
    Truncated bool     `json:"truncated"`
}

func (e *Env) NewGlobTool() tool.InvokableTool
```

### 5.3 Ripgrep 参数映射

```
GrepInputV2 → ripgrep 命令行参数:

output_mode=content  → rg --json (默认行为)
output_mode=files    → rg --files-with-matches
output_mode=count    → rg --count

before_context=N     → rg -B N
after_context=N      → rg -A N
context=N            → rg -C N

offset=N             → 结果切片 [N:]
multiline=true       → rg --multiline

max_results=N        → rg --max-count (近似) + 结果截断
```

---

## 6. Todo 工具组件设计

### 6.1 TodoStore V2 — 持久化 + 依赖

```go
// internal/tools/todo_store.go （重构）

// TodoItemV2 扩展任务项
type TodoItemV2 struct {
    ID        int        `json:"id"`
    Title     string     `json:"title"`
    Status    TodoStatus `json:"status"`
    BlockedBy []int      `json:"blocked_by,omitempty"` // 依赖的任务 ID 列表
}

// TodoStoreV2 持久化任务存储
type TodoStoreV2 struct {
    mu        sync.RWMutex
    items     []TodoItemV2
    filePath  string           // ~/.jcoding/todos/<session_id>.json
    onChange  func([]TodoItemV2)
}

// NewTodoStoreV2 创建持久化 TodoStore
func NewTodoStoreV2(sessionID string) *TodoStoreV2

// Load 从磁盘加载任务列表（启动时/resume 时调用）
func (s *TodoStoreV2) Load() error

// save 写入磁盘（每次变更后自动调用）
func (s *TodoStoreV2) save() error

// --- 增量 API ---

// AddItem 添加单个任务
func (s *TodoStoreV2) AddItem(title string, blockedBy []int) (TodoItemV2, error)

// UpdateItem 更新单个任务状态
// 验证: blocked_by 中的任务必须已完成才能设为 in_progress
func (s *TodoStoreV2) UpdateItem(id int, status TodoStatus) error

// RemoveItem 移除单个任务
func (s *TodoStoreV2) RemoveItem(id int) error

// --- 兼容 API ---

// Update 全量替换（向后兼容）
func (s *TodoStoreV2) Update(items []TodoItemV2)

// --- 查询 API ---

// Items / HasItems / HasIncomplete / Summary 同 V1

// BlockedItems 返回被阻塞的任务
func (s *TodoStoreV2) BlockedItems() []TodoItemV2

// ReadyItems 返回可执行的任务（无阻塞且为 pending）
func (s *TodoStoreV2) ReadyItems() []TodoItemV2
```

### 6.2 Todo 工具扩展

```go
// internal/tools/todo.go （扩展工具接口）

// TodoAction 操作类型
type TodoAction string

const (
    TodoActionUpdate TodoAction = "update"  // 兼容：全量替换
    TodoActionAdd    TodoAction = "add"     // 新增
    TodoActionModify TodoAction = "modify"  // 修改状态
    TodoActionRemove TodoAction = "remove"  // 删除
    TodoActionRead   TodoAction = "read"    // 查询
)

// TodoInputV2 统一输入
type TodoInputV2 struct {
    Action    TodoAction   `json:"action"`              // 操作类型
    Items     []TodoItemV2 `json:"items,omitempty"`     // update 兼容
    Title     string       `json:"title,omitempty"`     // add
    BlockedBy []int        `json:"blocked_by,omitempty"` // add
    ID        int          `json:"id,omitempty"`        // modify/remove
    Status    TodoStatus   `json:"status,omitempty"`    // modify
}
```

### 6.3 持久化文件格式

```json
// ~/.jcoding/todos/550e8400-e29b.json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "updated_at": "2026-04-10T10:30:00Z",
  "items": [
    {
      "id": 1,
      "title": "重构 Edit 工具添加冲突检测",
      "status": "completed",
      "blocked_by": []
    },
    {
      "id": 2,
      "title": "实现多编辑支持",
      "status": "in_progress",
      "blocked_by": [1]
    },
    {
      "id": 3,
      "title": "添加编码检测",
      "status": "pending",
      "blocked_by": [1]
    }
  ]
}
```

---

## 7. Execute 工具组件设计

### 7.1 BackgroundManager V2

```go
// internal/tools/background.go （扩展）

// BgTaskV2 增强后台任务
type BgTaskV2 struct {
    ID        string
    Command   string
    Status    BgTaskStatus
    Output    strings.Builder  // 内存缓冲
    LogFile   *os.File         // 磁盘持久化
    Started   time.Time
    Ended     time.Time
    LastWrite time.Time        // 最后输出时间（Stall 检测用）
}

// BackgroundManagerV2 增强后台管理器
type BackgroundManagerV2 struct {
    mu            sync.Mutex
    tasks         map[string]*BgTaskV2
    notifications []BgNotification
    nextID        int
    env           *Env
    notifier      BgNotifier
    stallTimeout  time.Duration   // 默认 45s
    logDir        string          // ~/.jcoding/tasks/
}

func NewBackgroundManagerV2(env *Env, logDir string) *BackgroundManagerV2

// Run 启动后台任务，输出写入磁盘
func (bm *BackgroundManagerV2) Run(ctx context.Context, command string) string

// PromoteToBackground 前台任务超时后升级为后台
func (bm *BackgroundManagerV2) PromoteToBackground(taskID string, partialOutput string) string

// startStallWatcher 启动 Stall 检测协程
func (bm *BackgroundManagerV2) startStallWatcher(task *BgTaskV2)
```

### 7.2 自适应后台化

```go
// internal/tools/execute.go （扩展）

const (
    // BlockingBudgetMS 前台命令最大阻塞时间
    BlockingBudgetMS = 15_000 // 15秒

    // StallTimeoutMS 后台任务无输出超时
    StallTimeoutMS = 45_000 // 45秒
)

// ExecuteInputV2 扩展执行参数
type ExecuteInputV2 struct {
    Command    string `json:"command"`
    Timeout    int    `json:"timeout,omitempty"`
    Background bool   `json:"background,omitempty"`
    // Description 命令描述（用于 TUI 显示）
    Description string `json:"description,omitempty"`
}

// StreamingOutput 流式输出通道
type StreamingOutput struct {
    Chunk     string
    Timestamp time.Time
    Final     bool // 命令完成标记
}
```

### 7.3 Sleep 检测

```go
// internal/tools/sleep_detect.go

import "regexp"

var sleepPattern = regexp.MustCompile(`(?i)\bsleep\s+(\d+)`)

const DefaultSleepThreshold = 30 // 秒

// DetectSleep 检测命令中的 sleep 模式
// 返回 sleep 秒数，0 表示无 sleep
func DetectSleep(command string) int

// SleepWarning 生成 sleep 告警消息
func SleepWarning(seconds int) string
```

### 7.4 数据流：Execute V2

```
Agent 调用 execute_v2(command, timeout=120s)
    │
    ▼
DetectSleep(command) → 超阈值? → 返回告警，建议改用 background
    │ 正常
    ▼
background=true? ─── 是 ──→ BgManagerV2.Run()
    │ 否                       │
    ▼                          ▼
前台执行，启动 StreamingOutput  创建 BgTaskV2 + LogFile
    │                          回写 task_id
    ├── 每 100ms → TUI 输出 chunk
    │
    ├── BlockingBudget 超时 (15s)?
    │   │ 是
    │   ▼
    │   PromoteToBackground() → 返回 task_id + partial_output
    │
    └── 命令完成？
        │ 是
        ▼
    返回完整 output

后台 Stall 检测:
    │
    ▼
startStallWatcher(task)
    │
    每 5s 检测 task.LastWrite
    │
    └── now - LastWrite > 45s?
        │ 是
        ▼
    注入 Stall 通知: "命令 bg_N 可能卡住（45s 无输出）"
    Agent 可选: 终止 / 发送输入 / 继续等待
```

---

## 8. 公共组件设计

### 8.1 Env V2 扩展

```go
// internal/tools/env.go （扩展）

type EnvV2 struct {
    Exec          Executor
    pwd           string
    platform      string
    TodoStore     *TodoStoreV2
    PlanStore     *PlanStore
    FileTracker   *FileTracker
    MCPManager    *MCPManager
    BgManager     *BackgroundManagerV2
    OnEnvChange   func(envLabel string, isLocal bool, err error)
}

func NewEnvV2(pwd, platform, sessionID string) *EnvV2 {
    logDir := filepath.Join(os.UserHomeDir(), ".jcoding", "tasks")
    todoDir := filepath.Join(os.UserHomeDir(), ".jcoding", "todos")
    return &EnvV2{
        Exec:        NewLocalExecutor(platform),
        pwd:         pwd,
        platform:    platform,
        TodoStore:   NewTodoStoreV2(sessionID),
        PlanStore:   NewPlanStore(),
        FileTracker: NewFileTracker(),
        MCPManager:  NewMCPManager(NewFileTokenStore()),
        BgManager:   NewBackgroundManagerV2(nil, logDir), // env 后补
    }
}
```

### 8.2 目录结构

```
~/.jcoding/
├── config.json          # 配置文件
├── debug.log            # 调试日志
├── oauth/               # OAuth Token 存储（加密）
│   ├── server1.json
│   └── server2.json
├── todos/               # 任务持久化
│   ├── <session_id>.json
│   └── ...
├── tasks/               # 后台任务输出日志
│   ├── bg_1.log
│   ├── bg_2.log
│   └── ...
└── sessions/            # 会话记录
    └── ...
```

---

## 9. 实现计划

### Phase 1: 安全基础（Edit 核心）

| 任务 | 文件 | 描述 | 依赖 |
|------|------|------|------|
| 1.1 FileTracker 组件 | `internal/tools/file_tracker.go` | mtime + MD5 冲突检测 | 无 |
| 1.2 BinaryDetector 组件 | `internal/tools/binary_detect.go` | 编码检测 + 二进制判断 | 无 |
| 1.3 Read 文件大小限制 | `internal/tools/read.go` | MaxFileSize 检查 | 1.2 |
| 1.4 Edit 集成冲突检测 | `internal/tools/edit.go` | 接入 FileTracker | 1.1 |
| 1.5 Write 集成冲突检测 | `internal/tools/write.go` | 接入 FileTracker | 1.1 |
| 1.6 单元测试 | `internal/tools/*_test.go` | 冲突/编码/二进制测试 | 1.1-1.5 |

### Phase 2: 效率提升

| 任务 | 文件 | 描述 | 依赖 |
|------|------|------|------|
| 2.1 多编辑支持 | `internal/tools/edit.go` | EditInputV2 + applyMultiEdits | Phase 1 |
| 2.2 Unified Diff 输出 | `internal/tools/edit.go` | generateUnifiedDiff | 2.1 |
| 2.3 上下文行参数 | `internal/tools/grep.go` | -B/-A/-C 映射 | 无 |
| 2.4 自适应后台化 | `internal/tools/execute.go` | BlockingBudget + Promote | 无 |
| 2.5 流式进度通道 | `internal/tools/execute.go` | StreamingOutput channel | 2.4 |
| 2.6 TUI 流式渲染 | `internal/tui/tui.go` | 接收 StreamingOutput | 2.5 |

### Phase 3: 持久化

| 任务 | 文件 | 描述 | 依赖 |
|------|------|------|------|
| 3.1 TodoStoreV2 | `internal/tools/todo_store.go` | 磁盘持久化 + 增量 API | 无 |
| 3.2 Todo 依赖关系 | `internal/tools/todo_store.go` | blocked_by 验证逻辑 | 3.1 |
| 3.3 Todo 工具扩展 | `internal/tools/todo.go` | TodoInputV2 多操作路由 | 3.1 |
| 3.4 BgManager 输出持久化 | `internal/tools/background.go` | LogFile 磁盘写入 | 无 |
| 3.5 Stall 检测 | `internal/tools/background.go` | startStallWatcher | 3.4 |
| 3.6 Sleep 检测 | `internal/tools/sleep_detect.go` | 正则匹配 + 告警 | 无 |

### Phase 4: MCP 增强

| 任务 | 文件 | 描述 | 依赖 |
|------|------|------|------|
| 4.1 MCPManager 核心 | `internal/tools/mcp_manager.go` | 连接池 + 状态管理 | 无 |
| 4.2 重连机制 | `internal/tools/mcp_reconnect.go` | 指数退避 | 4.1 |
| 4.3 OAuth 认证 | `internal/tools/mcp_oauth.go` | PKCE + Token 存储 | 4.1 |
| 4.4 资源发现工具 | `internal/tools/mcp_resources.go` | list + read 工具 | 4.1 |
| 4.5 动态连接工具 | `internal/tools/mcp_connect.go` | mcp_connect / mcp_disconnect | 4.1 |
| 4.6 能力检测 | `internal/tools/mcp_manager.go` | Capabilities 解析 | 4.1 |

### Phase 5: 交互增强

| 任务 | 文件 | 描述 | 依赖 |
|------|------|------|------|
| 5.1 选项描述支持 | `internal/tools/ask_user.go` | AskUserOptionV2 | 无 |
| 5.2 批量提问 | `internal/tools/ask_user.go` | AskUserInputV2 多问题 | 5.1 |
| 5.3 多选模式 | `internal/tools/ask_user.go` | multi_select 参数 | 5.1 |
| 5.4 TUI 多选渲染 | `internal/tui/input_views.go` | 复选框组件 | 5.3 |
| 5.5 TUI 批量问题视图 | `internal/tui/input_views.go` | 多问题分步展示 | 5.2 |

### Phase 6: 搜索增强

| 任务 | 文件 | 描述 | 依赖 |
|------|------|------|------|
| 6.1 多输出模式 | `internal/tools/grep.go` | files / count 模式 | 无 |
| 6.2 分页支持 | `internal/tools/grep.go` | offset 参数 | 6.1 |
| 6.3 Glob 工具 | `internal/tools/glob.go` | 独立文件查找 | 无 |
| 6.4 多行匹配 | `internal/tools/grep.go` | --multiline 映射 | 无 |

---

## 10. 风险评估

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| 冲突检测误报（mtime 精度不足） | 中 | 中 | 结合 content hash 双重校验 |
| MCP OAuth 流程兼容性 | 高 | 高 | 分阶段对接，优先支持主流提供商 |
| 多编辑原子性在 SSH 远程模式失败 | 中 | 高 | 先读后写，失败时用原始内容回滚 |
| Stall 检测误判（命令确实需要长时间） | 中 | 低 | 可配置超时阈值 + Agent 可选继续等待 |
| 磁盘持久化并发写冲突 | 低 | 中 | 文件锁 + 写入临时文件后 rename |
| 输出日志磁盘空间满 | 低 | 中 | 定期清理 + 配置上限 |

---

## 附录 A: 接口兼容矩阵

| 工具 | V1 参数 | V2 新增参数 | 兼容策略 |
|------|--------|-----------|---------|
| edit | file_path, old_string, new_string, replace_all, start_line, end_line | edits[] | 无 edits 时走 V1 路径 |
| read | file_path, offset, limit | （无新增） | 增加内部检测逻辑 |
| write | file_path, content | （无新增） | 增加冲突检测 |
| grep | pattern, path, include, case_insensitive, max_results | before_context, after_context, context, output_mode, offset, multiline | 新参数全有默认值 |
| ask_user | question, options | questions[], multi_select, description | 无 questions 时走单问题路径 |
| execute | command, timeout, background | description | 内部增强，参数无变化 |
| todowrite | items[] | action, title, blocked_by, id, status | 无 action 时走全量替换 |

## 附录 B: 依赖库评估

| 功能 | 候选库 | 说明 |
|------|-------|------|
| 文件编码检测 | `golang.org/x/text/encoding` | Go 标准扩展库 |
| MD5 哈希 | `crypto/md5` | 标准库，性能足够 |
| Unified Diff | `github.com/pmezard/go-difflib` | 成熟稳定，Go diff 标准选择 |
| 文件监视 | `github.com/fsnotify/fsnotify` | 跨平台，社区活跃 |
| OAuth2 PKCE | `golang.org/x/oauth2` | Go 标准扩展库 |
| 指数退避 | `github.com/cenkalti/backoff/v4` | 轻量级，广泛使用 |
| 二进制检测 | `net/http.DetectContentType` | 标准库，magic bytes 检测 |
