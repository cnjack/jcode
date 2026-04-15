---
title: MCP Integration
parent: Overview
nav_order: 12
---

# MCP Integration

Connect any [Model Context Protocol](https://modelcontextprotocol.io/)-compatible server to extend jcode's capabilities. MCP servers provide additional tools that merge seamlessly with the built-ins.

## What Is MCP?

The Model Context Protocol (MCP) is a standard for connecting AI tools to external services. With MCP, jcode can:

- Query databases
- Search documentation
- Manage cloud resources
- Interact with APIs
- And much more

## Add an MCP Server

### Via Command Line

```bash
jcode mcp add github gh-mcp
jcode mcp add db http://localhost:3001/mcp
```

### Via Config File

Add servers to `~/.jcode/config.json`:

```json
{
  "mcp_servers": {
    "github": {
      "type": "stdio",
      "command": "gh-mcp"
    },
    "db": {
      "type": "http",
      "url": "http://localhost:3001/mcp"
    },
    "remote-api": {
      "type": "sse",
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer token..."
      }
    }
  }
}
```

## Server Types

| Type | How It Works | Use Case |
|---|---|---|
| **stdio** | Runs a local subprocess | Local tools like `gh-mcp`, database clients |
| **http** | Connects to an HTTP endpoint | Self-hosted MCP servers |
| **sse** | Server-Sent Events stream | Remote/cloud MCP servers |

## Server Configuration

### stdio servers

| Field | Description |
|---|---|
| `command` | Executable to run |
| `args` | Command arguments |
| `env` | Environment variables (`["KEY=VALUE"]`) |

### http / sse servers

| Field | Description |
|---|---|
| `url` | Server URL |
| `headers` | Custom HTTP headers (map) |

## MCP Server Status

The status bar shows connected MCP servers:

```
  Agent │ Model: openai / gpt-4o │ Approve: Ask │ [████░░░░░░] 2% │ MCP: 2/5
```

This means 2 out of 5 configured MCP servers are connected.

{: .note }
MCP servers auto-reconnect with exponential backoff if they disconnect. Check `jcode doctor` for connection status.

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

## Managing MCP Servers

| Action | How |
|---|---|
| List servers | `jcode mcp list` |
| Add a server | `jcode mcp add <name> <url>` |
| Check connectivity | `jcode doctor` |
| View in web UI | `GET /api/mcp` |
