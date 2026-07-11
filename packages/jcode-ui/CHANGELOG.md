# Changelog

## 0.2.3 — 2026-07

Republish of 0.2.2, which was again published with npm and shipped the literal
`workspace:*` specifier (uninstallable — same failure as 0.2.0, now
deprecated). No code changes. Reminder: **always `pnpm publish`.**

## 0.2.2 — 2026-07 (with jcode-ui-core 0.2.1) — BROKEN on npm, use 0.2.3

Post-release review fixes (PR #133 findings):

- **AG-UI runtime**: `enqueueMessage` is now a real type-ahead queue — drafts
  composed mid-run are kept in `RuntimeState.queued` and drained one per
  natural turn end (never after a user `stop()`); `removeQueuedMessage` works.
  Previously queued drafts were silently dropped.
- **Composer**: `send()` keeps uploading/error attachment slots (only `done`
  ones are consumed) and resets dictation buffers so recognized text can't
  resurface after sending.
- **ConnectionBanner**: the "Reconnected" flash now auto-hides — the effect no
  longer cancels its own timeout via a self-triggering dependency.
- **WorkflowCanvas**: `interactive` can no longer be silently overridden by
  props spread; per-flag overrides still supported.
- **Transcription**: active-segment ref survives backward seeks.
- **FileTree renderer**: trailing annotations ("(dir)", size columns) are now
  actually stripped as documented.
- Packaging: `./package.json` added to both packages' exports; core selftest
  artifacts excluded from the published tarball; deprecated CSS
  (`word-break: break-word`, `clip`) replaced.

## 0.2.1 — 2026-07

Republish of 0.2.0 with correct dependency metadata: the 0.2.0 tarball was
published with npm, which leaked the literal `workspace:*` specifier for
`jcode-ui-core` and made the package uninstallable. No code changes.
`jcode-ui@0.2.0` is deprecated on the registry. Always publish with
`pnpm publish` (it rewrites workspace specifiers at pack time).

## 0.2.0 — 2026-07

The "complete the loop" release: scoped theming, the full conversation
lifecycle, a second-generation composer, streaming-stable markdown, and three
new optional subentries. See the
[migration guide](https://www.j-code.net/chat-ui/docs/guides/migration-0.2).

### Breaking

- **Scoped tokens.** All design tokens moved from `:root` to `[data-jcode-ui]`
  and gained a `--jcode-` prefix (`--color-primary` → `--jcode-color-primary`).
  Components stamp the attribute on their own roots; nothing leaks into host
  pages. Legacy names keep working via `jcode-ui/compat.css`; shadcn apps can
  inherit their theme via `jcode-ui/shadcn.css`.
- Animation classes/keyframes prefixed: `.animate-fade-in` →
  `.jcode-animate-fade-in`, `@keyframes fade-in` → `jcode-fade-in`, etc.
- `RuntimeState` gains a required `connection` slice (defaulted to
  `'connected'` by `normalizeState` — external-store/mock users unaffected).

### Conversation loop

- **BranchPicker** — `‹ 2/3 ›` version navigation on branched messages
  (`Message.versions` + `switchVersion`), plus **regenerate** and **👍/👎
  feedback** actions in the message footer and **retry** on failed turns. All
  fail-visible: controls render only when the host implements the action.
- **ConnectionBanner** — sticky `reconnecting / disconnected / reconnected`
  transport status driven by `RuntimeState.connection`.
- **ThreadWelcome + Suggestions** — empty-state hero and prompt pills
  (starters and follow-ups; `<Thread suggestions={…}>` renders under the last
  turn when idle).
- **ExportButton / exportThreadMarkdown** — download the timeline as portable
  markdown (tools fold into `<details>`, fences escape safely).
- **QuoteSelection + formatQuote** — select thread text → floating Quote
  button → blockquote lands in the composer via the new `ComposerHandle`.

### Approvals

- Host-defined **approval options** (`Approval.options`, arbitrary ids —
  ACP-compatible) render as one control per option; `allow_always` kinds keep
  the two-step arming UX. New optional action `resolveApprovalOption`.

### Composer 2

- **AttachmentAdapter** — pluggable uploads with progress + retry; generic
  file chips alongside the base64 image fast path. Drag & drop and paste
  (screenshots) built in.
- **Slots** — `leadingControls` / `trailingControls` / `footer`; new styled
  **ModelSelector** drops into `leadingControls`.
- **Dictation** — optional Web Speech input (`enableDictation`).
- `ChatInput`/`Composer` expose `ComposerHandle { insertText, focus }` via ref.

### Rendering

- **Streaming-stable markdown** — unclosed fences/emphasis/links/tables are
  completed before parse; finished blocks are cached (`useStreamingMarkdown`)
  so long streams stop re-rendering the whole document.
- **Code-block chrome** — language + filename bar (```` ```ts title=a.ts ````
  or `ts:a.ts`) with a copy button (`bindCodeBlockCopy`).
- **Optional plugins** — `jcode-ui/plugins/mermaid` and
  `jcode-ui/plugins/katex` (dynamic-import peers; zero cost when unused).

### Runtime-wired renderers & components

- **TaskList** (todo lists as a first-class component + `RuntimeTaskList`),
  **FileTree**, **TestResults** (go test / vitest parsing), **StackTrace**
  (Go/JS, `jcode-ui:open-file` events), **Artifact** container, and
  `slots` on `Message`/`ToolCallCard`.

### New subentries

- **`jcode-ui/canvas`** — agent workflow canvas on `@xyflow/react` (optional
  peer): status-aware nodes, animated edges, `toolTreeToGraph`.
- **`jcode-ui/voice`** — SpeechInput, Transcription, AudioPlayer,
  VoiceVisualizer (browser APIs only).
- **ThreadList** — `ThreadStore` contract + styled sidebar list
  (`createMockThreadStore` included).
- **`createAGUIRuntime`** — drive the components from any AG-UI backend
  (LangGraph, CrewAI, Mastra, …); SSE transport, JSON-Patch shared state,
  injectable transport for tests.

### Fixes

- Message → tool-card vertical rhythm tightened (hover actions row no longer
  reserves 30px).
- Approval primitive stamps its own root (theming works without a wrapper).

## 0.1.1 — 2026-06

Initial public release: Thread, ChatInput, Message, ToolCallCard (+9
renderers), ApprovalBanner, AskUserCard, ContextBar, Reasoning, Sources,
Attachment; ExternalStore/Mock runtimes; token-driven theming.
