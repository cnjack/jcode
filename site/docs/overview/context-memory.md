---
title: Context & Memory
parent: Overview
nav_order: 12
---

# Context & Memory

jcode automatically understands your project and provides the agent with rich context. You can also customize behavior through AGENTS.md files.

## Automatic Context

When jcode starts, it detects and provides to the agent:

| Context | How It's Detected |
|---|---|
| **Working directory** | Current directory |
| **Platform** | OS and architecture |
| **Git branch** | `git rev-parse --abbrev-ref HEAD` |
| **Git status** | Uncommitted changes (dirty/clean) |
| **Last commit** | `git log -1` |
| **Project type** | Detected from files like `go.mod`, `package.json`, `Cargo.toml`, etc. |
| **Directory tree** | Shallow tree (depth 2, max 200 lines) |
| **Environment** | Local or SSH, with labels |
| **Skills** | Available skill packs and slash commands |

## AGENTS.md — Custom Instructions

Use `AGENTS.md` files to give the agent project-specific instructions. These are loaded automatically and injected into the agent's context.

### Where to Place AGENTS.md

Three levels, all merged together:

| Priority | File | Scope |
|---|---|---|
| 1 (lowest) | `~/.jcode/AGENTS.md` | Global — applies to all projects |
| 2 | `./AGENTS.md` | Project-level — shared with team |
| 3 (highest) | `./AGENTS.local.md` | Personal — typically git-ignored |

### Example AGENTS.md

```markdown
# Project Conventions

- Use Go 1.22+ features (range over int, etc.)
- Follow the existing error wrapping pattern with `fmt.Errorf`
- All public functions must have doc comments
- Run `make lint` before considering any task complete

# Architecture

- `cmd/jcode/` — Entry points
- `internal/` — All internal packages
- `web/` — Vue 3 frontend

# Testing

- Run tests with `make test`
- Integration tests are in `tests/` directory
```

### Include Directives

AGENTS.md supports `@include` directives to reference other files:

```markdown
# Project Conventions

@include ./docs/conventions/coding-style.md
@include ./docs/conventions/testing.md
```

- Includes are resolved relative to the file's directory
- Recursion depth limited to 5 levels
- Circular includes are detected and blocked
- Total content is capped at 40,000 characters

## Context Compaction

When the conversation gets long, jcode automatically summarizes older messages while keeping recent ones intact.

### Auto-Compaction

Configure when compaction triggers:

```json
{
  "compaction": {
    "enabled": true,
    "threshold": 0.75,
    "keep_recent": 6,
    "summary_model": "openai/gpt-4o-mini"
  }
}
```

| Setting | Default | Description |
|---|---|---|
| `enabled` | — | Enable auto-compaction |
| `threshold` | 0.75 | Trigger compaction when context reaches this fraction |
| `keep_recent` | 6 | Number of recent messages to preserve |
| `summary_model` | — | Model to use for generating summaries |

### Manual Compaction

Type `/compact` in the TUI to trigger compaction at any time.

## Smart Prompt Caching

Enable prompt caching to reduce redundant computation across turns:

```json
{
  "prompt": {
    "cache_enabled": true
  }
}
```
