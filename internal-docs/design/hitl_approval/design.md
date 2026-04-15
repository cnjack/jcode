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

## 5. Key Design Decisions

### 5.1 Approval Dialog

The approval dialog distinguishes between normal tool calls and external path access with differentiated headers:
- **Normal tool call**: "⚠️ Tool Approval Required"
- **External path access**: "⚠️ External Path Access"

Footer options: `[y] Approve once  [a] Approve all  [n] Reject`

### 5.2 Rejection Feedback Mechanism

When the user presses `n` (reject), two things happen:

1. **TUI visual feedback**: A rejection notice line is appended to the chat view:
   ```
   ⚠ Rejected: <tool_name> — user denied this operation
   ```

2. **Agent middleware return**: The tool middleware returns a structured rejection message that includes a reminder telling the LLM not to retry or use alternative approaches to circumvent the user's decision.

### 5.3 Module Synchronization

- TUI mode changes are propagated to Runner via a unidirectional channel (`autoApproveCh`)
- Environment switches (e.g., SSH to a new host) trigger `workpath` updates in approval state, so path detection recalculates against the new working directory

### 5.4 Status Bar Indicator

The status bar always shows the current approval mode:
- **Manual mode**: `Approve: Ask`
- **Auto mode**: `Approve: Auto` (rendered with warning style)

---

## 6. Path Detection Edge Cases

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

### 8.1 Integration Test Scenarios

| Scenario | Steps | Expected Result |
|----------|-------|-----------------|
| **Internal file read** | Agent calls `read` for a file within `workpath` | Executes directly, no dialog |
| **External sensitive file** | Agent tries to read `~/.ssh/id_rsa` | Dialog shows "External Path Access", requires manual approval |
| **ApproveOnce** | Press `y` in manual mode | Only current operation proceeds, status bar stays 🔒 |
| **ApproveAll** | Press `a` in manual mode | All subsequent operations auto-proceed, status bar shows 🔓 |
| **Mode toggle** | Press `Ctrl+A` at any time | 🔒 ↔ 🔓 toggles instantly |
| **Rejection feedback** | Press `n` to reject | TUI shows rejection notice; agent receives reminder not to retry |
| **Environment switch** | SSH to new workpath, then read file | Path detection recalculates against new `workpath` |

### 8.2 Manual Verification Checklist

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
