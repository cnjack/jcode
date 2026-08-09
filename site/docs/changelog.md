---
title: Changelog
nav_order: 11
---

# Changelog

All notable changes to jcode are documented here. This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html) and follows the [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) format.

For implementation-level detail, see the repository's full [CHANGELOG.md](https://github.com/cnjack/jcode/blob/main/CHANGELOG.md).

## Unreleased

#### Added
- **Live managed model catalogs.** ChatGPT/Codex, Grok, and GitHub Copilot now load the models available to the selected account. Enabled models remain available after restart, and Copilot preserves each model's required Responses or Chat Completions transport.
- **Provider-backed image generation.** jcode can discover image-capable providers, verify their runtime capabilities, configure image models, and invoke managed provider tools from the same agent workflow used for coding tasks.
- **Grok Imagine with account sign-in.** Connected xAI accounts can select `grok-imagine-image` or `grok-imagine-image-quality` as the independent Image Model without copying an API key.
- **Durable generated-image artifacts.** Generated images appear as first-class timeline cards and artifacts, with revision and lifecycle state that survives session replay across Web, Desktop, TUI, ACP, and Cloud transport.

#### Changed
- **Focused Ask User flow.** Pending questions now open in a bottom dock with paging, keyboard-friendly choices, custom answers, skip, submit locking, and retryable errors. Completed answers stay in the conversation as compact receipts instead of collapsing into generic tool activity.
- **Cleaner new sessions.** A brand-new empty task hides task-specific titlebar controls until the conversation actually contains content.

#### Fixed
- Grok device sign-in accepts xAI's official account verification page without weakening verification-URL origin checks.

#### Security
- Billable provider operations require an explicit approval choice before execution. Session and configuration persistence also gains file locking, directory synchronization, security journaling, and secret-safe MCP updates.

---

## Latest Release

### [0.12.2](https://github.com/cnjack/jcode/releases/tag/v0.12.2) - 2026-08-05

**Structured Turn History • Reliable Cancellation and Replay**

#### Fixed
- Live TUI, Web, and ACP history now preserves the complete assistant/tool transcript instead of flattening each turn into one text response, so continued turns match the persisted replay.
- Parallel tool-call results and large-output truncation remain paired consistently between live and resumed sessions.
- Cancellation and stream failures preserve visible partial text while dropping incomplete tool calls that could make a session impossible to resume.

---

## Recent Releases

### [0.12.1](https://github.com/cnjack/jcode/releases/tag/v0.12.1) - 2026-08-02

**Artifact Workbench • Safe Preview and Sharing • Faster Site Navigation**

#### Added
- **Desktop and Web artifacts.** Agent outputs can be registered as durable, revisioned artifacts and opened from an automatic workbench with quick-look and focus layouts, replay support, automation indicators, and five-language UI.
- Safe renderers cover Markdown, code, text, CSV, images, PDFs, and sandboxed HTML. Desktop can open or reveal validated local artifacts, while signed-in users can optionally create end-to-end encrypted Cloud shares.

#### Security
- Artifact access rejects sensitive paths, unsafe extensions, symlinks, hard links, kind spoofing, and active content outside the sandboxed renderer.

#### Changed
- The public site now uses route-level loading boundaries and navigation-intent preloading for a faster Chat UI page transition.

---

### [0.11.4](https://github.com/cnjack/jcode/releases/tag/v0.11.4) - 2026-07-31

**Desktop Task Context • Native Open In • Browser Bridge Setup**

#### Added
- Desktop now shows the active task, workspace, branch, model, Cloud sync, and task actions in the window titlebar.
- The **Open in…** menu discovers installed editors, terminals, and file managers through native macOS, Windows, and Linux registration systems, using platform icons and strict workspace/application validation.
- Browser settings now link directly to the Browser Bridge store listing and setup guide when the extension is offline.

---

### [0.11.3](https://github.com/cnjack/jcode/releases/tag/v0.11.3) - 2026-07-30

**Layered Configuration • Markdown Custom Agents • Remote Access**

#### Added
- **Layered project configuration.** jcode merges global config, walk-up project `.jcode/config.json`, standalone MCP files, and supported environment variables with clear precedence. Settings identifies project and agent-provided scopes.
- **Markdown custom agents.** Reusable roles load from user and project `*.agent.md` files, can be selected in CLI and Web/Desktop, persist with sessions, and apply consistently to subagents, workflows, and team members.

#### Changed
- Remote Access has a focused onboarding page and a more reliable system-browser device authorization flow.
- Model discovery now relies on models.dev instead of duplicate hand-maintained entries for providers already present in the generated catalog.

#### Security
- New project MCP servers require explicit trust, dangerous inherited environment variables are blocked, and project config cannot override security-sensitive capabilities.

#### Fixed
- Cloud sends without a session ID now create and acknowledge the exact session before dispatch, preventing active-task misrouting; synchronized models retain reasoning-effort metadata.
- The saved SSH alias picker is theme-aware and keyboard accessible, and Langfuse traces now record safe user input plus final output without exposing multimodal payload data.

---

### [0.11.2](https://github.com/cnjack/jcode/releases/tag/v0.11.2) - 2026-07-23

**Encrypted Provider Sync • Trusted Devices • Model Visibility**

#### Added
- Added opt-in, end-to-end encrypted provider configuration sync across approved Desktop devices, including approval and revocation, conflict-safe revisions, provider metadata, and Cloud-hosted provider proxy support. Local providers continue to work directly when Cloud is unavailable.

#### Changed
- Cloud sync and panel controls use a quieter icon-first toolbar treatment while preserving accessible state labels.

#### Fixed
- Model pickers, favorites, recents, and provider counts now consistently honor persisted enable/disable state; missing visibility data fails closed.
- Generated provider ordering no longer contains duplicate entries.

---

### [0.11.1](https://github.com/cnjack/jcode/releases/tag/v0.11.1) - 2026-07-22

**Encrypted Device Relay • Desktop Pairing • Product Composer**

#### Added
- **jcloud device relay.** `jcode login` connects Web/Desktop to an outbound-only, end-to-end encrypted relay so approved remote devices can browse workspaces, resume sessions, send messages, and manage goals without exposing plaintext to the relay.
- Desktop approval replaces QR pairing, with device management, encryption state, and per-session Cloud sync in Settings.
- The product composer moved into `jcode-ui/product`, project last-activity ordering persists across restarts, and `JCODE_NO_BROWSER=1` supports headless login.

#### Changed
- Conversation switching now resumes a session in one round trip, and selecting the already-active model or mode no longer causes redundant resets or flicker.

#### Fixed
- Composer popups remain inside the viewport with correct stacking, and deleting the active conversation returns to a clean welcome session instead of stale content.

---

## Version Index: 0.10.1–0.5.2

These releases predate the expanded summaries above. The full repository changelog and pull requests remain the source for implementation detail.

| Version | Date | Highlights |
| --- | --- | --- |
| `0.10.1` | 2026-07-20 | Native Computer Use and deferred tool discovery; Alibaba Token Plan; stronger conversation recovery, permission boundaries, developer telemetry settings, and signed desktop helper packaging. |
| `0.9.6` | 2026-07-17 | Opt-in LLM approval reviewer; Kimi For Coding and K3 vision support; Settings picker for the small-model role. |
| `0.9.5` | 2026-07-14 | Small-model routing and generated session titles; subagent support over ACP; Settings layout polish. |
| `0.9.4` | 2026-07-13 | Vue-to-React migration and reusable `jcode-ui`; grouped tool activity, turn-level change summaries, dual-channel output, and richer TUI transcripts. |
| `0.9.3` | 2026-07-06 | Workflow slash commands across TUI, Web, and ACP; Desktop sidecar inherits the login-shell environment; Browser Bridge reliability updates. |
| `0.9.2` | 2026-07-06 | Long-horizon hardening for bounded tool output, atomic file writes, process cancellation, compaction, and prompt handling. |
| `0.9.1` | 2026-07-05 | Deterministic JavaScript workflows with parallel agents, preflight validation, CLI commands, and built-in workflow templates. |
| `0.8.1` | 2026-07-05 | Browser Use, learned cross-session memory, configurable lifecycle hooks, agent-evaluation showcases, and token auth for non-loopback Web servers. |
| `0.7.2` | 2026-06-28 | Card-based provider and model management, custom providers, visibility controls, and per-model reasoning settings. |
| `0.7.1` | 2026-06-24 | Scheduled and manual automations through Web, CLI, and agent tools, with dedicated navigation and run history. |
| `0.6.4` | 2026-06-23 | Parallel Web tasks and Docker workspaces, plus safer branch checkout, task lifecycle, terminal, and container cleanup behavior. |
| `0.6.3` | 2026-06-22 | Usage statistics for tokens, context, model/provider breakdown, trends, and cache-hit rate. |
| `0.6.2` | 2026-06-22 | Five-language Web UI, semantic icon/token polish, remote-workspace improvements, and Desktop/build fixes. |
| `0.6.1` | 2026-06-21 | Task-centric multi-project Web workspace, Tauri Desktop shell, and SSH remote execution. |
| `0.5.2` | 2026-06-15 | Seven shared themes with TUI `/theme` and Web Appearance settings; redesigned todo/workbench; interactive Ask User and MCP OAuth/settings management. |

---

## Earlier Releases

### [0.5.1] - 2026-06-13

**1M-Context Models • Adaptive Window Sizing • Approval-Gate Hardening**

#### Added
- **1M-context model support.** New `model.ResolveContextLimit()` is the single source of truth for window resolution (config override → registry → known-models fallback → config default), replacing five copy-pasted blocks. 1M-context models the registry under-reported no longer fall back to a 200K window and compact at ~150K.
- New config knobs: `context_limits` (per provider/model or bare id), `default_context_limit`, and a now-wired `compaction.threshold` so summarization/compaction scale off a configurable fraction.
- Model registry refresh that survives `go generate`: GLM-5.2 injected on the Zhipu/Z.ai providers (1M window), MiniMax-M3 corrected to its 1M window, and `recommendedModels` / offline known-models tables updated to the 2026 flagships.
- Approval dialog splits **Allow** into "Allow once" / "Allow all" so approving one command no longer changes the session mode.

#### Security
- **Hardened the approval gate.** Background commands no longer bypass approval; the "safe command" allowlist now rejects shell operators (`; & | < > $() ${}`) and matches whole command words, so a safe prefix can't smuggle a payload (e.g. `git status && rm -rf /`); web "approve once" no longer silently promotes the session to Autopilot; `ask_user` is auto-approved again; the manual and teammate approval paths share one `decide()`.

#### Changed
- Removed the dead SSE transport (`internal/web/sse.go`, the `sse.ts` composable, `/api/events`) — the web frontend only used WebSocket, and parity was verified.

#### Fixed
- Fixed pre-existing `vue-tsc` errors so `npm run build` passes again; addressed CodeRabbit feedback (workpath race, WebSocket error logging, test timeouts).

---

### [0.5.0] - 2026-06-13

**Unified Ask/Plan/Autopilot Selector • Persistent Session Goal • Dependency Upgrades**

#### Added
- Unified **Ask / Plan / Autopilot** session-mode selector across the TUI, web, and ACP frontends. Cycle it with **Shift+Tab** in the TUI (replacing the old Ctrl+P / Ctrl+A shortcuts), the dropdown in the web UI, or `session/set_mode` over ACP. New `default_mode` config (`ask`/`plan`/`autopilot`); legacy `auto_approve: true` maps to `autopilot`. Web Plan mode now truly restricts to read-only tools.
- Persistent **session goal** with auto-continuation (codex-style): the agent keeps working toward a goal across turns until it completes, blocks, or hits a 25-continuation cap. `goal_set`/`goal_get`/`goal_update` tools and a shared `/goal` command, persisted and restored on every resume path. TUI `/goal <objective>|status|clear` with a 🎯 indicator; web `GET/POST/DELETE /api/goal` + `goal_update` events, a `GoalBanner`, and a 🎯 input toggle; ACP advertises `/goal` via `available_commands`.

#### Changed
- TUI mode pills collapsed into a single tri-state pill (Shift+Tab); web auto-approve folded into Autopilot; ACP sessions advertise three modes (`agent` accepted as an alias for `ask`).
- Upgraded dependencies: bubbletea v2.0.7, eino v0.9.6, eino-ext langfuse v0.1.1, acp-go-sdk v0.13.5, mcp-go v0.54.1, golang.org/x/sys v0.45.0.

---

### [0.4.11] - 2026-06-03

**ACP Presentation Enhancements • Session Listing by cwd**

#### Changed
- Richer ACP tool-call presentation: semantic tool kinds, friendly titles (e.g. `Read main.go (1-50)`), file locations with line numbers, Pending → InProgress → Completed/Failed status streaming, and diff content for edit/write calls.
- ACP `ListSessions` now scoped by `cwd` and returns an `UpdatedAt` field.
- TUI: extracted `renderViewportContent()` and preserved scroll-to-bottom position in the batched render path.

#### Removed
- ACP client filesystem/terminal executor path (the v2 client capability extension is no longer supported); ACP sessions now use the local executor.

#### Fixed
- Delay the ACP slash-command broadcast so commands advertise at the right time.

---

### [0.4.10] - 2026-06-03

**Slash Commands • TUI Render Performance**

#### Added
- **Slash commands** — skills exposed as `/`-prefixed commands in the TUI and web (with ChatInput autocomplete). Upgraded `acp-go-sdk` to v0.13.4, migrated to the stable `CloseSession`, and added `ResumeSession` + `Logout` with a Resume capability.

#### Changed
- TUI render performance: multi-layer render cache and stream debounce with precise cache invalidation on state changes.
- Split the monolithic `tui.go` into focused modules; added a pre-commit hook and applied fmt/lint fixes.

#### Fixed
- Invalidate the sidebar cache on viewport height change.

---

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

**ACP Transport • ACP Error Handling**

#### Added
- Initial ACP transport integration work for editor-driven sessions.

#### Fixed
- Improved ACP command execution timeout and cancellation error handling.

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

Use the full repository [CHANGELOG.md](https://github.com/cnjack/jcode/blob/main/CHANGELOG.md) to see detailed changes for each release, including:

- **Added** features and capabilities
- **Changed** behavior and improvements
- **Fixed** bugs and issues
- **Technical** implementation details
- **Deprecated** features

## Breaking Changes

Breaking changes and migration notes are called out in the full repository changelog and the package-specific `jcode-ui` changelogs. When upgrading across several minor releases, review every intervening entry—especially configuration, session-mode, provider, and reusable UI package changes.

## Getting Help

- **Questions about a feature?** Check the [Features](overview) section
- **Command reference?** See [Commands & Shortcuts](commands)
- **Configuration questions?** Visit [Configuration](configuration)
- **Report a bug?** Open an issue on [GitHub](https://github.com/cnjack/jcode/issues)

---

*Last updated: August 9, 2026*
