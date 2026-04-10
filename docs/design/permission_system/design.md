# 权限系统增强 — 技术设计文档

## 架构概述

### 设计原则

1. **渐进增强**: 在现有 `ApprovalState` 基础上扩展，不破坏已有接口
2. **关注点分离**: 规则存储、规则评估、审批交互、策略执行分别独立
3. **零配置可用**: 无权限文件时退回当前白名单行为，无感知升级
4. **接口优先**: 核心组件通过 Go 接口定义，便于测试和扩展

### 系统架构图

```
┌──────────────────────────────────────────────────────────────────┐
│                        Agent Loop (Eino)                         │
│                                                                  │
│  ┌──────────────┐    ┌──────────────────┐    ┌───────────────┐  │
│  │  Tool Call    │───▶│ approvalMiddleware│───▶│  Tool Invoke  │  │
│  │ (agent.go)   │    │ (middleware.go)   │    │  (tools/*)    │  │
│  └──────────────┘    └────────┬─────────┘    └───────────────┘  │
│                               │                                  │
└───────────────────────────────┼──────────────────────────────────┘
                                │
                    ┌───────────▼───────────┐
                    │   PermissionEngine    │  ← 新增核心组件
                    │   (permission/)       │
                    ├───────────────────────┤
                    │ • RuleStore (持久化)   │
                    │ • RuleEvaluator (评估) │
                    │ • PolicyEnforcer (策略)│
                    │ • AuditLogger (审计)  │
                    └───────────┬───────────┘
                                │
              ┌─────────────────┼─────────────────┐
              ▼                 ▼                  ▼
     ┌────────────┐   ┌────────────────┐  ┌──────────────┐
     │ RuleStore   │   │ ApprovalQueue  │  │ PolicyStore  │
     │ JSON Files  │   │ (async queue)  │  │ policy.json  │
     └────────────┘   └───────┬────────┘  └──────────────┘
                              │
                     ┌────────▼────────┐
                     │  TUI Approval   │
                     │  (tui/)         │
                     └─────────────────┘
```

### 包结构

```
internal/
  permission/            ← 新增包
    engine.go            # PermissionEngine 主入口
    rule.go              # Rule 定义 + RuleStore 接口/实现
    evaluator.go         # RuleEvaluator 规则评估引擎
    policy.go            # PolicyEnforcer 策略执行
    queue.go             # ApprovalQueue 异步批准队列
    audit.go             # AuditLogger 审计日志
    mcp.go               # MCP 工具权限命名解析
    permission_test.go   # 单元测试
  runner/
    approval.go          # 改造: 委托给 PermissionEngine
  agent/
    middleware.go         # 不变: 仍通过 ApprovalFunc 调用
```

---

## 核心组件

### 1. Rule — 权限规则定义

```go
// internal/permission/rule.go

package permission

import (
    "regexp"
    "time"
)

// Action 定义规则的行为类型
type Action string

const (
    ActionAllow Action = "allow"
    ActionDeny  Action = "deny"
    ActionAsk   Action = "ask"
)

// Scope 定义规则的作用范围
type Scope string

const (
    ScopeSession Scope = "session"  // 当前会话
    ScopeProject Scope = "project"  // 项目级 (<project>/.jcoding/permissions.json)
    ScopeUser    Scope = "user"     // 用户级 (~/.jcoding/permissions.json)
)

// Rule 描述一条权限规则
type Rule struct {
    ID          string    `json:"id"`
    Tool        string    `json:"tool"`                  // 工具名，支持 glob（如 mcp__github__*）
    Action      Action    `json:"action"`
    ArgsPattern string    `json:"args_pattern,omitempty"` // 参数正则（可选）
    Scope       Scope     `json:"scope"`
    Reason      string    `json:"reason,omitempty"`       // deny 原因说明
    CreatedAt   time.Time `json:"created_at"`

    compiledPattern *regexp.Regexp // 缓存编译后的正则
}

// Match 判断该规则是否匹配给定的工具调用
func (r *Rule) Match(toolName, toolArgs string) bool {
    // 1. 工具名匹配（精确匹配或 glob 模式）
    if !matchGlob(r.Tool, toolName) {
        return false
    }
    // 2. 参数模式匹配（无模式时仅匹配工具名）
    if r.ArgsPattern == "" {
        return true
    }
    if r.compiledPattern == nil {
        r.compiledPattern, _ = regexp.Compile(r.ArgsPattern)
    }
    if r.compiledPattern == nil {
        return false
    }
    return r.compiledPattern.MatchString(toolArgs)
}
```

### 2. RuleStore — 规则存储接口

```go
// internal/permission/rule.go

// RuleStore 定义规则的持久化存储接口
type RuleStore interface {
    // Load 从持久化存储加载规则列表
    Load() ([]Rule, error)

    // Save 将规则列表写入持久化存储
    Save(rules []Rule) error

    // Add 添加一条新规则
    Add(rule Rule) error

    // Remove 根据 ID 删除规则
    Remove(id string) error

    // Clear 清空所有规则
    Clear() error
}

// FileRuleStore 基于 JSON 文件的 RuleStore 实现
type FileRuleStore struct {
    path string // JSON 文件路径
}

// NewFileRuleStore 创建文件规则存储
// path 示例: ~/.jcoding/permissions.json 或 <project>/.jcoding/permissions.json
func NewFileRuleStore(path string) *FileRuleStore {
    return &FileRuleStore{path: path}
}
```

**文件格式** (`permissions.json`):
```json
{
  "version": 1,
  "rules": [
    {
      "id": "a1b2c3d4",
      "tool": "execute",
      "action": "allow",
      "args_pattern": "^go (test|build|vet|run)",
      "scope": "user",
      "created_at": "2026-04-10T00:00:00Z"
    }
  ]
}
```

### 3. RuleEvaluator — 规则评估引擎

```go
// internal/permission/evaluator.go

package permission

// Decision 代表评估结果
type Decision struct {
    Action  Action  // 最终行为
    RuleID  string  // 匹配的规则 ID（无匹配时为空）
    Source  Scope   // 规则来源层级
    Reason  string  // deny 原因
}

// RuleEvaluator 负责收集多层规则并按优先级评估
type RuleEvaluator struct {
    sessionRules []Rule       // 会话级规则（内存）
    projectStore RuleStore    // 项目级存储
    userStore    RuleStore    // 用户级存储
    policy       *PolicyEnforcer // 策略执行器（可选）
}

// NewRuleEvaluator 创建规则评估器
func NewRuleEvaluator(userStore, projectStore RuleStore) *RuleEvaluator {
    return &RuleEvaluator{
        userStore:    userStore,
        projectStore: projectStore,
    }
}

// SetPolicy 设置策略执行器（可选）
func (e *RuleEvaluator) SetPolicy(p *PolicyEnforcer) {
    e.policy = p
}

// Evaluate 对工具调用进行权限评估
//
// 评估优先级:
//   1. 策略 deny 规则（不可覆盖）
//   2. 会话级规则
//   3. 项目级规则
//   4. 用户级规则
//   5. 内置默认规则（现有白名单逻辑）
func (e *RuleEvaluator) Evaluate(toolName, toolArgs string) Decision {
    // Step 1: 策略检查（最高优先级）
    if e.policy != nil {
        if d, ok := e.policy.Check(toolName, toolArgs); ok {
            return d
        }
    }

    // Step 2: 按层级查找匹配规则
    layers := []struct {
        scope Scope
        rules []Rule
    }{
        {ScopeSession, e.sessionRules},
        {ScopeProject, e.loadRules(e.projectStore)},
        {ScopeUser, e.loadRules(e.userStore)},
    }

    for _, layer := range layers {
        // 在每层内: deny 优先于 allow 优先于 ask
        var matched []Rule
        for _, r := range layer.rules {
            if r.Match(toolName, toolArgs) {
                matched = append(matched, r)
            }
        }
        if d, ok := resolveMatched(matched, layer.scope); ok {
            return d
        }
    }

    // Step 3: 无匹配规则，返回默认行为
    return e.defaultDecision(toolName, toolArgs)
}

// AddSessionRule 添加会话级规则
func (e *RuleEvaluator) AddSessionRule(rule Rule) {
    rule.Scope = ScopeSession
    e.sessionRules = append(e.sessionRules, rule)
}

// loadRules 从 RuleStore 加载规则（内部方法）
func (e *RuleEvaluator) loadRules(store RuleStore) []Rule {
    if store == nil {
        return nil
    }
    rules, _ := store.Load()
    return rules
}

// defaultDecision 实现现有的白名单逻辑作为兜底
func (e *RuleEvaluator) defaultDecision(toolName, toolArgs string) Decision {
    // 保留现有 approval.go 中的白名单逻辑:
    // - noApprovalNeeded 工具 → allow
    // - read 工具 workpath 内 → allow
    // - execute 安全前缀 → allow
    // - 其他 → ask
    return Decision{Action: ActionAsk}
}
```

**规则解析优先级（同层内）**:

```
deny > allow > ask
```

若同层存在冲突的 deny 和 allow 规则，deny 胜出。

### 4. PermissionEngine — 主入口

```go
// internal/permission/engine.go

package permission

import (
    "context"
    "os"
    "path/filepath"
)

// ApprovalFunc 是与 agent 层兼容的审批回调类型
type ApprovalFunc func(ctx context.Context, toolName, toolArgs string) (bool, error)

// PermissionEngine 整合规则评估、审批队列、审计日志
type PermissionEngine struct {
    evaluator  *RuleEvaluator
    queue      *ApprovalQueue
    audit      *AuditLogger
    userStore  RuleStore
    projStore  RuleStore
}

// NewPermissionEngine 创建权限引擎
//
// configDir: ~/.jcoding/
// projectDir: 当前项目根目录（可为空）
func NewPermissionEngine(configDir, projectDir string) *PermissionEngine {
    userStore := NewFileRuleStore(filepath.Join(configDir, "permissions.json"))

    var projStore RuleStore
    if projectDir != "" {
        projPath := filepath.Join(projectDir, ".jcoding", "permissions.json")
        if _, err := os.Stat(filepath.Dir(projPath)); err == nil {
            projStore = NewFileRuleStore(projPath)
        }
    }

    evaluator := NewRuleEvaluator(userStore, projStore)

    // 可选: 加载策略文件
    policyPath := filepath.Join(configDir, "policy.json")
    if enforcer, err := LoadPolicyEnforcer(policyPath); err == nil {
        evaluator.SetPolicy(enforcer)
    }

    return &PermissionEngine{
        evaluator: evaluator,
        userStore: userStore,
        projStore: projStore,
        audit:     NewAuditLogger(filepath.Join(configDir, "audit.log")),
    }
}

// RequestApproval 实现 ApprovalFunc 签名，可直接传入 approvalMiddleware
func (e *PermissionEngine) RequestApproval(ctx context.Context, toolName, toolArgs string) (bool, error) {
    decision := e.evaluator.Evaluate(toolName, toolArgs)

    // 审计记录
    e.audit.Log(toolName, toolArgs, decision)

    switch decision.Action {
    case ActionAllow:
        return true, nil
    case ActionDeny:
        return false, nil
    case ActionAsk:
        // 委托给审批队列或直接用户交互
        if e.queue != nil {
            return e.queue.Enqueue(ctx, toolName, toolArgs)
        }
        // 兜底: 同步用户审批（保持现有行为）
        return false, nil
    }
    return false, nil
}

// SaveRule 保存规则到指定层级
func (e *PermissionEngine) SaveRule(rule Rule) error {
    switch rule.Scope {
    case ScopeUser:
        return e.userStore.Add(rule)
    case ScopeProject:
        if e.projStore != nil {
            return e.projStore.Add(rule)
        }
        return e.userStore.Add(rule) // 无项目存储时退回用户级
    case ScopeSession:
        e.evaluator.AddSessionRule(rule)
        return nil
    }
    return nil
}

// ListRules 列出所有生效规则（合并各层级）
func (e *PermissionEngine) ListRules() []Rule {
    var all []Rule
    all = append(all, e.evaluator.sessionRules...)
    if e.projStore != nil {
        if rules, err := e.projStore.Load(); err == nil {
            all = append(all, rules...)
        }
    }
    if rules, err := e.userStore.Load(); err == nil {
        all = append(all, rules...)
    }
    return all
}
```

### 5. MCP 权限命名解析

```go
// internal/permission/mcp.go

package permission

import "strings"

const mcpPrefix = "mcp__"

// MCPToolName 构造 MCP 工具的权限名称
// 格式: mcp__<serverName>__<toolName>
func MCPToolName(serverName, toolName string) string {
    return mcpPrefix + serverName + "__" + toolName
}

// MCPServerWildcard 构造 MCP 服务器的通配权限名称
// 格式: mcp__<serverName>__*
func MCPServerWildcard(serverName string) string {
    return mcpPrefix + serverName + "__*"
}

// ParseMCPToolName 解析权限名称中的 MCP 服务器与工具
// 返回 serverName, toolName, isMCP
func ParseMCPToolName(name string) (server, tool string, isMCP bool) {
    if !strings.HasPrefix(name, mcpPrefix) {
        return "", "", false
    }
    rest := strings.TrimPrefix(name, mcpPrefix)
    parts := strings.SplitN(rest, "__", 2)
    if len(parts) != 2 {
        return "", "", false
    }
    return parts[0], parts[1], true
}

// IsMCPTool 判断工具名是否为 MCP 工具
func IsMCPTool(toolName string) bool {
    return strings.HasPrefix(toolName, mcpPrefix)
}
```

### 6. ApprovalQueue — 异步批准队列

```go
// internal/permission/queue.go

package permission

import (
    "context"
    "sync"
)

const maxQueueDepth = 50

// ApprovalRequest 代表一个待审批的请求
type ApprovalRequest struct {
    ToolName string
    ToolArgs string
    Result   chan ApprovalResult
}

// ApprovalResult 审批结果
type ApprovalResult struct {
    Approved bool
    SaveRule *Rule // 非 nil 时表示用户选择"记住此决定"
}

// UserApprovalFunc 用户交互审批回调（由 TUI 提供）
type UserApprovalFunc func(ctx context.Context, req ApprovalRequest) (ApprovalResult, error)

// ApprovalQueue 管理异步审批请求的队列
type ApprovalQueue struct {
    mu       sync.Mutex
    pending  []ApprovalRequest
    userFunc UserApprovalFunc
    sem      chan struct{} // 限制并发审批数
}

// NewApprovalQueue 创建异步审批队列
func NewApprovalQueue(userFunc UserApprovalFunc) *ApprovalQueue {
    return &ApprovalQueue{
        userFunc: userFunc,
        sem:      make(chan struct{}, 1), // 同一时间只展示一个审批
    }
}

// Enqueue 将审批请求加入队列，阻塞等待结果
func (q *ApprovalQueue) Enqueue(ctx context.Context, toolName, toolArgs string) (bool, error) {
    q.mu.Lock()
    if len(q.pending) >= maxQueueDepth {
        q.mu.Unlock()
        return false, context.DeadlineExceeded
    }

    req := ApprovalRequest{
        ToolName: toolName,
        ToolArgs: toolArgs,
        Result:   make(chan ApprovalResult, 1),
    }
    q.pending = append(q.pending, req)
    q.mu.Unlock()

    // 排队获取展示权
    select {
    case q.sem <- struct{}{}:
        defer func() { <-q.sem }()
    case <-ctx.Done():
        return false, ctx.Err()
    }

    result, err := q.userFunc(ctx, req)
    if err != nil {
        return false, err
    }
    return result.Approved, nil
}

// PendingCount 返回待审批请求数
func (q *ApprovalQueue) PendingCount() int {
    q.mu.Lock()
    defer q.mu.Unlock()
    return len(q.pending)
}
```

### 7. PolicyEnforcer — 策略执行器

```go
// internal/permission/policy.go

package permission

import (
    "encoding/json"
    "os"
)

// Policy 组织级策略配置
type Policy struct {
    Version             int           `json:"version"`
    DenyRules           []PolicyRule  `json:"deny_rules"`
    RequireApproval     []string      `json:"require_approval"`
    MaxAutoApproveCount int           `json:"max_auto_approve_count"`
}

// PolicyRule 策略中的 deny 规则
type PolicyRule struct {
    Tool        string `json:"tool"`
    ArgsPattern string `json:"args_pattern,omitempty"`
    Reason      string `json:"reason"`
}

// PolicyEnforcer 执行组织级策略
type PolicyEnforcer struct {
    policy *Policy
    rules  []Rule // 预编译的规则
}

// LoadPolicyEnforcer 从文件加载策略
func LoadPolicyEnforcer(path string) (*PolicyEnforcer, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    var policy Policy
    if err := json.Unmarshal(data, &policy); err != nil {
        return nil, err
    }
    enforcer := &PolicyEnforcer{policy: &policy}
    enforcer.compile()
    return enforcer, nil
}

// compile 将策略规则预编译为 Rule 对象
func (e *PolicyEnforcer) compile() {
    for _, pr := range e.policy.DenyRules {
        e.rules = append(e.rules, Rule{
            Tool:        pr.Tool,
            Action:      ActionDeny,
            ArgsPattern: pr.ArgsPattern,
            Reason:      pr.Reason,
        })
    }
    for _, tool := range e.policy.RequireApproval {
        e.rules = append(e.rules, Rule{
            Tool:   tool,
            Action: ActionAsk,
        })
    }
}

// Check 检查工具调用是否被策略限制
// 返回 decision 和 matched（是否有策略命中）
func (e *PolicyEnforcer) Check(toolName, toolArgs string) (Decision, bool) {
    for _, r := range e.rules {
        if r.Match(toolName, toolArgs) {
            return Decision{
                Action: r.Action,
                Source: "policy",
                Reason: r.Reason,
            }, true
        }
    }
    return Decision{}, false
}
```

### 8. AuditLogger — 审计日志

```go
// internal/permission/audit.go

package permission

import (
    "encoding/json"
    "os"
    "time"
)

// AuditEntry 审计日志条目
type AuditEntry struct {
    Timestamp time.Time `json:"ts"`
    Tool      string    `json:"tool"`
    Args      string    `json:"args"`
    Decision  Action    `json:"decision"`
    RuleID    string    `json:"rule_id,omitempty"`
    Source    Scope     `json:"source,omitempty"`
}

// AuditLogger 将权限决策写入 JSONL 文件
type AuditLogger struct {
    path string
}

// NewAuditLogger 创建审计日志记录器
func NewAuditLogger(path string) *AuditLogger {
    return &AuditLogger{path: path}
}

// Log 记录一条权限决策
func (l *AuditLogger) Log(tool, args string, d Decision) {
    entry := AuditEntry{
        Timestamp: time.Now(),
        Tool:      tool,
        Args:      args,
        Decision:  d.Action,
        RuleID:    d.RuleID,
        Source:    d.Source,
    }
    f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
    if err != nil {
        return // 审计日志写入失败不应阻塞主流程
    }
    defer f.Close()
    json.NewEncoder(f).Encode(entry)
}
```

---

## 数据流

### 工具调用完整审批流程

```
Agent 发起工具调用
        │
        ▼
┌─────────────────────┐
│ approvalMiddleware   │  (internal/agent/middleware.go — 不变)
│ 调用 ApprovalFunc   │
└─────────┬───────────┘
          │
          ▼
┌─────────────────────┐
│ PermissionEngine    │
│ .RequestApproval()  │
└─────────┬───────────┘
          │
          ▼
┌─────────────────────────────────┐
│ PolicyEnforcer.Check()          │  ← 策略优先（P2）
│ 有 deny 命中? → 直接拒绝       │
└─────────┬───────────────────────┘
          │ 无命中
          ▼
┌─────────────────────────────────┐
│ RuleEvaluator.Evaluate()        │
│                                 │
│ 会话规则 → 项目规则 → 用户规则  │
│ 每层内: deny > allow > ask      │
│                                 │
│ 无匹配 → defaultDecision()     │
│ (保留现有白名单逻辑)            │
└─────────┬───────────────────────┘
          │
          ▼
    ┌─────────────┐
    │  Decision?  │
    └──┬──┬──┬────┘
       │  │  │
  allow│  │  │ask
       │  │  │
       ▼  │  ▼
   ✅通过 │ ┌──────────────────┐
       │  │ │ ApprovalQueue    │  ← 异步队列（P1）
  deny │  │ │ .Enqueue()       │
       │  │ └────────┬─────────┘
       ▼  │          ▼
   ❌拒绝 │  ┌──────────────────┐
          │  │ TUI 用户审批     │
          │  │ [y/n/a/A]        │
          │  └────────┬─────────┘
          │           │
          │     ┌─────┴──────┐
          │     │ 记住决定?  │
          │     └─────┬──────┘
          │           │ 是
          │           ▼
          │  ┌──────────────────┐
          │  │ SaveRule()       │
          │  │ → permissions.json│
          │  └─────────────────┘
          │
          ▼
┌─────────────────────┐
│ AuditLogger.Log()   │  ← 审计记录（P2）
└─────────────────────┘
```

### MCP 工具权限流程

```
MCP 工具调用
    │
    ▼
┌──────────────────────────────────┐
│ 工具名转换:                      │
│ "create_issue" (github server)   │
│    → "mcp__github__create_issue" │
└──────────┬───────────────────────┘
           │
           ▼
   PermissionEngine.RequestApproval()
           │
           ▼
   RuleEvaluator.Evaluate("mcp__github__create_issue", args)
           │
           ├── 匹配 mcp__github__create_issue → 精确规则
           ├── 匹配 mcp__github__*            → 服务器通配
           └── 无匹配                         → 默认 ask
```

---

## 与现有代码的集成方案

### 1. ApprovalState 改造

现有 `internal/runner/approval.go` 中的 `ApprovalState` 改造为委托模式：

```go
// internal/runner/approval.go — 改造后

type ApprovalState struct {
    p        *tea.Program
    mode     tui.ApprovalMode
    workpath string
    engine   *permission.PermissionEngine // 新增
}

func (s *ApprovalState) RequestApproval(ctx context.Context, toolName, toolArgs string) (bool, error) {
    // Auto 模式仍直接通过
    if s.mode == tui.ModeAuto {
        return true, nil
    }

    // 委托给 PermissionEngine
    if s.engine != nil {
        return s.engine.RequestApproval(ctx, toolName, toolArgs)
    }

    // 兜底: 保留原有逻辑
    return s.legacyApproval(ctx, toolName, toolArgs)
}
```

### 2. MCP 工具名注入

在 `internal/tools/mcp.go` 的 `LoadMCPTools` 中，为每个 MCP 工具包装权限名称：

```go
// MCP 工具加载后，包装工具名
for _, t := range serverTools {
    wrappedName := permission.MCPToolName(serverName, t.Info().Name)
    // 注册到权限引擎的工具名映射
}
```

### 3. middleware.go 无需修改

`approvalMiddleware` 仍通过 `ApprovalFunc` 回调调用，签名未变。`PermissionEngine.RequestApproval` 自然满足此签名。

### 4. TUI 审批增强

`internal/tui/tui.go` 中的审批交互增加"记住决定"选项：

```go
// 新增审批响应选项
type ToolApprovalResponse struct {
    Approved   bool
    Mode       ApprovalMode
    SaveScope  permission.Scope // 新增: 保存到哪个层级
    SaveRule   bool             // 新增: 是否保存规则
}
```

---

## 实现计划

### Phase 1: 基础权限引擎（P0 — 2 周）

| 任务 | 估时 | 依赖 | 产出 |
|------|------|------|------|
| 定义 Rule/Action/Scope 类型 | 2h | 无 | `permission/rule.go` |
| 实现 FileRuleStore (JSON 读写) | 4h | Rule 类型 | `permission/rule.go` |
| 实现 RuleEvaluator 规则匹配 + 优先级 | 6h | RuleStore | `permission/evaluator.go` |
| 实现 PermissionEngine 主入口 | 4h | RuleEvaluator | `permission/engine.go` |
| defaultDecision 兼容现有白名单 | 3h | PermissionEngine | `permission/evaluator.go` |
| 改造 ApprovalState 委托模式 | 3h | PermissionEngine | `runner/approval.go` |
| 单元测试（规则匹配、优先级、文件存储） | 6h | 全部 | `permission/permission_test.go` |
| CLI permissions 子命令 | 4h | FileRuleStore | `cmd/jcode/commands.go` |

### Phase 2: MCP 权限 + 审批增强（P0/P1 — 1.5 周）

| 任务 | 估时 | 依赖 | 产出 |
|------|------|------|------|
| MCP 工具命名解析 | 2h | 无 | `permission/mcp.go` |
| MCP 工具权限集成 | 4h | MCP命名, PermissionEngine | `tools/mcp.go` |
| TUI "记住决定"选项 | 4h | PermissionEngine | `tui/tui.go` |
| 路径白名单配置化 | 3h | FileRuleStore | `permission/evaluator.go` |
| ApprovalQueue 异步队列 | 6h | PermissionEngine | `permission/queue.go` |
| 集成测试 | 4h | 全部 | 测试文件 |

### Phase 3: 策略与审计（P2 — 1 周）

| 任务 | 估时 | 依赖 | 产出 |
|------|------|------|------|
| PolicyEnforcer 实现 | 4h | 无 | `permission/policy.go` |
| AuditLogger 实现 | 3h | 无 | `permission/audit.go` |
| 策略集成到 RuleEvaluator | 2h | PolicyEnforcer | `permission/evaluator.go` |
| 文档与集成测试 | 3h | 全部 | 文档 + 测试 |

### 里程碑

```
Week 1-2:  ✅ 持久化规则 + 三层系统 + CLI 管理
Week 3-4:  ✅ MCP 权限 + TUI 增强 + 异步队列
Week 4-5:  ✅ 策略限制 + 审计日志 + 全量测试
```

---

## 风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| 正则性能劣化 | 低 | 中 | 预编译 + 缓存，限制正则复杂度 |
| 规则冲突用户困惑 | 中 | 中 | 提供 `permissions explain <tool>` 诊断命令 |
| 文件并发写入 | 低 | 低 | 文件锁 (flock) + 原子写入 (write-rename) |
| MCP 工具名变更 | 低 | 中 | 通配符规则兜底 (`mcp__server__*`) |
| 向后兼容性破坏 | 低 | 高 | 无权限文件时 100% 退回现有行为 |

---

## 参考资料

- PRD: [docs/prd/permission_system.md](../../prd/permission_system.md)
- 分析文档: [docs/analyse/permission_system.md](../../analyse/permission_system.md)
- 现有实现: [internal/runner/approval.go](../../../internal/runner/approval.go)
- 中间件: [internal/agent/middleware.go](../../../internal/agent/middleware.go)
- 配置系统: [internal/config/config.go](../../../internal/config/config.go)
- MCP 工具加载: [internal/tools/mcp.go](../../../internal/tools/mcp.go)
