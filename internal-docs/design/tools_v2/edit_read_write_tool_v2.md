# Edit/Read/Write Tools V2 — 完整设计

## 概述

基于 Claude Code 的 FileEditTool (25+ 项验证 + LSP 集成 + 文件历史) 和 FileReadTool (多媒体 + 多态输出)，为 jcode 设计等价的文件操作工具集。

---

## 1. Claude Code 实现深度分析

### 1.1 FileEditTool

#### 验证管线 (25+ 项检查)

```
1. Secret detection (防泄露 API 密钥)
2. File existence check
3. File size limit (1 GiB OOM prevention)
4. Encoding detection (UTF-16LE / UTF-8)
5. Staleness check (mtime + content hash vs last read)
6. Quote normalization (curly → straight quotes)
7. Replace-all logic (multi-match requires replace_all: true)
8. Line-ending preservation (CRLF vs LF)
9. settings.json protection
```

#### 执行管线

```
1. Skill discovery → 2. LSP diagnostic prepare → 3. Directory creation
4. File history backup → 5. Patch generation → 6. Disk write
7. LSP notification (didChange/didSave) → 8. VSCode notification
9. Git diff → 10. Event log → 11. Attribution tracking
```

#### Multi-Edit

```typescript
// getPatchForEdits() 支持批量编辑同一文件
edits: Array<{ old_string: string, new_string: string }>
```

### 1.2 FileReadTool

#### 输出多态

| 文件类型 | 输出形式 |
|---------|---------|
| Text | 行号 + 内容 |
| Image (PNG/JPG/WebP) | base64 image block |
| PDF | 全文或分页 PNG |
| Notebook | Cell 解析 |
| Directory | 目录列表 |
| Binary | 拒绝 + 提示 |
| Unchanged | file_unchanged (去重) |

#### 安全检查

- 设备文件拦截 (/dev/random, /dev/stdin, /proc/*/fd/0-2)
- 二进制文件检测 (magic bytes + 扩展名)
- 文件大小分级限制
- Screenshot 路径规范化 (macOS thin-space)

### 1.3 NotebookEditTool

- Jupyter JSON 精确解析
- Cell 级操作: replace/insert/delete
- 保留 cell ID、元数据、输出

---

## 2. jcode Edit Tool V2 设计

### 2.1 数据模型

```go
// EditInputV2 增强编辑参数
type EditInputV2 struct {
    FilePath   string     `json:"file_path"`
    OldString  string     `json:"old_string"`
    NewString  string     `json:"new_string"`
    ReplaceAll bool       `json:"replace_all,omitempty"`
    StartLine  int        `json:"start_line,omitempty"`
    EndLine    int        `json:"end_line,omitempty"`

    // V2 新增
    Edits      []EditOp   `json:"edits,omitempty"`  // 批量编辑（与OldString/NewString互斥）
}

// EditOp 单个编辑操作
type EditOp struct {
    OldString string `json:"old_string"`
    NewString string `json:"new_string"`
}

// EditResult 结构化编辑结果
type EditResult struct {
    FilePath    string     `json:"file_path"`
    Created     bool       `json:"created"`
    Edits       int        `json:"edits_applied"`
    Diff        string     `json:"diff"`          // Unified diff
    Conflict    *ConflictInfo `json:"conflict,omitempty"`
    BackupPath  string     `json:"backup_path,omitempty"`
}

type ConflictInfo struct {
    Detected bool   `json:"detected"`
    Message  string `json:"message"`
}
```

### 2.2 验证管线

```go
func (t *EditToolV2) validate(ctx context.Context, input *EditInputV2) error {
    // 1. 参数互斥检查
    if input.OldString != "" && len(input.Edits) > 0 {
        return fmt.Errorf("cannot use both old_string and edits[]")
    }

    // 2. 文件路径规范化
    input.FilePath = t.env.ResolvePath(input.FilePath)

    // 3. 文件大小检查 (10MB 限制)
    info, err := t.env.Exec.Stat(ctx, input.FilePath)
    if err == nil && info.Size > MaxEditFileSize {
        return fmt.Errorf("file too large: %d bytes (max %d)", info.Size, MaxEditFileSize)
    }

    // 4. 二进制检测
    if err == nil {
        if isBinary, _ := t.detectBinary(ctx, input.FilePath); isBinary {
            return fmt.Errorf("cannot edit binary file: %s", input.FilePath)
        }
    }

    // 5. 编码检测（可选，初期仅 UTF-8）
    // 未来扩展: UTF-16LE 检测

    return nil
}
```

### 2.3 冲突检测集成

```go
func (t *EditToolV2) checkConflict(ctx context.Context, path string) (*ConflictInfo, error) {
    if t.env.FileTracker == nil {
        return nil, nil // FileTracker 未启用
    }

    result, err := t.env.FileTracker.CheckConflict(path)
    if err != nil {
        return nil, err
    }

    switch result.Status {
    case ConflictNone:
        return nil, nil
    case ConflictModified:
        return &ConflictInfo{
            Detected: true,
            Message: fmt.Sprintf(
                "File modified externally since last read (old hash: %s, new hash: %s). "+
                "Re-read the file to see current content.",
                result.OldHash[:8], result.NewHash[:8]),
        }, nil
    case ConflictFileGone:
        return &ConflictInfo{
            Detected: true,
            Message:  "File was deleted since last read",
        }, nil
    }
    return nil, nil
}
```

### 2.4 Multi-Edit 原子执行

```go
func (t *EditToolV2) applyMultiEdits(ctx context.Context, path string, edits []EditOp) (*EditResult, error) {
    // 读取原始内容
    content, err := t.env.Exec.ReadFile(ctx, path)
    if err != nil {
        return nil, err
    }
    original := string(content)
    modified := original

    // 按顺序应用所有编辑
    for i, edit := range edits {
        if !strings.Contains(modified, edit.OldString) {
            // 回滚: 已应用的编辑不可逆，返回错误
            return nil, fmt.Errorf("edit %d: old_string not found in file (after %d edits applied)",
                i+1, i)
        }
        modified = strings.Replace(modified, edit.OldString, edit.NewString, 1)
    }

    // 创建备份（如果 FileTracker 启用）
    var backupPath string
    if t.env.FileTracker != nil {
        backupPath, _ = t.env.FileTracker.CreateBackup(path, content)
    }

    // 原子写入
    if err := t.env.Exec.WriteFile(ctx, path, []byte(modified), 0o644); err != nil {
        return nil, err
    }

    // 生成 unified diff
    diff := generateUnifiedDiff(original, modified, path)

    // 更新 FileTracker
    if t.env.FileTracker != nil {
        info, _ := t.env.Exec.Stat(ctx, path)
        t.env.FileTracker.TrackRead(path, []byte(modified), info.ModTime)
    }

    return &EditResult{
        FilePath:   path,
        Edits:      len(edits),
        Diff:       diff,
        BackupPath: backupPath,
    }, nil
}
```

### 2.5 Unified Diff 生成

```go
import "github.com/pmezard/go-difflib/difflib"

func generateUnifiedDiff(original, modified, filename string) string {
    diff := difflib.UnifiedDiff{
        A:        difflib.SplitLines(original),
        B:        difflib.SplitLines(modified),
        FromFile: "a/" + filename,
        ToFile:   "b/" + filename,
        Context:  3,
    }
    text, _ := difflib.GetUnifiedDiffString(diff)
    return text
}
```

### 2.6 执行主流程

```go
func (t *EditToolV2) Invoke(ctx context.Context, input string) (string, error) {
    var params EditInputV2
    if err := json.Unmarshal([]byte(input), &params); err != nil {
        return "", err
    }

    // 1. 验证
    if err := t.validate(ctx, &params); err != nil {
        return err.Error(), nil
    }

    // 2. 冲突检测
    if conflict, err := t.checkConflict(ctx, params.FilePath); err != nil {
        return "", err
    } else if conflict != nil && conflict.Detected {
        return conflict.Message, nil
    }

    // 3. 执行编辑
    var result *EditResult
    var err error

    if len(params.Edits) > 0 {
        // V2 批量编辑
        result, err = t.applyMultiEdits(ctx, params.FilePath, params.Edits)
    } else {
        // V1 单编辑（保持兼容）
        result, err = t.applySingleEdit(ctx, &params)
    }

    if err != nil {
        return err.Error(), nil
    }

    // 4. 返回结构化结果
    return t.formatResult(result), nil
}

func (t *EditToolV2) formatResult(r *EditResult) string {
    var b strings.Builder
    if r.Created {
        b.WriteString(fmt.Sprintf("Created %s\n", r.FilePath))
    } else {
        b.WriteString(fmt.Sprintf("Edited %s (%d edit(s) applied)\n", r.FilePath, r.Edits))
    }
    if r.Diff != "" {
        b.WriteString("\n")
        b.WriteString(r.Diff)
    }
    if r.BackupPath != "" {
        b.WriteString(fmt.Sprintf("\nBackup: %s", r.BackupPath))
    }
    return b.String()
}
```

---

## 3. jcode Read Tool V2 设计

### 3.1 数据模型

```go
// ReadInputV2 增强读取参数
type ReadInputV2 struct {
    FilePath string `json:"file_path"`
    Offset   int    `json:"offset,omitempty"`    // 起始行（1-indexed）
    Limit    int    `json:"limit,omitempty"`      // 读取行数

    // V2 不增加 PDF/Image/Notebook（jcode 面向开发者，不需要多媒体）
}

// ReadResult 结构化读取结果
type ReadResult struct {
    Path        string `json:"path"`
    IsDirectory bool   `json:"is_directory,omitempty"`
    Size        int64  `json:"size,omitempty"`
    Content     string `json:"content,omitempty"`
    Encoding    string `json:"encoding,omitempty"`
    TotalLines  int    `json:"total_lines,omitempty"`
    Truncated   bool   `json:"truncated,omitempty"`
    Error       string `json:"error,omitempty"`
}
```

### 3.2 增强功能

```go
func (t *ReadToolV2) Invoke(ctx context.Context, input string) (string, error) {
    var params ReadInputV2
    json.Unmarshal([]byte(input), &params)

    path := t.env.ResolvePath(params.FilePath)

    // 1. Stat 检查
    info, err := t.env.Exec.Stat(ctx, path)
    if err != nil {
        return fmt.Sprintf("Error: %s", err), nil
    }

    // 2. 目录处理
    if info.IsDir {
        return t.readDirectory(ctx, path)
    }

    // 3. 二进制检测
    if isBinary := detectBinaryByExtension(path); isBinary {
        return fmt.Sprintf("Binary file detected: %s (%d bytes). Cannot display.", path, info.Size), nil
    }

    // 4. 文件大小检查
    if info.Size > MaxReadFileSize { // 10MB
        return fmt.Sprintf("File too large: %d bytes (max %d). Use offset/limit for partial read.",
            info.Size, MaxReadFileSize), nil
    }

    // 5. 读取文件
    content, err := t.env.Exec.ReadFile(ctx, path)
    if err != nil {
        return fmt.Sprintf("Error reading file: %s", err), nil
    }

    // 6. 编码检测
    encoding := detectEncoding(content)
    if encoding != "utf-8" && encoding != "ascii" {
        return fmt.Sprintf("File encoding: %s. Content may not display correctly.", encoding), nil
    }

    // 7. FileTracker 记录
    if t.env.FileTracker != nil {
        t.env.FileTracker.TrackRead(path, content, info.ModTime)
    }

    // 8. 行范围截取
    text := string(content)
    lines := strings.Split(text, "\n")
    totalLines := len(lines)

    offset := params.Offset
    limit := params.Limit
    if offset <= 0 { offset = 1 }
    if limit <= 0 { limit = 2000 } // 默认限制

    start := offset - 1 // 转为 0-indexed
    if start >= len(lines) { start = len(lines) - 1 }
    end := start + limit
    if end > len(lines) { end = len(lines) }
    truncated := end < len(lines)

    // 9. 添加行号
    var b strings.Builder
    for i := start; i < end; i++ {
        fmt.Fprintf(&b, "%4d │ %s\n", i+1, lines[i])
    }
    if truncated {
        fmt.Fprintf(&b, "\n... (%d more lines, total %d)", len(lines)-end, totalLines)
    }

    return b.String(), nil
}
```

### 3.3 二进制检测

```go
// 常见二进制文件扩展名
var binaryExtensions = map[string]bool{
    ".exe": true, ".dll": true, ".so": true, ".dylib": true,
    ".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
    ".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
    ".mp3": true, ".mp4": true, ".avi": true, ".mkv": true,
    ".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
    ".wasm": true, ".o": true, ".a": true, ".pyc": true, ".class": true,
    ".sqlite": true, ".db": true,
}

func detectBinaryByExtension(path string) bool {
    ext := strings.ToLower(filepath.Ext(path))
    return binaryExtensions[ext]
}

func detectBinaryByContent(content []byte) bool {
    // 检查前 8192 字节中的 NULL 字节
    checkLen := 8192
    if len(content) < checkLen {
        checkLen = len(content)
    }
    for i := 0; i < checkLen; i++ {
        if content[i] == 0 {
            return true
        }
    }
    return false
}
```

---

## 4. jcode Write Tool V2 设计

### 4.1 增强

```go
func (t *WriteToolV2) Invoke(ctx context.Context, input string) (string, error) {
    var params struct {
        FilePath string `json:"file_path"`
        Content  string `json:"content"`
    }
    json.Unmarshal([]byte(input), &params)

    path := t.env.ResolvePath(params.FilePath)

    // 1. 冲突检测（文件已存在时）
    if t.env.FileTracker != nil {
        if conflict, _ := t.env.FileTracker.CheckConflict(path); conflict != nil && conflict.Detected {
            return fmt.Sprintf("Conflict: %s. Re-read the file first.", conflict.Message), nil
        }
    }

    // 2. 文件大小检查
    if len(params.Content) > MaxWriteFileSize { // 10MB
        return fmt.Sprintf("Content too large: %d bytes (max %d)", len(params.Content), MaxWriteFileSize), nil
    }

    // 3. 备份（如果文件已存在）
    existingContent, err := t.env.Exec.ReadFile(ctx, path)
    if err == nil && t.env.FileTracker != nil {
        t.env.FileTracker.CreateBackup(path, existingContent)
    }

    // 4. 写入
    if err := t.env.Exec.WriteFile(ctx, path, []byte(params.Content), 0o644); err != nil {
        return fmt.Sprintf("Error writing file: %s", err), nil
    }

    // 5. 更新 FileTracker
    if t.env.FileTracker != nil {
        info, _ := t.env.Exec.Stat(ctx, path)
        if info != nil {
            t.env.FileTracker.TrackRead(path, []byte(params.Content), info.ModTime)
        }
    }

    // 6. 生成 diff（如果是覆写）
    if existingContent != nil {
        diff := generateUnifiedDiff(string(existingContent), params.Content, path)
        return fmt.Sprintf("Written to %s (%d bytes)\n\n%s", path, len(params.Content), diff), nil
    }
    return fmt.Sprintf("Created %s (%d bytes)", path, len(params.Content)), nil
}
```

---

## 5. 对比矩阵

| 维度 | Claude Code | jcode V1 | jcode V2 (设计) |
|------|-------------|----------|-----------------|
| **编辑策略** | 结构化 diff + patch | 字符串替换 | 字符串替换 + unified diff |
| **多编辑** | 支持 (getPatchForEdits) | 不支持 | 支持 (edits[] 参数) |
| **冲突检测** | mtime + content hash | 无 | mtime + MD5 双重校验 |
| **文件历史** | SHA256 内容寻址备份 | 无 | SHA256 前缀备份 |
| **引号处理** | 保留排版样式 | 仅提示 | 提示 + 最长公共子串 |
| **编码感知** | UTF-8/UTF-16 | 无 | UTF-8 + 二进制检测 |
| **文件大小** | 1 GiB | 无限制 | 10 MB |
| **二进制检测** | magic bytes + 扩展名 | 无 | 扩展名 + NULL 检测 |
| **diff 输出** | 结构化 patch + git diff | 无 | unified diff |
| **LSP 集成** | didChange/didSave | 无 | 无 (不在 V2 范围) |
| **图像支持** | PNG/JPG/GIF/WebP | 无 | 无 (开发者工具不需要) |
| **PDF 支持** | 全文/分页 PNG | 无 | 无 |
| **Notebook** | 专用工具 | 无 | 无 |
| **远程能力** | 无 | SSH executor | SSH executor (保留) |

---

## 6. 常量定义

```go
const (
    MaxEditFileSize  = 10 << 20  // 10 MB
    MaxReadFileSize  = 10 << 20  // 10 MB
    MaxWriteFileSize = 10 << 20  // 10 MB
    DefaultReadLimit = 2000      // 默认读取行数
    MaxReadLimit     = 10000     // 最大读取行数
    DiffContextLines = 3         // diff 上下文行数
)
```
