---
title: Generated API
parent: API Reference
nav_order: 6
---

# Generated API

> Auto-generated from TypeScript sources on **2026-07-11**.
> Do not edit by hand — run `node script/generate_jcode_ui_api_docs.mjs`.
>
> Human-written guides: [Types](/chat-ui/docs/api/types) · [Runtime](/chat-ui/docs/api/runtime) · [Hooks](/chat-ui/docs/api/hooks) · [Primitives](/chat-ui/docs/api/primitives) · [Components](/chat-ui/docs/api/components).

**226** public symbols extracted.

## `jcode-ui`

| Symbol | Kind | Source |
|--------|------|--------|
| [`ApprovalBanner`](#jcode-ui-approvalbanner) | const | `packages/jcode-ui/src/components/ApprovalBanner.tsx` |
| [`Artifact`](#jcode-ui-artifact) | const | `packages/jcode-ui/src/components/Artifact.tsx` |
| [`AskUserCard`](#jcode-ui-askusercard) | const | `packages/jcode-ui/src/components/AskUserCard.tsx` |
| [`AudioPlayer`](#jcode-ui-audioplayer) | const | `packages/jcode-ui/src/voice/AudioPlayer.tsx` |
| [`BranchPicker`](#jcode-ui-branchpicker) | const | `packages/jcode-ui/src/components/BranchPicker.tsx` |
| [`BrowserShotRenderer`](#jcode-ui-browsershotrenderer) | const | `packages/jcode-ui/src/toolRenderers/browserShot.tsx` |
| [`CanvasControls`](#jcode-ui-canvascontrols) | const | `packages/jcode-ui/src/canvas/CanvasControls.tsx` |
| [`CanvasPanel`](#jcode-ui-canvaspanel) | const | `packages/jcode-ui/src/canvas/CanvasPanel.tsx` |
| [`ChatInput`](#jcode-ui-chatinput) | const | `packages/jcode-ui/src/components/ChatInput.tsx` |
| [`CompactToolRow`](#jcode-ui-compacttoolrow) | const | `packages/jcode-ui/src/components/CompactToolRow.tsx` |
| [`ConnectionBanner`](#jcode-ui-connectionbanner) | const | `packages/jcode-ui/src/components/ConnectionBanner.tsx` |
| [`ContextBar`](#jcode-ui-contextbar) | const | `packages/jcode-ui/src/components/ContextBar.tsx` |
| [`DiffRenderer`](#jcode-ui-diffrenderer) | const | `packages/jcode-ui/src/toolRenderers/diff.tsx` |
| [`ExploringGroupCard`](#jcode-ui-exploringgroupcard) | const | `packages/jcode-ui/src/components/ExploringGroupCard.tsx` |
| [`ExportButton`](#jcode-ui-exportbutton) | const | `packages/jcode-ui/src/components/ExportButton.tsx` |
| [`FileViewerRenderer`](#jcode-ui-fileviewerrenderer) | const | `packages/jcode-ui/src/toolRenderers/fileViewer.tsx` |
| [`GenericRenderer`](#jcode-ui-genericrenderer) | const | `packages/jcode-ui/src/toolRenderers/generic.tsx` |
| [`Message`](#jcode-ui-message) | const | `packages/jcode-ui/src/components/Message.tsx` |
| [`QuoteSelection`](#jcode-ui-quoteselection) | const | `packages/jcode-ui/src/components/QuoteSelection.tsx` |
| [`RuntimeTaskList`](#jcode-ui-runtimetasklist) | const | `packages/jcode-ui/src/components/TaskList.tsx` |
| [`SearchRenderer`](#jcode-ui-searchrenderer) | const | `packages/jcode-ui/src/toolRenderers/search.tsx` |
| [`SkillRenderer`](#jcode-ui-skillrenderer) | const | `packages/jcode-ui/src/toolRenderers/skill.tsx` |
| [`SpeechInput`](#jcode-ui-speechinput) | const | `packages/jcode-ui/src/voice/SpeechInput.tsx` |
| [`StackTraceRenderer`](#jcode-ui-stacktracerenderer) | const | `packages/jcode-ui/src/toolRenderers/stackTrace.tsx` |
| [`Suggestions`](#jcode-ui-suggestions) | const | `packages/jcode-ui/src/components/Suggestions.tsx` |
| [`TeamCreateRenderer`](#jcode-ui-teamcreaterenderer) | const | `packages/jcode-ui/src/toolRenderers/team.tsx` |
| [`TeamListRenderer`](#jcode-ui-teamlistrenderer) | const | `packages/jcode-ui/src/toolRenderers/team.tsx` |
| [`TeamMessageRenderer`](#jcode-ui-teammessagerenderer) | const | `packages/jcode-ui/src/toolRenderers/team.tsx` |
| [`TeamSpawnRenderer`](#jcode-ui-teamspawnrenderer) | const | `packages/jcode-ui/src/toolRenderers/team.tsx` |
| [`TestResultsRenderer`](#jcode-ui-testresultsrenderer) | const | `packages/jcode-ui/src/toolRenderers/testResults.tsx` |
| [`ThreadWelcome`](#jcode-ui-threadwelcome) | const | `packages/jcode-ui/src/components/ThreadWelcome.tsx` |
| [`TodoRenderer`](#jcode-ui-todorenderer) | const | `packages/jcode-ui/src/toolRenderers/todo.tsx` |
| [`ToolCallCard`](#jcode-ui-toolcallcard) | const | `packages/jcode-ui/src/components/ToolCallCard.tsx` |
| [`Transcription`](#jcode-ui-transcription) | const | `packages/jcode-ui/src/voice/Transcription.tsx` |
| [`VoiceVisualizer`](#jcode-ui-voicevisualizer) | const | `packages/jcode-ui/src/voice/VoiceVisualizer.tsx` |
| [`WorkflowCanvas`](#jcode-ui-workflowcanvas) | const | `packages/jcode-ui/src/canvas/WorkflowCanvas.tsx` |
| [`WorkflowNode`](#jcode-ui-workflownode) | const | `packages/jcode-ui/src/canvas/WorkflowNode.tsx` |
| [`ApiBaseProvider`](#jcode-ui-apibaseprovider) | function | `packages/jcode-ui/src/lib/apiBaseContext.tsx` |
| [`balanceEmphasis`](#jcode-ui-balanceemphasis) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`balanceInlineCode`](#jcode-ui-balanceinlinecode) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`bindCodeBlockCopy`](#jcode-ui-bindcodeblockcopy) | function | `packages/jcode-ui/src/lib/markdown.ts` |
| [`completeStreamingMarkdown`](#jcode-ui-completestreamingmarkdown) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`completeStreamingMarkdownInfo`](#jcode-ui-completestreamingmarkdowninfo) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`completeTableRow`](#jcode-ui-completetablerow) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`createDefaultToolRegistry`](#jcode-ui-createdefaulttoolregistry) | function | `packages/jcode-ui/src/components/ToolRegistryContext.tsx` |
| [`extCategory`](#jcode-ui-extcategory) | function | `packages/jcode-ui/src/toolRenderers/fileTree.tsx` |
| [`formatQuote`](#jcode-ui-formatquote) | function | `packages/jcode-ui/src/components/QuoteSelection.tsx` |
| [`formatRelative`](#jcode-ui-formatrelative) | function | `packages/jcode-ui/src/components/ThreadList.tsx` |
| [`hashString`](#jcode-ui-hashstring) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`headTail`](#jcode-ui-headtail) | function | `packages/jcode-ui/src/toolRenderers/terminal.tsx` |
| [`ModelSelector`](#jcode-ui-modelselector) | function | `packages/jcode-ui/src/components/ModelSelector.tsx` |
| [`parseCodeInfo`](#jcode-ui-parsecodeinfo) | function | `packages/jcode-ui/src/lib/markdown.ts` |
| [`parseStackTrace`](#jcode-ui-parsestacktrace) | function | `packages/jcode-ui/src/toolRenderers/stackTrace.tsx` |
| [`parseTestOutput`](#jcode-ui-parsetestoutput) | function | `packages/jcode-ui/src/toolRenderers/testResults.tsx` |
| [`Reasoning`](#jcode-ui-reasoning) | function | `packages/jcode-ui/src/components/Reasoning.tsx` |
| [`registerCodeBlockRenderer`](#jcode-ui-registercodeblockrenderer) | function | `packages/jcode-ui/src/lib/markdown.ts` |
| [`registerMathRenderer`](#jcode-ui-registermathrenderer) | function | `packages/jcode-ui/src/lib/markdown.ts` |
| [`renderMarkdown`](#jcode-ui-rendermarkdown) | function | `packages/jcode-ui/src/lib/markdown.ts` |
| [`renderMarkdownStreaming`](#jcode-ui-rendermarkdownstreaming) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`scanFenceState`](#jcode-ui-scanfencestate) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`Sources`](#jcode-ui-sources) | function | `packages/jcode-ui/src/components/Sources.tsx` |
| [`splitTopLevelBlocks`](#jcode-ui-splittoplevelblocks) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`stripTrailingLink`](#jcode-ui-striptrailinglink) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`Thread`](#jcode-ui-thread) | function | `packages/jcode-ui/src/components/Thread.tsx` |
| [`ToolRegistryProvider`](#jcode-ui-toolregistryprovider) | function | `packages/jcode-ui/src/components/ToolRegistryContext.tsx` |
| [`toolTreeToGraph`](#jcode-ui-tooltreetograph) | function | `packages/jcode-ui/src/canvas/toolTreeToGraph.ts` |
| [`truncate`](#jcode-ui-truncate) | function | `packages/jcode-ui/src/toolRenderers/terminal.tsx` |
| [`useStreamingMarkdown`](#jcode-ui-usestreamingmarkdown) | function | `packages/jcode-ui/src/lib/useStreamingMarkdown.ts` |
| [`useToolRegistry`](#jcode-ui-usetoolregistry) | function | `packages/jcode-ui/src/components/ToolRegistryContext.tsx` |
| [`ApiBaseProviderProps`](#jcode-ui-apibaseproviderprops) | interface | `packages/jcode-ui/src/lib/apiBaseContext.tsx` |
| [`ApprovalBannerProps`](#jcode-ui-approvalbannerprops) | interface | `packages/jcode-ui/src/components/ApprovalBanner.tsx` |
| [`ArtifactProps`](#jcode-ui-artifactprops) | interface | `packages/jcode-ui/src/components/Artifact.tsx` |
| [`AskUserCardProps`](#jcode-ui-askusercardprops) | interface | `packages/jcode-ui/src/components/AskUserCard.tsx` |
| [`AudioPlayerProps`](#jcode-ui-audioplayerprops) | interface | `packages/jcode-ui/src/voice/AudioPlayer.tsx` |
| [`BranchPickerProps`](#jcode-ui-branchpickerprops) | interface | `packages/jcode-ui/src/components/BranchPicker.tsx` |
| [`CanvasControlsProps`](#jcode-ui-canvascontrolsprops) | interface | `packages/jcode-ui/src/canvas/CanvasControls.tsx` |
| [`CanvasPanelProps`](#jcode-ui-canvaspanelprops) | interface | `packages/jcode-ui/src/canvas/CanvasPanel.tsx` |
| [`ChatInputProps`](#jcode-ui-chatinputprops) | interface | `packages/jcode-ui/src/components/ChatInput.tsx` |
| [`CodeBlockHookArgs`](#jcode-ui-codeblockhookargs) | interface | `packages/jcode-ui/src/lib/markdown.ts` |
| [`CompactToolRowProps`](#jcode-ui-compacttoolrowprops) | interface | `packages/jcode-ui/src/components/CompactToolRow.tsx` |
| [`CompletionResult`](#jcode-ui-completionresult) | interface | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`ContextBarProps`](#jcode-ui-contextbarprops) | interface | `packages/jcode-ui/src/components/ContextBar.tsx` |
| [`ExploringGroupCardProps`](#jcode-ui-exploringgroupcardprops) | interface | `packages/jcode-ui/src/components/ExploringGroupCard.tsx` |
| [`ExportButtonProps`](#jcode-ui-exportbuttonprops) | interface | `packages/jcode-ui/src/components/ExportButton.tsx` |
| [`KatexApi`](#jcode-ui-katexapi) | interface | `packages/jcode-ui/src/plugins/external-modules.d.ts` |
| [`KatexOptions`](#jcode-ui-katexoptions) | interface | `packages/jcode-ui/src/plugins/external-modules.d.ts` |
| [`KatexPluginOptions`](#jcode-ui-katexpluginoptions) | interface | `packages/jcode-ui/src/plugins/katex.ts` |
| [`MermaidApi`](#jcode-ui-mermaidapi) | interface | `packages/jcode-ui/src/plugins/external-modules.d.ts` |
| [`MermaidPluginOptions`](#jcode-ui-mermaidpluginoptions) | interface | `packages/jcode-ui/src/plugins/mermaid.ts` |
| [`MermaidRenderResult`](#jcode-ui-mermaidrenderresult) | interface | `packages/jcode-ui/src/plugins/external-modules.d.ts` |
| [`MessageProps`](#jcode-ui-messageprops) | interface | `packages/jcode-ui/src/components/Message.tsx` |
| [`MessageSlots`](#jcode-ui-messageslots) | interface | `packages/jcode-ui/src/components/Message.tsx` |
| [`ModelSelectorOption`](#jcode-ui-modelselectoroption) | interface | `packages/jcode-ui/src/components/ModelSelector.tsx` |
| [`ModelSelectorProps`](#jcode-ui-modelselectorprops) | interface | `packages/jcode-ui/src/components/ModelSelector.tsx` |
| [`ReasoningProps`](#jcode-ui-reasoningprops) | interface | `packages/jcode-ui/src/components/Reasoning.tsx` |
| [`RuntimeTaskListProps`](#jcode-ui-runtimetasklistprops) | interface | `packages/jcode-ui/src/components/TaskList.tsx` |
| [`SourcesProps`](#jcode-ui-sourcesprops) | interface | `packages/jcode-ui/src/components/Sources.tsx` |
| [`SpeechInputProps`](#jcode-ui-speechinputprops) | interface | `packages/jcode-ui/src/voice/SpeechInput.tsx` |
| [`StackFrame`](#jcode-ui-stackframe) | interface | `packages/jcode-ui/src/toolRenderers/stackTrace.tsx` |
| [`StackTrace`](#jcode-ui-stacktrace) | interface | `packages/jcode-ui/src/toolRenderers/stackTrace.tsx` |
| [`SuggestionItem`](#jcode-ui-suggestionitem) | interface | `packages/jcode-ui/src/components/Suggestions.tsx` |
| [`SuggestionsProps`](#jcode-ui-suggestionsprops) | interface | `packages/jcode-ui/src/components/Suggestions.tsx` |
| [`TestCase`](#jcode-ui-testcase) | interface | `packages/jcode-ui/src/toolRenderers/testResults.tsx` |
| [`TestSummary`](#jcode-ui-testsummary) | interface | `packages/jcode-ui/src/toolRenderers/testResults.tsx` |
| [`ThreadProps`](#jcode-ui-threadprops) | interface | `packages/jcode-ui/src/components/Thread.tsx` |
| [`ThreadWelcomeProps`](#jcode-ui-threadwelcomeprops) | interface | `packages/jcode-ui/src/components/ThreadWelcome.tsx` |
| [`ToolCallCardProps`](#jcode-ui-toolcallcardprops) | interface | `packages/jcode-ui/src/components/ToolCallCard.tsx` |
| [`ToolCallCardSlots`](#jcode-ui-toolcallcardslots) | interface | `packages/jcode-ui/src/components/ToolCallCard.tsx` |
| [`ToolGraph`](#jcode-ui-toolgraph) | interface | `packages/jcode-ui/src/canvas/toolTreeToGraph.ts` |
| [`ToolRegistryProviderProps`](#jcode-ui-toolregistryproviderprops) | interface | `packages/jcode-ui/src/components/ToolRegistryContext.tsx` |
| [`ToolTreeToGraphOptions`](#jcode-ui-tooltreetographoptions) | interface | `packages/jcode-ui/src/canvas/toolTreeToGraph.ts` |
| [`TranscriptionProps`](#jcode-ui-transcriptionprops) | interface | `packages/jcode-ui/src/voice/Transcription.tsx` |
| [`TranscriptSegment`](#jcode-ui-transcriptsegment) | interface | `packages/jcode-ui/src/voice/Transcription.tsx` |
| [`VoiceVisualizerProps`](#jcode-ui-voicevisualizerprops) | interface | `packages/jcode-ui/src/voice/VoiceVisualizer.tsx` |
| [`WorkflowCanvasProps`](#jcode-ui-workflowcanvasprops) | interface | `packages/jcode-ui/src/canvas/WorkflowCanvas.tsx` |
| [`CodeBlockHook`](#jcode-ui-codeblockhook) | type | `packages/jcode-ui/src/lib/markdown.ts` |
| [`JcodeStepData`](#jcode-ui-jcodestepdata) | type | `packages/jcode-ui/src/canvas/WorkflowNode.tsx` |
| [`JcodeStepNode`](#jcode-ui-jcodestepnode) | type | `packages/jcode-ui/src/canvas/WorkflowNode.tsx` |
| [`JcodeStepStatus`](#jcode-ui-jcodestepstatus) | type | `packages/jcode-ui/src/canvas/WorkflowNode.tsx` |
| [`MathRenderer`](#jcode-ui-mathrenderer) | type | `packages/jcode-ui/src/lib/markdown.ts` |
| [`SpeechInputStatus`](#jcode-ui-speechinputstatus) | type | `packages/jcode-ui/src/voice/SpeechInput.tsx` |

### `ApprovalBanner`

<!-- jcode-ui-approvalbanner -->

`const` · `packages/jcode-ui/src/components/ApprovalBanner.tsx`

```ts
export const ApprovalBanner = …
```

### `Artifact`

<!-- jcode-ui-artifact -->

`const` · `packages/jcode-ui/src/components/Artifact.tsx`

```ts
export const Artifact = …
```

### `AskUserCard`

<!-- jcode-ui-askusercard -->

`const` · `packages/jcode-ui/src/components/AskUserCard.tsx`

```ts
export const AskUserCard = …
```

### `AudioPlayer`

<!-- jcode-ui-audioplayer -->

`const` · `packages/jcode-ui/src/voice/AudioPlayer.tsx`

```ts
export const AudioPlayer = …
```

### `BranchPicker`

<!-- jcode-ui-branchpicker -->

`const` · `packages/jcode-ui/src/components/BranchPicker.tsx`

```ts
export const BranchPicker = …
```

### `BrowserShotRenderer`

<!-- jcode-ui-browsershotrenderer -->

`const` · `packages/jcode-ui/src/toolRenderers/browserShot.tsx`

```ts
export const BrowserShotRenderer = …
```

### `CanvasControls`

<!-- jcode-ui-canvascontrols -->

`const` · `packages/jcode-ui/src/canvas/CanvasControls.tsx`

```ts
export const CanvasControls = …
```

### `CanvasPanel`

<!-- jcode-ui-canvaspanel -->

`const` · `packages/jcode-ui/src/canvas/CanvasPanel.tsx`

```ts
export const CanvasPanel = …
```

### `ChatInput`

<!-- jcode-ui-chatinput -->

`const` · `packages/jcode-ui/src/components/ChatInput.tsx`

```ts
export const ChatInput = …
```

### `CompactToolRow`

<!-- jcode-ui-compacttoolrow -->

`const` · `packages/jcode-ui/src/components/CompactToolRow.tsx`

```ts
export const CompactToolRow = …
```

### `ConnectionBanner`

<!-- jcode-ui-connectionbanner -->

`const` · `packages/jcode-ui/src/components/ConnectionBanner.tsx`

```ts
export const ConnectionBanner = …
```

### `ContextBar`

<!-- jcode-ui-contextbar -->

`const` · `packages/jcode-ui/src/components/ContextBar.tsx`

```ts
export const ContextBar = …
```

### `DiffRenderer`

<!-- jcode-ui-diffrenderer -->

`const` · `packages/jcode-ui/src/toolRenderers/diff.tsx`

```ts
export const DiffRenderer = …
```

### `ExploringGroupCard`

<!-- jcode-ui-exploringgroupcard -->

`const` · `packages/jcode-ui/src/components/ExploringGroupCard.tsx`

```ts
export const ExploringGroupCard = …
```

### `ExportButton`

<!-- jcode-ui-exportbutton -->

`const` · `packages/jcode-ui/src/components/ExportButton.tsx`

```ts
export const ExportButton = …
```

### `FileViewerRenderer`

<!-- jcode-ui-fileviewerrenderer -->

`const` · `packages/jcode-ui/src/toolRenderers/fileViewer.tsx`

```ts
export const FileViewerRenderer = …
```

### `GenericRenderer`

<!-- jcode-ui-genericrenderer -->

`const` · `packages/jcode-ui/src/toolRenderers/generic.tsx`

```ts
export const GenericRenderer = …
```

### `Message`

<!-- jcode-ui-message -->

`const` · `packages/jcode-ui/src/components/Message.tsx`

```ts
export const Message = …
```

### `QuoteSelection`

<!-- jcode-ui-quoteselection -->

`const` · `packages/jcode-ui/src/components/QuoteSelection.tsx`

```ts
export const QuoteSelection = …
```

### `RuntimeTaskList`

<!-- jcode-ui-runtimetasklist -->

`const` · `packages/jcode-ui/src/components/TaskList.tsx`

TaskList bound to the runtime `todos` selector.

```ts
/** TaskList bound to the runtime `todos` selector. */
export const RuntimeTaskList = …
```

### `SearchRenderer`

<!-- jcode-ui-searchrenderer -->

`const` · `packages/jcode-ui/src/toolRenderers/search.tsx`

```ts
export const SearchRenderer = …
```

### `SkillRenderer`

<!-- jcode-ui-skillrenderer -->

`const` · `packages/jcode-ui/src/toolRenderers/skill.tsx`

```ts
export const SkillRenderer = …
```

### `SpeechInput`

<!-- jcode-ui-speechinput -->

`const` · `packages/jcode-ui/src/voice/SpeechInput.tsx`

```ts
export const SpeechInput = …
```

### `StackTraceRenderer`

<!-- jcode-ui-stacktracerenderer -->

`const` · `packages/jcode-ui/src/toolRenderers/stackTrace.tsx`

```ts
export const StackTraceRenderer = …
```

### `Suggestions`

<!-- jcode-ui-suggestions -->

`const` · `packages/jcode-ui/src/components/Suggestions.tsx`

```ts
export const Suggestions = …
```

### `TeamCreateRenderer`

<!-- jcode-ui-teamcreaterenderer -->

`const` · `packages/jcode-ui/src/toolRenderers/team.tsx`

```ts
export const TeamCreateRenderer = …
```

### `TeamListRenderer`

<!-- jcode-ui-teamlistrenderer -->

`const` · `packages/jcode-ui/src/toolRenderers/team.tsx`

```ts
export const TeamListRenderer = …
```

### `TeamMessageRenderer`

<!-- jcode-ui-teammessagerenderer -->

`const` · `packages/jcode-ui/src/toolRenderers/team.tsx`

```ts
export const TeamMessageRenderer = …
```

### `TeamSpawnRenderer`

<!-- jcode-ui-teamspawnrenderer -->

`const` · `packages/jcode-ui/src/toolRenderers/team.tsx`

```ts
export const TeamSpawnRenderer = …
```

### `TestResultsRenderer`

<!-- jcode-ui-testresultsrenderer -->

`const` · `packages/jcode-ui/src/toolRenderers/testResults.tsx`

```ts
export const TestResultsRenderer = …
```

### `ThreadWelcome`

<!-- jcode-ui-threadwelcome -->

`const` · `packages/jcode-ui/src/components/ThreadWelcome.tsx`

```ts
export const ThreadWelcome = …
```

### `TodoRenderer`

<!-- jcode-ui-todorenderer -->

`const` · `packages/jcode-ui/src/toolRenderers/todo.tsx`

```ts
export const TodoRenderer = …
```

### `ToolCallCard`

<!-- jcode-ui-toolcallcard -->

`const` · `packages/jcode-ui/src/components/ToolCallCard.tsx`

```ts
export const ToolCallCard = …
```

### `Transcription`

<!-- jcode-ui-transcription -->

`const` · `packages/jcode-ui/src/voice/Transcription.tsx`

```ts
export const Transcription = …
```

### `VoiceVisualizer`

<!-- jcode-ui-voicevisualizer -->

`const` · `packages/jcode-ui/src/voice/VoiceVisualizer.tsx`

```ts
export const VoiceVisualizer = …
```

### `WorkflowCanvas`

<!-- jcode-ui-workflowcanvas -->

`const` · `packages/jcode-ui/src/canvas/WorkflowCanvas.tsx`

```ts
export const WorkflowCanvas = …
```

### `WorkflowNode`

<!-- jcode-ui-workflownode -->

`const` · `packages/jcode-ui/src/canvas/WorkflowNode.tsx`

```ts
export const WorkflowNode = …
```

### `ApiBaseProvider`

<!-- jcode-ui-apibaseprovider -->

`function` · `packages/jcode-ui/src/lib/apiBaseContext.tsx`

```ts
export function ApiBaseProvider({ apiBase, children }: ApiBaseProviderProps) { … }
```

### `balanceEmphasis`

<!-- jcode-ui-balanceemphasis -->

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Close dangling emphasis runs (`**`, `*`, `_`). Counts are taken over prose
only (code stripped). Underscores are counted only when they flank a word
boundary, so `snake_case` and URLs don't trigger a false close.

```ts
export function balanceEmphasis(text: string): string { … }
```

### `balanceInlineCode`

<!-- jcode-ui-balanceinlinecode -->

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Drop the contents of *closed* fenced blocks (used for delimiter counting). */
function stripFenced(text: string): string {
  const out: string[] = []
  let open: Fence | null = null
  for (const line of text.split('\n')) {
    const m = FENCE_LINE.exec(line)
    if (m) {
      const char = m[2][0]
      const len = m[2].length
      const info = m[3]
      if (open == null) {
        open = { char, len }
        continue
      }
      if (char === open.char && len >= open.len && info.trim() === '') {
        open = null
        continue
      }
    }
    if (open == null) out.push(line)
  }
  return out.join('\n')
}

/** Blank out inline code spans so their delimiters don't skew emphasis counts. */
function stripInlineCode(text: string): string {
  return text.replace(/`+[^`]*`+/g, ' ')
}

/**
Close a dangling inline code span. Counts backticks in prose (outside fenced
blocks); an odd count means one span is open, so append a closing backtick.

```ts
export function balanceInlineCode(text: string): string { … }
```

### `bindCodeBlockCopy`

<!-- jcode-ui-bindcodeblockcopy -->

`function` · `packages/jcode-ui/src/lib/markdown.ts`

Attach one delegated click handler that powers every `.jcode-codeblock__copy`
button under `root`. Call once on the container that holds rendered markdown
(idempotent per element). On click, copies the decoded `data-code` payload and
flips the label to "Copied" for 1.5s.

@returns a cleanup function that removes the listener.

```ts
export function bindCodeBlockCopy(root: HTMLElement): () => void {
  const KEY = '__jcodeCopyBound'
  const el = root as HTMLElement & Record<string, unknown>
  if (el[KEY]) return () => {}
  el[KEY] = true

  const onClick = (ev: Event) => {
    const target = ev.target as HTMLElement | null
    const btn = target?.closest<HTMLElement>('.jcode-codeblock__copy')
    if (!btn || !root.contains(btn)) return
    const raw = btn.getAttribute('data-code')
    if (raw == null) return
    let code = raw
    try {
      code = decodeURIComponent(raw)
    } catch {
      /* keep raw if it was not encoded */
    }
    void navigator.clipboard?.writeText(code).then(
      () => flashCopied(btn),
      () => {
        /* clipboard unavailable */
      },
    )
  }

  root.addEventListener('click', onClick)
  return () => {
    root.removeEventListener('click', onClick)
    el[KEY] = false
  }
}

function flashCopied(btn: HTMLElement): void {
  if (btn.getAttribute('data-copied') === '1') return
  const prev = btn.textContent
  btn.setAttribute('data-copied', '1')
  btn.textContent = 'Copied'
  window.setTimeout(() => {
    btn.textContent = prev
    btn.removeAttribute('data-copied')
  }, 1500)
}
```

### `completeStreamingMarkdown`

<!-- jcode-ui-completestreamingmarkdown -->

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Pure completion: raw streaming buffer → renderable markdown string.

```ts
export function completeStreamingMarkdown(md: string): string { … }
```

### `completeStreamingMarkdownInfo`

<!-- jcode-ui-completestreamingmarkdowninfo -->

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

True when an open code fence was auto-closed (last block is streaming). */
  fenceStreaming: boolean
}

/**
Complete unclosed markdown structures. Reports whether a code fence was closed
so the renderer can flag the active code block (shimmer). See the module doc.

```ts
export function completeStreamingMarkdownInfo(md: string): CompletionResult { … }
```

### `completeTableRow`

<!-- jcode-ui-completetablerow -->

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Add the trailing pipe to a table row that is still being typed.

```ts
export function completeTableRow(text: string): string { … }
```

### `createDefaultToolRegistry`

<!-- jcode-ui-createdefaulttoolregistry -->

`function` · `packages/jcode-ui/src/components/ToolRegistryContext.tsx`

ToolRendererRegistry context — the host provides a registry (built with
createDefaultToolRegistry or a custom one) once at the app root, and every
ToolCallCard reads from it. This avoids prop-drilling the registry through
every component.
/

import { createContext, useContext } from 'react'
import type { ReactNode } from 'react'
import { createToolRendererRegistry } from 'jcode-ui-core/adapters'
import type { ToolRendererRegistry, ToolRenderer } from 'jcode-ui-core/adapters'
import { TerminalRenderer } from '../toolRenderers/terminal.js'
import { FileViewerRenderer } from '../toolRenderers/fileViewer.js'
import { DiffRenderer } from '../toolRenderers/diff.js'
import { SearchRenderer } from '../toolRenderers/search.js'
import { TodoRenderer } from '../toolRenderers/todo.js'
import { SkillRenderer } from '../toolRenderers/skill.js'
import {
  TeamListRenderer,
  TeamMessageRenderer,
  TeamCreateRenderer,
  TeamSpawnRenderer,
} from '../toolRenderers/team.js'
import { BrowserShotRenderer } from '../toolRenderers/browserShot.js'
import { FileTreeRenderer } from '../toolRenderers/fileTree.js'
import { GenericRenderer } from '../toolRenderers/generic.js'

const Ctx = createContext<ToolRendererRegistry | null>(null)

/** Build the jcode default registry (matches Vue ToolCallCard renderType map).

```ts
export function createDefaultToolRegistry(): ToolRendererRegistry { … }
```

### `extCategory`

<!-- jcode-ui-extcategory -->

`function` · `packages/jcode-ui/src/toolRenderers/fileTree.tsx`

FileTreeRenderer — `list_dir` / `glob`. Parses a line-separated path list
(tolerant of leading bullets / tree glyphs) into a collapsible tree.

- Directories collapse/expand (chevron + folder icon).
- Files get an extension-colored dot (colors resolve to theme tokens in p5.css).
- Rows highlight on hover.
- Default: fully expanded. When the tree exceeds 200 nodes, only the top
  level stays open (second level and below collapse) to keep it scannable.
/

import { memo, useMemo, useState } from 'react'
import { ChevronRightIcon, FolderIcon, FolderOpenIcon } from '@heroicons/react/24/outline'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

interface TreeNode {
  name: string
  path: string
  isDir: boolean
  children: TreeNode[]
}

const LARGE_TREE_THRESHOLD = 200

export const FileTreeRenderer = memo(function FileTreeRenderer({
  output,
  displayOutput,
  error,
  status,
}: ToolRendererProps) {
  const raw = displayOutput || output || ''
  const { roots, count } = useMemo(() => parsePathList(raw), [raw])

  if (roots.length === 0) {
    if (error) {
      return (
        <div className="jcode-filetree__msg jcode-filetree__msg--error">{error}</div>
      )
    }
    if (status === 'running') {
      return <div className="jcode-filetree__msg animate-pulse">Listing…</div>
    }
    if (raw.trim()) {
      // Parsed nothing but there is text — show it raw rather than an empty box.
      return <pre className="jcode-filetree__raw">{raw}</pre>
    }
    return <div className="jcode-filetree__msg">Empty</div>
  }

  // >200 nodes: keep only the top level open.
  const initialOpenDepth = count > LARGE_TREE_THRESHOLD ? 1 : Infinity

  return (
    <div data-jcode-ui="" className="jcode-filetree">
      {count > LARGE_TREE_THRESHOLD && (
        <div className="jcode-filetree__hint">{count} entries · deep folders collapsed</div>
      )}
      <ul className="jcode-filetree__list" role="tree">
        {roots.map((node) => (
          <TreeRow key={node.path} node={node} depth={0} initialOpenDepth={initialOpenDepth} />
        ))}
      </ul>
    </div>
  )
})

function TreeRow({
  node,
  depth,
  initialOpenDepth,
}: {
  node: TreeNode
  depth: number
  initialOpenDepth: number
}) {
  const [open, setOpen] = useState(depth < initialOpenDepth)
  const indent = { paddingLeft: `${depth * 0.85 + 0.35}rem` }

  if (!node.isDir) {
    return (
      <li role="treeitem" className="jcode-filetree__row jcode-filetree__row--file" style={indent}>
        <span className="jcode-filetree__lead" aria-hidden />
        <span className={`jcode-filetree__dot jcode-filetree__dot--${extCategory(node.name)}`} aria-hidden />
        <span className="jcode-filetree__name">{node.name}</span>
      </li>
    )
  }

  return (
    <li role="treeitem" aria-expanded={open} className="jcode-filetree__group">
      <button
        type="button"
        className="jcode-filetree__row jcode-filetree__row--dir"
        style={indent}
        onClick={() => setOpen((v) => !v)}
      >
        <ChevronRightIcon className={`jcode-filetree__chevron${open ? ' jcode-filetree__chevron--open' : ''}`} />
        {open ? (
          <FolderOpenIcon className="jcode-filetree__folder" />
        ) : (
          <FolderIcon className="jcode-filetree__folder" />
        )}
        <span className="jcode-filetree__name jcode-filetree__name--dir">{node.name}</span>
        {!open && node.children.length > 0 && (
          <span className="jcode-filetree__badge">{node.children.length}</span>
        )}
      </button>
      {open && node.children.length > 0 && (
        <ul className="jcode-filetree__list" role="group">
          {node.children.map((child) => (
            <TreeRow
              key={child.path}
              node={child}
              depth={depth + 1}
              initialOpenDepth={initialOpenDepth}
            />
          ))}
        </ul>
      )}
    </li>
  )
}

/** Strip common list decorations and return the bare path (or ''). */
function cleanLine(line: string): string {
  let s = line.replace(/\r$/, '')
  // Drop leading tree glyphs / bullets / whitespace.
  s = s.replace(/^[\s│├└─\-*•▸▾▪]+/, '')
  s = s.trim()
  // Drop trailing annotations like "  (dir)" or size columns — keep first token
  // only when it clearly contains a path separator or looks like a filename.
  return s
}

export function parsePathList(text: string): { roots: TreeNode[]; count: number } {
  const nodes = new Map<string, TreeNode>() // full path → node
  const roots: TreeNode[] = []
  const seenLines = new Set<string>()

  const getOrCreate = (
    path: string,
    name: string,
    isDir: boolean,
    parent: TreeNode | null,
  ): TreeNode => {
    let node = nodes.get(path)
    if (!node) {
      node = { name, path, isDir, children: [] }
      nodes.set(path, node)
      if (parent) parent.children.push(node)
      else roots.push(node)
    } else if (isDir && !node.isDir) {
      node.isDir = true
    }
    return node
  }

  for (const rawLine of text.split('\n')) {
    const cleaned = cleanLine(rawLine)
    if (!cleaned || seenLines.has(cleaned)) continue
    seenLines.add(cleaned)
    const explicitDir = cleaned.endsWith('/')
    const path = cleaned.replace(/\/+$/, '')
    const segments = path.split('/').filter(Boolean)
    if (segments.length === 0) continue

    let parent: TreeNode | null = null
    let acc = ''
    segments.forEach((seg, i) => {
      acc = acc ? `${acc}/${seg}` : seg
      const isDir = i < segments.length - 1 || explicitDir
      parent = getOrCreate(acc, seg, isDir, parent)
    })
  }

  sortNodes(roots)
  return { roots, count: nodes.size }
}

/** Directories first, then case-insensitive name order (recursive). */
function sortNodes(nodes: TreeNode[]): void {
  nodes.sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1
    return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
  })
  for (const n of nodes) if (n.children.length > 1) sortNodes(n.children)
}

/** Map a filename to a dot color category (resolved to a token in p5.css).

```ts
export function extCategory(name: string): string { … }
```

### `formatQuote`

<!-- jcode-ui-formatquote -->

`function` · `packages/jcode-ui/src/components/QuoteSelection.tsx`

QuoteSelection — "quote this" affordance for selected thread text.

Watches text selections inside jcode-ui prose (`.jcode-prose` under a
`[data-jcode-ui]` root) and floats a small Quote button at the selection.
Picking it hands the text to `onQuote` — typically wired into the composer:

  const input = useRef<ComposerHandle>(null)
  <QuoteSelection onQuote={(t) => input.current?.insertText(formatQuote(t))} />
  <ChatInput ref={input} />

Renders in a portal so ancestor overflow/transform can't clip the button.
/

import { memo, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { ChatBubbleBottomCenterTextIcon } from '@heroicons/react/24/outline'

export interface QuoteSelectionProps {
  /** Receives the selected plain text when the user clicks Quote. */
  onQuote: (text: string) => void
  /** Button label. Default "Quote". */
  label?: string
  /** Max characters captured. Default 2000. */
  maxLength?: number
}

/** Turn selected text into a markdown blockquote block for the composer.

```ts
export function formatQuote(text: string): string { … }
```

### `formatRelative`

<!-- jcode-ui-formatrelative -->

`function` · `packages/jcode-ui/src/components/ThreadList.tsx`

ThreadList — session/thread list sidebar (the sidebar analog of `Thread`).

Reads a `ThreadStore` (via `ThreadStoreProvider` from jcode-ui-core) and
renders grouped rows: an Active group and a collapsible Archived group. Each
row shows the title, a relative timestamp, and a pulsing dot while running;
the active row gets an `--jcode-accent-wash` fill plus a 2px accent bar.

Fail-visible: per-row controls (rename / archive / delete) and the New button
render only when the matching `store.actions.*` exists — mirroring how
`Message` shows its edit affordance only when `canEdit` is set. A host that
wires just `select` gets a clean read-only list with no dangling controls.

All visuals live in `../styles/threadlist.css` (`.jcode-threadlist-*`), which
the host imports via `jcode-ui/styles.css`. Style is aligned with Sources /
ContextBar: `data-jcode-ui` root, token-driven colors, heroicons, memo.
/

import { memo, useEffect, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'
import {
  ArchiveBoxIcon,
  ChatBubbleLeftRightIcon,
  ChevronRightIcon,
  EllipsisHorizontalIcon,
  PencilSquareIcon,
  PlusIcon,
  TrashIcon,
} from '@heroicons/react/24/outline'
import type { ThreadSummary, ThreadStoreActions } from 'jcode-ui-core'
import { useThreadListState, useThreadStoreActions } from 'jcode-ui-core'

export interface ThreadListProps {
  /** Optional small header label above the list (e.g. "Sessions"). */
  title?: string
  /** Extra class on the root (composed after `jcode-threadlist`). */
  className?: string
}

export const ThreadList = memo(function ThreadList({ title, className }: ThreadListProps) {
  const { threads, activeId, loading } = useThreadListState()
  const actions = useThreadStoreActions()
  const [renamingId, setRenamingId] = useState<string | null>(null)
  const [archivedOpen, setArchivedOpen] = useState(false)

  const { active, archived } = useMemo(() => {
    const a: ThreadSummary[] = []
    const ar: ThreadSummary[] = []
    for (const t of threads) (t.archived ? ar : a).push(t)
    const byRecency = (x: ThreadSummary, y: ThreadSummary) => y.updatedAt - x.updatedAt
    return { active: a.sort(byRecency), archived: ar.sort(byRecency) }
  }, [threads])

  const canCreate = !!actions.create

  // Empty state — centered prompt + New button (fail-visible).
  if (threads.length === 0 && !loading) {
    return (
      <div data-jcode-ui="" className={cx('jcode-threadlist jcode-threadlist--empty', className)}>
        <div className="jcode-threadlist-empty">
          <ChatBubbleLeftRightIcon className="jcode-threadlist-empty-icon" />
          <p className="jcode-threadlist-empty-text">No threads yet</p>
          {canCreate && (
            <button type="button" className="jcode-threadlist-new" onClick={() => actions.create!()}>
              <PlusIcon className="jcode-threadlist-icon" />
              New thread
            </button>
          )}
        </div>
      </div>
    )
  }

  return (
    <div data-jcode-ui="" className={cx('jcode-threadlist', className)}>
      {(title || canCreate) && (
        <div className="jcode-threadlist-header">
          {title && <span className="jcode-threadlist-title">{title}</span>}
          {canCreate && (
            <button type="button" className="jcode-threadlist-new" onClick={() => actions.create!()}>
              <PlusIcon className="jcode-threadlist-icon" />
              New thread
            </button>
          )}
        </div>
      )}

      <div className="jcode-threadlist-scroll">
        {loading && threads.length === 0 && (
          <div className="jcode-threadlist-loading">Loading…</div>
        )}

        {active.length > 0 && (
          <div className="jcode-threadlist-group">
            {archived.length > 0 && <div className="jcode-threadlist-group-label">Active</div>}
            <ul className="jcode-threadlist-rows">
              {active.map((t) => (
                <ThreadRow
                  key={t.id}
                  thread={t}
                  isActive={t.id === activeId}
                  actions={actions}
                  renaming={renamingId === t.id}
                  onStartRename={() => setRenamingId(t.id)}
                  onStopRename={() => setRenamingId(null)}
                />
              ))}
            </ul>
          </div>
        )}

        {archived.length > 0 && (
          <div className="jcode-threadlist-group">
            <button
              type="button"
              className="jcode-threadlist-archived-toggle"
              aria-expanded={archivedOpen}
              onClick={() => setArchivedOpen((o) => !o)}
            >
              <ChevronRightIcon
                className={cx(
                  'jcode-threadlist-chevron',
                  archivedOpen && 'jcode-threadlist-chevron--open',
                )}
              />
              Archived
              <span className="jcode-threadlist-count">{archived.length}</span>
            </button>
            {archivedOpen && (
              <ul className="jcode-threadlist-rows">
                {archived.map((t) => (
                  <ThreadRow
                    key={t.id}
                    thread={t}
                    isActive={t.id === activeId}
                    actions={actions}
                    renaming={renamingId === t.id}
                    onStartRename={() => setRenamingId(t.id)}
                    onStopRename={() => setRenamingId(null)}
                  />
                ))}
              </ul>
            )}
          </div>
        )}
      </div>
    </div>
  )
})

interface ThreadRowProps {
  thread: ThreadSummary
  isActive: boolean
  actions: ThreadStoreActions
  renaming: boolean
  onStartRename: () => void
  onStopRename: () => void
}

const ThreadRow = memo(function ThreadRow({
  thread,
  isActive,
  actions,
  renaming,
  onStartRename,
  onStopRename,
}: ThreadRowProps) {
  const [draft, setDraft] = useState(thread.title)

  // Re-seed the draft each time this row enters rename mode.
  useEffect(() => {
    if (renaming) setDraft(thread.title)
  }, [renaming, thread.title])

  const commit = () => {
    const text = draft.trim()
    onStopRename()
    if (text && text !== thread.title) actions.rename?.(thread.id, text)
  }

  if (renaming) {
    return (
      <li className="jcode-threadlist-item">
        <input
          className="jcode-threadlist-rename-input"
          value={draft}
          aria-label="Rename thread"
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              commit()
            } else if (e.key === 'Escape') {
              e.preventDefault()
              onStopRename()
            }
          }}
          // Clicking away discards the edit (Enter saves, Esc cancels).
          onBlur={onStopRename}
          // eslint-disable-next-line jsx-a11y/no-autofocus
          autoFocus
        />
      </li>
    )
  }

  const relative = formatRelative(thread.updatedAt)

  return (
    <li className="jcode-threadlist-item">
      <button
        type="button"
        className={cx('jcode-threadlist-row', isActive && 'jcode-threadlist-row--active')}
        aria-current={isActive ? 'page' : undefined}
        onClick={() => actions.select?.(thread.id)}
      >
        <span className="jcode-threadlist-row-main">
          <span className="jcode-threadlist-row-title">{thread.title || 'Untitled'}</span>
          <span className="jcode-threadlist-row-meta">
            {thread.status === 'running' && (
              <span className="jcode-threadlist-dot" title="Running" aria-hidden="true" />
            )}
            <span>{relative}</span>
          </span>
        </span>
      </button>
      <RowActions thread={thread} actions={actions} onRename={onStartRename} />
    </li>
  )
})

interface RowActionsProps {
  thread: ThreadSummary
  actions: ThreadStoreActions
  onRename: () => void
}

/** The hover ⋯ menu. Owns its own open state + focus management so click-outside
 auto-closes any other row's menu. Renders nothing if no menu action exists. */
function RowActions({ thread, actions, onRename }: RowActionsProps) {
  const [open, setOpen] = useState(false)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  const canRename = !!actions.rename
  const canArchive = !!actions.archive && !thread.archived
  const canRemove = !!actions.remove

  const close = (returnFocus?: boolean) => {
    setOpen(false)
    if (returnFocus) triggerRef.current?.focus()
  }

  useEffect(() => {
    if (!open) return
    // Focus the first item on open.
    menuRef.current?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus()
    // Dismiss on outside pointer-down.
    const onDoc = (e: MouseEvent) => {
      const target = e.target as Node
      if (!menuRef.current?.contains(target) && !triggerRef.current?.contains(target)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  if (!canRename && !canArchive && !canRemove) return null

  const onMenuKeyDown = (e: ReactKeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      close(true)
      return
    }
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      const items = Array.from(
        menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [],
      )
      if (items.length === 0) return
      const idx = items.indexOf(document.activeElement as HTMLButtonElement)
      const delta = e.key === 'ArrowDown' ? 1 : -1
      const next = (idx + delta + items.length) % items.length
      items[next]?.focus()
    }
  }

  return (
    <div className="jcode-threadlist-actions">
      <button
        ref={triggerRef}
        type="button"
        className="jcode-threadlist-menu-btn"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="Thread options"
        onClick={() => setOpen((o) => !o)}
      >
        <EllipsisHorizontalIcon className="jcode-threadlist-icon" />
      </button>
      {open && (
        <div ref={menuRef} role="menu" className="jcode-threadlist-menu" onKeyDown={onMenuKeyDown}>
          {canRename && (
            <button
              type="button"
              role="menuitem"
              className="jcode-threadlist-menu-item"
              onClick={() => {
                close()
                onRename()
              }}
            >
              <PencilSquareIcon className="jcode-threadlist-icon" />
              Rename
            </button>
          )}
          {canArchive && (
            <button
              type="button"
              role="menuitem"
              className="jcode-threadlist-menu-item"
              onClick={() => {
                close()
                actions.archive?.(thread.id)
              }}
            >
              <ArchiveBoxIcon className="jcode-threadlist-icon" />
              Archive
            </button>
          )}
          {canRemove && (
            <button
              type="button"
              role="menuitem"
              className="jcode-threadlist-menu-item jcode-threadlist-menu-item--danger"
              onClick={() => {
                close()
                actions.remove?.(thread.id)
              }}
            >
              <TrashIcon className="jcode-threadlist-icon" />
              Delete
            </button>
          )}
        </div>
      )}
    </div>
  )
}

/** Join truthy class fragments (tiny local helper — no clsx dependency). */
function cx(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(' ')
}

/**
Compact relative-time formatter (no date-fns / dayjs). Buckets: just now,
Nm, Nh, Nd, Nw, Nmo, Ny. `now` is injectable for deterministic tests.

```ts
export function formatRelative(ts: number, now: number = Date.now()): string { … }
```

### `hashString`

<!-- jcode-ui-hashstring -->

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Fast, stable string hash (djb2 + length) for block cache keys.

```ts
export function hashString(s: string): string { … }
```

### `headTail`

<!-- jcode-ui-headtail -->

`function` · `packages/jcode-ui/src/toolRenderers/terminal.tsx`

TerminalRenderer — `execute` tool with dual-channel streams/meta support.
Head/tail preview, stderr separation, exit/duration badge.
/

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

const HEAD_LINES = 5
const TAIL_LINES = 5

export const TerminalRenderer = memo(function TerminalRenderer({
  args,
  output,
  displayOutput,
  error,
  status,
  streams,
  meta,
}: ToolRendererProps) {
  let command = ''
  try {
    const parsed = JSON.parse(args)
    command = parsed.command ?? ''
  } catch {
    // ignore
  }

  const stdout = streams?.stdout ?? ''
  const stderr = streams?.stderr ?? ''
  const hasStreams = !!(stdout || stderr)
  const body = hasStreams
    ? ''
    : displayOutput || output || ''

  const stdoutPreview = useMemo(() => (stdout ? headTail(stdout, HEAD_LINES, TAIL_LINES) : null), [stdout])
  const stderrPreview = useMemo(() => (stderr ? headTail(stderr, HEAD_LINES, TAIL_LINES) : null), [stderr])
  const bodyPreview = useMemo(() => (body ? headTail(body, HEAD_LINES, TAIL_LINES) : null), [body])

  const exitCode = meta?.exit_code
  const durationMs = meta?.duration_ms
  const failed = status === 'error' || (typeof exitCode === 'number' && exitCode !== 0)

  return (
    <div className="jcode-terminal max-h-72 overflow-y-auto px-3 py-2.5 font-mono text-xs leading-relaxed">
      {command && (
        <div className="jcode-terminal__cmd">
          <span className="jcode-terminal__prompt select-none">$ </span>
          <span className="jcode-terminal__command">{command}</span>
        </div>
      )}

      {(typeof exitCode === 'number' || typeof durationMs === 'number') && (
        <div
          className="jcode-terminal__meta mt-1.5 flex flex-wrap gap-2 text-[10px] tabular-nums"
          style={{
            color: failed
              ? 'var(--jcode-color-error-fg)'
              : 'color-mix(in srgb, var(--jcode-code-fg, var(--jcode-color-muted-foreground)) 65%, transparent)',
          }}
        >
          {typeof exitCode === 'number' && (
            <span data-testid="terminal-exit">exit {exitCode}</span>
          )}
          {typeof durationMs === 'number' && durationMs > 0 && (
            <span data-testid="terminal-duration">{formatDuration(durationMs)}</span>
          )}
          {meta?.truncated && <span>truncated</span>}
        </div>
      )}

      {stdoutPreview && (
        <div className="jcode-terminal__out mt-1.5 whitespace-pre-wrap break-all" data-testid="terminal-stdout">
          {stdoutPreview.head}
          {stdoutPreview.omitted > 0 && (
            <div className="jcode-terminal__ellipsis opacity-60">… +{stdoutPreview.omitted} lines</div>
          )}
          {stdoutPreview.tail ? `\n${stdoutPreview.tail}` : null}
        </div>
      )}

      {stderrPreview && (
        <div className="jcode-terminal__err mt-1.5 whitespace-pre-wrap break-all" data-testid="terminal-stderr">
          <div className="jcode-terminal__stream-label mb-0.5 text-[10px] uppercase tracking-wide opacity-80">
            stderr
          </div>
          {stderrPreview.head}
          {stderrPreview.omitted > 0 && (
            <div className="jcode-terminal__ellipsis opacity-60">… +{stderrPreview.omitted} lines</div>
          )}
          {stderrPreview.tail ? `\n${stderrPreview.tail}` : null}
        </div>
      )}

      {!hasStreams && bodyPreview && (
        <div className="jcode-terminal__out mt-1.5 whitespace-pre-wrap break-all">
          {bodyPreview.head}
          {bodyPreview.omitted > 0 && (
            <div className="jcode-terminal__ellipsis opacity-60">… +{bodyPreview.omitted} lines</div>
          )}
          {bodyPreview.tail ? `\n${bodyPreview.tail}` : null}
        </div>
      )}

      {error && <div className="jcode-terminal__err mt-1 whitespace-pre-wrap">{error}</div>}
      {status === 'running' && <div className="jcode-terminal__run mt-1 animate-pulse">Running…</div>}
    </div>
  )
})

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

/** Keep head + tail lines with an omitted count (Codex-style mid ellipsis).

```ts
export function headTail(
  text: string,
  head: number,
  tail: number,
): { … }
```

### `ModelSelector`

<!-- jcode-ui-modelselector -->

`function` · `packages/jcode-ui/src/components/ModelSelector.tsx`

```ts
export function ModelSelector({
  models,
  value,
  onChange,
  disabled = false,
  placeholder = 'Select model',
  className,
}: ModelSelectorProps) { … }
```

### `parseCodeInfo`

<!-- jcode-ui-parsecodeinfo -->

`function` · `packages/jcode-ui/src/lib/markdown.ts`

Escape text for use as HTML text content or a double-quoted attribute. */
function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => HTML_ESCAPES[c])
}

// ─── Info-string parsing ───────────────────────────────────────────────────

/**
Parse a fenced-code info string into `{ lang, filename }`.
Supports two filename conventions:
  ```` ```ts title=a.ts ````  (also title="a b.ts")
  ```` ```ts:a.ts ````

```ts
export function parseCodeInfo(info: string): { … }
```

### `parseStackTrace`

<!-- jcode-ui-parsestacktrace -->

`function` · `packages/jcode-ui/src/toolRenderers/stackTrace.tsx`

```ts
export function parseStackTrace(text: string): StackTrace | null { … }
```

### `parseTestOutput`

<!-- jcode-ui-parsetestoutput -->

`function` · `packages/jcode-ui/src/toolRenderers/testResults.tsx`

```ts
export function parseTestOutput(text: string): TestSummary | null { … }
```

### `Reasoning`

<!-- jcode-ui-reasoning -->

`function` · `packages/jcode-ui/src/components/Reasoning.tsx`

```ts
export function Reasoning({ reasoning, defaultExpanded = false, durationMs }: ReasoningProps) { … }
```

### `registerCodeBlockRenderer`

<!-- jcode-ui-registercodeblockrenderer -->

`function` · `packages/jcode-ui/src/lib/markdown.ts`

Register a fenced-code-block renderer. Hooks run in registration order; the
first to return non-null wins (used by the mermaid plugin for ```` ```mermaid ````).

```ts
export function registerCodeBlockRenderer(hook: CodeBlockHook): void { … }
```

### `registerMathRenderer`

<!-- jcode-ui-registermathrenderer -->

`function` · `packages/jcode-ui/src/lib/markdown.ts`

Register a math renderer and (once) install the `$…$` / `$$…$$` tokenizers.
Until this is called, math delimiters are left as literal text — so a doc
that never registers katex renders `$x^2$` verbatim (and pays no math cost).

```ts
export function registerMathRenderer(render: MathRenderer): void { … }
```

### `renderMarkdown`

<!-- jcode-ui-rendermarkdown -->

`function` · `packages/jcode-ui/src/lib/markdown.ts`

Wrap each <table> so wide GFM tables scroll inside a framed container. */
function wrapTables(html: string): string {
  return html
    .replace(/<table(\s[^>]*)?>/gi, '<div class="jcode-md-table-wrap"><table$1>')
    .replace(/<\/table>/gi, '</table></div>')
}

// ─── Sanitization (DOM-only) ────────────────────────────────────────────────

// DOMPurify's default export auto-binds to `window` at import time when a DOM
// exists; in Node it stays an uninitialized factory with no `.sanitize`. Guard
// on capability so SSR/tests get a pass-through (the HTML is only ever injected
// in the browser, which always has a real sanitizer).
const canSanitize = typeof DOMPurify.sanitize === 'function'

const SANITIZE_CONFIG = {
  // class → code-block chrome + table wrap; data-* → copy button payload + meta;
  // style → katex inline layout; target → external links; mark → highlights.
  ADD_ATTR: ['target', 'class', 'style', 'data-code', 'data-lang', 'data-filename', 'data-mermaid-src'],
  ADD_TAGS: ['mark'],
}

function sanitize(html: string): string {
  return canSanitize ? DOMPurify.sanitize(html, SANITIZE_CONFIG) : html
}

/** Render markdown → sanitized HTML string.

```ts
export function renderMarkdown(text: string): string { … }
```

### `renderMarkdownStreaming`

<!-- jcode-ui-rendermarkdownstreaming -->

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Tag the last `.jcode-codeblock` as streaming so CSS can shimmer its tail. */
function markStreamingCodeblock(html: string): string {
  const MARK = 'class="jcode-codeblock"'
  const i = html.lastIndexOf(MARK)
  if (i < 0) return html
  return html.slice(0, i) + 'class="jcode-codeblock jcode-code-streaming"' + html.slice(i + MARK.length)
}

/**
Render a streaming buffer to sanitized HTML. Equivalent to
`renderMarkdown(completeStreamingMarkdown(md))`, plus a shimmer class on the
code block that is still streaming.

```ts
export function renderMarkdownStreaming(md: string): string { … }
```

### `scanFenceState`

<!-- jcode-ui-scanfencestate -->

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Streaming-stable markdown.

While an assistant message streams token-by-token, the tail of the buffer is
almost always mid-structure — an open code fence, a half-typed `**bold`, a
dangling `[link`. Rendering that raw produces flicker (a stray ``` turns the
rest of the doc into a code block for one frame). `completeStreamingMarkdown`
closes those open structures so every intermediate frame is valid markdown.

All helpers are pure and individually exported for unit testing.
/

import { renderMarkdown } from './markdown.js'

/** Matches a line that opens or closes a fenced code block (indent ≤ 3). */
const FENCE_LINE = /^(\s{0,3})(`{3,}|~{3,})(.*)$/

interface Fence {
  char: string
  len: number
}

/**
Scan for an unterminated code fence. Returns the open fence descriptor (so the
caller knows what to close with), or `null` when all fences are balanced.

```ts
export function scanFenceState(md: string): Fence | null { … }
```

### `Sources`

<!-- jcode-ui-sources -->

`function` · `packages/jcode-ui/src/components/Sources.tsx`

```ts
export function Sources({ sources }: SourcesProps) { … }
```

### `splitTopLevelBlocks`

<!-- jcode-ui-splittoplevelblocks -->

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Split markdown into top-level blocks on blank lines, keeping fenced code
blocks intact. Used to memoize completed blocks during streaming so per-frame
work is O(active block) instead of O(whole document).

```ts
export function splitTopLevelBlocks(md: string): string[] { … }
```

### `stripTrailingLink`

<!-- jcode-ui-striptrailinglink -->

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Remove a half-typed link/image at the very end of the buffer:
  `[label`         → dangling label
  `[label](url`    → dangling destination
Leaves completed links untouched.

```ts
export function stripTrailingLink(text: string): string { … }
```

### `Thread`

<!-- jcode-ui-thread -->

`function` · `packages/jcode-ui/src/components/Thread.tsx`

```ts
export function Thread({
  virtualize,
  emptyState,
  suggestions,
  renderPending,
  className,
  overscanBottom,
}: ThreadProps): ReactNode { … }
```

### `ToolRegistryProvider`

<!-- jcode-ui-toolregistryprovider -->

`function` · `packages/jcode-ui/src/components/ToolRegistryContext.tsx`

```ts
export function ToolRegistryProvider({ registry, children }: ToolRegistryProviderProps) { … }
```

### `toolTreeToGraph`

<!-- jcode-ui-tooltreetograph -->

`function` · `packages/jcode-ui/src/canvas/toolTreeToGraph.ts`

```ts
export function toolTreeToGraph(
  tools: ToolCall[],
  options?: ToolTreeToGraphOptions,
): ToolGraph { … }
```

### `truncate`

<!-- jcode-ui-truncate -->

`function` · `packages/jcode-ui/src/toolRenderers/terminal.tsx`

Count code points (not UTF-16 units) so CJK truncation is fair.

```ts
export function truncate(text: string, max: number): string { … }
```

### `useStreamingMarkdown`

<!-- jcode-ui-usestreamingmarkdown -->

`function` · `packages/jcode-ui/src/lib/useStreamingMarkdown.ts`

```ts
export function useStreamingMarkdown(md: string): string { … }
```

### `useToolRegistry`

<!-- jcode-ui-usetoolregistry -->

`function` · `packages/jcode-ui/src/components/ToolRegistryContext.tsx`

```ts
export function useToolRegistry(): ToolRendererRegistry { … }
```

### `ApiBaseProviderProps`

<!-- jcode-ui-apibaseproviderprops -->

`interface` · `packages/jcode-ui/src/lib/apiBaseContext.tsx`

```ts
export interface ApiBaseProviderProps {
  /** API base URL with no trailing slash. */
  apiBase: string
  children: React.ReactNode
}
```

### `ApprovalBannerProps`

<!-- jcode-ui-approvalbannerprops -->

`interface` · `packages/jcode-ui/src/components/ApprovalBanner.tsx`

```ts
export interface ApprovalBannerProps {
  approval: Approval
}
```

### `ArtifactProps`

<!-- jcode-ui-artifactprops -->

`interface` · `packages/jcode-ui/src/components/Artifact.tsx`

```ts
export interface ArtifactProps {
  /** Primary label in the header. */
  title: string
  /** Secondary label (path, size, language…). Rendered muted + mono. */
  subtitle?: string
  /** Leading icon node (e.g. a heroicon or extension dot). */
  icon?: ReactNode
  /** Right-aligned actions (copy, download, open…). */
  actions?: ReactNode
  /** When provided, a close button appears at the far right. */
  onClose?: () => void
  /** Max height of the scrollable content region. Default '24rem'. */
  maxHeight?: string | number
  /** Extra classes on the root. */
  className?: string
  children: ReactNode
}
```

### `AskUserCardProps`

<!-- jcode-ui-askusercardprops -->

`interface` · `packages/jcode-ui/src/components/AskUserCard.tsx`

```ts
export interface AskUserCardProps {
  tool: ToolCall
}
```

### `AudioPlayerProps`

<!-- jcode-ui-audioplayerprops -->

`interface` · `packages/jcode-ui/src/voice/AudioPlayer.tsx`

```ts
export interface AudioPlayerProps {
  /** Audio source URL (or object URL / data URI). */
  src: string
  /** Fires on every playback tick and seek, in milliseconds. */
  onTimeUpdate?: (ms: number) => void
  /** Autoplay once metadata is ready (subject to browser policy). */
  autoPlay?: boolean
  className?: string
}
```

### `BranchPickerProps`

<!-- jcode-ui-branchpickerprops -->

`interface` · `packages/jcode-ui/src/components/BranchPicker.tsx`

```ts
export interface BranchPickerProps {
  message: MessageData
}
```

### `CanvasControlsProps`

<!-- jcode-ui-canvascontrolsprops -->

`interface` · `packages/jcode-ui/src/canvas/CanvasControls.tsx`

```ts
export interface CanvasControlsProps {
  /** Corner to dock the controls (default 'bottom-left'). */
  position?: PanelPosition
  className?: string
}
```

### `CanvasPanelProps`

<!-- jcode-ui-canvaspanelprops -->

`interface` · `packages/jcode-ui/src/canvas/CanvasPanel.tsx`

```ts
export interface CanvasPanelProps {
  /** Corner to dock the panel (default 'top-right'). */
  position?: PanelPosition
  className?: string
  children?: ReactNode
}
```

### `ChatInputProps`

<!-- jcode-ui-chatinputprops -->

`interface` · `packages/jcode-ui/src/components/ChatInput.tsx`

```ts
export interface ChatInputProps {
  /** Slash commands (host-fetched). */
  slashCommands?: SlashCommand[]
  /** Allow image attachments (gated by model vision support). Legacy path. */
  allowImages?: boolean
  /** `accept` for the file picker. Default `image/*`. */
  acceptImages?: string
  /**
   * Pluggable attachment pipeline. Supersedes `allowImages` when provided —
   * files route through the adapter with an upload progress state machine.
   */
  attachmentAdapter?: AttachmentAdapter
  /** Fired on send with the completed attachments (adapter path). */
  onSendAttachments?: (attachments: PendingAttachment[]) => void
  /** Content rendered after the add-attachment control (e.g. a ModelSelector). */
  leadingControls?: ReactNode
  /** Content rendered just before the send button (e.g. a mode picker). */
  trailingControls?: ReactNode
  /** Content rendered below the composer row. */
  footer?: ReactNode
  /** Enable the dictation mic button (rendered only when the browser supports it). */
  enableDictation?: boolean
  /** BCP-47 language tag for dictation. */
  dictationLang?: string
  /** Placeholder text. */
  placeholder?: string
  /** Show the context bar suffix. Default true. */
  showContextBar?: boolean
  /** Callback after a message is sent/queued (host snaps timeline to bottom). */
  onSent?: () => void
}
```

### `CodeBlockHookArgs`

<!-- jcode-ui-codeblockhookargs -->

`interface` · `packages/jcode-ui/src/lib/markdown.ts`

Markdown rendering — marked + highlight.js + DOMPurify.

Ported from the Vue composable and extended with:
  - a code-block "chrome" renderer (filename bar + copy button), and
  - a zero-cost plugin hook table (mermaid / katex register into it; the core
    never imports the plugin files, so not importing them costs nothing).

Framework-agnostic; returns an HTML string the consumer injects via
dangerouslySetInnerHTML. Sanitization runs only when a DOM is present
(browser); in SSR/Node it is a no-op so the pipeline stays testable — the
browser, which is the only place the HTML is ever injected, always sanitizes.
/

import { Marked } from 'marked'
import type { TokenizerAndRendererExtension, Tokens } from 'marked'
import hljs from 'highlight.js'
import DOMPurify from 'dompurify'

// ─── Plugin hook table ───────────────────────────────────────────────────
// Optional plugins register renderers here. `markdown.ts` never imports the
// plugin files (mermaid.ts / katex.ts), only the reverse — so a consumer that
// never calls registerMermaid()/registerKatex() ships zero plugin code.

/** Arguments passed to a fenced-code-block hook.

```ts
export interface CodeBlockHookArgs {
  /** Raw (un-highlighted) code text. */
  code: string
  /** First token of the info string, e.g. `ts` for ```` ```ts title=a.ts ````. */
  lang: string
  /** Parsed filename (from `title=` or `lang:file` conventions), or ''. */
  filename: string
}
```

### `CompactToolRowProps`

<!-- jcode-ui-compacttoolrowprops -->

`interface` · `packages/jcode-ui/src/components/CompactToolRow.tsx`

```ts
export interface CompactToolRowProps {
  tool: ToolCall
}
```

### `CompletionResult`

<!-- jcode-ui-completionresult -->

`interface` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

```ts
export interface CompletionResult {
  text: string
  /** True when an open code fence was auto-closed (last block is streaming). */
  fenceStreaming: boolean
}
```

### `ContextBarProps`

<!-- jcode-ui-contextbarprops -->

`interface` · `packages/jcode-ui/src/components/ContextBar.tsx`

```ts
export interface ContextBarProps {
  /** Optional host-provided context breakdown for the popover. */
  breakdown?: TaskContextBreakdown | null
  /** Diameter of the ring in px. Default 20. */
  size?: number
  /** Threshold (0-1) above which the ring turns red. Default 0.9. */
  dangerThreshold?: number
  /** Show the popover on hover. Default true. */
  showPopover?: boolean
}
```

### `ExploringGroupCardProps`

<!-- jcode-ui-exploringgroupcardprops -->

`interface` · `packages/jcode-ui/src/components/ExploringGroupCard.tsx`

```ts
export interface ExploringGroupCardProps {
  group: ExploringGroup
  className?: string
}
```

### `ExportButtonProps`

<!-- jcode-ui-exportbuttonprops -->

`interface` · `packages/jcode-ui/src/components/ExportButton.tsx`

```ts
export interface ExportButtonProps {
  /** Download filename. Default `conversation.md`. */
  filename?: string
  /** Document title inside the markdown. */
  title?: string
  className?: string
}
```

### `KatexApi`

<!-- jcode-ui-katexapi -->

`interface` · `packages/jcode-ui/src/plugins/external-modules.d.ts`

```ts
export interface KatexApi {
    renderToString(tex: string, options?: KatexOptions): string
  }
```

### `KatexOptions`

<!-- jcode-ui-katexoptions -->

`interface` · `packages/jcode-ui/src/plugins/external-modules.d.ts`

```ts
export interface KatexOptions {
    displayMode?: boolean
    throwOnError?: boolean
    output?: 'html' | 'mathml' | 'htmlAndMathml'
    [key: string]: unknown
  }
```

### `KatexPluginOptions`

<!-- jcode-ui-katexpluginoptions -->

`interface` · `packages/jcode-ui/src/plugins/katex.ts`

```ts
export interface KatexPluginOptions {
  /** Throw instead of rendering an error node. Default: false. */
  throwOnError?: boolean
  /** Any other KaTeX option is passed through. */
  [key: string]: unknown
}
```

### `MermaidApi`

<!-- jcode-ui-mermaidapi -->

`interface` · `packages/jcode-ui/src/plugins/external-modules.d.ts`

```ts
export interface MermaidApi {
    initialize(config: Record<string, unknown>): void
    render(id: string, text: string, container?: Element): Promise<MermaidRenderResult>
    parse?(text: string): Promise<boolean> | boolean
  }
```

### `MermaidPluginOptions`

<!-- jcode-ui-mermaidpluginoptions -->

`interface` · `packages/jcode-ui/src/plugins/mermaid.ts`

```ts
export interface MermaidPluginOptions {
  /** Mermaid theme, e.g. 'default' | 'neutral' | 'dark' | 'forest'. */
  theme?: string
  /** Any other mermaid.initialize() option is passed through. */
  [key: string]: unknown
}
```

### `MermaidRenderResult`

<!-- jcode-ui-mermaidrenderresult -->

`interface` · `packages/jcode-ui/src/plugins/external-modules.d.ts`

```ts
export interface MermaidRenderResult {
    svg: string
    bindFunctions?: (element: Element) => void
  }
```

### `MessageProps`

<!-- jcode-ui-messageprops -->

`interface` · `packages/jcode-ui/src/components/Message.tsx`

```ts
export interface MessageProps {
  message: MessageData
  /** Allow editing (typically user messages when idle). */
  canEdit?: boolean
  /** Optional chrome overrides (avatar / header / footer tail). */
  slots?: MessageSlots
}
```

### `MessageSlots`

<!-- jcode-ui-messageslots -->

`interface` · `packages/jcode-ui/src/components/Message.tsx`

Message — flat chat message (matches web/src/components/ChatMessage.vue).

Layout (NOT chat-bubble cards):
  [avatar] Role label
           markdown content (jcode-gutter, no bg / no border)
           duration · copy / edit (hover)

User and assistant share the same left-aligned structure; only the avatar
fill and label color differ. System messages keep the same skeleton with
a level-tinted avatar.
/

import { memo, useCallback, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import {
  ArrowPathIcon,
  CheckIcon,
  HandThumbDownIcon,
  HandThumbUpIcon,
  PencilSquareIcon,
  Square2StackIcon,
} from '@heroicons/react/24/outline'
import type { Message as MessageData } from 'jcode-ui-core'
import { useRuntimeActions } from 'jcode-ui-core/runtime'
import { bindCodeBlockCopy } from '../lib/markdown.js'
import { useStreamingMarkdown } from '../lib/useStreamingMarkdown.js'
import { AttachmentList } from './Attachment.js'
import { BranchPicker } from './BranchPicker.js'
import { Reasoning } from './Reasoning.js'
import { Sources } from './Sources.js'

/** Render-prop overrides for the message chrome. Each replaces a piece of the
 default layout; omitting one keeps the built-in rendering unchanged.

```ts
export interface MessageSlots {
  /** Replaces the avatar circle (inside the default header row). */
  avatar?: (message: MessageData) => ReactNode
  /** Replaces the entire role-header row (avatar + label). */
  header?: (message: MessageData) => ReactNode
  /** Appended to the tail of the action footer. */
  footerExtra?: (message: MessageData) => ReactNode
}
```

### `ModelSelectorOption`

<!-- jcode-ui-modelselectoroption -->

`interface` · `packages/jcode-ui/src/components/ModelSelector.tsx`

```ts
export interface ModelSelectorOption {
  id: string
  label: string
  /** Provider name — used for grouping and search. */
  provider?: string
  /** Optional one-line description shown under the label. */
  description?: string
}
```

### `ModelSelectorProps`

<!-- jcode-ui-modelselectorprops -->

`interface` · `packages/jcode-ui/src/components/ModelSelector.tsx`

```ts
export interface ModelSelectorProps {
  models: ModelSelectorOption[]
  /** Selected model id. */
  value?: string
  onChange: (id: string) => void
  disabled?: boolean
  /** Trigger label when nothing is selected. Default "Select model". */
  placeholder?: string
  className?: string
}
```

### `ReasoningProps`

<!-- jcode-ui-reasoningprops -->

`interface` · `packages/jcode-ui/src/components/Reasoning.tsx`

```ts
export interface ReasoningProps {
  /** The reasoning text (markdown). */
  reasoning: string
  /** Default expanded. Default false. */
  defaultExpanded?: boolean
  /** Show a "Thought for Ns" label using the message duration. */
  durationMs?: number
}
```

### `RuntimeTaskListProps`

<!-- jcode-ui-runtimetasklistprops -->

`interface` · `packages/jcode-ui/src/components/TaskList.tsx`

```ts
export interface RuntimeTaskListProps {
  title?: string
  compact?: boolean
  hideProgress?: boolean
  className?: string
}
```

### `SourcesProps`

<!-- jcode-ui-sourcesprops -->

`interface` · `packages/jcode-ui/src/components/Sources.tsx`

```ts
export interface SourcesProps {
  sources: MessageSource[]
}
```

### `SpeechInputProps`

<!-- jcode-ui-speechinputprops -->

`interface` · `packages/jcode-ui/src/voice/SpeechInput.tsx`

```ts
export interface SpeechInputProps {
  /** BCP-47 language tag for recognition, e.g. `en-US`, `zh-CN`. Default `en-US`. */
  lang?: string
  /** Called with recognized text. `final` distinguishes stable vs interim results. */
  onTranscript: (text: string, meta: { final: boolean }) => void
  /** Fallback sink: when Speech Recognition is unavailable, the recorded clip is handed here. */
  onAudio?: (blob: Blob) => void
  /** Disable the control. */
  disabled?: boolean
  /** Optional label rendered next to the button. */
  label?: string
  className?: string
}
```

### `StackFrame`

<!-- jcode-ui-stackframe -->

`interface` · `packages/jcode-ui/src/toolRenderers/stackTrace.tsx`

```ts
export interface StackFrame {
  func: string
  file?: string
  line?: number
  column?: number
  raw: string
  /** node_modules or language-runtime frame — collapsed by default. */
  isRuntime: boolean
}
```

### `StackTrace`

<!-- jcode-ui-stacktrace -->

`interface` · `packages/jcode-ui/src/toolRenderers/stackTrace.tsx`

```ts
export interface StackTrace {
  kind: 'go' | 'js'
  message: string
  frames: StackFrame[]
}
```

### `SuggestionItem`

<!-- jcode-ui-suggestionitem -->

`interface` · `packages/jcode-ui/src/components/Suggestions.tsx`

```ts
export interface SuggestionItem {
  /** Stable key; defaults to the label. */
  id?: string
  /** Pill text. */
  label: string
  /** Message to send; defaults to the label. */
  prompt?: string
}
```

### `SuggestionsProps`

<!-- jcode-ui-suggestionsprops -->

`interface` · `packages/jcode-ui/src/components/Suggestions.tsx`

```ts
export interface SuggestionsProps {
  items: SuggestionItem[]
  /** Intercept picks (e.g. to prefill the composer instead of sending). */
  onPick?: (item: SuggestionItem) => void
  /** Compact single-line variant with horizontal scroll. */
  scroll?: boolean
  /** Disable all pills (e.g. while running). */
  disabled?: boolean
}
```

### `TestCase`

<!-- jcode-ui-testcase -->

`interface` · `packages/jcode-ui/src/toolRenderers/testResults.tsx`

```ts
export interface TestCase {
  name: string
  status: 'pass' | 'fail' | 'skip'
  durationMs?: number
  detail?: string
}
```

### `TestSummary`

<!-- jcode-ui-testsummary -->

`interface` · `packages/jcode-ui/src/toolRenderers/testResults.tsx`

```ts
export interface TestSummary {
  framework: 'go' | 'vitest' | 'jest' | 'unknown'
  passed: number
  failed: number
  skipped: number
  cases: TestCase[]
}
```

### `ThreadProps`

<!-- jcode-ui-threadprops -->

`interface` · `packages/jcode-ui/src/components/Thread.tsx`

```ts
export interface ThreadProps {
  /** Disable virtualization (short/replay timelines). Default true. */
  virtualize?: boolean
  /** Empty-state node (typically `<ThreadWelcome>`). */
  emptyState?: ReactNode
  /** Follow-up content under the last turn when idle (typically
   *  `<Suggestions scroll>`), aligned to the chat column. */
  suggestions?: ReactNode
  /** Override the pending ("Thinking…") indicator. */
  renderPending?: () => ReactNode
  /** className passthrough for the scroll container. */
  className?: string
  /** Extra bottom padding (px) to clear a sticky composer. */
  overscanBottom?: number
}
```

### `ThreadWelcomeProps`

<!-- jcode-ui-threadwelcomeprops -->

`interface` · `packages/jcode-ui/src/components/ThreadWelcome.tsx`

```ts
export interface ThreadWelcomeProps {
  /** Brand mark / logo slot. Default: a neutral chat glyph. */
  logo?: ReactNode
  /** Headline. */
  title?: string
  /** Supporting line under the headline. */
  subtitle?: string
  /** Extra content below (typically `<Suggestions>`). */
  children?: ReactNode
}
```

### `ToolCallCardProps`

<!-- jcode-ui-toolcallcardprops -->

`interface` · `packages/jcode-ui/src/components/ToolCallCard.tsx`

```ts
export interface ToolCallCardProps {
  tool: ToolCall
  /** Override the registry (defaults to the context-provided one). */
  registry?: ToolRendererRegistry
  /** Extra classes (e.g. pl-9 to indent under the message content column). */
  className?: string
  /** Nesting depth for subagent children. */
  depth?: number
  /** Optional header/footer overrides. Omit for the default card (unchanged). */
  slots?: ToolCallCardSlots
}
```

### `ToolCallCardSlots`

<!-- jcode-ui-toolcallcardslots -->

`interface` · `packages/jcode-ui/src/components/ToolCallCard.tsx`

```ts
export interface ToolCallCardSlots {
  /**
   * Replace the content of the title-row button (expand/collapse interaction
   * is preserved — clicking still toggles the card).
   */
  header?: (tool: ToolCall) => ReactNode
  /** Extra content appended below the card body. */
  footer?: (tool: ToolCall) => ReactNode
}
```

### `ToolGraph`

<!-- jcode-ui-toolgraph -->

`interface` · `packages/jcode-ui/src/canvas/toolTreeToGraph.ts`

```ts
export interface ToolGraph {
  nodes: JcodeStepNode[]
  edges: Edge[]
}
```

### `ToolRegistryProviderProps`

<!-- jcode-ui-toolregistryproviderprops -->

`interface` · `packages/jcode-ui/src/components/ToolRegistryContext.tsx`

```ts
export interface ToolRegistryProviderProps {
  /** Defaults to createDefaultToolRegistry() if omitted. */
  registry?: ToolRendererRegistry
  children: ReactNode
}
```

### `ToolTreeToGraphOptions`

<!-- jcode-ui-tooltreetographoptions -->

`interface` · `packages/jcode-ui/src/canvas/toolTreeToGraph.ts`

```ts
export interface ToolTreeToGraphOptions {
  /** Fixed node width used for horizontal spacing (default 220, matches CSS). */
  nodeWidth?: number
  /** Vertical distance between depth levels (default 120). */
  levelGap?: number
  /** Horizontal gap between adjacent nodes (default 40). */
  siblingGap?: number
}
```

### `TranscriptionProps`

<!-- jcode-ui-transcriptionprops -->

`interface` · `packages/jcode-ui/src/voice/Transcription.tsx`

```ts
export interface TranscriptionProps {
  segments: TranscriptSegment[]
  /** Current playback position (ms) — drives the active highlight. */
  currentTimeMs?: number
  /** Seek handler; a segment is clickable when both this and `startMs` exist. */
  onSeek?: (ms: number) => void
  className?: string
}
```

### `TranscriptSegment`

<!-- jcode-ui-transcriptsegment -->

`interface` · `packages/jcode-ui/src/voice/Transcription.tsx`

```ts
export interface TranscriptSegment {
  id: string
  text: string
  /** Segment start offset in milliseconds. */
  startMs?: number
  /** Segment end offset in milliseconds. */
  endMs?: number
  /** Optional speaker label / diarization tag. */
  speaker?: string
}
```

### `VoiceVisualizerProps`

<!-- jcode-ui-voicevisualizerprops -->

`interface` · `packages/jcode-ui/src/voice/VoiceVisualizer.tsx`

```ts
export interface VoiceVisualizerProps {
  /** Live input to analyze (e.g. from `getUserMedia`). Omit for the idle state. */
  stream?: MediaStream | null
  /** When false, renders the idle breathing state even if a stream is present. Default true. */
  active?: boolean
  /** Number of bars to render. Default 32. */
  bars?: number
  className?: string
}
```

### `WorkflowCanvasProps`

<!-- jcode-ui-workflowcanvasprops -->

`interface` · `packages/jcode-ui/src/canvas/WorkflowCanvas.tsx`

```ts
export interface WorkflowCanvasProps extends ReactFlowProps {
  /** When false, disables node dragging / connecting / selection and pan-on-drag. */
  interactive?: boolean
  /** Render the dotted background layer (default true). */
  showBackground?: boolean
  children?: ReactNode
}
```

### `CodeBlockHook`

<!-- jcode-ui-codeblockhook -->

`type` · `packages/jcode-ui/src/lib/markdown.ts`

Raw (un-highlighted) code text. */
  code: string
  /** First token of the info string, e.g. `ts` for ```` ```ts title=a.ts ````. */
  lang: string
  /** Parsed filename (from `title=` or `lang:file` conventions), or ''. */
  filename: string
}

/** Return HTML to fully replace the code block, or `null` to fall through.

```ts
export type CodeBlockHook = (args: CodeBlockHookArgs) => string | null;
```

### `JcodeStepData`

<!-- jcode-ui-jcodestepdata -->

`type` · `packages/jcode-ui/src/canvas/WorkflowNode.tsx`

Payload for a `jcodeStep` node. Declared as a type literal (not an
interface) so it satisfies React Flow's `Record<string, unknown>` node-data
constraint via an implicit index signature.

```ts
export type JcodeStepData = {
  title: string
  subtitle?: string
  /** Icon slot — a string (emoji / glyph) or any React node. */
  icon?: ReactNode
  status?: JcodeStepStatus
};
```

### `JcodeStepNode`

<!-- jcode-ui-jcodestepnode -->

`type` · `packages/jcode-ui/src/canvas/WorkflowNode.tsx`

Icon slot — a string (emoji / glyph) or any React node. */
  icon?: ReactNode
  status?: JcodeStepStatus
}

/** Concrete node type for this renderer.

```ts
export type JcodeStepNode = Node<JcodeStepData, 'jcodeStep'>;
```

### `JcodeStepStatus`

<!-- jcode-ui-jcodestepstatus -->

`type` · `packages/jcode-ui/src/canvas/WorkflowNode.tsx`

WorkflowNode — the custom `jcodeStep` React Flow node.

A token-driven card (surface / radius-lg / shadow-sm) with an icon slot,
title, subtitle and a status affordance. Status drives the frame:
  - running → primary border, breathing pulse
  - error   → destructive border + tint
  - done / pending → neutral

Props are typed as the base `NodeProps` (not `NodeProps<JcodeStepNode>`) so
the component stays assignable to React Flow's `NodeTypes` without a cast;
`data` is narrowed internally. Styling lives in `./canvas.css`.
/

import { memo } from 'react'
import type { ReactNode } from 'react'
import { Handle, Position } from '@xyflow/react'
import type { Node, NodeProps, NodeTypes } from '@xyflow/react'

/** Lifecycle of a step; a superset of the runtime `ToolStatus`.

```ts
export type JcodeStepStatus = 'pending' | 'running' | 'done' | 'error';
```

### `MathRenderer`

<!-- jcode-ui-mathrenderer -->

`type` · `packages/jcode-ui/src/lib/markdown.ts`

Render TeX to an HTML string. `displayMode` = block (`$$…$$`) vs inline (`$…$`).

```ts
export type MathRenderer = (tex: string, displayMode: boolean) => string;
```

### `SpeechInputStatus`

<!-- jcode-ui-speechinputstatus -->

`type` · `packages/jcode-ui/src/voice/SpeechInput.tsx`

```ts
export type SpeechInputStatus = 'idle' | 'listening' | 'recording' | 'error';
```

## `jcode-ui-core`

| Symbol | Kind | Source |
|--------|------|--------|
| [`ToolRendererRegistry`](#jcode-ui-core-toolrendererregistry) | class | `packages/jcode-ui-core/src/adapters/index.ts` |
| [`ApprovalBlock`](#jcode-ui-core-approvalblock) | function | `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx` |
| [`createAGUIRuntime`](#jcode-ui-core-createaguiruntime) | function | `packages/jcode-ui-core/src/runtime/aguiRuntime.ts` |
| [`createExternalStoreRuntime`](#jcode-ui-core-createexternalstoreruntime) | function | `packages/jcode-ui-core/src/runtime/externalStore.ts` |
| [`createFetchTransport`](#jcode-ui-core-createfetchtransport) | function | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`createInlineImageAdapter`](#jcode-ui-core-createinlineimageadapter) | function | `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts` |
| [`createMockRuntime`](#jcode-ui-core-createmockruntime) | function | `packages/jcode-ui-core/src/runtime/mockRuntime.ts` |
| [`createMockThreadStore`](#jcode-ui-core-createmockthreadstore) | function | `packages/jcode-ui-core/src/threads/store.ts` |
| [`createToolRendererRegistry`](#jcode-ui-core-createtoolrendererregistry) | function | `packages/jcode-ui-core/src/adapters/index.ts` |
| [`exportThreadMarkdown`](#jcode-ui-core-exportthreadmarkdown) | function | `packages/jcode-ui-core/src/export/markdown.ts` |
| [`groupExploringTimeline`](#jcode-ui-core-groupexploringtimeline) | function | `packages/jcode-ui-core/src/timeline/groupExploring.ts` |
| [`isApprovalItem`](#jcode-ui-core-isapprovalitem) | function | `packages/jcode-ui-core/src/types/index.ts` |
| [`isCollapsibleTool`](#jcode-ui-core-iscollapsibletool) | function | `packages/jcode-ui-core/src/timeline/groupExploring.ts` |
| [`isExploringItem`](#jcode-ui-core-isexploringitem) | function | `packages/jcode-ui-core/src/types/index.ts` |
| [`isMessageItem`](#jcode-ui-core-ismessageitem) | function | `packages/jcode-ui-core/src/types/index.ts` |
| [`isToolItem`](#jcode-ui-core-istoolitem) | function | `packages/jcode-ui-core/src/types/index.ts` |
| [`MessageView`](#jcode-ui-core-messageview) | function | `packages/jcode-ui-core/src/primitives/MessageView.tsx` |
| [`nextAttachmentId`](#jcode-ui-core-nextattachmentid) | function | `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts` |
| [`normalizeState`](#jcode-ui-core-normalizestate) | function | `packages/jcode-ui-core/src/runtime/index.ts` |
| [`parseResolvedAnswers`](#jcode-ui-core-parseresolvedanswers) | function | `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx` |
| [`RuntimeProvider`](#jcode-ui-core-runtimeprovider) | function | `packages/jcode-ui-core/src/runtime/context.tsx` |
| [`summarizeExploringSteps`](#jcode-ui-core-summarizeexploringsteps) | function | `packages/jcode-ui-core/src/timeline/groupExploring.ts` |
| [`Thread`](#jcode-ui-core-thread) | function | `packages/jcode-ui-core/src/primitives/Thread.tsx` |
| [`ThreadStoreProvider`](#jcode-ui-core-threadstoreprovider) | function | `packages/jcode-ui-core/src/threads/context.tsx` |
| [`ToolCallProvider`](#jcode-ui-core-toolcallprovider) | function | `packages/jcode-ui-core/src/primitives/ToolCallView.tsx` |
| [`ToolCallView`](#jcode-ui-core-toolcallview) | function | `packages/jcode-ui-core/src/primitives/ToolCallView.tsx` |
| [`useAutoScroll`](#jcode-ui-core-useautoscroll) | function | `packages/jcode-ui-core/src/hooks/index.ts` |
| [`useFocusOnIdle`](#jcode-ui-core-usefocusonidle) | function | `packages/jcode-ui-core/src/hooks/index.ts` |
| [`useIsAtBottom`](#jcode-ui-core-useisatbottom) | function | `packages/jcode-ui-core/src/hooks/index.ts` |
| [`useQueuedMessages`](#jcode-ui-core-usequeuedmessages) | function | `packages/jcode-ui-core/src/hooks/index.ts` |
| [`useRuntimeActions`](#jcode-ui-core-useruntimeactions) | function | `packages/jcode-ui-core/src/runtime/context.tsx` |
| [`useRuntimeSelector`](#jcode-ui-core-useruntimeselector) | function | `packages/jcode-ui-core/src/runtime/context.tsx` |
| [`useRuntimeState`](#jcode-ui-core-useruntimestate) | function | `packages/jcode-ui-core/src/runtime/context.tsx` |
| [`useStreamFollow`](#jcode-ui-core-usestreamfollow) | function | `packages/jcode-ui-core/src/hooks/index.ts` |
| [`useThreadListState`](#jcode-ui-core-usethreadliststate) | function | `packages/jcode-ui-core/src/threads/context.tsx` |
| [`useThreadStoreActions`](#jcode-ui-core-usethreadstoreactions) | function | `packages/jcode-ui-core/src/threads/context.tsx` |
| [`useToolCallContext`](#jcode-ui-core-usetoolcallcontext) | function | `packages/jcode-ui-core/src/primitives/ToolCallView.tsx` |
| [`AGUIMessage`](#jcode-ui-core-aguimessage) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`AGUIPatchOp`](#jcode-ui-core-aguipatchop) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`AGUIRunInput`](#jcode-ui-core-aguiruninput) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`AGUIRuntime`](#jcode-ui-core-aguiruntime) | interface | `packages/jcode-ui-core/src/runtime/aguiRuntime.ts` |
| [`AGUIToolCall`](#jcode-ui-core-aguitoolcall) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`Approval`](#jcode-ui-core-approval) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ApprovalBlockProps`](#jcode-ui-core-approvalblockprops) | interface | `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx` |
| [`ApprovalBlockRenderSlots`](#jcode-ui-core-approvalblockrenderslots) | interface | `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx` |
| [`ApprovalDecisionActions`](#jcode-ui-core-approvaldecisionactions) | interface | `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx` |
| [`ApprovalOption`](#jcode-ui-core-approvaloption) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`AskUserAnswer`](#jcode-ui-core-askuseranswer) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`AskUserControls`](#jcode-ui-core-askusercontrols) | interface | `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx` |
| [`AskUserOption`](#jcode-ui-core-askuseroption) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`AskUserQuestion`](#jcode-ui-core-askuserquestion) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`AttachmentAdapter`](#jcode-ui-core-attachmentadapter) | interface | `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts` |
| [`ChatImage`](#jcode-ui-core-chatimage) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ChatRuntime`](#jcode-ui-core-chatruntime) | interface | `packages/jcode-ui-core/src/runtime/index.ts` |
| [`ComposerHandle`](#jcode-ui-core-composerhandle) | interface | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`ComposerProps`](#jcode-ui-core-composerprops) | interface | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`ComposerRenderSlots`](#jcode-ui-core-composerrenderslots) | interface | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`DictationState`](#jcode-ui-core-dictationstate) | interface | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`ExploringGroup`](#jcode-ui-core-exploringgroup) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ExportMarkdownOptions`](#jcode-ui-core-exportmarkdownoptions) | interface | `packages/jcode-ui-core/src/export/markdown.ts` |
| [`Goal`](#jcode-ui-core-goal) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`InlineImageAdapterOptions`](#jcode-ui-core-inlineimageadapteroptions) | interface | `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts` |
| [`Message`](#jcode-ui-core-message) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`MessageActions`](#jcode-ui-core-messageactions) | interface | `packages/jcode-ui-core/src/primitives/MessageView.tsx` |
| [`MessageSource`](#jcode-ui-core-messagesource) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`MessageVersion`](#jcode-ui-core-messageversion) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`MessageViewProps`](#jcode-ui-core-messageviewprops) | interface | `packages/jcode-ui-core/src/primitives/MessageView.tsx` |
| [`MockThreadStore`](#jcode-ui-core-mockthreadstore) | interface | `packages/jcode-ui-core/src/threads/store.ts` |
| [`PendingAttachment`](#jcode-ui-core-pendingattachment) | interface | `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts` |
| [`PendingAttachmentItem`](#jcode-ui-core-pendingattachmentitem) | interface | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`QueuedMessage`](#jcode-ui-core-queuedmessage) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`RuntimeActions`](#jcode-ui-core-runtimeactions) | interface | `packages/jcode-ui-core/src/runtime/index.ts` |
| [`RuntimeState`](#jcode-ui-core-runtimestate) | interface | `packages/jcode-ui-core/src/runtime/index.ts` |
| [`SlashMenuState`](#jcode-ui-core-slashmenustate) | interface | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`TaskContextBreakdown`](#jcode-ui-core-taskcontextbreakdown) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ThreadListState`](#jcode-ui-core-threadliststate) | interface | `packages/jcode-ui-core/src/threads/store.ts` |
| [`ThreadProps`](#jcode-ui-core-threadprops) | interface | `packages/jcode-ui-core/src/primitives/Thread.tsx` |
| [`ThreadRenderSlots`](#jcode-ui-core-threadrenderslots) | interface | `packages/jcode-ui-core/src/primitives/Thread.tsx` |
| [`ThreadStore`](#jcode-ui-core-threadstore) | interface | `packages/jcode-ui-core/src/threads/store.ts` |
| [`ThreadStoreActions`](#jcode-ui-core-threadstoreactions) | interface | `packages/jcode-ui-core/src/threads/store.ts` |
| [`ThreadSummary`](#jcode-ui-core-threadsummary) | interface | `packages/jcode-ui-core/src/threads/store.ts` |
| [`TodoItem`](#jcode-ui-core-todoitem) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`TokenSnapshot`](#jcode-ui-core-tokensnapshot) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolCall`](#jcode-ui-core-toolcall) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolCallContextValue`](#jcode-ui-core-toolcallcontextvalue) | interface | `packages/jcode-ui-core/src/primitives/ToolCallView.tsx` |
| [`ToolCallViewProps`](#jcode-ui-core-toolcallviewprops) | interface | `packages/jcode-ui-core/src/primitives/ToolCallView.tsx` |
| [`ToolDisplayInfo`](#jcode-ui-core-tooldisplayinfo) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolMeta`](#jcode-ui-core-toolmeta) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolPresentation`](#jcode-ui-core-toolpresentation) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolRendererProps`](#jcode-ui-core-toolrendererprops) | interface | `packages/jcode-ui-core/src/adapters/index.ts` |
| [`ToolStreams`](#jcode-ui-core-toolstreams) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`AGUIEvent`](#jcode-ui-core-aguievent) | type | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`AGUIRole`](#jcode-ui-core-aguirole) | type | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`AGUITransport`](#jcode-ui-core-aguitransport) | type | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`ApprovalOptionKind`](#jcode-ui-core-approvaloptionkind) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`ConnectionState`](#jcode-ui-core-connectionstate) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`GoalStatus`](#jcode-ui-core-goalstatus) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`MockThreadStoreSeed`](#jcode-ui-core-mockthreadstoreseed) | type | `packages/jcode-ui-core/src/threads/store.ts` |
| [`PartialRuntimeState`](#jcode-ui-core-partialruntimestate) | type | `packages/jcode-ui-core/src/runtime/index.ts` |
| [`PendingStatus`](#jcode-ui-core-pendingstatus) | type | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`Role`](#jcode-ui-core-role) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`SystemLevel`](#jcode-ui-core-systemlevel) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`ThreadItem`](#jcode-ui-core-threaditem) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`ThreadItemKind`](#jcode-ui-core-threaditemkind) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolRenderer`](#jcode-ui-core-toolrenderer) | type | `packages/jcode-ui-core/src/adapters/index.ts` |

### `ToolRendererRegistry`

<!-- jcode-ui-core-toolrendererregistry -->

`class` · `packages/jcode-ui-core/src/adapters/index.ts`

Name-keyed registry of tool renderers, with a fallback. Lookups are
case-sensitive and exact (no globbing) — keep tool names stable.

Register a single renderer, or a whole map at once. The registry is mutable
so consumers can register at app bootstrap and add more later.

```ts
export class ToolRendererRegistry {
  private renderers = new Map<string, ToolRenderer>()
  private fallback: ToolRenderer | null = null

  /** Register a renderer for one or more tool names (later writes win). */
  register(name: string, renderer: ToolRenderer): this
  register(names: string[], renderer: ToolRenderer): this
  register(nameOrNames: string | string[], renderer: ToolRenderer): this {
    const names = Array.isArray(nameOrNames) ? nameOrNames : [nameOrNames]
    for (const n of names) this.renderers.set(n, renderer)
    return this
  }

  /** Register a batch of { name → renderer } entries. */
  registerAll(entries: Record<string, ToolRenderer>): this {
    for (const [name, renderer] of Object.entries(entries)) {
      this.renderers.set(name, renderer)
    }
    return this
  }

  /** Set the renderer used when no name-specific match exists. */
  setFallback(renderer: ToolRenderer): this {
    this.fallback = renderer
    return this
  }

  /** Look up a renderer by tool name, falling back if absent. Returns null
   *  only when nothing is registered AND no fallback is set. */
  get(name: string): ToolRenderer | null {
    return this.renderers.get(name) ?? this.fallback
  }

  /** True if a name-specific renderer is registered. */
  has(name: string): boolean {
    return this.renderers.has(name)
  }
}
```

### `ApprovalBlock`

<!-- jcode-ui-core-approvalblock -->

`function` · `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx`

```ts
export function ApprovalBlock({ approval, className, renderPending, renderResolved }: ApprovalBlockProps): ReactNode { … }
```

### `createAGUIRuntime`

<!-- jcode-ui-core-createaguiruntime -->

`function` · `packages/jcode-ui-core/src/runtime/aguiRuntime.ts`

```ts
export function createAGUIRuntime(options: AGUIRuntimeOptions): AGUIRuntime { … }
```

### `createExternalStoreRuntime`

<!-- jcode-ui-core-createexternalstoreruntime -->

`function` · `packages/jcode-ui-core/src/runtime/externalStore.ts`

ExternalStoreRuntime — wrap any Redux-shaped external store as a ChatRuntime.

The host store holds the *full* app state (e.g. an RTK root state with many
slices). We select just the `RuntimeState` slice out of it via a provided
selector, and bind the action callbacks. The resulting `ChatRuntime` is what
jcode-ui components consume.

Snapshot caching: the host store's state reference changes only when a reducer
dispatches. We cache the normalized RuntimeState keyed on that raw reference,
so `getState()` returns a stable identity between dispatches — a hard
requirement for React's `useSyncExternalStore` (which loops otherwise).
/

import type { ChatRuntime, PartialRuntimeState, RuntimeActions } from './index.js'
import { normalizeState } from './index.js'

export interface ExternalStoreRuntimeOptions<THostState> {
  /** The external store. Must expose Redux-compatible getState/subscribe. */
  store: {
    getState: () => THostState
    subscribe: (listener: () => void) => () => void
  }
  /** Project the host state down to a (possibly partial) RuntimeState. */
  select: (state: THostState) => PartialRuntimeState
  /** Action bag. Identity should be stable across renders. */
  actions: RuntimeActions
}

/**
Wrap an external store as a ChatRuntime. The returned object's `getState`
always returns a fully-populated RuntimeState (missing slices defaulted), and
returns the SAME object reference between dispatches (so it's safe to pass to
useSyncExternalStore's getSnapshot).

```ts
export function createExternalStoreRuntime<THostState>(
  opts: ExternalStoreRuntimeOptions<THostState>,
): ChatRuntime { … }
```

### `createFetchTransport`

<!-- jcode-ui-core-createfetchtransport -->

`function` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

Parse one SSE event block into an AG-UI event. Joins multiple `data:` lines,
skips comments/other fields, and returns null for keep-alives, `[DONE]`
sentinels, or unparseable JSON (so a single bad frame can't kill the stream).
/
function parseSSEBlock(block: string): AGUIEvent | null {
  const dataLines: string[] = []
  for (const line of block.split('\n')) {
    if (line === '' || line.startsWith(':')) continue
    if (line.startsWith('data:')) {
      // A single leading space after the colon is part of the SSE framing.
      dataLines.push(line.slice(5).replace(/^ /, ''))
    }
  }
  if (dataLines.length === 0) return null
  const payload = dataLines.join('\n')
  if (payload === '[DONE]') return null
  try {
    return JSON.parse(payload) as AGUIEvent
  } catch {
    return null
  }
}

/**
Turn a byte stream of SSE frames into a stream of AG-UI events. Frame
boundaries are blank lines; CRLF is normalized to LF first so the same
splitter handles both line endings.
/
export async function* parseSSEStream(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<AGUIEvent> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n')
      let boundary = buf.indexOf('\n\n')
      while (boundary !== -1) {
        const ev = parseSSEBlock(buf.slice(0, boundary))
        if (ev) yield ev
        buf = buf.slice(boundary + 2)
        boundary = buf.indexOf('\n\n')
      }
    }
    const tail = (buf + decoder.decode()).trim()
    if (tail) {
      const ev = parseSSEBlock(tail)
      if (ev) yield ev
    }
  } finally {
    reader.releaseLock()
  }
}

/**
The default transport: HTTP POST + `text/event-stream`. Built from the
runtime's `url`/`headers` and closed over so the `AGUITransport` it returns
matches the `(input, signal)` shape tests use.

```ts
export function createFetchTransport(
  url: string,
  headers?: Record<string, string>,
): AGUITransport { … }
```

### `createInlineImageAdapter`

<!-- jcode-ui-core-createinlineimageadapter -->

`function` · `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts`

Reject images larger than this (bytes). Default 10MB. */
  maxBytes?: number
  /** `accept` for the picker. Default `image/*`. */
  accept?: string
}

/**
The default adapter: reads image files into base64 (the existing ChatImage
behavior) and reports an error for non-images or oversize files. Keeps the
`sendMessage(text, images)` fast-path working with zero host wiring.

```ts
export function createInlineImageAdapter(options: InlineImageAdapterOptions = {}): AttachmentAdapter { … }
```

### `createMockRuntime`

<!-- jcode-ui-core-createmockruntime -->

`function` · `packages/jcode-ui-core/src/runtime/mockRuntime.ts`

MockRuntime — a self-contained, scriptable ChatRuntime for demos, docs, and
tests. No backend required: you feed it a "script" of items + a streaming
text fragment sequence, and it emits them on a timer.
/

import type { ChatRuntime, PartialRuntimeState, RuntimeActions, RuntimeState } from './index.js'
import { normalizeState } from './index.js'
import type { ThreadItem } from '../types/index.js'

export interface MockRuntimeOptions {
  /** Initial items. */
  items?: ThreadItem[]
  /** Initial isRunning flag. */
  isRunning?: boolean
  /** Initial full/partial runtime state (overrides items/isRunning when set). */
  state?: PartialRuntimeState
  /** Captured-action handlers (defaults are no-ops that record to `.calls`). */
  actions?: Partial<RuntimeActions>
}

function maxSeq(items: ThreadItem[]): number {
  let m = 0
  for (const i of items) {
    if (i.seq > m) m = i.seq
  }
  return m
}

/**
Create a ChatRuntime backed by an in-memory store with pub/sub. Exposes
imperative mutators (`setItems`, `push`, `appendText`, `setRunning`, `patchState`)
so a script driver (or a test / docs demo) can evolve the state over time.

```ts
export function createMockRuntime(opts: MockRuntimeOptions = {}): ChatRuntime & { … }
```

### `createMockThreadStore`

<!-- jcode-ui-core-createmockthreadstore -->

`function` · `packages/jcode-ui-core/src/threads/store.ts`

Replace the whole thread array. */
  setThreads: (threads: ThreadSummary[]) => void
  /** Merge a partial state patch (threads/activeId/loading). */
  patchState: (partial: Partial<ThreadListState>) => void
  /** Recorded action invocations, for test assertions. */
  calls: { action: string; args: unknown[] }[]
}

function normalizeSeed(seed: MockThreadStoreSeed | undefined): ThreadListState {
  if (Array.isArray(seed)) return { threads: seed }
  return {
    threads: seed?.threads ?? [],
    activeId: seed?.activeId,
    loading: seed?.loading,
  }
}

/**
Create an in-memory `ThreadStore` with every action wired to real mutations.
Ideal for demos, docs, Storybook, and tests — no host store required.

@example
  const store = createMockThreadStore([
    { id: 'a', title: 'Refactor auth', updatedAt: Date.now(), status: 'running' },
  ])
  <ThreadStoreProvider store={store}><ThreadList /></ThreadStoreProvider>

```ts
export function createMockThreadStore(seed?: MockThreadStoreSeed): MockThreadStore { … }
```

### `createToolRendererRegistry`

<!-- jcode-ui-core-createtoolrendererregistry -->

`function` · `packages/jcode-ui-core/src/adapters/index.ts`

Register a renderer for one or more tool names (later writes win). */
  register(name: string, renderer: ToolRenderer): this
  register(names: string[], renderer: ToolRenderer): this
  register(nameOrNames: string | string[], renderer: ToolRenderer): this {
    const names = Array.isArray(nameOrNames) ? nameOrNames : [nameOrNames]
    for (const n of names) this.renderers.set(n, renderer)
    return this
  }

  /** Register a batch of { name → renderer } entries. */
  registerAll(entries: Record<string, ToolRenderer>): this {
    for (const [name, renderer] of Object.entries(entries)) {
      this.renderers.set(name, renderer)
    }
    return this
  }

  /** Set the renderer used when no name-specific match exists. */
  setFallback(renderer: ToolRenderer): this {
    this.fallback = renderer
    return this
  }

  /** Look up a renderer by tool name, falling back if absent. Returns null
 only when nothing is registered AND no fallback is set. */
  get(name: string): ToolRenderer | null {
    return this.renderers.get(name) ?? this.fallback
  }

  /** True if a name-specific renderer is registered. */
  has(name: string): boolean {
    return this.renderers.has(name)
  }
}

/** Create a fresh registry. Convenience over `new` for chained registration.

```ts
export function createToolRendererRegistry(): ToolRendererRegistry { … }
```

### `exportThreadMarkdown`

<!-- jcode-ui-core-exportthreadmarkdown -->

`function` · `packages/jcode-ui-core/src/export/markdown.ts`

```ts
exportThreadMarkdown(items: ThreadItem[], opts: ExportMarkdownOptions = {}): string { … }
```

### `groupExploringTimeline`

<!-- jcode-ui-core-groupexploringtimeline -->

`function` · `packages/jcode-ui-core/src/timeline/groupExploring.ts`

Collapse consecutive collapsible tools into exploring groups.
Non-tool items and non-collapsible tools always break a group.

```ts
export function groupExploringTimeline(items: ThreadItem[]): ThreadItem[] { … }
```

### `isApprovalItem`

<!-- jcode-ui-core-isapprovalitem -->

`function` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export function isApprovalItem(i: ThreadItem): i is Extract<ThreadItem, { kind: 'approval' }> { … }
```

### `isCollapsibleTool`

<!-- jcode-ui-core-iscollapsibletool -->

`function` · `packages/jcode-ui-core/src/timeline/groupExploring.ts`

Exploring-group coalescing for the chat timeline.

Adjacent collapsible/read-only tool items collapse into one synthetic
`exploring` ThreadItem. Mutating tools, agent text, and approvals break the
group. Grouping is UI-only — tool-call ids and model boundaries are unchanged.
/

import type { ExploringGroup, ThreadItem, ToolCall, ToolStatus } from '../types/index.js'

const COLLAPSIBLE_NAMES = new Set([
  'read',
  'grep',
  'glob',
  'todoread',
  'load_skill',
  'browser_snapshot',
  'browser_screenshot',
  'browser_read',
  'browser_tabs',
])

/** True when a tool should join an Exploring/Explored group.

```ts
export function isCollapsibleTool(tool: ToolCall): boolean { … }
```

### `isExploringItem`

<!-- jcode-ui-core-isexploringitem -->

`function` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export function isExploringItem(i: ThreadItem): i is Extract<ThreadItem, { kind: 'exploring' }> { … }
```

### `isMessageItem`

<!-- jcode-ui-core-ismessageitem -->

`function` · `packages/jcode-ui-core/src/types/index.ts`

Type guard helpers (kept generic so consumers can narrow item arrays).

```ts
export function isMessageItem(i: ThreadItem): i is Extract<ThreadItem, { kind: 'message' }> { … }
```

### `isToolItem`

<!-- jcode-ui-core-istoolitem -->

`function` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export function isToolItem(i: ThreadItem): i is Extract<ThreadItem, { kind: 'tool' }> { … }
```

### `MessageView`

<!-- jcode-ui-core-messageview -->

`function` · `packages/jcode-ui-core/src/primitives/MessageView.tsx`

```ts
export function MessageView({
  message,
  canEdit = false,
  showCopy = true,
  className,
  renderContent,
  renderAvatar,
  renderActions,
}: MessageViewProps): ReactNode { … }
```

### `nextAttachmentId`

<!-- jcode-ui-core-nextattachmentid -->

`function` · `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts`

`accept` attribute for the file picker (default falls back to `*​/*`). */
  accept?: string
  /**
Ingest a file. `onProgress` (0–1) may be called any number of times before
the promise settles. Resolve with `error` set for a handled failure.
/
  add(file: File, onProgress?: (p: number) => void): Promise<PendingAttachment>
  /** Optional cleanup when an attachment is removed (e.g. delete an upload). */
  remove?(id: string): void | Promise<void>
}

let idCounter = 0

/** Monotonic, collision-resistant id for composer-assigned attachments.

```ts
export function nextAttachmentId(): string { … }
```

### `normalizeState`

<!-- jcode-ui-core-normalizestate -->

`function` · `packages/jcode-ui-core/src/runtime/index.ts`

Merge a partial state onto the default empty state. Missing slices get
 safe defaults so components never have to null-check.

```ts
export function normalizeState(partial: PartialRuntimeState | undefined): RuntimeState { … }
```

### `parseResolvedAnswers`

<!-- jcode-ui-core-parseresolvedanswers -->

`function` · `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx`

Current selection map (question key → labels). */
  selected: Record<string, string[]>
  /** Current "Other" text map (question key → free text). */
  other: Record<string, string>
  /** Toggle an option. Honors multi_select. */
  toggleOption: (question: AskUserQuestion, label: string) => void
  /** Set the free-text value for a question. */
  setOther: (question: AskUserQuestion, value: string) => void
  /** Submit the current selections (no-op if nothing chosen per question). */
  submit: () => void
  /** Submit empty answers (skip). */
  skip: () => void
}

export interface AskUserBlockRenderSlots {
  /** Render the pending interactive card. */
  renderPending?: (questions: AskUserQuestion[], controls: AskUserControls) => ReactNode
  /** Render the resolved (replay) view. */
  renderResolved?: (tool: ToolCall, answers: AskUserAnswer[]) => ReactNode
}

export interface AskUserBlockProps extends AskUserBlockRenderSlots {
  tool: ToolCall
  /** className passthrough. */
  className?: string
}

const EMPTY_STATE: AskUserState = { selected: {}, other: {} }

export function AskUserBlock({ tool, className, renderPending, renderResolved }: AskUserBlockProps): ReactNode {
  const actions = useRuntimeActions()
  const questions = useMemo(() => extractQuestions(tool), [tool])
  const isPending = !!tool.askUserId && tool.status === 'running' && !tool.output

  const [state, setState] = useState<AskUserState>(EMPTY_STATE)

  const keyOf = useCallback((q: AskUserQuestion) => q.header ?? q.question, [])

  const toggleOption = useCallback(
    (q: AskUserQuestion, label: string) => {
      const key = keyOf(q)
      setState((s) => ({
        ...s,
        selected: q.multi_select
          ? { ...s.selected, [key]: toggle(s.selected[key], label) }
          : { ...s.selected, [key]: [label] },
      }))
    },
    [keyOf],
  )

  const setOther = useCallback(
    (q: AskUserQuestion, value: string) => {
      const key = keyOf(q)
      setState((s) => ({ ...s, other: { ...s.other, [key]: value } }))
    },
    [keyOf],
  )

  const submit = useCallback(() => {
    const answers: AskUserAnswer[] = questions.map((q) => {
      const key = keyOf(q)
      const sel = state.selected[key] ?? []
      const other = state.other[key] ?? ''
      return {
        question_header: key,
        answer: sel.length > 0 ? sel.join(', ') : other,
        selected: sel.length > 0 ? sel : undefined,
      }
    })
    if (tool.askUserId) actions.submitAskUser(tool.askUserId, answers)
  }, [actions, keyOf, questions, state, tool.askUserId])

  const skip = useCallback(() => {
    if (tool.askUserId) actions.submitAskUser(tool.askUserId, [])
  }, [actions, tool.askUserId])

  // Digit-key shortcuts (1-9) select an option for the first unanswered question.
  useEffect(() => {
    if (!isPending) return
    function onKey(e: KeyboardEvent) {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return
      const n = Number(e.key)
      if (!Number.isInteger(n) || n < 1 || n > 9) return
      const q = questions.find((qq) => (state.selected[keyOf(qq)]?.length ?? 0) === 0)
      if (!q?.options || n > q.options.length) return
      e.preventDefault()
      toggleOption(q, q.options[n - 1].label)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [isPending, questions, state.selected, keyOf, toggleOption])

  if (!isPending) {
    const answers = parseResolvedAnswers(tool)
    return <div data-jcode-ui="" className={className}>{renderResolved?.(tool, answers) ?? <DefaultResolved tool={tool} answers={answers} />}</div>
  }

  const controls: AskUserControls = {
    selected: state.selected,
    other: state.other,
    toggleOption,
    setOther,
    submit,
    skip,
  }

  return (
    <div data-jcode-ui="" className={className} data-ask-user-id={tool.askUserId}>
      {renderPending?.(questions, controls) ?? <DefaultPending questions={questions} controls={controls} />}
    </div>
  )
}

/** Extract the questions list from tool fields (with fallbacks). */
function extractQuestions(tool: ToolCall): AskUserQuestion[] {
  if (tool.askUserQuestions && tool.askUserQuestions.length > 0) return tool.askUserQuestions
  try {
    const parsed = JSON.parse(tool.args)
    if (Array.isArray(parsed.questions)) return parsed.questions as AskUserQuestion[]
    if (parsed.question) {
      // legacy single-question shape
      return [{ question: parsed.question, options: parsed.options ?? [] }]
    }
  } catch {
    // ignore
  }
  return []
}

/** Best-effort parse of a resolved tool's output into answers (for replay).

```ts
export function parseResolvedAnswers(tool: ToolCall): AskUserAnswer[] { … }
```

### `RuntimeProvider`

<!-- jcode-ui-core-runtimeprovider -->

`function` · `packages/jcode-ui-core/src/runtime/context.tsx`

React binding for ChatRuntime: a Context provider + hooks that subscribe via
`useSyncExternalStore` (the React 18 idiom for external stores — handles
tearing and concurrent rendering). Selector memoization is layered on top
with a ref cache so granular reads don't re-render on unrelated changes.

Stability contract: the ChatRuntime's getState() MUST return a stable
reference between dispatches (the ExternalStoreRuntime + MockRuntime both
honor this by caching). Without it, useSyncExternalStore infinite-loops.
/

import { createContext, useContext, useMemo, useRef, useSyncExternalStore } from 'react'
import type { ReactNode } from 'react'
import type { ChatRuntime, RuntimeState } from './index.js'

const RuntimeContext = createContext<ChatRuntime | null>(null)

export interface RuntimeProviderProps {
  runtime: ChatRuntime
  children: ReactNode
}

/** Provide a ChatRuntime to a subtree. Components under it read state/actions
 via `useRuntimeState` / `useRuntimeSelector` / `useRuntimeActions`.

```ts
export function RuntimeProvider({ runtime, children }: RuntimeProviderProps): ReactNode { … }
```

### `summarizeExploringSteps`

<!-- jcode-ui-core-summarizeexploringsteps -->

`function` · `packages/jcode-ui-core/src/timeline/groupExploring.ts`

Summarize an exploring group into action lines (Read / Search / List …).

```ts
export function summarizeExploringSteps(tools: ToolCall[]): { … }
```

### `Thread`

<!-- jcode-ui-core-thread -->

`function` · `packages/jcode-ui-core/src/primitives/Thread.tsx`

```ts
export function Thread({
  renderItem,
  renderPending,
  renderEmpty,
  renderFooter,
  virtualize = true,
  estimateSize = 80,
  scrollThreshold = 80,
  overscanBottom = 0,
  className,
  role = 'log',
  containerRef,
  mapItems,
}: ThreadProps): ReactNode { … }
```

### `ThreadStoreProvider`

<!-- jcode-ui-core-threadstoreprovider -->

`function` · `packages/jcode-ui-core/src/threads/context.tsx`

React binding for `ThreadStore`: a Context provider + hooks that subscribe via
`useSyncExternalStore` (the React 18 idiom for external stores). This mirrors
the runtime binding in `../runtime/context.tsx` exactly — the thread list is
just a second external store living alongside the conversation runtime.

Stability contract: the store's `getState()` MUST return a stable reference
between changes (the mock honors this; real hosts must too), otherwise
`useSyncExternalStore` infinite-loops.
/

import { createContext, useContext, useMemo, useSyncExternalStore } from 'react'
import type { ReactNode } from 'react'
import type { ThreadStore, ThreadListState, ThreadStoreActions } from './store.js'

const ThreadStoreContext = createContext<ThreadStore | null>(null)

export interface ThreadStoreProviderProps {
  store: ThreadStore
  children: ReactNode
}

/** Provide a `ThreadStore` to a subtree. `ThreadList` reads it via the hooks.

```ts
export function ThreadStoreProvider({ store, children }: ThreadStoreProviderProps): ReactNode { … }
```

### `ToolCallProvider`

<!-- jcode-ui-core-toolcallprovider -->

`function` · `packages/jcode-ui-core/src/primitives/ToolCallView.tsx`

```ts
export function ToolCallProvider({ value, children }: { value: ToolCallContextValue; children: ReactNode }) { … }
```

### `ToolCallView`

<!-- jcode-ui-core-toolcallview -->

`function` · `packages/jcode-ui-core/src/primitives/ToolCallView.tsx`

```ts
export function ToolCallView({
  tool,
  depth = 0,
  maxDepth = 4,
  defaultExpanded,
  className,
  renderHeader,
  renderSubagentOutput,
  renderSubagentChildren,
}: ToolCallViewProps): ReactNode { … }
```

### `useAutoScroll`

<!-- jcode-ui-core-useautoscroll -->

`function` · `packages/jcode-ui-core/src/hooks/index.ts`

Behavioral hooks for chat UI primitives. These contain the interaction logic
the Vue version baked into App.vue (scroll tracking, type-ahead draining,
etc.) but framework-correct and reusable.
/

import { useCallback, useEffect, useRef } from 'react'
import { useRuntimeState } from '../runtime/context.js'

/**
Auto-scroll-to-bottom tracking: reports whether the user is "at the bottom"
of a scroll container (within `threshold` px of the bottom edge). When at the
bottom, streaming content auto-follows; when scrolled up, it does NOT yank the
user back down (the core streaming-UX contract from the Vue version).

Returns the container ref to attach, the live `isAtBottom` flag, and a
`scrollToBottom` imperative. The caller decides when to call the latter
(typically on send, and on new content if `isAtBottom`).

```ts
export function useAutoScroll<T extends HTMLElement>(threshold = 80) { … }
```

### `useFocusOnIdle`

<!-- jcode-ui-core-usefocusonidle -->

`function` · `packages/jcode-ui-core/src/hooks/index.ts`

Auto-focus a ref on mount and when `isRunning` flips false (the Vue version
refocuses the composer when a turn ends).

```ts
export function useFocusOnIdle<T extends HTMLElement>(isRunning: boolean) { … }
```

### `useIsAtBottom`

<!-- jcode-ui-core-useisatbottom -->

`function` · `packages/jcode-ui-core/src/hooks/index.ts`

Imperatively scroll to the bottom edge. `behavior` defaults to 'auto'
 (instant) since this is called mid-stream. */
  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'auto') => {
    const el = ref.current
    if (!el) return
    el.scrollTo({ top: el.scrollHeight, behavior })
    isAtBottomRef.current = true
  }, [])

  /** Attach to the container's onScroll (or wire a listener). Updates the flag. */
  const onScroll = useCallback(() => {
    const el = ref.current
    if (!el) return
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight
    isAtBottomRef.current = distance <= threshold
  }, [threshold])

  /** Read the current flag. Use this in effects; for render, prefer the
 `useIsAtBottom` hook below which re-renders on change. */
  const getIsAtBottom = useCallback(() => isAtBottomRef.current, [])

  return { ref, onScroll, scrollToBottom, getIsAtBottom, isAtBottomRef }
}

/**
Re-render-friendly version of the at-bottom flag: re-renders the component
when the flag flips. Use sparingly (the scroll handler runs a lot); for most
cases the imperative `getIsAtBottom` + an effect is enough.

NOTE: this intentionally tracks a coarse boolean — it only re-renders on
crossing the threshold, not on every scroll event.

```ts
export function useIsAtBottom<T extends HTMLElement>(threshold = 80) { … }
```

### `useQueuedMessages`

<!-- jcode-ui-core-usequeuedmessages -->

`function` · `packages/jcode-ui-core/src/hooks/index.ts`

Track + drain the type-ahead queue: returns the current queued messages.
Draining is the runtime's job (it sends the next queued message on each turn
end); this hook just surfaces the queue for rendering.

```ts
export function useQueuedMessages() { … }
```

### `useRuntimeActions`

<!-- jcode-ui-core-useruntimeactions -->

`function` · `packages/jcode-ui-core/src/runtime/context.tsx`

Stable handle to the action bag. Identity is owned by the runtime.

```ts
export function useRuntimeActions() { … }
```

### `useRuntimeSelector`

<!-- jcode-ui-core-useruntimeselector -->

`function` · `packages/jcode-ui-core/src/runtime/context.tsx`

Subscribe to a derived slice of RuntimeState. The selector MUST be stable
(memoize with useCallback) or return a primitive; otherwise React will
re-render on every store change. For object returns, also pass an `isEqual`
(e.g. shallow-equal) to avoid identity churn.

```ts
export function useRuntimeSelector<T>(
  selector: (state: RuntimeState) => T,
  isEqual: (a: T, b: T) => boolean = Object.is,
): T {
  const runtime = useRuntime()
  // Subscribe to the raw snapshot (stable identity), then memoize the selected
  // value in a ref so a stable selector returning a primitive doesn't trigger
  // spurious re-renders.
  const snapshot = useSyncExternalStore(runtime.subscribe, runtime.getState, runtime.getState)
  const cacheRef = useRef< { … }
```

### `useRuntimeState`

<!-- jcode-ui-core-useruntimestate -->

`function` · `packages/jcode-ui-core/src/runtime/context.tsx`

Subscribe to the full RuntimeState. Re-renders on any store change. Prefer
`useRuntimeSelector` for granular reads to avoid re-rendering on unrelated
state changes.

```ts
export function useRuntimeState(): RuntimeState { … }
```

### `useStreamFollow`

<!-- jcode-ui-core-usestreamfollow -->

`function` · `packages/jcode-ui-core/src/hooks/index.ts`

Stream-follow effect: when the runtime emits new/changed items, scroll to
bottom ONLY if the user was already at the bottom. This is the declarative
form of the Vue watch on `timeline.length + lastMessage.content.length`.

`dep` should be a value that changes whenever there's new content to follow
(e.g. items length, or last-item content length).

```ts
export function useStreamFollow<T extends HTMLElement>(
  autoScroll: ReturnType<typeof useAutoScroll<T>>,
  dep: unknown,
) { … }
```

### `useThreadListState`

<!-- jcode-ui-core-usethreadliststate -->

`function` · `packages/jcode-ui-core/src/threads/context.tsx`

Subscribe to the full `ThreadListState`. Re-renders on any list change.

```ts
export function useThreadListState(): ThreadListState { … }
```

### `useThreadStoreActions`

<!-- jcode-ui-core-usethreadstoreactions -->

`function` · `packages/jcode-ui-core/src/threads/context.tsx`

Stable handle to the thread-list action bag (identity owned by the store).

```ts
export function useThreadStoreActions(): ThreadStoreActions { … }
```

### `useToolCallContext`

<!-- jcode-ui-core-usetoolcallcontext -->

`function` · `packages/jcode-ui-core/src/primitives/ToolCallView.tsx`

```ts
export function useToolCallContext(): ToolCallContextValue | null { … }
```

### `AGUIMessage`

<!-- jcode-ui-core-aguimessage -->

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

An AG-UI conversation message (as sent in run input / MESSAGES_SNAPSHOT).

```ts
export interface AGUIMessage {
  id: string
  role: AGUIRole | string
  content?: string | null
  /** Tool name (present on `role: 'tool'` result messages). */
  name?: string
  /** Links a `role: 'tool'` message to the tool call it answers. */
  toolCallId?: string
  /** Present on assistant messages that invoke tools. */
  toolCalls?: AGUIToolCall[]
}
```

### `AGUIPatchOp`

<!-- jcode-ui-core-aguipatchop -->

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

Tool name (present on `role: 'tool'` result messages). */
  name?: string
  /** Links a `role: 'tool'` message to the tool call it answers. */
  toolCallId?: string
  /** Present on assistant messages that invoke tools. */
  toolCalls?: AGUIToolCall[]
}

/** A single RFC 6902 JSON Patch operation (STATE_DELTA payload element).

```ts
export interface AGUIPatchOp {
  op: 'add' | 'replace' | 'remove' | 'move' | 'copy' | 'test' | string
  path: string
  value?: unknown
  from?: string
}
```

### `AGUIRunInput`

<!-- jcode-ui-core-aguiruninput -->

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

The POST body sent to open a run (AG-UI `RunAgentInput`, trimmed).

```ts
export interface AGUIRunInput {
  threadId: string
  runId: string
  messages: AGUIMessage[]
  state?: unknown
  tools?: unknown[]
  context?: unknown[]
  forwardedProps?: unknown
}
```

### `AGUIRuntime`

<!-- jcode-ui-core-aguiruntime -->

`interface` · `packages/jcode-ui-core/src/runtime/aguiRuntime.ts`

createAGUIRuntime — drive jcode-ui from any AG-UI protocol backend.

An AG-UI server (LangGraph, CrewAI, Mastra, …) emits a normalized event stream;
this reducer folds that stream into `RuntimeState` so those backends need zero
glue to render in jcode-ui. It is the AG-UI sibling of `mockRuntime` /
`externalStore`: same `{ getState, subscribe, actions }` contract, same
stable-snapshot discipline that `context.tsx` requires from `getState`.

Design notes (constraints not obvious from the code):
- Snapshot stability: `getState()` returns the SAME object reference until a
  real change, or `useSyncExternalStore` loops. We rebuild the snapshot only
  inside the batched flush.
- Notify batching: many events arrive per microtask (START/CONTENT/END for one
  token burst). We coalesce listener notifications with `queueMicrotask`, like
  externalStore leans on the host store's single post-dispatch notification.
- Item indices: `msgIndex`/`toolIndex` map ids → positions in `items`. Only
  append + full MESSAGES_SNAPSHOT rebuild ever run, so appended indices stay
  valid; immutable per-item replacement preserves positions.
- Agent state lives OUTSIDE RuntimeState (it is arbitrary backend JSON, not a
  chat concept) and is read via `getAgentState()`.
/

import type { ChatRuntime, RuntimeActions, RuntimeState } from './index.js'
import type { ConnectionState, Message, Role, ThreadItem, ToolCall } from '../types/index.js'
import type {
  AGUIEvent,
  AGUIMessage,
  AGUIPatchOp,
  AGUIRunInput,
  AGUITransport,
} from './aguiEvents.js'
import { createFetchTransport } from './aguiEvents.js'

export interface AGUIRuntimeOptions {
  /** AG-UI run endpoint (POST, streams `text/event-stream`). */
  url: string
  /** Extra request headers (auth, etc.) for the default transport. */
  headers?: Record<string, string>
  /** Override the event source — inject a scripted stream in tests, or swap in
 WebSocket/other transports. Defaults to `createFetchTransport(url, headers)`. */
  transport?: AGUITransport
  /** Stable thread id for the whole session. Auto-generated when omitted. */
  threadId?: string
}

/** The AG-UI runtime adds read-only agent-state access to the base contract.

```ts
export interface AGUIRuntime extends ChatRuntime {
  /** The latest STATE_SNAPSHOT with STATE_DELTA patches applied, or undefined. */
  getAgentState: () => unknown
}
```

### `AGUIToolCall`

<!-- jcode-ui-core-aguitoolcall -->

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

A tool call embedded in an AG-UI assistant message (OpenAI-shaped).

```ts
export interface AGUIToolCall {
  id: string
  type?: string
  function: { name: string; arguments: string }
}
```

### `Approval`

<!-- jcode-ui-core-approval -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A pending approval gate. While `resolved` is falsy the UI shows the decision
controls; once resolved it collapses to an inline note.

Two shapes: the classic boolean contract (allow once / allow all / deny), or
host-defined `options` (arbitrary ids, e.g. ACP permission_request) — when
`options` is present the UI renders one control per option instead.

```ts
export interface Approval {
  id: string
  tool_name: string
  tool_args: string
  /** Target outside the workspace root — UI flags it prominently. */
  is_external: boolean
  resolved?: boolean
  approved?: boolean
  /** True while a resolve request is in flight (disables controls). */
  resolving?: boolean
  /** Host-defined decision options; absent → classic boolean controls. */
  options?: ApprovalOption[]
  /** The chosen option id once resolved (options mode). */
  resolvedOptionId?: string
}
```

### `ApprovalBlockProps`

<!-- jcode-ui-core-approvalblockprops -->

`interface` · `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx`

```ts
export interface ApprovalBlockProps extends ApprovalBlockRenderSlots {
  approval: Approval
  /** className passthrough. */
  className?: string
}
```

### `ApprovalBlockRenderSlots`

<!-- jcode-ui-core-approvalblockrenderslots -->

`interface` · `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx`

```ts
export interface ApprovalBlockRenderSlots {
  /** Render the pending decision card. Receives the action callbacks. */
  renderPending?: (approval: Approval, actions: ApprovalDecisionActions) => ReactNode
  /** Render the resolved inline note. */
  renderResolved?: (approval: Approval) => ReactNode
}
```

### `ApprovalDecisionActions`

<!-- jcode-ui-core-approvaldecisionactions -->

`interface` · `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx`

```ts
export interface ApprovalDecisionActions {
  allowOnce: () => void
  allowAllArm: () => void
  allowAllConfirm: () => void
  allowAllCancel: () => void
  deny: () => void
  armed: boolean
  /** Options mode: choose a non-arming option (kind ≠ 'allow_always'). */
  choose: (optionId: string) => void
  /** Options mode: id currently armed for two-step confirm, or null. */
  armedOptionId: string | null
  /** Options mode: arm an 'allow_always' option (first click). */
  armOption: (optionId: string) => void
  /** Options mode: confirm the armed option (second click). */
  confirmOption: (optionId: string) => void
  /** Options mode: cancel arming. */
  cancelArm: () => void
}
```

### `ApprovalOption`

<!-- jcode-ui-core-approvaloption -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A host-defined approval decision (e.g. an ACP permission option). The `id`
 is echoed back verbatim via `resolveApprovalOption`.

```ts
export interface ApprovalOption {
  id: string
  label: string
  kind?: ApprovalOptionKind
  description?: string
}
```

### `AskUserAnswer`

<!-- jcode-ui-core-askuseranswer -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A resolved `ask_user` answer (mirrors backend AskUserAnswer).

```ts
export interface AskUserAnswer {
  question_header: string
  /** single-select label or free text. */
  answer: string
  /** multi-select labels. */
  selected?: string[]
}
```

### `AskUserControls`

<!-- jcode-ui-core-askusercontrols -->

`interface` · `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx`

AskUserBlock — the headless interactive question block.

Owns: the pending/resolved split, per-question selection state (single +
multi-select), free-text "Other" input, digit-key shortcuts (1-9), and
dispatching via runtime actions. Does NOT own styling or the output-format
parsing for resolved display (those live in the styled wrapper).

The `renderPending` slot receives a `controls` object exposing the live
`selected`/`other` maps plus mutators (`toggleOption`, `setOther`) and the
`submit`/`skip` actions — so a styled consumer needs no local state.
/

import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import type { AskUserQuestion, AskUserAnswer, ToolCall } from '../types/index.js'
import { useRuntimeActions } from '../runtime/context.js'

export interface AskUserState {
  /** Per-question-header selected labels (single-select: one entry; multi: N). */
  selected: Record<string, string[]>
  /** Per-question-header free-text "Other" value. */
  other: Record<string, string>
}

/** Controls handed to the pending render-prop.

```ts
export interface AskUserControls {
  /** Current selection map (question key → labels). */
  selected: Record<string, string[]>
  /** Current "Other" text map (question key → free text). */
  other: Record<string, string>
  /** Toggle an option. Honors multi_select. */
  toggleOption: (question: AskUserQuestion, label: string) => void
  /** Set the free-text value for a question. */
  setOther: (question: AskUserQuestion, value: string) => void
  /** Submit the current selections (no-op if nothing chosen per question). */
  submit: () => void
  /** Submit empty answers (skip). */
  skip: () => void
}
```

### `AskUserOption`

<!-- jcode-ui-core-askuseroption -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

An option in an `ask_user` question.

```ts
export interface AskUserOption {
  label: string
  description?: string
}
```

### `AskUserQuestion`

<!-- jcode-ui-core-askuserquestion -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A single interactive question posed by the agent mid-run.

```ts
export interface AskUserQuestion {
  question: string
  header?: string
  options?: AskUserOption[]
  multi_select?: boolean
}
```

### `AttachmentAdapter`

<!-- jcode-ui-core-attachmentadapter -->

`interface` · `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts`

Stable id (adapter- or composer-assigned) for keying, remove, retry. */
  id: string
  /** Coarse kind — drives tile (image) vs. chip (file) presentation. */
  kind: 'image' | 'file'
  /** Display name (usually the original filename). */
  name: string
  /** Size in bytes when known. */
  size?: number
  /** MIME type when known. */
  media_type?: string
  /** base64 payload for images (ChatImage fast-path, no `data:` prefix). */
  data?: string
  /** Host URL once uploaded (upload adapters). */
  url?: string
  /** Upload progress in the range 0–1. */
  progress?: number
  /** Failure message; when set the attachment is in the error state. */
  error?: string
}

/**
Turns raw `File`s into `PendingAttachment`s. Supplied to `Composer` via the
`attachmentAdapter` prop.

```ts
export interface AttachmentAdapter {
  /** `accept` attribute for the file picker (default falls back to `*​/*`). */
  accept?: string
  /**
   * Ingest a file. `onProgress` (0–1) may be called any number of times before
   * the promise settles. Resolve with `error` set for a handled failure.
   */
  add(file: File, onProgress?: (p: number) => void): Promise<PendingAttachment>
  /** Optional cleanup when an attachment is removed (e.g. delete an upload). */
  remove?(id: string): void | Promise<void>
}
```

### `ChatImage`

<!-- jcode-ui-core-chatimage -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A base64-encoded image attached to a message (no `data:` prefix).

Image-first (vision models). Generic file/PDF adapters are host concerns —
see docs/chat-ui attachments guide. Optional `name` is used for tooltips and
a11y labels (mirrors assistant-ui Attachment.name).

```ts
export interface ChatImage {
  data: string
  media_type: string
  /** Original filename when known (file picker / drag-drop). */
  name?: string
}
```

### `ChatRuntime`

<!-- jcode-ui-core-chatruntime -->

`interface` · `packages/jcode-ui-core/src/runtime/index.ts`

Send a user-authored message. `images` are base64 payloads. */
  sendMessage: (text: string, images?: { data: string; media_type: string }[]) => void
  /** Enqueue a message while a turn is running (type-ahead). */
  enqueueMessage: (text: string, images?: { data: string; media_type: string }[]) => void
  /** Remove a queued message by id (before it is sent). */
  removeQueuedMessage: (id: string) => void
  /** Cancel the in-flight turn. */
  stop: () => void
  /** Resolve an approval gate. `approveAll` arms "allow all future" semantics. */
  resolveApproval: (id: string, approved: boolean, approveAll?: boolean) => void
  /** Answer an `ask_user` batch. */
  submitAskUser: (id: string, answers: { question_header: string; answer: string; selected?: string[] }[]) => void
  /** Edit a past user message and resend from that point. */
  editMessage: (id: string, newText: string) => void

  // ── Optional capabilities ─────────────────────────────────────────────
  // Fail-visible convention: when a host omits one of these, the UI control
  // that would dispatch it is NOT rendered (never a dead button).

  /** Resolve an approval that carries host-defined `options` (e.g. ACP
 permission_request with arbitrary option ids). */
  resolveApprovalOption?: (id: string, optionId: string) => void
  /** Regenerate an assistant message; the host appends a new version. */
  regenerate?: (messageId: string) => void
  /** Switch the visible version of a branched message. */
  switchVersion?: (messageId: string, versionId: string) => void
  /** Record 👍/👎 feedback on an assistant message. */
  submitFeedback?: (messageId: string, rating: 'up' | 'down', comment?: string) => void
  /** Retry a failed assistant turn (system error follow-up). */
  retryMessage?: (messageId: string) => void
}

/**
The contract every jcode-ui data source implements. `getState`/`subscribe`
deliberately match the Redux `Store` signature so a real RTK store can be
wrapped with a thin selector + the actions bound.

```ts
export interface ChatRuntime {
  getState: () => RuntimeState
  subscribe: (listener: () => void) => () => void
  /** Action bag. Stable identity is recommended (consumers should memoize). */
  readonly actions: RuntimeActions
}
```

### `ComposerHandle`

<!-- jcode-ui-core-composerhandle -->

`interface` · `packages/jcode-ui-core/src/primitives/Composer.tsx`

Imperative handle exposed via `ref` (used by quote / insert features).

```ts
export interface ComposerHandle {
  /** Insert text at the caret (replacing any selection) and refocus. */
  insertText(text: string): void
  /** Focus the textarea. */
  focus(): void
}
```

### `ComposerProps`

<!-- jcode-ui-core-composerprops -->

`interface` · `packages/jcode-ui-core/src/primitives/Composer.tsx`

```ts
export interface ComposerProps extends ComposerRenderSlots {
  /** Placeholder text. */
  placeholder?: string
  /** Max textarea height in px before it scrolls internally. */
  maxRows?: number
  /** Slash commands (fetched by the host). Empty/undefined disables the menu. */
  slashCommands?: SlashCommand[]
  /** Whether image attachments are allowed (gated by model vision support). */
  allowImages?: boolean
  /** `accept` attribute for the file picker (default `image/*`). */
  acceptImages?: string
  /** Max image size in bytes (default 10MB). */
  maxImageBytes?: number
  /**
   * Pluggable attachment pipeline. When provided it supersedes the legacy
   * base64 image path: the picker/paste/drop route files through the adapter
   * and a pending state machine tracks upload progress.
   */
  attachmentAdapter?: AttachmentAdapter
  /** Fired on send with the completed attachments (adapter path). */
  onSendAttachments?: (attachments: PendingAttachment[]) => void
  /** Content rendered after the add-attachment control (e.g. a ModelSelector). */
  leadingControls?: ReactNode
  /** Content rendered just before the submit button. */
  trailingControls?: ReactNode
  /** Content rendered below the composer row. */
  footer?: ReactNode
  /** Enable the dictation button (rendered only when the browser supports it). */
  enableDictation?: boolean
  /** BCP-47 language tag for dictation (default: browser default). */
  dictationLang?: string
  /** aria-label for the textarea. */
  ariaLabel?: string
  /** Controlled initial value (uncontrolled thereafter). */
  defaultValue?: string
  /** className passthrough. */
  className?: string
  /** Callback after a message is sent or queued (host snaps timeline to bottom). */
  onSent?: () => void
}
```

### `ComposerRenderSlots`

<!-- jcode-ui-core-composerrenderslots -->

`interface` · `packages/jcode-ui-core/src/primitives/Composer.tsx`

```ts
export interface ComposerRenderSlots {
  /** Render the slash-command dropdown when `slashState` is open. */
  renderSlashMenu?: (state: SlashMenuState) => ReactNode
  /** Render queued-message chips above the textarea. */
  renderQueue?: (queued: { id: string; text: string; images?: ChatImage[] }[]) => ReactNode
  /** Render the send/stop button. `mode` is 'send' or 'stop'.
   *  Call `onActivate` on click (send when idle, stop when running). */
  renderSubmitButton?: (mode: 'send' | 'stop', disabled: boolean, onActivate: () => void) => ReactNode
  /** Render attached-image thumbnails (legacy `allowImages` strip). */
  renderAttachments?: (imgs: ChatImage[], remove: (i: number) => void) => ReactNode
  /** Render the pending-attachment strip (adapter path). */
  renderPendingAttachments?: (items: PendingAttachmentItem[]) => ReactNode
  /**
   * Render the "add attachment" control (paperclip / +). Called with
   * `openPicker` that opens the hidden file input. When omitted and
   * attachments are enabled, a minimal default button is rendered.
   * Mirrors assistant-ui `ComposerAddAttachment`.
   */
  renderAddAttachment?: (openPicker: () => void) => ReactNode
  /** Render an overlay while files are dragged over the composer. */
  renderDropOverlay?: () => ReactNode
  /** Render the dictation (microphone) button. Only called when supported. */
  renderDictationButton?: (state: DictationState, toggle: () => void) => ReactNode
  /** Optional content rendered before the textarea inside the input row
   *  (e.g. a "+" menu button). */
  renderPrefix?: () => ReactNode
  /** Optional content rendered after the textarea (e.g. a context ring). */
  renderSuffix?: () => ReactNode
}
```

### `DictationState`

<!-- jcode-ui-core-dictationstate -->

`interface` · `packages/jcode-ui-core/src/primitives/Composer.tsx`

Latest adapter snapshot (or the provisional entry while uploading). */
  attachment: PendingAttachment
  status: PendingStatus
  /** Remove this attachment (also invokes `adapter.remove` when present). */
  remove: () => void
  /** Re-run `adapter.add` for this file (error recovery). */
  retry: () => void
}

/** Dictation (speech-to-text) UI state passed to `renderDictationButton`.

```ts
export interface DictationState {
  listening: boolean
}
```

### `ExploringGroup`

<!-- jcode-ui-core-exploringgroup -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

Backend tool_call_id for precise result matching. */
  toolCallID?: string
  name: string
  args: string
  output?: string
  /** Clean output for UI display (metadata stripped). */
  displayOutput?: string
  error?: string
  status: ToolStatus
  timestamp: number
  displayInfo?: ToolDisplayInfo
  /** Nested tool calls (subagent inner calls). */
  children?: ToolCall[]
  /** ask_user: request id while awaiting an answer (live runs only). */
  askUserId?: string
  /** ask_user: backend-normalized questions to render. */
  askUserQuestions?: AskUserQuestion[]
  /** Dual-channel streams (execute). */
  streams?: ToolStreams
  /** Dual-channel meta (execute). */
  meta?: ToolMeta
  /** Dual-channel presentation (execute). */
  presentation?: ToolPresentation
}

/**
A UI-only coalesced group of collapsible read/search/list tool calls.
Does not change model-facing tool boundaries.

```ts
export interface ExploringGroup {
  id: string
  tools: ToolCall[]
  status: ToolStatus
}
```

### `ExportMarkdownOptions`

<!-- jcode-ui-core-exportmarkdownoptions -->

`interface` · `packages/jcode-ui-core/src/export/markdown.ts`

```ts
export interface ExportMarkdownOptions {
  /** Document H1. Default: "Conversation". */
  title?: string
  /** Timestamp for the header line; omitted → no stamp (deterministic). */
  now?: Date
  /** Truncate a single tool output beyond this many chars. Default 4000. */
  maxToolOutput?: number
  /** Role display labels. */
  labels?: { user?: string; assistant?: string; system?: string }
}
```

### `Goal`

<!-- jcode-ui-core-goal -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

An active agent goal (set via /goal or a dedicated control).

```ts
export interface Goal {
  objective: string
  status: GoalStatus
  tokens_used?: number
  created_at?: number
  updated_at?: number
}
```

### `InlineImageAdapterOptions`

<!-- jcode-ui-core-inlineimageadapteroptions -->

`interface` · `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts`

Options for {@link createInlineImageAdapter}.

```ts
export interface InlineImageAdapterOptions {
  /** Reject images larger than this (bytes). Default 10MB. */
  maxBytes?: number
  /** `accept` for the picker. Default `image/*`. */
  accept?: string
}
```

### `Message`

<!-- jcode-ui-core-message -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A single chat message. Streaming assistant text is represented as a message
whose `content` grows over time — the runtime owns the accumulation, not the
component, so `Message` re-renders idempotently on `content` changes.

```ts
export interface Message {
  id: string
  role: Role
  content: string
  timestamp: number
  /** Origin channel for inbound messages (e.g. 'wechat'). Drives avatar tint. */
  source?: string
  images?: ChatImage[]
  /** system-message severity. */
  level?: SystemLevel
  /** Optional raw detail (collapsed by default). */
  detail?: string
  /** Assistant turn elapsed (ms), stamped on the final message of a turn. */
  durationMs?: number
  /** Optional model reasoning / chain-of-thought text (rendered collapsible).
   *  Mirrors assistant-ui's Reasoning component + OpenAI/Anthropic thinking. */
  reasoning?: string
  /** Optional citation sources for the message (rendered as a Sources list).
   *  Mirrors assistant-ui's Sources component. */
  sources?: MessageSource[]
  /** Alternate versions (edit/regenerate branches). `content` mirrors the
   *  active version; absent for unbranched messages. */
  versions?: MessageVersion[]
  /** Which entry of `versions` is showing. */
  activeVersionId?: string
  /** Recorded 👍/👎 feedback, when the host persists it. */
  feedback?: 'up' | 'down'
}
```

### `MessageActions`

<!-- jcode-ui-core-messageactions -->

`interface` · `packages/jcode-ui-core/src/primitives/MessageView.tsx`

MessageView — the headless chat bubble.

Owns: role-aware structure (avatar + label + body + actions), copy/edit
interactions, and image toggling. Does NOT own markdown rendering (passed in
via `renderContent`) or styling — the styled jcode-ui `Message` supplies the
marked+highlight.js+DOMPurify pipeline and token-driven classes.

Streaming is invisible here: `MessageView` just re-renders when
`message.content` changes. The runtime owns accumulation.
/

import { useCallback, useState } from 'react'
import type { ReactNode } from 'react'
import type { Message } from '../types/index.js'
import { useRuntimeActions } from '../runtime/context.js'

export interface MessageViewRenderSlots {
  /** Render the message body (markdown → sanitized HTML). Default: raw text. */
  renderContent?: (htmlOrText: string, message: Message) => ReactNode
  /** Render the avatar glyph for a role. */
  renderAvatar?: (role: Message['role']) => ReactNode
  /**
Render the hover action row (copy / edit). When provided, this replaces the
default text buttons entirely — the styled wrapper supplies icon buttons
while MessageView still owns the copy/edit state and handlers.
/
  renderActions?: (actions: MessageActions) => ReactNode
}

/** Action handles passed to `renderActions` so the caller can wire its own UI.

```ts
export interface MessageActions {
  /** Whether the copy action just fired (show a "copied" affordance). */
  copied: boolean
  /** Copy the message content to the clipboard. */
  onCopy: () => void
  /** Whether the user may edit this message (role==='user' && !isRunning). */
  canEdit: boolean
  /** Enter edit mode for this message. */
  onEdit: () => void
  /** Whether the message is currently being edited (hide actions while true). */
  editing: boolean
}
```

### `MessageSource`

<!-- jcode-ui-core-messagesource -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A citation source attached to a message (e.g. a retrieved doc or URL).

```ts
export interface MessageSource {
  /** Stable id for keying. */
  id: string
  /** Display title of the source. */
  title: string
  /** Optional URL or deep link. */
  url?: string
  /** Optional snippet/excerpt quoted from the source. */
  snippet?: string
}
```

### `MessageVersion`

<!-- jcode-ui-core-messageversion -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

One alternate take of a message — produced by editing a user message or
 regenerating an assistant one. The parent `Message.content` always mirrors
 the active version so non-branching consumers keep working untouched.

```ts
export interface MessageVersion {
  id: string
  content: string
  timestamp: number
  reasoning?: string
  sources?: MessageSource[]
  images?: ChatImage[]
}
```

### `MessageViewProps`

<!-- jcode-ui-core-messageviewprops -->

`interface` · `packages/jcode-ui-core/src/primitives/MessageView.tsx`

```ts
export interface MessageViewProps extends MessageViewRenderSlots {
  message: Message
  /** Whether the user may edit (typically role==='user' && !isRunning). */
  canEdit?: boolean
  /** Whether to show the copy action on hover. Default true. */
  showCopy?: boolean
  /** className passthrough. */
  className?: string
}
```

### `MockThreadStore`

<!-- jcode-ui-core-mockthreadstore -->

`interface` · `packages/jcode-ui-core/src/threads/store.ts`

The mock store plus imperative test/demo hooks.

```ts
export interface MockThreadStore extends ThreadStore {
  /** Replace the whole thread array. */
  setThreads: (threads: ThreadSummary[]) => void
  /** Merge a partial state patch (threads/activeId/loading). */
  patchState: (partial: Partial<ThreadListState>) => void
  /** Recorded action invocations, for test assertions. */
  calls: { action: string; args: unknown[] }[]
}
```

### `PendingAttachment`

<!-- jcode-ui-core-pendingattachment -->

`interface` · `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts`

AttachmentAdapter — the pluggable contract for composer attachments.

The headless `Composer` owns the pending-attachment state machine
(uploading → done / error, progress passthrough). *How* a raw `File` becomes
a sendable attachment is delegated to an adapter, so hosts can choose between:

  - inline base64 images (vision fast-path — `createInlineImageAdapter`), or
  - an upload adapter that PUTs to object storage and returns a host URL, or
  - a hybrid that inlines small images and uploads everything else.

An adapter never touches React; it takes a `File`, optionally reports
progress, and resolves a `PendingAttachment`. A failed attachment resolves
with `error` set (rather than rejecting) so the composer can render it inline
with a retry affordance — a thrown/rejected error is also tolerated by the
composer and surfaced the same way.
/

/** One attachment in the composer's pending strip.

```ts
export interface PendingAttachment {
  /** Stable id (adapter- or composer-assigned) for keying, remove, retry. */
  id: string
  /** Coarse kind — drives tile (image) vs. chip (file) presentation. */
  kind: 'image' | 'file'
  /** Display name (usually the original filename). */
  name: string
  /** Size in bytes when known. */
  size?: number
  /** MIME type when known. */
  media_type?: string
  /** base64 payload for images (ChatImage fast-path, no `data:` prefix). */
  data?: string
  /** Host URL once uploaded (upload adapters). */
  url?: string
  /** Upload progress in the range 0–1. */
  progress?: number
  /** Failure message; when set the attachment is in the error state. */
  error?: string
}
```

### `PendingAttachmentItem`

<!-- jcode-ui-core-pendingattachmentitem -->

`interface` · `packages/jcode-ui-core/src/primitives/Composer.tsx`

A pending-attachment row surfaced to `renderPendingAttachments`.

```ts
export interface PendingAttachmentItem {
  /** Latest adapter snapshot (or the provisional entry while uploading). */
  attachment: PendingAttachment
  status: PendingStatus
  /** Remove this attachment (also invokes `adapter.remove` when present). */
  remove: () => void
  /** Re-run `adapter.add` for this file (error recovery). */
  retry: () => void
}
```

### `QueuedMessage`

<!-- jcode-ui-core-queuedmessage -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A message composed while the agent is running; drained turn-by-turn.

```ts
export interface QueuedMessage {
  id: string
  text: string
  images?: ChatImage[]
}
```

### `RuntimeActions`

<!-- jcode-ui-core-runtimeactions -->

`interface` · `packages/jcode-ui-core/src/runtime/index.ts`

The conversation timeline (messages, tool calls, approvals, interleaved). */
  items: ThreadItem[]
  /** True while the agent is producing output (drives the "Thinking…" row, the
 composer's send→stop button swap, and auto-follow behavior). */
  isRunning: boolean
  /** Live token/context snapshot, or null when no turn has reported usage yet. */
  tokenSnapshot: TokenSnapshot | null
  /** Active goal banner, or null. */
  goal: Goal | null
  /** Todo list (also rendered inside `todowrite` tool calls). */
  todos: TodoItem[]
  /** Type-ahead queue: messages composed mid-turn, drained on each turn end. */
  queued: QueuedMessage[]
  /** Transport liveness (drives ConnectionBanner). Defaults to 'connected'. */
  connection: ConnectionState
}

/**
Actions the UI dispatches. The runtime forwards these to the host store
(which may in turn POST to a backend, resolve locally, etc.). Keeping these
as a flat bag of callbacks (rather than a discriminated union dispatched via
`runtime.dispatch`) means consumers wiring a Zustand store or plain React
state don't have to model their actions in our shape — they just hand us the
functions they already have.

```ts
export interface RuntimeActions {
  /** Send a user-authored message. `images` are base64 payloads. */
  sendMessage: (text: string, images?: { data: string; media_type: string }[]) => void
  /** Enqueue a message while a turn is running (type-ahead). */
  enqueueMessage: (text: string, images?: { data: string; media_type: string }[]) => void
  /** Remove a queued message by id (before it is sent). */
  removeQueuedMessage: (id: string) => void
  /** Cancel the in-flight turn. */
  stop: () => void
  /** Resolve an approval gate. `approveAll` arms "allow all future" semantics. */
  resolveApproval: (id: string, approved: boolean, approveAll?: boolean) => void
  /** Answer an `ask_user` batch. */
  submitAskUser: (id: string, answers: { question_header: string; answer: string; selected?: string[] }[]) => void
  /** Edit a past user message and resend from that point. */
  editMessage: (id: string, newText: string) => void

  // ── Optional capabilities ─────────────────────────────────────────────
  // Fail-visible convention: when a host omits one of these, the UI control
  // that would dispatch it is NOT rendered (never a dead button).

  /** Resolve an approval that carries host-defined `options` (e.g. ACP
   *  permission_request with arbitrary option ids). */
  resolveApprovalOption?: (id: string, optionId: string) => void
  /** Regenerate an assistant message; the host appends a new version. */
  regenerate?: (messageId: string) => void
  /** Switch the visible version of a branched message. */
  switchVersion?: (messageId: string, versionId: string) => void
  /** Record 👍/👎 feedback on an assistant message. */
  submitFeedback?: (messageId: string, rating: 'up' | 'down', comment?: string) => void
  /** Retry a failed assistant turn (system error follow-up). */
  retryMessage?: (messageId: string) => void
}
```

### `RuntimeState`

<!-- jcode-ui-core-runtimestate -->

`interface` · `packages/jcode-ui-core/src/runtime/index.ts`

ChatRuntime — the host-agnostic data source for jcode-ui.

Components never touch the store directly. They talk to a `ChatRuntime`, which
is an `ExternalStore`-shaped interface (matching Redux's store signature) so
it can wrap RTK, Zustand, Pinia-via-snapshot, or a hand-rolled reducer with
zero adapter code.

Why this abstraction exists: jcode-ui must render the same whether the data
comes from a live WebSocket-backed RTK store, a replayed JSONL session, or a
mock playground. The runtime is the single seam.
/

import type {
  ThreadItem,
  TokenSnapshot,
  Goal,
  TodoItem,
  QueuedMessage,
  ConnectionState,
} from '../types/index.js'

/**
The read-side state a Thread + Composer render from. Consumers provide an
object of this shape (or a subset — see `PartialRuntimeState`); the runtime
normalizes missing slices to safe defaults.

```ts
export interface RuntimeState {
  /** The conversation timeline (messages, tool calls, approvals, interleaved). */
  items: ThreadItem[]
  /** True while the agent is producing output (drives the "Thinking…" row, the
   *  composer's send→stop button swap, and auto-follow behavior). */
  isRunning: boolean
  /** Live token/context snapshot, or null when no turn has reported usage yet. */
  tokenSnapshot: TokenSnapshot | null
  /** Active goal banner, or null. */
  goal: Goal | null
  /** Todo list (also rendered inside `todowrite` tool calls). */
  todos: TodoItem[]
  /** Type-ahead queue: messages composed mid-turn, drained on each turn end. */
  queued: QueuedMessage[]
  /** Transport liveness (drives ConnectionBanner). Defaults to 'connected'. */
  connection: ConnectionState
}
```

### `SlashMenuState`

<!-- jcode-ui-core-slashmenustate -->

`interface` · `packages/jcode-ui-core/src/primitives/Composer.tsx`

```ts
export interface SlashMenuState {
  open: boolean
  /** Filtered commands for the current input. */
  commands: SlashCommand[]
  /** Active (highlighted) index, or -1. */
  activeIndex: number
  /** Apply a command: inserts its slash text at the caret. */
  apply: (cmd: SlashCommand) => void
}
```

### `TaskContextBreakdown`

<!-- jcode-ui-core-taskcontextbreakdown -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

Per-task context-window breakdown (host-provided; powers the ContextBar popover).

```ts
export interface TaskContextBreakdown {
  context_limit: number
  system_prompt_tokens: number
  system_tools_tokens: number
  mcp_tools_tokens: number
  skills_tokens: number
  messages_tokens: number
}
```

### `ThreadListState`

<!-- jcode-ui-core-threadliststate -->

`interface` · `packages/jcode-ui-core/src/threads/store.ts`

Stable id (used for React keys, selection, and action targeting). */
  id: string
  /** Display title. Hosts may lazily fill this from the first user message. */
  title: string
  /** Last-activity timestamp (ms epoch). Drives relative-time + default sort. */
  updatedAt: number
  /** Live status. `running` renders a pulsing status dot; default is idle. */
  status?: 'idle' | 'running'
  /** Soft-hidden: rendered under the collapsible "Archived" group. */
  archived?: boolean
  /** Free-form host metadata (project id, trigger kind, avatar tint, …). */
  meta?: Record<string, unknown>
}

/** The read-side state the `ThreadList` renders from.

```ts
export interface ThreadListState {
  /** All threads (active + archived). The UI splits/sorts them for display. */
  threads: ThreadSummary[]
  /** The currently-open thread id, or undefined when none is selected. */
  activeId?: string
  /** True while the initial list is loading (drives a skeleton/spinner). */
  loading?: boolean
}
```

### `ThreadProps`

<!-- jcode-ui-core-threadprops -->

`interface` · `packages/jcode-ui-core/src/primitives/Thread.tsx`

```ts
export interface ThreadProps extends ThreadRenderSlots {
  /** Enable windowed virtualization. Default true. Disable for short/replay
   *  timelines. */
  virtualize?: boolean
  /** Estimated row height in px (virtualizer uses this before measure). */
  estimateSize?: number
  /** Bottom-edge threshold in px within which we consider the user "at bottom". */
  scrollThreshold?: number
  /** Extra padding after the last item (e.g. to clear a sticky composer). */
  overscanBottom?: number
  /** className passthrough for the scroll container. */
  className?: string
  /** Accessibility role for the scroll container. */
  role?: string
  /** Ref callback for the scroll container (for parent scroll control). */
  containerRef?: (el: HTMLElement | null) => void
  /**
   * Optional pure transform applied to runtime items before render (e.g.
   * exploring-group coalescing). Must not mutate the input array.
   */
  mapItems?: (items: ThreadItem[]) => ThreadItem[]
}
```

### `ThreadRenderSlots`

<!-- jcode-ui-core-threadrenderslots -->

`interface` · `packages/jcode-ui-core/src/primitives/Thread.tsx`

```ts
export interface ThreadRenderSlots {
  /** Render a single timeline item. Dispatch on `item.kind` to pick a sub-view. */
  renderItem: (item: ThreadItem) => ReactNode
  /** Optional trailing "agent is working" row shown when isRunning. */
  renderPending?: () => ReactNode
  /** Optional empty state (no items at all). */
  renderEmpty?: () => ReactNode
  /** Optional footer after the last item when idle (e.g. follow-up
   *  suggestions). Not rendered while running or when the thread is empty. */
  renderFooter?: () => ReactNode
}
```

### `ThreadStore`

<!-- jcode-ui-core-threadstore -->

`interface` · `packages/jcode-ui-core/src/threads/store.ts`

Open/select a thread by id. */
  select?: (id: string) => void
  /** Create a new thread (host decides id + initial title). */
  create?: () => void
  /** Rename a thread. */
  rename?: (id: string, title: string) => void
  /** Archive (soft-hide) a thread. */
  archive?: (id: string) => void
  /** Permanently remove a thread. */
  remove?: (id: string) => void
}

/**
The contract every thread-list data source implements. `getState`/`subscribe`
match the Redux `Store` signature so an RTK store wraps with a thin selector.

Stability contract: `getState()` MUST return a stable reference between
changes (React's `useSyncExternalStore` loops otherwise). The mock honors
this by only replacing the state object on an actual mutation.

```ts
export interface ThreadStore {
  getState: () => ThreadListState
  subscribe: (listener: () => void) => () => void
  /** Action bag. Stable identity recommended (consumers may memoize). */
  readonly actions: ThreadStoreActions
}
```

### `ThreadStoreActions`

<!-- jcode-ui-core-threadstoreactions -->

`interface` · `packages/jcode-ui-core/src/threads/store.ts`

All threads (active + archived). The UI splits/sorts them for display. */
  threads: ThreadSummary[]
  /** The currently-open thread id, or undefined when none is selected. */
  activeId?: string
  /** True while the initial list is loading (drives a skeleton/spinner). */
  loading?: boolean
}

/**
Actions the `ThreadList` dispatches. All optional — the host wires only what
it supports, and the UI hides controls for the rest (fail-visible). Keeping
these a flat bag of callbacks (rather than a dispatched union) means a host
can hand us the functions it already has with zero adapter code.

```ts
export interface ThreadStoreActions {
  /** Open/select a thread by id. */
  select?: (id: string) => void
  /** Create a new thread (host decides id + initial title). */
  create?: () => void
  /** Rename a thread. */
  rename?: (id: string, title: string) => void
  /** Archive (soft-hide) a thread. */
  archive?: (id: string) => void
  /** Permanently remove a thread. */
  remove?: (id: string) => void
}
```

### `ThreadSummary`

<!-- jcode-ui-core-threadsummary -->

`interface` · `packages/jcode-ui-core/src/threads/store.ts`

ThreadStore — the host-agnostic data source for a session/thread *list*.

This is the sidebar analog of `ChatRuntime`: where `ChatRuntime` drives a
single conversation, `ThreadStore` drives the collection of conversations
(jcode calls them sessions; the cloud console calls them runs). Both hosts
implement the same `getState`/`subscribe`/`actions` shape so the styled
`ThreadList` renders identically over Redux, Zustand, or a hand-rolled store.

Fail-visible actions: every action is optional. A host that can't (or won't)
support renaming simply omits `actions.rename`, and the UI renders no rename
control — mirroring how `Message` only shows the edit affordance when
`canEdit` is set. Never call an action without guarding its presence.
/

/** A single row in the thread list — a lightweight summary, not the full convo.

```ts
export interface ThreadSummary {
  /** Stable id (used for React keys, selection, and action targeting). */
  id: string
  /** Display title. Hosts may lazily fill this from the first user message. */
  title: string
  /** Last-activity timestamp (ms epoch). Drives relative-time + default sort. */
  updatedAt: number
  /** Live status. `running` renders a pulsing status dot; default is idle. */
  status?: 'idle' | 'running'
  /** Soft-hidden: rendered under the collapsible "Archived" group. */
  archived?: boolean
  /** Free-form host metadata (project id, trigger kind, avatar tint, …). */
  meta?: Record<string, unknown>
}
```

### `TodoItem`

<!-- jcode-ui-core-todoitem -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A todo/goal tracking item. (id is a number — matches the Go backend.)

```ts
export interface TodoItem {
  id: number
  title: string
  status: 'pending' | 'in_progress' | 'completed' | 'cancelled'
}
```

### `TokenSnapshot`

<!-- jcode-ui-core-tokensnapshot -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

Live token/context usage snapshot for a turn.

```ts
export interface TokenSnapshot {
  total_tokens: number
  prompt_tokens: number
  completion_tokens: number
  cached_tokens?: number
  reasoning_tokens?: number
  cache_write_tokens?: number
  call_count?: number
  cache_hit_rate?: number
  cache_supported?: boolean
  model_context_limit: number
}
```

### `ToolCall`

<!-- jcode-ui-core-toolcall -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A tool invocation. `args`/`output` are raw JSON strings; renderers parse
them. `children` carries subagent-nested calls (rendered recursively).

```ts
export interface ToolCall {
  id: string
  /** Backend tool_call_id for precise result matching. */
  toolCallID?: string
  name: string
  args: string
  output?: string
  /** Clean output for UI display (metadata stripped). */
  displayOutput?: string
  error?: string
  status: ToolStatus
  timestamp: number
  displayInfo?: ToolDisplayInfo
  /** Nested tool calls (subagent inner calls). */
  children?: ToolCall[]
  /** ask_user: request id while awaiting an answer (live runs only). */
  askUserId?: string
  /** ask_user: backend-normalized questions to render. */
  askUserQuestions?: AskUserQuestion[]
  /** Dual-channel streams (execute). */
  streams?: ToolStreams
  /** Dual-channel meta (execute). */
  meta?: ToolMeta
  /** Dual-channel presentation (execute). */
  presentation?: ToolPresentation
}
```

### `ToolCallContextValue`

<!-- jcode-ui-core-toolcallcontextvalue -->

`interface` · `packages/jcode-ui-core/src/primitives/ToolCallView.tsx`

ToolCallView — the headless expand/collapse shell for a tool invocation.

Owns: expand/collapse state, renderer lookup via ToolRendererRegistry, and
subagent recursion. Does NOT own per-tool body chrome — the styled
`jcode-ui` `ToolCallCard` supplies header styling + CSS for `.toolcall-body`.

Subagent: only `tool.name === 'subagent'` (NOT team_spawn — that has its own
renderer). Children recurse as nested ToolCallView instances. ask_user tools
route to the host's renderAskUser slot.
/

import { createContext, useContext, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import type { ToolCall } from '../types/index.js'
import type { ToolRendererRegistry, ToolRendererProps } from '../adapters/index.js'

/** Context the host provides to wire the registry + the subagent/askuser slots.

```ts
export interface ToolCallContextValue {
  registry: ToolRendererRegistry
  /** Render nested subagent children. Default: recurses into ToolCallView. */
  renderChild?: (child: ToolCall, depth: number) => ReactNode
  /** Render an ask_user tool (interactive question block). */
  renderAskUser?: (tool: ToolCall) => ReactNode
}
```

### `ToolCallViewProps`

<!-- jcode-ui-core-toolcallviewprops -->

`interface` · `packages/jcode-ui-core/src/primitives/ToolCallView.tsx`

```ts
export interface ToolCallViewProps {
  tool: ToolCall
  /** Nesting depth (subagent children). 0 = top-level. */
  depth?: number
  /** Max subagent recursion depth. Default 4. */
  maxDepth?: number
  /** Default expanded state. Default false (subagents default true). */
  defaultExpanded?: boolean
  /** Render-prop for the header. Falls back to a default row. */
  renderHeader?: (tool: ToolCall, expanded: boolean, toggle: () => void) => ReactNode
  /**
   * Optional subagent body (output/error). Styled layer supplies markdown.
   * Receives only output-related fields — never args.
   */
  renderSubagentOutput?: (tool: ToolCall) => ReactNode
  /**
   * Optional custom renderer for the full children list (e.g. compact rows +
   * exploring groups). When set, overrides per-child renderChild recursion.
   */
  renderSubagentChildren?: (children: ToolCall[]) => ReactNode
  /** className passthrough. */
  className?: string
}
```

### `ToolDisplayInfo`

<!-- jcode-ui-core-tooldisplayinfo -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

Stable id for keying. */
  id: string
  /** Display title of the source. */
  title: string
  /** Optional URL or deep link. */
  url?: string
  /** Optional snippet/excerpt quoted from the source. */
  snippet?: string
}

/** Display metadata for a tool call, surfaced from the backend or extracted
 client-side from args. Lets renderers show a title/icon without parsing args.

```ts
export interface ToolDisplayInfo {
  title: string
  subtitle?: string
  icon?: string
  /** 'context' | 'mutation' | 'execution' — informational grouping. */
  category?: string
  /** Presentation kind: read | search | list | shell | edit | agent | other. */
  kind?: string
  /** When true, adjacent tools may coalesce into an Exploring group. */
  collapsible?: boolean
}
```

### `ToolMeta`

<!-- jcode-ui-core-toolmeta -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

Structured execution metadata for execute-style tools.

```ts
export interface ToolMeta {
  exit_code?: number
  duration_ms?: number
  timed_out?: boolean
  truncated?: boolean
  spill_path?: string
}
```

### `ToolPresentation`

<!-- jcode-ui-core-toolpresentation -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

Presentation hints attached to a tool result.

```ts
export interface ToolPresentation {
  kind?: string
  title?: string
  subtitle?: string
  collapsible?: boolean
}
```

### `ToolRendererProps`

<!-- jcode-ui-core-toolrendererprops -->

`interface` · `packages/jcode-ui-core/src/adapters/index.ts`

Tool renderer registry — the plugin seam for tool-call visualization.

`ToolCallCard` doesn't know how to render any specific tool. Instead it looks
up a renderer by `tool.name` in a `ToolRendererRegistry`. jcode-ui ships
default renderers (terminal/file-viewer/diff/search/…) as a preset; consumers
override or extend with their own. This is what makes the component reusable
across agents with completely different tool surfaces.
/

import type { ComponentType } from 'react'
import type { ToolCall, ToolDisplayInfo, ToolStatus } from '../types/index.js'

export type { ToolStatus }

/** Props every tool renderer receives.

```ts
export interface ToolRendererProps {
  /** Logical tool name (e.g. 'execute', 'read', 'edit', 'grep', …). */
  name: string
  /** Raw args JSON string. Renderers parse what they need. */
  args: string
  /** Raw output string (may be omitted while running). */
  output?: string
  /** Clean display output (backend metadata stripped). */
  displayOutput?: string
  /** Error string if the tool failed. */
  error?: string
  status: ToolStatus
  /** Pre-extracted display metadata (title/subtitle/icon). May be absent. */
  displayInfo?: ToolDisplayInfo
  /** Nested subagent calls — renderers decide whether to recurse. */
  children?: ToolCall[]
  /** Dual-channel streams (execute). */
  streams?: ToolCall['streams']
  /** Dual-channel meta (execute). */
  meta?: ToolCall['meta']
  /** Dual-channel presentation (execute). */
  presentation?: ToolCall['presentation']
}
```

### `ToolStreams`

<!-- jcode-ui-core-toolstreams -->

`interface` · `packages/jcode-ui-core/src/types/index.ts`

'context' | 'mutation' | 'execution' — informational grouping. */
  category?: string
  /** Presentation kind: read | search | list | shell | edit | agent | other. */
  kind?: string
  /** When true, adjacent tools may coalesce into an Exploring group. */
  collapsible?: boolean
}

export type ToolStatus = 'running' | 'done' | 'error'

/** Structured stdout/stderr for execute-style tools (dual-channel UI path).

```ts
export interface ToolStreams {
  stdout?: string
  stderr?: string
  aggregated?: string
}
```

### `AGUIEvent`

<!-- jcode-ui-core-aguievent -->

`type` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

Fields shared by every event. */
interface AGUIBaseEvent {
  type: string
  timestamp?: number
  rawEvent?: unknown
}

// Lifecycle -----------------------------------------------------------------
export interface RunStartedEvent extends AGUIBaseEvent {
  type: 'RUN_STARTED'
  threadId?: string
  runId?: string
}
export interface RunFinishedEvent extends AGUIBaseEvent {
  type: 'RUN_FINISHED'
  result?: unknown
}
export interface RunErrorEvent extends AGUIBaseEvent {
  type: 'RUN_ERROR'
  message: string
  code?: string
}
export interface StepStartedEvent extends AGUIBaseEvent {
  type: 'STEP_STARTED'
  stepName: string
}
export interface StepFinishedEvent extends AGUIBaseEvent {
  type: 'STEP_FINISHED'
  stepName: string
}

// Text messages -------------------------------------------------------------
export interface TextMessageStartEvent extends AGUIBaseEvent {
  type: 'TEXT_MESSAGE_START'
  messageId: string
  role?: string
}
export interface TextMessageContentEvent extends AGUIBaseEvent {
  type: 'TEXT_MESSAGE_CONTENT'
  messageId: string
  delta: string
}
export interface TextMessageEndEvent extends AGUIBaseEvent {
  type: 'TEXT_MESSAGE_END'
  messageId: string
}
export interface TextMessageChunkEvent extends AGUIBaseEvent {
  type: 'TEXT_MESSAGE_CHUNK'
  messageId?: string
  role?: string
  delta?: string
}

// Tool calls ----------------------------------------------------------------
export interface ToolCallStartEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_START'
  toolCallId: string
  toolCallName: string
  parentMessageId?: string
}
export interface ToolCallArgsEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_ARGS'
  toolCallId: string
  delta: string
}
export interface ToolCallEndEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_END'
  toolCallId: string
}
export interface ToolCallResultEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_RESULT'
  messageId?: string
  toolCallId: string
  content: unknown
  role?: string
}
export interface ToolCallChunkEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_CHUNK'
  toolCallId?: string
  toolCallName?: string
  parentMessageId?: string
  delta?: string
}

// State ---------------------------------------------------------------------
export interface StateSnapshotEvent extends AGUIBaseEvent {
  type: 'STATE_SNAPSHOT'
  snapshot: unknown
}
export interface StateDeltaEvent extends AGUIBaseEvent {
  type: 'STATE_DELTA'
  delta: AGUIPatchOp[]
}
export interface MessagesSnapshotEvent extends AGUIBaseEvent {
  type: 'MESSAGES_SNAPSHOT'
  messages: AGUIMessage[]
}

// Reasoning (supersedes THINKING_*) -----------------------------------------
export interface ReasoningMessageContentEvent extends AGUIBaseEvent {
  type: 'REASONING_MESSAGE_CONTENT'
  messageId?: string
  delta: string
}
export interface ReasoningMessageChunkEvent extends AGUIBaseEvent {
  type: 'REASONING_MESSAGE_CHUNK'
  messageId?: string
  delta?: string
}

// Passthrough ---------------------------------------------------------------
export interface CustomEvent extends AGUIBaseEvent {
  type: 'CUSTOM'
  name: string
  value: unknown
}
export interface RawEvent extends AGUIBaseEvent {
  type: 'RAW'
  event: unknown
  source?: string
}

/**
The discriminated union the reducer switches on. Unmodelled `type`s (STEP_*,
REASONING lifecycle markers, ACTIVITY_*, deprecated THINKING_*) still arrive
as objects at runtime and fall through to the reducer's default branch.

```ts
export type AGUIEvent =
  | RunStartedEvent
  | RunFinishedEvent
  | RunErrorEvent
  | StepStartedEvent
  | StepFinishedEvent
  | TextMessageStartEvent
  | TextMessageContentEvent
  | TextMessageEndEvent
  | TextMessageChunkEvent
  | ToolCallStartEvent
  | ToolCallArgsEvent
  | ToolCallEndEvent
  | ToolCallResultEvent
  | ToolCallChunkEvent
  | StateSnapshotEvent
  | StateDeltaEvent
  | MessagesSnapshotEvent
  | ReasoningMessageContentEvent
  | ReasoningMessageChunkEvent
  | CustomEvent
  | RawEvent;
```

### `AGUIRole`

<!-- jcode-ui-core-aguirole -->

`type` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

AG-UI role space. Not all map onto jcode's `Role` (see `toRole`).

```ts
export type AGUIRole = 'developer' | 'system' | 'assistant' | 'user' | 'tool';
```

### `AGUITransport`

<!-- jcode-ui-core-aguitransport -->

`type` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

A pluggable event source. The default (`createFetchTransport`) POSTs the run
input and streams the SSE response; tests inject a scripted async iterable.

```ts
export type AGUITransport = (
  input: AGUIRunInput,
  signal: AbortSignal,
) => AsyncIterable<AGUIEvent>;
```

### `ApprovalOptionKind`

<!-- jcode-ui-core-approvaloptionkind -->

`type` · `packages/jcode-ui-core/src/types/index.ts`

single-select label or free text. */
  answer: string
  /** multi-select labels. */
  selected?: string[]
}

/** Button treatment for a host-defined approval option. `allow_always` keeps
 the two-step arming UX; `custom` renders as a neutral choice.

```ts
export type ApprovalOptionKind = 'allow_once' | 'allow_always' | 'deny' | 'custom';
```

### `ConnectionState`

<!-- jcode-ui-core-connectionstate -->

`type` · `packages/jcode-ui-core/src/types/index.ts`

Origin channel for inbound messages (e.g. 'wechat'). Drives avatar tint. */
  source?: string
  images?: ChatImage[]
  /** system-message severity. */
  level?: SystemLevel
  /** Optional raw detail (collapsed by default). */
  detail?: string
  /** Assistant turn elapsed (ms), stamped on the final message of a turn. */
  durationMs?: number
  /** Optional model reasoning / chain-of-thought text (rendered collapsible).
 Mirrors assistant-ui's Reasoning component + OpenAI/Anthropic thinking. */
  reasoning?: string
  /** Optional citation sources for the message (rendered as a Sources list).
 Mirrors assistant-ui's Sources component. */
  sources?: MessageSource[]
  /** Alternate versions (edit/regenerate branches). `content` mirrors the
 active version; absent for unbranched messages. */
  versions?: MessageVersion[]
  /** Which entry of `versions` is showing. */
  activeVersionId?: string
  /** Recorded 👍/👎 feedback, when the host persists it. */
  feedback?: 'up' | 'down'
}

/** Transport liveness surfaced by the runtime (drives ConnectionBanner).

```ts
export type ConnectionState = 'connected' | 'reconnecting' | 'disconnected';
```

### `GoalStatus`

<!-- jcode-ui-core-goalstatus -->

`type` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export type GoalStatus = 'active' | 'complete' | 'blocked';
```

### `MockThreadStoreSeed`

<!-- jcode-ui-core-mockthreadstoreseed -->

`type` · `packages/jcode-ui-core/src/threads/store.ts`

Action bag. Stable identity recommended (consumers may memoize). */
  readonly actions: ThreadStoreActions
}

/** Seed for `createMockThreadStore` — an array of threads, or a partial state.

```ts
export type MockThreadStoreSeed = ThreadSummary[] | Partial<ThreadListState>;
```

### `PartialRuntimeState`

<!-- jcode-ui-core-partialruntimestate -->

`type` · `packages/jcode-ui-core/src/runtime/index.ts`

Action bag. Stable identity is recommended (consumers should memoize). */
  readonly actions: RuntimeActions
}

/** A `RuntimeState` where every slice is optional; useful for adapters that
 only implement part of the contract (e.g. a read-only replay runtime).

```ts
export type PartialRuntimeState = Partial<RuntimeState>;
```

### `PendingStatus`

<!-- jcode-ui-core-pendingstatus -->

`type` · `packages/jcode-ui-core/src/primitives/Composer.tsx`

Composer — the headless message composer.

Owns: textarea state, autosize, IME-safe key handling, send/queue/stop
dispatch, a slash-command palette skeleton, and (Composer 2) a pluggable
attachment pipeline, drag/paste ingestion, control slots, dictation, and an
imperative `ComposerHandle`. Does NOT own styling or the model/mode/workspace
pickers (those are app-specific — the styled jcode-ui `ChatInput` composes
this primitive and layers them on).

Streaming interaction: when the runtime reports `isRunning`, the send button
becomes a stop button, and `send()` routes to `enqueueMessage` instead of
`sendMessage` (type-ahead). The runtime drains the queue on each turn end.

Attachments: with no `attachmentAdapter` the legacy `allowImages` base64 path
is used (unchanged). With an adapter, every picked/pasted/dropped file flows
through `adapter.add`, tracked in a pending state machine (uploading →
done/error). On send, completed image attachments still ride the ChatImage
`images` argument (so `sendMessage` stays compatible) and the full completed
set is also handed to `onSendAttachments`.
/

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
} from 'react'
import type { ClipboardEvent, DragEvent, KeyboardEvent, ReactNode } from 'react'
import { useRuntimeActions, useRuntimeState } from '../runtime/context.js'
import type { ChatImage } from '../types/index.js'
import { nextAttachmentId } from './attachmentAdapter.js'
import type { AttachmentAdapter, PendingAttachment } from './attachmentAdapter.js'

export interface SlashCommand {
  /** The literal text inserted when chosen (e.g. '/goal'). */
  slash: string
  description?: string
}

/** Lifecycle status of a pending attachment slot.

```ts
export type PendingStatus = 'uploading' | 'done' | 'error';
```

### `Role`

<!-- jcode-ui-core-role -->

`type` · `packages/jcode-ui-core/src/types/index.ts`

Core message + tool types for jcode-ui.

These mirror the jcode backend contract (see `web/src/types/api.ts`) but are
framework-agnostic and the single source of truth for both `jcode-ui-core`
(headless primitives) and `jcode-ui` (styled components).
/

/** Who authored a message.

```ts
export type Role = 'user' | 'assistant' | 'system';
```

### `SystemLevel`

<!-- jcode-ui-core-systemlevel -->

`type` · `packages/jcode-ui-core/src/types/index.ts`

Original filename when known (file picker / drag-drop). */
  name?: string
}

/** Severity for `system` messages. Undefined → default neutral styling.

```ts
export type SystemLevel = 'error' | 'notice';
```

### `ThreadItem`

<!-- jcode-ui-core-threaditem -->

`type` · `packages/jcode-ui-core/src/types/index.ts`

The discriminated union rendered by `Thread`. A `seq` counter keeps DOM
identity stable across streaming updates and is used as the virtualizer key.

```ts
export type ThreadItem =
  | { kind: 'message'; data: Message; seq: number }
  | { kind: 'tool'; data: ToolCall; seq: number }
  | { kind: 'approval'; data: Approval; seq: number }
  | { kind: 'exploring'; data: ExploringGroup; seq: number };
```

### `ThreadItemKind`

<!-- jcode-ui-core-threaditemkind -->

`type` · `packages/jcode-ui-core/src/types/index.ts`

Target outside the workspace root — UI flags it prominently. */
  is_external: boolean
  resolved?: boolean
  approved?: boolean
  /** True while a resolve request is in flight (disables controls). */
  resolving?: boolean
  /** Host-defined decision options; absent → classic boolean controls. */
  options?: ApprovalOption[]
  /** The chosen option id once resolved (options mode). */
  resolvedOptionId?: string
}

/** Built-in thread-item kinds (exploring is UI-only coalescing).

```ts
export type ThreadItemKind = 'message' | 'tool' | 'approval' | 'exploring';
```

### `ToolRenderer`

<!-- jcode-ui-core-toolrenderer -->

`type` · `packages/jcode-ui-core/src/adapters/index.ts`

Logical tool name (e.g. 'execute', 'read', 'edit', 'grep', …). */
  name: string
  /** Raw args JSON string. Renderers parse what they need. */
  args: string
  /** Raw output string (may be omitted while running). */
  output?: string
  /** Clean display output (backend metadata stripped). */
  displayOutput?: string
  /** Error string if the tool failed. */
  error?: string
  status: ToolStatus
  /** Pre-extracted display metadata (title/subtitle/icon). May be absent. */
  displayInfo?: ToolDisplayInfo
  /** Nested subagent calls — renderers decide whether to recurse. */
  children?: ToolCall[]
  /** Dual-channel streams (execute). */
  streams?: ToolCall['streams']
  /** Dual-channel meta (execute). */
  meta?: ToolCall['meta']
  /** Dual-channel presentation (execute). */
  presentation?: ToolCall['presentation']
}

/** A tool renderer is just a React component.

```ts
export type ToolRenderer = ComponentType<ToolRendererProps>;
```

