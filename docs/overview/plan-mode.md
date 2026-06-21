---
title: Plan Mode
parent: Overview
nav_order: 8
---

# Plan Mode

Plan Mode lets the agent explore your codebase **read-only** and present a structured plan before making any changes. You review the plan, approve or reject it, and then the agent executes step by step.

## When to Use Plan Mode

- **Complex refactors** — Understand the scope before touching files
- **Unfamiliar codebases** — Let the agent explore and propose an approach
- **Risky changes** — Review the plan to catch issues before execution
- **Multi-step tasks** — See the full scope before committing

## How to Use Plan Mode

### 1. Enter Plan Mode

Press **Shift+Tab** in the TUI to cycle the mode selector to **Plan** (Ask for approval → Plan → Full access), or type a task and ask the agent to plan first.

The mode pill changes to indicate Plan:

```
  Plan │ Model: openai / gpt-4o │ [██░░░░░░░░] 12%
```

### 2. Agent Explores

In Plan Mode, the agent can only:
- Read files
- Search code
- Run safe commands (`ls`, `pwd`, `git status`, etc.)
- Ask you questions

It cannot modify files or run arbitrary commands.

### 3. Review the Plan

The agent generates a structured plan and presents it for your review:

```
  ╭ Plan Review ──────────────────────────────────────╮
  │                                                     │
  │  ## Plan: Add Pagination to User List              │
  │                                                     │
  │  1. Add pagination parameters to handler           │
  │  2. Update database query with LIMIT/OFFSET        │
  │  3. Add pagination metadata to response            │
  │  4. Update tests                                   │
  │                                                     │
  │  [Y] Approve  [N] Reject  [Esc] Dismiss            │
  ╰─────────────────────────────────────────────────────╯
```

### 4. Approve or Reject

- **Y** — Approve the plan. The agent extracts todo items and starts executing.
- **N** — Reject with feedback. The agent revises the plan.
- **Esc** — Dismiss without action.

### 5. Execution

After approval, the agent:
1. Extracts todo items from the plan steps
2. Switches to Executing Mode (full tools)
3. Works through each step, tracking progress with the todo bar
4. Automatically returns to Normal Mode when all todos are complete

## Plan Lifecycle

```
Planning → Generate Plan → Review → Approve/Reject → Executing → Complete → Normal
                              ↑           │
                              └── Revise ←┘ (if rejected)
```

## Tips

{: .tip }
- If the plan isn't what you expected, **reject with feedback** — the agent will revise it
- You can cycle between Ask for approval, Plan, and Full access with **Shift+Tab** at any time
- Plans are parsed from numbered steps (`1.`) and checkboxes (`- [ ]`)
