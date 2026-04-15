---
title: Get Started
nav_order: 1
---

# Get Started

## Prerequisites

- **Go 1.22+** installed
- An **API key** from an OpenAI-compatible provider (OpenAI, Anthropic, Azure, etc.)

## Install

### Quick Install (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/cnjack/jcode/main/script/install.sh | sh
```

This downloads the latest release binary for your platform and installs it to `/usr/local/bin`.

### From Source

Requires **Go 1.22+** and **Node.js + pnpm**.

```bash
git clone https://github.com/cnjack/jcode.git
cd jcode
make install
```

The `make install` command generates the model registry, builds the Vue 3 web frontend, and installs the Go binary to your `$GOPATH/bin`.

### Update

To update an existing installation:

```bash
jcode update
```

This checks GitHub releases, shows `v0.0.1 -> v0.0.2`, and replaces the binary in place.

## First Launch

Run jcode in your project directory:

```bash
cd my-project
jcode
```

On first launch, jcode runs a **setup wizard** that guides you through:

1. **Choose a provider** — Select your AI model provider (OpenAI, Anthropic, etc.)
2. **Enter your API key** — Your key is stored locally at `~/.jcode/config.json`
3. **Pick a model** — Select the default model for your session

That's it. You're ready to go.

## Verify Your Setup

Run the doctor command to verify everything is working:

```bash
jcode doctor
```

This checks:
- Model connectivity (sends a test message)
- MCP server connections (if configured)
- Configuration validity

## Your First Task

Start jcode and describe what you want:

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
```

Every action the agent takes is visible and requires your approval before modifying files.

## Build from Source

If you prefer to build from source:

```bash
git clone https://github.com/cnjack/jcode.git
cd jcode
make build
```

The `make build` command:
1. Generates the model registry from [models.dev](https://models.dev)
2. Builds the Vue 3 web frontend
3. Compiles the Go binary

{: .note }
The build requires both **Go** and **Node.js + pnpm** installed.
