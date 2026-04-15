# Todo Tool V2 — 完整设计

## 概述

基于 Claude Code 的任务管理系统（7种TaskType + 4层监视 + 磁盘持久化 + 依赖关系 + 代理总结），为 jcode 设计等价的 Todo 工具升级方案。

---

## 1. Claude Code 实现深度分析

### 1.1 任务系统

**7种任务类型**: code, test, review, debug, deploy, document, custom

**完整生命周期**: pending → in_progress → completed/failed/skipped

### 1.2 四层监视

| 层级 | 机制 | 用途 |
|------|------|------|
| 文件系统 | fs.watch | 外部文件变更触发 |
| 进程内 | onTasksUpdated 回调 | 实时同步 |
| 备用轮询 | 5s 间隔 | 容错 |
| 防抖 | 50ms 窗口 | 合并高频更新 |

### 1.3 任务列表观察器

- 外部任务自动认领
- 依赖关系 (blockedBy 必须全部完成才能推进)
- 防竞速条件 (optimistic lock)

### 1.4 TodoWriteTool

```typescript
// Input
{
  todos: Array<{
    id?: string
    content: string
    status: 'pending' | 'in_progress' | 'completed' | 'skipped'
    summary?: string
  }>
}

// Output — 包含 before/after 快照
{
  oldTodos: TodoList
  newTodos: TodoList
  verificationNudgeNeeded?: boolean  // 关闭3+项时触发验证 subagent
}
```

### 1.5 代理总结

- 30s 间隔后台周期总结
- 基于 token 阈值触发（15k input / 10k delta / 5 tool calls）
- Markdown 格式，含 frontmatter

---

## 2. jcode 当前实现分析

### 2.1 现有 TodoStore (internal/tools/todo.go)

```go
type TodoStore struct {
    items []TodoItem
    mu    sync.RWMutex
}

type TodoItem struct {
    ID     string `json:"id"`
    Title  string `json:"title"`
    Status string `json:"status"` // not_started, in_progress, completed
}
```

**特点**:
- 全量替换语义 (Update 替换整个列表)
- 并发安全
- 最多1个 in_progress
- 无持久化
- 无依赖关系

### 2.2 完成守护 (runner.go)

- 最多3次重试
- 注入 IncompleteSummary 提醒
- 无主动监视

---

## 3. jcode Todo Tool V2 设计

### 3.1 数据模型

```go
// TodoItemV2 增强待办项
type TodoItemV2 struct {
    ID        string   `json:"id"`
    Title     string   `json:"title"`
    Status    string   `json:"status"`                // not_started, in_progress, completed, skipped
    BlockedBy []string `json:"blocked_by,omitempty"`   // 依赖的 todo ID
    Summary   string   `json:"summary,omitempty"`      // 完成摘要
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

// TodoAction 操作类型
type TodoAction string
const (
    TodoActionUpdate TodoAction = "update"  // 全量替换（V1兼容）
    TodoActionAdd    TodoAction = "add"     // 增量添加
    TodoActionModify TodoAction = "modify"  // 修改单项
    TodoActionRemove TodoAction = "remove"  // 删除单项
    TodoActionRead   TodoAction = "read"    // 读取当前状态
)

// TodoInputV2 增强输入
type TodoInputV2 struct {
    Action TodoAction   `json:"action,omitempty"` // 默认 update
    Items  []TodoItemV2 `json:"items,omitempty"`  // update/add 的项
    ID     string       `json:"id,omitempty"`     // modify/remove 的目标
    Status string       `json:"status,omitempty"` // modify 的新状态
    Title  string       `json:"title,omitempty"`  // modify 的新标题

    // V1 兼容
    Todos []TodoItem `json:"todos,omitempty"`
}

// TodoResultV2 操作结果
type TodoResultV2 struct {
    Action   TodoAction   `json:"action"`
    OldItems []TodoItemV2 `json:"old_items,omitempty"`
    NewItems []TodoItemV2 `json:"new_items"`
    Changed  int          `json:"changed"`
}
```

### 3.2 TodoStoreV2

```go
// TodoStoreV2 增强待办存储
type TodoStoreV2 struct {
    items      []TodoItemV2
    sessionID  string
    storage    *StorageManager
    mu         sync.RWMutex
    dirty      bool
    onChange   func([]TodoItemV2)  // 变更回调（通知TUI）
}

func NewTodoStoreV2(sessionID string, storage *StorageManager) *TodoStoreV2 {
    ts := &TodoStoreV2{
        sessionID: sessionID,
        storage:   storage,
    }
    // 尝试从磁盘恢复
    ts.Load()
    return ts
}

// Update 全量替换（V1兼容）
func (ts *TodoStoreV2) Update(items []TodoItemV2) error {
    ts.mu.Lock()
    defer ts.mu.Unlock()

    // 验证
    if err := ts.validate(items); err != nil {
        return err
    }

    old := ts.items
    ts.items = items
    ts.dirty = true
    ts.notifyChange()
    ts.saveAsync()

    config.Logger().Printf("Todo update: %d → %d items", len(old), len(items))
    return nil
}

// Add 增量添加
func (ts *TodoStoreV2) Add(items []TodoItemV2) error {
    ts.mu.Lock()
    defer ts.mu.Unlock()

    // 检查 ID 冲突
    existing := make(map[string]bool)
    for _, item := range ts.items {
        existing[item.ID] = true
    }
    for _, item := range items {
        if existing[item.ID] {
            return fmt.Errorf("duplicate todo ID: %s", item.ID)
        }
        item.CreatedAt = time.Now()
        item.UpdatedAt = time.Now()
        ts.items = append(ts.items, item)
    }

    ts.dirty = true
    ts.notifyChange()
    ts.saveAsync()
    return nil
}

// Modify 修改单项
func (ts *TodoStoreV2) Modify(id, status, title string) error {
    ts.mu.Lock()
    defer ts.mu.Unlock()

    for i, item := range ts.items {
        if item.ID == id {
            // 依赖检查
            if status == "in_progress" {
                if err := ts.checkDependencies(item); err != nil {
                    return err
                }
            }
            // in_progress 排他
            if status == "in_progress" {
                for j, other := range ts.items {
                    if other.Status == "in_progress" && other.ID != id {
                        ts.items[j].Status = "not_started"
                    }
                }
            }

            if status != "" { ts.items[i].Status = status }
            if title != "" { ts.items[i].Title = title }
            ts.items[i].UpdatedAt = time.Now()

            ts.dirty = true
            ts.notifyChange()
            ts.saveAsync()
            return nil
        }
    }
    return fmt.Errorf("todo not found: %s", id)
}

// Remove 删除单项
func (ts *TodoStoreV2) Remove(id string) error {
    ts.mu.Lock()
    defer ts.mu.Unlock()

    for i, item := range ts.items {
        if item.ID == id {
            // 检查是否被其他 todo 依赖
            for _, other := range ts.items {
                for _, dep := range other.BlockedBy {
                    if dep == id {
                        return fmt.Errorf("cannot remove %s: blocked by %s", id, other.ID)
                    }
                }
            }
            ts.items = append(ts.items[:i], ts.items[i+1:]...)
            ts.dirty = true
            ts.notifyChange()
            ts.saveAsync()
            return nil
        }
    }
    return fmt.Errorf("todo not found: %s", id)
}
```

### 3.3 依赖验证

```go
// checkDependencies 检查 todo 的依赖是否都已完成
func (ts *TodoStoreV2) checkDependencies(item TodoItemV2) error {
    if len(item.BlockedBy) == 0 {
        return nil
    }

    for _, depID := range item.BlockedBy {
        dep := ts.findItem(depID)
        if dep == nil {
            return fmt.Errorf("dependency %s not found", depID)
        }
        if dep.Status != "completed" {
            return fmt.Errorf("cannot start %s: blocked by %s (status: %s)",
                item.ID, depID, dep.Status)
        }
    }
    return nil
}

// validate 验证 todo 列表
func (ts *TodoStoreV2) validate(items []TodoItemV2) error {
    // 检查重复 ID
    ids := make(map[string]bool)
    for _, item := range items {
        if ids[item.ID] {
            return fmt.Errorf("duplicate todo ID: %s", item.ID)
        }
        ids[item.ID] = true
    }

    // 检查最多1个 in_progress
    inProgress := 0
    for _, item := range items {
        if item.Status == "in_progress" {
            inProgress++
        }
    }
    if inProgress > 1 {
        return fmt.Errorf("at most 1 todo can be in_progress, got %d", inProgress)
    }

    // 验证依赖引用存在
    for _, item := range items {
        for _, dep := range item.BlockedBy {
            if !ids[dep] {
                return fmt.Errorf("todo %s depends on %s which does not exist", item.ID, dep)
            }
        }
    }

    // 检测循环依赖
    if hasCycle(items) {
        return fmt.Errorf("circular dependency detected")
    }

    return nil
}

// hasCycle 使用拓扑排序检测循环依赖
func hasCycle(items []TodoItemV2) bool {
    // 构建邻接表
    graph := make(map[string][]string)
    inDegree := make(map[string]int)
    for _, item := range items {
        graph[item.ID] = nil
        inDegree[item.ID] = 0
    }
    for _, item := range items {
        for _, dep := range item.BlockedBy {
            graph[dep] = append(graph[dep], item.ID)
            inDegree[item.ID]++
        }
    }

    // Kahn's algorithm
    queue := make([]string, 0)
    for id, deg := range inDegree {
        if deg == 0 { queue = append(queue, id) }
    }
    visited := 0
    for len(queue) > 0 {
        node := queue[0]
        queue = queue[1:]
        visited++
        for _, next := range graph[node] {
            inDegree[next]--
            if inDegree[next] == 0 {
                queue = append(queue, next)
            }
        }
    }
    return visited != len(items)
}
```

### 3.4 磁盘持久化

```go
// TodoFileFormat 磁盘存储格式
type TodoFileFormat struct {
    Version   int          `json:"version"`    // 格式版本，当前 2
    SessionID string       `json:"session_id"`
    UpdatedAt time.Time    `json:"updated_at"`
    Items     []TodoItemV2 `json:"items"`
}

func (ts *TodoStoreV2) filePath() string {
    return filepath.Join(ts.storage.TodosDir(), ts.sessionID+".json")
}

// Load 从磁盘恢复
func (ts *TodoStoreV2) Load() error {
    path := ts.filePath()
    data, err := os.ReadFile(path)
    if os.IsNotExist(err) { return nil }
    if err != nil { return err }

    var file TodoFileFormat
    if err := json.Unmarshal(data, &file); err != nil {
        config.Logger().Printf("Failed to parse todo file: %v", err)
        return nil // 忽略损坏的文件
    }

    ts.mu.Lock()
    ts.items = file.Items
    ts.dirty = false
    ts.mu.Unlock()

    config.Logger().Printf("Loaded %d todos from %s", len(file.Items), path)
    return nil
}

// saveAsync 异步保存到磁盘
func (ts *TodoStoreV2) saveAsync() {
    if ts.storage == nil { return }

    file := TodoFileFormat{
        Version:   2,
        SessionID: ts.sessionID,
        UpdatedAt: time.Now(),
        Items:     ts.items,
    }
    data, _ := json.MarshalIndent(file, "", "  ")
    ts.storage.WriteAsync(ts.filePath(), data, 0o600)
    ts.dirty = false
}

// SaveSync 同步保存（进程退出时调用）
func (ts *TodoStoreV2) SaveSync() error {
    ts.mu.RLock()
    defer ts.mu.RUnlock()

    if !ts.dirty { return nil }

    file := TodoFileFormat{
        Version:   2,
        SessionID: ts.sessionID,
        UpdatedAt: time.Now(),
        Items:     ts.items,
    }
    data, _ := json.MarshalIndent(file, "", "  ")
    return ts.storage.Write(ts.filePath(), data, 0o600)
}

func (ts *TodoStoreV2) notifyChange() {
    if ts.onChange != nil {
        ts.onChange(ts.items)
    }
}
```

### 3.5 工具执行逻辑

```go
func (t *TodoToolV2) Invoke(ctx context.Context, input string) (string, error) {
    var params TodoInputV2
    json.Unmarshal([]byte(input), &params)

    // V1 兼容
    if len(params.Todos) > 0 && params.Action == "" {
        params.Action = TodoActionUpdate
        for _, item := range params.Todos {
            params.Items = append(params.Items, TodoItemV2{
                ID: item.ID, Title: item.Title, Status: item.Status,
            })
        }
    }

    if params.Action == "" {
        params.Action = TodoActionUpdate
    }

    store := t.env.TodoStoreV2
    old := store.Snapshot()

    switch params.Action {
    case TodoActionUpdate:
        if err := store.Update(params.Items); err != nil {
            return err.Error(), nil
        }
    case TodoActionAdd:
        if err := store.Add(params.Items); err != nil {
            return err.Error(), nil
        }
    case TodoActionModify:
        if err := store.Modify(params.ID, params.Status, params.Title); err != nil {
            return err.Error(), nil
        }
    case TodoActionRemove:
        if err := store.Remove(params.ID); err != nil {
            return err.Error(), nil
        }
    case TodoActionRead:
        return store.Summary(), nil
    default:
        return fmt.Sprintf("Unknown action: %s", params.Action), nil
    }

    return t.formatResult(params.Action, old, store.Snapshot()), nil
}

func (t *TodoToolV2) formatResult(action TodoAction, old, new []TodoItemV2) string {
    var b strings.Builder

    switch action {
    case TodoActionUpdate:
        fmt.Fprintf(&b, "Todo list updated: %d items\n", len(new))
    case TodoActionAdd:
        fmt.Fprintf(&b, "Added %d todo(s)\n", len(new)-len(old))
    case TodoActionModify:
        b.WriteString("Todo modified\n")
    case TodoActionRemove:
        fmt.Fprintf(&b, "Removed todo, %d remaining\n", len(new))
    }

    // 摘要
    counts := map[string]int{}
    for _, item := range new {
        counts[item.Status]++
    }
    parts := []string{}
    if n := counts["completed"]; n > 0 { parts = append(parts, fmt.Sprintf("✓ %d completed", n)) }
    if n := counts["in_progress"]; n > 0 { parts = append(parts, fmt.Sprintf("● %d in progress", n)) }
    if n := counts["not_started"]; n > 0 { parts = append(parts, fmt.Sprintf("○ %d pending", n)) }
    if n := counts["skipped"]; n > 0 { parts = append(parts, fmt.Sprintf("- %d skipped", n)) }
    b.WriteString(strings.Join(parts, " | "))

    return b.String()
}
```

### 3.6 TUI Todo 状态栏

```go
// TodoStatusBar 状态栏组件
func renderTodoBar(items []TodoItemV2, width int) string {
    if len(items) == 0 { return "" }

    completed := 0
    total := len(items)
    for _, item := range items {
        if item.Status == "completed" || item.Status == "skipped" {
            completed++
        }
    }

    // 进度条
    pct := float64(completed) / float64(total) * 100
    barWidth := 20
    filled := int(pct / 100 * float64(barWidth))

    bar := styles.Success.Render(strings.Repeat("█", filled)) +
        styles.Muted.Render(strings.Repeat("░", barWidth-filled))

    // 当前任务
    var current string
    for _, item := range items {
        if item.Status == "in_progress" {
            current = " → " + item.Title
            break
        }
    }

    return fmt.Sprintf("📋 %s %d/%d (%.0f%%)%s",
        bar, completed, total, pct, current)
}
```

---

## 4. 对比矩阵

| 维度 | Claude Code | jcode V1 | jcode V2 (设计) |
|------|-------------|----------|-----------------|
| **存储** | 磁盘持久化 | 纯内存 | 内存 + JSON 磁盘 |
| **操作** | 全量替换 | 全量替换 | update/add/modify/remove/read |
| **状态** | pending/in_progress/completed/skipped | 3种 | 4种 (+ skipped) |
| **依赖** | blockedBy | 无 | blockedBy + 循环检测 |
| **持久化格式** | 嵌入 session JSONL | 无 | 独立 JSON 文件 |
| **版本号** | 无 | 无 | format version 2 |
| **监视** | 4层主动监视 | 被动 (完成时检查) | onChange 回调通知 |
| **验证** | schema 验证 | ID 唯一 + 状态有效 | + 依赖验证 + 循环检测 |
| **自动认领** | 外部任务自动拾取 | 无 | 无 (scope out) |
| **代理总结** | 30s 周期总结 | 无 | 无 (scope out) |
| **验证 nudge** | 关闭3+项触发 subagent | 无 | 无 (scope out) |
| **V1 兼容** | N/A | 当前版本 | 完全兼容 |
| **会话恢复** | 自动 | session 中记录快照 | 磁盘 + session 快照 |

---

## 5. Session 集成

```go
// 在 session/session.go 中扩展

// RecordTodoSnapshot V2 — 记录增强快照
type TodoSnapshotItemV2 struct {
    ID        string   `json:"id"`
    Title     string   `json:"title"`
    Status    string   `json:"status"`
    BlockedBy []string `json:"blocked_by,omitempty"`
}

func (r *Recorder) RecordTodoSnapshotV2(items []TodoItemV2) {
    snapItems := make([]TodoSnapshotItemV2, len(items))
    for i, item := range items {
        snapItems[i] = TodoSnapshotItemV2{
            ID: item.ID, Title: item.Title, Status: item.Status, BlockedBy: item.BlockedBy,
        }
    }
    r.writeEntry(Entry{
        Type:  "todo_snapshot",
        Todos: snapItems,
    })
}
```
