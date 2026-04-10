# Edit/Read/Write 工具 V2 设计

## 1. FileTracker — 冲突检测组件

```go
// internal/tools/file_tracker.go

// FileSnapshot 记录文件快照用于冲突检测
type FileSnapshot struct {
    Path       string
    ModTime    time.Time
    ContentMD5 [16]byte
    Size       int64
}

// FileTracker 管理文件快照，支持冲突检测
type FileTracker struct {
    mu        sync.RWMutex
    snapshots map[string]*FileSnapshot // path → snapshot
}

// Track 记录文件当前状态（读取或编辑后调用）
func (ft *FileTracker) Track(ctx context.Context, exec Executor, path string) error

// Check 检测文件是否被外部修改
// 返回 nil 表示未冲突，返回 ConflictError 表示冲突
func (ft *FileTracker) Check(ctx context.Context, exec Executor, path string) error

// ConflictError 冲突详情
type ConflictError struct {
    Path        string
    TrackedTime time.Time
    CurrentTime time.Time
    TrackedMD5  [16]byte
    CurrentMD5  [16]byte
}

func (e *ConflictError) Error() string
```

## 2. MultiEdit — 多编辑支持

```go
// internal/tools/edit.go （扩展 EditInput）

// EditOperation 单次编辑操作
type EditOperation struct {
    OldString string `json:"old_string"`
    NewString string `json:"new_string"`
    StartLine int    `json:"start_line,omitempty"`
    EndLine   int    `json:"end_line,omitempty"`
}

// EditInputV2 扩展编辑输入，支持单编辑和批量编辑
type EditInputV2 struct {
    FilePath   string          `json:"file_path"`
    OldString  string          `json:"old_string,omitempty"`   // 兼容模式
    NewString  string          `json:"new_string,omitempty"`   // 兼容模式
    ReplaceAll bool            `json:"replace_all,omitempty"`
    StartLine  int             `json:"start_line,omitempty"`
    EndLine    int             `json:"end_line,omitempty"`
    Edits      []EditOperation `json:"edits,omitempty"`        // 多编辑模式
}

// EditResult 编辑结果，包含 diff 摘要
type EditResult struct {
    Path         string `json:"path"`
    LinesChanged int    `json:"lines_changed"`
    Diff         string `json:"diff"`          // unified diff 格式
    Created      bool   `json:"created"`
}

// applyMultiEdits 原子应用多个编辑操作
// 从上到下排序后逐个应用，自动调整行偏移
// 任一失败则全部回滚
func applyMultiEdits(content string, edits []EditOperation) (string, string, error)

// generateUnifiedDiff 生成 unified diff 输出
func generateUnifiedDiff(path, before, after string) string
```

## 3. BinaryDetector — 二进制/编码检测

```go
// internal/tools/binary_detect.go

// FileEncoding 文件编码类型
type FileEncoding string

const (
    EncodingUTF8    FileEncoding = "utf-8"
    EncodingUTF16LE FileEncoding = "utf-16le"
    EncodingUTF16BE FileEncoding = "utf-16be"
    EncodingBinary  FileEncoding = "binary"
    EncodingUnknown FileEncoding = "unknown"
)

// DetectEncoding 检测文件编码
// 通过 BOM + 内容启发式判断
func DetectEncoding(header []byte) FileEncoding

// IsBinaryFile 判断文件是否为二进制
// 使用 magic bytes（前 512 字节）+ 扩展名联合判断
func IsBinaryFile(path string, header []byte) bool

// MaxFileSize 文件大小上限（默认 10MB）
const MaxFileSize = 10 * 1024 * 1024

// CheckFileSize 检查文件大小是否超限
func CheckFileSize(size int64) error
```

## 4. 数据流：Edit V2

```
Agent 调用 edit_v2(file_path, edits=[...])
    │
    ▼
解析输入 → 兼容路由（单编辑 or 多编辑）
    │
    ▼
FileTracker.Check(path) ── 冲突? ──→ 返回 ConflictError
    │ 无冲突
    ▼
ReadFile(path) → DetectEncoding → IsBinaryFile?
    │                                   │ 是
    │ 否                                ▼
    ▼                            返回 "binary file, cannot edit"
按顺序应用 edits（applyMultiEdits）
    │ 任一失败
    │──────────→ 全部回滚，返回错误
    │ 全部成功
    ▼
WriteFile(path, result) → FileTracker.Track(path)
    │
    ▼
generateUnifiedDiff → 返回 EditResult{diff, lines_changed}
```
