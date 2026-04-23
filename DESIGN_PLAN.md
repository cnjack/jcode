# JCODE TUI Redesign Plan

## 1. 设计总览

将当前单列全宽布局重构为 **响应式两栏布局**，核心变化：

- **Landing Page（无对话）**：Google 风格，居中 JCODE Logo + tagline + 输入框，无右侧栏，极简干净
- **Chat Mode（有对话后）**：主内容区（左）+ 信息侧栏（右）
- **窄屏回退（< 90 列）**：隐藏侧栏，回退单栏布局
- **品牌统一**：所有 "Little Jack" → "JCODE"

参考 [Crush](https://github.com/charmbracelet/crush) 和 [OpenCode](https://github.com/anomalyco/opencode) 的侧栏 + 主内容区分栏设计。

---

## 2. 布局线框图

### 2.1 Landing Page（无对话，无侧边栏）

```
+---------------------------------------------------------------+
|                                                               |
|                                                               |
|                                                               |
|                                                               |
|                                                               |
|                                                               |
|                                                               |
|                                                               |
|                    J  C  O  D  E                              |
|                    ═════════════                              |
|                    coding assistant                           |
|                                                               |
|                                                               |
|                                                               |
|                                                               |
|                                                               |
|                                                               |
|  ━━━ Agent ━━━ · ━━━ Ask ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ |
|  > Type your prompt here...                                   |
|  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ |
|                                                               |
+---------------------------------------------------------------+
```

- 终端中心区域显示 **J C O D E** 大 Logo（violet bold，字母间 2 空格）
- 下方细线 `═════════════` + "coding assistant" tagline（muted color）
- 下方大面积留白
- 底部：模式指示 pills（Agent/Plan + Ask/Auto）在输入框上方
- 输入框全宽，placeholder 提示
- **无任何右侧栏、无任何提示文字、无 header bar**

### 2.2 Chat Mode（有对话，宽屏 ≥ 90 列）

```
+----------------------------------------+----------------------+
|                                        │  J C O D E           |
|  👤 You: hello                         │  ─────────           |
|                                        │                      |
|  🤖 Assistant: Hi! How can I help?     │  Model               |
|                                        │  openai / gpt-4      |
|                                        │                      |
|  ● Read file.go                        │  Env                 |
|    ✓ result...                         │  🖥️  Local            |
|                                        │                      |
|                                        │  Usage               |
|                                        │  [████░░░░░] 45%     |
|                                        │                      |
|                                        │  📋 Todo (3/5)       |
|                                        │  ✓ completed task    |
|                                        │  ⏳ in progress       |
|                                        │  ○ pending task      |
|                                        │  ○ another task      |
|                                        │  ▼ 1 more            |
|                                        │                      |
|                                        │  MCP                 |
|                                        │  ● filesystem (3)    |
|                                        │                      |
+----------------------------------------+----------------------+
|  ━━━ Agent ━━━ · ━━━ Ask ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ |
|  > What's next?                                               |
|  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ |
+---------------------------------------------------------------+
```

- **左侧主区域**：聊天 viewport（对话内容、tool call、结果、spinner）
- **右侧边栏**（34 字符宽）：JCODE logo → Model → Env → Usage → Todo → MCP
- **底部**：模式 pills 行 + 输入框 + 精简状态栏
- 侧栏高度与 viewport 对齐（不含底部输入区）

### 2.3 窄屏回退（< 90 列）

```
+---------------------------------------------------------------+
|  👤 You: hello                                                |
|                                                               |
|  🤖 Assistant: Hi! How can I help?                            |
|                                                               |
|  ● Read file.go                                               |
|    ✓ result...                                                |
|                                                               |
|  ━━━ Agent ━━━ · ━━━ Ask ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ |
|  > What's next?                                               |
|  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ |
|  Model: openai/gpt-4  │  Approve: Ask  │ [██░░░] 40%         |
+---------------------------------------------------------------+
```

- 侧栏自动隐藏
- 关键信息（model、approve、usage）回退到精简状态栏
- 单栏布局与当前类似，但模式 pills 仍然在输入框上方

---

## 3. 右侧边栏设计（Sidebar Component）

### 3.1 文件：`internal/tui/sidebar_component.go`（新增）

```go
type SidebarState struct {
    Width             int
    Height            int
    EnvLabel          string
    ActiveProvider    string
    ActiveModel       string
    TotalTokens       int64
    ModelContextLimit int
    TodoItems         []tools.TodoItem
    TodoScrollOffset  int  // 滚动偏移
    MCPStatuses       []MCPStatusItem
    HasConversation   bool // 是否有对话内容
}

type SidebarComponent struct{}

func (s *SidebarComponent) View(state SidebarState) string
```

### 3.2 侧栏各区块设计

**Logo 区（2 行）：**
```
  J C O D E
  ─────────
```
- "J C O D E": `colorPrimary` (violet), Bold, 字母间 2 空格
- 下方 `─` 线：同色系但 muted

**Model 区（2-3 行）：**
```
  Model
  openai / gpt-4
```
- "Model" label: `colorMuted` + Bold
- model 名: `colorText`，provider/model 用 `/` 分隔

**Env 区（2 行）：**
```
  Env
  🖥️  Local
```
- SSH 时显示 `🔗 SSH (label)`

**Usage 区（2 行）：**
```
  Usage
  [████░░░░░] 45%
```
- 复用现有 `renderProgressBar()`
- bar 颜色：绿 <70%、橙 70-90%、红 >90%

**Todo 区（动态高度，最少 2 行）：**
```
  📋 Todo (3/5)
  ✓ completed
  ⏳ in progress
  ○ pending
  ▼ 1 more
```
- 可滚动：通过 `TodoScrollOffset` 控制显示哪些项
- 显示范围由可用高度动态计算
- 滚动指示：▲ 上方还有更多 / ▼ 下方还有更多
- 每项带对应状态图标（复用现有 todo styles）
- 最多占用侧栏剩余空间的 60%

**MCP 区（动态高度）：**
```
  MCP
  ● filesystem (3)
  ○ github (0)
```
- 只显示 running 的 server
- `●` green = running, `○` muted = inactive
- 格式：`name (toolCount)`

### 3.3 侧栏整体样式

```go
sidebarStyle = lipgloss.NewStyle().
    Border(lipgloss.Border{Left: "│"}).
    BorderForeground(colorMuted).
    PaddingLeft(1).
    PaddingRight(1)

sidebarSectionTitleStyle = lipgloss.NewStyle().
    Bold(true).
    Foreground(colorMuted)

sidebarItemStyle = lipgloss.NewStyle().
    Foreground(colorText)
```

侧栏左边界用 `│` 细线分隔主内容区，整体 padding 1，无上下边框。

---

## 4. 模式指示 Pills（输入框上方）

### 4.1 设计

在输入框 textarea 的**正上方**添加一行模式指示：

```
  ━━━ Agent ━━━ · ━━━ Ask ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

**Agent/Plan Pill：**
- 正常: `Agent` — cyan bold，无背景
- 计划: `Plan` — color "99" bold，无背景
- 当前激活模式用 `━━━` 包裹，例如 `━━━ Agent ━━━`

**Approve Pill：**
- Ask: `Ask` — `colorMuted`
- Auto: `Auto` — `colorWarning` (amber)
- 同样用 `━━━` 包裹激活状态

**分隔：** 两个 pill 之间用 ` · `（居中点）分隔

**剩余部分：** 用 `━` 或 `─` 填充到行尾

### 4.2 代码位置

新增 `renderModePills()` 在 `input_views.go`，`inputAreaView()` 在 textarea 上方插入这行。

---

## 5. Landing Page 实现

### 5.1 触发条件

当 `len(m.lines) == 0 && !m.thinking` 时，显示 Landing Page。

### 5.2 Landing Page View

新增 `landingPageView()` 方法：

```go
func (m Model) landingPageView() string {
    // 垂直居中计算
    logo := lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).
        Render("J  C  O  D  E")
    underline := lipgloss.NewStyle().Foreground(colorMuted).
        Render("═════════════")
    tagline := lipgloss.NewStyle().Foreground(colorMuted).
        Render("coding assistant")

    // 垂直居中排列
    contentHeight := 5 // logo + underline + tagline + spacing
    topPad := (m.height - contentHeight - m.inputAreaHeight()) / 2
    if topPad < 0 { topPad = 0 }

    parts := []string{
        strings.Repeat("\n", topPad),
        lipgloss.PlaceHorizontal(m.width, lipgloss.Center, logo),
        lipgloss.PlaceHorizontal(m.width, lipgloss.Center, underline),
        lipgloss.PlaceHorizontal(m.width, lipgloss.Center, tagline),
    }
    parts = append(parts, m.inputAreaView())
    return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
```

### 5.3 Landing → Chat 过渡

用户提交第一条消息后，`m.lines` 不再为空，自动切换到 Chat Mode 布局，侧边栏出现。

---

## 6. 品牌统一：Little Jack → JCODE

| 文件 | 当前文本 | 修改为 |
|------|---------|--------|
| `internal/tui/tui.go:288` | Welcome to Little Jack... | Welcome to JCODE... |
| `internal/tui/tui.go:1203` | Welcome to Little Jack | Welcome to JCODE |
| `internal/tui/tui.go:1529` | Welcome to Little Jack | Welcome to JCODE |
| `internal/tui/tui.go:2248` | 🚀 Little Jack — Coding Assistant | （删除，header 栏移除） |
| `internal/tui/tui.go:2470` | 🚀 Little Jack — Coding Assistant | J C O D E |
| `internal/tui/setup.go:477` | 🚀 Little Jack — Setup | ▓▒░ JCODE — Setup ░▒▓ |
| `cmd/jcode/main.go:21` | Little Jack — AI coding assistant | JCODE — AI coding assistant |
| `cmd/jcode/main.go:29` | Little Jack — Coding Assistant | JCODE — Coding Assistant |
| `internal/command/commands.go:20` | Little Jack — Coding Assistant | JCODE — Coding Assistant |
| `internal/command/commands.go:28` | Little Jack — Coding Assistant | JCODE — Coding Assistant |
| `internal/command/acp.go:191` | Little Jack — Coding Assistant | JCODE — Coding Assistant |
| `script/install.sh:148` | Little Jack — Coding Assistant | JCODE — Coding Assistant |
| `internal/prompts/system.md:1` | You are "Little Jack"... | You are "JCODE"... |
| `internal/prompts/plan.md:1` | You are "Little Jack"... | You are "JCODE"... |
| `internal-docs/design/.../ui.md` | 🚀 Little Jack — Coding Assistant | JCODE |

---

## 7. 两栏布局核心修改（tui.go View()）

### 7.1 新增常量与字段

```go
const sidebarWidth = 34  // 侧栏固定宽度（含边界和 padding）
const minWidthForSidebar = 90  // 显示侧栏的最小终端宽度

// Model 新增字段
type Model struct {
    // ... 现有字段 ...
    showSidebar        bool   // 当前是否显示侧栏
    sidebarScrollOffset int   // 侧栏 todo 滚动偏移
    // ...
}
```

### 7.2 View() 重构逻辑

```go
func (m Model) View() tea.View {
    // 各种 picker/modal 的提前返回保持不变 ...

    if !m.ready {
        return m.newView("\n  Initializing...")
    }

    // Landing page: 无对话且无思考中
    if len(m.lines) == 0 && !m.thinking && !m.agentDone {
        return m.newView(m.landingPageView())
    }

    // 决定是否显示侧栏
    m.showSidebar = m.width >= minWidthForSidebar

    // 计算主内容区宽度
    mainWidth := m.width
    if m.showSidebar {
        mainWidth = m.width - sidebarWidth - 1  // -1 for gap
    }

    // 渲染输入区（全宽，因为 pills 和状态栏需要全宽信息）
    footer := m.inputAreaView()
    footerHeight := lipgloss.Height(footer)

    // 主内容区（viewport）
    m.viewport.SetWidth(mainWidth)
    m.viewport.SetHeight(m.height - footerHeight)
    if m.viewport.Height() < 3 {
        m.viewport.SetHeight(3)
    }
    m.viewport.SetContent(strings.TrimRight(m.renderContent(), "\n"))

    vpView := m.viewport.View()
    if m.hasSelection || m.mouseSelecting {
        vpView = m.applySelectionHighlight(vpView)
    }

    // 组装主视图
    if m.showSidebar {
        // 渲染侧栏
        sidebar := m.renderSidebar()
        // 水平拼接：主内容 + 侧栏
        contentRow := lipgloss.JoinHorizontal(lipgloss.Top, vpView, sidebar)
        mainView := lipgloss.JoinVertical(lipgloss.Left, contentRow, footer)
        return m.newView(mainView)
    }

    // 窄屏：单栏布局
    mainView := lipgloss.JoinVertical(lipgloss.Left, vpView, footer)
    return m.newView(mainView)
}
```

### 7.3 Header 栏移除

当前 header（`🚀 Little Jack... | 🖥️ Env: Local`）完全移除：
- Logo → 侧栏 / Landing Page
- Env → 侧栏
- Team pill → 侧栏（新增）或保留在底部状态栏

---

## 8. 输入区重构（input_views.go）

### 8.1 inputAreaView() 新结构

```go
func (m Model) inputAreaView() string {
    var parts []string

    // 1. 模式 pills 行（新增）
    parts = append(parts, m.renderModePills())

    // 2. 输入框内容
    switch {
    case m.planReviewActive:
        parts = append(parts, m.planReviewPromptView())
    case m.askUserActive:
        parts = append(parts, m.askUserPromptView())
    default:
        if m.cmdSuggestionActive && len(m.cmdSuggestions) > 0 {
            parts = append(parts, m.renderCommandSuggestions())
        }
        parts = append(parts, lipgloss.NewStyle().PaddingLeft(1).PaddingRight(2).
            Render(strings.TrimRight(m.textarea.View(), "\n")))
    }

    // 3. 精简状态栏（窄屏时显示关键信息）
    if !m.showSidebar {
        parts = append(parts, m.renderFallbackStatusBar())
    } else {
        // 宽屏时状态栏只保留 copy notice + bg tasks
        parts = append(parts, m.renderMinimalStatusBar())
    }

    return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
```

### 8.2 Todo Bar 从输入区移除

`renderTodoBar()` 不再在 `inputAreaView()` 中调用，Todo 完全移到侧栏。

### 8.3 renderModePills() 新增

```go
func (m Model) renderModePills() string {
    // Agent/Plan pill
    var modePill string
    switch m.agentMode {
    case ModePlanning:
        modePill = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")).
            Render("━━━ Plan ━━━")
    default:
        modePill = lipgloss.NewStyle().Bold(true).Foreground(colorSecondary).
            Render("━━━ Agent ━━━")
    }

    // Approve pill
    var approvePill string
    if m.approvalMode == ModeAuto {
        approvePill = lipgloss.NewStyle().Bold(true).Foreground(colorWarning).
            Render("━━━ Auto ━━━")
    } else {
        approvePill = lipgloss.NewStyle().Bold(true).Foreground(colorMuted).
            Render("━━━ Ask ━━━")
    }

    // 分隔符
    separator := lipgloss.NewStyle().Foreground(colorMuted).Render(" · ")

    leftPart := modePill + separator + approvePill + " "
    leftW := lipgloss.Width(leftPart)

    // 用 ─ 填充到行尾
    remaining := m.width - leftW
    if remaining < 0 {
        remaining = 0
    }
    fill := lipgloss.NewStyle().Foreground(colorMuted).
        Render(strings.Repeat("─", remaining))

    return leftPart + fill
}
```

---

## 9. 侧栏渲染（新增 sidebar_component.go）

### 9.1 核心渲染逻辑

```go
func (s *SidebarComponent) View(state SidebarState) string {
    var sections []string

    // Logo
    sections = append(sections, "")
    sections = append(sections, s.renderLogo())
    sections = append(sections, "")

    // Model
    sections = append(sections, s.renderModelSection(state))
    sections = append(sections, "")

    // Env
    sections = append(sections, s.renderEnvSection(state))
    sections = append(sections, "")

    // Usage
    if state.TotalTokens > 0 || state.ModelContextLimit > 0 {
        sections = append(sections, s.renderUsageSection(state))
        sections = append(sections, "")
    }

    // Todo (可滚动，动态高度)
    if len(state.TodoItems) > 0 {
        sections = append(sections, s.renderTodoSection(state))
        sections = append(sections, "")
    }

    // MCP
    if len(state.MCPStatuses) > 0 {
        sections = append(sections, s.renderMCPSection(state))
        sections = append(sections, "")
    }

    content := strings.Join(sections, "\n")

    // 应用样式：左边界线 + padding
    style := lipgloss.NewStyle().
        Border(lipgloss.Border{Left: "│"}).
        BorderForeground(colorMuted).
        PaddingLeft(1).
        PaddingRight(1).
        Height(state.Height)

    return style.Width(state.Width).Render(content)
}
```

### 9.2 Todo 滚动实现

```go
func (s *SidebarComponent) renderTodoSection(state SidebarState) string {
    var lines []string
    completed, total := countTodos(state.TodoItems)
    header := fmt.Sprintf("📋 Todo (%d/%d)", completed, total)
    lines = append(lines, sidebarSectionTitleStyle.Render(header))

    // 计算可用行数
    usedLines := s.countUsedLines(state)  // 其他 section 已用行数
    available := state.Height - usedLines - 2  // 留给 todo 的行数
    if available < 3 {
        available = 3
    }

    items := state.TodoItems
    start := state.TodoScrollOffset
    if start < 0 {
        start = 0
    }
    if start > len(items) {
        start = len(items)
    }

    // 上方滚动指示
    if start > 0 {
        lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render("  ▲ "+strconv.Itoa(start)+" more"))
    }

    // 显示项
    end := start + available - 1  // -1 for scroll indicator space
    if end > len(items) {
        end = len(items)
    }
    for i := start; i < end && i < len(items); i++ {
        lines = append(lines, s.renderTodoItem(items[i]))
    }

    // 下方滚动指示
    if end < len(items) {
        lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render("  ▼ "+strconv.Itoa(len(items)-end)+" more"))
    }

    return strings.Join(lines, "\n")
}
```

### 9.3 侧栏滚动键绑定

在 `Update()` 的 key handling 中添加：

```go
case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+up"))):
    if m.showSidebar && len(m.todoStore.Items()) > 0 {
        m.sidebarScrollOffset--
        if m.sidebarScrollOffset < 0 {
            m.sidebarScrollOffset = 0
        }
    }
case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+down"))):
    if m.showSidebar && m.todoStore != nil {
        maxOffset := len(m.todoStore.Items()) - 5
        if maxOffset < 0 {
            maxOffset = 0
        }
        m.sidebarScrollOffset++
        if m.sidebarScrollOffset > maxOffset {
            m.sidebarScrollOffset = maxOffset
        }
    }
```

> **注**：`Ctrl+↑/↓` 为侧栏 todo 滚动快捷键。

---

## 10. 状态栏精简

### 10.1 宽屏状态栏（minimal）

仅保留：
- Copy notice（绿色斜体，2s 自动清除）
- Background tasks: `Bg: N running`（如有）

```go
func (m Model) renderMinimalStatusBar() string {
    var parts []string
    if m.copyNotice != "" {
        parts = append(parts, lipgloss.NewStyle().Foreground(colorSuccess).Italic(true).Render(m.copyNotice))
    }
    if m.bgRunning > 0 {
        parts = append(parts, lipgloss.NewStyle().Foreground(colorWarning).Render(fmt.Sprintf("Bg: %d running", m.bgRunning)))
    }
    if len(parts) == 0 {
        return lipgloss.NewStyle().Foreground(colorMuted).Render(strings.Repeat("─", m.width))
    }
    txt := strings.Join(parts, " │ ")
    txtW := lipgloss.Width(txt)
    fill := strings.Repeat("─", m.width-txtW-2)
    return lipgloss.NewStyle().Foreground(colorMuted).Render(fill+" "+txt)
}
```

### 10.2 窄屏状态栏（fallback）

回退当前 status bar 的大部分信息：
- Model: provider/model
- Approve: Ask/Auto
- Token usage bar
- Bg tasks
- Team count
- MCP summary

复用现有 `StatusBarComponent.View()` 但去掉 mode indicator（因为 pills 已显示）。

---

## 11. 鼠标支持调整

当前鼠标拖拽选择文本基于 viewport 坐标。两栏布局后，viewport 变窄，鼠标坐标系需要调整：

- `handleMouseClick/Drag/Release` 中的 `msg.X` 需要减去侧栏宽度（如果侧栏在右侧）
- 或者更简单：鼠标事件只作用于主 viewport 区域，侧栏不接受鼠标交互

在 `Update()` 的 mouse handling 中添加边界检查：

```go
if m.showSidebar {
    sidebarStartX := m.width - sidebarWidth
    if msg.X >= sidebarStartX {
        // 鼠标在侧栏区域，忽略（或未来用于侧栏交互）
        return m, nil
    }
}
```

---

## 12. Team Panel 位置

当前 team coordinator panel 在 viewport 下方、输入区上方。新布局中：

**方案 A**：team panel 跨全宽（在 viewport+sidebar 下方）
**方案 B**：team panel 只出现在主内容区下方

推荐 **方案 A**（跨全宽），因为 team 信息是重要的全局面板：

```
+----------------------------------------+----------------------+
|  Main viewport                         │  Sidebar             |
+----------------------------------------+----------------------+
|  Team: project (3)                                                 |
|  ● Main  ○ @backend 5s  ○ @frontend 2s                            |
+--------------------------------------------------------------------+
|  ━━━ Agent ━━━ · ━━━ Ask ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ |
|  > Input...                                                        |
+--------------------------------------------------------------------+
```

---

## 13. 实现顺序（推荐）

按依赖顺序执行，每步可独立测试：

### Phase 1: 基础架构（可独立测试）
1. **创建 `sidebar_component.go`**：SidebarComponent + SidebarState + 各 section 渲染
2. **修改 `styles.go`**：添加 sidebar 相关样式
3. **修改 `tui.go`**：
   - 新增 `showSidebar`, `sidebarScrollOffset` 字段
   - 新增 `sidebarWidth`, `minWidthForSidebar` 常量
   - 新增 `renderSidebar()` 方法
   - 修改 `View()`：实现两栏/单栏/landing 分支逻辑
   - 新增 `landingPageView()` 方法
   - 修改 `calcViewportHeight()` 适配新布局
   - 移除旧 header bar 渲染

### Phase 2: 输入区重构（依赖 Phase 1）
4. **修改 `input_views.go`**：
   - 新增 `renderModePills()`
   - 修改 `inputAreaView()`：插入 pills，移除 todo bar
   - 移除 status bar 中的 mode/model/approve/usage/todo/mcp
   - 新增 `renderMinimalStatusBar()` 和 `renderFallbackStatusBar()`

### Phase 3: 品牌统一（可并行）
5. **全局替换 "Little Jack" → "JCODE"**：
   - `internal/tui/tui.go`
   - `internal/tui/setup.go`
   - `cmd/jcode/main.go`
   - `internal/command/commands.go`
   - `internal/command/acp.go`
   - `script/install.sh`
   - `internal/prompts/system.md`
   - `internal/prompts/plan.md`
   - `internal-docs/design/autonomous_env_switching/ui.md`

### Phase 4: 交互完善（依赖 Phase 1-2）
6. **修改 `tui.go` Update()**：
   - 添加 `Ctrl+↑/↓` 侧栏 todo 滚动键绑定
   - 调整鼠标事件边界检查
   - 确保 landing → chat 过渡平滑

7. **测试场景**：
   - Landing page（无对话）
   - 第一条消息提交后切换布局
   - 宽屏两栏布局
   - 窄屏回退单栏
   - Plan/Agent 模式切换 pills 更新
   - Ask/Auto approve 切换 pills 更新
   - Todo 滚动
   - MCP 状态更新
   - Team panel 显示

---

## 14. 风险与注意事项

1. **viewport 宽度变化**：Markdown renderer (`glamour`) 的 word wrap 依赖于 viewport 宽度。侧栏出现后 viewport 变窄，需确保 `glamour.WithWordWrap()` 使用新的主内容区宽度。

2. **Team panel 全宽显示**：team panel 跨越主内容区 + 侧栏下方，需确保宽度正确。

3. **选择高亮坐标**：`applySelectionHighlight()` 中的坐标基于 viewport 宽度，需验证窄屏和宽屏下都正确。

4. **侧栏高度同步**：viewport 高度变化时（如 todo bar 消失、team panel 出现/隐藏），侧栏高度需同步重新计算。

5. **初始化顺序**：`View()` 中不能修改 Model 状态（Bubble Tea 规则），`showSidebar` 的计算应放在 `Update()` 的 `tea.WindowSizeMsg` 处理中。

---

## 15. 关键代码变更清单

| 文件 | 操作 | 变更内容 |
|------|------|---------|
| `internal/tui/sidebar_component.go` | 新增 | SidebarComponent, SidebarState, logo/model/env/usage/todo/mcp 渲染 |
| `internal/tui/styles.go` | 修改 | 添加 sidebarSectionTitleStyle, sidebarItemStyle, modePillActiveStyle, modePillInactiveStyle, landingLogoStyle 等 |
| `internal/tui/tui.go` | 大幅修改 | View() 两栏/landing 布局, renderSidebar(), landingPageView(), 移除 header, 新增字段 |
| `internal/tui/input_views.go` | 修改 | inputAreaView() 结构, 新增 renderModePills(), 移除 todo bar, 精简状态栏 |
| `internal/tui/statusbar_component.go` | 修改 | 窄屏回退逻辑，宽屏时大部分信息移到侧栏 |
| `internal/tui/selection.go` | 修改 | 鼠标事件边界检查（排除侧栏区域） |
| `internal/tui/team_view.go` | 可能修改 | Team panel 全宽渲染确认 |
| `cmd/jcode/main.go` | 修改 | CLI 描述 branding |
| `internal/command/commands.go` | 修改 | version/help branding |
| `internal/command/acp.go` | 修改 | ACP title branding |
| `internal/tui/setup.go` | 修改 | Setup wizard branding |
| `script/install.sh` | 修改 | Installer branding |
| `internal/prompts/system.md` | 修改 | AI persona name |
| `internal/prompts/plan.md` | 修改 | AI persona name |
| `internal-docs/.../ui.md` | 修改 | 设计文档更新 |
