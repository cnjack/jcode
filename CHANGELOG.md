# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.0.4] - 2026-04-16

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

[Unreleased]: https://github.com/cnjack/jcode/compare/v0.0.4...HEAD
[0.0.4]: https://github.com/cnjack/jcode/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/cnjack/jcode/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/cnjack/jcode/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/cnjack/jcode/releases/tag/v0.0.1
