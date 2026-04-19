# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.2] - 2026-04-20

### Added
- **TUI Cancel Agent Confirmation Dialog**
  - Confirmation dialog when cancelling a running agent to prevent accidental aborts
  - Visual feedback with styled Yes/No options in the TUI

- **TUI Interactive Slash Command Autocomplete**
  - Interactive suggestion list above the input when typing `/` commands
  - Keyboard navigation (Up/Down arrows) and filtering as you type
  - Tab or Enter to accept highlighted suggestion, Esc to dismiss

- **OnAgentStart Lifecycle Event**
  - New `OnAgentStart()` hook in `AgentEventHandler` for immediate working feedback
  - Called before any LLM call, providing instant visual indicator instead of waiting for first text chunk
  - Web frontend handles `agent_start` event for real-time running state

### Changed
- Simplified token usage tracking with `LastTotalTokens` field and `GetLastTotal()` accessor
- Working notification moved from `OnAgentText` to `OnAgentStart` for earlier feedback

### Fixed
- Resolved gocritic, staticcheck and ineffassign lint warnings
- Handle unchecked error returns in BLE code (errcheck)

## [0.3.1] - 2026-04-19

### Added
- **BLE IoT Device Notifications**
  - Auto-discovery and connection to JCODE-* BLE devices
  - Real-time agent status push to IoT devices (idle/working/attention/complete)
  - Nordic UART Service (NUS) protocol support for device commands
  - Lazy background connection with automatic reconnection on failure

- **Notifier System Refactoring**
  - `Notifier` interface with structured `NotifyEvent` (replaces raw strings)
  - `ChannelNotifier` wrapper to use any `Channel` as a `Notifier`
  - WeChat now integrated as notifier for automatic working/idle status pushes
  - Varied, time-aware rich messages for WeChat (4 variations per event type)
  - BLE-optimized short commands with ~10 char display values

### Changed
- `Notifier.Notify()` now takes structured `NotifyEvent` instead of string
- Event types: `EventIdle`, `EventWorking`, `EventApproval`, `EventDone`
- Each notifier formats events for its own display (BLE: device commands, WeChat: rich text)
- BLE messages: idle→"ready", working→"thinking", complete→"done"/"failed"
- WeChat messages now vary by time-of-day and rotate for natural feel
- Agent completion triggers "complete" then auto-returns to "idle" after 5 seconds

## [0.2.2] - 2026-04-18

### Added
- **Grep Tool Enhancements**
  - Hidden file support for grep searches
  - Updated default output mode for better usability

- **Doctor Command Enhancements**
  - Enhanced `jcode doctor` with improved system diagnostics

- **WeChat Idempotent Messaging**
  - Login reminder message for WeChat session activation
  - Idempotent message deduplication to prevent duplicate processing

### Changed
- Makefile now computes version from latest git tag automatically
- Message splitting adjusted to prevent orphaning tool-result messages during compaction
- Web sessions sorted by creation date for consistent ordering

### Fixed
- Web conversation rendering and session sort order
- Tool result handling improved for proper display in web UI

## [0.2.1] - 2026-04-17

### Added
- **Dark Mode / Theme System**
  - Light / Dark / System theme modes with CSS custom-property design tokens
  - `useTheme` composable for reactive theme management across all components
  - Pre-hydration script in `index.html` to prevent flash of unstyled content (FOUC)
  - Smooth cross-fade animations on theme transitions
  - All 15+ web components updated to use token-based theming instead of hardcoded colors

- **WeChat Channel Integration**
  - Bidirectional messaging: send prompts to jcode from WeChat, receive results back
  - Inbound WeChat messages displayed in web UI with green "WeChat" label
  - Toolbar toggle for quick enable/disable next to the Auto toggle
  - Settings dialog Channels tab with Connect/Disconnect flow and QR code
  - Approval and task-done notifications pushed to WeChat (10s delay for approvals)
  - Auto-enable on startup when `channel.web_enabled` is set and credentials exist
  - Busy reply when agent is running and new WeChat message arrives

- **Tool Call Enhancements**
  - `toolCallID` field for precise tool call/result matching
  - Tool call notifications with index ordering

### Changed
- Web UI design system migrated from hardcoded Tailwind classes to CSS custom properties (`tokens.css`)
- Execute tool (`InvokableRun`) now reports exit codes correctly instead of returning errors
- NotifyingHandler and SetOnMessage always registered in web mode (UI toggle works without config)
- DOMPurify bumped to 3.4.0

### Fixed
- Web UI showing user messages twice (duplicate from `user_message` event)
- WeChat inbound messages not reaching the agent in web mode
- Done/approval notifications not firing in web mode (handler wrapping issue)
- Approval notification race: resolved approvals no longer re-notified after 10s timer
- Exit code not reported when command fails in execute tool

## [0.1.1] - 2026-04-16

### Added
- **TUI Session Display Enhancements**
  - Full session reconstruction with complete tool call history on resume
  - Subagent session recording: inner tool calls and results now persisted to JSONL
  - Subagent entries displayed on session resume (start/done states)
  - Tool call ID linking between calls and results for full audit trails
  
- **Langfuse Integration Improvements**
  - Nested span support for subagent and teammate traces using `ParentObservationID`
  - Child trace context propagation through agent execution
  - Complete call chain visibility: LLM → Tool → Agent → Subagent → Teammate

- **Session Recording Enhancements**
  - `ToolCallID` field added to session entries for tool call/result pairing
  - Tool call parameters fully preserved in JSONL for complete conversation recovery

### Changed
- **TUI Parameter Display**
  - Streaming tool call arguments now accumulated across chunks before display
  - Full tool arguments shown in TUI (no truncation) for complete audit visibility
  - Subagent parameters displayed during streaming execution

- **Session Architecture**
  - Tool call history reconstruction from JSONL includes complete tool messages
  - Subagent inner tool calls and results recorded alongside parent session
  - Session resume preserves full context including nested tool interactions

### Fixed
- Streaming tool call arguments partial JSON fragments now properly accumulated
- Subagent tool call parameters missing from TUI display
- Subagent session content not persisted or displayed on resume
- Langfuse traces incomplete for subagent and teammate execution paths
- Tool parameters disappearing from conversation history during reduction

### Technical
- Runner event loop refactored for streaming tool call accumulation
- Subagent event loop enhanced with session recording and proper ToolCallID capture
- Session history reconstruction now handles tool call and tool result message types
- Middleware stack improved for nested tracing with proper span nesting

## [0.0.3] - 2026-04-10

### Added
- `jcode update` command for self-updating binary
- Installation script for easy setup from GitHub

### Changed
- Improved build and release workflow
- Enhanced artifact handling in CI/CD pipeline

## [0.0.2] - 2026-04-09

### Fixed
- Artifact download path corrected to 'internal/' in release workflow

## [0.0.1] - 2026-04-08

### Added
- Initial release of jcode
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

[Unreleased]: https://github.com/cnjack/jcode/compare/v0.3.2...HEAD
[0.3.2]: https://github.com/cnjack/jcode/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/cnjack/jcode/compare/v0.2.2...v0.3.1
[0.0.3]: https://github.com/cnjack/jcode/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/cnjack/jcode/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/cnjack/jcode/releases/tag/v0.0.1
