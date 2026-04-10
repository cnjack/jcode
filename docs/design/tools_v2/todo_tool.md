# Todo 工具 V2 设计

## 1. TodoStore V2 — 持久化 + 依赖

```go
// internal/tools/todo_store.go （重构）

// TodoItemV2 扩展任务项
type TodoItemV2 struct {
    ID        int        `json:"id"`
    Title     string     `json:"title"`
    Status    TodoStatus `json:"status"`
    BlockedBy []int      `json:"blocked_by,omitempty"` // 依赖的任务 ID 列表
}

// TodoStoreV2 持久化任务存储
type TodoStoreV2 struct {
    mu        sync.RWMutex
    items     []TodoItemV2
    filePath  string           // ~/.jcoding/todos/<session_id>.json
    onChange  func([]TodoItemV2)
}

// NewTodoStoreV2 创建持久化 TodoStore
func NewTodoStoreV2(sessionID string) *TodoStoreV2

// Load 从磁盘加载任务列表（启动时/resume 时调用）
func (s *TodoStoreV2) Load() error

// save 写入磁盘（每次变更后自动调用）
func (s *TodoStoreV2) save() error

// --- 增量 API ---

// AddItem 添加单个任务
func (s *TodoStoreV2) AddItem(title string, blockedBy []int) (TodoItemV2, error)

// UpdateItem 更新单个任务状态
// 验证: blocked_by 中的任务必须已完成才能设为 in_progress
func (s *TodoStoreV2) UpdateItem(id int, status TodoStatus) error

// RemoveItem 移除单个任务
func (s *TodoStoreV2) RemoveItem(id int) error

// --- 兼容 API ---

// Update 全量替换（向后兼容）
func (s *TodoStoreV2) Update(items []TodoItemV2)

// --- 查询 API ---

// Items / HasItems / HasIncomplete / Summary 同 V1

// BlockedItems 返回被阻塞的任务
func (s *TodoStoreV2) BlockedItems() []TodoItemV2

// ReadyItems 返回可执行的任务（无阻塞且为 pending）
func (s *TodoStoreV2) ReadyItems() []TodoItemV2
```

## 2. Todo 工具扩展

```go
// internal/tools/todo.go （扩展工具接口）

// TodoAction 操作类型
type TodoAction string

const (
    TodoActionUpdate TodoAction = "update"  // 兼容：全量替换
    TodoActionAdd    TodoAction = "add"     // 新增
    TodoActionModify TodoAction = "modify"  // 修改状态
    TodoActionRemove TodoAction = "remove"  // 删除
    TodoActionRead   TodoAction = "read"    // 查询
)

// TodoInputV2 统一输入
type TodoInputV2 struct {
    Action    TodoAction   `json:"action"`              // 操作类型
    Items     []TodoItemV2 `json:"items,omitempty"`     // update 兼容
    Title     string       `json:"title,omitempty"`     // add
    BlockedBy []int        `json:"blocked_by,omitempty"` // add
    ID        int          `json:"id,omitempty"`        // modify/remove
    Status    TodoStatus   `json:"status,omitempty"`    // modify
}
```

## 3. 持久化文件格式

```json
// ~/.jcoding/todos/550e8400-e29b.json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "updated_at": "2026-04-10T10:30:00Z",
  "items": [
    {
      "id": 1,
      "title": "重构 Edit 工具添加冲突检测",
      "status": "completed",
      "blocked_by": []
    },
    {
      "id": 2,
      "title": "实现多编辑支持",
      "status": "in_progress",
      "blocked_by": [1]
    },
    {
      "id": 3,
      "title": "添加编码检测",
      "status": "pending",
      "blocked_by": [1]
    }
  ]
}
```
