# Ask User Tool V2 — 完整设计

## 概述

基于 Claude Code 的 AskUserQuestionTool 多问题 + 选项描述 + 多选 + 注解系统，为 jcode 设计等价的交互式用户问答工具。

---

## 1. Claude Code 实现深度分析

### 1.1 输入 Schema

```typescript
{
  questions: Array<{              // 1-4 个问题
    question: string              // 清晰的问题，以 "?" 结尾
    header: string                // ≤16 字符标签（用于 chip 显示）
    options: Array<{              // 2-4 个选项
      label: string               // 1-5 词，简洁
      description: string         // 权衡说明
      preview?: string            // 可选 Markdown/HTML 预览
    }>
    multiSelect?: boolean         // 多选模式
  }>
  annotations?: Record<string, {  // 代码注解
    preview?: string
    notes?: string
  }>
}
```

### 1.2 输出 Schema

```typescript
{
  questions: Question[]           // 原始问题
  answers: Record<string, string> // question_text → answer_text
  annotations?: Annotations       // 用户注解
}
```

### 1.3 关键特性

| 特性 | 实现 |
|------|------|
| **批量提问** | 1-4 个问题一次性展示 |
| **选项描述** | 每个选项带权衡说明 |
| **预览** | Markdown/HTML 安全渲染 |
| **多选** | 复选框 UI，逗号分隔答案 |
| **延迟加载** | `shouldDefer: true`，通过 ToolSearch 发现 |
| **交互阻断** | Kairos channels 活跃时禁用 |

---

## 2. jcode 当前实现分析

### 2.1 现有结构 (internal/tools/ask_user.go)

```go
// AskUserInput 当前参数
type AskUserInput struct {
    Question string   `json:"question"`
    Options  []string `json:"options"`
}
```

### 2.2 现有交互流程

```
Agent → AskUserTool.Invoke()
  → askUserCh <- AskUserQuestionMsg{Question, Options}
  → TUI 显示: 问题 + 选项列表 + 自由文本
  → 用户选择/输入
  → askUserResponseCh <- AskUserResponse{Answer}
  → 返回 answer string
```

### 2.3 局限

- 一次只能问一个问题
- 选项无描述
- 不支持多选
- 无预览功能
- 无输入验证

---

## 3. jcode Ask User V2 设计

### 3.1 数据模型

```go
// AskUserOptionV2 增强选项
type AskUserOptionV2 struct {
    Label       string `json:"label"`                  // 选项标签（必填）
    Description string `json:"description,omitempty"`   // 权衡描述
}

// AskUserQuestion 单个问题
type AskUserQuestion struct {
    Question    string             `json:"question"`              // 问题文本
    Header      string             `json:"header,omitempty"`      // 短标签 ≤16字符
    Options     []AskUserOptionV2  `json:"options,omitempty"`     // 2-4个选项
    MultiSelect bool               `json:"multi_select,omitempty"` // 多选模式
}

// AskUserInputV2 批量提问输入
type AskUserInputV2 struct {
    // V2 批量模式
    Questions []AskUserQuestion `json:"questions,omitempty"` // 1-4个问题

    // V1 兼容字段（向后兼容）
    Question string   `json:"question,omitempty"`
    Options  []string `json:"options,omitempty"`
}

// AskUserAnswer 单个回答
type AskUserAnswer struct {
    QuestionHeader string   `json:"question_header"`
    Answer         string   `json:"answer"`
    Selected       []string `json:"selected,omitempty"` // 多选结果
}

// AskUserResponseV2 结构化响应
type AskUserResponseV2 struct {
    Answers []AskUserAnswer `json:"answers"`
}
```

### 3.2 工具 Schema

```go
func (t *AskUserToolV2) Schema() *schema.ParamsOneOf {
    return &schema.ParamsOneOf{
        Type: "object",
        Properties: map[string]*schema.ParamsOneOf{
            "questions": {
                Type: "array",
                Desc: "1-4 questions to ask the user. Each question has text, options, and optional multi-select.",
                Items: &schema.ParamsOneOf{
                    Type: "object",
                    Properties: map[string]*schema.ParamsOneOf{
                        "question": {Type: "string", Desc: "Clear question ending with '?'"},
                        "header":   {Type: "string", Desc: "Short label ≤16 chars for chip display"},
                        "options": {
                            Type: "array",
                            Desc: "2-4 selectable options",
                            Items: &schema.ParamsOneOf{
                                Type: "object",
                                Properties: map[string]*schema.ParamsOneOf{
                                    "label":       {Type: "string", Desc: "Option text, 1-5 words"},
                                    "description": {Type: "string", Desc: "Why choose this option"},
                                },
                                Required: []string{"label"},
                            },
                        },
                        "multi_select": {Type: "boolean", Desc: "Allow multiple selections"},
                    },
                    Required: []string{"question"},
                },
            },
            // V1 兼容
            "question": {Type: "string", Desc: "Single question (V1 compatibility)"},
            "options":  {Type: "array", Desc: "Simple string options (V1 compatibility)"},
        },
    }
}
```

### 3.3 执行逻辑

```go
func (t *AskUserToolV2) Invoke(ctx context.Context, input string) (string, error) {
    var params AskUserInputV2
    if err := json.Unmarshal([]byte(input), &params); err != nil {
        return "", err
    }

    // V1 兼容: 将单问题转换为 V2 格式
    questions := params.Questions
    if len(questions) == 0 && params.Question != "" {
        q := AskUserQuestion{
            Question: params.Question,
            Header:   "Question",
        }
        for _, opt := range params.Options {
            q.Options = append(q.Options, AskUserOptionV2{Label: opt})
        }
        questions = []AskUserQuestion{q}
    }

    // 验证
    if len(questions) == 0 {
        return "", fmt.Errorf("at least one question is required")
    }
    if len(questions) > 4 {
        return "", fmt.Errorf("at most 4 questions allowed, got %d", len(questions))
    }
    for i, q := range questions {
        if q.Question == "" {
            return "", fmt.Errorf("question %d has empty text", i+1)
        }
        if len(q.Options) > 0 && len(q.Options) < 2 {
            return "", fmt.Errorf("question %d: need at least 2 options", i+1)
        }
        if len(q.Options) > 4 {
            return "", fmt.Errorf("question %d: at most 4 options allowed", i+1)
        }
        if len(q.Header) > 16 {
            questions[i].Header = q.Header[:16]
        }
    }

    // 发送到 TUI
    respCh := make(chan AskUserResponseV2, 1)
    t.env.AskUserCh <- AskUserQuestionV2Msg{
        Questions: questions,
        RespCh:    respCh,
    }

    // 等待用户响应
    select {
    case <-ctx.Done():
        return "", ctx.Err()
    case resp := <-respCh:
        return t.formatResponse(resp)
    }
}

func (t *AskUserToolV2) formatResponse(resp AskUserResponseV2) (string, error) {
    if len(resp.Answers) == 1 {
        // 单问题简化输出
        return resp.Answers[0].Answer, nil
    }
    // 多问题结构化输出
    result, _ := json.Marshal(resp)
    return string(result), nil
}
```

### 3.4 TUI 渲染

#### 3.4.1 单问题 + 选项（带描述）

```
┌─────────────────────────────────────────┐
│ ❓ Which database should we use?         │
│                                          │
│  ● PostgreSQL                            │
│    Best for complex queries, ACID        │
│                                          │
│  ○ MongoDB                               │
│    Flexible schema, easy scaling         │
│                                          │
│  ○ SQLite                                │
│    Simple, embedded, zero config         │
│                                          │
│  Or type a custom answer below...        │
│  > _                                     │
│                                          │
│  [↑/↓] Navigate  [Enter] Select         │
│  [Tab] Custom input                      │
└─────────────────────────────────────────┘
```

#### 3.4.2 多选模式

```
┌─────────────────────────────────────────┐
│ ❓ Which features should we implement?   │
│                                          │
│  [✓] Authentication                      │
│      OAuth2 + session management         │
│                                          │
│  [ ] File upload                         │
│      S3-compatible storage               │
│                                          │
│  [✓] API rate limiting                   │
│      Token bucket algorithm              │
│                                          │
│  [ ] WebSocket support                   │
│      Real-time event streaming           │
│                                          │
│  [Space] Toggle  [Enter] Confirm         │
│  Selected: 2/4                           │
└─────────────────────────────────────────┘
```

#### 3.4.3 批量问题

```
┌─────────────────────────────────────────┐
│ Question 1/3  [DB Choice]                │
│                                          │
│ ❓ Which database should we use?         │
│  ● PostgreSQL                            │
│  ○ MongoDB                               │
│                                          │
│  [↑/↓] Navigate  [Enter] Next question  │
└─────────────────────────────────────────┘

┌─────────────────────────────────────────┐
│ Question 2/3  [Auth Method]              │
│                                          │
│ ❓ How should users authenticate?        │
│  ○ JWT tokens                            │
│  ● Session cookies                       │
│                                          │
│  [Enter] Next question  [Esc] Back       │
└─────────────────────────────────────────┘

┌─────────────────────────────────────────┐
│ Question 3/3  [Deploy Target]            │
│                                          │
│ ❓ Where will this be deployed?          │
│  > kubernetes                            │
│                                          │
│  [Enter] Submit all answers              │
└─────────────────────────────────────────┘
```

### 3.5 TUI 实现

```go
// AskUserV2View BubbleTea 组件
type AskUserV2View struct {
    questions    []AskUserQuestion
    answers      []AskUserAnswer
    currentQ     int          // 当前问题索引
    cursor       int          // 当前选项游标
    selected     map[int]bool // 多选已选集合
    customInput  textinput.Model
    inputMode    bool         // 是否在自由输入模式
}

func (v *AskUserV2View) Update(msg tea.KeyMsg) tea.Cmd {
    q := v.questions[v.currentQ]

    switch msg.String() {
    case "up", "k":
        if v.cursor > 0 { v.cursor-- }
    case "down", "j":
        if v.cursor < len(q.Options)-1 { v.cursor++ }
    case " ":
        if q.MultiSelect {
            v.selected[v.cursor] = !v.selected[v.cursor]
        }
    case "tab":
        v.inputMode = !v.inputMode
    case "enter":
        v.saveCurrentAnswer()
        if v.currentQ < len(v.questions)-1 {
            v.currentQ++
            v.cursor = 0
            v.selected = make(map[int]bool)
        } else {
            // 提交所有答案
            return v.submit()
        }
    case "esc":
        if v.currentQ > 0 {
            v.currentQ--
        }
    }
    return nil
}

func (v *AskUserV2View) saveCurrentAnswer() {
    q := v.questions[v.currentQ]
    answer := AskUserAnswer{QuestionHeader: q.Header}

    if v.inputMode && v.customInput.Value() != "" {
        answer.Answer = v.customInput.Value()
    } else if q.MultiSelect {
        var selected []string
        for i, sel := range v.selected {
            if sel { selected = append(selected, q.Options[i].Label) }
        }
        answer.Selected = selected
        answer.Answer = strings.Join(selected, ", ")
    } else if len(q.Options) > 0 {
        answer.Answer = q.Options[v.cursor].Label
    }

    if v.currentQ < len(v.answers) {
        v.answers[v.currentQ] = answer
    } else {
        v.answers = append(v.answers, answer)
    }
}

func (v *AskUserV2View) View(width int) string {
    q := v.questions[v.currentQ]
    var b strings.Builder

    // Header
    if len(v.questions) > 1 {
        b.WriteString(styles.Muted.Render(fmt.Sprintf(
            "Question %d/%d", v.currentQ+1, len(v.questions))))
        if q.Header != "" {
            b.WriteString(styles.Secondary.Render("  [" + q.Header + "]"))
        }
        b.WriteString("\n\n")
    }

    // 问题文本
    b.WriteString(styles.Warning.Render("❓ " + q.Question))
    b.WriteString("\n\n")

    // 选项
    for i, opt := range q.Options {
        if q.MultiSelect {
            if v.selected[i] {
                b.WriteString(styles.Success.Render("  [✓] "))
            } else {
                b.WriteString("  [ ] ")
            }
        } else {
            if i == v.cursor {
                b.WriteString(styles.Primary.Render("  ● "))
            } else {
                b.WriteString("  ○ ")
            }
        }
        b.WriteString(opt.Label)
        b.WriteString("\n")
        if opt.Description != "" {
            b.WriteString(styles.Muted.Render("    " + opt.Description))
            b.WriteString("\n")
        }
        b.WriteString("\n")
    }

    // 自由输入
    if v.inputMode {
        b.WriteString("  > " + v.customInput.View())
    } else {
        b.WriteString(styles.Muted.Render("  [Tab] Custom input"))
    }
    b.WriteString("\n\n")

    // 操作提示
    hints := []string{"[↑/↓] Navigate"}
    if q.MultiSelect {
        hints = append(hints, "[Space] Toggle")
        b.WriteString(styles.Muted.Render(fmt.Sprintf(
            "  Selected: %d/%d", countSelected(v.selected), len(q.Options))))
        b.WriteString("\n")
    }
    if v.currentQ < len(v.questions)-1 {
        hints = append(hints, "[Enter] Next")
    } else {
        hints = append(hints, "[Enter] Submit")
    }
    if v.currentQ > 0 {
        hints = append(hints, "[Esc] Back")
    }
    b.WriteString(styles.Muted.Render("  " + strings.Join(hints, "  ")))

    return b.String()
}
```

---

## 4. 对比矩阵

| 维度 | Claude Code | jcode V1 | jcode V2 (设计) |
|------|-------------|----------|-----------------|
| **提问数量** | 1-4 | 1 | 1-4 |
| **选项描述** | 有 (description) | 无 | 有 (description) |
| **预览功能** | Markdown/HTML | 无 | 无 (Phase 2) |
| **多选** | 支持 | 不支持 | 支持 |
| **注解系统** | 支持 | 无 | 无 (不必要) |
| **自由输入** | 支持 | 支持 | 支持 |
| **V1 兼容** | N/A | 当前版本 | 完全兼容 |
| **输入验证** | Zod schema | 无 | Go 编程验证 |
| **Header 标签** | ≤16 字符 | 无 | ≤16 字符 |
| **延迟加载** | shouldDefer | 否 | 否（保持即时可用） |

---

## 5. 向后兼容

### 5.1 V1 → V2 自动转换

```go
// V1 调用:
// {"question": "Which DB?", "options": ["PG", "Mongo"]}
//
// 自动转换为 V2:
// {"questions": [{"question": "Which DB?", "header": "Question",
//   "options": [{"label": "PG"}, {"label": "Mongo"}]}]}
```

### 5.2 V2 响应向后兼容

- 单问题时返回纯字符串（与 V1 一致）
- 多问题时返回 JSON 结构
- Agent 始终能正确解析
