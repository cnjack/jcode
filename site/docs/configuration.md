---
title: Configuration
nav_order: 7
---

# Configuration

jcode stores all configuration in a single JSON file at `~/.jcode/config.json`. The setup wizard creates this file on first launch.

## Config File Location

```
~/.jcode/
├── config.json          # Main configuration
├── debug.log            # Application diagnostics
├── AGENTS.md            # Global custom instructions
├── history              # Command history
├── sessions/            # Session recordings
│   ├── session.json     # Session index
│   └── {uuid}.jsonl     # Individual sessions
├── skills/              # User-installed skills
│   └── {name}/SKILL.md
├── teams/               # Team state
│   └── {name}/
└── storage/             # Persistent data
```

## Minimal Config

```json
{
  "providers": {
    "openai": {
      "api_key": "sk-..."
    }
  },
  "model": "openai/gpt-4o"
}
```

## Full Config Reference

```json
{
  "providers": {
    "openai": {
      "api_key": "sk-...",
      "base_url": "https://api.openai.com/v1"
    },
    "anthropic": {
      "api_key": "sk-ant-..."
    }
  },
  "model": "openai/gpt-4o",
  "small_model": "openai/gpt-4o-mini",
  "max_iterations": 1000,
  "default_mode": "approval",

  "context_limits": {
    "openai/gpt-4o": 128000,
    "my-custom-model": 256000
  },
  "default_context_limit": 200000,

  "ssh_aliases": [
    { "name": "prod", "addr": "deploy@10.0.1.5", "path": "/var/www/app" }
  ],

  "docker_aliases": [
    { "name": "devbox", "container": "my-dev-container", "path": "/workspace" }
  ],

  "mcp_servers": {
    "github": { "type": "stdio", "command": "gh-mcp" },
    "db": { "type": "http", "url": "http://localhost:3001/mcp" }
  },

  "budget": {
    "max_tokens_per_turn": 100000,
    "max_cost_per_session": 5.00,
    "warning_threshold": 0.8
  },

  "compaction": {
    "enabled": true,
    "threshold": 0.75,
    "keep_recent": 6
  },

  "prompt": {
    "memory_max_chars": 40000,
    "memory_max_depth": 5,
    "cache_enabled": true,
    "async_env_timeout": "5s"
  },

  "subagent": {
    "max_parallel": 3,
    "max_completed": 10,
    "max_depth": 3
  },

  "team": {
    "max_teammates": 5,
    "mailbox_poll_ms": 500,
    "message_cap": 50
  },

  "memory": {
    "enabled": true,
    "generate": true,
    "daily_token_budget": 300000,
    "cooldown_hours": 6,
    "summary_inject_tokens": 1200
  },

  "telemetry": {
    "langfuse": {
      "LANGFUSE_BASE_URL": "https://cloud.langfuse.com",
      "LANGFUSE_PUBLIC_KEY": "pk-...",
      "LANGFUSE_SECRET_KEY": "sk-..."
    }
  },

  "channel": {
    "web_enabled": true,
    "ble_enabled": false
  },

  "disabled_providers": []
}
```

## Configuration Sections

### providers

Map of provider name to provider config. Each provider needs:

| Field | Required | Description |
|---|---|---|
| `api_key` | Yes | Your API key |
| `base_url` | No | Custom base URL (defaults to the provider's standard endpoint) |

### model

Active model in `"provider/model"` format.

| Field | Description |
|---|---|
| `model` | Primary model for all interactions |
| `small_model` | Optional lightweight model. Powers the subagent `"small"` model alias (cheap delegated subtasks) and LLM session-title generation. Unset → subagents use the parent model and titles stay truncated first messages |
| `max_iterations` | Maximum agent iterations per turn (default: 1000) |

### context_limits

Per-model overrides for the context window size (in tokens). Use this to teach
jcode the window of a brand-new or custom model the registry doesn't know yet.
Keys may be `"provider/model"` (preferred) or a bare model id; the
`"provider/model"` form is checked first.

```json
{
  "context_limits": {
    "openai/gpt-4o": 128000,
    "my-custom-model": 256000
  }
}
```

An explicit `context_limits` entry takes precedence over the models.dev registry
and built-in tables. When no override matches and the limit is still unknown,
jcode falls back to [`default_context_limit`](#default_context_limit).

### default_context_limit

Fallback context window (in tokens) assumed when a model's limit can't be
determined from `context_limits`, the models.dev registry, or the built-in
tables. Default: `200000`.

```json
{ "default_context_limit": 200000 }
```

### ssh_aliases

Named SSH connections for quick access.

| Field | Description |
|---|---|
| `name` | Alias name shown in the `/ssh` picker |
| `addr` | Connection address (`user@host[:port]`) |
| `path` | Remote working directory |

### docker_aliases

Named Docker container workspaces for quick access. Fields mirror
[`ssh_aliases`](#ssh_aliases).

| Field | Description |
|---|---|
| `name` | Alias name shown in the picker |
| `container` | Container name or id |
| `path` | Working directory inside the container |

### mcp_servers

MCP server definitions. See [MCP Integration](mcp).

### budget

Token and cost limits per session.

| Field | Description |
|---|---|
| `max_tokens_per_turn` | Maximum tokens per agent turn |
| `max_cost_per_session` | Maximum cost in dollars |
| `warning_threshold` | Fraction (0-1) for budget warnings |

### compaction

Auto context compaction settings.

| Field | Default | Description |
|---|---|---|
| `enabled` | false | Enable auto-compaction |
| `threshold` | 0.75 | Context fraction that triggers compaction |
| `keep_recent` | 6 | Recent messages to preserve |

Compaction always runs on the session's main model — summary quality directly
bounds the agent's post-compaction performance, so it is deliberately not
routed to `small_model`.

### subagent

Subagent behavior limits.

| Field | Default | Description |
|---|---|---|
| `max_parallel` | — | Maximum concurrent subagents |
| `max_completed` | — | Completed tasks to retain |
| `max_depth` | 3 | Maximum nesting depth |

### team

Multi-agent team settings.

| Field | Default | Description |
|---|---|---|
| `max_teammates` | 5 | Maximum teammates per team |
| `mailbox_poll_ms` | 500 | Mailbox polling interval |
| `message_cap` | 50 | Messages displayed per teammate |

### memory

Cross-session learned memory. Works with zero config; all fields optional. See
[Project Memory]({% link overview/learned-memory.md %}) for the full picture.

| Field | Default | Description |
|---|---|---|
| `enabled` | `true` | Master switch for reading and writing memory |
| `generate` | `true` | `false` keeps notes + reading but disables the distillation pipeline |
| `model` | main `model` | Model used for extraction (`provider/model`). Not routed through `small_model`: memories persist across sessions, so extraction quality matters more than token cost |
| `daily_token_budget` | `300000` | Hard cap on tokens the pipeline may spend per day |
| `cooldown_hours` | `6` | Minimum gap between automatic pipeline runs |
| `max_age_days` | `30` | Only sessions newer than this are extracted |
| `max_unused_days` | `45` | Summaries unused this long are forgotten |
| `phase2_top_n` | `40` | Max summaries kept after consolidation ranking |
| `summary_inject_tokens` | `1200` | Cap on the memory summary injected into the prompt |

### default_mode

The session mode jcode starts in: `"approval"` (default), `"plan"`, or `"full_access"`. Applies to the TUI, web, and ACP frontends. The `--unsafe` flag overrides this and forces `full_access`.

```json
{ "default_mode": "approval" }
```

{: .note }
**Migration:** the old mode IDs `ask`, `agent`, and `autopilot` are no longer accepted — use `approval`, `plan`, and `full_access` respectively. An unrecognized value falls back to `approval`.

### auto_approve

{: .warning }
Deprecated — superseded by [`default_mode`](#default_mode) (`approval` / `plan` / `full_access`). Still honored only as a fallback when `default_mode` is unset: `true` maps to `full_access`. The old mode IDs `ask` / `agent` / `autopilot` are no longer accepted.

Set to `true` to auto-approve all tool calls. Equivalent to running with the `--unsafe` flag.

### theme

The built-in color theme for the **terminal UI**: `"jcode-dark"` (default), `"midnight"`, `"dracula"`, `"nord-dark"`, `"jcode-light"`, `"github-light"`, or `"solarized-light"`. Set it interactively with the [`/theme`](commands.html#slash-commands) command (which persists it here), or edit it directly. When unset, jcode auto-selects a light or dark default from the detected terminal background.

```json
{ "theme": "nord-dark" }
```

{: .note }
The web UI has its own theme preference (stored in the browser); the two are independent. Both share the same catalog of built-in themes, generated from a single source (`internal/theme`).

### disabled_skills

A list of skill names to exclude from the agent (slash commands, system-prompt
descriptions, and the `load_skill` tool). Toggle skills on/off from the web UI
(**Settings → Skills**); it persists here.

```json
{ "disabled_skills": ["pptx", "docx"] }
```

### channel

Notification channel settings. See [Channels](overview/channels).

| Field | Default | Description |
|---|---|---|
| `web_enabled` | false | Auto-enable WeChat channel in `jcode web` mode |
| `ble_enabled` | false | Enable BLE device notifications for JCode Buddy |

In TUI mode, channels are always available via `/channel` — no config needed.

## Changing Configuration

- **Setup wizard**: Run `jcode` and the wizard launches if config is missing
- **TUI**: Type `/setting` to open the settings menu
- **Manual**: Edit `~/.jcode/config.json` directly (changes are hot-reloaded)
- **Model picker**: Press **Ctrl+L** to switch models mid-session
