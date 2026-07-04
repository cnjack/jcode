---
title: Sessions & Resume
parent: Overview
nav_order: 11
---

# Sessions & Resume

Every jcode conversation is automatically recorded. You can list past sessions and pick up exactly where you left off.

## How Sessions Work

- Each session is saved as a JSONL file at `~/.jcode/sessions/{uuid}.jsonl`
- Sessions are created **lazily** — no file is written until the first real message
- All interactions are recorded: messages, tool calls, plan updates, todo snapshots, mode changes
- Sessions are per-project — organized by your working directory

## List Sessions

```bash
jcode sessions
```

This shows all sessions for the current project:

```
  UUID: a1b2c3d4-...
  Time: 2026-03-12 14:30
  Model: openai/gpt-4o

  UUID: e5f6a7b8-...
  Time: 2026-03-11 09:15
  Model: openai/gpt-4o

  Run `jcode --resume <UUID>` to continue a session.
```

## Resume a Session

Pick up where you left off:

```bash
jcode --resume a1b2c3d4-...
```

Or from within the TUI, type `/resume` to open the session picker:

```
  ┌──────────────── Resume Session ─────────────────┐
  │  > 2026-03-12  gpt-4o      fix nginx crash       │
  │    2026-03-11  gpt-4o      refactor auth module  │
  │    2026-03-10  claude-3.5  add pagination logic  │
  └─────────────────────────────────────────────────┘
```

## What Gets Restored

When you resume a session, jcode reconstructs the full state:

- **Conversation history** — All messages between you and the agent
- **Todo list** — Any pending or completed tasks
- **Plan state** — Active plans and their approval status
- **Session mode** — Ask for approval, Plan, or Full access (a saved Plan resumes as Ask for approval; Full access resumes as-is)
- **Environment** — Local or SSH connection

{: .note }
Session recording is **compact-aware**. If context compaction occurred during the original session, the compacted summary is used instead of the full history. This keeps resumed sessions efficient.

## Session Storage

| Path | Contents |
|---|---|
| `~/.jcode/sessions/{uuid}.jsonl` | Session recording |
| `~/.jcode/sessions/session.json` | Session index (per-project) |
| `~/.jcode/sessions/{uuid}/subagents/` | Teammate session recordings |

## One-Shot Mode

For quick tasks, use the `-p` flag to send a single prompt and exit:

```bash
jcode -p "Explain what main.go does"
```

The agent processes your request, displays the result, and exits. The session is still recorded for later review.
