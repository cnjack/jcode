# Agent Team 设计文档

## 1. 概述

### 1.1 目标

为 jcode 实现多 Agent 协作（Agent Team / Swarm）功能，允许主 Agent（Team Lead）创建和管理多个 Teammate Agent，它们可以并行工作、互相通信、共享或隔离资源，并在 BubbleTea TUI 中提供上下文切换和 Teammate 视图。

### 1.2 参考系统

| 参考 | 关键借鉴 |
|------|---------|
| **Claude Code** (TS/React) | 完整的 Swarm 架构：TeamFile、Mailbox、InProcessTeammate、AsyncLocalStorage 隔离、TUI 视图切换 |
| **Eino multiagent/host** (Go) | Graph-based Host 协调模式、Specialist 路由、Tool 封装、流式支持 |
| **jcode 现有架构** | Subagent 模式、Env 克隆、BubbleTea TUI、Session JSONL |

### 1.3 核心设计原则

1. **渐进式架构** — 在现有 subagent 基础上扩展，不破坏单 agent 模式
2. **进程内并发** — 使用 goroutine + context 隔离（对标 Claude Code 的 AsyncLocalStorage）
3. **文件 Mailbox** — 基于 JSON 文件的消息队列，支持锁竞争
4. **TUI 原生** — BubbleTea 组件化视图切换，Shift+Up/Down 导航

---

## 2. 架构总览

```
┌─────────────────────────────────────────────────────────┐
│                    User (BubbleTea TUI)                  │
│  ┌─────────┐  ┌──────────────┐  ┌────────────────────┐  │
│  │ InputBox │  │ ViewportArea │  │ CoordinatorPanel   │  │
│  │(textarea)│  │(conversation)│  │(teammate status)   │  │
│  └────┬─────┘  └──────┬───────┘  └─────────┬──────────┘  │
│       │               │                     │             │
│       └───────┬───────┘─────────────────────┘             │
│               │ (Shift+Up/Down: switch agent view)        │
└───────────────┼───────────────────────────────────────────┘
                │
    ┌───────────▼───────────────┐
    │     TeamManager           │
    │  ┌─────────────────────┐  │
    │  │ TeamFile (JSON)     │  │
    │  │ - leadAgentId       │  │
    │  │ - members[]         │  │
    │  │ - teamAllowedPaths  │  │
    │  └─────────────────────┘  │
    │                           │
    │  agents: map[id]*Agent    │
    │  mailboxes: per-agent     │
    └───┬───────┬───────┬───────┘
        │       │       │
   ┌────▼──┐ ┌─▼────┐ ┌▼─────┐
   │Leader │ │Agent1│ │Agent2│   (goroutines)
   │(main) │ │      │ │      │
   │       │ │ Env  │ │ Env  │   (isolated)
   │       │ │ Todo │ │ Todo │
   │       │ │ Sess │ │ Sess │
   └───┬───┘ └──┬───┘ └──┬───┘
       │        │         │
       └───Mailbox────────┘     (~/.jcode/teams/{name}/inboxes/)
```

---

## 3. 核心数据模型

### 3.1 TeamFile — 团队持久化状态

```go
// internal/team/types.go

// TeamFile 是团队的持久化状态，存储在 ~/.jcode/teams/{name}/team.json
type TeamFile struct {
    Name            string          `json:"name"`
    Description     string          `json:"description,omitempty"`
    CreatedAt       time.Time       `json:"created_at"`
    LeadAgentID     string          `json:"lead_agent_id"`     // "team-lead@{teamName}"
    LeadSessionID   string          `json:"lead_session_id"`   // 主 session UUID
    Members         []TeamMember    `json:"members"`
    AllowedPaths    []AllowedPath   `json:"allowed_paths,omitempty"`
}

type TeamMember struct {
    AgentID         string          `json:"agent_id"`          // "{name}@{team}"
    Name            string          `json:"name"`
    AgentType       string          `json:"agent_type,omitempty"` // "researcher", "coder", etc.
    Model           string          `json:"model,omitempty"`   // 可选模型覆盖
    Prompt          string          `json:"prompt,omitempty"`  // 初始任务
    Color           string          `json:"color,omitempty"`   // TUI 颜色
    Cwd             string          `json:"cwd"`
    JoinedAt        time.Time       `json:"joined_at"`
    IsActive        bool            `json:"is_active"`
    Subscriptions   []string        `json:"subscriptions,omitempty"`
    PermissionMode  string          `json:"permission_mode,omitempty"` // "normal", "plan", "auto"
}

type AllowedPath struct {
    Path    string `json:"path"`
    Mode    string `json:"mode"` // "read", "write", "execute"
}
```

### 3.2 TeammateState — 运行时状态（内存）

```go
// internal/team/state.go

type TeammateStatus string

const (
    StatusPending   TeammateStatus = "pending"
    StatusRunning   TeammateStatus = "running"
    StatusIdle      TeammateStatus = "idle"
    StatusCompleted TeammateStatus = "completed"
    StatusFailed    TeammateStatus = "failed"
    StatusKilled    TeammateStatus = "killed"
)

// TeammateState 是 teammate 的运行时状态（存在于 TeamManager 内存中）
type TeammateState struct {
    // 身份
    Identity        TeammateIdentity
    TaskID          string

    // 执行
    Status          TeammateStatus
    Agent           *adk.ChatModelAgent
    Env             *tools.Env          // 隔离的环境
    Cancel          context.CancelFunc  // 终止整个 teammate
    WorkCancel      context.CancelFunc  // 终止当前 turn

    // 会话
    Messages        []*schema.Message   // 最近 N 条消息（UI 展示用，capped）
    PendingMessages []string            // 等待送达的用户消息

    // Plan mode
    AwaitingApproval bool
    PermissionMode   string

    // 进度
    Progress        *AgentProgress
    IsIdle          bool
    ShutdownReq     bool
    OnIdleCallbacks []func()

    // UI
    SpinnerVerb     string
    Color           string
    LastToolCount   int
    LastTokenCount  int
}

type TeammateIdentity struct {
    AgentID         string  // "researcher@my-team"
    AgentName       string  // "researcher"
    TeamName        string  // "my-team"
    Color           string
    ParentSessionID string
}

type AgentProgress struct {
    ToolCallCount   int
    TokensUsed      int64
    LastToolName    string
    LastToolTime    time.Time
}
```

### 3.3 TeammateMessage — Mailbox 消息

```go
// internal/team/mailbox.go

type TeammateMessage struct {
    From      string    `json:"from"`
    Text      string    `json:"text"`
    Summary   string    `json:"summary,omitempty"`
    Timestamp time.Time `json:"timestamp"`
    Read      bool      `json:"read"`
    Color     string    `json:"color,omitempty"`
}

// StructuredMessage 是结构化消息的 discriminated union
type StructuredMessage struct {
    Type string `json:"type"` // "shutdown_request", "shutdown_response", "plan_approval_response"

    // shutdown_request
    Reason string `json:"reason,omitempty"`

    // shutdown_response / plan_approval_response
    RequestID string `json:"request_id,omitempty"`
    Approve   bool   `json:"approve,omitempty"`
    Feedback  string `json:"feedback,omitempty"`
}
```

---

## 4. 状态机

### 4.1 Teammate 生命周期状态机

```
                    ┌──────────────────────┐
                    │                      │
      ┌─────────►  pending  ──────────►  running
      │                                    │
      │                                    │ (turn 完成)
      │                                    ▼
      │                                  idle ◄─── (新消息到达)
      │                                    │         │
      │                                    │         │
      │               (shutdown approved)  │    (新消息)
      │                ┌───────────────────┤         │
      │                │                   │         │
      │                ▼                   ▼         │
      │           completed            running ◄─────┘
      │
      │  (error)                (abort/kill)
      └──── failed                killed
```

**状态转换规则**：

| 当前状态 | 事件 | 新状态 | 动作 |
|---------|------|--------|------|
| pending | 启动 agent loop | running | 开始 agent.Run() |
| running | turn 完成 | idle | 通知 onIdleCallbacks，开始 mailbox 轮询 |
| idle | 新消息到达 | running | 将消息注入 agent 上下文，重新运行 |
| idle | shutdown_request | running | 将 shutdown 消息注入 agent，等待 approve |
| running | shutdown_response(approve=true) | completed | abort controller 取消 |
| running | context cancel | killed | 清理资源 |
| running | error | failed | 记录错误，通知 leader |
| any terminal | 30s grace | evicted | 从 UI panel 移除 |

### 4.2 Team 整体状态机

```
  empty ──► created ──► active ──► dissolving ──► dissolved
              │                       ▲
              │                       │
              └──── (team_delete) ────┘
```

| 状态 | 含义 |
|------|------|
| empty | 无团队 |
| created | TeamFile 已创建，仅 leader |
| active | 有 >=1 teammate 在运行 |
| dissolving | Leader 发送 shutdown，等待所有 teammate 退出 |
| dissolved | 所有 teammate 已退出，TeamFile 清理 |

---

## 5. TeamManager — 核心协调器

```go
// internal/team/manager.go

type TeamManager struct {
    mu              sync.RWMutex
    teamName        string
    teamFile        *TeamFile
    teammates       map[string]*TeammateState  // agentID → state
    mailboxDir      string                     // ~/.jcode/teams/{name}/inboxes/

    // 依赖注入
    modelFactory    func(modelName string) (model.ToolCallingChatModel, error)
    toolFactory     func(env *tools.Env, depth int) []tool.BaseTool
    promptBuilder   func(ctx context.Context, env *tools.Env) string
    approvalFunc    func(agentID, toolName, toolArgs string) (bool, error)

    // TUI 通信
    tuiProgram      *tea.Program
    eventBus        *EventBus

    // 颜色分配
    colorIndex      int
    colorPalette    []string
}
```

### 5.1 核心方法

```go
// CreateTeam 创建新团队
func (m *TeamManager) CreateTeam(name, description string) error

// SpawnTeammate 启动一个新 teammate
func (m *TeamManager) SpawnTeammate(ctx context.Context, config SpawnConfig) (string, error)

// SendMessage 向指定 teammate 发送消息
func (m *TeamManager) SendMessage(to, from, message, summary string) error

// BroadcastMessage 广播消息给所有 teammate（除了 sender）
func (m *TeamManager) BroadcastMessage(from, message, summary string) error

// ShutdownTeammate 请求 teammate 优雅关闭
func (m *TeamManager) ShutdownTeammate(name, reason string) error

// KillTeammate 强制终止 teammate
func (m *TeamManager) KillTeammate(name string) error

// DissolveTeam 解散整个团队
func (m *TeamManager) DissolveTeam(ctx context.Context) error

// GetTeammateState 获取 teammate 运行时状态
func (m *TeamManager) GetTeammateState(agentID string) *TeammateState

// ListTeammates 列出所有 teammate 状态
func (m *TeamManager) ListTeammates() []*TeammateState
```

### 5.2 Teammate 执行器（goroutine）

```go
// internal/team/runner.go

// runTeammate 是每个 teammate 的主循环，运行在独立 goroutine 中
func (m *TeamManager) runTeammate(ctx context.Context, state *TeammateState) {
    defer m.cleanupTeammate(state)

    for {
        select {
        case <-ctx.Done():
            m.updateStatus(state, StatusKilled)
            return
        default:
        }

        // 1. 构建 agent（每个 turn 可以重建或复用）
        agent, err := m.buildAgent(ctx, state)
        if err != nil {
            m.updateStatus(state, StatusFailed)
            return
        }

        // 2. 运行 agent turn
        result, err := m.runAgentTurn(ctx, state, agent)
        if err != nil {
            if ctx.Err() != nil {
                m.updateStatus(state, StatusKilled)
                return
            }
            m.updateStatus(state, StatusFailed)
            return
        }

        // 3. 检查是否应该退出
        if state.ShutdownReq && result.ShutdownApproved {
            m.updateStatus(state, StatusCompleted)
            return
        }

        // 4. 标记为 idle，等待下一条消息
        m.updateStatus(state, StatusIdle)
        m.notifyIdle(state)

        // 5. 等待 mailbox 消息或用户消息
        msg, err := m.waitForMessage(ctx, state)
        if err != nil {
            if ctx.Err() != nil {
                m.updateStatus(state, StatusKilled)
                return
            }
            continue
        }

        // 6. 注入消息，继续循环
        state.PendingMessages = append(state.PendingMessages, msg.Text)
        m.updateStatus(state, StatusRunning)
    }
}

// waitForMessage 轮询 mailbox + 内存队列，500ms 间隔
func (m *TeamManager) waitForMessage(ctx context.Context, state *TeammateState) (*TeammateMessage, error) {
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-ticker.C:
            // 1. 检查内存中的 pending messages（来自 TUI 直接输入）
            if len(state.PendingMessages) > 0 {
                msg := state.PendingMessages[0]
                state.PendingMessages = state.PendingMessages[1:]
                return &TeammateMessage{Text: msg, From: "user"}, nil
            }

            // 2. 检查 mailbox（来自其他 agent）
            msgs, err := m.readUnreadMailbox(state.Identity.AgentName)
            if err != nil {
                continue
            }

            // 优先处理 shutdown_request
            for i, msg := range msgs {
                if isShutdownRequest(msg.Text) {
                    m.markMessageRead(state.Identity.AgentName, i)
                    state.ShutdownReq = true
                    return &msg, nil
                }
            }

            // 处理普通消息
            if len(msgs) > 0 {
                m.markMessageRead(state.Identity.AgentName, 0)
                return &msgs[0], nil
            }
        }
    }
}
```

---

## 6. Context 隔离

### 6.1 Go Context 方案（对标 AsyncLocalStorage）

Go 使用 `context.Context` 实现类似 AsyncLocalStorage 的隔离：

```go
// internal/team/context.go

type contextKey string

const (
    keyTeammateIdentity contextKey = "teammate_identity"
    keyTeamName         contextKey = "team_name"
    keyAgentID          contextKey = "agent_id"
    keyAgentName        contextKey = "agent_name"
)

// WithTeammateContext 将 teammate 上下文注入 context
func WithTeammateContext(ctx context.Context, identity TeammateIdentity) context.Context {
    ctx = context.WithValue(ctx, keyTeammateIdentity, identity)
    ctx = context.WithValue(ctx, keyTeamName, identity.TeamName)
    ctx = context.WithValue(ctx, keyAgentID, identity.AgentID)
    ctx = context.WithValue(ctx, keyAgentName, identity.AgentName)
    return ctx
}

// GetTeammateIdentity 从 context 获取 teammate 身份
func GetTeammateIdentity(ctx context.Context) (TeammateIdentity, bool) {
    id, ok := ctx.Value(keyTeammateIdentity).(TeammateIdentity)
    return id, ok
}

// IsTeammate 检查当前 context 是否在 teammate 中运行
func IsTeammate(ctx context.Context) bool {
    _, ok := GetTeammateIdentity(ctx)
    return ok
}

// GetAgentName 获取当前 agent 名称（leader 返回 "team-lead"）
func GetAgentName(ctx context.Context) string {
    if id, ok := GetTeammateIdentity(ctx); ok {
        return id.AgentName
    }
    return "team-lead"
}
```

### 6.2 Env 隔离

每个 Teammate 拥有独立的 `Env`：

```go
// 扩展现有 CloneForSubagent
func (e *Env) CloneForTeammate(agentName string) *Env {
    return &Env{
        Exec:        e.Exec,           // 共享执行器
        pwd:         e.pwd,            // 共享工作目录
        TodoStore:   NewTodoStore(),   // 独立 todo
        PlanStore:   NewPlanStore(),   // 独立 plan
        FileTracker: NewFileTracker(), // 独立文件追踪
        platform:    e.platform,
        Depth:       e.Depth + 1,
        AgentName:   agentName,        // NEW: 标识所属 agent
    }
}
```

---

## 7. Mailbox 系统

### 7.1 文件布局

```
~/.jcode/teams/
└── {team_name}/
    ├── team.json              # TeamFile
    ├── inboxes/
    │   ├── team-lead.json     # Leader 的收件箱
    │   ├── researcher.json    # Teammate 收件箱
    │   └── coder.json
    └── memory/
        ├── MEMORY.md          # 共享团队记忆索引
        ├── researcher/        # Per-agent 记忆目录
        └── coder/
```

### 7.2 Mailbox 实现

```go
// internal/team/mailbox.go

type Mailbox struct {
    baseDir string  // ~/.jcode/teams/{name}/inboxes/
}

func NewMailbox(teamName string) *Mailbox {
    baseDir := filepath.Join(config.JCodeDir(), "teams", teamName, "inboxes")
    os.MkdirAll(baseDir, 0755)
    return &Mailbox{baseDir: baseDir}
}

func (mb *Mailbox) inboxPath(agentName string) string {
    // 安全化路径，防止路径遍历
    safe := sanitizeAgentName(agentName)
    return filepath.Join(mb.baseDir, safe+".json")
}

// WriteMessage 写入消息（带文件锁）
func (mb *Mailbox) WriteMessage(recipientName string, msg TeammateMessage) error {
    path := mb.inboxPath(recipientName)
    lockPath := path + ".lock"

    // 获取文件锁（重试 10 次，5-100ms backoff）
    lock, err := acquireFileLock(lockPath, 10, 5*time.Millisecond, 100*time.Millisecond)
    if err != nil {
        return fmt.Errorf("acquire lock: %w", err)
    }
    defer lock.Release()

    // 读取现有消息
    messages, err := mb.readAllMessages(recipientName)
    if err != nil {
        messages = []TeammateMessage{}
    }

    msg.Read = false
    messages = append(messages, msg)

    // 写回文件
    data, _ := json.MarshalIndent(messages, "", "  ")
    return os.WriteFile(path, data, 0644)
}

// ReadUnread 读取未读消息
func (mb *Mailbox) ReadUnread(agentName string) ([]TeammateMessage, error) {
    all, err := mb.readAllMessages(agentName)
    if err != nil {
        return nil, err
    }
    var unread []TeammateMessage
    for _, m := range all {
        if !m.Read {
            unread = append(unread, m)
        }
    }
    return unread, nil
}

// MarkRead 标记消息已读
func (mb *Mailbox) MarkRead(agentName string, index int) error {
    path := mb.inboxPath(agentName)
    lockPath := path + ".lock"

    lock, err := acquireFileLock(lockPath, 10, 5*time.Millisecond, 100*time.Millisecond)
    if err != nil {
        return err
    }
    defer lock.Release()

    messages, err := mb.readAllMessages(agentName)
    if err != nil || index >= len(messages) {
        return err
    }
    messages[index].Read = true

    data, _ := json.MarshalIndent(messages, "", "  ")
    return os.WriteFile(path, data, 0644)
}

// sanitizeAgentName 防止路径遍历攻击
func sanitizeAgentName(name string) string {
    // 移除 ../ 、绝对路径、unicode 攻击字符
    name = filepath.Base(name)
    name = strings.Map(func(r rune) rune {
        if r == '/' || r == '\\' || r == '.' || r > 127 {
            return '_'
        }
        return r
    }, name)
    if name == "" || name == "_" {
        return "unknown"
    }
    return name
}
```

---

## 8. Team 相关 Tools

### 8.1 工具清单

| Tool 名称 | 功能 | 权限 |
|-----------|------|------|
| `team_create` | 创建团队 | 需要审批 |
| `team_delete` | 解散团队 | 需要审批 |
| `team_spawn` | 启动 teammate | 需要审批 |
| `team_send_message` | 发送消息 | 自动（Read-only 类） |
| `team_list` | 列出 teammates | 自动 |
| `team_status` | 查看 teammate 状态 | 自动 |

### 8.2 team_create Tool

```go
// internal/tools/team_create.go

type teamCreateInput struct {
    TeamName    string `json:"team_name"    desc:"团队名称" required:"true"`
    Description string `json:"description"  desc:"团队描述/目的"`
}

func (t *TeamCreateTool) Run(ctx context.Context, input string) (string, error) {
    var args teamCreateInput
    json.Unmarshal([]byte(input), &args)

    if t.manager.HasTeam() {
        return "", fmt.Errorf("已有活跃团队 %q，不能同时管理多个团队", t.manager.TeamName())
    }

    err := t.manager.CreateTeam(args.TeamName, args.Description)
    if err != nil {
        return "", err
    }

    return fmt.Sprintf("团队 %q 创建成功。Lead Agent ID: %s", args.TeamName, t.manager.LeadAgentID()), nil
}
```

### 8.3 team_spawn Tool

```go
// internal/tools/team_spawn.go

type teamSpawnInput struct {
    Name        string `json:"name"         desc:"Teammate 名称" required:"true"`
    Prompt      string `json:"prompt"       desc:"初始任务指令" required:"true"`
    AgentType   string `json:"agent_type"   desc:"Agent 类型: explore, coder, reviewer"`
    Model       string `json:"model"        desc:"模型覆盖（默认用当前模型）"`
    Cwd         string `json:"cwd"          desc:"工作目录（默认当前目录）"`
    Mode        string `json:"mode"         desc:"权限模式: normal, plan, auto"`
}

func (t *TeamSpawnTool) Run(ctx context.Context, input string) (string, error) {
    var args teamSpawnInput
    json.Unmarshal([]byte(input), &args)

    if !t.manager.HasTeam() {
        return "", fmt.Errorf("没有活跃团队，请先使用 team_create 创建团队")
    }

    agentID, err := t.manager.SpawnTeammate(ctx, SpawnConfig{
        Name:       args.Name,
        Prompt:     args.Prompt,
        AgentType:  args.AgentType,
        Model:      args.Model,
        Cwd:        args.Cwd,
        Permission: args.Mode,
    })
    if err != nil {
        return "", err
    }

    return fmt.Sprintf("Teammate %q 已启动 (ID: %s)", args.Name, agentID), nil
}
```

### 8.4 team_send_message Tool

```go
// internal/tools/team_send_message.go

type teamSendMessageInput struct {
    To      string `json:"to"       desc:"接收者: teammate名称, '*' 广播" required:"true"`
    Message string `json:"message"  desc:"消息内容" required:"true"`
    Summary string `json:"summary"  desc:"5-10字摘要（用于 UI 预览）"`
}

func (t *TeamSendMessageTool) Run(ctx context.Context, input string) (string, error) {
    var args teamSendMessageInput
    json.Unmarshal([]byte(input), &args)

    from := team.GetAgentName(ctx)

    if args.To == "*" {
        count, err := t.manager.BroadcastMessage(from, args.Message, args.Summary)
        if err != nil {
            return "", err
        }
        return fmt.Sprintf("消息已广播给 %d 个 teammate", count), nil
    }

    err := t.manager.SendMessage(args.To, from, args.Message, args.Summary)
    if err != nil {
        return "", err
    }
    return fmt.Sprintf("消息已发送给 @%s", args.To), nil
}
```

---

## 9. Memory 隔离与存储

### 9.1 分层记忆架构

```
~/.jcode/teams/{team_name}/memory/
├── MEMORY.md              # 共享团队记忆索引（所有 agent 可读写）
├── researcher/            # researcher 专属记忆
│   ├── findings.md
│   └── notes.md
├── coder/                 # coder 专属记忆
│   ├── implementation.md
│   └── issues.md
└── shared/                # 共享资源
    ├── context.md         # 项目上下文
    └── decisions.md       # 决策记录
```

### 9.2 Memory 隔离规则

```go
// internal/team/memory.go

type TeamMemory struct {
    baseDir string
}

func (m *TeamMemory) GetAgentMemDir(agentName string) string {
    safe := sanitizeAgentName(agentName)
    dir := filepath.Join(m.baseDir, safe)
    os.MkdirAll(dir, 0755)
    return dir
}

func (m *TeamMemory) GetSharedMemDir() string {
    dir := filepath.Join(m.baseDir, "shared")
    os.MkdirAll(dir, 0755)
    return dir
}

// ValidateAccess 检查 agent 是否有权访问某个记忆路径
func (m *TeamMemory) ValidateAccess(agentName, targetPath string) error {
    // 防止符号链接逃逸
    realPath, err := filepath.EvalSymlinks(targetPath)
    if err != nil {
        return err
    }

    // 允许访问: 自己的目录 + shared 目录 + MEMORY.md
    allowedPrefixes := []string{
        m.GetAgentMemDir(agentName),
        m.GetSharedMemDir(),
        filepath.Join(m.baseDir, "MEMORY.md"),
    }

    for _, prefix := range allowedPrefixes {
        if strings.HasPrefix(realPath, prefix) {
            return nil
        }
    }
    return fmt.Errorf("agent %q 无权访问 %q", agentName, targetPath)
}
```

---

## 10. Conversation 隔离与存储

### 10.1 Session 扩展

```go
// 扩展现有 session.Entry
type Entry struct {
    // 现有字段...
    Type      EntryType `json:"type"`
    UUID      string    `json:"uuid,omitempty"`
    Content   string    `json:"content,omitempty"`
    Name      string    `json:"name,omitempty"`
    Args      string    `json:"args,omitempty"`
    Output    string    `json:"output,omitempty"`

    // 新增 Team 字段
    AgentID   string    `json:"agent_id,omitempty"`   // 哪个 agent 产生的
    TeamName  string    `json:"team_name,omitempty"`  // 所属团队
}
```

### 10.2 Per-Agent Session 文件

```
~/.jcode/sessions/
├── {session-uuid}.jsonl              # Leader 的 session（现有）
└── teams/
    └── {team_name}/
        ├── researcher.jsonl          # Teammate session
        └── coder.jsonl
```

每个 Teammate 有独立的 Recorder：

```go
func (m *TeamManager) createTeammateRecorder(agentName, teamName string) *session.Recorder {
    dir := filepath.Join(config.JCodeDir(), "sessions", "teams", teamName)
    os.MkdirAll(dir, 0755)
    path := filepath.Join(dir, agentName+".jsonl")
    return session.NewRecorder(path)
}
```

### 10.3 消息容量管控

参考 Claude Code 的 `TEAMMATE_MESSAGES_UI_CAP = 50`：

```go
const TeammateMessagesCap = 50

func appendCappedMessage(msgs []*schema.Message, msg *schema.Message) []*schema.Message {
    msgs = append(msgs, msg)
    if len(msgs) > TeammateMessagesCap {
        msgs = msgs[len(msgs)-TeammateMessagesCap:]
    }
    return msgs
}
```

---

## 11. TUI 设计

### 11.1 组件层次结构

```
Model (main BubbleTea model)
├── viewport (conversation area)
│   ├── LeaderView (default - leader's conversation)
│   └── TeammateView (when viewing a teammate)
│       ├── TeammateViewHeader ("Viewing @researcher · [esc] return")
│       └── TeammateMessages (capped message list)
├── inputBox (textarea)
│   └── input routing → leader or viewed teammate
├── statusBar
│   ├── mode indicator
│   ├── env label
│   ├── token count
│   └── TeamStatusPill ("3 teammates")
└── CoordinatorPanel (bottom panel)
    ├── MainLine (return to leader) "● Main" / "○ Main"
    └── AgentLine[] (per running teammate)
        ├── color indicator (● viewed / ○ not viewed)
        ├── agent name + type
        ├── status (running/idle/completed)
        └── elapsed timer
```

### 11.2 新增 TUI 消息类型

```go
// internal/tui/team_messages.go

// TeammateSpawnedMsg 通知 TUI 新 teammate 已启动
type TeammateSpawnedMsg struct {
    AgentID   string
    Name      string
    Color     string
    Prompt    string
}

// TeammateStatusMsg 通知 TUI teammate 状态变化
type TeammateStatusMsg struct {
    AgentID string
    Status  team.TeammateStatus
    Error   string
}

// TeammateProgressMsg 通知 TUI teammate 进度更新
type TeammateProgressMsg struct {
    AgentID     string
    ToolName    string
    ToolCount   int
    TokenCount  int64
}

// TeammateMessageMsg 通知 TUI teammate 有新的对话消息
type TeammateMessageMsg struct {
    AgentID string
    Message *schema.Message
}

// TeamViewSwitchMsg 切换到查看某个 teammate 的对话
type TeamViewSwitchMsg struct {
    AgentID string  // 空字符串 = 返回 leader
}

// TeamPanelToggleMsg 切换 coordinator panel 的显示/隐藏
type TeamPanelToggleMsg struct{}
```

### 11.3 键绑定方案

```go
// internal/tui/keybindings.go

// Team 相关键绑定
var teamKeyMap = map[string]string{
    "shift+up":   "team:prevAgent",      // 切换到上一个 agent 视图
    "shift+down": "team:nextAgent",      // 切换到下一个 agent 视图
    "escape":     "team:exitView",       // 退出 teammate 视图，返回 leader
    "ctrl+t":     "team:togglePanel",    // 切换 coordinator panel
}
```

**交互流程**：

1. **Shift+Up/Down — 切换 Agent 视图**
   ```
   User 按 Shift+Up:
     → 获取 teammates 列表（按启动时间排序）
     → 计算当前 viewIndex
     → viewIndex-- (向上切换)
     → 如果 viewIndex == 0 → 切回 Leader
     → 否则 → 进入 teammates[viewIndex-1] 的视图
     → 更新 viewport 内容为该 agent 的 messages
     → 更新 statusBar 显示 agent 颜色和名称
     → 输入回路由到该 agent
   ```

2. **Escape — 返回 Leader**
   ```
   User 按 Escape (在 teammate view 中):
     → 设置 viewingAgentID = ""
     → 释放 teammate (清除 UI messages，设置 evict timer)
     → viewport 切回 leader 的 conversation
     → 输入路由回 leader
   ```

3. **Enter (在 Coordinator Panel 中)**
   ```
   User 按 Enter (选中某个 teammate):
     → 进入该 teammate 的详情视图
     → 显示: agent info, status, last tool, progress
     → 再次 Enter → 切换到该 teammate 的会话视图
   ```

### 11.4 输入路由

```go
// internal/tui/input_routing.go

func (m *Model) routeInput(input string) tea.Cmd {
    if m.viewingAgentID != "" {
        // 当前在查看某个 teammate
        return m.sendToTeammate(m.viewingAgentID, input)
    }
    // 默认路由到 leader
    return m.sendToLeader(input)
}

func (m *Model) sendToTeammate(agentID, input string) tea.Cmd {
    return func() tea.Msg {
        // 将消息添加到 teammate 的 pendingMessages
        m.teamManager.EnqueueUserMessage(agentID, input)
        return nil
    }
}
```

### 11.5 视图切换状态机

```go
// internal/tui/view_state.go

type ViewMode int

const (
    ViewModeLeader    ViewMode = iota  // 查看 leader 对话
    ViewModeTeammate                    // 查看 teammate 对话
    ViewModePanel                       // 在 coordinator panel 导航
)

// enterTeammateView 切换到 teammate 视图
func (m *Model) enterTeammateView(agentID string) {
    // 释放之前查看的 teammate（清除 messages，设置 evict timer）
    if m.viewingAgentID != "" {
        m.releaseTeammateView(m.viewingAgentID)
    }

    // 进入新 teammate 的视图
    m.viewingAgentID = agentID
    m.viewMode = ViewModeTeammate

    // 加载该 teammate 的最近消息到 viewport
    state := m.teamManager.GetTeammateState(agentID)
    if state != nil {
        m.loadTeammateMessages(state.Messages)
    }
}

// exitTeammateView 返回 leader 视图
func (m *Model) exitTeammateView() {
    if m.viewingAgentID != "" {
        m.releaseTeammateView(m.viewingAgentID)
    }
    m.viewingAgentID = ""
    m.viewMode = ViewModeLeader
    m.restoreLeaderViewport()
}

// releaseTeammateView 释放 teammate 的 UI 资源
func (m *Model) releaseTeammateView(agentID string) {
    state := m.teamManager.GetTeammateState(agentID)
    if state == nil {
        return
    }
    // 清除 UI messages（节省内存）
    state.Messages = nil
    // 如果是终态，设置 30s evict timer
    if state.Status.IsTerminal() {
        state.EvictAfter = time.Now().Add(30 * time.Second)
    }
}
```

### 11.6 Coordinator Panel 渲染

```go
// internal/tui/coordinator_panel.go

func (m *Model) renderCoordinatorPanel() string {
    if !m.teamManager.HasTeam() {
        return ""
    }

    var lines []string
    teammates := m.teamManager.ListTeammates()

    // Header
    lines = append(lines, m.teamStatusPill(len(teammates)))

    // Main line (Leader)
    if m.viewingAgentID == "" {
        lines = append(lines, "● Main")
    } else {
        lines = append(lines, "○ Main")
    }

    // Teammate lines
    for _, t := range teammates {
        indicator := "○"
        if m.viewingAgentID == t.Identity.AgentID {
            indicator = "●"
        }

        statusIcon := statusToIcon(t.Status)
        elapsed := time.Since(t.Identity.JoinedAt).Truncate(time.Second)

        line := fmt.Sprintf("%s %s @%s %s %s",
            lipgloss.NewStyle().Foreground(lipgloss.Color(t.Color)).Render(indicator),
            statusIcon,
            t.Identity.AgentName,
            t.Status,
            elapsed,
        )
        lines = append(lines, line)
    }

    return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func statusToIcon(s team.TeammateStatus) string {
    switch s {
    case team.StatusRunning:
        return "⟳"
    case team.StatusIdle:
        return "◇"
    case team.StatusCompleted:
        return "✓"
    case team.StatusFailed:
        return "✗"
    case team.StatusKilled:
        return "⊘"
    default:
        return "…"
    }
}
```

### 11.7 Teammate 消息渲染

```go
// internal/tui/teammate_message.go

// 当 leader 收到 teammate 消息时，以特殊格式渲染
func renderTeammateMessage(msg TeammateMessage) string {
    nameStyle := lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color(msg.Color))

    summaryStyle := lipgloss.NewStyle().
        Faint(true)

    header := nameStyle.Render("@"+msg.From) + " " + summaryStyle.Render(msg.Summary)
    return header + "\n" + msg.Text
}

// TeammateViewHeader 在 teammate 视图顶部显示
func renderTeammateViewHeader(identity TeammateIdentity) string {
    nameStyle := lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color(identity.Color))

    return fmt.Sprintf("Viewing %s · [esc] return",
        nameStyle.Render("@"+identity.AgentName))
}
```

---

## 12. 与 Eino multiagent/host 的集成

### 12.1 Host 模式 vs Swarm 模式对比

| 维度 | Eino Host | Claude Code Swarm | jcode 设计 |
|------|-----------|-------------------|-----------|
| 协调方式 | Graph-based 路由 | Mailbox-based 消息 | **混合**: 简单路由用 Host graph，复杂协作用 Mailbox |
| Agent 生命周期 | 单次调用 | 持久 goroutine | 持久 goroutine |
| 通信 | 通过 Host 转发 | 直接 P2P mailbox | 直接 P2P mailbox |
| 状态隔离 | `compose.ProcessState` | AsyncLocalStorage | `context.Context` + Env clone |

### 12.2 利用 Eino Host 的场景

对于简单的 **单轮多专家路由**（如"分析这段代码的安全性和性能"），可以直接使用 Eino 的 `multiagent/host`：

```go
// internal/team/host_agent.go

func (m *TeamManager) CreateHostAgent(ctx context.Context, specialists []HostSpecialist) (*host.MultiAgent, error) {
    hostModel, _ := m.modelFactory(m.teamFile.Members[0].Model)

    specs := make([]*host.Specialist, len(specialists))
    for i, s := range specialists {
        specModel, _ := m.modelFactory(s.Model)
        specs[i] = &host.Specialist{
            AgentMeta: host.AgentMeta{
                Name:        s.Name,
                IntendedUse: s.Description,
            },
            ChatModel:    specModel,
            SystemPrompt: s.SystemPrompt,
        }
    }

    return host.NewMultiAgent(ctx, &host.MultiAgentConfig{
        Host: host.Host{
            ToolCallingModel: hostModel.(model.ToolCallingChatModel),
            SystemPrompt:     "Route the task to the most appropriate specialist.",
        },
        Specialists: specs,
    })
}
```

### 12.3 决策：何时用哪种模式

```
用户任务 
  │
  ├── 简单路由? (多专家各做一部分，无须多轮协作)
  │   └── 使用 Eino Host 模式
  │       → host.MultiAgent.Generate() / Stream()
  │
  └── 复杂协作? (需要多轮交互、共享文件、互相 review)
      └── 使用 Swarm 模式
          → TeamManager.SpawnTeammate() + Mailbox
```

---

## 13. 实现路线图

### Phase 1: 核心基础设施

1. **`internal/team/types.go`** — 数据模型定义
2. **`internal/team/context.go`** — Context 隔离
3. **`internal/team/mailbox.go`** — Mailbox 消息系统
4. **`internal/team/manager.go`** — TeamManager 核心
5. **`internal/team/runner.go`** — Teammate 执行循环

### Phase 2: Tools

6. **`internal/tools/team_create.go`** — team_create tool
7. **`internal/tools/team_spawn.go`** — team_spawn tool
8. **`internal/tools/team_send_message.go`** — team_send_message tool
9. **`internal/tools/team_list.go`** — team_list tool
10. **`internal/tools/team_delete.go`** — team_delete tool

### Phase 3: TUI

11. **`internal/tui/team_messages.go`** — Team 相关 TUI 消息类型
12. **`internal/tui/coordinator_panel.go`** — Coordinator 面板组件
13. **`internal/tui/teammate_view.go`** — Teammate 视图切换
14. **`internal/tui/input_routing.go`** — 输入路由逻辑
15. 修改 `internal/tui/tui.go` — 集成 team 组件

### Phase 4: Session & Memory

16. **扩展 `internal/session/session.go`** — AgentID 字段
17. **`internal/team/memory.go`** — Team memory 隔离
18. **`internal/team/session.go`** — Per-agent session 文件

### Phase 5: 集成 & 配置

19. **扩展 `internal/config/config.go`** — Team 配置项
20. **修改 `cmd/jcode/main.go`** — 初始化 TeamManager
21. **修改 `internal/runner/runner.go`** — 支持 team 模式
22. **集成 Eino Host** — 简单路由场景

### Phase 6: Prompt & Skill

23. **扩展 `internal/prompts/system.md`** — 团队能力描述
24. **添加 team skill** — `internal/skills/builtin/team.md`

---

## 14. 配置扩展

```go
// 扩展 config.Config
type TeamConfig struct {
    MaxTeammates    int    `json:"max_teammates"`    // 最大 teammate 数（默认 5）
    MaxTeams        int    `json:"max_teams"`        // 最大团队数（默认 1）
    DefaultModel    string `json:"default_model"`    // Teammate 默认模型
    MailboxPollMs   int    `json:"mailbox_poll_ms"`  // Mailbox 轮询间隔（默认 500）
    MessageCap      int    `json:"message_cap"`      // UI 消息上限（默认 50）
    IdleTimeoutSec  int    `json:"idle_timeout_sec"` // Idle 超时自动清理（默认 300）
    EvictGraceSec   int    `json:"evict_grace_sec"`  // 终态 evict 延迟（默认 30）
}
```

---

## 15. 安全考量

1. **路径遍历防护** — `sanitizeAgentName()` 防止 `../` 攻击
2. **符号链接逃逸** — `filepath.EvalSymlinks()` 验证真实路径
3. **文件锁竞争** — 带 backoff 的重试机制
4. **内存限制** — `TeammateMessagesCap` 防止 OOM
5. **深度限制** — `MaxDepth` 防止无限嵌套
6. **并发安全** — `sync.RWMutex` 保护 TeamManager 状态
7. **优雅关闭** — shutdown 协议确保资源清理
8. **权限隔离** — 每个 teammate 可配置独立的 PermissionMode
