---
title: Changelog
nav_order: 10
---

# Changelog

All notable changes to jcode are documented here. This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html) and follows the [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) format.

For the **full changelog**, see [CHANGELOG.md](../CHANGELOG.md) in the repository root.

## Latest Release

### [Unreleased]

**WeChat Channel Integration • Tool Call Enhancements • Bug Fixes**

#### Added
- Bidirectional WeChat messaging: send prompts from WeChat, receive results back
- Inbound WeChat messages displayed in web UI with green "WeChat" label
- Toolbar toggle for quick WeChat enable/disable
- Settings dialog Channels tab with Connect/Disconnect and QR code login
- Approval and task-done notifications pushed to WeChat
- `toolCallID` for precise tool call/result matching

#### Changed
- Execute tool reports exit codes correctly instead of returning errors
- NotifyingHandler always registered in web mode (UI toggle works without config)
- DOMPurify bumped to 3.4.0

#### Fixed
- Web UI showing user messages twice
- WeChat inbound messages not reaching agent in web mode
- Done/approval notifications not firing in web mode
- Approval notification race with 10s timer
- Exit code not reported on command failure

---

### [0.0.4] - 2026-04-16

**TUI Session Display Enhancements • Langfuse Integration Improvements • Session Recording Enhancements**

#### Added
- Full session reconstruction with complete tool call history on resume
- Nested span support for subagent and teammate traces using `ParentObservationID`
- Subagent session recording with tool call and result persistence to JSONL
- Tool call ID linking between calls and results for full audit trails
- `ToolCallID` field in session entries for tool call/result pairing

#### Changed
- Streaming tool call arguments now accumulated across chunks before display
- Tool call history reconstruction from JSONL includes complete tool messages
- Subagent inner tool calls and results recorded alongside parent session
- Tool parameters displayed during streaming execution (no truncation)

#### Fixed
- Streaming tool call arguments partial JSON fragments now properly accumulated
- Subagent tool call parameters missing from TUI display
- Subagent session content not persisted or displayed on resume
- Langfuse traces incomplete for subagent and teammate execution paths
- Tool parameters disappearing from conversation history during reduction

---

## Version History

### [0.0.3] - 2026-04-10

**Self-update Support • Installation Script**

#### Added
- `jcode update` command for self-updating binary
- Installation script for easy setup from GitHub

#### Changed
- Improved build and release workflow
- Enhanced artifact handling in CI/CD pipeline

---

### [0.0.2] - 2026-04-09

**Artifact Download Fix**

#### Fixed
- Artifact download path corrected to 'internal/' in release workflow

---

### [0.0.1] - 2026-04-08

**Initial Release**

#### Added
- Core agent loop with file operations, shell execution, and regex search
- Todo tracking and interactive user prompting
- Plan Mode for structured task review before execution
- Agent Teams for parallel multi-agent coordination
- SSH support for remote code editing
- Multi-model support with OpenAI-compatible APIs
- Session recording and resume capability
- Web interface with React/Vue frontend
- Langfuse integration for tracing
- Per-agent token usage tracking
- WebSocket support for real-time communication

---

## How to Check Your Version

```bash
jcode version
```

Shows:
- Current version number
- Build timestamp
- Git commit SHA

## How to Update

```bash
jcode update
```

This automatically downloads and installs the latest version. You can also update manually:

```bash
curl -fsSL https://raw.githubusercontent.com/cnjack/jcode/main/script/install.sh | sh
```

## What Changed Between Versions

Use the full [CHANGELOG.md](../CHANGELOG.md) to see detailed changes for each release, including:

- **Added** features and capabilities
- **Changed** behavior and improvements
- **Fixed** bugs and issues
- **Technical** implementation details
- **Deprecated** features

## Breaking Changes

This section will document any breaking changes that may affect your workflow:

**Currently:** No breaking changes between v0.0.1 and v0.0.4.

All releases maintain backward compatibility with existing configurations and workflows.

## Getting Help

- **Questions about a feature?** Check the [Features](overview) section
- **Command reference?** See [Commands & Shortcuts](commands)
- **Configuration questions?** Visit [Configuration](configuration)
- **Report a bug?** Open an issue on [GitHub](https://github.com/cnjack/jcode/issues)

---

*Last updated: April 16, 2026*
