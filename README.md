<div align="center">

# jcode

**AI Coding Agent in Your Terminal**

Read files · Edit code · Run commands · Manage tasks — all driven by natural language, right where your code lives.

Works locally and on remote servers over SSH. Supports any OpenAI-compatible model.

[Install](#install) · [Features](#features) · [Agent Teams](#-agent-teams) · [SSH](#-ssh--work-on-any-machine) · [Config](#configuration)

</div>

---

```
 ◆ Found it — the goroutine in handleConnection() is never joined.
   I'll patch it now.

   ⚙ Tool  edit   path=server.go

   ╭─────────────────────────────────────────────────────╮
   │  - go handle(conn)                                  │
   │  + wg.Add(1)                                        │
   │  + go func() { defer wg.Done(); handle(conn) }()   │
   ╰─────────────────────────────────────────────────────╯
      ✓ Edit applied

──────────────────────────────────────────────────────────
 > _
──────────────────────────────────────────────────────────
  Agent │ Model: openai / gpt-4o │ Approve: Ask │ [████░░░░░░] 2% │ MCP: 2/5
```

## Install

```bash
go install github.com/cnjack/jcode/cmd/jcode@latest
```

First launch creates `~/.jcode/config.json` with a setup wizard. Run `jcode --doctor` to verify model & MCP connectivity.

## Features

### Core Agent Loop

Describe a task in plain English. The agent reads your codebase, writes surgical edits, runs commands, and reports every step — no black boxes.

| Capability | How it works |
|---|---|
| **File operations** | Read, edit (string-level diffs), and write files with inline before/after display |
| **Shell execution** | Run any command; output shown in a bordered box. Safe commands (`ls`, `git status`, …) auto-approved |
| **Regex search** | `grep` tool with ripgrep fallback — search across entire codebases in seconds |
| **Todo tracking** | Live `📋 Todo (2/5)` bar above the input area; agent updates progress automatically |
| **Ask user** | Agent can prompt you with questions and choices mid-task when it needs clarification |

### 🤝 Agent Teams

Spawn multiple AI teammates that work **in parallel**, each with independent tools, conversation history, and environment. The lead agent coordinates; teammates idle until they receive an explicit message.

```
 You › Create a team and spawn a backend developer

   ⚙ Tool  team_create   team_name=dev-team
      ✓ Done

   ⚙ Tool  team_spawn   name=backend  prompt="Senior Go backend developer"
      ✓ Done

 You › Send backend a task

   ⚙ Tool  team_send_message   to=backend  message="Add pagination to /users"
      ✓ Message sent to @backend
```

Switch between agent views with **Shift+↑/↓** and see live status in the team panel:

```
  ╭ Team: dev-team (2) ───────────────────────────╮
  │  ● Main (leader)                               │
  │  ○ ⟳ @architect 1m32s [3 tools]               │
  │  ○ ◇ @backend   0m45s                          │
  │                                                 │
  │  shift+↑/↓: switch agent | esc: back to leader │
  ╰─────────────────────────────────────────────────╯
```

- **Persistent mailbox** — session-scoped, file-based message passing between teammates
- **Per-agent approval** — mutating tool calls surface an approval dialog tagged with the teammate's name and color
- **Independent conversations** — each agent has its own full chat history, tool calls, and markdown-rendered output
- **Agent types** — `explore` (read-only), `general` (full tools), `coordinator` (can spawn sub-teams)

### 🌐 SSH — work on any machine

Type `/ssh user@host` and every tool runs transparently on the remote host. No agents, no tunnels, no extra setup.

```
 You › /ssh deploy@10.0.1.5:/var/www/app

   ✓ SSH  Connected · linux/amd64

 You › why is nginx restarting?

   ⚙ Tool  execute  [deploy@10.0.1.5]  docker logs app-nginx-1 --tail 20

   ╭─────────────────────────────────────────────────────╮
   │  nginx: [emerg] bind() to 0.0.0.0:80 failed        │
   │  (98: Address already in use)                       │
   ╰─────────────────────────────────────────────────────╯

 ◆ Port 80 is already taken. Let me find what's holding it.
```

Save connections as named aliases and jump between hosts with `/ssh`:

```
  ┌─────── /ssh ────────────────────────────────────┐
  │  > 🔗 prod        deploy@10.0.1.5:/var/www/app  │
  │    🔗 staging      ci@10.0.1.8:/srv/staging      │
  │    ➕ Connect New SSH                             │
  └─────────────────────────────────────────────────┘
```

### 📋 Plan Mode

Press **Ctrl+P** to enter Plan Mode. The agent explores your codebase **read-only** and presents a structured plan before touching any file. Review, approve or reject with feedback — then let it execute step by step.

```
  Plan │ Model: openai / gpt-4o │ Approve: Ask │ [██░░░░░░░░] 12%
```

### 🔌 MCP Integration

Connect any [MCP](https://modelcontextprotocol.io/)-compatible server — stdio, HTTP, or SSE — and its tools merge with the built-ins. Auto-reconnect with exponential backoff. Status shown live in the status bar.

```json
{
  "mcp_servers": {
    "github": { "transport": "stdio", "command": "gh-mcp" },
    "db":     { "transport": "http",  "url": "http://localhost:3001/mcp" }
  }
}
```

```
  Agent │ Model: openai / gpt-4o │ Approve: Ask │ [████░░░░░░] 2% │ MCP: 2/5
```

### 💰 Token Usage & Budget Control

Real-time context window tracking with a **color-coded progress bar** in the status bar:

| Progress | Color | Meaning |
|---|---|---|
| `[████░░░░░░] 45%` | 🟢 Green | Comfortable — plenty of context left |
| `[███████░░░] 78%` | 🟠 Orange | Approaching limit — consider compacting |
| `[█████████░] 92%` | 🔴 Red | Near limit — auto-compaction may trigger |

Set cost guardrails in `config.json`:

```json
{
  "budget": {
    "max_cost_per_session": 5.00,
    "warning_threshold": 0.8
  }
}
```

The agent receives in-context warnings when nearing limits and stops if the budget is exceeded. Model pricing is auto-fetched from [models.dev](https://models.dev).

### 🧠 Context Management

- **Auto-compaction** — when the context window fills up, older conversation is summarized while preserving the most recent messages
- **Manual compaction** — type `/compact` anytime to free up context
- **Smart prompt caching** — reduces redundant prompt computation across turns
- **AGENTS.md support** — global (`~/.jcode/AGENTS.md`), project-level, and local (`.local.md`, git-ignored) agent instructions with `@include` directives

### 🛠 Skills

Domain-specific skills loaded on demand. Built-in skills include **PR review** and **security review**. Add your own skill packs to `~/.jcode/skills/` or `<project>/.jcode/skills/`.

Skills register as slash commands — type `/review-pr` or `/security-review` to activate.

### ⚡ Subagents & Background Tasks

- **Subagents** — delegate subtasks to independent child agents (`explore`, `general`, or `coordinator` type) with up to 3 levels of nesting
- **Background commands** — long-running builds/tests run async; check with `/bg` or the `check_background` tool
- **Status tracking** — `Bg: 3 running` shown in status bar; task IDs for programmatic access

### 📼 Session Resume

Every conversation is recorded as JSONL. Resume any past session:

```
  ┌──────────────── Resume Session ─────────────────┐
  │  > 2026-03-12  gpt-4o      fix nginx crash       │
  │    2026-03-11  gpt-4o      refactor auth module  │
  │    2026-03-10  claude-3.5  add pagination logic  │
  └─────────────────────────────────────────────────┘
```

```bash
jcode --session           # list sessions
jcode --resume <UUID>     # pick up where you left off
```

### 🧭 Context Awareness

At startup the agent automatically detects:

- Git branch, dirty status, last commit
- Project type (Go, Python, JS, Rust, Java, …)
- Directory structure
- SSH environment labels
- Available skills

No manual configuration needed — the agent adapts to your project.

## Keyboard Shortcuts

| Key | Action |
|---|---|
| **Enter** | Submit prompt / select option |
| **Ctrl+C** | Press once to warn, twice to exit |
| **Ctrl+A** | Toggle approval mode (Ask ↔ Auto) |
| **Ctrl+P** | Toggle Plan ↔ Agent mode |
| **Ctrl+L** | Clear viewport |
| **Shift+↑/↓** | Switch between teammates |
| **Esc** | Return to leader view |
| **/** | Start slash command |

## Slash Commands

| Command | Action |
|---|---|
| `/model` | Switch model mid-session |
| `/setting` | Open settings menu |
| `/ssh` | Connect to SSH host |
| `/resume` | Resume a previous session |
| `/compact` | Compact conversation context |
| `/bg` | Check background tasks |
| `/<skill>` | Activate a loaded skill |

## Configuration

Config lives at `~/.jcode/config.json`. Key sections:

| Section | What it controls |
|---|---|
| `providers` | API keys and base URLs for each model provider |
| `model` / `small_model` | Active model and lightweight model for summaries |
| `ssh_aliases` | Named SSH connections |
| `mcp_servers` | MCP server definitions (stdio / HTTP / SSE) |
| `budget` | Token and cost limits per session |
| `compaction` | Auto-compaction threshold, recent message count |
| `prompt` | Memory size, cache, async env timeout |
| `subagent` | Parallel limit, nesting depth |
| `team` | Max teammates, mailbox poll interval |
| `telemetry` | Optional [Langfuse](https://langfuse.com) tracing |

```bash
jcode --doctor    # verify model + MCP connectivity
jcode --version   # show version, commit, build time
```

## License

MIT
