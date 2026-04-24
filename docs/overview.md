---
title: Overview
nav_order: 2
has_children: true
---

# Overview

jcode is an AI-powered coding agent that runs in your terminal. You describe tasks in natural language, and jcode uses a suite of built-in tools to explore your codebase, edit files, run commands, and deliver results — all while keeping you in control.

## How It Works

1. **You describe a task** — Type what you want in plain English (or any language)
2. **jcode explores** — It reads your project files, runs searches, and understands context
3. **jcode acts** — Edits, writes, and executes are shown to you for approval
4. **You review** — Every change is visible. Approve, reject, or guide the agent

## Core Capabilities

| Capability | Description |
|---|---|
| **File Operations** | Read, edit (string-level diffs), and write files with inline before/after display |
| **Shell Execution** | Run any command. Safe commands like `ls` and `git status` are auto-approved |
| **Code Search** | Regex search across your entire codebase in seconds |
| **Task Tracking** | Live todo bar tracks progress. The agent updates tasks automatically |
| **Interactive Questions** | The agent can ask you for clarification mid-task |
| **Plan Mode** | Explore read-only and present a plan before making changes |
| **SSH Remote** | Work on any machine over SSH — same tools, same experience |
| **Agent Teams** | Spawn parallel AI teammates for complex tasks |
| **Subagents** | Delegate subtasks to independent child agents |
| **MCP Integration** | Connect any MCP-compatible server for extended capabilities |
| **Session Resume** | Every conversation is recorded. Pick up where you left off |
| **Web Interface** | Browser-based UI with terminal access |
| **Channels** | Push notifications to WeChat when approval is needed or tasks complete |
| **JCode Buddy** | Physical desktop companion with pixel cat that reacts to your coding activity |
| **Skills** | Domain-specific skill packs loaded on demand |

## Design Philosophy

- **Transparency first** — Every tool call is visible. No hidden actions.
- **Human in the loop** — Mutating operations require your approval.
- **Bring your own model** — Works with any OpenAI-compatible API.
- **Zero magic** — What you see is what happens. No background rewrites.
- **Terminal-native** — Built for developers who live in the terminal.

## What Makes jcode Different?

- **Approve every edit** — Unlike agents that auto-apply changes, jcode shows you the diff before writing
- **Plan before act** — Plan Mode gives you a structured plan to review before execution
- **SSH transparency** — All tools work seamlessly on remote machines, not just locally
- **Parallel teams** — Multiple AI agents working simultaneously with message passing
- **Context-aware** — Automatically detects your project type, git state, and directory structure
