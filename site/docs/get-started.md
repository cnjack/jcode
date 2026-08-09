---
title: Get Started
nav_order: 1
---

# Get Started

## Prerequisites

- **Go 1.22+** installed
- Either an **API key** from an OpenAI-compatible provider or an eligible ChatGPT/Codex, Grok, or GitHub Copilot account

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

The `make install` command generates the model registry, builds the React web frontend, and installs the Go binary to your `$GOPATH/bin`.

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

1. **Choose a provider** — Select your AI model provider (OpenAI, xAI, GitHub Copilot, etc.)
2. **Choose authentication** — Enter an API key, or sign in with ChatGPT, Grok, or GitHub using the displayed device code
3. **Pick a model** — Select the default model for your session

API keys remain in `~/.jcode/config.json`. Managed account credentials stay in
the owner-only local store `~/.jcode/provider-auth.json`; provider configuration
contains only a non-secret account binding.

{: .note }
**Sign in with ChatGPT** uses the ChatGPT/Codex subscription channel. It is
separate from an OpenAI API key and does not turn ChatGPT subscription access
into OpenAI API credits.

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
2. Builds the React web frontend
3. Compiles the Go binary

{: .note }
The build requires both **Go** and **Node.js + pnpm** installed.
