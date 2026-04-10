# Grep 工具 V2 设计

## 1. 扩展参数

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

## 2. Glob 工具

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

## 3. Ripgrep 参数映射

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
