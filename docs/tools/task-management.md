---
title: Task Management
parent: Tools
nav_order: 4
---

# Task Management

## todowrite

Manage a live task list that tracks progress. The agent uses this to plan and track work.

**Approval:** Auto-approved.

The todo bar appears above the input area:

```
  📋 Todo (2/5)  [██░░░░░░░░]
  ✓ 1. Read current auth code
  ✓ 2. Identify the bug
  ⟳ 3. Fix the session handling
  ○ 4. Add test for the fix
  ○ 5. Run tests
```

Key features:
- Full-replacement: the agent sends the complete list each time
- Only one task can be "in progress" at a time
- Status values: `pending`, `in_progress`, `completed`, `cancelled`

## todoread

Read the current task list.

**Approval:** Auto-approved.

Returns the current todos with a summary count.

## Goals

A goal is a persistent, cross-turn objective. Once set, the agent keeps working
toward it across turns — when it would otherwise stop, it is automatically
reminded to continue until the objective is verifiably complete or it hits the
continuation cap. See [Goals]({% link goal.md %}) for the full workflow and the
`/goal` command.

These tools back the goal feature in every frontend (TUI, web, ACP):

| Tool | What It Does |
|---|---|
| `goal_set` | Set or replace the session goal. Takes `objective` (a clear, self-contained description of the desired end state, max 4000 chars). Replacing a goal resets accounting. |
| `goal_get` | Read the current goal — its objective, status, and token usage. Takes no parameters. Returns `No goal set.` when none exists. |
| `goal_update` | Mark the goal `complete` (objective verifiably done) or `blocked` (cannot proceed). Takes `status`. Either value stops the automatic continuation. |

**Approval:** Require approval (in **Ask for approval** mode).

A goal carries one of three statuses: `active` (the agent keeps working),
`complete`, or `blocked`. The agent is instructed to mark `complete` only when
the objective is verified against the real state of files, command output, or
tests — not its intent.

## Ask User

The agent can ask you questions when it needs clarification or a decision.

**Approval:** Auto-approved.

```
  ╭ Question ──────────────────────────────────────────╮
  │  Which database driver should I use?                │
  │                                                     │
  │  > pgx (Recommended) — Fast, native PostgreSQL      │
  │    lib/pq — Well-tested, widely used                │
  │    sqlx — Extended standard library                 │
  │    [Type your own answer]                           │
  ╰────────────────────────────────────────────────────╯
```

The agent can ask multiple questions at once and provide selectable options. You can always type a custom answer.
