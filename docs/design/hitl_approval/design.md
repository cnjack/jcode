# Human-in-the-Loop Approval Mechanism — Design Document

> **Version**: 1.0
> **Last Updated**: 2026-03-25
> **Modules**: `internal/tui`, `internal/runner`, `internal/agent`
> **Branch**: `feat/switch_env`

---

## 1. Overview

This document describes the improved design for the Human-in-the-Loop (HITL) approval mechanism. The core objective is to resolve authorization ambiguity, lack of revocability, and over-permissive defaults in the original implementation. A **state-machine-driven dual-mode approval system** provides fine-grained, controllable, and secure tool-call approval.

---

## 2. Problem Statement

| Problem | Impact | User Perception |
|---------|--------|-----------------|
| **One-shot authorization misleading** | Pressing `y` once silently auto-approves the entire session | Users believe they approved only the current operation |
| **No revocation mechanism** | Once auto-approve is enabled, there is no way to disable it | Users lose control over subsequent sensitive operations |
| **Over-permissive read tool** | Reads any file (including `~/.ssh/*` and other sensitive paths) without approval | Information leakage risk |

---

## 3. Design Goals

### 3.1 Functional Goals
- **Fine-grained approval**: Distinguish "approve once" (`ApproveOnce`) from "approve all subsequent" (`ApproveAll`)
- **Revocable authorization**: Users can toggle approval mode at any time via `Ctrl+A`
- **Smart path detection**: The `read` tool auto-approves within `workpath`; external paths require approval
- **Rejection feedback**: When a user rejects a tool call, the TUI displays a rejection notice and a system-level reminder is sent to the LLM instructing it not to retry or circumvent the rejected operation

### 3.2 Non-Functional Goals
- **Least privilege principle**: Manual approval by default; auto mode requires explicit opt-in
- **State consistency**: Real-time synchronization of approval state between TUI and Runner
- **User awareness**: Status bar clearly indicates the current mode; approval dialog shows unambiguous options

---

## 4. System Architecture

### 4.1 State Machine

```
┌────────────────────────────────────────────────────────┐
│                  Approval State Machine                 │
├────────────────────────────────────────────────────────┤
│                                                        │
│  ┌──────────────┐           ┌──────────────┐          │
│  │   MANUAL     │──[a]────►│    AUTO      │          │
│  │  (default)   │◄──[Ctrl+A]│ (auto-approve)│         │
│  └──────┬───────┘           └──────┬───────┘          │
│         │                           │                  │
│    [y]/[n] stay MANUAL         [any] stay AUTO         │
│         ▼                           ▼                  │
│   execute / reject             execute directly        │
│                                                        │
│  ┌─ Tool Call Approval Decision Tree ──────────┐      │
│  │                                              │      │
│  │  1. AUTO mode ──────────────► execute        │      │
│  │  2. No-approval tool list ──► execute        │      │
│  │  3. read + within workpath ─► execute        │      │
│  │  4. read + outside workpath ► prompt user    │      │
│  │  5. execute + safe command ─► execute        │      │
│  │  6. other tool + MANUAL ────► prompt user    │      │
│  │                                              │      │
│  └──────────────────────────────────────────────┘      │
└────────────────────────────────────────────────────────┘
```

### 4.2 State Enumeration

```go
// internal/tui/messages.go
type ApprovalMode int

const (
    ModeManual ApprovalMode = iota // Manual approval mode (default)
    ModeAuto                       // Auto-approve mode
)
```

### 4.3 State Transition Table

| Current State | Trigger | Target State | Behavior |
|--------------|---------|-------------|----------|
| MANUAL | ToolCall(no-approval) | MANUAL | Execute directly |
| MANUAL | ToolCall(needs approval) | MANUAL | Show dialog, wait for user |
| MANUAL | ApproveOnce (`y`) | MANUAL | Execute current, stay manual |
| MANUAL | ApproveAll (`a`) | **AUTO** | Execute current, switch to auto |
| MANUAL | Reject (`n`) | MANUAL | Reject execution, show rejection notice, send reminder to LLM |
| MANUAL | ToggleMode (`Ctrl+A`) | **AUTO** | User-initiated toggle |
| AUTO | ToolCall(any) | AUTO | Execute directly |
| AUTO | ToggleMode (`Ctrl+A`) | **MANUAL** | User-initiated toggle |

---

## 5. Detailed Implementation

### 5.1 Message Structures (`internal/tui/messages.go`)

```go
// ToolApprovalResponse is the user's response to a tool approval request
type ToolApprovalResponse struct {
    Approved bool
    Mode     ApprovalMode // Mode after this response (stay MANUAL or switch to AUTO)
}

// ToolApprovalRequestMsg is sent when a tool needs user approval
type ToolApprovalRequestMsg struct {
    Name       string
    Args       string
    Resp       chan ToolApprovalResponse
    IsExternal bool // Whether this is an external path access
}

// ApprovalModeChangedMsg notifies mode changes (optional, for broadcast)
type ApprovalModeChangedMsg struct {
    Mode ApprovalMode
}
```

### 5.2 Approval State Management (`internal/runner/approval.go`)

```go
type ApprovalState struct {
    p        *tea.Program
    mode     ApprovalMode
    workpath string
}

func (s *ApprovalState) RequestApproval(ctx context.Context, toolName, toolArgs string) (bool, error) {
    // [1] AUTO mode: pass all directly
    if s.mode == ModeAuto {
        return true, nil
    }

    // [2] MANUAL mode: tiered decision
    if s.isNoApprovalNeeded(toolName) {
        return true, nil
    }

    if toolName == "read" {
        return s.handleReadApproval(toolArgs)
    }

    if toolName == "execute" && s.isSafeCommand(toolArgs) {
        return true, nil
    }

    // [3] Default: request user approval
    return s.requestUserApproval(ctx, toolName, toolArgs, false)
}

// Path detection: check if a path is within workpath
func (s *ApprovalState) isWithinWorkpath(path string) bool {
    absPath, _ := filepath.Abs(path)
    absWork, _ := filepath.Abs(s.workpath)
    rel, err := filepath.Rel(absWork, absPath)
    if err != nil { return false }
    return !strings.HasPrefix(rel, "..") && rel != ".."
}
```

### 5.3 TUI Interaction Layer (`internal/tui/tui.go`)

#### Model Extension
```go
type Model struct {
    // Approval state machine fields
    approvalMode       ApprovalMode
    approvalPending    bool
    approvalToolName   string
    approvalToolArgs   string
    approvalRespChan   chan ToolApprovalResponse
    approvalIsExternal bool
}
```

#### Keyboard Event Handling
```go
// Inside approval dialog
if m.approvalPending {
    switch msg.String() {
    case "y", "Y":  // ApproveOnce
        m.sendApprovalResponse(true, ModeManual)
    case "a", "A":  // ApproveAll → switch to AUTO
        m.approvalMode = ModeAuto
        m.sendApprovalResponse(true, ModeAuto)
    case "n", "N", "esc":  // Reject
        m.sendApprovalResponse(false, m.approvalMode)
    }
}

// Global shortcut: toggle approval mode
if msg.String() == "ctrl+a" && !m.approvalPending {
    m.approvalMode = 1 - m.approvalMode // toggle
    m.notifyModeChange()
}
```

#### Status Bar Indicator
```go
if state.AutoApprove {
    rightParts = append(rightParts, "Approve: " + warningStyle.Render("Auto"))
} else {
    rightParts = append(rightParts, "Approve: " + mutedStyle.Render("Ask"))
}
```

### 5.4 Approval Dialog View (`internal/tui/pickers.go`)

```go
func (m Model) approvalDialogView() string {
    header := "⚠️ Tool Approval Required"
    if m.approvalIsExternal {
        header = "⚠️ External Path Access"
    }
    footer := "[y] Approve once  [a] Approve all  [n] Reject"
    return renderDialog(header, m.approvalToolName, m.approvalToolArgs, footer)
}
```

### 5.5 Rejection Feedback Mechanism

When the user presses `n` (reject), two things happen:

1. **TUI visual feedback**: A rejection notice line is appended to the chat view:
   ```
   ⚠ Rejected: <tool_name> — user denied this operation
   ```

2. **Agent middleware return**: The tool middleware in `internal/agent/agent.go` returns a structured rejection message that includes a reminder telling the LLM not to retry or use alternative approaches:
   ```
   Tool execution was rejected by user. IMPORTANT: The user has explicitly denied
   this operation. Do NOT attempt to perform the same action using alternative tools,
   different commands, or workarounds. Respect the user's decision and either ask the
   user how they would like to proceed or move on to a different task.
   ```

### 5.6 Module Synchronization (`cmd/coding/main.go`)

```go
// Listen for TUI mode changes
case enabled := <-autoApproveCh:
    approvalState.SetSessionApproval(enabled)

// Listen for workpath changes
env.OnEnvChange = func(envLabel string, isLocal bool, err error) {
    approvalState.SetWorkpath(newPwd)
}
```

---

## 6. Interface Contracts

### 6.1 Public Channel Definitions
```go
var autoApproveCh = make(chan bool, 1)   // true=Auto, false=Manual
func GetAutoApproveChannel() <-chan bool { return autoApproveCh }
```

### 6.2 Path Detection Edge Cases

| Input Path | Workpath | Result | Notes |
|-----------|----------|--------|-------|
| `./config.yaml` | `/project` | ✅ Internal | Relative path resolved |
| `/project/src/main.go` | `/project` | ✅ Internal | Absolute subdirectory |
| `~/.ssh/id_rsa` | `/project` | ❌ External | User home directory |
| `/etc/passwd` | `/project` | ❌ External | System path |
| `../other/file.txt` | `/project` | ❌ External | Parent directory traversal |

---

## 7. File Change Manifest

| File | Change Type | Key Changes |
|------|------------|-------------|
| `internal/tui/messages.go` | Added | `ApprovalMode` enum, approval message structs, channels |
| `internal/tui/tui.go` | Modified | Model extension, key handling, status bar, channel sync |
| `internal/tui/pickers.go` | Modified | Approval dialog view, external path warning |
| `internal/tui/statusbar_component.go` | Modified | Approval mode indicator in status bar |
| `internal/runner/approval.go` | Refactored | State machine logic, path detection, unified approval entry |
| `internal/runner/approval_test.go` | Added | Unit tests for path detection, mode transitions |
| `internal/agent/agent.go` | Modified | Enhanced rejection message with LLM reminder |
| `cmd/coding/main.go` | Modified | Initialization flow, mode sync, environment switch listener |

---

## 8. Test Plan

### 8.1 Unit Tests

```go
func TestIsWithinWorkpath(t *testing.T) {
    tests := []struct{
        workpath, target string
        expected bool
    }{
        {"/proj", "/proj/a.txt", true},
        {"/proj", "/home/user/.ssh/key", false},
        {"/proj", "/proj/../other", false},
    }
}
```

### 8.2 Integration Test Scenarios

| Scenario | Steps | Expected Result |
|----------|-------|-----------------|
| **Internal file read** | Agent calls `read` for a file within `workpath` | Executes directly, no dialog |
| **External sensitive file** | Agent tries to read `~/.ssh/id_rsa` | Dialog shows "External Path Access", requires manual approval |
| **ApproveOnce** | Press `y` in manual mode | Only current operation proceeds, status bar stays 🔒 |
| **ApproveAll** | Press `a` in manual mode | All subsequent operations auto-proceed, status bar shows 🔓 |
| **Mode toggle** | Press `Ctrl+A` at any time | 🔒 ↔ 🔓 toggles instantly |
| **Rejection feedback** | Press `n` to reject | TUI shows rejection notice; agent receives reminder not to retry |
| **Environment switch** | SSH to new workpath, then read file | Path detection recalculates against new `workpath` |

### 8.3 Manual Verification Checklist

- [ ] Status bar indicator matches current mode
- [ ] Approval dialog options are clear and unambiguous
- [ ] `Ctrl+A` works outside the dialog and does not conflict inside it
- [ ] Path detection supports relative paths, absolute paths, and symlinks
- [ ] Mode changes sync to Runner in real time with no state drift
- [ ] Rejection shows visual feedback in TUI and sends reminder to the LLM

---

## 9. Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| **Path detection bypass** | Malicious path construction accesses external files | Normalize with `filepath.Abs` + `filepath.Rel`; reject `..` prefixes |
| **State desynchronization** | TUI and Runner modes diverge | Unidirectional channel sync; Runner is the authoritative source |
| **UX degradation** | Frequent approval dialogs disrupt workflow | Maintain no-approval tool list; provide clear "ApproveAll" option |
| **Concurrency race** | Approval state race under concurrent tool calls | Access `ApprovalState.mode` under the `tea.Program` single-thread model |
| **LLM circumvention** | LLM retries rejected action via alternative tools | Rejection message includes explicit "do not retry" instruction |

---

## 10. Future Enhancements

1. **Configurable approval policy**: Allow users to customize the no-approval tool list and safe command allowlist via config file
2. **Approval audit log**: Record every approval decision (timestamp, tool, path, user choice) for security traceability
3. **Session-scoped authorization**: Limit "ApproveAll" to the current agent session rather than making it globally permanent
4. **Visual path hierarchy**: Highlight the relationship between the target path and `workpath` in the approval dialog
