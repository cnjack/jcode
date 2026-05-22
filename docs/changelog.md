---
title: Changelog
nav_order: 10
---

# Changelog

All notable changes to jcode are documented here. This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html) and follows the [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) format.

For the **full changelog**, see [CHANGELOG.md](../CHANGELOG.md) in the repository root.

## Unreleased

### In Development

---

## Latest Release

### [0.4.9] - 2026-05-23

**Skill & Team Tool Display • Session Title Fix • Deferred Session Files**

#### Added
- Enhanced tool display info for skill and team tools (load_skill, team_list, team_create, team_spawn, team_send_message, team_delete) in the web UI.
- TUI formatted output for load_skill, team_list, and team operations — compact, styled summaries instead of raw text.
- ACP tool kind mappings for team tools — team_list is read-only, team_create/spawn/send_message/delete are execute-type.
- Skill description attribute in loaded skill output for richer context in tool results.

#### Changed
- Session title generation now only fires for truly new sessions — resuming a session with a client-provided UUID that has no file on disk still generates a title.
- System prompt recording is deferred until the first real message, preventing empty session files when jcode is opened and immediately closed.

#### Fixed
- Simplified function signatures and removed unused variables in ToolCallCard and TerminalPanel components.
- Removed unused import in Langfuse telemetry.

---

### [0.4.8] - 2026-05-22

**Custom Model Registry • New Model Providers • Shared State Fix**

#### Added
- Custom model support in ModelRegistry — users can add and use custom models alongside the built-in registry.
- Two new model providers added to the built-in registry.

#### Fixed
- Deep-copy providers in `NewModelRegistry` to prevent shared state mutation across instances.

---

### [0.4.7] - 2026-05-21

**Web Frontend Redesign • Pencil Design System • Multi-tab Terminal**

#### Added
- Complete web frontend visual overhaul based on the Pencil design system.
- Borderless input area with a right panel showing file tree and changes.
- Multi-tab terminal with resizable panels and search renderer.
- Themed bash output and dark mode fixes.
- Inline channel toggle in the input area with sidebar border.

#### Changed
- Aligned TopBar, Sidebar, and ChatInput with the Pencil design spec.
- Removed toolbar separator lines, compacted textarea, and shortened file paths.
- Enlarged input card, removed sidebar lines, and added terminal close button.
- Removed max-width constraint on input card for widescreen displays.

#### Fixed
- Updated terminal icon from laptop to lightning bolt for consistency.
- Restored header row matching Pencil design — no separator line, click to collapse.
- Hide title row when expanded and collapse via hover button.
- Unified search and diff expanded content, removed internal dividers.
- Removed duplicate inner headers from expanded tool call cards.

---

### [0.4.6] - 2026-05-14

**Context Management • TUI Version Display • Executor Pager Fix**

#### Added
- Enhanced context management with telemetry and approval improvements.
- Version display in TUI sidebar and logo rendering.

#### Changed
- Disabled pager and interactive prompts in all executors for non-interactive use.

#### Fixed
- Dependency updates: bumped marked (18.0.2), postcss (8.5.10), nokogiri (1.19.3).

---

### [0.4.5] - 2026-04-28

**DeepSeek Thinking Mode Fix**

#### Fixed
- Passthrough `reasoning_content` for DeepSeek thinking mode so reasoning tokens are preserved in the response.

---

### [0.4.4] - 2026-04-28

**Cached Token Telemetry • TUI Textarea Fix • Version Display Fixes**

#### Added
- Enhanced token usage tracking with cached tokens in Langfuse telemetry.
- Refactored TUI textarea line calculation for improved accuracy.

#### Fixed
- Corrected version formatting in Makefile and version rendering in shortcut hints.
- Removed version formatting in server version display.
- Fixed web version to use the build version instead of placeholder.

---

### [0.4.3] - 2026-04-27

**ACPExecutor • ACP Error Handling**

#### Added
- ACPExecutor to delegate file system and terminal operations via the ACP client protocol.

#### Fixed
- Improved error handling for command execution timeout and cancellation in ACP executor.

---

### [0.4.2] - 2026-04-27

**Web Message Actions • Edit • Retry • Copy**

#### Added
- Message edit, retry, and copy actions in the web UI.

#### Fixed
- Addressed CodeRabbit review feedback on message actions implementation.

---

### [0.4.1] - 2026-04-26

**Multimodal Image Input • ACP Vision Support • Session Image Restore**

#### Added
- Multimodal image input for the web UI and ACP transport when the selected model supports image input.
- Session persistence now stores attached images and restores them during session replay.

---

### [0.3.10] - 2026-04-25

**Auto-open Browser • Resize Reflow • Model Picker Redesign**

#### Added
- Automatic browser opening after the web server starts.
- Keyboard shortcuts help in the TUI.

#### Changed
- Redesigned model picker with provider grouping and visibility controls.
- TUI content now reflows correctly on terminal resize.
- Subagent output renders as Markdown with expand/collapse support.

#### Fixed
- Ctrl+Y copy now uses stored raw assistant text instead of reconstructing it from rendered lines.

---

### [0.3.9] - 2026-04-24

**Keyboard Shortcuts Help • Context-sensitive Hints**

#### Added
- Keyboard shortcuts help panel with context-sensitive usage hints.

---

### [0.3.8] - 2026-04-24

**Sidebar Cleanup • Better ANSI Width Handling**

#### Changed
- Cleaned up the TUI sidebar component structure.
- Improved ANSI width handling in layout rendering.

---

### [0.3.7] - 2026-04-24

**JCode Buddy Docs • Multi-line Paste • Web Fixes**

#### Added
- JCode Buddy BLE documentation and configuration guidance.
- Multi-line paste support with reference storage.

#### Changed
- Internal documentation cleanup around the new Buddy and paste flows.

#### Fixed
- Web TODO panel rendering issues.
- Frontend session split issues.

---

### [0.3.6] - 2026-04-24

**Sidebar Divider Alignment • Stable Team Teammates • Build Cleanup**

#### Changed
- Removed the redundant `build_frontend.sh` helper; frontend builds now flow through `make`.

#### Fixed
- TUI sidebar divider alignment is now enforced with manual line-by-line composition and ANSI-aware truncation/padding.
- Spawned teammates are no longer cancelled when the leader agent finishes its current run.

---

### [0.3.5] - 2026-04-23

**TUI Redesign • [J]CODE Branding • Updated Screenshots and Docs**

#### Added
- New two-column TUI layout with a dedicated sidebar and refreshed orange-accented styling.
- Updated screenshots for TUI, web interface, Zed ACP integration, and agent teams.
- Added `DESIGN_PLAN.md` to capture the redesigned product direction.

#### Changed
- Rebranded docs, icons, and UI surfaces to `[J]CODE`.
- Improved web session restoration to preserve current session content.
- Removed the copy notice and tightened sidebar spacing in the redesigned TUI.

---

### [0.3.4] - 2026-04-22

**Bidirectional BLE • ACP Approval Fixes • Resume Consistency**

#### Added
- Bidirectional BLE communication support so BLE devices can send commands back into the runtime.

#### Changed
- `/resume` now behaves consistently with the `--resume` CLI flag.

#### Fixed
- ACP tool-call approval rejection now works correctly when multiple tool calls are pending.
- Additional ACP handler review fixes improved approval/session stability.

---

### [0.3.3] - 2026-04-21

**Tool Display Polish • Session UX Improvements • Resume Bug Fixes**

#### Added
- Refined tool call display and session UX across TUI and web.
- Stronger session ID validation and better message history reconstruction.

#### Changed
- Execute tool output now strips redundant `STDERR` headers and supports optional descriptions.
- Tool call cards, approval surfaces, and settings/session UI were polished across interfaces.

#### Fixed
- Resume/session ID bugs were resolved across web, ACP, and TUI.
- Message history reconstruction is more reliable when restoring sessions.

---

### [0.3.2] - 2026-04-20

**TUI Cancel Confirmation • Slash Command Autocomplete • OnAgentStart Lifecycle Event**

#### Added
- Cancel agent confirmation dialog to prevent accidental aborts when cancelling a running agent
- Interactive slash command autocomplete with keyboard navigation and filtering
  - Type `/` to see all available commands, filter as you type, Up/Down to navigate
  - Tab or Enter to accept, Esc to dismiss
- `OnAgentStart()` lifecycle event for immediate working feedback before any LLM call
- Web frontend handles `agent_start` SSE/WS event for real-time running state

#### Changed
- Simplified token usage tracking with `LastTotalTokens` field
- Working notification moved from `OnAgentText` to `OnAgentStart` for earlier visual feedback

#### Fixed
- Resolved gocritic, staticcheck and ineffassign lint warnings
- Handle unchecked error returns in BLE code (errcheck)

---

### [0.3.1] - 2026-04-19

**BLE IoT Notifications • Notifier System Refactoring • Rich Message Formatting**

#### Added
- BLE IoT device notifications with auto-discovery of JCODE-* devices
- Real-time agent status push to BLE devices (idle/working/attention/complete)
- Nordic UART Service (NUS) protocol support for device commands
- Lazy background connection with automatic reconnection on failure
- `Notifier` interface with structured `NotifyEvent` types
- `ChannelNotifier` wrapper to use any `Channel` as a `Notifier`
- WeChat integrated as notifier for automatic working/idle status pushes
- Varied, time-aware rich messages for WeChat (4+ variations per event)
- BLE-optimized short commands with ~10 character display values

#### Changed
- `Notifier.Notify()` now takes structured events instead of raw strings
- Event types: `EventIdle`, `EventWorking`, `EventApproval`, `EventDone`
- Each notifier formats events for its display (BLE: commands, WeChat: rich text)
- BLE messages: idle→"ready", working→"thinking", complete→"done"/"failed"
- WeChat messages vary by time-of-day and rotate for natural conversation
- Agent completion triggers "complete" then returns to "idle" after 5 seconds

---

### [0.2.2] - 2026-04-18

**Grep Enhancements • Doctor Command • WeChat Idempotency • Web UI Fixes**

#### Added
- Hidden file support and updated default output mode for grep tool
- Enhanced `jcode doctor` with improved system diagnostics
- Login reminder message for WeChat session activation
- Idempotent message deduplication for WeChat to prevent duplicate processing

#### Changed
- Makefile computes version from latest git tag automatically
- Message splitting adjusted to prevent orphaning tool-result messages during compaction
- Web sessions sorted by creation date for consistent ordering

#### Fixed
- Web conversation rendering and session sort order
- Tool result handling for proper display in web UI

---

### [0.2.1] - 2026-04-17

**Dark Mode / Theme System • WeChat Channel Integration • Tool Call Enhancements • Bug Fixes**

#### Added
- Dark / Light / System theme modes with CSS custom-property design tokens
- `useTheme` composable for reactive theme management across all web components
- Pre-hydration script to prevent flash of unstyled content (FOUC) for light-theme users
- Smooth cross-fade animations on theme transitions
- Bidirectional WeChat messaging: send prompts from WeChat, receive results back
- Inbound WeChat messages displayed in web UI with green "WeChat" label
- Toolbar toggle for quick WeChat enable/disable
- Settings dialog Channels tab with Connect/Disconnect and QR code login
- Approval and task-done notifications pushed to WeChat
- `toolCallID` for precise tool call/result matching

#### Changed
- Web UI design system migrated from hardcoded Tailwind classes to CSS custom properties
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

### [0.1.1] - 2026-04-16

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

### [0.0.5] - 2026-04-16

**Changelog Pages • Tool Call IDs • Execute Exit-Code Handling**

#### Added
- Root and docs-site changelog pages.
- `toolCallID` propagation and indexed tool notifications for clearer tool/result pairing.
- PR description guidance template for contributors.

#### Changed
- Documentation and AGENTS guidance were refreshed.
- DOMPurify bumped to 3.4.0.

#### Fixed
- Execute tool reports command exit codes cleanly even when the command fails.

---

### [0.0.4] - 2026-04-16

**TUI Recording Flow • Langfuse Tracing Updates**

#### Added
- Improved TUI display and session recording flow.
- `Recorder.HasRecording()` and startup checks for existing recordings.
- Better Langfuse tracing for interactive runs, subagents, and teammates.

#### Changed
- Runner and interactive startup plumbing were refined for recorded sessions.
- Generated model registry output stopped being tracked in version control.

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

**Currently:** No breaking changes between v0.0.1 and v0.4.9.

All releases maintain backward compatibility with existing configurations and workflows.

## Getting Help

- **Questions about a feature?** Check the [Features](overview) section
- **Command reference?** See [Commands & Shortcuts](commands)
- **Configuration questions?** Visit [Configuration](configuration)
- **Report a bug?** Open an issue on [GitHub](https://github.com/cnjack/jcode/issues)

---

*Last updated: May 23, 2026*
