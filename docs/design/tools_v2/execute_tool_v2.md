# Execute Tool V2 — 完整设计

## 概述

基于 Claude Code 的 BashTool (自适应后台化 + 流式进度 + Stall 检测 + Sleep 阻止)，为 jcode 设计等价的命令执行工具升级方案。

---

## 1. Claude Code 实现深度分析

### 1.1 核心特性

| 特性 | 实现细节 |
|------|---------|
| **自适应后台化** | 15s blocking budget，超时自动提升为后台任务 |
| **流式进度** | AsyncGenerator，每2s yield 进度更新 |
| **Stall 检测** | 45s 无输出增长 → 检测交互式提示 |
| **Sleep 阻止** | 正则检测 `sleep N`，N>threshold 时拒绝 |
| **命令分类** | search/read/list 命令标记为可折叠 |
| **Sed 预览** | sed 命令按 FileEdit diff 方式审批 |
| **输出管理** | EndTruncatingAccumulator，64MB 磁盘持久化 |
| **沙盒模式** | 默认沙盒，`dangerouslyDisableSandbox` 可选关闭 |

### 1.2 后台任务生命周期

```
running → completed → read
        → failed
        → killed
        → stalled (45s no output)
```

### 1.3 关键常量

```typescript
ASSISTANT_BLOCKING_BUDGET_MS = 15_000  // 15s 前台预算
STALL_TIMEOUT_MS = 45_000             // 45s stall 检测
MAX_TASK_OUTPUT = 64 * 1024 * 1024    // 64MB 磁盘日志
PROGRESS_INTERVAL_MS = 2_000          // 2s 进度间隔
```

---

## 2. jcode 当前实现分析

### 2.1 现状

```go
// ExecuteInput 当前参数
type ExecuteInput struct {
    Command    string `json:"command"`
    Timeout    int    `json:"timeout"`     // 默认120s, 最大600s
    Background bool   `json:"background"`
}

// BackgroundManager
type BackgroundManager struct {
    tasks  map[string]*BgTask  // bg_1, bg_2, ...
    nextID int
}

type BgTask struct {
    ID        string
    Command   string
    Status    string   // running, completed, failed
    Output    string   // ≤2000 字符
    Error     string
}
```

### 2.2 局限

| 问题 | 描述 |
|------|------|
| **无自适应后台化** | 必须预先指定 background=true |
| **无流式进度** | 前台阻塞等待，无中间输出 |
| **无 Stall 检测** | 卡死的命令只能等超时 |
| **无 Sleep 检测** | `sleep 3600` 会阻塞整个 agent |
| **内存输出限制** | 2000字符截断，无磁盘持久化 |
| **无命令分类** | 所有命令同一审批策略 |
| **固定超时** | 无 per-command 超时策略 |

---

## 3. jcode Execute Tool V2 设计

### 3.1 数据模型

```go
// ExecuteInputV2 增强执行参数
type ExecuteInputV2 struct {
    Command     string `json:"command"`
    Timeout     int    `json:"timeout,omitempty"`      // 默认120s
    Background  bool   `json:"background,omitempty"`
    Description string `json:"description,omitempty"`  // 用户可见的任务描述
}

// BgTaskV2 增强后台任务
type BgTaskV2 struct {
    ID          string
    Command     string
    Description string
    Status      TaskStatus
    StartTime   time.Time
    EndTime     time.Time

    // 输出管理
    buffer      *OutputBuffer  // 内存缓冲
    logFile     *TaskLog       // 磁盘日志

    // 状态监控
    lastWrite   time.Time      // 最后输出时间（Stall 检测）
    exitCode    int
    err         error

    mu          sync.Mutex
}

type TaskStatus string
const (
    TaskRunning   TaskStatus = "running"
    TaskCompleted TaskStatus = "completed"
    TaskFailed    TaskStatus = "failed"
    TaskKilled    TaskStatus = "killed"
    TaskStalled   TaskStatus = "stalled"
    TaskPromoted  TaskStatus = "promoted" // 前台→后台提升
)

// OutputBuffer 双层输出缓冲（内存 + 磁盘）
type OutputBuffer struct {
    memory    strings.Builder  // 尾部 4KB 内存缓冲（用于 progress 显示）
    memoryMax int              // 4096
    totalSize int64            // 总输出大小
    logFile   *TaskLog         // 磁盘日志
}

// StreamChunk 流式输出块
type StreamChunk struct {
    Data      string
    Timestamp time.Time
    IsStderr  bool
}
```

### 3.2 后台管理器 V2

```go
// BackgroundManagerV2 增强后台任务管理
type BackgroundManagerV2 struct {
    tasks      map[string]*BgTaskV2
    nextID     int
    storage    *StorageManager
    mu         sync.RWMutex

    // 监控配置
    blockingBudget time.Duration  // 15s 前台预算
    stallTimeout   time.Duration  // 45s stall 检测
    maxTasks       int            // 最大并发后台任务数
}

func NewBackgroundManagerV2(storage *StorageManager) *BackgroundManagerV2 {
    return &BackgroundManagerV2{
        tasks:          make(map[string]*BgTaskV2),
        storage:        storage,
        blockingBudget: 15 * time.Second,
        stallTimeout:   45 * time.Second,
        maxTasks:       20,
    }
}
```

### 3.3 自适应后台化

```go
// executeWithBudget 带时间预算的前台执行
func (t *ExecuteToolV2) executeWithBudget(
    ctx context.Context,
    cmd string,
    timeout time.Duration,
    description string,
    onProgress func(StreamChunk),
) (*ExecuteResult, error) {

    // 创建命令
    execCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    // 启动命令并获取 stdout/stderr pipes
    stdout, stderr, process, err := t.env.Exec.ExecStreaming(execCtx, cmd, t.env.Pwd())
    if err != nil {
        return nil, err
    }

    output := &OutputBuffer{memoryMax: 4096}
    stallTimer := time.NewTimer(t.bgMgr.stallTimeout)
    budgetTimer := time.NewTimer(t.bgMgr.blockingBudget)
    progressTicker := time.NewTicker(2 * time.Second)
    defer progressTicker.Stop()

    resultCh := make(chan *ExecuteResult, 1)

    // 输出收集 goroutine
    go func() {
        scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
        for scanner.Scan() {
            line := scanner.Text()
            output.Write([]byte(line + "\n"))
            stallTimer.Reset(t.bgMgr.stallTimeout)
        }
        process.Wait()
        resultCh <- &ExecuteResult{
            Output:   output.TailString(4096),
            ExitCode: process.ExitCode(),
        }
    }()

    // 主循环：等待完成或触发策略
    for {
        select {
        case result := <-resultCh:
            // 命令完成
            return result, nil

        case <-budgetTimer.C:
            // ===== 自适应后台化 =====
            taskID := t.bgMgr.promoteToBackground(cmd, description, process, output)
            return &ExecuteResult{
                Promoted: true,
                TaskID:   taskID,
                Message:  fmt.Sprintf("Command running for >%s, promoted to background task %s. Use check_background to monitor.",
                    t.bgMgr.blockingBudget, taskID),
            }, nil

        case <-stallTimer.C:
            // ===== Stall 检测 =====
            config.Logger().Printf("Stall detected for command: %s (no output for %s)", cmd, t.bgMgr.stallTimeout)
            // 不杀进程，发送警告给 agent
            return &ExecuteResult{
                Output:  output.TailString(4096),
                Warning: fmt.Sprintf("Command may be stalled (no output for %s). It might be waiting for input. Consider killing with Ctrl+C or using a different approach.", t.bgMgr.stallTimeout),
            }, nil

        case <-progressTicker.C:
            // 流式进度报告
            if onProgress != nil {
                onProgress(StreamChunk{
                    Data:      output.TailString(500),
                    Timestamp: time.Now(),
                })
            }

        case <-ctx.Done():
            process.Kill()
            return nil, ctx.Err()
        }
    }
}
```

### 3.4 前台→后台提升

```go
func (bgm *BackgroundManagerV2) promoteToBackground(
    cmd, description string,
    process Process,
    output *OutputBuffer,
) string {
    bgm.mu.Lock()
    defer bgm.mu.Unlock()

    bgm.nextID++
    id := fmt.Sprintf("bg_%d", bgm.nextID)

    // 创建磁盘日志
    logFile, err := NewTaskLog(bgm.storage, id)
    if err != nil {
        config.Logger().Printf("Failed to create task log: %v", err)
    }

    // 写入已有输出
    if logFile != nil {
        logFile.Write([]byte(output.String()))
    }

    task := &BgTaskV2{
        ID:          id,
        Command:     cmd,
        Description: description,
        Status:      TaskPromoted,
        StartTime:   time.Now(),
        buffer:      output,
        logFile:     logFile,
        lastWrite:   time.Now(),
    }
    bgm.tasks[id] = task

    // 继续异步收集输出
    go bgm.monitorTask(task, process)

    return id
}

func (bgm *BackgroundManagerV2) monitorTask(task *BgTaskV2, process Process) {
    defer func() {
        task.EndTime = time.Now()
        if task.logFile != nil {
            task.logFile.Close()
        }
    }()

    // 从 process 继续读取输出
    scanner := bufio.NewScanner(process.Stdout())
    stallTimer := time.NewTimer(bgm.stallTimeout)

    for scanner.Scan() {
        line := scanner.Text() + "\n"
        task.mu.Lock()
        task.buffer.Write([]byte(line))
        if task.logFile != nil {
            task.logFile.Write([]byte(line))
        }
        task.lastWrite = time.Now()
        task.mu.Unlock()
        stallTimer.Reset(bgm.stallTimeout)
    }

    task.mu.Lock()
    if process.ExitCode() == 0 {
        task.Status = TaskCompleted
    } else {
        task.Status = TaskFailed
        task.exitCode = process.ExitCode()
    }
    task.mu.Unlock()
}
```

### 3.5 Sleep 检测

```go
var sleepPatterns = []*regexp.Regexp{
    regexp.MustCompile(`\bsleep\s+(\d+)`),
    regexp.MustCompile(`\bsleep\s+(\d+)m`),
    regexp.MustCompile(`\bsleep\s+(\d+)h`),
}

const maxSleepSeconds = 30

func detectSleep(command string) (bool, string) {
    for _, pattern := range sleepPatterns {
        matches := pattern.FindStringSubmatch(command)
        if len(matches) < 2 {
            continue
        }
        seconds, err := strconv.Atoi(matches[1])
        if err != nil {
            continue
        }
        // 根据单位调整
        if strings.HasSuffix(matches[0], "m") {
            seconds *= 60
        } else if strings.HasSuffix(matches[0], "h") {
            seconds *= 3600
        }
        if seconds > maxSleepSeconds {
            return true, fmt.Sprintf(
                "Command contains 'sleep %s' which would block for %ds (max %ds). "+
                "Consider using background mode or a shorter sleep.",
                matches[1], seconds, maxSleepSeconds)
        }
    }
    return false, ""
}
```

### 3.6 命令分类

```go
type CommandCategory string
const (
    CmdSearch   CommandCategory = "search"   // grep, rg, find, ag
    CmdRead     CommandCategory = "read"     // cat, head, tail, less, jq
    CmdList     CommandCategory = "list"     // ls, tree, du
    CmdSafe     CommandCategory = "safe"     // pwd, echo, date, whoami
    CmdGit      CommandCategory = "git"      // git status, git log, git diff
    CmdMutating CommandCategory = "mutating" // 其他所有
)

var commandCategories = map[string]CommandCategory{
    "grep": CmdSearch, "rg": CmdSearch, "find": CmdSearch, "ag": CmdSearch,
    "cat": CmdRead, "head": CmdRead, "tail": CmdRead, "less": CmdRead, "jq": CmdRead, "wc": CmdRead,
    "ls": CmdList, "tree": CmdList, "du": CmdList, "df": CmdList,
    "pwd": CmdSafe, "echo": CmdSafe, "date": CmdSafe, "whoami": CmdSafe, "uname": CmdSafe,
    "which": CmdSafe, "env": CmdSafe, "printenv": CmdSafe,
}

func classifyCommand(cmd string) CommandCategory {
    // 提取第一个命令（忽略管道、重定向后的部分用于分类）
    parts := strings.Fields(cmd)
    if len(parts) == 0 {
        return CmdMutating
    }
    base := filepath.Base(parts[0])

    // git 子命令分类
    if base == "git" && len(parts) > 1 {
        switch parts[1] {
        case "status", "log", "diff", "show", "branch", "tag", "remote", "config":
            return CmdGit
        }
        return CmdMutating
    }

    if cat, ok := commandCategories[base]; ok {
        return cat
    }
    return CmdMutating
}

// IsCollapsible 搜索/读取/列表类命令可折叠
func (c CommandCategory) IsCollapsible() bool {
    return c == CmdSearch || c == CmdRead || c == CmdList || c == CmdGit
}
```

### 3.7 执行主流程

```go
func (t *ExecuteToolV2) Invoke(ctx context.Context, input string) (string, error) {
    var params ExecuteInputV2
    json.Unmarshal([]byte(input), &params)

    // 1. Sleep 检测
    if blocked, msg := detectSleep(params.Command); blocked {
        return msg, nil
    }

    // 2. 超时处理
    timeout := time.Duration(params.Timeout) * time.Second
    if timeout == 0 {
        timeout = 120 * time.Second
    }
    if timeout > 600*time.Second {
        timeout = 600 * time.Second
    }

    // 3. 后台执行
    if params.Background {
        return t.executeBackground(ctx, params.Command, params.Description, timeout)
    }

    // 4. 前台执行（带自适应后台化）
    result, err := t.executeWithBudget(ctx, params.Command, timeout, params.Description,
        func(chunk StreamChunk) {
            // 发送进度到 TUI
            t.env.OnProgress(ToolProgressMsg{
                ToolName:      "execute",
                PartialOutput: chunk.Data,
                ElapsedSec:    int(time.Since(t.startTime).Seconds()),
            })
        },
    )
    if err != nil {
        return fmt.Sprintf("Error: %s", err), nil
    }

    return t.formatResult(result), nil
}

func (t *ExecuteToolV2) executeBackground(ctx context.Context, cmd, desc string, timeout time.Duration) (string, error) {
    taskID := t.bgMgr.StartBackground(ctx, cmd, desc, timeout)
    return fmt.Sprintf("Background task started: %s\nCommand: %s\nUse check_background tool to monitor.", taskID, cmd), nil
}

// ExecuteResult 执行结果
type ExecuteResult struct {
    Output   string
    ExitCode int
    Warning  string
    Promoted bool
    TaskID   string
    Message  string
}

func (t *ExecuteToolV2) formatResult(r *ExecuteResult) string {
    if r.Promoted {
        return r.Message
    }

    var b strings.Builder
    if r.Output != "" {
        b.WriteString(r.Output)
    }
    if r.ExitCode != 0 {
        b.WriteString(fmt.Sprintf("\n[Exit code: %d]", r.ExitCode))
    }
    if r.Warning != "" {
        b.WriteString(fmt.Sprintf("\n⚠️ %s", r.Warning))
    }
    return b.String()
}
```

### 3.8 Executor 接口扩展

```go
// Executor 接口 V2 扩展
type ExecutorV2 interface {
    Executor // 嵌入 V1 接口

    // ExecStreaming 流式执行（返回 stdout/stderr readers）
    ExecStreaming(ctx context.Context, command string, workDir string) (
        stdout io.ReadCloser,
        stderr io.ReadCloser,
        process Process,
        err error,
    )
}

// Process 进程控制接口
type Process interface {
    Wait() error
    Kill() error
    ExitCode() int
    Stdout() io.ReadCloser
}

// LocalProcess 本地进程实现
type LocalProcess struct {
    cmd *exec.Cmd
}

func (p *LocalProcess) Wait() error     { return p.cmd.Wait() }
func (p *LocalProcess) Kill() error     { return p.cmd.Process.Kill() }
func (p *LocalProcess) ExitCode() int   { return p.cmd.ProcessState.ExitCode() }
```

---

## 4. Check Background Tool V2

```go
// CheckBackgroundInputV2
type CheckBackgroundInputV2 struct {
    TaskID string `json:"task_id,omitempty"` // 空=列出所有
}

func (t *CheckBackgroundToolV2) Invoke(ctx context.Context, input string) (string, error) {
    var params CheckBackgroundInputV2
    json.Unmarshal([]byte(input), &params)

    if params.TaskID != "" {
        return t.checkSingle(params.TaskID)
    }
    return t.listAll()
}

func (t *CheckBackgroundToolV2) checkSingle(taskID string) (string, error) {
    task, ok := t.bgMgr.GetTask(taskID)
    if !ok {
        return fmt.Sprintf("Task %s not found", taskID), nil
    }

    var b strings.Builder
    fmt.Fprintf(&b, "Task: %s\n", task.ID)
    fmt.Fprintf(&b, "Command: %s\n", task.Command)
    fmt.Fprintf(&b, "Status: %s\n", task.Status)
    fmt.Fprintf(&b, "Started: %s\n", task.StartTime.Format(time.RFC3339))

    if task.Status == TaskCompleted || task.Status == TaskFailed {
        fmt.Fprintf(&b, "Ended: %s\n", task.EndTime.Format(time.RFC3339))
        fmt.Fprintf(&b, "Duration: %s\n", task.EndTime.Sub(task.StartTime).Round(time.Second))
    }

    // 读取输出（优先磁盘日志）
    var output string
    if task.logFile != nil {
        output, _ = task.logFile.ReadAll()
    } else {
        task.mu.Lock()
        output = task.buffer.TailString(4096)
        task.mu.Unlock()
    }

    if output != "" {
        fmt.Fprintf(&b, "\nOutput (last 4096 chars):\n%s", output)
    }

    if task.Status == TaskStalled {
        fmt.Fprintf(&b, "\n⚠️ Task appears stalled (no output for %s)", t.bgMgr.stallTimeout)
    }

    return b.String(), nil
}
```

---

## 5. 对比矩阵

| 维度 | Claude Code | jcode V1 | jcode V2 (设计) |
|------|-------------|----------|-----------------|
| **Shell** | Bash + PowerShell | Bash | Bash (保留) |
| **默认超时** | 30s | 120s | 120s (保留) |
| **自适应后台化** | 15s 时间预算 | 无 | 15s 时间预算 |
| **流式进度** | AsyncGenerator | 无 | goroutine + channel |
| **输出存储** | 64MB 磁盘 | 2000字符内存 | 4KB 内存 + 64MB 磁盘 |
| **Stall 检测** | 45s 看门狗 | 无 | 45s 看门狗 |
| **Sleep 检测** | 正则模式匹配 | 无 | 正则模式匹配 |
| **命令分类** | search/read/list | 无 | search/read/list/safe/git |
| **前台→后台提升** | 自动 | 无 | 自动 |
| **沙盒** | 默认开启 | 无 | 无 (Linux 不需要) |
| **Sed 预览** | 按 FileEdit 审批 | 无 | 无 (V2 scope out) |
| **进度间隔** | 2s 可配 | 无 | 2s |
| **远程执行** | 无 | SSH executor | SSH executor (保留) |

---

## 6. 常量定义

```go
const (
    DefaultTimeout     = 120 * time.Second
    MaxTimeout         = 600 * time.Second
    BlockingBudget     = 15 * time.Second    // 前台时间预算
    StallTimeout       = 45 * time.Second    // Stall 检测
    ProgressInterval   = 2 * time.Second     // 进度报告间隔
    MemoryBufferSize   = 4096                // 内存输出缓冲
    MaxTaskLogSize     = 64 << 20            // 64MB 磁盘日志
    MaxBackgroundTasks = 20                  // 最大并发后台任务
    MaxSleepSeconds    = 30                  // Sleep 检测阈值
)
```
