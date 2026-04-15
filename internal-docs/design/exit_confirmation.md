# Exit Confirmation - State Machine Design

## Overview
双击 Ctrl+C 退出机制，当有任务在执行时提示用户确认。

## State Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                      Exit State Machine                      │
└─────────────────────────────────────────────────────────────┘

                    ┌──────────────┐
                    │   RUNNING    │◄────────┐
                    │ (Normal Run) │         │
                    └──────┬───────┘         │
                           │                 │
                           │ Ctrl+C          │
                           │                 │
                    ┌──────▼──────────┐      │
                    │  Check Status   │      │
                    └──────┬──────────┘      │
                           │                 │
              ┌────────────┴────────────┐    │
              │                         │    │
              │                         │    │
    ┌─────────▼──────┐       ┌─────────▼────▼──────┐
    │  No Task       │       │  Task Running        │
    │  Running       │       │  (thinking=true)     │
    └─────────┬──────┘       └─────────┬────────────┘
              │                        │
              │                        │ Set exitPending=true
              │                        │ Show warning
              │                        │ Start 2s timer
              │                        │
              │              ┌─────────▼────────────┐
              │              │   EXIT_PENDING       │
              │              │  (Waiting confirm)   │
              │              └─────────┬────────────┘
              │                        │
              │                        │
              │           ┌────────────┼────────────┐
              │           │            │            │
              │    Ctrl+C │    Timeout │    Other   │
              │    (2nd)  │    (2s)    │    key     │
              │           │            │            │
    ┌─────────▼───────────▼┐  ┌────────▼─┐  ┌───────▼────┐
    │       QUIT            │  │ Clear    │  │ Clear      │
    │   tea.Quit()          │  │ warning  │  │ warning    │
    └───────────────────────┘  └────────┬─┘  └──────┬─────┘
                                        │           │
                                        │           │
                                        └───────────┴──────►
                                               │
                                        ┌──────▼──────┐
                                        │   RUNNING   │
                                        └─────────────┘
```

## States

### RUNNING (Normal)
- **Description**: 正常运行状态，用户可以输入或 agent 在执行
- **Fields**: `exitPending = false`
- **Display**: 无额外提示

### EXIT_PENDING (Waiting Confirmation)
- **Description**: 第一次 Ctrl+C 后，等待用户第二次确认
- **Fields**: `exitPending = true`, `exitWarningTime = time.Now()`
- **Display**: 底部显示黄色警告 `⚠ Agent is running. Press Ctrl+C again to force quit.`
- **Timeout**: 2 秒后自动清除

### QUIT
- **Description**: 退出状态
- **Action**: 调用 `tea.Quit()`，应用退出

## Events

### Ctrl+C Pressed
- **From RUNNING (no task)** → **QUIT**
  - 条件：`thinking=false` 或 `agentDone=true`
  - 动作：直接退出

- **From RUNNING (task running)** → **EXIT_PENDING**
  - 条件：`thinking=true` 且 `agentDone=false`
  - 动作：
    1. 设置 `exitPending = true`
    2. 记录 `exitWarningTime = time.Now()`
    3. 显示警告消息
    4. 启动 2 秒定时器

- **From EXIT_PENDING** → **QUIT**
  - 条件：`exitPending=true`
  - 动作：强制退出

### Timeout (2s elapsed)
- **From EXIT_PENDING** → **RUNNING**
  - 动作：
    1. 设置 `exitPending = false`
    2. 清除警告消息

### Other Key Pressed
- **From EXIT_PENDING** → **RUNNING**
  - 动作：
    1. 设置 `exitPending = false`
    2. 清除警告消息
  - 说明：用户继续操作，取消退出意图

## Implementation

### Model Fields
```go
type Model struct {
    // ... existing fields ...
    
    // Exit confirmation
    exitPending     bool       // true when waiting for 2nd Ctrl+C
    exitWarningTime time.Time  // when the warning was shown
}
```

### Key Handler Logic
```go
case "ctrl+c":
    // Check if already pending
    if m.exitPending {
        return m, tea.Quit  // Force quit on 2nd Ctrl+C
    }
    
    // Check if agent is running
    if m.thinking && !m.agentDone {
        m.exitPending = true
        m.exitWarningTime = time.Now()
        m.lines = append(m.lines, 
            warningStyle.Render("⚠ Agent is running. Press Ctrl+C again to force quit."))
        m.refreshViewport()
        
        // Start timeout timer
        return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
            return ExitTimeoutMsg{}
        })
    }
    
    // No task running, quit immediately
    return m, tea.Quit
```

### Timer Handler
```go
case ExitTimeoutMsg:
    if m.exitPending && time.Since(m.exitWarningTime) >= 2*time.Second {
        m.exitPending = false
        // Remove warning message from lines
    }
```

### Reset on Other Keys
```go
// In Update() before processing other keys
if m.exitPending && msg.String() != "ctrl+c" {
    m.exitPending = false
    // Clear warning
}
```

## User Experience

### Scenario 1: No Task Running
```
User: Ctrl+C
System: [Exits immediately]
```

### Scenario 2: Task Running - Confirm Exit
```
User: Ctrl+C
System: ⚠ Agent is running. Press Ctrl+C again to force quit.
User: Ctrl+C (within 2s)
System: [Exits]
```

### Scenario 3: Task Running - Cancel Exit
```
User: Ctrl+C
System: ⚠ Agent is running. Press Ctrl+C again to force quit.
User: [types something / waits 2s]
System: [Warning clears, continues running]
```

## Benefits

1. **Prevents Accidental Exit**: 避免误按 Ctrl+C 导致正在执行的任务中断
2. **Clear Feedback**: 明确告知用户当前有任务在执行
3. **Quick Override**: 允许用户快速双击强制退出
4. **Auto Recovery**: 2 秒超时自动恢复，不需要手动清除警告
