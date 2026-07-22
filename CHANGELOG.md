# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Small model picker in the web/desktop settings.** Settings → Providers gains a "Model roles" section: pick `small_model` from the enabled-model catalog (grouped by provider) or clear it back to "follow main model". Changes persist to config and apply immediately — the `"small"` alias, automations, and session titles pick up the new value without a restart. New `POST /api/small-model` endpoint, `small_model` exposed in `GET /api/config`; a configured ref whose model was since disabled/removed still renders (marked unavailable) so it can be cleared. Localized in all five UI languages.
- **Built-in color themes, unified across terminal and web.** A new single source of truth (`internal/theme`) defines 7 themes — 4 dark (jcode Dark, Midnight, Dracula, Nord) and 3 light (jcode Light, GitHub Light, Solarized Light) — as a typed semantic palette. `go generate` emits the web CSS (`[data-theme]` blocks) and the picker registry from that one Go file, so the two renderers can never drift.
- **`/theme` command** in the TUI opens a live-preview selector: arrow keys repaint the whole UI, Enter applies and persists to `config.theme`, Esc reverts. When no theme is persisted, the startup default is auto-selected from the terminal background. New `theme` config field.
- **Appearance settings tab** in the web UI: a System (follow-OS) option plus dark/light swatch grids that render a true mini-preview of each theme. Themes apply via `html[data-theme]`; the legacy light/dark/system localStorage values migrate automatically.
- **Docker container workspaces (web).** The remote-connect wizard can now bind a task to a Docker container, alongside SSH. A new `DockerExecutor` (Docker Go SDK, `client.FromEnv` → honors `DOCKER_HOST`) runs all agent file/command operations inside the container via `docker exec`, mirroring the SSH executor. A stopped container is started on connect and stopped again (ref-counted) once no task is using it; a one-shot container that exits immediately is reported with its logs rather than failing silently. The embedded terminal opens a real TTY *inside* the bound container (`docker exec`, bash→sh). Container-bound tasks are keyed `docker://<container>/<path>`, and the `switch_env` tool plus saved Docker aliases (`docker_aliases` in config) cover reconnects. Daemon-gated integration tests cover the lifecycle.
- **`small_model` is now a real feature: cheap subagents + LLM session titles.** The `"small"` model alias resolves to `config.small_model` via the model factory and is accepted by the `subagent` tool, workflow/flow specs, `team_spawn`, and the automation model override; unset or malformed `small_model` degrades gracefully to the session model instead of erroring. The subagent tool description nudges the main model to delegate mechanical, low-stakes subtasks (searches, inventories, simple extraction) to it. Session titles upgrade from first-message truncation to a small-model-generated title (async, best-effort, same-language) across TUI/web/ACP; the small model is read from config at fire time, so enabling/disabling it mid-run takes effect without a restart. `jcode doctor` probes small-model connectivity alongside the primary model — both probes now share the factory construction path and are bounded by a 60s timeout so a silent endpoint can't hang the doctor. Wire-level routing is locked by in-process e2e tests (mock OpenAI endpoint asserting the request-body `model` field).

### Changed
- **Memory distillation no longer silently falls back to `small_model`.** The extraction chain is now `memory.model` → main `model`: distilled memories persist across sessions, so extraction quality outweighs token savings. Point `memory.model` at a cheap model explicitly if you want the old behavior. Compaction likewise stays on the main model by design (summary quality bounds post-compaction performance).
- Renamed the session modes to **Ask for approval / Plan / Full access** across the web UI, terminal UI, and ACP. Their canonical IDs are now `approval` / `plan` / `full_access`; the old `ask`, `agent`, and `autopilot` IDs are no longer accepted.
- The terminal palette was de-frozen: the ~50 lipgloss styles that were baked in at import time are now rebuilt from the active theme by `ApplyTheme`, and previously-hardcoded colors (subagent purple, on-primary text, team-panel and context-bar colors) are now semantic tokens. Markdown (glamour) follows the theme's light/dark appearance.

### Fixed
- **Subagent per-spawn model override was a silent no-op in every production surface.** The `subagent` tool honors its `model` param only when a `ModelFactory` is injected, but neither the TUI nor the web engine ever wired one into `SubagentDeps` (only workflow tools got factories) — so a documented feature never worked. Both surfaces now share one factory across subagent + workflow deps.
- Subagent token usage is now attributed to the model that actually served the run; previously it was always logged under the session's main model, which would misstate costs for any model-overridden subagent. Team usage events are normalized the same way (alias resolved, bare model id) so one model no longer splits into two stat buckets, and all usage writers now report the cache-support flag (`CacheSeen`), not just the main runner.
- Teammate model overrides now resolve through the shared model factory instead of a hand-rolled parser — `team_spawn` gets baseURL/effort resolution, instance caching, and the `"small"` alias for free, instead of silently falling back to the leader model.
- Session titles are bound to the session they were generated for: an in-flight async title can no longer land on a different session after `/resume` re-points the live recorder, and racing first messages fire the title generator exactly once (atomic claim, refiner released after use).
- Web: recorders created after task build (lazy creation, session switch, post-delete) now get the same title hook as the task's original recorder, so LLM titles no longer silently vanish for those sessions.
- The memory distillation pipeline logs a breadcrumb when `small_model` is set but `memory.model` isn't — the run rides the main model by design, and the log points at `memory.model` for users who relied on the old implicit small-model fallback.
- TUI: pressing **Esc** while viewing a teammate now returns to the leader (the handler matched a key string that bubbletea never emits, so it was dead code).
- TUI: the Cancel-agent confirmation now defaults to the non-destructive **Wait** button, matching the Quit dialog — `Ctrl+C` then `Enter` no longer aborts a running agent by reflex.
- TUI: **`?`** opens the keyboard-shortcuts help when the input is empty, and the help panel's slash-command list is now generated from the command registry (so `/goal`, skill commands, and `/theme` always appear).

### Removed
- Dead config fields that were parsed but never honored anywhere: `fallback_model` (top-level) and `compaction.summary_model`. Old config files still load (the keys are ignored), and note the keys are dropped from `config.json` the next time jcode saves settings. (`summary_model` was also mis-documented as a working feature — docs corrected.)

## [0.11.1] - 2026-07-22

### Added
- **jcloud device relay — remote-control jcode from anywhere.** A new `internal/cloud/` package implements an outbound-only WebSocket relay to jcloud: `jcode login` authenticates via device-code flow, the connector auto-connects on `jcode web` start, and remote clients (mobile/desktop) can browse workspaces, open sessions, send messages, and arm goals through the relay. All relay traffic is end-to-end encrypted (AES-256-GCM envelopes, P-256 ECIES key exchange, BIP39 recovery phrase) — the relay server never sees plaintext. Device identity is stored in the OS keyring (macOS Keychain / Windows Credential Manager / libsecret). New `jcode cloud status|disconnect|guide` CLI commands and `docs/cloud.md` user guide.
- **Cloud pairing with desktop approval.** Remote devices pair by requesting access; the desktop shows a pairing inbox (web UI badge + settings panel) where the user approves or rejects. QR-code pairing was removed in favor of the desktop approval flow. Per-session cloud sync is opt-in via a toggle in the chat header.
- **Cloud settings UI.** Settings is now a first-class full-page view (renamed from `SettingsDialog`) with a dedicated Cloud tab: login/logout, device list, pairing management, E2EE status, and sync preferences. A `CloudBadge` in the sidebar shows relay connection state at a glance.
- **Product composer extracted to `jcode-ui/product`.** `ChatInput`, `WorkspacePicker`, `BranchPicker`, `GoalBanner`, and `drafts` moved from `web/src/components` into `packages/jcode-ui/src/product/` as a host-agnostic composer library. Hosts inject a `ProductComposerHost` (state + actions + strings + icons); the web adapter is `web/src/app/composerHost.ts`. New `ProviderIcon` component and full i18n string tables (5 languages). Package gains vitest coverage.
- **Project-level last-activity timestamp.** The sidebar persists per-project last-used time so projects sort by recency across restarts.
- **`JCODE_NO_BROWSER=1`** env var disables browser auto-open during `jcode login` (for headless/SSH environments).

### Changed
- **One-shot session resume.** Conversation switching is now a single round trip (`GET /api/sessions/:id/resume`) instead of the previous multi-request dance, cutting perceived switch latency significantly.
- **Model and mode switches are idempotent.** Re-selecting the current model or mode no longer triggers redundant state resets or UI flicker.
- Relay capabilities report the actual E2EE state at register time; the cloud connector reports `full_access` rejection when the mode ceiling disallows it.

### Fixed
- **Composer popup stacking and viewport overflow.** Popups (workspace picker, branch picker, goal banner) now anchor correctly in compact mode, stay within the viewport, and stack with proper z-index ordering.
- **Deleting the open conversation** now lands on the welcome screen with a fresh session instead of showing a stale/deleted chat. Deleting a conversation while the agent is running is blocked to prevent state corruption.

## [0.5.1] - 2026-06-13

### Added
- **1M-context model support.** New `model.ResolveContextLimit()` is the single source of truth for context-window resolution (config override → registry → `knownModels` fallback → config default → `DefaultContextLimitFallback`), replacing five copy-pasted resolution blocks across the interactive/ACP/web/runner call sites. Previously any 1M-context model the registry didn't know (or under-reported) silently fell back to a 200K window and triggered compaction at ~150K.
- New config knobs: `context_limits` (per provider/model or bare id), `default_context_limit`, and the formerly-dead `compaction.threshold` is now wired so summarization/compaction/reduction scale off a configurable fraction.
- Model registry updates that survive `go generate`: `additionalModels` merge-injection (GLM-5.2 on the four first-party Zhipu/Z.ai providers, `contextWindow=1000000`), `contextLimitOverrides` (corrects MiniMax-M3 to its advertised 1M window), and refreshed `recommendedModels` / offline `knownModels` tables to the 2026 1M flagships.
- `docs/model-research.md` caching the per-provider context-window survey, plus `context_limit_test.go` covering resolution order and the registry corrections.
- Approval dialog now splits **Allow** into "Allow once" / "Allow all" so approving a single command no longer changes the session mode.

### Changed
- Removed the dead SSE transport: the web frontend only ever used WebSocket, so `internal/web/sse.go` (`SSEBroker`/`SSEEvent`/`ServeSSE`), `web/src/composables/sse.ts`, and the `/api/events` endpoint were deleted. Every event is still broadcast over WebSocket (parity verified).

### Security
- **Hardened the approval gate (P0+P1).** Background commands no longer bypass approval (Ask/Plan previously auto-approved any `execute` with `background=true`, a model-controlled flag). `isSafeCommand` now rejects shell operators (`; & | < > \` $() ${} ()`) and matches whole command words, so a "safe" prefix can no longer smuggle a payload (e.g. `git status && rm -rf /`). Web "approve once" no longer silently promotes the whole session to Autopilot (`ResolveApproval` gained an `approveAll` flag carried through the API/WS payload). `ask_user` is auto-approved again (the allowlist held the dead name `question`). `RequestApproval` and the teammate approval path now share a single `decide()` so they can't drift.

### Fixed
- Fixed pre-existing `vue-tsc` errors (`noUncheckedIndexedAccess` narrowing in `TerminalPanel.vue` / `ToolCallCard.vue` and the dispatch typing) so `npm run build` passes again.
- Addressed CodeRabbit review feedback: workpath race, WebSocket error logging, and test timeouts.

## [0.5.0] - 2026-06-13

### Added
- Unified **Ask / Plan / Autopilot** session-mode selector across the TUI, web, and ACP frontends (Copilot-style). A single `internal/mode.SessionMode` drives both the tool/prompt axis (Plan is read-only) and the approval axis (Autopilot auto-approves). New `default_mode` config option (`ask` / `plan` / `autopilot`); the legacy `auto_approve` is kept as a fallback (`true` → `autopilot`).
- Persistent **session goal** with auto-continuation across TUI/Web/ACP (codex-style): a goal the agent keeps working toward across turns until it verifiably completes, marks it blocked, or hits a 25-continuation safety cap. New `goal_set`/`goal_get`/`goal_update` tools and a shared `/goal` grammar; the continuation loop injects a continuation prompt while a goal is active (appending only the per-turn delta to avoid quadratic context growth). Goals are persisted via `goal_update` session entries and restored on every resume path. TUI exposes `/goal <objective>|status|clear` with a 🎯 indicator; web adds `GET/POST/DELETE /api/goal`, `goal_update` WS events, a `GoalBanner`, and a 🎯 input toggle; ACP advertises `/goal` via `available_commands`.

### Changed
- TUI: the two separate mode pills (Agent/Plan + Ask/Auto) are now a single tri-state pill cycled with **Shift+Tab** (Ask → Plan → Autopilot). The old **Ctrl+P** (plan toggle) and **Ctrl+A** (approval toggle) shortcuts are removed; the approval dialog's "Approve All" now switches the session to Autopilot.
- Web: the mode dropdown offers Ask / Plan / Autopilot and the separate auto-approve toggle is folded into Autopilot. Web **Plan** mode now actually swaps to the read-only tool set (previously it only prefixed the prompt). `POST /api/mode` accepts `ask`/`plan`/`autopilot` (legacy `build` still accepted).
- ACP: sessions advertise three modes (`ask`/`plan`/`autopilot`); the legacy `agent` mode id is still accepted as an alias for `ask`. Selecting "Allow All" emits a `current_mode_update` to Autopilot.
- Upgraded dependencies to latest: bubbletea v2.0.7, eino v0.9.6, eino-ext langfuse v0.1.1, acp-go-sdk v0.13.5, mcp-go v0.54.1, golang.org/x/sys v0.45.0.

## [0.4.11] - 2026-06-03

### Changed
- Improved ACP tool-call presentation for friendlier editor display and follow-along support: tool names mapped to semantic `ToolKind`s, friendly per-call titles (e.g. `Read main.go (1-50)`), file locations with absolute paths and line numbers, status streaming (Pending → InProgress → Completed/Failed) including a status transition for auto-approved tools, diff content for edit/write calls, and failure detection from the output prefix.
- ACP `ListSessions` is now scoped by `cwd` and returns an `UpdatedAt` field on each `SessionInfo`.
- TUI: extracted `renderViewportContent()` to centralize trimmed rendering and preserve scroll-to-bottom position in the batched render path.

### Removed
- ACP client filesystem/terminal executor path (the v2 client capability extension is no longer supported); ACP sessions now use the local executor. Cleaned up `NewEnvWithExecutor`, `IsACP`, and `SetACP`.

### Fixed
- Delay the ACP slash-command broadcast so commands are advertised at the right time.

## [0.4.10] - 2026-06-03

### Added
- **Slash commands.** Skills are now exposed as `/`-prefixed commands in the TUI and web UI, with ChatInput autocomplete on the web. Upgraded `coder/acp-go-sdk` v0.12.0 → v0.13.4, migrated `UnstableCloseSession` → stable `CloseSession`, added `ResumeSession` and `Logout` (both promoted to stable), and advertise the Resume capability in `SessionCapabilities`.

### Changed
- `perf(tui)`: multi-layer render cache and stream debounce with precise cache invalidation on state changes, sharply reducing redraw cost during streaming.
- Refactored the monolithic `tui.go` into focused modules.
- Added a pre-commit hook and applied fmt/lint fixes across the tree.

### Fixed
- Invalidate the sidebar cache on viewport height change; sanitized the render fast path and debounce logic per review.

## [0.4.9] - 2026-05-23

### Added
- Enhanced tool display info for skill and team tools (load_skill, team_list, team_create, team_spawn, team_send_message, team_delete) in the web UI.
- TUI formatted output for load_skill, team_list, and team operations (team_send_message, team_create, team_spawn, team_delete) — compact, styled summaries instead of raw text.
- ACP tool kind mappings for team tools — team_list is read-only, team_create/spawn/send_message/delete are execute-type.
- Skill description attribute in loaded skill output for richer context in tool results.

### Changed
- Session title generation now only fires for truly new sessions — resuming a session with a client-provided UUID that has no file on disk still generates a title.
- System prompt recording is deferred until the first real message, preventing empty session files when jcode is opened and immediately closed.

### Fixed
- Simplified function signatures and removed unused variables in ToolCallCard and TerminalPanel components.
- Removed unused import in Langfuse telemetry.

## [0.4.8] - 2026-05-22

### Added
- Custom model support in ModelRegistry — users can now add and use custom models alongside the built-in registry.
- Two new model providers added to the built-in registry.

### Fixed
- Deep-copy providers in `NewModelRegistry` to prevent shared state mutation across instances.
- Addressed PR review comments on model registry code.
- Removed unnecessary blank line in registry generation.

## [0.4.7] - 2026-05-21

### Added
- **Web Frontend Redesign** — complete visual overhaul based on the Pencil design system.
- Borderless input area with a right panel showing file tree and changes.
- Multi-tab terminal with resizable panels and search renderer.
- Themed bash output and dark mode fixes.
- Inline channel toggle in the input area with sidebar border.

### Changed
- Aligned TopBar, Sidebar, and ChatInput with the Pencil design spec.
- Removed toolbar separator lines, compacted textarea, and shortened file paths.
- Enlarged input card, removed sidebar lines, and added terminal close button.
- Removed max-width constraint on input card for widescreen displays.

### Fixed
- Updated terminal icon from laptop to lightning bolt for consistency.
- Restored header row matching Pencil design — no separator line, click to collapse.
- Hide title row when expanded and collapse via hover button.
- Unified search and diff expanded content, removed internal dividers.
- Removed duplicate inner headers from expanded tool call cards.

## [0.4.6] - 2026-05-14

### Added
- Enhanced context management with telemetry and approval improvements.
- Version display in TUI sidebar and logo rendering.

### Changed
- Disabled pager and interactive prompts in all executors for non-interactive use.

### Fixed
- Dependency updates: bumped marked (18.0.2), postcss (8.5.10), nokogiri (1.19.3).

## [0.4.5] - 2026-04-28

### Fixed
- Passthrough `reasoning_content` for DeepSeek thinking mode so reasoning tokens are preserved in the response.

## [0.4.4] - 2026-04-28

### Added
- Enhanced token usage tracking with cached tokens in Langfuse telemetry.
- Refactored TUI textarea line calculation for improved accuracy.

### Fixed
- Corrected version formatting in Makefile and version rendering in shortcut hints.
- Removed version formatting in server version display.
- Fixed web version to use the build version instead of placeholder.

## [0.4.3] - 2026-04-27

### Added
- Initial ACP transport integration work for editor-driven sessions.

### Fixed
- Improved ACP command execution timeout and cancellation error handling.

## [0.4.2] - 2026-04-27

### Added
- Message edit, retry, and copy actions in the web UI.

### Fixed
- Addressed CodeRabbit review feedback on message actions implementation.

## [0.4.1] - 2026-04-26

### Added
- Added multimodal image support for web and ACP transports when the selected model accepts image input.
- Added image persistence and multimodal message reconstruction in session history so restored sessions retain attached images.

## [0.3.10] - 2026-04-25

### Added
- Added automatic browser opening after the web server starts listening.
- Added a keyboard shortcuts help surface for the TUI.

### Changed
- Redesigned the model picker with provider grouping and visibility controls.
- Reflowed TUI content correctly when the terminal is resized.
- Rendered subagent output as Markdown in the TUI with expand/collapse support.

### Fixed
- Stored raw assistant text for Ctrl+Y copy actions instead of reconstructing it from rendered lines.

## [0.3.9] - 2026-04-24

### Added
- Added a keyboard shortcuts help panel with context-sensitive usage hints.

## [0.3.8] - 2026-04-24

### Changed
- Cleaned up sidebar component structure in the TUI.
- Improved ANSI width handling for sidebar and layout rendering.

## [0.3.7] - 2026-04-24

### Added
- Added JCode Buddy BLE documentation and configuration guidance.
- Added multi-line paste support with reference storage.

### Changed
- Cleaned up internal documentation around the new Buddy and paste workflows.

### Fixed
- Fixed web TODO panel rendering issues.
- Fixed frontend session split issues.

## [0.3.6] - 2026-04-24

### Changed
- Removed the redundant `build_frontend.sh` helper; frontend builds are now handled by the Makefile workflow.

### Fixed
- Reworked TUI main/sidebar composition to render the divider column independently with ANSI-aware truncation and width padding, keeping the vertical rule aligned under styled or wide-character content.
- Fixed teammate lifecycle management so spawned teammates are no longer cancelled when the leader agent's current run finishes.

## [0.3.5] - 2026-04-23

### Added
- **TUI Redesign**
  - New two-column TUI layout with a dedicated sidebar component and refreshed orange-accented styling.
  - Improved sidebar truncation, spacing, and status presentation.
- **Updated Product Visuals**
  - Added fresh documentation screenshots for the TUI, web interface, Zed ACP flow, and agent teams.
  - Added `DESIGN_PLAN.md` to capture the redesigned product direction.

### Changed
- Updated branding across docs, icons, and UI components to `[J]CODE`.
- Improved web session restoration to preserve current session content alongside session identifiers.
- Removed the copy notice prompt and tightened sidebar spacing in the redesigned TUI.
- Refreshed documentation to match the redesigned product surfaces.

### Removed
- Deleted an obsolete PR artifact file from the repository.

## [0.3.4] - 2026-04-22

### Added
- **Bidirectional BLE Control**
  - Added bidirectional BLE communication so JCODE BLE devices can send commands back to the agent runtime.

### Changed
- Made `/resume` behave consistently with the `--resume` CLI flag across entry points.

### Fixed
- Fixed ACP tool-call approval rejection when multiple tool calls are pending in the same turn.
- Applied ACP handler stability and review fixes across approval and session flows.

## [0.3.3] - 2026-04-21

### Added
- **Tool Display and Session UX Polish**
  - Refined tool call display, session resume flow, and UI polish across TUI and web.
  - Added stronger session ID validation and improved message history reconstruction on restore.

### Changed
- Execute tool output now strips the `STDERR` header unconditionally and supports optional descriptions.
- Improved tool call cards, approval surfaces, and settings/session UI consistency in web and TUI.

### Fixed
- Fixed duplicated session identifiers and resume bugs across web, ACP, and TUI.
- Fixed formatting in session ID validation logic.
- Fixed message history reconstruction when restoring existing sessions.

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

## [0.0.5] - 2026-04-16

### Added
- **Changelog and Tool Call Traceability**
  - Added repository and docs-site changelog pages.
  - Added `toolCallID` propagation and indexed tool notifications for clearer tool/result pairing.
- Added a PR description guidance template for contributors.

### Changed
- Improved error handling and logging around tool execution, handlers, and web session updates.
- Updated project documentation and AGENTS guidance.
- Bumped DOMPurify to 3.4.0.

### Fixed
- Execute tool now reports command exit codes cleanly even when the command itself fails.

## [0.0.4] - 2026-04-16

### Added
- **TUI and Session Recording Improvements**
  - Improved TUI display and session recording flow.
  - Added `Recorder.HasRecording()` and startup checks for existing recordings.
- **Langfuse Tracing Improvements**
  - Updated Langfuse integration with improved tracing capabilities for interactive runs, subagents, and teammates.

### Changed
- Refined runner, session history, and interactive startup plumbing for recorded sessions.
- Stopped tracking generated model registry output in version control.

### Fixed
- Updated runtime dependencies for improved compatibility and telemetry behavior.

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

[Unreleased]: https://github.com/cnjack/jcode/compare/v0.5.1...HEAD
[0.5.1]: https://github.com/cnjack/jcode/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/cnjack/jcode/compare/v0.4.11...v0.5.0
[0.4.11]: https://github.com/cnjack/jcode/compare/v0.4.10...v0.4.11
[0.4.10]: https://github.com/cnjack/jcode/compare/v0.4.9...v0.4.10
[0.4.9]: https://github.com/cnjack/jcode/compare/v0.4.8...v0.4.9
[0.4.8]: https://github.com/cnjack/jcode/compare/v0.4.7...v0.4.8
[0.4.7]: https://github.com/cnjack/jcode/compare/v0.4.6...v0.4.7
[0.4.6]: https://github.com/cnjack/jcode/compare/v0.4.5...v0.4.6
[0.4.5]: https://github.com/cnjack/jcode/compare/v0.4.4...v0.4.5
[0.4.4]: https://github.com/cnjack/jcode/compare/v0.4.3...v0.4.4
[0.4.3]: https://github.com/cnjack/jcode/compare/v0.4.2...v0.4.3
[0.4.2]: https://github.com/cnjack/jcode/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/cnjack/jcode/compare/v0.3.10...v0.4.1
[0.3.6]: https://github.com/cnjack/jcode/compare/v0.3.5...v0.3.6
[0.3.5]: https://github.com/cnjack/jcode/compare/v0.3.4...v0.3.5
[0.3.4]: https://github.com/cnjack/jcode/compare/v0.3.3...v0.3.4
[0.3.3]: https://github.com/cnjack/jcode/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/cnjack/jcode/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/cnjack/jcode/compare/v0.2.2...v0.3.1
[0.2.2]: https://github.com/cnjack/jcode/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/cnjack/jcode/compare/v0.1.1...v0.2.1
[0.1.1]: https://github.com/cnjack/jcode/compare/v0.0.5...v0.1.1
[0.0.5]: https://github.com/cnjack/jcode/compare/v0.0.4...v0.0.5
[0.0.4]: https://github.com/cnjack/jcode/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/cnjack/jcode/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/cnjack/jcode/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/cnjack/jcode/releases/tag/v0.0.1
