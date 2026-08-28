---
title: Task Management
parent: Tools
nav_order: 4
---

# Task Management

## Agent Tasks (cross-session)

Background subagent runs and durable work items are recorded in a persistent,
project-scoped task registry (`~/.jcode/tasks`). Every task gets a stable,
collision-free reference (`task_<16 hex>`) that survives sessions and restarts,
so you can pick up work from any later session in the same project.

```text
Background task started: task_9f2c41ab77e0d3f5 (local id bg_subagent_1)
Task reference: task_9f2c41ab77e0d3f5 — stable across sessions; use
task_read/task_get with this ref, task_message to follow up, and mention it
as @task_9f2c41ab77e0d3f5.
```

Tasks keep an append-only message timeline. Messages are delivered exactly
once per idempotency key (safe to retry), and messaging a task that is
archived or already finished fails with an explicit error instead of silently
dropping the text. If the process that owned a running task dies, reads
surface it as `failed` ("owning process exited") rather than spinning forever.

### `@task` mentions (TUI)

Type `@` plus a few characters of a task name (or ref) to get a completion
popup listing the tasks visible in your project — Tab/Enter accepts and
inserts the full `@task_…` reference. On submit, each mention is resolved and
attached to the prompt as a clearly fenced, untrusted-data context block;
unknown or ambiguous mentions block the send with an explicit error.

Archived tasks never appear in completion and mentions of them report
"archived". A task from another project is never readable or mentionable.

### `/task` commands (TUI)

| Command | Effect |
|---|---|
| `/task` or `/task list` | List this project's tasks with refs and statuses |
| `/task create <name> [description]` | Create a durable work item |
| `/task read <ref\|name>` | Status, output, error and message timeline |
| `/task message <ref\|name> <text>` | Append a message to the task timeline |
| `/task stop <ref\|name>` | Stop a task running in this session (foreign owners are refused with an explanation) |
| `/task archive <ref\|name>` | Soft-delete a finished task |

### Agent tools

| Tool | Approval | What it does |
|---|---|---|
| `task_list` | Auto | List tasks in this project (cross-session), filterable by status |
| `task_get` | Auto | Status, output, error and timeline for a ref/name |
| `task_read` | Auto | Read any task in the persistent registry, including from earlier sessions |
| `task_create` | Auto | Create a durable work item |
| `task_message` | Requires approval | Send a message to a task timeline (exactly-once per `idempotency_key`) |
| `task_stop` | Requires approval | Stop a running task; refuses tasks owned by other sessions with an explicit error |

### Web / Desktop

The same registry is exposed over REST on the engine's server:

| Endpoint | Effect |
|---|---|
| `GET /api/agent-tasks?status=…` | List |
| `POST /api/agent-tasks` `{name, description}` | Create |
| `GET /api/agent-tasks/{ref}` | Read (timeline included) |
| `POST /api/agent-tasks/{ref}/messages` `{message, idempotency_key}` | Message |
| `POST /api/agent-tasks/{ref}/stop` | Stop (live engines only; foreign owners → 409) |
| `POST /api/agent-tasks/{ref}/archive` | Archive |

Remote (SSH/Docker) and cloud sessions own their own registries on their own
machine; a local ref is never treated as a remote one — unknown local refs
report that they may belong to another session, machine, or executor.

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
