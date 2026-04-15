---
title: Agent
parent: Overview
nav_order: 4
---

# Agent

The agent is the core of jcode. It's the AI that understands your requests, reasons about your codebase, and takes actions using tools.

## How the Agent Works

1. You send a **message** describing what you want
2. The agent **reasons** about the task using your project context
3. It **invokes tools** — reading files, searching code, editing, running commands
4. Each tool call is **shown to you** for approval (if required)
5. The agent **reports results** and continues until the task is done

## Agent Modes

jcode operates in three modes:

### Normal Mode

The default mode. The agent has access to all tools and works on your task directly.

```
  Agent │ Model: openai / gpt-4o │ Approve: Ask │ [████░░░░░░] 2%
```

### Plan Mode

Press **Ctrl+P** to enter Plan Mode. The agent explores your codebase **read-only** — it can read files and run safe commands, but cannot modify anything. It generates a structured plan for your review.

{: .note }
Plan Mode is ideal for complex tasks where you want to review the approach before making changes.

```
  Plan │ Model: openai / gpt-4o │ Approve: Ask │ [██░░░░░░░░] 12%
```

### Executing Mode

After you approve a plan, the agent switches to Executing Mode. It follows the plan step by step, tracking progress with the todo list. When all tasks are complete, it automatically returns to Normal Mode.

## Context Awareness

When jcode starts, it automatically detects:

- **Git state** — current branch, uncommitted changes, last commit
- **Project type** — Go, Python, JavaScript, Rust, Java, etc.
- **Directory structure** — shallow tree of your project
- **Environment** — local or SSH, platform and architecture
- **Available skills** — loaded from your skill packs
- **Custom instructions** — from AGENTS.md files

This context is injected into every conversation so the agent understands your project without manual configuration.

## Token Usage & Budget

The status bar shows a **color-coded progress bar** indicating context window usage:

| Color | Usage | Meaning |
|---|---|---|
| Green | < 70% | Comfortable — plenty of context |
| Orange | 70-90% | Approaching limit |
| Red | > 90% | Near limit — auto-compaction may trigger |

### Cost Guardrails

Set budget limits in your config:

```json
{
  "budget": {
    "max_cost_per_session": 5.00,
    "warning_threshold": 0.8
  }
}
```

The agent receives warnings when approaching limits and stops if the budget is exceeded.

## Context Compaction

When the context window fills up, jcode automatically summarizes older conversation while preserving recent messages. You can also trigger manual compaction:

- Type `/compact` in the TUI
- The agent compacts and frees context for continued work

## Error Recovery

The agent handles common errors automatically:

- **Rate limits** — retries with exponential backoff (up to 5 retries)
- **Context overflow** — compresses context and continues
- **Truncated output** — asks the model to continue
- **Network errors** — retries transient failures
