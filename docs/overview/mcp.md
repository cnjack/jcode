---
title: MCP Integration
parent: Overview
nav_order: 12
---

# MCP Integration

Connect any [Model Context Protocol](https://modelcontextprotocol.io/)-compatible server to extend jcode's capabilities. MCP servers provide additional tools that merge seamlessly with the built-ins.

## What Is MCP?

The Model Context Protocol (MCP) is an open protocol that allows AI models to safely interact with external tools and data sources. With MCP, jcode can:

- Query databases
- Search documentation
- Manage cloud resources
- Interact with APIs (GitHub, Linear, Notion, etc.)
- Control browsers or other applications
- And much more

jcode has built-in tools (file read/write, shell commands, search, etc.). Through MCP, you can add more tools without modifying jcode itself.

## MCP Server Management

### Add a Server

#### Via Command Line

```bash
# Add an HTTP/SSE server
jcode mcp add context7 https://mcp.context7.com/mcp

# Add with custom headers
jcode mcp add context7 https://mcp.context7.com/mcp \
  --header "CONTEXT7_API_KEY: your-key"

# Add a stdio server (local process)
jcode mcp add chrome-devtools -- npx chrome-devtools-mcp@latest

# Specify server type explicitly
jcode mcp add db -t http http://localhost:3001/mcp
```

#### Via Config File

Add servers to `~/.jcode/config.json`:

```json
{
  "mcp_servers": {
    "github": {
      "type": "stdio",
      "command": "gh-mcp"
    },
    "context7": {
      "type": "http",
      "url": "https://mcp.context7.com/mcp",
      "headers": {
        "CONTEXT7_API_KEY": "your-key"
      }
    },
    "remote-api": {
      "type": "sse",
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer token..."
      }
    },
    "chrome-devtools": {
      "type": "stdio",
      "command": "npx",
      "args": ["chrome-devtools-mcp@latest"],
      "env": ["SOME_VAR=value"]
    }
  }
}
```

### List Servers

```bash
jcode mcp list
```

### Check Connectivity

```bash
jcode doctor
```

## Server Types

| Type | How It Works | Use Case |
|---|---|---|
| **stdio** | Runs a local subprocess, communicates via stdin/stdout | Local tools like `gh-mcp`, `npx` packages |
| **http** | Connects to an HTTP endpoint | Self-hosted MCP servers |
| **sse** | Server-Sent Events stream | Remote/cloud MCP servers |

{: .note }
When using `jcode mcp add`, the server type is **auto-detected** from the URL. Use `--type` / `-t` to override.

## Server Configuration

### stdio Servers

| Field | Description |
|---|---|
| `command` | Executable to run |
| `args` | Command arguments (array of strings) |
| `env` | Environment variables (`["KEY=VALUE"]`) |

### http / sse Servers

| Field | Description |
|---|---|
| `url` | Server URL |
| `headers` | Custom HTTP headers (map of key-value pairs) |

## Loading Status

MCP servers initialize **asynchronously** after startup, so the TUI is usable immediately. The status bar shows live connection progress:

```
  Agent │ Model: openai / gpt-4o │ Approve: Ask │ [████░░░░░░] 2% │ MCP: 2/5
```

This means 2 out of 5 configured MCP servers are connected. Once all servers connect, the status updates automatically.

{: .note }
MCP servers auto-reconnect with exponential backoff if they disconnect. Check `jcode doctor` for detailed connection status.

## Using MCP Tools

Once connected, MCP tools appear alongside built-in tools. The agent can use them just like any other tool:

```
  ⚙ Tool  mcp_github_search_issues  query="memory leak"

  ╭─────────────────────────────────────────────────────╮
  │  Found 3 issues:                                    │
  │  #142 - Memory leak in connection pool              │
  │  #98  - RSS grows over time                         │
  │  #45  - Large payloads not released                 │
  ╰─────────────────────────────────────────────────────╯
```

## Security

MCP tools may access and operate external systems. Be aware of security implications.

### Approval Mechanism

jcode requests user confirmation for sensitive operations (file modifications, command execution). MCP tools follow the **same approval mechanism** — all MCP tool calls prompt for confirmation by default.

### Prompt Injection Risks

Content returned by MCP tools may contain malicious instructions attempting to trick the AI into performing dangerous operations. To stay safe:

- Only use MCP servers from **trusted sources**
- Review whether AI-proposed operations are reasonable
- Keep **manual approval** enabled for high-risk operations

{: .warning }
In unsafe mode (`--unsafe`), MCP tool operations are also auto-approved. Only use unsafe mode when you fully trust all configured MCP servers.

## Quick Reference

| Action | Command |
|---|---|
| Add a server | `jcode mcp add <name> <url-or-command>` |
| Add with headers | `jcode mcp add <name> <url> --header "Key: Value"` |
| Add stdio server | `jcode mcp add <name> -- <command> [args...]` |
| List servers | `jcode mcp list` |
| Check connectivity | `jcode doctor` |
| View in web UI | `GET /api/mcp` |
