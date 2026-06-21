---
title: Tools
nav_order: 4
has_children: true
---

# Tools

jcode comes with a comprehensive set of built-in tools that the agent uses to interact with your codebase and environment. Every tool call is visible to you in the TUI.

## How Tools Work

When the agent decides to take an action, it invokes a tool. Each tool call appears in the conversation with:

- The **tool name** and key parameters
- A **live indicator** while running
- The **result** when complete

```
  ⚙ Tool  edit   path=server.go

  ╭─────────────────────────────────────────────────────╮
  │  - go handle(conn)                                  │
  │  + wg.Add(1)                                        │
  │  + go func() { defer wg.Done(); handle(conn) }()   │
  ╰─────────────────────────────────────────────────────╯
     ✓ Edit applied
```

## Approval & Safety

Not all tool calls require your approval. jcode categorizes tools by risk:

| Category | Tools | Approval |
|---|---|---|
| **Read-only** | `read`, `grep`, `glob` | Auto-approved (within project) |
| **Safe commands** | `ls`, `pwd`, `git status`, `git log` | Auto-approved |
| **Management** | `todowrite`, `todoread`, `ask_user` | Auto-approved |
| **Delegation** | `subagent`, `check_background` | Auto-approved |
| **Team** | `team_create`, `team_spawn`, `team_send_message`, `team_list`, `team_delete` | Auto-approved |
| **File edits** | `edit`, `write` | **Require approval** |
| **Commands** | `execute` (non-safe) | **Require approval** |
| **Environment** | `switch_env` | **Require approval** |

When a tool requires approval (in **Ask for approval** mode), you see a dialog with the tool name and arguments. You can:
- **Y** — Approve this action
- **A** — Approve all remaining actions (switches the session to **Full access**)
- **N** — Reject

{: .tip }
To auto-approve everything up front, switch to **Full access** mode with **Shift+Tab** (cycles Ask for approval → Plan → Full access). In Full access all tool calls are approved automatically — use with caution.

## Tool Inventory

| Tool | What It Does |
|---|---|
| **read** | Read file contents with line numbers. Also lists directories. |
| **edit** | Make precise string replacements in files. Supports multi-edit mode. |
| **write** | Write or overwrite file contents. |
| **execute** | Run shell commands. Background mode for long-running tasks. |
| **grep** | Search for patterns across your codebase. Uses ripgrep when available. |
| **glob** | Find files by name pattern. |
| **todowrite** | Manage a live task list. |
| **todoread** | Read the current task list. |
| **subagent** | Delegate a task to a child agent. |
| **check_background** | Check status of background tasks. |
| **ask_user** | Ask the user a question with optional choices. |
| **switch_env** | Switch between local and SSH environments. |
| **load_skill** | Load a skill's full instructions. |
| **MCP tools** | Dynamically loaded from configured MCP servers. |
