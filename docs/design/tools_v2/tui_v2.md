# TUI V2 — 完整设计

## 概述

基于 Claude Code 的 Ink/React 组件化 TUI 架构，为 jcode 的 BubbleTea TUI 设计等价的 V2 升级方案。核心目标：组件化拆分、多态消息渲染、结构化权限对话框、流式进度显示、虚拟滚动。

---

## 1. Claude Code TUI 架构分析

### 1.1 核心架构

- **框架**: React + Ink（React for CLI）+ Yoga 布局引擎
- **渲染**: 自定义 Ink 渲染器，帧缓冲 diff 优化
- **状态管理**: Zustand-like AppState + React Context Provider
- **组件数**: 180+ 组件文件

### 1.2 关键设计模式

| 模式 | 实现 |
|------|------|
| **消息多态** | Message 组件根据 type 路由到 30+ 专用渲染组件 |
| **权限多态** | PermissionRequest 根据 tool 路由到专用权限对话框 |
| **虚拟滚动** | VirtualMessageList + ScrollBox，缓存行高 |
| **组件化工具结果** | 每个工具自定义 renderToolResultMessage |
| **流式进度** | onProgress callback → HookProgressMessage 实时更新 |
| **设计系统** | Dialog, Pane, Tabs, ThemeProvider 基础组件库 |
| **模态叠加** | ModalContext 管理 slash-command、settings 等浮层 |

### 1.3 组件层级

```
App → REPL → FullscreenLayout
├── ScrollBox → Messages → VirtualMessageList
│   └── MessageRow → Message (多态)
│       ├── AssistantTextMessage
│       ├── AssistantToolUseMessage → ToolUseLoader
│       ├── UserBashOutputMessage
│       └── 30+ 其他类型
├── Modal (slash-command、settings 浮层)
├── PermissionRequest (多态权限对话框)
├── SpinnerWithVerb (执行中指示器)
└── PromptInput → TextInput + StatusLine + Footer
```

---

## 2. jcode 当前 TUI 分析

### 2.1 架构

- **框架**: BubbleTea（Go 终端 UI 框架）
- **结构**: 单体 Model 结构体（~125 个字段）
- **渲染**: 单一 View() 函数 + 条件分支渲染

### 2.2 局限

| 问题 | 描述 |
|------|------|
| **单体 Model** | 所有状态（viewport、textarea、pickers、approval）混杂一起 |
| **无组件化** | View() 内 if-else 判断渲染路径，难以维护 |
| **固定消息格式** | 所有工具结果使用相同的 bordered box 渲染 |
| **无流式进度** | 工具执行中只显示 spinner，无实时输出 |
| **无虚拟滚动** | 全量渲染历史消息，大会话性能下降 |
| **硬编码权限对话框** | 所有工具共享一个审批 UI，无定制化 |
| **频道通信** | 15+ 全局 channel，类型安全弱 |

---

## 3. jcode TUI V2 设计

### 3.1 设计原则

1. **组件化拆分** — 独立组件负责独立关注点
2. **消息多态** — 每种工具定义自己的渲染方式
3. **权限多态** — 每种工具定义自己的审批 UI
4. **流式进度** — 工具执行中实时显示部分输出
5. **渐进式迁移** — 保留 BubbleTea 框架，增量重构

### 3.2 组件化拆分

#### 3.2.1 新文件结构

```
internal/tui/
├── tui.go                    # Model + Init/Update/View 主干（精简）
├── messages.go               # 消息类型定义（保留）
├── styles.go                 # 样式系统（保留）
│
├── components/               # ===== V2 新增 =====
│   ├── viewport.go           # 增强 viewport（虚拟滚动支持）
│   ├── message_list.go       # 消息列表组件
│   ├── message_renderer.go   # 消息多态渲染路由
│   ├── input_area.go         # 输入区域组件
│   ├── status_bar.go         # 状态栏组件（重构）
│   ├── spinner.go            # 增强 spinner（动词 + 工具名）
│   └── progress.go           # 流式进度组件
│
├── permissions/              # ===== V2 新增 =====
│   ├── router.go             # 权限对话框路由
│   ├── bash_permission.go    # Bash 命令审批
│   ├── edit_permission.go    # 文件编辑审批（含 diff 预览）
│   ├── write_permission.go   # 文件写入审批
│   └── default_permission.go # 默认审批（fallback）
│
├── renderers/                # ===== V2 新增 =====
│   ├── registry.go           # 工具渲染器注册表
│   ├── bash_result.go        # Bash 输出渲染（可展开/折叠）
│   ├── edit_result.go        # 编辑结果渲染（结构化 diff）
│   ├── grep_result.go        # 搜索结果渲染（高亮匹配）
│   ├── read_result.go        # 文件读取渲染
│   ├── todo_result.go        # Todo 变更渲染
│   ├── subagent_result.go    # Subagent 进度渲染
│   └── default_result.go     # 默认工具结果渲染
│
├── pickers.go                # 选择器（保留，重构为组件）
├── input_views.go            # 输入视图（迁移到 input_area.go）
├── format.go                 # 格式化工具（保留）
├── setup.go                  # TUI 初始化（保留）
├── session_helper.go         # 会话辅助（保留）
└── ssh_handlers.go           # SSH 处理（保留）
```

#### 3.2.2 消息多态渲染

```go
// renderers/registry.go

// ToolRenderer 工具结果渲染接口
type ToolRenderer interface {
    // RenderToolUse 渲染工具调用（执行中显示）
    RenderToolUse(name string, args map[string]interface{}, width int) string
    // RenderToolResult 渲染工具结果
    RenderToolResult(name string, output string, err string, width int, verbose bool) string
    // RenderProgress 渲染执行中进度（可选）
    RenderProgress(data ProgressData, width int) string
}

// RendererRegistry 工具渲染器注册表
type RendererRegistry struct {
    renderers map[string]ToolRenderer
    fallback  ToolRenderer
}

func NewRendererRegistry() *RendererRegistry {
    r := &RendererRegistry{
        renderers: make(map[string]ToolRenderer),
        fallback:  &DefaultRenderer{},
    }
    // 注册内置渲染器
    r.Register("execute", &BashRenderer{})
    r.Register("edit", &EditRenderer{})
    r.Register("read", &ReadRenderer{})
    r.Register("grep", &GrepRenderer{})
    r.Register("write", &WriteRenderer{})
    r.Register("todowrite", &TodoRenderer{})
    r.Register("todoread", &TodoRenderer{})
    r.Register("subagent", &SubagentRenderer{})
    r.Register("ask_user", &AskUserRenderer{})
    return r
}

func (r *RendererRegistry) Render(name, output, errStr string, width int, verbose bool) string {
    if renderer, ok := r.renderers[name]; ok {
        return renderer.RenderToolResult(name, output, errStr, width, verbose)
    }
    return r.fallback.RenderToolResult(name, output, errStr, width, verbose)
}
```

#### 3.2.3 各工具渲染器

```go
// renderers/bash_result.go

type BashRenderer struct{}

func (br *BashRenderer) RenderToolResult(name, output, errStr string, width int, verbose bool) string {
    var b strings.Builder
    lines := strings.Split(output, "\n")

    if verbose || len(lines) <= 10 {
        // 完整输出
        b.WriteString(renderBorderedBox(output, width, styles.Secondary))
    } else {
        // 折叠模式：显示前3行 + 后3行
        header := strings.Join(lines[:3], "\n")
        footer := strings.Join(lines[len(lines)-3:], "\n")
        b.WriteString(renderBorderedBox(
            header+"\n"+
                styles.Muted.Render(fmt.Sprintf("  ... (%d lines hidden) ...", len(lines)-6))+"\n"+
                footer,
            width, styles.Secondary,
        ))
    }

    if errStr != "" {
        b.WriteString("\n")
        b.WriteString(styles.Error.Render("stderr: "+errStr))
    }
    return b.String()
}

func (br *BashRenderer) RenderProgress(data ProgressData, width int) string {
    // 实时显示 bash 输出的最后几行
    lines := strings.Split(data.PartialOutput, "\n")
    if len(lines) > 5 {
        lines = lines[len(lines)-5:]
    }
    return renderBorderedBox(
        strings.Join(lines, "\n"),
        width, styles.Muted,
    ) + "\n" + styles.Muted.Render(
        fmt.Sprintf("  ⏱ %ds elapsed, %d lines", data.ElapsedSec, data.TotalLines),
    )
}
```

```go
// renderers/edit_result.go

type EditRenderer struct{}

func (er *EditRenderer) RenderToolResult(name, output, errStr string, width int, verbose bool) string {
    // 解析结构化 diff
    if diff := tryParseDiff(output); diff != nil {
        return er.renderStructuredDiff(diff, width)
    }
    // fallback: 显示原始输出
    return renderBorderedBox(output, width, styles.Secondary)
}

func (er *EditRenderer) renderStructuredDiff(diff *DiffResult, width int) string {
    var b strings.Builder
    b.WriteString(styles.Secondary.Render(fmt.Sprintf("  📝 %s", diff.FilePath)))
    b.WriteString("\n")

    for _, hunk := range diff.Hunks {
        for _, line := range hunk.Lines {
            switch {
            case strings.HasPrefix(line, "+"):
                b.WriteString(styles.Success.Render(line))
            case strings.HasPrefix(line, "-"):
                b.WriteString(styles.Error.Render(line))
            default:
                b.WriteString(styles.Muted.Render(line))
            }
            b.WriteString("\n")
        }
    }
    return b.String()
}
```

```go
// renderers/grep_result.go

type GrepRenderer struct{}

func (gr *GrepRenderer) RenderToolResult(name, output, errStr string, width int, verbose bool) string {
    lines := strings.Split(strings.TrimSpace(output), "\n")
    var b strings.Builder

    // 按文件分组匹配结果
    groups := groupByFile(lines)
    for file, matches := range groups {
        b.WriteString(styles.Secondary.Render("  "+file))
        b.WriteString("\n")
        displayMatches := matches
        if !verbose && len(matches) > 5 {
            displayMatches = matches[:5]
        }
        for _, m := range displayMatches {
            // 高亮匹配文本
            b.WriteString("    " + highlightMatch(m, width-4))
            b.WriteString("\n")
        }
        if !verbose && len(matches) > 5 {
            b.WriteString(styles.Muted.Render(
                fmt.Sprintf("    ... (%d more matches)", len(matches)-5),
            ))
            b.WriteString("\n")
        }
    }

    total := len(lines)
    b.WriteString(styles.Muted.Render(fmt.Sprintf("  %d matches in %d files", total, len(groups))))
    return b.String()
}
```

#### 3.2.4 权限对话框多态

```go
// permissions/router.go

// PermissionRenderer 权限对话框渲染接口
type PermissionRenderer interface {
    // RenderApprovalDialog 渲染审批对话框
    RenderApprovalDialog(toolName string, args map[string]interface{}, width int) string
    // Options 返回可用操作选项
    Options() []ApprovalOption
}

type ApprovalOption struct {
    Key   string // "y", "a", "n", "e"
    Label string // "Approve once", "Approve all", "Reject", "Edit"
}

type PermissionRouter struct {
    renderers map[string]PermissionRenderer
    fallback  PermissionRenderer
}

func NewPermissionRouter() *PermissionRouter {
    r := &PermissionRouter{
        renderers: make(map[string]PermissionRenderer),
        fallback:  &DefaultPermission{},
    }
    r.Register("execute", &BashPermission{})
    r.Register("edit", &EditPermission{})
    r.Register("write", &WritePermission{})
    return r
}
```

```go
// permissions/bash_permission.go

type BashPermission struct{}

func (bp *BashPermission) RenderApprovalDialog(toolName string, args map[string]interface{}, width int) string {
    command := args["command"].(string)
    var b strings.Builder

    b.WriteString(styles.Warning.Render("⚠️  Command Execution"))
    b.WriteString("\n\n")

    // 语法高亮显示命令
    b.WriteString(renderCommandBox(command, width))
    b.WriteString("\n")

    // 如果是危险命令，显示警告
    if isDangerous(command) {
        b.WriteString(styles.Error.Render("  ⚡ This command may modify your system"))
        b.WriteString("\n")
    }

    b.WriteString("\n")
    b.WriteString(styles.Muted.Render("  [y] Approve  [a] Approve all  [n] Reject  [e] Edit"))
    return b.String()
}

func (bp *BashPermission) Options() []ApprovalOption {
    return []ApprovalOption{
        {Key: "y", Label: "Approve once"},
        {Key: "a", Label: "Approve all"},
        {Key: "n", Label: "Reject"},
        {Key: "e", Label: "Edit command"},
    }
}
```

```go
// permissions/edit_permission.go

type EditPermission struct{}

func (ep *EditPermission) RenderApprovalDialog(toolName string, args map[string]interface{}, width int) string {
    filePath := args["file_path"].(string)
    oldStr := args["old_string"].(string)
    newStr := args["new_string"].(string)

    var b strings.Builder
    b.WriteString(styles.Warning.Render("⚠️  File Edit"))
    b.WriteString("\n\n")
    b.WriteString(styles.Secondary.Render("  📝 " + filePath))
    b.WriteString("\n\n")

    // 内联 diff 预览
    b.WriteString(renderInlineDiff(oldStr, newStr, width-4))
    b.WriteString("\n\n")
    b.WriteString(styles.Muted.Render("  [y] Approve  [a] Approve all  [n] Reject"))
    return b.String()
}
```

#### 3.2.5 流式进度系统

```go
// components/progress.go

// ProgressData 工具执行进度数据
type ProgressData struct {
    ToolName      string
    PartialOutput string
    ElapsedSec    int
    TotalLines    int
    Percentage    float64  // 0-100，-1 表示未知
}

// ProgressDisplay 流式进度显示组件
type ProgressDisplay struct {
    data       ProgressData
    blink      bool
    lastUpdate time.Time
}

func (pd *ProgressDisplay) View(width int) string {
    var b strings.Builder

    // 动画指示器
    indicator := "●"
    if pd.blink {
        indicator = "○"
    }
    b.WriteString(styles.Secondary.Render(indicator + " "))
    b.WriteString(styles.Secondary.Render(pd.data.ToolName))

    // 进度条（如果有百分比）
    if pd.data.Percentage >= 0 {
        b.WriteString(" ")
        b.WriteString(renderProgressBar(pd.data.Percentage, 20))
    }

    // 经过时间
    b.WriteString(styles.Muted.Render(fmt.Sprintf(" %ds", pd.data.ElapsedSec)))
    b.WriteString("\n")

    // 部分输出（最后几行）
    if pd.data.PartialOutput != "" {
        renderer := getToolRenderer(pd.data.ToolName)
        b.WriteString(renderer.RenderProgress(pd.data, width))
    }

    return b.String()
}

func renderProgressBar(pct float64, width int) string {
    filled := int(pct / 100 * float64(width))
    empty := width - filled
    return styles.Success.Render(strings.Repeat("█", filled)) +
        styles.Muted.Render(strings.Repeat("░", empty)) +
        fmt.Sprintf(" %.0f%%", pct)
}
```

#### 3.2.6 增强 Spinner

```go
// components/spinner.go

// SpinnerWithVerb 带动词的增强 spinner
type SpinnerWithVerb struct {
    spinner  spinner.Model
    verb     string     // "Running", "Thinking", "Searching"
    toolName string     // 当前工具名
    elapsed  time.Duration
    start    time.Time
}

func NewSpinnerWithVerb() SpinnerWithVerb {
    s := spinner.New()
    s.Spinner = spinner.MiniDot
    s.Style = styles.Secondary
    return SpinnerWithVerb{spinner: s, start: time.Now()}
}

func (sw *SpinnerWithVerb) SetTool(verb, toolName string) {
    sw.verb = verb
    sw.toolName = toolName
    sw.start = time.Now()
}

func (sw SpinnerWithVerb) View() string {
    elapsed := time.Since(sw.start).Round(time.Second)
    parts := []string{
        sw.spinner.View(),
        styles.Secondary.Render(sw.verb),
    }
    if sw.toolName != "" {
        parts = append(parts, styles.Muted.Render(sw.toolName))
    }
    if elapsed > 2*time.Second {
        parts = append(parts, styles.Muted.Render(fmt.Sprintf("(%s)", elapsed)))
    }
    return strings.Join(parts, " ")
}
```

#### 3.2.7 消息列表组件

```go
// components/message_list.go

// MessageList 管理消息显示
type MessageList struct {
    messages      []DisplayMessage
    registry      *RendererRegistry
    width         int
    verbose       bool
    searchQuery   string
    searchMatches []int  // 匹配消息的索引
}

// DisplayMessage 统一消息展示模型
type DisplayMessage struct {
    Type      MessageType
    Role      string   // "user", "assistant", "tool"
    Content   string
    ToolName  string
    ToolArgs  map[string]interface{}
    ToolError string
    Timestamp time.Time
}

type MessageType int
const (
    MsgUser MessageType = iota
    MsgAssistant
    MsgToolCall
    MsgToolResult
    MsgProgress
    MsgSystem
)

func (ml *MessageList) Render() string {
    var b strings.Builder
    for _, msg := range ml.messages {
        switch msg.Type {
        case MsgUser:
            b.WriteString(styles.Primary.Render("❯ "))
            b.WriteString(msg.Content)
        case MsgAssistant:
            // Markdown 渲染
            rendered, _ := glamour.Render(msg.Content, "dark")
            b.WriteString(rendered)
        case MsgToolCall:
            b.WriteString(styles.Secondary.Render("🔧 "))
            renderer := ml.registry.GetRenderer(msg.ToolName)
            b.WriteString(renderer.RenderToolUse(msg.ToolName, msg.ToolArgs, ml.width))
        case MsgToolResult:
            renderer := ml.registry.GetRenderer(msg.ToolName)
            b.WriteString(renderer.RenderToolResult(
                msg.ToolName, msg.Content, msg.ToolError, ml.width, ml.verbose))
        case MsgProgress:
            b.WriteString(styles.Muted.Render(msg.Content))
        }
        b.WriteString("\n\n")
    }
    return b.String()
}
```

### 3.3 Model 精简

```go
// tui.go — V2 精简后的 Model

type Model struct {
    // Core
    mode    Mode
    ready   bool
    width   int
    height  int

    // Components (extracted)
    viewport    viewport.Model
    msgList     *components.MessageList
    input       *components.InputArea
    statusBar   *components.StatusBar
    spinner     components.SpinnerWithVerb
    progress    *components.ProgressDisplay

    // Routers
    permRouter  *permissions.PermissionRouter
    renderers   *renderers.RendererRegistry

    // Interactive State (simplified)
    interState  InteractiveState

    // Context
    ctx ModelContext
}

type InteractiveState struct {
    Kind         InteractiveKind
    ApprovalData *ApprovalData
    PlanData     *PlanReviewData
    AskUserData  *AskUserData
    PickerData   *PickerData
}

type InteractiveKind int
const (
    InterNone InteractiveKind = iota
    InterApproval
    InterPlanReview
    InterAskUser
    InterModelPicker
    InterSessionPicker
    InterSettingMenu
    InterSSHSetup
)

type ModelContext struct {
    agentMode   AgentMode
    pwd         string
    envLabel    string
    todoStore   *tools.TodoStore
    tokens      TokenInfo
    pendingTool string
}
```

### 3.4 新增消息类型

```go
// messages.go — V2 新增

// ToolProgressMsg 工具执行进度
type ToolProgressMsg struct {
    ToolName      string
    PartialOutput string
    ElapsedSec    int
    TotalLines    int
}

// StreamingDiffMsg 流式 diff 更新
type StreamingDiffMsg struct {
    FilePath string
    Hunks    []DiffHunk
}

// ConflictDetectedMsg 文件冲突检测
type ConflictDetectedMsg struct {
    FilePath   string
    OldHash    string
    NewHash    string
    Resolution string // "overwrite", "abort", "merge"
}

// BackgroundPromotionMsg 前台→后台提升
type BackgroundPromotionMsg struct {
    TaskID  string
    Command string
    Reason  string // "timeout" or "user_request"
}
```

---

## 4. 对比矩阵

| 维度 | Claude Code | jcode V1 | jcode V2 (设计) |
|------|-------------|----------|-----------------|
| **框架** | React + Ink | BubbleTea | BubbleTea (保留) |
| **组件化** | 180+ 组件文件 | 单体 Model | 组件化拆分 (~20 文件) |
| **消息渲染** | 30+ 专用渲染组件 | 统一 bordered box | 7+ 专用渲染器 + 注册表 |
| **权限对话框** | 12+ 工具自定义对话框 | 单一通用对话框 | 3+ 专用对话框 + 路由 |
| **流式进度** | onProgress + AsyncGenerator | 仅 spinner | ProgressDisplay + 部分输出 |
| **虚拟滚动** | VirtualMessageList | 无 | 增强 viewport 组件 |
| **状态管理** | Zustand-like Store | 125+ 字段 Model | 拆分 Components + Context |
| **搜索** | 全文搜索 + 高亮 | 无 | MessageList 搜索 |
| **Diff 预览** | 结构化 diff + 语法高亮 | 简单 +/- 颜色 | 结构化 diff 渲染器 |
| **折叠/展开** | 可展开工具输出 | 固定截断 | verbose 模式切换 |
| **设计系统** | Dialog, Pane, Tabs, Theme | 内联样式 | styles 包 + 组件模板 |

---

## 5. 数据流

### 5.1 消息流（V2）

```
Runner.runInner()
  ├── AgentTextMsg ────────→ Model.Update() → msgList.Append(MsgAssistant)
  ├── ToolCallMsg ─────────→ Model.Update() → msgList.Append(MsgToolCall)
  │                                           → spinner.SetTool("Running", name)
  ├── ToolProgressMsg ─────→ Model.Update() → progress.Update(data)
  │     (NEW: 流式进度)                       → msgList.UpdateLast(MsgProgress)
  ├── ToolResultMsg ───────→ Model.Update() → msgList.Append(MsgToolResult)
  │                                           → renderers.Render(name, output)
  └── AgentDoneMsg ────────→ Model.Update() → spinner.Stop()
                                              → input.Focus()
```

### 5.2 审批流（V2）

```
ApprovalState.RequestApproval(toolName, args)
  │
  ├── AUTO Mode → return true
  │
  └── MANUAL Mode
      ├── Read-only tool → return true
      └── Mutating tool
          │
          ├── ToolApprovalRequestMsg → TUI
          │     │
          │     ├── permRouter.GetRenderer(toolName)
          │     │     └── BashPermission.RenderApprovalDialog()
          │     │         ├── 命令高亮显示
          │     │         ├── 危险命令警告
          │     │         └── 操作选项
          │     │
          │     ├── User: [y] → Approve → respCh
          │     ├── User: [a] → Approve + AutoMode → respCh
          │     ├── User: [n] → Reject → respCh
          │     └── User: [e] → Edit → 编辑框 → 重新提交
          │
          └── respCh ← response
```

---

## 6. 实现优先级

### Phase 1: 组件化基础
1. 创建 `components/`, `renderers/`, `permissions/` 目录
2. 提取 RendererRegistry + DefaultRenderer
3. 提取 SpinnerWithVerb
4. 精简 Model 结构体

### Phase 2: 工具渲染器
5. BashRenderer (折叠/展开)
6. EditRenderer (结构化 diff)
7. GrepRenderer (分组高亮)
8. SubagentRenderer (进度框)

### Phase 3: 权限对话框
9. PermissionRouter
10. BashPermission (命令预览)
11. EditPermission (diff 预览)

### Phase 4: 流式进度
12. ProgressDisplay 组件
13. ToolProgressMsg 消息管道
14. Runner 集成 progress callback

### Phase 5: 增强体验
15. MessageList 搜索
16. verbose 模式切换
17. 消息折叠/展开
