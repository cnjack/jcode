---
title: Subagents
parent: Overview
nav_order: 5
---

# Subagents

Subagents let the main agent delegate subtasks to independent child agents. Each subagent runs with its own tools and conversation, then reports results back to the parent.

## When to Use Subagents

The agent automatically uses subagents when it needs to:
- **Research** a specific part of the codebase
- **Work on multiple things in parallel**
- **Break a complex task into smaller pieces**

You don't need to explicitly invoke subagents — the agent decides when delegation is appropriate.

## Subagent Types

| Type | Capabilities | Best For |
|---|---|---|
| **explore** | Read-only: `read`, `grep`, `execute` (safe only) | Researching code, answering questions, finding files |
| **general** | Full tools: read, write, edit, execute, search | Completing specific tasks like fixing a bug or writing a function |
| **coordinator** | General tools + can spawn child subagents | Breaking down complex tasks and managing parallel work |

## How They Work

1. The parent agent creates a subagent with a specific task
2. The subagent works independently — its intermediate steps don't pollute the parent's context
3. Only the **final result** is returned to the parent agent
4. Progress is shown in the TUI with a bordered subagent panel

```
  ╭ Subagent: find-auth-bug ──────────────────────────╮
  │  ⚙ read  auth/handler.go                          │
  │  ⚙ grep  pattern="session"  path=auth/            │
  │  ⚙ read  auth/middleware.go                       │
  ╰────────────────────────────────────────────────────╯
```

## Background Subagents

Subagents can run in the **background**, allowing the parent agent to continue working:

- The subagent starts and returns a task ID immediately
- The parent can check status later with `check_background`
- Multiple background subagents can run **in parallel**

This is useful when multiple independent tasks need to happen simultaneously.

## Nesting Depth

Subagents can be nested up to **3 levels deep**:
- Level 0: Main agent
- Level 1: Subagent spawned by main agent
- Level 2: Sub-subagent spawned by a coordinator
- Level 3: Maximum nesting depth

## Configuration

Control subagent behavior in your config:

```json
{
  "subagent": {
    "max_parallel": 3,
    "max_completed": 10,
    "max_depth": 3
  }
}
```

| Setting | Default | Description |
|---|---|---|
| `max_parallel` | — | Maximum concurrent subagents |
| `max_completed` | — | How many completed tasks to keep in memory |
| `max_depth` | 3 | Maximum nesting depth |
