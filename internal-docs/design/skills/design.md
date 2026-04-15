# 技能系统增强 — 技术设计文档

## 1. 架构概述

### 1.1 当前架构

```
┌──────────────┐     Layer 1: descriptions        ┌────────────┐
│  Loader      │──────────────────────────────────▶│ System     │
│  (skills.go) │     注入系统提示                    │ Prompt     │
└──────┬───────┘                                   └────────────┘
       │
       │  Layer 2: load_skill tool
       ▼
┌──────────────┐     GetContent()                  ┌────────────┐
│  loadSkillTool│──────────────────────────────────▶│ tool_result│
│  (tool.go)   │     全文注入 agent 上下文            │ → agent    │
└──────────────┘                                   └────────────┘
```

### 1.2 目标架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Skill Loader                                │
│  ┌─────────┐  ┌─────────┐  ┌──────────┐  ┌──────────────────────┐  │
│  │ Builtin │  │  User   │  │ Project  │  │ MCP Skill Provider  │  │
│  │ (embed) │  │ (~/.jc) │  │ (.jc/)   │  │ (tools/list → Skill)│  │
│  └────┬────┘  └────┬────┘  └────┬─────┘  └──────────┬───────────┘  │
│       └────────────┴────────────┴────────────────────┘              │
│                              │                                      │
│                     ┌────────┴────────┐                             │
│                     │  SkillRegistry  │◄──── fsnotify watcher       │
│                     └────────┬────────┘                             │
└──────────────────────────────┼──────────────────────────────────────┘
                               │
                    ┌──────────┴──────────┐
                    │                     │
              ┌─────▼─────┐        ┌──────▼──────┐
              │  Inline   │        │    Fork     │
              │  Mode     │        │    Mode     │
              │           │        │             │
              │ GetContent│        │ SubAgent    │
              │ → tool_   │        │ + filtered  │
              │   result  │        │   tools     │
              └───────────┘        │ + model     │
                                   │   override  │
                                   └──────┬──────┘
                                          │
                                   ┌──────▼──────┐
                                   │  Telemetry  │
                                   │  Recorder   │
                                   └─────────────┘
```

### 1.3 设计原则

1. **向后兼容** — 现有 `Skill` 结构体扩展字段，零值即默认行为
2. **组合优于继承** — Fork 模式复用现有 `subagentTool` 模式，而非新建执行器
3. **声明式配置** — 行为由 frontmatter 驱动，代码路径统一
4. **失败隔离** — Fork agent 的 panic/timeout 通过 `context.WithTimeout` + recover 隔离

---

## 2. 核心组件

### 2.1 扩展 Skill 结构体

```go
// internal/skills/skills.go

// SkillContext 定义技能执行模式
type SkillContext string

const (
    SkillContextInline SkillContext = "inline" // 默认：内容注入 tool_result
    SkillContextFork   SkillContext = "fork"   // 子 agent 隔离执行
)

// SkillHooks 定义执行前后 hook
type SkillHooks struct {
    Pre  string // 执行前 shell 命令
    Post string // 执行后 shell 命令
}

// Skill represents a loaded skill with metadata and content.
type Skill struct {
    Name        string       // directory name or frontmatter name
    Description string       // short description for system prompt (Layer 1)
    Slash       string       // optional slash command trigger (e.g. "/review-pr")
    Body        string       // full markdown content (Layer 2, on-demand)
    Builtin     bool         // true if embedded in binary
    Path        string       // filesystem path (empty for built-in)

    // --- 新增字段 ---
    Model        string       // 模型覆盖标识，空值使用全局模型
    AllowedTools []string     // 工具白名单，空值允许全部
    Context      SkillContext // 执行模式：inline (默认) 或 fork
    Timeout      int          // Fork 模式超时秒数，默认 120
    Hooks        SkillHooks   // pre/post 执行 hook
    Tags         []string     // 分类标签
    Aliases      []string     // 触发别名

    // --- 运行时字段 ---
    Source       string       // "builtin" | "user" | "project" | "mcp"
}
```

### 2.2 扩展 Frontmatter 解析器

```go
// internal/skills/skills.go

// parseFrontmatter extracts key: value pairs from frontmatter text.
// 扩展支持列表值（逗号分隔）和嵌套键（hooks.pre）。
func parseFrontmatter(fm string, sk *Skill) {
    scanner := bufio.NewScanner(strings.NewReader(fm))
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" || line == "---" {
            continue
        }
        idx := strings.Index(line, ":")
        if idx < 0 {
            continue
        }
        key := strings.TrimSpace(line[:idx])
        val := strings.TrimSpace(line[idx+1:])
        switch key {
        case "name":
            sk.Name = val
        case "description":
            sk.Description = val
        case "slash":
            sk.Slash = val
        case "model":
            sk.Model = val
        case "context":
            if val == "fork" {
                sk.Context = SkillContextFork
            }
        case "timeout":
            if n, err := strconv.Atoi(val); err == nil && n > 0 {
                sk.Timeout = n
            }
        case "allowedTools":
            sk.AllowedTools = splitCSV(val)
        case "tags":
            sk.Tags = splitCSV(val)
        case "aliases":
            sk.Aliases = splitCSV(val)
        case "hooks.pre":
            sk.Hooks.Pre = val
        case "hooks.post":
            sk.Hooks.Post = val
        }
    }
}

// splitCSV 将逗号分隔的字符串拆分为 trimmed 字符串切片
func splitCSV(s string) []string {
    parts := strings.Split(s, ",")
    result := make([]string, 0, len(parts))
    for _, p := range parts {
        p = strings.TrimSpace(p)
        if p != "" {
            result = append(result, p)
        }
    }
    return result
}
```

### 2.3 Fork 执行器

```go
// internal/skills/executor.go — 新文件

// ForkExecutorDeps 是 Fork 执行所需的外部依赖
type ForkExecutorDeps struct {
    DefaultModel model.ToolCallingChatModel
    ModelFactory func(modelID string) (model.ToolCallingChatModel, error) // 按名称创建模型
    AllTools     []tool.BaseTool          // 父 agent 的完整工具列表
    Env          *tools.Env               // 当前执行环境
    Recorder     *session.Recorder
    Notifier     tools.SubagentNotifier
    ProgressFn   tools.SubagentProgressFn
}

// ForkExecutor 在独立子 agent 中执行技能
type ForkExecutor struct {
    deps *ForkExecutorDeps
}

func NewForkExecutor(deps *ForkExecutorDeps) *ForkExecutor {
    return &ForkExecutor{deps: deps}
}

// Execute 创建子 agent 并执行技能指令，返回最终结果文本
func (f *ForkExecutor) Execute(ctx context.Context, sk *Skill) (string, error) {
    // 1. 超时控制
    timeout := time.Duration(sk.Timeout) * time.Second
    if timeout == 0 {
        timeout = 120 * time.Second
    }
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    // 2. 模型选择
    chatModel := f.deps.DefaultModel
    if sk.Model != "" && f.deps.ModelFactory != nil {
        m, err := f.deps.ModelFactory(sk.Model)
        if err != nil {
            config.Logger().Printf("[skills] fork model override failed for %q: %v, using default", sk.Model, err)
        } else {
            chatModel = m
        }
    }

    // 3. 工具过滤
    childTools := f.filterTools(sk.AllowedTools)

    // 4. 创建子 agent
    childEnv := f.deps.Env.CloneForSubagent()
    prompt := fmt.Sprintf("You are executing the skill %q. Follow these instructions:\n\n%s", sk.Name, sk.Body)

    ag, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
        Name:        fmt.Sprintf("skill-%s", sk.Name),
        Description: sk.Description,
        Instruction: prompt,
        Model:       chatModel,
        ToolsConfig: adk.ToolsConfig{
            ToolsNodeConfig: compose.ToolsNodeConfig{
                Tools: childTools,
            },
        },
        MaxIterations: 50,
        ModelRetryConfig: &adk.ModelRetryConfig{
            MaxRetries: 2,
        },
    })
    if err != nil {
        return "", fmt.Errorf("failed to create fork agent for skill %q: %w", sk.Name, err)
    }

    // 5. 执行并收集结果
    return f.runAndCollect(ctx, ag, sk.Name)
}

// filterTools 根据白名单过滤工具列表
func (f *ForkExecutor) filterTools(allowedTools []string) []tool.BaseTool {
    if len(allowedTools) == 0 {
        return f.deps.AllTools
    }
    allowed := make(map[string]struct{}, len(allowedTools))
    for _, name := range allowedTools {
        allowed[name] = struct{}{}
    }
    // 防止递归 fork
    delete(allowed, "subagent")

    var filtered []tool.BaseTool
    for _, t := range f.deps.AllTools {
        info, err := t.(tool.InvokableTool).Info(context.Background())
        if err != nil {
            continue
        }
        if _, ok := allowed[info.Name]; ok {
            filtered = append(filtered, t)
        }
    }
    config.Logger().Printf("[skills] fork tool filter: %d/%d tools allowed", len(filtered), len(f.deps.AllTools))
    return filtered
}
```

### 2.4 增强 load_skill 工具

```go
// internal/skills/tool.go — 修改

type loadSkillInput struct {
    Name string `json:"name"`
}

type loadSkillTool struct {
    loader       *Loader
    forkExecutor *ForkExecutor    // 可选，为 nil 时 fork 降级为 inline
    telemetry    *SkillTelemetry  // 可选
    env          *tools.Env       // 用于 hook 执行
}

func (t *loadSkillTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
    var input loadSkillInput
    if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
        return "", fmt.Errorf("failed to parse input: %w", err)
    }

    sk := t.loader.Get(input.Name)
    if sk == nil {
        // 尝试按别名查找
        sk = t.loader.GetByAlias(input.Name)
    }
    if sk == nil {
        return "Error: Unknown skill '" + input.Name + "'. Available skills:\n" + t.loader.Descriptions(), nil
    }

    // 遥测：开始
    var telCtx *SkillTelemetryContext
    if t.telemetry != nil {
        telCtx = t.telemetry.Begin(sk.Name, string(sk.Context))
    }

    // Pre hook
    if sk.Hooks.Pre != "" && t.env != nil {
        out, err := t.env.Executor().Exec(ctx, sk.Hooks.Pre)
        if err != nil {
            return fmt.Sprintf("Error: pre-hook failed: %s\n%s", err, out), nil
        }
    }

    var result string
    var execErr error

    switch sk.Context {
    case SkillContextFork:
        if t.forkExecutor != nil {
            result, execErr = t.forkExecutor.Execute(ctx, sk)
        } else {
            // 降级为 inline
            config.Logger().Printf("[skills] fork executor not available, falling back to inline for %q", sk.Name)
            result = t.loader.GetContent(input.Name)
        }
    default:
        result = t.loader.GetContent(input.Name)
    }

    // Post hook
    if sk.Hooks.Post != "" && t.env != nil {
        out, err := t.env.Executor().Exec(ctx, sk.Hooks.Post)
        if err != nil {
            config.Logger().Printf("[skills] post-hook failed for %q: %v: %s", sk.Name, err, out)
        }
    }

    // 遥测：结束
    if telCtx != nil {
        telCtx.End(execErr)
    }

    if execErr != nil {
        return fmt.Sprintf("Error executing skill %q: %v", sk.Name, execErr), nil
    }
    return result, nil
}
```

### 2.5 MCP 技能提供者

```go
// internal/skills/mcp_provider.go — 新文件

// MCPSkillProvider 从 MCP 服务器发现并注册技能
type MCPSkillProvider struct {
    loader *Loader
}

func NewMCPSkillProvider(loader *Loader) *MCPSkillProvider {
    return &MCPSkillProvider{loader: loader}
}

// DiscoverFromServer 连接 MCP 服务器，将工具列表转化为技能注册
func (p *MCPSkillProvider) DiscoverFromServer(ctx context.Context, serverName string, srv *config.MCPServer) error {
    // 复用 tools.LoadMCPTools 的连接逻辑
    // 仅对配置 "skills": true 的服务器生效

    mcpTools, _ := tools.LoadMCPTools(ctx, map[string]*config.MCPServer{serverName: srv})
    for _, t := range mcpTools {
        invokable, ok := t.(tool.InvokableTool)
        if !ok {
            continue
        }
        info, err := invokable.Info(ctx)
        if err != nil {
            continue
        }

        sk := &Skill{
            Name:        fmt.Sprintf("mcp-%s-%s", serverName, info.Name),
            Description: info.Desc,
            Body:        formatMCPToolBody(info),
            Context:     SkillContextFork, // MCP 技能固定 fork
            Source:       "mcp",
        }
        p.loader.Register(sk)
        config.Logger().Printf("[skills] discovered MCP skill: %s from server %s", sk.Name, serverName)
    }
    return nil
}
```

### 2.6 fsnotify 文件监视器

```go
// internal/skills/watcher.go — 新文件

// Watcher 监听技能目录变更并触发自动重载
type Watcher struct {
    loader     *Loader
    projectDir string
    debounce   time.Duration
    stopCh     chan struct{}
    notifyFn   func() // Rescan 完成回调（通知 TUI）
}

func NewWatcher(loader *Loader, projectDir string, notifyFn func()) *Watcher {
    return &Watcher{
        loader:     loader,
        projectDir: projectDir,
        debounce:   500 * time.Millisecond,
        stopCh:     make(chan struct{}),
        notifyFn:   notifyFn,
    }
}

// Start 启动文件系统监听，阻塞直到 Stop 被调用
func (w *Watcher) Start() error {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return fmt.Errorf("fsnotify create failed: %w", err)
    }
    defer watcher.Close()

    // 添加监听目录
    userDir := filepath.Join(config.ConfigDir(), "skills")
    if err := w.addDirRecursive(watcher, userDir); err != nil {
        config.Logger().Printf("[skills] watcher: failed to watch user dir: %v", err)
    }
    if w.projectDir != "" {
        projDir := filepath.Join(w.projectDir, ".jcoding", "skills")
        if err := w.addDirRecursive(watcher, projDir); err != nil {
            config.Logger().Printf("[skills] watcher: failed to watch project dir: %v", err)
        }
    }

    var debounceTimer *time.Timer

    for {
        select {
        case <-w.stopCh:
            return nil
        case event, ok := <-watcher.Events:
            if !ok {
                return nil
            }
            config.Logger().Printf("[skills] watcher event: %s %s", event.Op, event.Name)

            // 防抖：500ms 内合并多次事件
            if debounceTimer != nil {
                debounceTimer.Stop()
            }
            debounceTimer = time.AfterFunc(w.debounce, func() {
                config.Logger().Printf("[skills] watcher: triggering rescan")
                w.loader.Rescan(w.projectDir)
                if w.notifyFn != nil {
                    w.notifyFn()
                }
            })
        case err, ok := <-watcher.Errors:
            if !ok {
                return nil
            }
            config.Logger().Printf("[skills] watcher error: %v", err)
        }
    }
}

// Stop 停止文件监听
func (w *Watcher) Stop() {
    close(w.stopCh)
}
```

### 2.7 遥测记录器

```go
// internal/skills/telemetry.go — 新文件

// SkillTelemetryEntry 是写入 JSONL 的单条遥测记录
type SkillTelemetryEntry struct {
    Timestamp   time.Time `json:"timestamp"`
    SkillName   string    `json:"skill_name"`
    ExecMode    string    `json:"exec_mode"`    // "inline" | "fork"
    DurationMs  int64     `json:"duration_ms"`
    Success     bool      `json:"success"`
    Error       string    `json:"error,omitempty"`
}

// SkillTelemetry 管理技能执行遥测
type SkillTelemetry struct {
    filePath string
    mu       sync.Mutex
    tracer   *telemetry.LangfuseTracer // 可选
}

func NewSkillTelemetry(tracer *telemetry.LangfuseTracer) *SkillTelemetry {
    return &SkillTelemetry{
        filePath: filepath.Join(config.ConfigDir(), "skill_telemetry.jsonl"),
        tracer:   tracer,
    }
}

// SkillTelemetryContext 追踪单次技能执行
type SkillTelemetryContext struct {
    tel       *SkillTelemetry
    skillName string
    execMode  string
    startTime time.Time
}

func (t *SkillTelemetry) Begin(skillName, execMode string) *SkillTelemetryContext {
    return &SkillTelemetryContext{
        tel:       t,
        skillName: skillName,
        execMode:  execMode,
        startTime: time.Now(),
    }
}

func (c *SkillTelemetryContext) End(err error) {
    entry := SkillTelemetryEntry{
        Timestamp:  time.Now(),
        SkillName:  c.skillName,
        ExecMode:   c.execMode,
        DurationMs: time.Since(c.startTime).Milliseconds(),
        Success:    err == nil,
    }
    if err != nil {
        entry.Error = err.Error()
    }

    c.tel.mu.Lock()
    defer c.tel.mu.Unlock()

    f, ferr := os.OpenFile(c.tel.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if ferr != nil {
        config.Logger().Printf("[skills] telemetry write failed: %v", ferr)
        return
    }
    defer f.Close()
    json.NewEncoder(f).Encode(entry)
}
```

---

## 3. 数据流

### 3.1 Inline 模式（现有，不变）

```
用户消息 → Agent → tool_call(load_skill, name="review-pr")
                       │
                       ▼
                  Loader.GetContent()
                       │
                       ▼
                  tool_result: <skill>...</skill>
                       │
                       ▼
                  Agent 继续处理（技能内容在上下文中）
```

### 3.2 Fork 模式（新增）

```
用户消息 → Agent → tool_call(load_skill, name="security-review")
                       │
                       ▼
                  检查 sk.Context == "fork"
                       │
                       ▼
              ┌── pre hook (git stash) ──┐
              │                          │ 失败则中断
              ▼                          │
        ForkExecutor.Execute()           │
              │                          │
              ├── 选择模型 (sk.Model)     │
              ├── 过滤工具 (sk.AllowedTools)
              ├── CloneForSubagent()     │
              ├── 创建子 ChatModelAgent   │
              ├── 流式执行 + 收集结果     │
              │                          │
              ▼                          │
        最终结果文本                      │
              │                          │
              ├── post hook (git stash pop)
              ├── 遥测记录               │
              ▼                          │
        tool_result: 技能执行结果         │
              │                          │
              ▼                          │
        Agent 继续处理                    │
```

### 3.3 MCP 技能发现流程

```
启动时 → 遍历 MCPServers 配置
              │
              ├── 过滤 "skills": true 的服务器
              │
              ▼
        MCPSkillProvider.DiscoverFromServer()
              │
              ├── client.Initialize()
              ├── tools/list → []ToolInfo
              │
              ▼
        每个 ToolInfo → Skill{Context: fork, Source: "mcp"}
              │
              ▼
        Loader.Register(sk)
```

### 3.4 文件监视流程

```
Watcher.Start()
    │
    ├── fsnotify.NewWatcher()
    ├── watcher.Add(~/.jcoding/skills/)
    ├── watcher.Add(.jcoding/skills/)
    │
    ▼
事件循环:
    event (Create/Write/Remove)
        │
        ├── 防抖 timer (500ms)
        │
        ▼ (timer 触发)
    Loader.Rescan(projectDir)
        │
        ▼
    notifyFn() → TUI 刷新技能列表
```

---

## 4. 状态管理

### 4.1 Loader 状态机

```
                  NewLoader()
                      │
                      ▼
              ┌───────────────┐
              │  initialized  │ ← 内置 + 用户技能已加载
              └───────┬───────┘
                      │ ScanProjectSkills()
                      ▼
              ┌───────────────┐
              │    ready      │ ← 全部技能源已扫描
              └───────┬───────┘
                      │ fsnotify event / Rescan()
                      ▼
              ┌───────────────┐
              │  reloading    │ ← 清除非内置 → 重新扫描
              └───────┬───────┘
                      │
                      ▼
              ┌───────────────┐
              │    ready      │
              └───────────────┘
```

### 4.2 Skill 生命周期

| 状态 | 触发 | 行为 |
|------|------|------|
| 已注册 | 扫描/发现 | 描述注入系统提示 |
| 已加载（inline） | `load_skill` 调用 | Body 注入 tool_result |
| 执行中（fork） | `load_skill` 调用 | 子 agent 运行中 |
| 已完成 | Fork agent 结束 | 结果返回 + 遥测记录 |
| 不可用 | MCP 连接失败 | 标记 unavailable，load 返回错误 |

### 4.3 并发安全

- `Loader.skills` map 受 `sync.RWMutex` 保护（已有）
- `Watcher` 的 Rescan 通过 `Loader.mu` 保证与 Get/All 的安全交替
- `SkillTelemetry` 的 JSONL 写入受 `sync.Mutex` 保护
- Fork 执行的子 agent 在独立 goroutine 中运行，通过 `context` 传递取消信号

---

## 5. 实现计划

### 阶段一：P0 核心能力（约 5 个任务）

| 任务 | 文件 | 依赖 | 说明 |
|------|------|------|------|
| T-01: 扩展 Skill 结构体 | `internal/skills/skills.go` | 无 | 新增字段，零值兼容 |
| T-02: 增强 frontmatter 解析 | `internal/skills/skills.go` | T-01 | 新增 switch case + splitCSV |
| T-03: 实现 ForkExecutor | `internal/skills/executor.go`（新） | T-01 | 子 agent 创建 + 工具过滤 + 超时 |
| T-04: 改造 loadSkillTool | `internal/skills/tool.go` | T-02, T-03 | 分支 inline/fork + hook |
| T-05: 单元测试 | `internal/skills/*_test.go` | T-01~T-04 | frontmatter 解析 + fork mock |

### 阶段二：P1 扩展能力（约 4 个任务）

| 任务 | 文件 | 依赖 | 说明 |
|------|------|------|------|
| T-06: MCP 技能发现 | `internal/skills/mcp_provider.go`（新） | T-01 | 复用 LoadMCPTools |
| T-07: fsnotify 监视器 | `internal/skills/watcher.go`（新） | 无 | 防抖 + Rescan + TUI 通知 |
| T-08: 遥测记录器 | `internal/skills/telemetry.go`（新） | 无 | JSONL + Langfuse span |
| T-09: 集成接线 | `cmd/coding/`, `internal/runner/` | T-06~T-08 | watcher 启停 + 遥测注入 |

### 阶段三：P2 增强能力（约 3 个任务）

| 任务 | 文件 | 依赖 | 说明 |
|------|------|------|------|
| T-10: Hook 执行 | `internal/skills/tool.go` | T-04 | Env.Executor.Exec shell |
| T-11: 模板变量替换 | `internal/skills/skills.go` | T-01 | GetContent 内替换 |
| T-12: 安全加固 | `internal/skills/skills.go` | T-01 | 符号链接检查 + 文件大小限制 |

### 关键依赖

```
T-01 ──▶ T-02 ──▶ T-04
              │         ▲
              │         │
T-03 ─────────┘─────────┘
T-06 ◀── T-01
T-07 (独立)
T-08 (独立)
T-09 ◀── T-06, T-07, T-08
T-10 ◀── T-04
T-11 ◀── T-01
T-12 ◀── T-01
```

### 新增依赖

| 依赖 | 用途 | 版本 |
|------|------|------|
| `github.com/fsnotify/fsnotify` | 文件系统监听 | v1.7+ |

> 注：Eino (`github.com/cloudwego/eino`)、MCP (`github.com/mark3labs/mcp-go`) 均为现有依赖，无需新增。

### 测试策略

| 组件 | 测试类型 | 覆盖要求 |
|------|---------|---------|
| frontmatter 解析 | 单元测试 | 100% 字段 + 向后兼容 |
| 工具过滤 | 单元测试 | 白名单 + 空白名单 + subagent 排除 |
| ForkExecutor | 集成测试（mock model） | 正常完成 + 超时 + 模型覆盖 |
| Watcher | 集成测试 | Create/Write/Remove 事件 + 防抖 |
| 遥测 | 单元测试 | JSONL 格式 + 并发写入 |
| MCP 发现 | 集成测试（mock server） | 工具→技能映射 |
