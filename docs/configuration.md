---
title: Configuration
nav_order: 6
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
  "fallback_model": "anthropic/claude-3-5-sonnet",
  "max_iterations": 1000,
  "auto_approve": false,

  "ssh_aliases": [
    { "name": "prod", "addr": "deploy@10.0.1.5", "path": "/var/www/app" }
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
    "keep_recent": 6,
    "summary_model": "openai/gpt-4o-mini"
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
| `small_model` | Lightweight model for summaries and compaction |
| `fallback_model` | Used when the primary model fails |
| `max_iterations` | Maximum agent iterations per turn (default: 1000) |

### ssh_aliases

Named SSH connections for quick access.

| Field | Description |
|---|---|
| `name` | Alias name shown in the `/ssh` picker |
| `addr` | Connection address (`user@host[:port]`) |
| `path` | Remote working directory |

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
| `summary_model` | — | Model for summaries (uses `small_model` if unset) |

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

### auto_approve

Set to `true` to auto-approve all tool calls. Equivalent to running with `--unsafe` flag.

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
