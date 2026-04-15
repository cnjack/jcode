# Grep Tool V2 — 完整设计

## 概述

基于 Claude Code 的 GrepTool (多输出模式 + 上下文行 + 分页 + 多行匹配) 和 GlobTool (独立文件查找)，为 jcode 设计等价的搜索工具集。

---

## 1. Claude Code 实现深度分析

### 1.1 GrepTool

```typescript
// Input Schema
{
  pattern: string              // 正则 (ripgrep -e)
  path?: string               // 搜索目录
  glob?: string               // 文件过滤
  output_mode?: 'content' | 'files_with_matches' | 'count'
  -B?: number                 // 上文行数
  -A?: number                 // 下文行数
  -C?: number                 // 上下文行数
  -n?: boolean                // 行号 (默认 true)
  -i?: boolean                // 大小写不敏感
  type?: string               // 文件类型 (rg --type)
  head_limit?: number         // 截断行数 (默认 250)
  offset?: number             // 跳过 N 行
  multiline?: boolean         // 多行匹配 (rg -U)
}

// Output Schema
{
  mode: string
  numFiles: number
  filenames: string[]
  content?: string
  appliedLimit?: number
  appliedOffset?: number
}
```

**关键特性**:
- 最大结果 20KB（超过持久化到磁盘）
- VCS 目录排除: .git, .svn, .hg, .bzr, .jj, .sl
- 250 行默认上限（防止上下文膨胀）
- 500 字符行长度限制（防止 minified 文件污染）
- 权限集成: 按 hook 规则排除文件

### 1.2 GlobTool

```typescript
{
  pattern: string  // glob 模式
  path?: string    // 搜索目录
}
// Output: { durationMs, numFiles, filenames[], truncated }
```

**关键特性**:
- 100 文件上限
- 相对路径输出（节省 token）
- UNC 路径安全检查

---

## 2. jcode 当前实现分析

### 2.1 现状 (internal/tools/grep.go ~300行)

```go
type GrepInput struct {
    Pattern         string `json:"pattern"`
    Path            string `json:"path"`
    Include         string `json:"include"`           // glob 过滤
    CaseInsensitive bool   `json:"case_insensitive"`
    MaxResults      int    `json:"max_results"`       // 默认50, 最大200
}
```

**执行**: 优先 ripgrep，回退 grep
- 排除: .git, node_modules, vendor, __pycache__, .venv
- SSH 支持

### 2.2 局限

- 仅一种输出模式（行内容列表）
- 无上下文行
- 无分页
- 无多行匹配
- 无独立文件查找工具
- 最大 200 结果（偏少）

---

## 3. jcode Grep Tool V2 设计

### 3.1 数据模型

```go
// GrepOutputMode 输出模式
type GrepOutputMode string
const (
    GrepModeContent GrepOutputMode = "content"             // 默认：显示匹配行
    GrepModeFiles   GrepOutputMode = "files_with_matches"  // 仅文件名
    GrepModeCount   GrepOutputMode = "count"               // 仅计数
)

// GrepInputV2 增强搜索参数
type GrepInputV2 struct {
    Pattern         string         `json:"pattern"`
    Path            string         `json:"path,omitempty"`
    Include         string         `json:"include,omitempty"`          // glob 过滤
    CaseInsensitive bool           `json:"case_insensitive,omitempty"`
    MaxResults      int            `json:"max_results,omitempty"`      // 默认250

    // V2 新增
    OutputMode      GrepOutputMode `json:"output_mode,omitempty"`     // 默认 content
    BeforeContext   int            `json:"before_context,omitempty"`  // -B：上文行数
    AfterContext    int            `json:"after_context,omitempty"`   // -A: 下文行数
    Context         int            `json:"context,omitempty"`         // -C: 上下文行数
    Offset          int            `json:"offset,omitempty"`          // 分页偏移
    Multiline       bool           `json:"multiline,omitempty"`       // 多行匹配
    FileType        string         `json:"file_type,omitempty"`       // rg --type
}

// GrepResultV2 结构化搜索结果
type GrepResultV2 struct {
    Mode       GrepOutputMode `json:"mode"`
    NumFiles   int            `json:"num_files"`
    Filenames  []string       `json:"filenames"`
    Content    string         `json:"content,omitempty"`
    MatchCount int            `json:"match_count,omitempty"`
    Truncated  bool           `json:"truncated,omitempty"`
    Offset     int            `json:"offset,omitempty"`
    Limit      int            `json:"limit,omitempty"`
}
```

### 3.2 Schema 定义

```go
func (t *GrepToolV2) Schema() *schema.ParamsOneOf {
    return &schema.ParamsOneOf{
        Type: "object",
        Properties: map[string]*schema.ParamsOneOf{
            "pattern":          {Type: "string", Desc: "Regex pattern to search for"},
            "path":             {Type: "string", Desc: "Directory to search in (default: cwd)"},
            "include":          {Type: "string", Desc: "Glob pattern to filter files (e.g. '*.go')"},
            "case_insensitive": {Type: "boolean", Desc: "Case-insensitive search"},
            "max_results":      {Type: "integer", Desc: "Max results to return (default: 250)"},
            "output_mode": {
                Type: "string",
                Desc: "Output mode: 'content' (default), 'files_with_matches', or 'count'",
            },
            "before_context": {Type: "integer", Desc: "Lines of context before each match (-B)"},
            "after_context":  {Type: "integer", Desc: "Lines of context after each match (-A)"},
            "context":        {Type: "integer", Desc: "Lines of context before and after (-C)"},
            "offset":         {Type: "integer", Desc: "Skip N lines before applying max_results"},
            "multiline":      {Type: "boolean", Desc: "Enable multiline matching (dot matches newlines)"},
            "file_type":      {Type: "string", Desc: "File type filter (e.g. 'go', 'py', 'js')"},
        },
        Required: []string{"pattern"},
    }
}
```

### 3.3 Ripgrep 参数构建

```go
func (t *GrepToolV2) buildRipgrepArgs(params *GrepInputV2) []string {
    var args []string

    // 基础参数
    args = append(args, "--no-heading", "--line-number", "--color=never")

    // 输出模式
    switch params.OutputMode {
    case GrepModeFiles:
        args = append(args, "--files-with-matches")
    case GrepModeCount:
        args = append(args, "--count")
    default:
        // content 模式是默认行为
    }

    // 大小写
    if params.CaseInsensitive {
        args = append(args, "--ignore-case")
    }

    // 上下文行
    if params.Context > 0 {
        args = append(args, fmt.Sprintf("--context=%d", params.Context))
    } else {
        if params.BeforeContext > 0 {
            args = append(args, fmt.Sprintf("--before-context=%d", params.BeforeContext))
        }
        if params.AfterContext > 0 {
            args = append(args, fmt.Sprintf("--after-context=%d", params.AfterContext))
        }
    }

    // 多行匹配
    if params.Multiline {
        args = append(args, "--multiline")
    }

    // 文件类型
    if params.FileType != "" {
        args = append(args, "--type="+params.FileType)
    }

    // 文件过滤
    if params.Include != "" {
        args = append(args, "--glob="+params.Include)
    }

    // 排除目录
    for _, exclude := range defaultExcludes {
        args = append(args, "--glob=!"+exclude)
    }

    // 行长度限制（防止 minified 文件污染）
    args = append(args, "--max-columns=500", "--max-columns-preview")

    // 最大结果数
    maxResults := params.MaxResults
    if maxResults <= 0 {
        maxResults = 250
    }
    if maxResults > 1000 {
        maxResults = 1000
    }
    // 如果有 offset，需要多获取 offset 行
    args = append(args, fmt.Sprintf("--max-count=%d", maxResults+params.Offset))

    // 模式
    args = append(args, "-e", params.Pattern)

    // 搜索路径
    searchPath := params.Path
    if searchPath == "" {
        searchPath = t.env.Pwd()
    } else {
        searchPath = t.env.ResolvePath(searchPath)
    }
    args = append(args, searchPath)

    return args
}

var defaultExcludes = []string{
    ".git", "node_modules", "vendor", "__pycache__", ".venv",
    ".svn", ".hg", ".bzr", ".jj", ".sl",
}
```

### 3.4 分页处理

```go
func (t *GrepToolV2) applyPagination(output string, params *GrepInputV2) (string, bool) {
    lines := strings.Split(output, "\n")

    offset := params.Offset
    if offset < 0 { offset = 0 }
    limit := params.MaxResults
    if limit <= 0 { limit = 250 }

    // 跳过 offset 行
    if offset >= len(lines) {
        return "", false
    }
    lines = lines[offset:]

    // 截断到 limit
    truncated := len(lines) > limit
    if truncated {
        lines = lines[:limit]
    }

    return strings.Join(lines, "\n"), truncated
}
```

### 3.5 执行主流程

```go
func (t *GrepToolV2) Invoke(ctx context.Context, input string) (string, error) {
    var params GrepInputV2
    json.Unmarshal([]byte(input), &params)

    // 1. 构建 ripgrep 命令
    args := t.buildRipgrepArgs(&params)
    cmd := "rg " + strings.Join(shellescape(args), " ")

    // 2. 执行（优先 rg，回退 grep）
    stdout, stderr, err := t.env.Exec.Exec(ctx, cmd, t.env.Pwd(), 30*time.Second)
    if err != nil && !isExitCode1(err) { // exit code 1 = no matches
        // 回退到 grep
        cmd = t.buildGrepFallback(&params)
        stdout, stderr, err = t.env.Exec.Exec(ctx, cmd, t.env.Pwd(), 30*time.Second)
    }

    if isExitCode1(err) {
        return "No matches found.", nil
    }
    if err != nil {
        return fmt.Sprintf("Search error: %s", stderr), nil
    }

    // 3. 分页处理
    content, truncated := t.applyPagination(stdout, &params)

    // 4. 构建结果
    result := GrepResultV2{
        Mode:      params.OutputMode,
        Content:   content,
        Truncated: truncated,
    }

    if result.Mode == "" {
        result.Mode = GrepModeContent
    }

    // 提取文件名
    result.Filenames = extractFilenames(content, result.Mode)
    result.NumFiles = len(result.Filenames)

    if result.Mode == GrepModeCount {
        result.MatchCount = countMatches(content)
    }

    // 5. 格式化输出
    return t.formatResult(&result), nil
}

func (t *GrepToolV2) formatResult(r *GrepResultV2) string {
    var b strings.Builder

    switch r.Mode {
    case GrepModeFiles:
        for _, f := range r.Filenames {
            b.WriteString(f + "\n")
        }
        fmt.Fprintf(&b, "\n%d files matched", r.NumFiles)

    case GrepModeCount:
        b.WriteString(r.Content)
        fmt.Fprintf(&b, "\n\nTotal: %d matches in %d files", r.MatchCount, r.NumFiles)

    default: // content
        b.WriteString(r.Content)
        if r.Truncated {
            fmt.Fprintf(&b, "\n\n... results truncated (showing %d lines, use offset for more)", r.Limit)
        }
        fmt.Fprintf(&b, "\n\n%d files matched", r.NumFiles)
    }

    return b.String()
}
```

---

## 4. Glob Tool 设计

### 4.1 数据模型

```go
// GlobInput 文件查找参数
type GlobInput struct {
    Pattern  string `json:"pattern"`             // glob 模式
    Path     string `json:"path,omitempty"`      // 搜索目录
    MaxDepth int    `json:"max_depth,omitempty"` // 最大深度
    Limit    int    `json:"limit,omitempty"`     // 结果上限（默认100）
}

// GlobResult 文件查找结果
type GlobResult struct {
    Files     []string      `json:"files"`
    NumFiles  int           `json:"num_files"`
    Truncated bool          `json:"truncated"`
    Duration  time.Duration `json:"duration_ms"`
}
```

### 4.2 执行逻辑

```go
func (t *GlobTool) Invoke(ctx context.Context, input string) (string, error) {
    var params GlobInput
    json.Unmarshal([]byte(input), &params)

    limit := params.Limit
    if limit <= 0 { limit = 100 }
    if limit > 500 { limit = 500 }

    searchPath := params.Path
    if searchPath == "" {
        searchPath = t.env.Pwd()
    } else {
        searchPath = t.env.ResolvePath(searchPath)
    }

    start := time.Now()

    // 使用 find 或 fd 执行 glob
    var files []string
    var err error

    if params.Pattern == "" {
        return "Error: pattern is required", nil
    }

    // 构建 find 命令
    cmd := t.buildFindCommand(searchPath, params.Pattern, params.MaxDepth, limit+1)
    stdout, _, err := t.env.Exec.Exec(ctx, cmd, searchPath, 30*time.Second)
    if err != nil {
        return fmt.Sprintf("Error: %s", err), nil
    }

    files = strings.Split(strings.TrimSpace(stdout), "\n")
    if files[0] == "" { files = nil }

    // 转为相对路径
    for i, f := range files {
        if rel, err := filepath.Rel(searchPath, f); err == nil {
            files[i] = rel
        }
    }

    truncated := len(files) > limit
    if truncated {
        files = files[:limit]
    }

    duration := time.Since(start)

    // 格式化
    var b strings.Builder
    for _, f := range files {
        b.WriteString(f + "\n")
    }
    fmt.Fprintf(&b, "\n%d files found", len(files))
    if truncated {
        fmt.Fprintf(&b, " (truncated at %d)", limit)
    }
    fmt.Fprintf(&b, " in %dms", duration.Milliseconds())

    return b.String(), nil
}

func (t *GlobTool) buildFindCommand(path, pattern string, maxDepth, limit int) string {
    var parts []string
    parts = append(parts, "find", shellescape1(path))

    if maxDepth > 0 {
        parts = append(parts, "-maxdepth", fmt.Sprint(maxDepth))
    }

    // 排除 VCS 目录
    for _, dir := range defaultExcludes {
        parts = append(parts, "-not", "-path", fmt.Sprintf("*/%s/*", dir))
    }

    parts = append(parts, "-name", shellescape1(pattern), "-type", "f")
    parts = append(parts, "|", "head", "-n", fmt.Sprint(limit))

    return strings.Join(parts, " ")
}
```

---

## 5. 对比矩阵

| 维度 | Claude Code | jcode V1 | jcode V2 (设计) |
|------|-------------|----------|-----------------|
| **搜索工具** | Grep + Glob + LSP + ToolSearch | 1 (grep) | 2 (grep + glob) |
| **输出模式** | content / files / count | 仅 content | content / files / count |
| **上下文行** | -A/-B/-C | 无 | -A/-B/-C |
| **分页** | head_limit + offset | 无 | max_results + offset |
| **多行匹配** | rg --multiline | 无 | rg --multiline |
| **文件类型** | rg --type | glob | rg --type + glob |
| **行长度限制** | 500 字符 | 无 | 500 字符 |
| **默认结果上限** | 250 行 | 50 | 250 行 |
| **Glob 工具** | 独立 GlobTool | 无 | 独立 GlobTool |
| **LSP 集成** | 9 种操作 | 无 | 无 (scope out) |
| **VCS 排除** | 6 种 | 5 种 | 10 种 |
| **结果持久化** | 20KB+ 磁盘 | 内存 | 50KB+ 磁盘 |
| **远程支持** | 有 | SSH | SSH (保留) |

---

## 6. 工具注册

```go
// 在 tools 包的工具注册中
func RegisterSearchTools(env *Env) []tool.InvokableTool {
    return []tool.InvokableTool{
        NewGrepToolV2(env),
        NewGlobTool(env),
    }
}
```
