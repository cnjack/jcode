# jcode vs Claude Code 深度对比分析与改进方案

> 生成日期: 2026-04-09
> 对比版本: jcode (Go/Eino) vs Claude Code (TypeScript/Ink)

---

## 目录

1. [总体架构对比](#1-总体架构对比)
2. [Agent Loop](#2-agent-loop)
3. [Tool 系统](#3-tool-系统)
4. [SubAgent 系统](#4-subagent-系统)
5. [Skills 技能系统](#5-skills-技能系统)
6. [Reminder 提醒系统](#6-reminder-提醒系统)
7. [TUI 终端界面](#7-tui-终端界面)
8. [Approval 审批系统](#8-approval-审批系统)
9. [Integration 集成能力](#9-integration-集成能力)
10. [LSP 代码智能](#10-lsp-代码智能)
11. [Permission & Sandbox 权限与沙箱](#11-permission--sandbox-权限与沙箱)
12. [Agent Teams 设计方案](#12-agent-teams-设计方案)
13. [改进优先级总结](#13-改进优先级总结)

---

## 1. 总体架构对比

| 维度 | jcode (Go) | Claude Code (TypeScript) |
|------|-----------|------------------------|
| **语言** | Go + Eino框架 | TypeScript + 自建框架 |
| **UI框架** | BubbleTea (Elm架构) | Ink (React for Terminal) |
| **Agent引擎** | Eino ChatModelAgent | 自建 QueryEngine |
| **工具数** | ~15 内置 | 50+ 内置 |
| **插件系统** | MCP | MCP + 原生插件 |
| **远程执行** | SSH | SSH + Bridge + CCR |
| **沙箱** | 无 | @anthropic-ai/sandbox-runtime |
| **LSP** | 无 | 完整 LSP 客户端 |
| **代码量** | ~5K行核心 | ~50K+行核心 |

---

## 2. Agent Loop

### 2.1 当前 jcode 方案

```
runner.Run() → ag.Run(ctx, input) → Iterator
  for event := iterator.Next() {
    处理 Tool 消息 / 流式 Assistant 消息
  }
  → Todo 补完守卫 (最多3次重试)
  → 发送 TokenUpdateMsg
```

**特点:**
- 基于 Eino 框架的迭代器事件流
- 固定上限 `maxIterations = 1000`
- 简单的 Todo 不完成守卫
- 无上下文压缩/摘要机制

### 2.2 Claude Code 方案

```
QueryEngine.submitMessage(prompt)
  → queryLoop() {
    while(true) {
      1. 多层 Context 压缩 (Snip → Microcompact → Autocompact)
      2. 调用 API 流式处理
      3. 执行工具调用
      4. 处理附件 (Memory, Skills, Queued Commands)
      5. 检查 maxTurns / StopHooks / Token Budget
      6. 多层错误恢复
    }
  }
```

**Claude Code 核心优势:**
- **5层上下文压缩**: Snip → Microcompact → Autocompact → Reactive Compact → Context Collapse
- **4层错误恢复**: Collapse Drain → Reactive Compact → OTK 升级(8k→64k) → Multi-turn OTK
- **StopHooks 系统**: 可注入外部业务逻辑决定是否继续
- **Token Budget 管理**: 主动预算跟踪与续期

### 2.3 改进建议

| 优先级 | 改进项 | 说明 | 预估工作量 |
|--------|--------|------|-----------|
| **P0** | 上下文压缩 | 实现消息历史摘要/截断，防止 context overflow | 2-3天 |
| **P0** | Token Budget | 跟踪每轮 token 使用，接近上限时提醒/压缩 | 1天 |
| **P1** | 错误恢复循环 | 区分 413/max_output_tokens 等错误类型，分别处理 | 2天 |
| **P1** | StopHooks | 在每轮结束后可注入外部检查逻辑 | 1天 |
| **P2** | 自适应迭代控制 | maxIterations 可配置，按任务类型调整 | 0.5天 |

---

## 3. Tool 系统

### 3.1 当前 jcode 方案

- 15个内置工具，实现 `tool.InvokableTool` 接口
- 硬编码注册于 `cmd/coding/main.go`
- `schema.ParamsOneOf` 定义参数（无运行时验证）
- MCP 动态加载外部工具
- 简短执行管道：JSON解析 → 预检 → Executor执行 → 格式化返回

### 3.2 Claude Code 方案

- 50+ 内置工具，通过 `buildTool()` 声明式定义
- **Zod schema** 运行时类型验证（包括枚举、默认值、嵌套对象）
- `getAllBaseTools()` 集中发现 + 条件加载（feature flags）
- `searchHint`, `maxResultSizeChars`, `getToolUseSummary` 等丰富元数据
- 工具分类常量 (`ALL_AGENT_DISALLOWED_TOOLS`, `ASYNC_AGENT_ALLOWED_TOOLS`)
- 懒加载 Schema 防止启动延迟

### 3.3 改进建议

| 优先级 | 改进项 | 说明 |
|--------|--------|------|
| **P0** | 参数验证层 | 添加枚举、默认值、范围约束的参数 Schema 验证 |
| **P1** | 工具注册表 | Registry Pattern 替代硬编码列表，支持条件加载 |
| **P1** | 工具元数据 | 添加 `category`, `searchHint`, `maxResultSize` 元数据 |
| **P1** | 结果大小限制 | 工具结果超过阈值时自动截断 |
| **P2** | 工具分类常量 | 为不同 agent 类型定义工具白名单/黑名单 |
| **P2** | 工具摘要生成 | `getToolUseSummary()` 用于上下文压缩时替代完整结果 |

---

## 4. SubAgent 系统

### 4.1 当前 jcode 方案

- 2种类型: `explore`(只读) / `general`(可写)
- **同步阻塞式**: 父 agent 等待子 agent 完成
- 最大 50 步迭代限制
- 共享 Executor 隔离 TodoStore
- 无嵌套子 agent
- 仅返回最终文本结果

### 4.2 Claude Code 方案

- **多种Agent模式**: AgentTool (本地) / RemoteAgent (CCR) / Coordinator (编排) / Buddy (队伍)
- **异步非阻塞**: `run_in_background=true` 立即返回 agentId
- 支持并行多 Agent 执行
- **双向通信**: 通过 `SendMessageTool` 向子 agent 发送后续消息
- **深度隔离**: worktree / 远程会话 / 权限模式
- **结构化通知**: XML 格式 `<task-notification>` 含元数据
- **4层工具过滤**: 全局禁用 / 自定义禁用 / 异步限制 / 队伍限制

### 4.3 改进建议

| 优先级 | 改进项 | 说明 |
|--------|--------|------|
| **P0** | 异步子 Agent | 支持 `background=true` 模式，立即返回 taskId |
| **P0** | 并行执行 | 多个子 Agent 并发运行，总耗时 ≈ max(T1,T2,...) |
| **P1** | 结构化结果 | 返回 JSON 含 status/result/tokenUsage/duration |
| **P1** | 工具过滤 | 按 Agent 类型配置可用工具集 |
| **P2** | Agent 间消息 | 支持向运行中的子 Agent 发送后续指令 |
| **P2** | 嵌套支持 | 允许子 Agent 创建其下级子 Agent（有深度限制） |

---

## 5. Skills 技能系统

### 5.1 当前 jcode 方案

- 三层支持: 内置 + 用户(`~/.jcoding/skills/`) + 项目(`.jcoding/skills/`)
- 两层加载: Frontmatter (Layer1 发现描述) + 按需加载 (Layer2 完整内容)
- 4个元数据字段 (name, description, author, version)
- 3个内置技能: PR评论、代码审查、安全审查
- 同步加载，XML标签包装

### 5.2 Claude Code 方案

- 五层来源: 策略 + 用户 + 项目 + 额外目录 + MCP
- **15+ 元数据字段**: 模型选择、工具白名单、执行隔离、路径条件等
- 支持 `inline`/`fork` 执行隔离
- Per-skill 模型选择
- 参数模板 `{arg}` 替换
- 路径激活 (glob patterns)
- 功能标记条件加载
- 并行加载 + 去重 + 缓存

### 5.3 改进建议

| 优先级 | 改进项 | 说明 |
|--------|--------|------|
| **P1** | 富 Frontmatter | 新增 tools, model, isolation, paths 等元数据字段 |
| **P1** | 工具白名单 | 技能可声明只需要哪些工具 |
| **P1** | 参数模板 | 支持 `{arg}` 占位符替换 |
| **P2** | 执行上下文 | 声明 inline/fork 隔离模式 |
| **P2** | 路径激活 | 基于文件 glob 模式自动激活相关技能 |
| **P2** | Per-skill 模型 | 技能可指定使用的模型 |

---

## 6. Reminder 提醒系统

### 6.1 当前 jcode 方案

- `reminderMiddleware` 在每次模型调用前注入提醒
- 5个内置提醒规则: plan_execution, todo_check, token_warning, token_critical, tool_error_streak
- `ReminderContext` 含 14 个上下文变量
- `AGENTS.md` 自动发现并追加到系统提示末尾
- Go template 变量注入

### 6.2 Claude Code 方案

- **4层指令体系**: 托管(/etc) → 用户(~/.claude) → 项目(CLAUDE.md) → 本地(.local.md)
- `@include` 引用外部文件指令
- YAML Frontmatter + HTML 注释处理
- **缓存感知的系统提示分割** (`splitSysPromptPrefix`)
- 多源上下文融合 (systemContext + userContext)
- 附件系统自动交付 (Memory, Skills, Diagnostics)
- StopHooks / PostSamplingHooks / StopFailureHooks

### 6.3 改进建议

| 优先级 | 改进项 | 说明 |
|--------|--------|------|
| **P1** | 多层指令文件 | 支持 `~/.jcoding/AGENTS.md` (全局) + 项目级 + 本地级 |
| **P1** | @include 指令 | 支持在 AGENTS.md 中引用外部文件 |
| **P1** | 提示缓存优化 | 静态部分与动态部分分离，利用 prompt caching |
| **P2** | 附件系统 | 自动交付 Memory/Skills/Diagnostics 作为附件 |
| **P2** | 自定义提醒规则 | 允许用户在配置中添加自定义提醒条件 |

---

## 7. TUI 终端界面

### 7.1 当前 jcode 方案

- BubbleTea (Elm 架构)，单体 Model + Update/View
- ~40+ 状态字段平铺在 Model 中
- lipgloss 样式 + glamour Markdown 渲染
- 基础 Picker 组件(模型/会话/SSH/目录)
- 格式化: execute(末尾5行), edit(diff), subagent(标记)
- 无 Vim 模式、无语音、无快捷键系统

### 7.2 Claude Code 方案

- Ink (React for Terminal)，组件树 + Context Provider + Hooks
- **设计系统**: ThemedBox, ThemedText, Divider 等基础组件
- **虚拟消息列表**: VirtualMessageList 高性能渲染
- **流式 Markdown**: StreamingMarkdown 实时渲染
- **Vim 模式**: 完整状态机 (Normal/Insert/Visual/Operator)
- **语音输入**: Push-to-talk + 音量波形可视化
- **快捷键系统**: 可配置的 keybindings.json
- **FPS 监控**: 渲染性能追踪

### 7.3 改进建议

| 优先级 | 改进项 | 说明 |
|--------|--------|------|
| **P0** | Model 拆分 | 将 40+ 字段分组为 AgentState/InputState/ViewportState 等 |
| **P1** | 组件库 | 提取 messages/input/statusbar/pickers 为独立子组件 |
| **P1** | 快捷键系统 | 从硬编码迁移到可配置的 KeyBinding 结构 |
| **P1** | 流式 Markdown | 改进 glamour 渲染为流式渐进输出 |
| **P2** | Vim 模式 | 参考 Claude Code 的 Vim 状态机实现 |
| **P3** | 语音输入 | 预留语音输入接口 |

---

## 8. Approval 审批系统

### 8.1 当前 jcode 方案

- **二进制模式**: ModeManual / ModeAuto
- 硬编码安全工具列表 (glob, grep, todowrite, todoread, question, webfetch, subagent, check_background)
- Read 工具: 工作目录内自动批准
- Execute: 14个安全前缀命令 (ls, pwd, git status...)
- TUI 弹窗: Yes/No/Always Allow
- **会话级别**: 重启后重置为 Manual

### 8.2 Claude Code 方案

- **三层权限行为**: allow / deny / ask
- **持久化规则系统**: `Tool(content)` 模式匹配 (如 `Bash(npm:*)`)
- **4个存储目的地**: localSettings / remoteSettings / managedSettings / classifier
- **Bash 分类器**: ML/规则混合的命令安全性判断
- **20+ 危险模式检测**: 命令替换、管道注入、IFS注入等
- **工具特定 UI**: BashPermissionRequest, FileEditPermissionRequest 等
- **组织级策略**: policyLimits API 推送企业规则

### 8.3 改进建议

| 优先级 | 改进项 | 说明 |
|--------|--------|------|
| **P0** | 持久化规则 | 允许 "永久允许此命令" 并保存到配置文件 |
| **P0** | 规则模式匹配 | 支持 `Bash(npm:*)`, `Read(/home/*)` 等通配符规则 |
| **P1** | 危险命令检测 | 检测命令替换 `$()`, 管道注入, `sudo` 等危险模式 |
| **P1** | 3层权限行为 | allow/deny/ask 替代简单的 Manual/Auto |
| **P2** | 危险路径检测 | 阻止 `rm -rf /`, 保护 `.git`, `.ssh` 等敏感目录 |
| **P2** | 工具特定审批UI | 不同工具显示不同审批信息和上下文 |

---

## 9. Integration 集成能力

### 9.1 当前 jcode 方案

- MCP 客户端: 3种传输 (stdio, SSE, HTTP)
- 使用 `mark3labs/mcp-go v0.45.0`
- 基础 Headers 认证
- SSH 远程执行 (单连接)
- 无 OAuth, 无代理支持, 无连接恢复
- 无资源/提示列举

### 9.2 Claude Code 方案

- MCP: **7种传输** (stdio, SSE, HTTP, WebSocket, SDK, IDE, Claude.AI Proxy)
- 使用官方 `@modelcontextprotocol/sdk`
- **完整 OAuth 2.0** + PKCE + XAA(企业交叉应用访问)
- **多范围配置**: local / user / project / enterprise / claudeai / managed
- **远程执行**: Bridge + Direct Connect + CCR (Cloud Code Remote)
- **连接恢复**: 指数退避 + 智能重连
- **代理支持**: HTTP/HTTPS/SOCKS proxy
- **资源列举**: MCP resources + prompts 完整支持

### 9.3 改进建议

| 优先级 | 改进项 | 说明 |
|--------|--------|------|
| **P0** | 连接恢复 | MCP 连接失败后指数退避重试 |
| **P1** | OAuth 支持 | 实现 OAuth 2.0 PKCE 流程用于 MCP 认证 |
| **P1** | 资源列举 | 支持 MCP resources 和 prompts 端点 |
| **P1** | WebSocket 传输 | 添加 WebSocket MCP 传输支持 |
| **P2** | 代理支持 | HTTP/SOCKS 代理用于企业网络 |
| **P2** | 多范围配置 | 支持全局/用户/项目级别的 MCP 配置 |

---

## 10. LSP 代码智能

### 10.1 当前 jcode 方案

- **完全缺乏 LSP 集成**
- 仅有 `grep` 工具进行正则文本搜索
- 无 AST 解析、无符号理解、无类型信息
- 无法区分代码/注释/字符串中的匹配

### 10.2 Claude Code 方案

**完整 LSP 服务架构:**
- `LSPClient`: vscode-jsonrpc stdio 通信
- `LSPServerManager`: 多实例路由(按文件扩展名)
- `LSPServerInstance`: 状态机管理 + 崩溃恢复(最多3次)
- `LSPDiagnosticRegistry`: 异步诊断通知处理

**9项代码智能操作:**
| 操作 | 功能 |
|-----|------|
| goToDefinition | 查找符号定义位置 |
| findReferences | 查找所有引用 |
| hover | 获取类型/文档信息 |
| documentSymbol | 列出文件中所有符号 |
| workspaceSymbol | 工作区符号搜索 |
| goToImplementation | 查找接口实现 |
| prepareCallHierarchy | 调用层次入口 |
| incomingCalls | 谁调用了此函数 |
| outgoingCalls | 此函数调用了谁 |

**附加特性:**
- 文件索引系统 (nucleo/fzf-v2, <5ms 查询 270k+ 文件)
- 诊断自动作为附件注入对话
- gitignore 文件自动过滤

### 10.3 改进建议

| 优先级 | 改进项 | 说明 |
|--------|--------|------|
| **P1** | LSP 客户端 | 实现基于 stdio 的 JSON-RPC LSP 客户端 |
| **P1** | LSP 工具 | 新增 `lsp` 工具支持 definition/references/hover |
| **P1** | 诊断集成 | 收集 LSP 诊断并注入 agent 上下文 |
| **P2** | 多语言支持 | 按文件扩展名路由到不同 LSP 服务器 |
| **P2** | 文件索引 | 实现高性能模糊文件搜索 |
| **P2** | 崩溃恢复 | LSP 进程崩溃后自动重启(最多3次) |

---

## 11. Permission & Sandbox 权限与沙箱

### 11.1 当前 jcode 方案

- **无原生沙箱**: 直接 `exec.CommandContext("bash", "-c", command)`
- 仅工作目录路径检查 (read 工具)
- Write/Edit 工具**无路径限制**
- 无网络隔离、无进程隔离、无资源限制
- SSH `InsecureIgnoreHostKey()` 跳过证书验证
- 仅超时控制 (2分钟默认, 最高10分钟)

### 11.2 Claude Code 方案

- **企业级沙箱**: `@anthropic-ai/sandbox-runtime`
  - 文件系统隔离 (FsRead/FsWrite RestrictionConfig)
  - 网络隔离 (allowedHosts/deniedHosts, 私有网络控制)
  - 进程资源限制 (Namespace 隔离)
- **危险路径保护**: `.git`, `.claude`, `.bashrc`, `.ssh` 等
- **符号链接逃逸检测**: `path.realpath()` + 大小写规范化
- **危险删除检测**: `/`, `/boot`, `/etc`, `/sys` 等阻止
- **沙箱违规追踪**: SandboxViolationStore 记录所有违规事件

### 11.3 改进建议

| 优先级 | 改进项 | 说明 |
|--------|--------|------|
| **P0** | Write/Edit 路径检查 | 对所有写操作进行工作目录路径验证 |
| **P0** | 危险路径保护 | 阻止修改 `.git/`, `.ssh/`, `.bashrc` 等 |
| **P0** | SSH 证书验证 | 替换 InsecureIgnoreHostKey 为 known_hosts 验证 |
| **P1** | 危险命令检测 | 检测 `rm -rf /`, `chmod 777`, `sudo` 等危险操作 |
| **P1** | 符号链接检测 | 解析符号链接防止路径逃逸 |
| **P2** | 资源限制 | CPU/内存/文件描述符配额 |
| **P2** | 网络访问控制 | 可配置的域名白名单/黑名单 |
| **P3** | 沙箱集成 | 考虑 Linux namespace/cgroup 轻量沙箱 |

---

## 12. Agent Teams 设计方案

基于 Claude Code 的 Coordinator/Buddy 模式和 jcode 当前的 SubAgent 基础，提出以下 Agent Teams 设计:

### 12.1 Claude Code 的多 Agent 协调架构

Claude Code 支持以下编排模式:

```
Coordinator Mode (编排器模式):
┌──────────────────────────┐
│    Coordinator Agent     │ ← 主 Agent 负责任务分解
│    (可用所有工具)          │
└──────────┬───────────────┘
           │ AgentTool (并行)
   ┌───────┼──────────┐
   ↓       ↓          ↓
┌──────┐ ┌──────┐ ┌──────┐
│ W-1  │ │ W-2  │ │ W-3  │ ← Worker Agents (后台执行)
│(前端)│ │(后端)│ │(测试)│
└──────┘ └──────┘ └──────┘
   │       │          │
   └───────┼──────────┘
           ↓
   <task-notification> ← XML 结构化结果通知
```

**关键设计要素:**
1. **Coordinator** 角色: 只做规划和分配，不直接执行修改
2. **Worker** 异步执行: `run_in_background=true` 立即返回
3. **消息队列**: `enqueuePendingNotification()` 异步推送结果
4. **双向通信**: `SendMessageTool` 向运行中的 Agent 发送后续消息
5. **worktree 隔离**: 每个 Worker 在独立 git worktree 中工作

### 12.2 jcode Agent Teams 设计方案

#### 12.2.1 核心概念

```
┌──────────────────────────────────────────┐
│              TeamCoordinator              │
│  (主 Agent, 负责任务分解与结果汇总)        │
└─────────────────┬────────────────────────┘
                  │
    ┌─────────────┼─────────────────┐
    │ TaskQueue   │  NotifyChannel  │
    │ (任务队列)   │  (结果通知)      │
    ├─────────────┼─────────────────┤
    ↓             ↓                 ↓
┌────────┐  ┌────────┐       ┌────────┐
│TeamWorker│ │TeamWorker│     │TeamWorker│
│ (goroutine) │ (goroutine)│  │ (goroutine)│
│ ID: w-1 │  │ ID: w-2 │     │ ID: w-3 │
│ Role:前端│  │ Role:后端│     │ Role:测试│
│ Tools:[] │  │ Tools:[] │     │ Tools:[] │
└────────┘  └────────┘       └────────┘
```

#### 12.2.2 数据结构设计

```go
// internal/teams/types.go

// TeamConfig 团队配置
type TeamConfig struct {
    MaxWorkers      int           `json:"max_workers"`       // 最大 Worker 数
    MaxIterPerWorker int          `json:"max_iter_per_worker"` // 每 Worker 最大迭代
    IsolationMode   string        `json:"isolation_mode"`     // "shared" | "worktree" | "copy"
    MergeStrategy   string        `json:"merge_strategy"`     // "sequential" | "parallel_merge"
}

// Team 代表一个 Agent 团队
type Team struct {
    ID          string
    Config      TeamConfig
    Coordinator *TeamCoordinator
    Workers     map[string]*TeamWorker
    TaskQueue   chan *Task
    NotifyCh    chan *TaskNotification
    mu          sync.RWMutex
}

// Task 任务定义
type Task struct {
    ID          string            `json:"id"`
    Description string            `json:"description"`
    Prompt      string            `json:"prompt"`
    WorkerRole  string            `json:"worker_role"`   // "frontend" | "backend" | "test" | "research"
    Tools       []string          `json:"tools"`          // 可用工具白名单
    DependsOn   []string          `json:"depends_on"`     // 依赖的其他 Task ID
    Priority    int               `json:"priority"`
    Status      TaskStatus        `json:"status"`
    Result      *TaskResult       `json:"result,omitempty"`
}

type TaskStatus string
const (
    TaskPending   TaskStatus = "pending"
    TaskRunning   TaskStatus = "running"
    TaskCompleted TaskStatus = "completed"
    TaskFailed    TaskStatus = "failed"
    TaskCancelled TaskStatus = "cancelled"
)

// TaskResult 任务结果
type TaskResult struct {
    Output      string        `json:"output"`
    TokensUsed  int64         `json:"tokens_used"`
    Duration    time.Duration `json:"duration"`
    FilesChanged []string     `json:"files_changed"`
    Error       string        `json:"error,omitempty"`
}

// TaskNotification 任务通知
type TaskNotification struct {
    TaskID      string      `json:"task_id"`
    WorkerID    string      `json:"worker_id"`
    Status      TaskStatus  `json:"status"`
    Result      *TaskResult `json:"result"`
    Timestamp   time.Time   `json:"timestamp"`
}

// TeamWorker Worker Agent
type TeamWorker struct {
    ID          string
    Role        string
    Agent       *adk.ChatModelAgent
    Env         *tools.Env          // 隔离的执行环境
    Tools       []tool.InvokableTool
    CurrentTask *Task
    Status      WorkerStatus
    cancel      context.CancelFunc
}

type WorkerStatus string
const (
    WorkerIdle    WorkerStatus = "idle"
    WorkerBusy    WorkerStatus = "busy"
    WorkerStopped WorkerStatus = "stopped"
)
```

#### 12.2.3 Coordinator 工具设计

```go
// internal/teams/tools.go

// 1. create_team - 创建 Agent 团队
type CreateTeamInput struct {
    Workers []WorkerSpec `json:"workers"`
}
type WorkerSpec struct {
    Role  string   `json:"role"`   // 角色名称
    Tools []string `json:"tools"`  // 工具白名单 (空=继承默认)
}

// 2. assign_task - 分配任务给 Worker
type AssignTaskInput struct {
    WorkerRole  string   `json:"worker_role"`
    Description string   `json:"description"`
    Prompt      string   `json:"prompt"`
    DependsOn   []string `json:"depends_on,omitempty"` // 依赖任务
    Background  bool     `json:"background"`           // 后台执行
}

// 3. check_tasks - 检查所有任务状态
type CheckTasksInput struct {
    TaskIDs []string `json:"task_ids,omitempty"` // 空=全部
}

// 4. send_message - 向运行中的 Worker 发送消息
type SendMessageInput struct {
    WorkerID string `json:"worker_id"`
    Message  string `json:"message"`
}

// 5. merge_results - 汇总所有 Worker 结果
type MergeResultsInput struct {
    TaskIDs  []string `json:"task_ids"`
    Strategy string   `json:"strategy"` // "concatenate" | "summarize" | "diff"
}

// 6. dismiss_team - 解散团队
type DismissTeamInput struct {
    Reason string `json:"reason,omitempty"`
}
```

#### 12.2.4 Worker 隔离策略

```go
// internal/teams/isolation.go

// IsolationStrategy 隔离策略接口
type IsolationStrategy interface {
    Setup(workerID string, parentEnv *tools.Env) (*tools.Env, error)
    Teardown(workerID string) error
    MergeBack(workerID string) error
}

// SharedIsolation - 共享同一个工作目录 (适合只读研究)
type SharedIsolation struct{}
func (s *SharedIsolation) Setup(id string, parent *tools.Env) (*tools.Env, error) {
    return parent.CloneForSubagent(), nil // 共享 Executor, 隔离 TodoStore
}

// WorktreeIsolation - Git Worktree 隔离 (适合并行修改)
type WorktreeIsolation struct {
    basePath string
    worktrees map[string]string // workerID → worktree path
}
func (w *WorktreeIsolation) Setup(id string, parent *tools.Env) (*tools.Env, error) {
    // 1. git worktree add /tmp/jcode-teams/<id> -b team/<id>
    // 2. 创建新 Env 指向 worktree 路径
    // 3. 返回隔离的 Env
    return nil, nil
}
func (w *WorktreeIsolation) MergeBack(id string) error {
    // git merge team/<id> 并处理冲突
    return nil
}

// CopyIsolation - 目录复制隔离 (非 Git 项目)
type CopyIsolation struct {
    copies map[string]string
}
func (c *CopyIsolation) Setup(id string, parent *tools.Env) (*tools.Env, error) {
    // cp -r <workdir> /tmp/jcode-teams/<id>/
    return nil, nil
}
```

#### 12.2.5 执行流程

```go
// internal/teams/coordinator.go

type TeamCoordinator struct {
    team        *Team
    parentAgent *adk.ChatModelAgent
    model       model.ChatModel
    notifyCh    chan *TaskNotification
}

// RunTeam 启动团队协调循环
func (tc *TeamCoordinator) RunTeam(ctx context.Context) error {
    // 1. 创建 Worker Agents (每个一个 goroutine)
    for _, spec := range tc.team.Config.Workers {
        worker := tc.createWorker(ctx, spec)
        tc.team.Workers[worker.ID] = worker
        go tc.workerLoop(ctx, worker)  // 后台 goroutine
    }

    // 2. 监听通知
    for {
        select {
        case notif := <-tc.notifyCh:
            // 将通知格式化后注入 Coordinator 对话
            tc.injectNotification(notif)
        case <-ctx.Done():
            tc.shutdownAll()
            return ctx.Err()
        }
    }
}

// workerLoop 单个 Worker 的运行循环
func (tc *TeamCoordinator) workerLoop(ctx context.Context, worker *TeamWorker) {
    for task := range tc.team.TaskQueue {
        if task.WorkerRole != worker.Role {
            tc.team.TaskQueue <- task // 放回队列
            continue
        }

        worker.CurrentTask = task
        worker.Status = WorkerBusy
        task.Status = TaskRunning

        // 等待依赖完成
        tc.waitDependencies(ctx, task)

        // 执行任务
        startTime := time.Now()
        result, err := tc.executeTask(ctx, worker, task)

        // 发送通知
        tc.notifyCh <- &TaskNotification{
            TaskID:   task.ID,
            WorkerID: worker.ID,
            Status:   task.Status,
            Result:   result,
        }

        worker.Status = WorkerIdle
    }
}

// executeTask 在 Worker 上执行单个任务
func (tc *TeamCoordinator) executeTask(ctx context.Context, worker *TeamWorker, task *Task) (*TaskResult, error) {
    messages := []schema.Message{
        schema.SystemMessage(tc.buildWorkerPrompt(worker, task)),
        schema.UserMessage(task.Prompt),
    }

    var output strings.Builder
    tokensBefore := model.TokenTracker.GetTotal()

    iter := worker.Agent.Run(ctx, messages)
    for {
        event, ok := iter.Next()
        if !ok { break }
        if event.Type == adk.EventTypeAssistantText {
            output.WriteString(event.Content)
        }
    }

    return &TaskResult{
        Output:     output.String(),
        TokensUsed: model.TokenTracker.GetTotal() - tokensBefore,
        Duration:   time.Since(startTime),
    }, nil
}
```

#### 12.2.6 使用场景示例

```
用户: "帮我重构用户系统，分离前后端代码，并添加测试"

Coordinator 分析后创建团队:
  ┌─────────────────────────┐
  │ Coordinator Plan:        │
  │ 1. Research (explore)    │
  │ 2. Backend (general)     │
  │ 3. Frontend (general)    │
  │ 4. Testing (general)     │
  │                          │
  │ Dependencies:            │
  │ Backend → Research       │
  │ Frontend → Research      │
  │ Testing → Backend,Front  │
  └─────────────────────────┘

执行流:
  T0: Research Worker 分析现有代码结构 (只读)
  T1: Research 完成 → Backend + Frontend 并行启动
  T2: Backend 重构 API | Frontend 重构 UI (并行, worktree 隔离)
  T3: 两者完成 → Testing Worker 编写测试
  T4: 所有完成 → Coordinator 汇总结果, merge worktrees
```

#### 12.2.7 实现路线图

| 阶段 | 内容 | 预估 |
|------|------|------|
| **Phase 1: 基础并行** | 异步 SubAgent + goroutine 并发 + 通知 channel | 1周 |
| **Phase 2: Team Tools** | create_team / assign_task / check_tasks 工具 | 1周 |
| **Phase 3: 隔离策略** | SharedIsolation + WorktreeIsolation | 1周 |
| **Phase 4: 依赖调度** | 任务依赖图 + 拓扑排序执行 | 3天 |
| **Phase 5: TUI 集成** | 团队状态面板 + Worker 进度显示 | 3天 |
| **Phase 6: 高级特性** | SendMessage / MergeResults / 冲突处理 | 1周 |

---

## 13. 改进优先级总结

### P0 - 必须立即实现 (安全/核心体验)

| # | 模块 | 改进项 | 预估 |
|---|------|--------|------|
| 1 | Sandbox | Write/Edit 路径验证 + 危险路径保护 | 1天 |
| 2 | Sandbox | 替换 InsecureIgnoreHostKey | 0.5天 |
| 3 | Agent Loop | 上下文压缩(消息摘要/截断) | 2-3天 |
| 4 | Agent Loop | Token Budget 跟踪与管理 | 1天 |
| 5 | Approval | 持久化审批规则 | 1天 |
| 6 | Approval | 规则模式匹配 (`Bash(npm:*)`) | 1天 |
| 7 | TUI | Model 拆分(状态分组) | 1天 |

### P1 - 高价值改进 (功能增强)

| # | 模块 | 改进项 | 预估 |
|---|------|--------|------|
| 8 | SubAgent | 异步子 Agent + 并行执行 | 3天 |
| 9 | Tool | 参数验证层 + 工具注册表 | 2天 |
| 10 | LSP | LSP 客户端 + definition/references/hover 工具 | 5天 |
| 11 | Integration | MCP 连接恢复 + OAuth 支持 | 3天 |
| 12 | Reminder | 多层指令文件 + @include | 2天 |
| 13 | Skills | 富 Frontmatter + 工具白名单 | 2天 |
| 14 | Agent Loop | 错误恢复循环 | 2天 |
| 15 | TUI | 组件库 + 快捷键系统 | 3天 |
| 16 | Approval | 危险命令检测 + 3层权限 | 2天 |

### P2 - 锦上添花 (高级功能)

| # | 模块 | 改进项 | 预估 |
|---|------|--------|------|
| 17 | Agent Teams | Phase 1-4 基础团队协调 | 3周 |
| 18 | LSP | 多语言 + 诊断自动注入 | 3天 |
| 19 | SubAgent | Agent 间消息 + 嵌套支持 | 3天 |
| 20 | Integration | WebSocket + 代理 + 多范围配置 | 3天 |
| 21 | TUI | Vim 模式 | 3天 |
| 22 | Sandbox | 网络控制 + 资源限制 | 3天 |

### P3 - 远期目标

| # | 模块 | 改进项 |
|---|------|--------|
| 23 | Agent Teams | Phase 5-6 高级特性 |
| 24 | Sandbox | Linux namespace/cgroup 沙箱 |
| 25 | TUI | 语音输入 |
| 26 | Tool | 工具摘要生成系统 |

---

## 附录 A: Claude Code 关键文件索引

| 模块 | 文件 | 说明 |
|------|------|------|
| Agent Loop | `src/QueryEngine.ts`, `src/query/query.ts` | 主循环 + 压缩 |
| Tools | `src/tools/`, `src/tools.ts`, `src/Tool.ts` | 工具系统 |
| SubAgent | `src/coordinator/`, `src/buddy/`, `src/tasks/` | 多Agent协调 |
| Skills | `src/skills/` | 技能系统 |
| TUI | `src/ink/`, `src/components/`, `src/screens/` | UI组件 |
| Approval | `src/hooks/toolPermission/`, `src/tools/BashTool/bashPermissions.ts` | 权限 |
| MCP | `src/services/mcp/` | MCP集成 |
| LSP | `src/services/lsp/` | LSP服务 |
| Sandbox | `src/utils/sandbox/` | 沙箱 |
| Remote | `src/bridge/`, `src/remote/` | 远程执行 |
| Keybindings | `src/keybindings/` | 快捷键 |
| Voice | `src/voice/` | 语音输入 |
| Vim | `src/vim/` | Vim模式 |
