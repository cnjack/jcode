---
title: Commands & Shortcuts
nav_order: 7
---

# Commands & Shortcuts

## Command Line

### Subcommands

| Command | Description |
|---|---|
| `jcode` | Start the interactive TUI (default) |
| `jcode web` | Start the web interface |
| `jcode acp` | Start headless JSON-RPC server (for editor integration) |
| `jcode doctor` | Verify model and MCP connectivity |
| `jcode version` | Show version, build time, and commit |
| `jcode update` | Update jcode to the latest version |
| `jcode sessions` | List recorded sessions for this project |
| `jcode mcp add <name> <url>` | Add an MCP server |
| `jcode mcp list` | List configured MCP servers |

### Flags

| Flag | Short | Description |
|---|---|---|
| `--prompt <text>` | `-p` | One-shot mode: run a single prompt and exit |
| `--resume <UUID>` | | Resume a previous session |
| `--unsafe` | | Auto-approve all tool calls |
| `--doctor` | | Run system health check |
| `--version` | | Print version info |

### Web Server Flags

| Flag | Default | Description |
|---|---|---|
| `--port` | 8080 | HTTP port for web interface |
| `--host` | 127.0.0.1 | Server bind address |

### MCP Add Flags

| Flag | Short | Description |
|---|---|---|
| `--type` | `-t` | Server type: `sse`, `http`, or `stdio` (auto-detected) |
| `--header` | | HTTP header in `Key: Value` format (repeatable) |
| `--env` | | Environment variable in `KEY=VALUE` format (repeatable) |

## Keyboard Shortcuts (TUI)

| Shortcut | Action |
|---|---|
| **Enter** | Submit prompt |
| **Shift+Enter** | Insert new line |
| **Shift+Tab** | Cycle session mode (Ask → Plan → Autopilot) |
| **Ctrl+L** | Open model picker |
| **Ctrl+C** | Exit (press once for confirmation, twice to force) |
| **Ctrl+T** | Toggle team coordinator panel |
| **Ctrl+Y** | Copy last assistant message to clipboard |
| **Shift+Up/Down** | Switch between teammate views |
| **Up/Down** | Navigate prompt history |
| **Esc** | Clear selection / exit teammate view / cancel dialog |
| **Right-click** | Paste from clipboard |

## Slash Commands

Type these in the TUI input area:

| Command | Description |
|---|---|
| `/model` | Switch model mid-session |
| `/setting` | Open settings menu |
| `/ssh` | Open SSH connection wizard |
| `/resume` | Resume a previous session |
| `/compact` | Compact conversation context |
| `/goal` | Set a persistent objective the agent works toward ([Goals](goal.html)) |
| `/bg` | Show background tasks |
| `/review-pr` | Run PR review skill |
| `/pr-comments` | Fetch PR comments |
| `/security-review` | Run security review |
| `/<custom-skill>` | Any skill with a slash command |

## One-Shot Mode

For quick tasks without entering the interactive TUI:

```bash
jcode -p "Explain what main.go does"
jcode -p "Fix the TODO in auth.go"
```

The agent processes the request, displays output, and exits. The session is saved for later review.

## Headless Mode (ACP)

For editor and tool integration:

```bash
jcode acp
```

Starts a JSON-RPC server on stdio using the Agent Communication Protocol. Designed for programmatic access — no TUI.
