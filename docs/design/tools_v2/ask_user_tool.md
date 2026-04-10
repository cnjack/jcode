# Ask User 工具 V2 设计

## 1. 扩展问题模型

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

## 2. TUI 交互流程

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
