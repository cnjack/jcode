# Execute 工具 V2 设计

## 1. BackgroundManager V2

```go
// internal/tools/background.go （扩展）

// BgTaskV2 增强后台任务
type BgTaskV2 struct {
    ID        string
    Command   string
    Status    BgTaskStatus
    Output    strings.Builder  // 内存缓冲
    LogFile   *os.File         // 磁盘持久化
    Started   time.Time
    Ended     time.Time
    LastWrite time.Time        // 最后输出时间（Stall 检测用）
}

// BackgroundManagerV2 增强后台管理器
type BackgroundManagerV2 struct {
    mu            sync.Mutex
    tasks         map[string]*BgTaskV2
    notifications []BgNotification
    nextID        int
    env           *Env
    notifier      BgNotifier
    stallTimeout  time.Duration   // 默认 45s
    logDir        string          // ~/.jcoding/tasks/
}

func NewBackgroundManagerV2(env *Env, logDir string) *BackgroundManagerV2

// Run 启动后台任务，输出写入磁盘
func (bm *BackgroundManagerV2) Run(ctx context.Context, command string) string

// PromoteToBackground 前台任务超时后升级为后台
func (bm *BackgroundManagerV2) PromoteToBackground(taskID string, partialOutput string) string

// startStallWatcher 启动 Stall 检测协程
func (bm *BackgroundManagerV2) startStallWatcher(task *BgTaskV2)
```

## 2. 自适应后台化

```go
// internal/tools/execute.go （扩展）

const (
    // BlockingBudgetMS 前台命令最大阻塞时间
    BlockingBudgetMS = 15_000 // 15秒

    // StallTimeoutMS 后台任务无输出超时
    StallTimeoutMS = 45_000 // 45秒
)

// ExecuteInputV2 扩展执行参数
type ExecuteInputV2 struct {
    Command    string `json:"command"`
    Timeout    int    `json:"timeout,omitempty"`
    Background bool   `json:"background,omitempty"`
    // Description 命令描述（用于 TUI 显示）
    Description string `json:"description,omitempty"`
}

// StreamingOutput 流式输出通道
type StreamingOutput struct {
    Chunk     string
    Timestamp time.Time
    Final     bool // 命令完成标记
}
```

## 3. Sleep 检测

```go
// internal/tools/sleep_detect.go

import "regexp"

var sleepPattern = regexp.MustCompile(`(?i)\bsleep\s+(\d+)`)

const DefaultSleepThreshold = 30 // 秒

// DetectSleep 检测命令中的 sleep 模式
// 返回 sleep 秒数，0 表示无 sleep
func DetectSleep(command string) int

// SleepWarning 生成 sleep 告警消息
func SleepWarning(seconds int) string
```

## 4. 数据流：Execute V2

```
Agent 调用 execute_v2(command, timeout=120s)
    │
    ▼
DetectSleep(command) → 超阈值? → 返回告警，建议改用 background
    │ 正常
    ▼
background=true? ─── 是 ──→ BgManagerV2.Run()
    │ 否                       │
    ▼                          ▼
前台执行，启动 StreamingOutput  创建 BgTaskV2 + LogFile
    │                          回写 task_id
    ├── 每 100ms → TUI 输出 chunk
    │
    ├── BlockingBudget 超时 (15s)?
    │   │ 是
    │   ▼
    │   PromoteToBackground() → 返回 task_id + partial_output
    │
    └── 命令完成？
        │ 是
        ▼
    返回完整 output

后台 Stall 检测:
    │
    ▼
startStallWatcher(task)
    │
    每 5s 检测 task.LastWrite
    │
    └── now - LastWrite > 45s?
        │ 是
        ▼
    注入 Stall 通知: "命令 bg_N 可能卡住（45s 无输出）"
    Agent 可选: 终止 / 发送输入 / 继续等待
```
