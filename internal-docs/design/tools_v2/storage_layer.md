# Storage Layer V2 — 完整设计

## 概述

基于 Claude Code 的多层持久化架构，为 jcode 设计等价的存储系统。核心目标：从纯内存模型演进为磁盘持久化 + 内存缓存的混合架构，支持崩溃恢复、大结果持久化、文件历史追踪。

---

## 1. Claude Code 存储架构分析

### 1.1 存储层级

| 层级 | 位置 | 格式 | 用途 |
|------|------|------|------|
| Session Transcript | `~/.claude/projects/{hash}/{sessionId}.jsonl` | JSONL | 会话记录（append-only） |
| File History | `~/.claude/project-backups/{hash}@v{N}.bak` | Binary | 文件编辑历史快照 |
| Tool Results | `{projectDir}/{sessionId}/tool-results/{tool}-{hash}.json` | JSON | 大型工具输出持久化 |
| Paste Cache | `~/.claude/paste-cache/{sha256-16}.txt` | Text | 大段粘贴内容外部存储 |
| Image Cache | `~/.claude/image-cache/{sessionId}/{imageId}.{ext}` | Binary | 图片缓存 |
| OAuth Tokens | Keychain / `~/.claude/.credentials.json` | JSON | 安全凭证存储 |
| Config | `~/.claude/config.json` + 7层settings | JSON | 分层配置管理 |
| Session Memory | `~/.claude/.memory/{sessionId}.md` | Markdown | 会话摘要 |

### 1.2 关键设计特征

- **批量写入队列**: 100ms 刷新间隔，每个文件独立写入队列
- **内容寻址**: Paste/Image 使用 SHA256 Hash 作为文件名
- **崩溃安全**: process exit 时 drain 写入队列 + 重新追加元数据
- **文件权限**: 0o600 (rw-------)，目录 0o700
- **大小限制**: 50MB 读取限制防止 OOM，1GB 文件编辑上限

---

## 2. jcode 当前存储分析

### 2.1 现状

| 组件 | 存储方式 | 局限 |
|------|---------|------|
| Session | JSONL 文件 + sessions.json 索引 | 无写入缓冲，无崩溃恢复 |
| Todo | 纯内存(TodoStore) | 进程退出丢失 |
| Plan | 纯内存(PlanStore) | 无磁盘持久化 |
| Background Tasks | 纯内存(BackgroundManager) | 2000字符截断，无磁盘日志 |
| Config | JSON 文件 | 单层，无分级 |
| File History | 无 | 无冲突检测，无备份 |
| Tool Results | 内存中完整返回 | 大结果可能OOM |

### 2.2 核心差距

1. **无文件历史追踪** — 编辑不可逆
2. **无大结果持久化** — 长命令输出内存截断
3. **Todo/Plan 不持久** — 会话恢复丢失状态
4. **无写入缓冲** — 每次写入直接 I/O
5. **无凭证安全存储** — OAuth token 无持久化

---

## 3. jcode Storage V2 设计

### 3.1 目录结构

```
~/.jcoding/
├── config.json                    # 全局配置（现有）
├── debug.log                      # 调试日志（现有）
├── sessions.json                  # 会话索引（现有）
├── sessions/
│   └── {uuid}.json                # 会话 JSONL（现有）
│
├── storage/                       # ===== V2 新增 =====
│   ├── file-history/              # 文件编辑历史
│   │   └── {sha256-prefix}@v{N}.bak
│   │
│   ├── tool-results/              # 大型工具输出
│   │   └── {session-uuid}/
│   │       └── {tool}-{hash}.json
│   │
│   ├── todos/                     # Todo 持久化
│   │   └── {session-uuid}.json
│   │
│   ├── plans/                     # Plan 持久化
│   │   └── {session-uuid}.json
│   │
│   ├── tasks/                     # 后台任务日志
│   │   └── {task-id}.log
│   │
│   └── oauth/                     # OAuth 令牌
│       └── {provider}.json
```

### 3.2 核心接口

```go
// StorageManager 统一存储管理器
type StorageManager struct {
    baseDir     string        // ~/.jcoding/storage/
    sessionID   string        // 当前会话 UUID
    mu          sync.RWMutex
    writeQueue  *WriteQueue   // 异步写入队列
}

// WriteQueue 异步批量写入
type WriteQueue struct {
    entries   map[string][]WriteEntry  // path → pending writes
    flushCh   chan struct{}
    interval  time.Duration            // 默认 100ms
    mu        sync.Mutex
}

type WriteEntry struct {
    Data     []byte
    Mode     os.FileMode
    Append   bool         // true=追加, false=覆写
    Callback func(error)
}
```

### 3.3 文件历史追踪 (FileTracker)

```go
// FileTracker 追踪文件状态，检测冲突
type FileTracker struct {
    tracked  map[string]*FileState
    mu       sync.RWMutex
    storage  *StorageManager
    maxSnaps int   // 每会话最大快照数，默认100
}

type FileState struct {
    Path         string
    ContentHash  string    // MD5 of content at last read
    ModTime      time.Time // mtime at last read
    Version      int       // 单调递增版本号
    BackupPath   string    // 备份文件路径
    Encoding     string    // 检测到的编码
}

// TrackRead 在读取文件时记录状态
func (ft *FileTracker) TrackRead(path string, content []byte, modTime time.Time) {
    hash := md5Hash(content)
    ft.tracked[path] = &FileState{
        Path: path, ContentHash: hash, ModTime: modTime,
    }
}

// CheckConflict 在编辑前检测冲突
func (ft *FileTracker) CheckConflict(path string) (ConflictResult, error) {
    state, ok := ft.tracked[path]
    if !ok {
        return ConflictResult{Status: ConflictNone}, nil
    }
    // 1. 检查 mtime
    info, err := os.Stat(path)
    if err != nil {
        return ConflictResult{Status: ConflictFileGone}, nil
    }
    if info.ModTime().Equal(state.ModTime) {
        return ConflictResult{Status: ConflictNone}, nil
    }
    // 2. mtime 变了，检查内容 hash（防止 false positive）
    content, _ := os.ReadFile(path)
    currentHash := md5Hash(content)
    if currentHash == state.ContentHash {
        // 内容没变，只是 mtime 变了（touch/IDE save 等）
        return ConflictResult{Status: ConflictNone}, nil
    }
    return ConflictResult{
        Status:     ConflictModified,
        OldHash:    state.ContentHash,
        NewHash:    currentHash,
        OldModTime: state.ModTime,
        NewModTime: info.ModTime(),
    }, nil
}

// CreateBackup 创建文件备份
func (ft *FileTracker) CreateBackup(path string, content []byte) (string, error) {
    state := ft.tracked[path]
    state.Version++
    hash := sha256Prefix(path + fmt.Sprint(state.Version))
    backupPath := filepath.Join(ft.storage.FileHistoryDir(), hash+"@v"+fmt.Sprint(state.Version)+".bak")
    return backupPath, ft.storage.Write(backupPath, content, 0o600)
}

type ConflictStatus int
const (
    ConflictNone     ConflictStatus = iota
    ConflictModified                        // 外部修改
    ConflictFileGone                        // 文件已删除
)

type ConflictResult struct {
    Status     ConflictStatus
    OldHash    string
    NewHash    string
    OldModTime time.Time
    NewModTime time.Time
}
```

### 3.4 大型工具输出持久化

```go
// ToolResultStore 管理大型工具输出
type ToolResultStore struct {
    storage  *StorageManager
    maxSize  int64   // 持久化阈值，默认 50000 字符
}

// PersistIfLarge 如果输出超过阈值则持久化到磁盘
func (trs *ToolResultStore) PersistIfLarge(toolName, result string) (PersistedResult, bool) {
    if len(result) < int(trs.maxSize) {
        return PersistedResult{}, false
    }

    hash := sha256Short(result)
    filename := fmt.Sprintf("%s-%s.json", toolName, hash)
    relPath := filepath.Join(trs.storage.sessionID, "tool-results", filename)
    absPath := filepath.Join(trs.storage.ToolResultsDir(), filename)

    persisted := PersistedResult{
        FilePath:     relPath,
        OriginalSize: len(result),
        Preview:      truncatePreview(result, 500),
        HasMore:      true,
    }

    data, _ := json.Marshal(map[string]interface{}{
        "tool":    toolName,
        "result":  result,
        "size":    len(result),
    })
    trs.storage.WriteAsync(absPath, data, 0o600)
    return persisted, true
}

// Retrieve 读取持久化的工具输出
func (trs *ToolResultStore) Retrieve(filePath string) (string, error) {
    absPath := filepath.Join(trs.storage.baseDir, filePath)
    data, err := os.ReadFile(absPath)
    if err != nil { return "", err }
    var record map[string]interface{}
    json.Unmarshal(data, &record)
    return record["result"].(string), nil
}

type PersistedResult struct {
    FilePath     string `json:"filepath"`
    OriginalSize int    `json:"original_size"`
    Preview      string `json:"preview"`
    HasMore      bool   `json:"has_more"`
}
```

### 3.5 Todo 持久化

```go
// TodoStoreV2 扩展现有 TodoStore，增加磁盘持久化
type TodoStoreV2 struct {
    TodoStore                        // 嵌入现有 TodoStore
    storage    *StorageManager
    sessionID  string
    dirty      bool                  // 是否有未保存的变更
}

// TodoFileFormat 磁盘格式
type TodoFileFormat struct {
    SessionID string         `json:"session_id"`
    UpdatedAt time.Time      `json:"updated_at"`
    Items     []TodoItemV2   `json:"items"`
}

type TodoItemV2 struct {
    ID        string   `json:"id"`
    Title     string   `json:"title"`
    Status    string   `json:"status"`     // not_started, in_progress, completed
    BlockedBy []string `json:"blocked_by,omitempty"` // 依赖的 todo ID
}

func (ts *TodoStoreV2) Save() error {
    data := TodoFileFormat{
        SessionID: ts.sessionID,
        UpdatedAt: time.Now(),
        Items:     ts.toV2Items(),
    }
    bytes, _ := json.MarshalIndent(data, "", "  ")
    return ts.storage.Write(ts.filePath(), bytes, 0o600)
}

func (ts *TodoStoreV2) Load() error {
    bytes, err := os.ReadFile(ts.filePath())
    if os.IsNotExist(err) { return nil }
    if err != nil { return err }
    var data TodoFileFormat
    if err := json.Unmarshal(bytes, &data); err != nil { return err }
    ts.fromV2Items(data.Items)
    return nil
}
```

### 3.6 后台任务日志

```go
// TaskLog 后台任务磁盘日志
type TaskLog struct {
    taskID   string
    file     *os.File
    maxSize  int64    // 64MB 上限
    written  int64
}

func NewTaskLog(storage *StorageManager, taskID string) (*TaskLog, error) {
    path := filepath.Join(storage.TasksDir(), taskID+".log")
    f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
    if err != nil { return nil, err }
    return &TaskLog{taskID: taskID, file: f, maxSize: 64 << 20}, nil
}

func (tl *TaskLog) Write(data []byte) (int, error) {
    if tl.written+int64(len(data)) > tl.maxSize {
        return 0, fmt.Errorf("task log exceeded %dMB limit", tl.maxSize>>20)
    }
    n, err := tl.file.Write(data)
    tl.written += int64(n)
    return n, err
}

func (tl *TaskLog) ReadAll() (string, error) {
    path := filepath.Join(filepath.Dir(tl.file.Name()), tl.taskID+".log")
    data, err := os.ReadFile(path)
    return string(data), err
}
```

### 3.7 OAuth 令牌存储

```go
// TokenStore OAuth 令牌持久化
type TokenStore struct {
    dir string // ~/.jcoding/storage/oauth/
}

type OAuthToken struct {
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token,omitempty"`
    ExpiresAt    time.Time `json:"expires_at,omitempty"`
    Scopes       []string  `json:"scopes,omitempty"`
    Provider     string    `json:"provider"`
}

func (ts *TokenStore) Save(provider string, token OAuthToken) error {
    path := filepath.Join(ts.dir, provider+".json")
    data, _ := json.MarshalIndent(token, "", "  ")
    return os.WriteFile(path, data, 0o600)
}

func (ts *TokenStore) Get(provider string) (*OAuthToken, error) {
    path := filepath.Join(ts.dir, provider+".json")
    data, err := os.ReadFile(path)
    if os.IsNotExist(err) { return nil, nil }
    if err != nil { return nil, err }
    var token OAuthToken
    return &token, json.Unmarshal(data, &token)
}

func (ts *TokenStore) Delete(provider string) error {
    return os.Remove(filepath.Join(ts.dir, provider+".json"))
}
```

---

## 4. 对比矩阵

| 维度 | Claude Code | jcode V1 | jcode V2 (设计) |
|------|-------------|----------|-----------------|
| **会话存储** | JSONL + 100ms 刷新队列 | JSONL 同步写入 | JSONL + WriteQueue(100ms) |
| **文件历史** | SHA256 内容寻址备份 | 无 | SHA256 前缀 + 版本号备份 |
| **冲突检测** | mtime + content hash | 无 | mtime + MD5 双重校验 |
| **工具输出** | 50KB+ 磁盘持久化 | 2000字符内存截断 | 50KB+ 磁盘持久化 + preview |
| **Todo** | 7种TaskType + 4层监视 | 内存 TodoStore | 内存 + JSON 磁盘持久化 |
| **后台任务** | 64MB 磁盘日志 | 2000字符内存 | 64MB 磁盘日志 |
| **OAuth** | Keychain + 文件回退 | 无 | 加密文件存储(0o600) |
| **配置** | 7层分级 settings | 单层 config.json | 2层(全局 + 项目) |
| **崩溃恢复** | drain + 元数据重追加 | 无 | WriteQueue drain + Todo save |
| **文件权限** | 0o600 (文件) + 0o700 (目录) | 0o644 | 0o600 (敏感) + 0o644 (普通) |

---

## 5. 写入队列实现

```go
// WriteQueue 实现异步批量写入
func NewWriteQueue(interval time.Duration) *WriteQueue {
    wq := &WriteQueue{
        entries:  make(map[string][]WriteEntry),
        flushCh: make(chan struct{}, 1),
        interval: interval,
    }
    go wq.drainLoop()
    return wq
}

func (wq *WriteQueue) Enqueue(path string, entry WriteEntry) {
    wq.mu.Lock()
    wq.entries[path] = append(wq.entries[path], entry)
    wq.mu.Unlock()
    // 非阻塞通知
    select {
    case wq.flushCh <- struct{}{}:
    default:
    }
}

func (wq *WriteQueue) drainLoop() {
    timer := time.NewTicker(wq.interval)
    defer timer.Stop()
    for {
        select {
        case <-timer.C:
            wq.drain()
        case <-wq.flushCh:
            // 收到新数据，等待 interval 后批量写入
        }
    }
}

func (wq *WriteQueue) drain() {
    wq.mu.Lock()
    pending := wq.entries
    wq.entries = make(map[string][]WriteEntry)
    wq.mu.Unlock()

    for path, entries := range pending {
        for _, entry := range entries {
            var err error
            dir := filepath.Dir(path)
            os.MkdirAll(dir, 0o700)
            if entry.Append {
                f, e := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, entry.Mode)
                if e != nil { err = e } else {
                    _, err = f.Write(entry.Data)
                    f.Close()
                }
            } else {
                err = os.WriteFile(path, entry.Data, entry.Mode)
            }
            if entry.Callback != nil {
                entry.Callback(err)
            }
        }
    }
}

// DrainSync 进程退出前同步刷新
func (wq *WriteQueue) DrainSync() {
    wq.drain()
}
```

---

## 6. StorageManager 完整实现

```go
func NewStorageManager(sessionID string) (*StorageManager, error) {
    baseDir := filepath.Join(os.Getenv("HOME"), ".jcoding", "storage")
    if err := os.MkdirAll(baseDir, 0o700); err != nil {
        return nil, err
    }
    sm := &StorageManager{
        baseDir:    baseDir,
        sessionID:  sessionID,
        writeQueue: NewWriteQueue(100 * time.Millisecond),
    }
    // 确保子目录存在
    for _, sub := range []string{"file-history", "tool-results", "todos", "plans", "tasks", "oauth"} {
        os.MkdirAll(filepath.Join(baseDir, sub), 0o700)
    }
    return sm, nil
}

func (sm *StorageManager) Write(path string, data []byte, mode os.FileMode) error {
    dir := filepath.Dir(path)
    if err := os.MkdirAll(dir, 0o700); err != nil {
        return err
    }
    return os.WriteFile(path, data, mode)
}

func (sm *StorageManager) WriteAsync(path string, data []byte, mode os.FileMode) {
    sm.writeQueue.Enqueue(path, WriteEntry{
        Data: data, Mode: mode, Append: false,
    })
}

func (sm *StorageManager) AppendAsync(path string, data []byte, mode os.FileMode) {
    sm.writeQueue.Enqueue(path, WriteEntry{
        Data: data, Mode: mode, Append: true,
    })
}

func (sm *StorageManager) Close() {
    sm.writeQueue.DrainSync()
}

// 便利方法
func (sm *StorageManager) FileHistoryDir() string {
    return filepath.Join(sm.baseDir, "file-history")
}
func (sm *StorageManager) ToolResultsDir() string {
    return filepath.Join(sm.baseDir, "tool-results", sm.sessionID)
}
func (sm *StorageManager) TodosDir() string {
    return filepath.Join(sm.baseDir, "todos")
}
func (sm *StorageManager) PlansDir() string {
    return filepath.Join(sm.baseDir, "plans")
}
func (sm *StorageManager) TasksDir() string {
    return filepath.Join(sm.baseDir, "tasks")
}
func (sm *StorageManager) OAuthDir() string {
    return filepath.Join(sm.baseDir, "oauth")
}
```

---

## 7. 清理与轮换策略

| 存储 | 保留策略 | 清理触发 |
|------|---------|---------|
| 文件历史 | 每会话最多100个快照 | LRU 淘汰最旧 |
| 工具输出 | 随会话删除 | 会话清理时级联删除 |
| Todo | 最近20个会话 | 启动时扫描 + 删除过期 |
| 后台任务 | 单文件 64MB，最多50个文件 | 超限时删除最旧 |
| OAuth | Token 过期后保留 | 主动调用 Delete |

```go
// Cleanup 启动时清理过期数据
func (sm *StorageManager) Cleanup() {
    // 1. 清理过期 todo 文件（保留最近20个）
    sm.cleanupDir(sm.TodosDir(), 20)
    // 2. 清理过期任务日志（保留最近50个）
    sm.cleanupDir(sm.TasksDir(), 50)
    // 3. 文件历史不主动清理（由 FileTracker 维护）
}

func (sm *StorageManager) cleanupDir(dir string, keepN int) {
    entries, _ := os.ReadDir(dir)
    if len(entries) <= keepN { return }
    // 按修改时间排序，删除最旧的
    sort.Slice(entries, func(i, j int) bool {
        infoI, _ := entries[i].Info()
        infoJ, _ := entries[j].Info()
        return infoI.ModTime().Before(infoJ.ModTime())
    })
    for i := 0; i < len(entries)-keepN; i++ {
        os.Remove(filepath.Join(dir, entries[i].Name()))
    }
}
```
