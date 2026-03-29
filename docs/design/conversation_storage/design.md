# Conversation Storage Redesign

## 1. Problem Statement

The current session storage (JSONL) only records five basic events: `session_start`, `user`, `assistant`, `tool_call`, `tool_result`. Several critical runtime states exist entirely in memory, causing the following issues:

| Problem | Impact |
|---------|--------|
| **Plan state not persisted** | PlanStore is purely in-memory; plan draft/approved/rejected states are lost after resume |
| **Todo state not persisted** | TodoStore is purely in-memory; all todo items are lost after resume, completion guard fails across sessions |
| **SubAgent context invisible** | Subagent tool calls and final results are not recorded in JSONL; impossible to know what subagent did after resume |
| **Compact events not tracked** | `compactHistory()` replaces messages in memory, but JSONL still has full history; resume restores pre-compact state |
| **Mode switches not recorded** | Normal → Planning → Executing transitions are not persisted |
| **Background task results not recorded** | Asynchronously completed background task outputs are not in session |
| **ReconstructHistory loses tool calls** | Currently only restores user/assistant text; tool_call/tool_result are skipped, resulting in incomplete context after restoration |

## 2. Design Goals

1. **Structured session model** — Record state transitions between plan phase, execution phase, and normal phase
2. **Persist Plan state** — Plan's draft/submitted/approved/rejected + content + feedback all go into JSONL
3. **Persist Todo state** — Todo list snapshots recorded with todowrite calls
4. **Record SubAgent interactions** — Subagent start, type, prompt, and final result as structured events
5. **Record Compact events** — Summary text produced by compact + number of compressed messages; use summary as basis on resume
6. **Record mode switches** — Normal/Planning/Executing transition events
7. **Maintain append-only JSONL** — Don't change storage format, only extend entry types
8. **Backward compatibility** — Old session files can still be loaded and replayed normally

## 3. Extended Entry Types

Add 6 new entry types on top of the existing 5:

```go
const (
    // Existing
    EntrySessionStart EntryType = "session_start"
    EntryUser         EntryType = "user"
    EntryAssistant    EntryType = "assistant"
    EntryToolCall     EntryType = "tool_call"
    EntryToolResult   EntryType = "tool_result"

    // New
    EntryPlanUpdate    EntryType = "plan_update"     // plan state change
    EntryTodoSnapshot  EntryType = "todo_snapshot"   // todo list full snapshot
    EntrySubagentStart EntryType = "subagent_start"  // subagent start
    EntrySubagentResult EntryType = "subagent_result" // subagent complete
    EntryModeChange    EntryType = "mode_change"     // mode switch
    EntryCompact       EntryType = "compact"         // history compression
)
```

## 4. Extended Entry Struct

```go
type Entry struct {
    Type      EntryType `json:"type"`
    Timestamp string    `json:"timestamp"`

    // session_start
    UUID     string `json:"uuid,omitempty"`
    Project  string `json:"project,omitempty"`
    Provider string `json:"provider,omitempty"`
    Model    string `json:"model,omitempty"`

    // user, assistant
    Content string `json:"content,omitempty"`

    // tool_call, tool_result
    Name   string `json:"name,omitempty"`
    Args   string `json:"args,omitempty"`
    Output string `json:"output,omitempty"`
    Error  string `json:"error,omitempty"`

    // plan_update
    PlanStatus  string `json:"plan_status,omitempty"`   // draft/submitted/approved/rejected
    PlanTitle   string `json:"plan_title,omitempty"`
    PlanContent string `json:"plan_content,omitempty"`
    Feedback    string `json:"feedback,omitempty"`       // 拒绝时的反馈

    // todo_snapshot
    Todos []TodoSnapshotItem `json:"todos,omitempty"`

    // subagent_start, subagent_result
    SubagentName string `json:"subagent_name,omitempty"`
    SubagentType string `json:"subagent_type,omitempty"`  // explore/general

    // mode_change
    Mode string `json:"mode,omitempty"` // normal/planning/executing

    // compact
    Summary      string `json:"summary,omitempty"`
    CompactedN   int    `json:"compacted_n,omitempty"` // 被压缩的消息数
}

type TodoSnapshotItem struct {
    ID     int    `json:"id"`
    Title  string `json:"title"`
    Status string `json:"status"`
}
```

All new fields use `omitempty`; old sessions deserialize with zero values for these fields, fully compatible.

## 5. New Recorder Methods

```go
// Existing API unchanged
RecordUser(content string)
RecordAssistant(content string)
RecordToolCall(name, args string)
RecordToolResult(name, output string, err error)

// New API
RecordPlanUpdate(status, title, content, feedback string)
RecordTodoSnapshot(items []TodoItem)
RecordSubagentStart(name, agentType string)
RecordSubagentResult(name, output string, err error)
RecordModeChange(mode string)
RecordCompact(summary string, compactedN int)
```

## 6. Session State Reconstruction

Add a new `ReconstructState()` function to build complete session state on resume:

```go
// SessionState is the complete resumable session state
type SessionState struct {
    History   []adk.Message     // Message sequence that can be sent to agent
    Plan      *PlanSnapshot     // Last plan state, nil means no plan
    Todos     []TodoSnapshotItem // Last todo snapshot
    Mode      string            // Last mode (normal/planning/executing)
    EnvTarget string            // Last environment (local/ssh alias)
}

type PlanSnapshot struct {
    Status   string
    Title    string
    Content  string
    Feedback string
}
```

### Reconstruction Logic

```
1. Sequential scan of entries
2. On EntryCompact → discard previously accumulated history, use summary as System message base
3. On EntryUser/EntryAssistant → append to history
4. On EntryPlanUpdate → update Plan snapshot
5. On EntryTodoSnapshot → update Todos snapshot
6. On EntryModeChange → update Mode
7. On switch_env tool_call/result → update EnvTarget (existing logic)
8. Return SessionState
```

Core improvement: **compact-aware** — If session has compact events, resume starts from after compact instead of from beginning, avoiding restoration of already-compressed lengthy history.

## 7. Integration Points

### 7.1 main.go — Mode Switch Recording

```go
// Add in applyModeSwitch:
if rec != nil {
    rec.RecordModeChange(modeString(newMode))
}
```

### 7.2 main.go — Plan State Recording

```go
// Add after planStore.Submit():
if rec != nil {
    rec.RecordPlanUpdate("submitted", "Plan", resp, "")
}

// Add after planStore.Approve():
if rec != nil {
    rec.RecordPlanUpdate("approved", planStore.Title(), planStore.Content(), "")
}

// Add after planStore.Reject():
if rec != nil {
    rec.RecordPlanUpdate("rejected", "", "", feedback)
}
```

### 7.3 main.go — Compact Recording

```go
// Add in compactCh handler:
// After compactHistory, if compact succeeded, record summary
if rec != nil && len(history) > 0 {
    // Find summary produced by compact (history[0] is system message)
    rec.RecordCompact(history[0].Content, oldMessageCount - len(history))
}
```

### 7.4 TodoStore — Snapshot Recording

Add an `OnUpdate` callback to TodoStore; when todowrite tool updates todos, automatically trigger recorder to record snapshot:

```go
type TodoStore struct {
    mu       sync.RWMutex
    items    []TodoItem
    OnUpdate func(items []TodoItem) // NEW: update callback
}

func (s *TodoStore) Update(items []TodoItem) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.items = make([]TodoItem, len(items))
    copy(s.items, items)
    if s.OnUpdate != nil {
        snapshot := make([]TodoItem, len(items))
        copy(snapshot, items)
        s.OnUpdate(snapshot) // call without holding lock
    }
}
```

In main.go initialization:
```go
env.TodoStore.OnUpdate = func(items []tools.TodoItem) {
    if rec != nil {
        rec.RecordTodoSnapshot(items)
    }
}
```

### 7.5 SubAgent — Event Recording

Add Recorder reference to SubagentDeps:

```go
type SubagentDeps struct {
    ChatModel model.ToolCallingChatModel
    Notifier  SubagentNotifier
    Recorder  *session.Recorder  // NEW
}
```

In subagentTool.InvokableRun:
```go
// On start
if s.deps.Recorder != nil {
    s.deps.Recorder.RecordSubagentStart(input.Name, agentType)
}
// On completion
if s.deps.Recorder != nil {
    s.deps.Recorder.RecordSubagentResult(input.Name, result, nil)
}
```

### 7.6 Resume Flow Refactoring

```go
// Existing: entries → ReconstructHistory() → []adk.Message
// New: entries → ReconstructState() → SessionState

state := session.ReconstructState(entries)
initialHistory = state.History

// Restore Plan
if state.Plan != nil {
    switch state.Plan.Status {
    case "approved":
        planStore.Submit(state.Plan.Title, state.Plan.Content)
        planStore.Approve()
    case "submitted":
        planStore.Submit(state.Plan.Title, state.Plan.Content)
    }
}

// Restore Todos
if len(state.Todos) > 0 {
    todoItems := convertSnapshotToTodoItems(state.Todos)
    env.TodoStore.Update(todoItems)
}

// Restore Mode
if state.Mode != "" {
    applyModeSwitch(modeFromString(state.Mode))
}

// Restore Env (existing logic preserved)
targetEnv := state.EnvTarget
```

## 8. Data Flow Diagram

```
User Input
    │
    ▼
┌──────────────────────────────────────────────────────┐
│  main.go goroutine                                   │
│                                                      │
│  promptCh → RecordUser()                             │
│           → runner.Run()                             │
│              ├─ RecordToolCall()                      │
│              ├─ RecordToolResult()                    │
│              │   └─ todowrite → OnUpdate callback     │
│              │                   └─ RecordTodoSnapshot│
│              ├─ subagent → RecordSubagentStart()      │
│              │           → RecordSubagentResult()     │
│              └─ RecordAssistant()                     │
│                                                      │
│  planModeCh → RecordModeChange()                     │
│  planStore  → RecordPlanUpdate()                     │
│  compactCh  → RecordCompact()                        │
│                                                      │
│  resumeCh   → LoadSession()                          │
│             → ReconstructState()                     │
│             → Restore Plan/Todo/Mode/Env             │
└──────────────────────────────────────────────────────┘
          │
          ▼
   ~/.jcoding/sessions/{UUID}.json  (JSONL)
```

## 9. JSONL Example

```jsonl
{"type":"session_start","uuid":"abc-123","project":"/home/user/proj","provider":"openai","model":"gpt-4o","timestamp":"2026-03-29T10:00:00Z"}
{"type":"mode_change","mode":"planning","timestamp":"2026-03-29T10:00:01Z"}
{"type":"user","content":"帮我重构 auth 模块","timestamp":"2026-03-29T10:00:02Z"}
{"type":"tool_call","name":"read","args":"{\"path\":\"internal/auth/auth.go\"}","timestamp":"2026-03-29T10:00:03Z"}
{"type":"tool_result","name":"read","output":"package auth...","timestamp":"2026-03-29T10:00:04Z"}
{"type":"assistant","content":"## Plan\n1. 拆分 handler...\n2. 新增 middleware...","timestamp":"2026-03-29T10:00:10Z"}
{"type":"plan_update","plan_status":"submitted","plan_title":"Plan","plan_content":"## Plan\n1. 拆分 handler...\n2. 新增 middleware...","timestamp":"2026-03-29T10:00:11Z"}
{"type":"plan_update","plan_status":"approved","plan_title":"Plan","plan_content":"## Plan\n1. 拆分 handler...\n2. 新增 middleware...","timestamp":"2026-03-29T10:00:30Z"}
{"type":"todo_snapshot","todos":[{"id":1,"title":"拆分 handler","status":"pending"},{"id":2,"title":"新增 middleware","status":"pending"}],"timestamp":"2026-03-29T10:00:31Z"}
{"type":"mode_change","mode":"executing","timestamp":"2026-03-29T10:00:32Z"}
{"type":"user","content":"Your plan has been approved. Execute it step by step...","timestamp":"2026-03-29T10:00:33Z"}
{"type":"subagent_start","subagent_name":"explore-auth","subagent_type":"explore","timestamp":"2026-03-29T10:00:40Z"}
{"type":"subagent_result","subagent_name":"explore-auth","output":"Found 3 handler functions...","timestamp":"2026-03-29T10:00:50Z"}
{"type":"todo_snapshot","todos":[{"id":1,"title":"拆分 handler","status":"completed"},{"id":2,"title":"新增 middleware","status":"in_progress"}],"timestamp":"2026-03-29T10:01:00Z"}
{"type":"compact","summary":"[Summary] 用户要求重构 auth 模块...","compacted_n":8,"timestamp":"2026-03-29T10:05:00Z"}
{"type":"mode_change","mode":"normal","timestamp":"2026-03-29T10:10:00Z"}
```

## 10. Implementation Plan

### Phase 1: Entry 扩展 + Recorder 新方法 (internal/session/)
1. 扩展 `EntryType` 常量
2. 扩展 `Entry` struct + 新增 `TodoSnapshotItem`
3. 新增 `Recorder` 方法: `RecordPlanUpdate`, `RecordTodoSnapshot`, `RecordSubagentStart`, `RecordSubagentResult`, `RecordModeChange`, `RecordCompact`

### Phase 2: State 重建 (internal/session/history.go)
1. 定义 `SessionState`, `PlanSnapshot`
2. 实现 `ReconstructState()` — compact-aware 的全状态重建
3. 保留旧的 `ReconstructHistory()` 作为兼容

### Phase 3: 录制接入 (cmd/coding/main.go)
1. 模式切换 → `RecordModeChange`
2. Plan 状态变更 → `RecordPlanUpdate`
3. Compact 事件 → `RecordCompact`
4. 接入 resume 新流程 → 使用 `ReconstructState`

### Phase 4: TodoStore 回调 (internal/tools/todo.go)
1. TodoStore 添加 `OnUpdate` 回调字段
2. main.go 中绑定回调到 recorder

### Phase 5: SubAgent 录制 (internal/tools/subagent.go)
1. SubagentDeps 添加 `Recorder` 字段
2. InvokableRun 中调用 recorder

## 11. Backward Compatibility

- 旧 JSONL 文件：新字段全部 `omitempty`，`json.Unmarshal` 对未知字段零值处理
- `ReconstructHistory()` 保留不变，旧代码路径不受影响
- `ReconstructState()` 对旧 session 正常工作（Plan/Todo/Mode 字段为零值/nil）
- 新 entry types 在旧代码中被 `switch` default 忽略

## 12. History ↔ Eino Summarization Synchronization

### Problem

The application-layer `history` variable (user/assistant text only) and Eino agent's internal `state.Messages` (containing ToolCalls + Tool role messages) have two layers of desynchronization:

1. **Tool calls not in history** — Each turn, tool_call/tool_result messages produced internally by the agent are lost after the turn ends (history only appends plain text assistant message)
2. **Summarization results not synced back** — Eino's summarization middleware compresses `state.Messages` in `BeforeModelRewriteState`, but `history` is unaffected. The next turn passes in the full uncompressed history again, causing summarization to repeatedly trigger every turn

### Solution

Implement bidirectional synchronization via `summarization.Config.Finalize` callback + `syncSummarization()` helper:

```
runner.Run()
  ├─ agent iteration
  │   └─ BeforeModelRewriteState
  │       └─ summarization triggers
  │           └─ Finalize callback → summCapture.capture(summary, N)
  └─ returns resp

history = append(history, assistantMsg(resp))
history = syncSummarization(summCapture, history, rec)
  ├─ if !fired → return history (no-op)
  └─ if fired  → [summary_system_msg, last_user, last_assistant]
                  + RecordCompact(summary, N) to JSONL
```

### Key Design Decisions

1. **`summarizationCapture` is single-thread safe** — Finalize is called synchronously within `runner.Run()`, drain is called immediately after `runner.Run()` returns, no concurrency

2. **Keep last 2 messages** — `syncSummarization` keeps the last 2 entries in history (latest user + assistant), consistent with manual compact behavior

3. **Unified compact recording** — Both Eino automatic summarization and manual compact write to JSONL via `RecordCompact`, `ReconstructState` handles uniformly on resume

4. **Tool calls by design not restored** — tool_call + tool_result are managed internally by Eino agent within each turn; only text summary is preserved after turn ends. This is correct because tool call IDs cannot be reconstructed across turns

5. **SubAgent events are metadata** — `subagent_start/result` are for audit trail, do not affect agent behavior after resume (subagent results have already been passed to agent via tool_call/tool_result path)

### Data Flow Overview

```
Turn N:
  history = [sys_summary?, ...user/assistant text pairs...]
           │
           ▼
  runner.Run(ag, history)
           │
  Eino agent internal state.Messages:
    [instruction, ...history..., user_N]
    iteration 0: → model → assistant(ToolCalls) → tool results
    iteration 1: → model → assistant(ToolCalls) → tool results
           ...
    [summarization may fire → Finalize → summCapture.capture()]
           ...
    final iteration: → model → assistant(text)
           │
           ▼
  resp = "final text"
  history = append(history, assistantMsg(resp))
  history = syncSummarization(summCapture, history, rec)
           │
           ▼
Turn N+1:
  history = [compact_summary, last_user, last_assistant]  ← if summarization fired
         or [original history + new assistant]             ← if not fired
```
