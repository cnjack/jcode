---
title: Generated API
parent: API Reference
nav_order: 6
---

# Generated API

> Auto-generated from TypeScript sources on **2026-08-26**.
> Do not edit by hand — run `node script/generate_jcode_ui_api_docs.mjs`.
>
> Human-written guides: [Types](/chat-ui/docs/api/types) · [Runtime](/chat-ui/docs/api/runtime) · [Hooks](/chat-ui/docs/api/hooks) · [Primitives](/chat-ui/docs/api/primitives) · [Components](/chat-ui/docs/api/components).

**364** public symbols extracted.

## `jcode-ui`

| Symbol | Kind | Source |
|--------|------|--------|
| [`ActivityGroupCard`](#jcode-ui-activitygroupcard) | const | `packages/jcode-ui/src/components/ActivityGroupCard.tsx` |
| [`ApprovalBanner`](#jcode-ui-approvalbanner) | const | `packages/jcode-ui/src/components/ApprovalBanner.tsx` |
| [`Artifact`](#jcode-ui-artifact) | const | `packages/jcode-ui/src/components/Artifact.tsx` |
| [`AskUserCard`](#jcode-ui-askusercard) | const | `packages/jcode-ui/src/components/AskUserCard.tsx` |
| [`Attachment`](#jcode-ui-attachment) | const | `packages/jcode-ui/src/components/Attachment.tsx` |
| [`AttachmentList`](#jcode-ui-attachmentlist) | const | `packages/jcode-ui/src/components/Attachment.tsx` |
| [`AudioPlayer`](#jcode-ui-audioplayer) | const | `packages/jcode-ui/src/voice/AudioPlayer.tsx` |
| [`BranchPicker` (const)](#jcode-ui-branchpicker-const) | const | `packages/jcode-ui/src/components/BranchPicker.tsx` |
| [`BrowserShotRenderer`](#jcode-ui-browsershotrenderer) | const | `packages/jcode-ui/src/toolRenderers/browserShot.tsx` |
| [`CanvasControls`](#jcode-ui-canvascontrols) | const | `packages/jcode-ui/src/canvas/CanvasControls.tsx` |
| [`CanvasPanel`](#jcode-ui-canvaspanel) | const | `packages/jcode-ui/src/canvas/CanvasPanel.tsx` |
| [`ChatInput` (const)](#jcode-ui-chatinput-const) | const | `packages/jcode-ui/src/components/ChatInput.tsx` |
| [`CompactToolRow`](#jcode-ui-compacttoolrow) | const | `packages/jcode-ui/src/components/CompactToolRow.tsx` |
| [`CompletedTurnCard`](#jcode-ui-completedturncard) | const | `packages/jcode-ui/src/components/CompletedTurnCard.tsx` |
| [`ComputerActRenderer`](#jcode-ui-computeractrenderer) | const | `packages/jcode-ui/src/toolRenderers/computerAct.tsx` |
| [`ComputerShotRenderer`](#jcode-ui-computershotrenderer) | const | `packages/jcode-ui/src/toolRenderers/computerShot.tsx` |
| [`ConnectionBanner`](#jcode-ui-connectionbanner) | const | `packages/jcode-ui/src/components/ConnectionBanner.tsx` |
| [`ContextBar`](#jcode-ui-contextbar) | const | `packages/jcode-ui/src/components/ContextBar.tsx` |
| [`DiffRenderer`](#jcode-ui-diffrenderer) | const | `packages/jcode-ui/src/toolRenderers/diff.tsx` |
| [`ExploringGroupCard`](#jcode-ui-exploringgroupcard) | const | `packages/jcode-ui/src/components/ExploringGroupCard.tsx` |
| [`ExportButton`](#jcode-ui-exportbutton) | const | `packages/jcode-ui/src/components/ExportButton.tsx` |
| [`FileTreeRenderer`](#jcode-ui-filetreerenderer) | const | `packages/jcode-ui/src/toolRenderers/fileTree.tsx` |
| [`FileViewerRenderer`](#jcode-ui-fileviewerrenderer) | const | `packages/jcode-ui/src/toolRenderers/fileViewer.tsx` |
| [`GeneratedImageCard`](#jcode-ui-generatedimagecard) | const | `packages/jcode-ui/src/components/GeneratedImageCard.tsx` |
| [`GenericRenderer`](#jcode-ui-genericrenderer) | const | `packages/jcode-ui/src/toolRenderers/generic.tsx` |
| [`Message` (jcode-ui)](#jcode-ui-message) | const | `packages/jcode-ui/src/components/Message.tsx` |
| [`PendingAttachmentList`](#jcode-ui-pendingattachmentlist) | const | `packages/jcode-ui/src/components/Attachment.tsx` |
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
| [`TerminalRenderer`](#jcode-ui-terminalrenderer) | const | `packages/jcode-ui/src/toolRenderers/terminal.tsx` |
| [`TestResultsRenderer`](#jcode-ui-testresultsrenderer) | const | `packages/jcode-ui/src/toolRenderers/testResults.tsx` |
| [`ThreadList`](#jcode-ui-threadlist) | const | `packages/jcode-ui/src/components/ThreadList.tsx` |
| [`ThreadWelcome`](#jcode-ui-threadwelcome) | const | `packages/jcode-ui/src/components/ThreadWelcome.tsx` |
| [`TodoRenderer`](#jcode-ui-todorenderer) | const | `packages/jcode-ui/src/toolRenderers/todo.tsx` |
| [`ToolBatchGroupCard`](#jcode-ui-toolbatchgroupcard) | const | `packages/jcode-ui/src/components/ToolBatchGroup.tsx` |
| [`ToolCallCard`](#jcode-ui-toolcallcard) | const | `packages/jcode-ui/src/components/ToolCallCard.tsx` |
| [`Transcription`](#jcode-ui-transcription) | const | `packages/jcode-ui/src/voice/Transcription.tsx` |
| [`TurnChangesCard`](#jcode-ui-turnchangescard) | const | `packages/jcode-ui/src/components/TurnChangesCard.tsx` |
| [`VoiceVisualizer`](#jcode-ui-voicevisualizer) | const | `packages/jcode-ui/src/voice/VoiceVisualizer.tsx` |
| [`WorkflowAnimatedEdge`](#jcode-ui-workflowanimatededge) | const | `packages/jcode-ui/src/canvas/WorkflowEdge.tsx` |
| [`WorkflowCanvas`](#jcode-ui-workflowcanvas) | const | `packages/jcode-ui/src/canvas/WorkflowCanvas.tsx` |
| [`WorkflowNode`](#jcode-ui-workflownode) | const | `packages/jcode-ui/src/canvas/WorkflowNode.tsx` |
| [`WorkflowTemporaryEdge`](#jcode-ui-workflowtemporaryedge) | const | `packages/jcode-ui/src/canvas/WorkflowEdge.tsx` |
| [`ApiBaseProvider`](#jcode-ui-apibaseprovider) | function | `packages/jcode-ui/src/lib/apiBaseContext.tsx` |
| [`balanceEmphasis`](#jcode-ui-balanceemphasis) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`balanceInlineCode`](#jcode-ui-balanceinlinecode) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`bindCodeBlockCopy`](#jcode-ui-bindcodeblockcopy) | function | `packages/jcode-ui/src/lib/markdown.ts` |
| [`BranchPicker` (function)](#jcode-ui-branchpicker-function) | function | `packages/jcode-ui/src/product/BranchPicker.tsx` |
| [`ChatInput` (function)](#jcode-ui-chatinput-function) | function | `packages/jcode-ui/src/product/ChatInput.tsx` |
| [`completeStreamingMarkdown`](#jcode-ui-completestreamingmarkdown) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`completeStreamingMarkdownInfo`](#jcode-ui-completestreamingmarkdowninfo) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`completeTableRow`](#jcode-ui-completetablerow) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`createDefaultToolRegistry`](#jcode-ui-createdefaulttoolregistry) | function | `packages/jcode-ui/src/components/ToolRegistryContext.tsx` |
| [`extCategory`](#jcode-ui-extcategory) | function | `packages/jcode-ui/src/toolRenderers/fileTree.tsx` |
| [`formatQuote`](#jcode-ui-formatquote) | function | `packages/jcode-ui/src/components/QuoteSelection.tsx` |
| [`formatRelative`](#jcode-ui-formatrelative) | function | `packages/jcode-ui/src/components/ThreadList.tsx` |
| [`GoalBanner`](#jcode-ui-goalbanner) | function | `packages/jcode-ui/src/product/GoalBanner.tsx` |
| [`hashString`](#jcode-ui-hashstring) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`headTail`](#jcode-ui-headtail) | function | `packages/jcode-ui/src/toolRenderers/terminal.tsx` |
| [`isRemotePath`](#jcode-ui-isremotepath) | function | `packages/jcode-ui/src/product/remote.ts` |
| [`ModelSelector`](#jcode-ui-modelselector) | function | `packages/jcode-ui/src/components/ModelSelector.tsx` |
| [`parseCodeInfo`](#jcode-ui-parsecodeinfo) | function | `packages/jcode-ui/src/lib/markdown.ts` |
| [`parsePathList`](#jcode-ui-parsepathlist) | function | `packages/jcode-ui/src/toolRenderers/fileTree.tsx` |
| [`parseRemoteLabel`](#jcode-ui-parseremotelabel) | function | `packages/jcode-ui/src/product/remote.ts` |
| [`parseStackTrace`](#jcode-ui-parsestacktrace) | function | `packages/jcode-ui/src/toolRenderers/stackTrace.tsx` |
| [`parseTestOutput`](#jcode-ui-parsetestoutput) | function | `packages/jcode-ui/src/toolRenderers/testResults.tsx` |
| [`ProviderIcon`](#jcode-ui-providericon) | function | `packages/jcode-ui/src/product/ProviderIcon.tsx` |
| [`readDraft`](#jcode-ui-readdraft) | function | `packages/jcode-ui/src/product/drafts.ts` |
| [`Reasoning`](#jcode-ui-reasoning) | function | `packages/jcode-ui/src/components/Reasoning.tsx` |
| [`registerCodeBlockRenderer`](#jcode-ui-registercodeblockrenderer) | function | `packages/jcode-ui/src/lib/markdown.ts` |
| [`registerMathRenderer`](#jcode-ui-registermathrenderer) | function | `packages/jcode-ui/src/lib/markdown.ts` |
| [`renderMarkdown`](#jcode-ui-rendermarkdown) | function | `packages/jcode-ui/src/lib/markdown.ts` |
| [`renderMarkdownStreaming`](#jcode-ui-rendermarkdownstreaming) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`resolveProductComposerStrings`](#jcode-ui-resolveproductcomposerstrings) | function | `packages/jcode-ui/src/product/strings.ts` |
| [`scanFenceState`](#jcode-ui-scanfencestate) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`Sources`](#jcode-ui-sources) | function | `packages/jcode-ui/src/components/Sources.tsx` |
| [`splitTopLevelBlocks`](#jcode-ui-splittoplevelblocks) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`stripTrailingLink`](#jcode-ui-striptrailinglink) | function | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`Thread` (jcode-ui)](#jcode-ui-thread) | function | `packages/jcode-ui/src/components/Thread.tsx` |
| [`ToolIcon`](#jcode-ui-toolicon) | function | `packages/jcode-ui/src/components/toolIcons.tsx` |
| [`ToolRegistryProvider`](#jcode-ui-toolregistryprovider) | function | `packages/jcode-ui/src/components/ToolRegistryContext.tsx` |
| [`ToolRow`](#jcode-ui-toolrow) | function | `packages/jcode-ui/src/components/ToolRow.tsx` |
| [`ToolRowHeader`](#jcode-ui-toolrowheader) | function | `packages/jcode-ui/src/components/ToolRow.tsx` |
| [`toolTreeToGraph`](#jcode-ui-tooltreetograph) | function | `packages/jcode-ui/src/canvas/toolTreeToGraph.ts` |
| [`truncate`](#jcode-ui-truncate) | function | `packages/jcode-ui/src/toolRenderers/terminal.tsx` |
| [`useComposerStrings`](#jcode-ui-usecomposerstrings) | function | `packages/jcode-ui/src/product/useComposerStrings.ts` |
| [`useStreamingMarkdown`](#jcode-ui-usestreamingmarkdown) | function | `packages/jcode-ui/src/lib/useStreamingMarkdown.ts` |
| [`useToolRegistry`](#jcode-ui-usetoolregistry) | function | `packages/jcode-ui/src/components/ToolRegistryContext.tsx` |
| [`workspaceName`](#jcode-ui-workspacename) | function | `packages/jcode-ui/src/product/remote.ts` |
| [`WorkspacePicker`](#jcode-ui-workspacepicker) | function | `packages/jcode-ui/src/product/WorkspacePicker.tsx` |
| [`writeDraft`](#jcode-ui-writedraft) | function | `packages/jcode-ui/src/product/drafts.ts` |
| [`ActivityGroupCardProps`](#jcode-ui-activitygroupcardprops) | interface | `packages/jcode-ui/src/components/ActivityGroupCard.tsx` |
| [`ApiBaseProviderProps`](#jcode-ui-apibaseproviderprops) | interface | `packages/jcode-ui/src/lib/apiBaseContext.tsx` |
| [`ApprovalBannerProps`](#jcode-ui-approvalbannerprops) | interface | `packages/jcode-ui/src/components/ApprovalBanner.tsx` |
| [`ArtifactProps`](#jcode-ui-artifactprops) | interface | `packages/jcode-ui/src/components/Artifact.tsx` |
| [`AskUserCardProps`](#jcode-ui-askusercardprops) | interface | `packages/jcode-ui/src/components/AskUserCard.tsx` |
| [`AskUserCardStrings`](#jcode-ui-askusercardstrings) | interface | `packages/jcode-ui/src/components/AskUserCard.tsx` |
| [`AttachmentListProps`](#jcode-ui-attachmentlistprops) | interface | `packages/jcode-ui/src/components/Attachment.tsx` |
| [`AttachmentProps`](#jcode-ui-attachmentprops) | interface | `packages/jcode-ui/src/components/Attachment.tsx` |
| [`AudioPlayerProps`](#jcode-ui-audioplayerprops) | interface | `packages/jcode-ui/src/voice/AudioPlayer.tsx` |
| [`BranchPickerProps`](#jcode-ui-branchpickerprops) | interface | `packages/jcode-ui/src/components/BranchPicker.tsx` |
| [`BrowseFolder`](#jcode-ui-browsefolder) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`BrowseResult`](#jcode-ui-browseresult) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`CanvasControlsProps`](#jcode-ui-canvascontrolsprops) | interface | `packages/jcode-ui/src/canvas/CanvasControls.tsx` |
| [`CanvasPanelProps`](#jcode-ui-canvaspanelprops) | interface | `packages/jcode-ui/src/canvas/CanvasPanel.tsx` |
| [`ChatInputProps`](#jcode-ui-chatinputprops) | interface | `packages/jcode-ui/src/components/ChatInput.tsx` |
| [`CodeBlockHookArgs`](#jcode-ui-codeblockhookargs) | interface | `packages/jcode-ui/src/lib/markdown.ts` |
| [`CompactToolRowProps`](#jcode-ui-compacttoolrowprops) | interface | `packages/jcode-ui/src/components/CompactToolRow.tsx` |
| [`CompletedTurnCardProps`](#jcode-ui-completedturncardprops) | interface | `packages/jcode-ui/src/components/CompletedTurnCard.tsx` |
| [`CompletionResult`](#jcode-ui-completionresult) | interface | `packages/jcode-ui/src/lib/streamingMarkdown.ts` |
| [`ContextBarProps`](#jcode-ui-contextbarprops) | interface | `packages/jcode-ui/src/components/ContextBar.tsx` |
| [`CustomAgentInfo`](#jcode-ui-customagentinfo) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`ExploringGroupCardProps`](#jcode-ui-exploringgroupcardprops) | interface | `packages/jcode-ui/src/components/ExploringGroupCard.tsx` |
| [`ExportButtonProps`](#jcode-ui-exportbuttonprops) | interface | `packages/jcode-ui/src/components/ExportButton.tsx` |
| [`GeneratedImageCardProps`](#jcode-ui-generatedimagecardprops) | interface | `packages/jcode-ui/src/components/GeneratedImageCard.tsx` |
| [`GeneratedImageCardStrings`](#jcode-ui-generatedimagecardstrings) | interface | `packages/jcode-ui/src/components/GeneratedImageCard.tsx` |
| [`GitBranchesResult`](#jcode-ui-gitbranchesresult) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`GitCheckoutResult`](#jcode-ui-gitcheckoutresult) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`KatexApi`](#jcode-ui-katexapi) | interface | `packages/jcode-ui/src/plugins/external-modules.d.ts` |
| [`KatexOptions`](#jcode-ui-katexoptions) | interface | `packages/jcode-ui/src/plugins/external-modules.d.ts` |
| [`KatexPluginOptions`](#jcode-ui-katexpluginoptions) | interface | `packages/jcode-ui/src/plugins/katex.ts` |
| [`MermaidApi`](#jcode-ui-mermaidapi) | interface | `packages/jcode-ui/src/plugins/external-modules.d.ts` |
| [`MermaidPluginOptions`](#jcode-ui-mermaidpluginoptions) | interface | `packages/jcode-ui/src/plugins/mermaid.ts` |
| [`MermaidRenderResult`](#jcode-ui-mermaidrenderresult) | interface | `packages/jcode-ui/src/plugins/external-modules.d.ts` |
| [`MessageProps`](#jcode-ui-messageprops) | interface | `packages/jcode-ui/src/components/Message.tsx` |
| [`MessageSlots`](#jcode-ui-messageslots) | interface | `packages/jcode-ui/src/components/Message.tsx` |
| [`ModelInfo`](#jcode-ui-modelinfo) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`ModelRef`](#jcode-ui-modelref) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`ModelSelectorOption`](#jcode-ui-modelselectoroption) | interface | `packages/jcode-ui/src/components/ModelSelector.tsx` |
| [`ModelSelectorProps`](#jcode-ui-modelselectorprops) | interface | `packages/jcode-ui/src/components/ModelSelector.tsx` |
| [`PendingAttachmentListProps`](#jcode-ui-pendingattachmentlistprops) | interface | `packages/jcode-ui/src/components/Attachment.tsx` |
| [`ProductChatInputProps`](#jcode-ui-productchatinputprops) | interface | `packages/jcode-ui/src/product/ChatInput.tsx` |
| [`ProductComposerHost`](#jcode-ui-productcomposerhost) | interface | `packages/jcode-ui/src/product/host.ts` |
| [`ProductComposerStrings`](#jcode-ui-productcomposerstrings) | interface | `packages/jcode-ui/src/product/strings.ts` |
| [`ProviderInfo`](#jcode-ui-providerinfo) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`QuoteSelectionProps`](#jcode-ui-quoteselectionprops) | interface | `packages/jcode-ui/src/components/QuoteSelection.tsx` |
| [`ReasoningOption`](#jcode-ui-reasoningoption) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`ReasoningProps`](#jcode-ui-reasoningprops) | interface | `packages/jcode-ui/src/components/Reasoning.tsx` |
| [`RemoteMeta`](#jcode-ui-remotemeta) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`RuntimeTaskListProps`](#jcode-ui-runtimetasklistprops) | interface | `packages/jcode-ui/src/components/TaskList.tsx` |
| [`SlashCommandInfo`](#jcode-ui-slashcommandinfo) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`SourcesProps`](#jcode-ui-sourcesprops) | interface | `packages/jcode-ui/src/components/Sources.tsx` |
| [`SpeechInputProps`](#jcode-ui-speechinputprops) | interface | `packages/jcode-ui/src/voice/SpeechInput.tsx` |
| [`StackFrame`](#jcode-ui-stackframe) | interface | `packages/jcode-ui/src/toolRenderers/stackTrace.tsx` |
| [`StackTrace`](#jcode-ui-stacktrace) | interface | `packages/jcode-ui/src/toolRenderers/stackTrace.tsx` |
| [`SuggestionItem`](#jcode-ui-suggestionitem) | interface | `packages/jcode-ui/src/components/Suggestions.tsx` |
| [`SuggestionsProps`](#jcode-ui-suggestionsprops) | interface | `packages/jcode-ui/src/components/Suggestions.tsx` |
| [`TaskContextBreakdown` (jcode-ui)](#jcode-ui-taskcontextbreakdown) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`TaskListItemProps`](#jcode-ui-tasklistitemprops) | interface | `packages/jcode-ui/src/components/TaskList.tsx` |
| [`TaskListProps`](#jcode-ui-tasklistprops) | interface | `packages/jcode-ui/src/components/TaskList.tsx` |
| [`TaskStats`](#jcode-ui-taskstats) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`TestCase`](#jcode-ui-testcase) | interface | `packages/jcode-ui/src/toolRenderers/testResults.tsx` |
| [`TestSummary`](#jcode-ui-testsummary) | interface | `packages/jcode-ui/src/toolRenderers/testResults.tsx` |
| [`ThreadListProps`](#jcode-ui-threadlistprops) | interface | `packages/jcode-ui/src/components/ThreadList.tsx` |
| [`ThreadProps` (jcode-ui)](#jcode-ui-threadprops) | interface | `packages/jcode-ui/src/components/Thread.tsx` |
| [`ThreadWelcomeProps`](#jcode-ui-threadwelcomeprops) | interface | `packages/jcode-ui/src/components/ThreadWelcome.tsx` |
| [`ToolBatchGroupCardProps`](#jcode-ui-toolbatchgroupcardprops) | interface | `packages/jcode-ui/src/components/ToolBatchGroup.tsx` |
| [`ToolCallCardProps`](#jcode-ui-toolcallcardprops) | interface | `packages/jcode-ui/src/components/ToolCallCard.tsx` |
| [`ToolCallCardSlots`](#jcode-ui-toolcallcardslots) | interface | `packages/jcode-ui/src/components/ToolCallCard.tsx` |
| [`ToolGraph`](#jcode-ui-toolgraph) | interface | `packages/jcode-ui/src/canvas/toolTreeToGraph.ts` |
| [`ToolRegistryProviderProps`](#jcode-ui-toolregistryproviderprops) | interface | `packages/jcode-ui/src/components/ToolRegistryContext.tsx` |
| [`ToolRowProps`](#jcode-ui-toolrowprops) | interface | `packages/jcode-ui/src/components/ToolRow.tsx` |
| [`ToolTreeToGraphOptions`](#jcode-ui-tooltreetographoptions) | interface | `packages/jcode-ui/src/canvas/toolTreeToGraph.ts` |
| [`TranscriptionProps`](#jcode-ui-transcriptionprops) | interface | `packages/jcode-ui/src/voice/Transcription.tsx` |
| [`TranscriptSegment`](#jcode-ui-transcriptsegment) | interface | `packages/jcode-ui/src/voice/Transcription.tsx` |
| [`TurnChangesCardProps`](#jcode-ui-turnchangescardprops) | interface | `packages/jcode-ui/src/components/TurnChangesCard.tsx` |
| [`VoiceVisualizerProps`](#jcode-ui-voicevisualizerprops) | interface | `packages/jcode-ui/src/voice/VoiceVisualizer.tsx` |
| [`WorkflowCanvasProps`](#jcode-ui-workflowcanvasprops) | interface | `packages/jcode-ui/src/canvas/WorkflowCanvas.tsx` |
| [`WorkspaceTaskRef`](#jcode-ui-workspacetaskref) | interface | `packages/jcode-ui/src/product/types.ts` |
| [`AgentMode`](#jcode-ui-agentmode) | type | `packages/jcode-ui/src/product/types.ts` |
| [`CodeBlockHook`](#jcode-ui-codeblockhook) | type | `packages/jcode-ui/src/lib/markdown.ts` |
| [`GeneratedImageState`](#jcode-ui-generatedimagestate) | type | `packages/jcode-ui/src/components/GeneratedImageCard.tsx` |
| [`JcodeStepData`](#jcode-ui-jcodestepdata) | type | `packages/jcode-ui/src/canvas/WorkflowNode.tsx` |
| [`JcodeStepNode`](#jcode-ui-jcodestepnode) | type | `packages/jcode-ui/src/canvas/WorkflowNode.tsx` |
| [`JcodeStepStatus`](#jcode-ui-jcodestepstatus) | type | `packages/jcode-ui/src/canvas/WorkflowNode.tsx` |
| [`MathRenderer`](#jcode-ui-mathrenderer) | type | `packages/jcode-ui/src/lib/markdown.ts` |
| [`RemoteKind`](#jcode-ui-remotekind) | type | `packages/jcode-ui/src/product/types.ts` |
| [`RemotePrefill`](#jcode-ui-remoteprefill) | type | `packages/jcode-ui/src/product/types.ts` |
| [`SpeechInputStatus`](#jcode-ui-speechinputstatus) | type | `packages/jcode-ui/src/voice/SpeechInput.tsx` |
| [`ToolIconProps`](#jcode-ui-tooliconprops) | type | `packages/jcode-ui/src/components/toolIcons.tsx` |
| [`WorkspaceKind`](#jcode-ui-workspacekind) | type | `packages/jcode-ui/src/product/types.ts` |

<a id="jcode-ui-activitygroupcard"></a>

### `ActivityGroupCard`

`const` · `packages/jcode-ui/src/components/ActivityGroupCard.tsx`

```ts
export const ActivityGroupCard = …
```

<a id="jcode-ui-approvalbanner"></a>

### `ApprovalBanner`

`const` · `packages/jcode-ui/src/components/ApprovalBanner.tsx`

```ts
export const ApprovalBanner = …
```

<a id="jcode-ui-artifact"></a>

### `Artifact`

`const` · `packages/jcode-ui/src/components/Artifact.tsx`

```ts
export const Artifact = …
```

<a id="jcode-ui-askusercard"></a>

### `AskUserCard`

`const` · `packages/jcode-ui/src/components/AskUserCard.tsx`

```ts
export const AskUserCard = …
```

<a id="jcode-ui-attachment"></a>

### `Attachment`

`const` · `packages/jcode-ui/src/components/Attachment.tsx`

```ts
export const Attachment = …
```

<a id="jcode-ui-attachmentlist"></a>

### `AttachmentList`

`const` · `packages/jcode-ui/src/components/Attachment.tsx`

```ts
export const AttachmentList = …
```

<a id="jcode-ui-audioplayer"></a>

### `AudioPlayer`

`const` · `packages/jcode-ui/src/voice/AudioPlayer.tsx`

```ts
export const AudioPlayer = …
```

<a id="jcode-ui-branchpicker"></a>

<a id="jcode-ui-branchpicker-const"></a>

### `BranchPicker` (const)

`const` · `packages/jcode-ui/src/components/BranchPicker.tsx`

```ts
export const BranchPicker = …
```

<a id="jcode-ui-browsershotrenderer"></a>

### `BrowserShotRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/browserShot.tsx`

```ts
export const BrowserShotRenderer = …
```

<a id="jcode-ui-canvascontrols"></a>

### `CanvasControls`

`const` · `packages/jcode-ui/src/canvas/CanvasControls.tsx`

```ts
export const CanvasControls = …
```

<a id="jcode-ui-canvaspanel"></a>

### `CanvasPanel`

`const` · `packages/jcode-ui/src/canvas/CanvasPanel.tsx`

```ts
export const CanvasPanel = …
```

<a id="jcode-ui-chatinput"></a>

<a id="jcode-ui-chatinput-const"></a>

### `ChatInput` (const)

`const` · `packages/jcode-ui/src/components/ChatInput.tsx`

```ts
export const ChatInput = …
```

<a id="jcode-ui-compacttoolrow"></a>

### `CompactToolRow`

`const` · `packages/jcode-ui/src/components/CompactToolRow.tsx`

```ts
export const CompactToolRow = …
```

<a id="jcode-ui-completedturncard"></a>

### `CompletedTurnCard`

`const` · `packages/jcode-ui/src/components/CompletedTurnCard.tsx`

```ts
export const CompletedTurnCard = …
```

<a id="jcode-ui-computeractrenderer"></a>

### `ComputerActRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/computerAct.tsx`

```ts
export const ComputerActRenderer = …
```

<a id="jcode-ui-computershotrenderer"></a>

### `ComputerShotRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/computerShot.tsx`

```ts
export const ComputerShotRenderer = …
```

<a id="jcode-ui-connectionbanner"></a>

### `ConnectionBanner`

`const` · `packages/jcode-ui/src/components/ConnectionBanner.tsx`

```ts
export const ConnectionBanner = …
```

<a id="jcode-ui-contextbar"></a>

### `ContextBar`

`const` · `packages/jcode-ui/src/components/ContextBar.tsx`

```ts
export const ContextBar = …
```

<a id="jcode-ui-diffrenderer"></a>

### `DiffRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/diff.tsx`

```ts
export const DiffRenderer = …
```

<a id="jcode-ui-exploringgroupcard"></a>

### `ExploringGroupCard`

`const` · `packages/jcode-ui/src/components/ExploringGroupCard.tsx`

@deprecated See module note — use ActivityGroupCard for new timelines.

```ts
/** @deprecated See module note — use ActivityGroupCard for new timelines. */
export const ExploringGroupCard = …
```

<a id="jcode-ui-exportbutton"></a>

### `ExportButton`

`const` · `packages/jcode-ui/src/components/ExportButton.tsx`

```ts
export const ExportButton = …
```

<a id="jcode-ui-filetreerenderer"></a>

### `FileTreeRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/fileTree.tsx`

```ts
export const FileTreeRenderer = …
```

<a id="jcode-ui-fileviewerrenderer"></a>

### `FileViewerRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/fileViewer.tsx`

```ts
export const FileViewerRenderer = …
```

<a id="jcode-ui-generatedimagecard"></a>

### `GeneratedImageCard`

`const` · `packages/jcode-ui/src/components/GeneratedImageCard.tsx`

```ts
export const GeneratedImageCard = …
```

<a id="jcode-ui-genericrenderer"></a>

### `GenericRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/generic.tsx`

```ts
export const GenericRenderer = …
```

<a id="jcode-ui-message"></a>

### `Message` (jcode-ui)

`const` · `packages/jcode-ui/src/components/Message.tsx`

```ts
export const Message = …
```

<a id="jcode-ui-pendingattachmentlist"></a>

### `PendingAttachmentList`

`const` · `packages/jcode-ui/src/components/Attachment.tsx`

Renders the composer's pending-attachment strip (Composer 2 adapter path).

```ts
/** Renders the composer's pending-attachment strip (Composer 2 adapter path). */
export const PendingAttachmentList = …
```

<a id="jcode-ui-quoteselection"></a>

### `QuoteSelection`

`const` · `packages/jcode-ui/src/components/QuoteSelection.tsx`

```ts
export const QuoteSelection = …
```

<a id="jcode-ui-runtimetasklist"></a>

### `RuntimeTaskList`

`const` · `packages/jcode-ui/src/components/TaskList.tsx`

TaskList bound to the runtime `todos` selector.

```ts
/** TaskList bound to the runtime `todos` selector. */
export const RuntimeTaskList = …
```

<a id="jcode-ui-searchrenderer"></a>

### `SearchRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/search.tsx`

```ts
export const SearchRenderer = …
```

<a id="jcode-ui-skillrenderer"></a>

### `SkillRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/skill.tsx`

```ts
export const SkillRenderer = …
```

<a id="jcode-ui-speechinput"></a>

### `SpeechInput`

`const` · `packages/jcode-ui/src/voice/SpeechInput.tsx`

```ts
export const SpeechInput = …
```

<a id="jcode-ui-stacktracerenderer"></a>

### `StackTraceRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/stackTrace.tsx`

```ts
export const StackTraceRenderer = …
```

<a id="jcode-ui-suggestions"></a>

### `Suggestions`

`const` · `packages/jcode-ui/src/components/Suggestions.tsx`

```ts
export const Suggestions = …
```

<a id="jcode-ui-teamcreaterenderer"></a>

### `TeamCreateRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/team.tsx`

```ts
export const TeamCreateRenderer = …
```

<a id="jcode-ui-teamlistrenderer"></a>

### `TeamListRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/team.tsx`

```ts
export const TeamListRenderer = …
```

<a id="jcode-ui-teammessagerenderer"></a>

### `TeamMessageRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/team.tsx`

```ts
export const TeamMessageRenderer = …
```

<a id="jcode-ui-teamspawnrenderer"></a>

### `TeamSpawnRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/team.tsx`

```ts
export const TeamSpawnRenderer = …
```

<a id="jcode-ui-terminalrenderer"></a>

### `TerminalRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/terminal.tsx`

```ts
export const TerminalRenderer = …
```

<a id="jcode-ui-testresultsrenderer"></a>

### `TestResultsRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/testResults.tsx`

```ts
export const TestResultsRenderer = …
```

<a id="jcode-ui-threadlist"></a>

### `ThreadList`

`const` · `packages/jcode-ui/src/components/ThreadList.tsx`

```ts
export const ThreadList = …
```

<a id="jcode-ui-threadwelcome"></a>

### `ThreadWelcome`

`const` · `packages/jcode-ui/src/components/ThreadWelcome.tsx`

```ts
export const ThreadWelcome = …
```

<a id="jcode-ui-todorenderer"></a>

### `TodoRenderer`

`const` · `packages/jcode-ui/src/toolRenderers/todo.tsx`

```ts
export const TodoRenderer = …
```

<a id="jcode-ui-toolbatchgroupcard"></a>

### `ToolBatchGroupCard`

`const` · `packages/jcode-ui/src/components/ToolBatchGroup.tsx`

@deprecated See module note — use ActivityGroupCard for new timelines.

```ts
/** @deprecated See module note — use ActivityGroupCard for new timelines. */
export const ToolBatchGroupCard = …
```

<a id="jcode-ui-toolcallcard"></a>

### `ToolCallCard`

`const` · `packages/jcode-ui/src/components/ToolCallCard.tsx`

```ts
export const ToolCallCard = …
```

<a id="jcode-ui-transcription"></a>

### `Transcription`

`const` · `packages/jcode-ui/src/voice/Transcription.tsx`

```ts
export const Transcription = …
```

<a id="jcode-ui-turnchangescard"></a>

### `TurnChangesCard`

`const` · `packages/jcode-ui/src/components/TurnChangesCard.tsx`

```ts
export const TurnChangesCard = …
```

<a id="jcode-ui-voicevisualizer"></a>

### `VoiceVisualizer`

`const` · `packages/jcode-ui/src/voice/VoiceVisualizer.tsx`

```ts
export const VoiceVisualizer = …
```

<a id="jcode-ui-workflowanimatededge"></a>

### `WorkflowAnimatedEdge`

`const` · `packages/jcode-ui/src/canvas/WorkflowEdge.tsx`

```ts
export const WorkflowAnimatedEdge = …
```

<a id="jcode-ui-workflowcanvas"></a>

### `WorkflowCanvas`

`const` · `packages/jcode-ui/src/canvas/WorkflowCanvas.tsx`

```ts
export const WorkflowCanvas = …
```

<a id="jcode-ui-workflownode"></a>

### `WorkflowNode`

`const` · `packages/jcode-ui/src/canvas/WorkflowNode.tsx`

```ts
export const WorkflowNode = …
```

<a id="jcode-ui-workflowtemporaryedge"></a>

### `WorkflowTemporaryEdge`

`const` · `packages/jcode-ui/src/canvas/WorkflowEdge.tsx`

```ts
export const WorkflowTemporaryEdge = …
```

<a id="jcode-ui-apibaseprovider"></a>

### `ApiBaseProvider`

`function` · `packages/jcode-ui/src/lib/apiBaseContext.tsx`

```ts
export function ApiBaseProvider({ apiBase, children }: ApiBaseProviderProps) { … }
```

<a id="jcode-ui-balanceemphasis"></a>

### `balanceEmphasis`

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Close dangling emphasis runs (`**`, `*`, `_`). Counts are taken over prose
only (code stripped). Underscores are counted only when they flank a word
boundary, so `snake_case` and URLs don't trigger a false close.

```ts
export function balanceEmphasis(text: string): string { … }
```

<a id="jcode-ui-balanceinlinecode"></a>

### `balanceInlineCode`

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Close a dangling inline code span. Counts backticks in prose (outside fenced
blocks); an odd count means one span is open, so append a closing backtick.

```ts
export function balanceInlineCode(text: string): string { … }
```

<a id="jcode-ui-bindcodeblockcopy"></a>

### `bindCodeBlockCopy`

`function` · `packages/jcode-ui/src/lib/markdown.ts`

Attach one delegated click handler that powers every `.jcode-codeblock__copy`
button under `root`. Call once on the container that holds rendered markdown
(idempotent per element). On click, copies the decoded `data-code` payload and
flips the label to "Copied" for 1.5s.

@returns a cleanup function that removes the listener.

```ts
export function bindCodeBlockCopy(root: HTMLElement): () => void { … }
```

<a id="jcode-ui-branchpicker-function"></a>

### `BranchPicker` (function)

`function` · `packages/jcode-ui/src/product/BranchPicker.tsx`

```ts
export function BranchPicker({ host, placement = 'top' }: { host: ProductComposerHost; placement?: 'top' | 'bottom' }) { … }
```

<a id="jcode-ui-chatinput-function"></a>

### `ChatInput` (function)

`function` · `packages/jcode-ui/src/product/ChatInput.tsx`

```ts
export function ChatInput({ host, onSent, pickerPlacement = 'top', elevated = false }: ProductChatInputProps) { … }
```

<a id="jcode-ui-completestreamingmarkdown"></a>

### `completeStreamingMarkdown`

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Pure completion: raw streaming buffer → renderable markdown string.

```ts
export function completeStreamingMarkdown(md: string): string { … }
```

<a id="jcode-ui-completestreamingmarkdowninfo"></a>

### `completeStreamingMarkdownInfo`

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Complete unclosed markdown structures. Reports whether a code fence was closed
so the renderer can flag the active code block (shimmer). See the module doc.

```ts
export function completeStreamingMarkdownInfo(md: string): CompletionResult { … }
```

<a id="jcode-ui-completetablerow"></a>

### `completeTableRow`

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Add the trailing pipe to a table row that is still being typed.

```ts
export function completeTableRow(text: string): string { … }
```

<a id="jcode-ui-createdefaulttoolregistry"></a>

### `createDefaultToolRegistry`

`function` · `packages/jcode-ui/src/components/ToolRegistryContext.tsx`

Build the jcode default registry (matches Vue ToolCallCard renderType map).

```ts
export function createDefaultToolRegistry(): ToolRendererRegistry { … }
```

<a id="jcode-ui-extcategory"></a>

### `extCategory`

`function` · `packages/jcode-ui/src/toolRenderers/fileTree.tsx`

Map a filename to a dot color category (resolved to a token in p5.css).

```ts
export function extCategory(name: string): string { … }
```

<a id="jcode-ui-formatquote"></a>

### `formatQuote`

`function` · `packages/jcode-ui/src/components/QuoteSelection.tsx`

Turn selected text into a markdown blockquote block for the composer.

```ts
export function formatQuote(text: string): string { … }
```

<a id="jcode-ui-formatrelative"></a>

### `formatRelative`

`function` · `packages/jcode-ui/src/components/ThreadList.tsx`

Compact relative-time formatter (no date-fns / dayjs). Buckets: just now,
Nm, Nh, Nd, Nw, Nmo, Ny. `now` is injectable for deterministic tests.

```ts
export function formatRelative(ts: number, now: number = Date.now()): string { … }
```

<a id="jcode-ui-goalbanner"></a>

### `GoalBanner`

`function` · `packages/jcode-ui/src/product/GoalBanner.tsx`

```ts
export function GoalBanner({ host }: { host: ProductComposerHost }) { … }
```

<a id="jcode-ui-hashstring"></a>

### `hashString`

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Fast, stable string hash (djb2 + length) for block cache keys.

```ts
export function hashString(s: string): string { … }
```

<a id="jcode-ui-headtail"></a>

### `headTail`

`function` · `packages/jcode-ui/src/toolRenderers/terminal.tsx`

Keep head + tail lines with an omitted count (Codex-style mid ellipsis).

```ts
export function headTail(
  text: string,
  head: number,
  tail: number,
): { … }
```

<a id="jcode-ui-isremotepath"></a>

### `isRemotePath`

`function` · `packages/jcode-ui/src/product/remote.ts`

```ts
export function isRemotePath(path: string): boolean { … }
```

<a id="jcode-ui-modelselector"></a>

### `ModelSelector`

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

<a id="jcode-ui-parsecodeinfo"></a>

### `parseCodeInfo`

`function` · `packages/jcode-ui/src/lib/markdown.ts`

Parse a fenced-code info string into `{ lang, filename }`.
Supports two filename conventions:
  ```` ```ts title=a.ts ````  (also title="a b.ts")
  ```` ```ts:a.ts ````

```ts
export function parseCodeInfo(info: string): { … }
```

<a id="jcode-ui-parsepathlist"></a>

### `parsePathList`

`function` · `packages/jcode-ui/src/toolRenderers/fileTree.tsx`

```ts
export function parsePathList(text: string): { … }
```

<a id="jcode-ui-parseremotelabel"></a>

### `parseRemoteLabel`

`function` · `packages/jcode-ui/src/product/remote.ts`

Decompose ssh:// / docker:// project labels for wizard reconnect.

```ts
export function parseRemoteLabel(label: string): RemoteMeta | null { … }
```

<a id="jcode-ui-parsestacktrace"></a>

### `parseStackTrace`

`function` · `packages/jcode-ui/src/toolRenderers/stackTrace.tsx`

```ts
export function parseStackTrace(text: string): StackTrace | null { … }
```

<a id="jcode-ui-parsetestoutput"></a>

### `parseTestOutput`

`function` · `packages/jcode-ui/src/toolRenderers/testResults.tsx`

```ts
export function parseTestOutput(text: string): TestSummary | null { … }
```

<a id="jcode-ui-providericon"></a>

### `ProviderIcon`

`function` · `packages/jcode-ui/src/product/ProviderIcon.tsx`

```ts
export function ProviderIcon({ provider, size = 18, custom = false, resolveIcon }: ProviderIconProps) { … }
```

<a id="jcode-ui-readdraft"></a>

### `readDraft`

`function` · `packages/jcode-ui/src/product/drafts.ts`

readDraft returns the saved draft for a session ('' when none).

```ts
export function readDraft(sessionId: string): string { … }
```

<a id="jcode-ui-reasoning"></a>

### `Reasoning`

`function` · `packages/jcode-ui/src/components/Reasoning.tsx`

```ts
export function Reasoning({ reasoning, defaultExpanded = false, durationMs }: ReasoningProps) { … }
```

<a id="jcode-ui-registercodeblockrenderer"></a>

### `registerCodeBlockRenderer`

`function` · `packages/jcode-ui/src/lib/markdown.ts`

Register a fenced-code-block renderer. Hooks run in registration order; the
first to return non-null wins (used by the mermaid plugin for ```` ```mermaid ````).

```ts
export function registerCodeBlockRenderer(hook: CodeBlockHook): void { … }
```

<a id="jcode-ui-registermathrenderer"></a>

### `registerMathRenderer`

`function` · `packages/jcode-ui/src/lib/markdown.ts`

Register a math renderer and (once) install the `$…$` / `$$…$$` tokenizers.
Until this is called, math delimiters are left as literal text — so a doc
that never registers katex renders `$x^2$` verbatim (and pays no math cost).

```ts
export function registerMathRenderer(render: MathRenderer): void { … }
```

<a id="jcode-ui-rendermarkdown"></a>

### `renderMarkdown`

`function` · `packages/jcode-ui/src/lib/markdown.ts`

Render markdown → sanitized HTML string.

```ts
export function renderMarkdown(text: string): string { … }
```

<a id="jcode-ui-rendermarkdownstreaming"></a>

### `renderMarkdownStreaming`

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Render a streaming buffer to sanitized HTML. Equivalent to
`renderMarkdown(completeStreamingMarkdown(md))`, plus a shimmer class on the
code block that is still streaming.

```ts
export function renderMarkdownStreaming(md: string): string { … }
```

<a id="jcode-ui-resolveproductcomposerstrings"></a>

### `resolveProductComposerStrings`

`function` · `packages/jcode-ui/src/product/strings.ts`

Merge host overrides over the English defaults.

```ts
export function resolveProductComposerStrings(
  overrides?: Partial<ProductComposerStrings>,
): ProductComposerStrings { … }
```

<a id="jcode-ui-scanfencestate"></a>

### `scanFenceState`

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Scan for an unterminated code fence. Returns the open fence descriptor (so the
caller knows what to close with), or `null` when all fences are balanced.

```ts
export function scanFenceState(md: string): Fence | null { … }
```

<a id="jcode-ui-sources"></a>

### `Sources`

`function` · `packages/jcode-ui/src/components/Sources.tsx`

```ts
export function Sources({ sources }: SourcesProps) { … }
```

<a id="jcode-ui-splittoplevelblocks"></a>

### `splitTopLevelBlocks`

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Split markdown into top-level blocks on blank lines, keeping fenced code
blocks intact. Used to memoize completed blocks during streaming so per-frame
work is O(active block) instead of O(whole document).

```ts
export function splitTopLevelBlocks(md: string): string[] { … }
```

<a id="jcode-ui-striptrailinglink"></a>

### `stripTrailingLink`

`function` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

Remove a half-typed link/image at the very end of the buffer:
  `[label`         → dangling label
  `[label](url`    → dangling destination
Leaves completed links untouched.

```ts
export function stripTrailingLink(text: string): string { … }
```

<a id="jcode-ui-thread"></a>

### `Thread` (jcode-ui)

`function` · `packages/jcode-ui/src/components/Thread.tsx`

```ts
export function Thread({
  virtualize,
  emptyState,
  suggestions,
  renderPending,
  pendingLabel,
  className,
  overscanBottom,
  hidePendingAskUser = false,
  turnDurationLabel,
  turnExpandLabel,
  turnCollapseLabel,
}: ThreadProps): ReactNode { … }
```

<a id="jcode-ui-toolicon"></a>

### `ToolIcon`

`function` · `packages/jcode-ui/src/components/toolIcons.tsx`

Resolve a Heroicon for a tool by kind (preferred) then name.

```ts
export function ToolIcon({ kind, name, ...rest }: ToolIconProps) { … }
```

<a id="jcode-ui-toolregistryprovider"></a>

### `ToolRegistryProvider`

`function` · `packages/jcode-ui/src/components/ToolRegistryContext.tsx`

```ts
export function ToolRegistryProvider({ registry, children }: ToolRegistryProviderProps) { … }
```

<a id="jcode-ui-toolrow"></a>

### `ToolRow`

`function` · `packages/jcode-ui/src/components/ToolRow.tsx`

One expandable row: slot-headered ToolCallCard with the compact row line.

```ts
export function ToolRow({ tool, className }: ToolRowProps) { … }
```

<a id="jcode-ui-toolrowheader"></a>

### `ToolRowHeader`

`function` · `packages/jcode-ui/src/components/ToolRow.tsx`

Slot header for a grouped tool row. Rendered inside ToolCallCard's
slot-header button, so clicking anywhere on the row toggles the tool body.

```ts
export function ToolRowHeader({ tool }: { tool: ToolCall }) { … }
```

<a id="jcode-ui-tooltreetograph"></a>

### `toolTreeToGraph`

`function` · `packages/jcode-ui/src/canvas/toolTreeToGraph.ts`

```ts
export function toolTreeToGraph(
  tools: ToolCall[],
  options?: ToolTreeToGraphOptions,
): ToolGraph { … }
```

<a id="jcode-ui-truncate"></a>

### `truncate`

`function` · `packages/jcode-ui/src/toolRenderers/terminal.tsx`

Count code points (not UTF-16 units) so CJK truncation is fair.

```ts
export function truncate(text: string, max: number): string { … }
```

<a id="jcode-ui-usecomposerstrings"></a>

### `useComposerStrings`

`function` · `packages/jcode-ui/src/product/useComposerStrings.ts`

```ts
export function useComposerStrings(host: ProductComposerHost): ProductComposerStrings { … }
```

<a id="jcode-ui-usestreamingmarkdown"></a>

### `useStreamingMarkdown`

`function` · `packages/jcode-ui/src/lib/useStreamingMarkdown.ts`

```ts
export function useStreamingMarkdown(md: string): string { … }
```

<a id="jcode-ui-usetoolregistry"></a>

### `useToolRegistry`

`function` · `packages/jcode-ui/src/components/ToolRegistryContext.tsx`

```ts
export function useToolRegistry(): ToolRendererRegistry { … }
```

<a id="jcode-ui-workspacename"></a>

### `workspaceName`

`function` · `packages/jcode-ui/src/product/remote.ts`

Display name for a workspace path (local basename or remote label).

```ts
export function workspaceName(path: string): string { … }
```

<a id="jcode-ui-workspacepicker"></a>

### `WorkspacePicker`

`function` · `packages/jcode-ui/src/product/WorkspacePicker.tsx`

```ts
export function WorkspacePicker({ host, placement = 'top' }: { host: ProductComposerHost; placement?: 'top' | 'bottom' }) { … }
```

<a id="jcode-ui-writedraft"></a>

### `writeDraft`

`function` · `packages/jcode-ui/src/product/drafts.ts`

writeDraft saves a draft; empty text removes the entry.

```ts
export function writeDraft(sessionId: string, text: string): void { … }
```

<a id="jcode-ui-activitygroupcardprops"></a>

### `ActivityGroupCardProps`

`interface` · `packages/jcode-ui/src/components/ActivityGroupCard.tsx`

```ts
export interface ActivityGroupCardProps {
  group: ActivityGroup
  className?: string
}
```

<a id="jcode-ui-apibaseproviderprops"></a>

### `ApiBaseProviderProps`

`interface` · `packages/jcode-ui/src/lib/apiBaseContext.tsx`

```ts
export interface ApiBaseProviderProps {
  /** API base URL with no trailing slash. */
  apiBase: string
  children: React.ReactNode
}
```

<a id="jcode-ui-approvalbannerprops"></a>

### `ApprovalBannerProps`

`interface` · `packages/jcode-ui/src/components/ApprovalBanner.tsx`

```ts
export interface ApprovalBannerProps {
  approval: Approval
  /** Render inside the matching tool card. The tool header already owns the
   *  verb/target, and resolved state stays as a compact inline header badge. */
  embedded?: boolean
}
```

<a id="jcode-ui-artifactprops"></a>

### `ArtifactProps`

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

<a id="jcode-ui-askusercardprops"></a>

### `AskUserCardProps`

`interface` · `packages/jcode-ui/src/components/AskUserCard.tsx`

```ts
export interface AskUserCardProps {
  tool: ToolCall
  /** Docked cards replace the product composer while the agent waits. */
  placement?: 'timeline' | 'dock'
  /** Host-provided copy keeps the package backend and i18n agnostic. */
  strings?: Partial<AskUserCardStrings>
}
```

<a id="jcode-ui-askusercardstrings"></a>

### `AskUserCardStrings`

`interface` · `packages/jcode-ui/src/components/AskUserCard.tsx`

```ts
export interface AskUserCardStrings {
  title: string
  helper: string
  previous: string
  next: string
  skip: string
  submit: string
  submitting: string
  customPlaceholder: string
  recommended: string
  multiSelect: string
  skipped: string
  noAnswer: string
  submitError: string
}
```

<a id="jcode-ui-attachmentlistprops"></a>

### `AttachmentListProps`

`interface` · `packages/jcode-ui/src/components/Attachment.tsx`

```ts
export interface AttachmentListProps {
  images: ChatImage[]
  onRemove?: (index: number) => void
  size?: number
  /** Click-to-preview. Default true. */
  preview?: boolean
  className?: string
}
```

<a id="jcode-ui-attachmentprops"></a>

### `AttachmentProps`

`interface` · `packages/jcode-ui/src/components/Attachment.tsx`

```ts
export interface AttachmentProps {
  image: ChatImage
  /** Optional remove handler — renders the × button when provided (composer). */
  onRemove?: () => void
  /** Thumbnail size in px. Default 56 (assistant-ui tile is ~56). */
  size?: number
  /** Allow click-to-preview lightbox. Default true. */
  preview?: boolean
}
```

<a id="jcode-ui-audioplayerprops"></a>

### `AudioPlayerProps`

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

<a id="jcode-ui-branchpickerprops"></a>

### `BranchPickerProps`

`interface` · `packages/jcode-ui/src/components/BranchPicker.tsx`

```ts
export interface BranchPickerProps {
  message: MessageData
}
```

<a id="jcode-ui-browsefolder"></a>

### `BrowseFolder`

`interface` · `packages/jcode-ui/src/product/types.ts`

```ts
export interface BrowseFolder {
  name: string
  path: string
}
```

<a id="jcode-ui-browseresult"></a>

### `BrowseResult`

`interface` · `packages/jcode-ui/src/product/types.ts`

```ts
export interface BrowseResult {
  current: string
  folders: BrowseFolder[]
}
```

<a id="jcode-ui-canvascontrolsprops"></a>

### `CanvasControlsProps`

`interface` · `packages/jcode-ui/src/canvas/CanvasControls.tsx`

```ts
export interface CanvasControlsProps {
  /** Corner to dock the controls (default 'bottom-left'). */
  position?: PanelPosition
  className?: string
}
```

<a id="jcode-ui-canvaspanelprops"></a>

### `CanvasPanelProps`

`interface` · `packages/jcode-ui/src/canvas/CanvasPanel.tsx`

```ts
export interface CanvasPanelProps {
  /** Corner to dock the panel (default 'top-right'). */
  position?: PanelPosition
  className?: string
  children?: ReactNode
}
```

<a id="jcode-ui-chatinputprops"></a>

### `ChatInputProps`

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

<a id="jcode-ui-codeblockhookargs"></a>

### `CodeBlockHookArgs`

`interface` · `packages/jcode-ui/src/lib/markdown.ts`

Arguments passed to a fenced-code-block hook.

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

<a id="jcode-ui-compacttoolrowprops"></a>

### `CompactToolRowProps`

`interface` · `packages/jcode-ui/src/components/CompactToolRow.tsx`

```ts
export interface CompactToolRowProps {
  tool: ToolCall
}
```

<a id="jcode-ui-completedturncardprops"></a>

### `CompletedTurnCardProps`

`interface` · `packages/jcode-ui/src/components/CompletedTurnCard.tsx`

```ts
export interface CompletedTurnCardProps {
  turn: CompletedTurn
  renderActivity: (item: ThreadItem) => ReactNode
  durationLabel?: (durationMs: number) => string
  expandLabel?: string
  collapseLabel?: string
}
```

<a id="jcode-ui-completionresult"></a>

### `CompletionResult`

`interface` · `packages/jcode-ui/src/lib/streamingMarkdown.ts`

```ts
export interface CompletionResult {
  text: string
  /** True when an open code fence was auto-closed (last block is streaming). */
  fenceStreaming: boolean
}
```

<a id="jcode-ui-contextbarprops"></a>

### `ContextBarProps`

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

<a id="jcode-ui-customagentinfo"></a>

### `CustomAgentInfo`

`interface` · `packages/jcode-ui/src/product/types.ts`

Discoverable top-level agent definition. Empty selection means Default.

```ts
export interface CustomAgentInfo {
  name: string
  description: string
  /** Optional model applied when this agent is selected. */
  model?: string
}
```

<a id="jcode-ui-exploringgroupcardprops"></a>

### `ExploringGroupCardProps`

`interface` · `packages/jcode-ui/src/components/ExploringGroupCard.tsx`

```ts
export interface ExploringGroupCardProps {
  group: ExploringGroup
  className?: string
}
```

<a id="jcode-ui-exportbuttonprops"></a>

### `ExportButtonProps`

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

<a id="jcode-ui-generatedimagecardprops"></a>

### `GeneratedImageCardProps`

`interface` · `packages/jcode-ui/src/components/GeneratedImageCard.tsx`

```ts
export interface GeneratedImageCardProps {
  state: GeneratedImageState
  provider?: string
  model?: string
  aspectRatio?: string
  startedAt?: number
  imageSrc?: string
  title?: string
  alt?: string
  artifact?: ArtifactRef
  errorCode?: string
  errorMessage?: string
  assetError?: string
  strings?: Partial<GeneratedImageCardStrings>
  onOpenImage?: () => void
  onDownload?: () => void
  onOpenArtifact?: () => void
  onReveal?: () => void
  onOpenSettings?: () => void
}
```

<a id="jcode-ui-generatedimagecardstrings"></a>

### `GeneratedImageCardStrings`

`interface` · `packages/jcode-ui/src/components/GeneratedImageCard.tsx`

```ts
export interface GeneratedImageCardStrings {
  queued: string
  queuedHint: string
  generating: string
  saving: string
  savingHint: string
  succeeded: string
  failed: string
  uncertain: string
  uncertainHint: string
  cancelled: string
  cancelledHint: string
  authError: string
  quotaError: string
  safetyError: string
  rateLimitError: string
  downloadError: string
  persistError: string
  genericError: string
  loadingAsset: string
  assetError: string
  download: string
  openImage: string
  openArtifact: string
  reveal: string
  openSettings: string
}
```

<a id="jcode-ui-gitbranchesresult"></a>

### `GitBranchesResult`

`interface` · `packages/jcode-ui/src/product/types.ts`

```ts
export interface GitBranchesResult {
  /** Empty if not a git repo. */
  current: string
  /** Local branches, most-recently-committed first. */
  branches: string[]
}
```

<a id="jcode-ui-gitcheckoutresult"></a>

### `GitCheckoutResult`

`interface` · `packages/jcode-ui/src/product/types.ts`

```ts
export interface GitCheckoutResult {
  /** New current branch on success; '' when blocked. */
  branch: string
  /** true when a plain switch was aborted by uncommitted changes. */
  blocked?: boolean
  message?: string
  /** Files that would be overwritten, parsed from git's output. */
  files?: string[]
  stashed?: boolean
}
```

<a id="jcode-ui-katexapi"></a>

### `KatexApi`

`interface` · `packages/jcode-ui/src/plugins/external-modules.d.ts`

```ts
export interface KatexApi {
    renderToString(tex: string, options?: KatexOptions): string
  }
```

<a id="jcode-ui-katexoptions"></a>

### `KatexOptions`

`interface` · `packages/jcode-ui/src/plugins/external-modules.d.ts`

```ts
export interface KatexOptions {
    displayMode?: boolean
    throwOnError?: boolean
    output?: 'html' | 'mathml' | 'htmlAndMathml'
    [key: string]: unknown
  }
```

<a id="jcode-ui-katexpluginoptions"></a>

### `KatexPluginOptions`

`interface` · `packages/jcode-ui/src/plugins/katex.ts`

```ts
export interface KatexPluginOptions {
  /** Throw instead of rendering an error node. Default: false. */
  throwOnError?: boolean
  /** Any other KaTeX option is passed through. */
  [key: string]: unknown
}
```

<a id="jcode-ui-mermaidapi"></a>

### `MermaidApi`

`interface` · `packages/jcode-ui/src/plugins/external-modules.d.ts`

```ts
export interface MermaidApi {
    initialize(config: Record<string, unknown>): void
    render(id: string, text: string, container?: Element): Promise<MermaidRenderResult>
    parse?(text: string): Promise<boolean> | boolean
  }
```

<a id="jcode-ui-mermaidpluginoptions"></a>

### `MermaidPluginOptions`

`interface` · `packages/jcode-ui/src/plugins/mermaid.ts`

```ts
export interface MermaidPluginOptions {
  /** Mermaid theme, e.g. 'default' | 'neutral' | 'dark' | 'forest'. */
  theme?: string
  /** Any other mermaid.initialize() option is passed through. */
  [key: string]: unknown
}
```

<a id="jcode-ui-mermaidrenderresult"></a>

### `MermaidRenderResult`

`interface` · `packages/jcode-ui/src/plugins/external-modules.d.ts`

```ts
export interface MermaidRenderResult {
    svg: string
    bindFunctions?: (element: Element) => void
  }
```

<a id="jcode-ui-messageprops"></a>

### `MessageProps`

`interface` · `packages/jcode-ui/src/components/Message.tsx`

```ts
export interface MessageProps {
  message: MessageData
  /** Allow editing (typically user messages when idle). */
  canEdit?: boolean
  /** Hide the legacy footer duration when a completed-turn disclosure owns it. */
  showDuration?: boolean
  /** Optional chrome overrides (avatar / header / footer tail). */
  slots?: MessageSlots
}
```

<a id="jcode-ui-messageslots"></a>

### `MessageSlots`

`interface` · `packages/jcode-ui/src/components/Message.tsx`

Render-prop overrides for the message chrome. Each replaces a piece of the
 default layout; omitting one keeps the built-in rendering unchanged.

```ts
export interface MessageSlots {
  /** Supplies an avatar inside an opt-in role header. */
  avatar?: (message: MessageData) => ReactNode
  /** Replaces the entire role-header row (avatar + label). */
  header?: (message: MessageData) => ReactNode
  /** Appended to the tail of the action footer. */
  footerExtra?: (message: MessageData) => ReactNode
}
```

<a id="jcode-ui-modelinfo"></a>

### `ModelInfo`

`interface` · `packages/jcode-ui/src/product/types.ts`

```ts
export interface ModelInfo {
  id: string
  name: string
  tool_call: boolean
  context_limit?: number
  reasoning?: boolean
  recommended?: boolean
  default_enabled?: boolean
  /** Whether this model is available in the chat picker. Omitted is treated as disabled. */
  enabled?: boolean
  image_support?: boolean
  /** Transport modalities advertised by the model catalog. Legacy hosts may
   * omit them; consumers treat an omitted output list as text-only. */
  input_modalities?: string[]
  output_modalities?: string[]
  capability_availability?: 'supported' | 'unsupported' | 'unknown'
  image_sizes?: string[]
  image_aspect_ratios?: string[]
  image_resolutions?: string[]
  /** How this model exposes its reasoning/thinking controls. */
  reasoning_options?: ReasoningOption[]
}
```

<a id="jcode-ui-modelref"></a>

### `ModelRef`

`interface` · `packages/jcode-ui/src/product/types.ts`

```ts
export interface ModelRef {
  provider: string
  model: string
}
```

<a id="jcode-ui-modelselectoroption"></a>

### `ModelSelectorOption`

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

<a id="jcode-ui-modelselectorprops"></a>

### `ModelSelectorProps`

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

<a id="jcode-ui-pendingattachmentlistprops"></a>

### `PendingAttachmentListProps`

`interface` · `packages/jcode-ui/src/components/Attachment.tsx`

```ts
export interface PendingAttachmentListProps {
  items: PendingAttachmentItem[]
  /** Image tile size in px. Default 56. */
  size?: number
  className?: string
}
```

<a id="jcode-ui-productchatinputprops"></a>

### `ProductChatInputProps`

`interface` · `packages/jcode-ui/src/product/ChatInput.tsx`

```ts
export interface ProductChatInputProps {
  /** Host state + actions projection (see host.ts). */
  host: ProductComposerHost
  /** Fired when the user dispatches a message (sent now or queued mid-turn). */
  onSent?: () => void
  /** Direction for workspace/branch panels. Welcome opens downward. */
  pickerPlacement?: 'top' | 'bottom'
  /**
   * Elevate the whole composer into a bordered, shadowed card (welcome /
   * new-task screen). Docked conversation composers stay recessed.
   */
  elevated?: boolean
}
```

<a id="jcode-ui-productcomposerhost"></a>

### `ProductComposerHost`

`interface` · `packages/jcode-ui/src/product/host.ts`

```ts
export interface ProductComposerHost {
  // ── Model catalog state ───────────────────────────────────────────────────
  providerName: string
  modelName: string
  mode: AgentMode
  /**
   * Modes the host allows in the mode picker, in pick order. Absent ⇒ all
   * four modes (the desktop default). Cloud hosts cap cloud-originated
   * sessions at auto (M20), so they pass `['approval', 'plan', 'auto']` and
   * a `modeCeilingHint` string explaining the ceiling.
   */
  allowedModes?: AgentMode[]
  providers: ProviderInfo[]
  /** Favorite refs as "provider/model" keys. */
  favoriteModels: string[]
  recentModels: ModelRef[]
  /** Whether the current model accepts image input (paste/attach gating). */
  imageSupport: boolean
  /** Per-"provider/model" reasoning-effort overrides. */
  effortOverrides: Record<string, string>
  /** Available top-level custom agents. Empty hides the agent picker. */
  agents?: CustomAgentInfo[]
  /** Selected custom agent name. Empty means the built-in Default agent. */
  agentName?: string

  // ── Chat state ────────────────────────────────────────────────────────────
  slashCommands: SlashCommandInfo[]
  /** True when the conversation has any timeline items (welcome ↔ docked layout). */
  hasMessages: boolean
  goalArmed: boolean
  /** Current conversation id — keys composer drafts and task-stats lookups. */
  sessionId: string

  // ── Workspace state ───────────────────────────────────────────────────────
  projectPath: string
  /** Missing means a legacy project-bound host. */
  workspaceKind?: 'project' | 'scratch'
  /** All known tasks; the workspace picker derives the workspace list from `project`. */
  tasks: WorkspaceTaskRef[]

  // ── Presentation ──────────────────────────────────────────────────────────
  /** Localized labels; merged over the built-in English defaults. */
  strings?: Partial<ProductComposerStrings>
  /** Resolve a provider id to an inline SVG string (null ⇒ initial-letter fallback). */
  resolveProviderIcon?: (provider: string, custom?: boolean) => string | null

  // ── Model actions ─────────────────────────────────────────────────────────
  /** Switch the active model (host persists + updates its store). */
  selectModel: (provider: string, model: string) => void | Promise<void>
  /** Switch the session approval mode. */
  selectMode: (mode: AgentMode) => void | Promise<void>
  /** Select a top-level custom agent; empty string restores Default. */
  selectAgent?: (name: string) => void | Promise<void>
  /** Set/clear (empty string) a per-model reasoning-effort override. */
  setEffort: (provider: string, model: string, effort: string) => void | Promise<void>
  /** Toggle a favorite; the host knows the resulting state. */
  toggleFavorite: (provider: string, model: string) => void | Promise<void>
  /** Enable/disable a model in the picker (Manage Models dialog). */
  setModelEnabled: (provider: string, model: string, enabled: boolean) => void | Promise<void>
  /** Re-hydrate the model catalog after the Manage dialog closes. */
  refreshModels: () => void | Promise<void>

  // ── Chat actions ──────────────────────────────────────────────────────────
  setGoalArmed: (armed: boolean) => void
  /** Context-capacity popup data; null when unavailable. */
  fetchTaskStats: (sessionId: string) => Promise<TaskStats | null>

  // ── Workspace actions ─────────────────────────────────────────────────────
  /** Return the subset of local paths that no longer exist on disk. */
  validateWorkspacePaths: (paths: string[]) => Promise<string[]>
  browseFolders: (path?: string) => Promise<BrowseResult>
  /**
   * Activate a workspace. When `path` equals the current project the host starts
   * a NEW session in place; otherwise it switches projects. Throws on failure
   * (the picker shows the error inline).
   */
  switchWorkspace: (path: string) => Promise<void>
  /** Create and focus a fresh JCode-managed no-project workspace. */
  startScratchWorkspace?: () => Promise<void>
  /**
  /* … truncated */
}
```

<a id="jcode-ui-productcomposerstrings"></a>

### `ProductComposerStrings`

`interface` · `packages/jcode-ui/src/product/strings.ts`

Product-composer strings — every user-facing label the product composer
(ChatInput / WorkspacePicker / BranchPicker / GoalBanner) renders.

The jcode-ui package deliberately does NOT depend on an i18n library: hosts
inject already-localized strings through `ProductComposerHost.strings`
(partial — merged over these English defaults). Fields that need
interpolation are functions, so plural/variable handling stays on the host
side (the jcode app maps these 1:1 onto its i18next `chat.*` / `goal.*` /
`branches.*` / `workspace.*` keys).

```ts
export interface ProductComposerStrings {
  // ── composer textarea ──
  placeholder: string
  goalPlaceholder: string
  queuePlaceholder: string
  /** Fallback message body when only images are attached (no text). */
  attachedImages: string
  send: string
  queue: string
  stop: string
  stopAgent: string
  stopAndNext: string
  removeQueued: string
  add: string
  attachFiles: string
  command: string
  goal: string
  workflowBadge: string
  goalSlashDesc: string
  goalHintNext: string
  goalHintNextReplaces: string
  goalHintRemove: string
  goalHintReplace: string
  modelNoImages: string

  // ── mode picker ──
  modeApproval: string
  modeApprovalSub: string
  modePlan: string
  modePlanSub: string
  modeAuto: string
  modeAutoSub: string
  modeFullAccess: string
  modeFullAccessSub: string
  /** Shown in the mode picker when the host restricts `allowedModes` (M20). */
  modeCeilingHint: string

  // ── custom agent picker ──
  agentTitle: string
  agentDefault: string
  agentDefaultSub: string

  // ── model picker / manage dialog ──
  modelFilter: string
  modelCurrent: string
  modelFavorites: string
  modelReasoning: string
  modelTools: string
  modelImages: string
  modelImageOutput: string
  modelNoImageInput: string
  modelNone: string
  modelNoMatch: string
  modelManage: string
  modelManageTitle: string
  modelToggleVisibility: string
  modelVisibleCount: (visible: number, total: number) => string
  effort: string
  effortTitle: string
  effortDefault: string

  // ── context-capacity popup ──
  contextTitle: string
  contextSystemPrompt: string
  contextSystemTools: string
  contextMcpTools: string
  contextSkills: string
  contextMessages: string
  contextInput: string
  contextOutput: string
  contextCached: string
  contextReasoning: string
  contextCacheHitRate: string

  // ── workspace picker ──
  workspaceNone: string
  workspaceNonePlural: string
  workspaceSearch: string
  workspaceLoading: string
  workspaceNoFolders: string
  workspaceOpen: string
  workspaceOpenFolder: string
  workspaceOpenError: string
  workspacePathPlaceholder: string
  workspaceScratchAction: string
  remoteConnect: string

  // ── branch picker ──
  branchesTitle: string
  branchesNone: string
  branchSearch: string
  branchCreate: string
  branchCreateBtn: string
  branchNewName: string
  branchCurrent: (name: string) => string
  branchConfirmTitle: string
  branchConfirmIntro: (branch: string) => string
  branchConfirmMore: (count: number) => string
  branchConfirmStash: string
  branchConfirmDiscard: string
  branchConfirmCancel: string
  branchConfirmHint: string
  branchSwitchError: string

  // ── goal banner ──
  goalStatusActive: string
  goalStatusCompleted: string
  goalStatusBlocked: string
  goalStarted: string
  goalElapsed: string
  goalEdit: string
  goalEditTitle: string
  goalClear: string
  goalSaveFailed: string
  goalTokens: (used: number) => string
  goalTokensK: (k: string) => string
  durationSeconds: (n: number) => string
  durationMinutes: (m: number, s: number) => string
  durationHours: (h: number, m: number) => string

  // ── shared ──
  commonTokens: (used: string) => string
  commonLoading: string
  commonClose: string
  commonCancel: string
  commonSave: string
  commonDone: string
  commonEnable: string
  commonDisable: string
  commonRecommended: string
}
```

<a id="jcode-ui-providerinfo"></a>

### `ProviderInfo`

`interface` · `packages/jcode-ui/src/product/types.ts`

```ts
export interface ProviderInfo {
  id: string
  name: string
  /** Canonical provider family used for icon lookup. */
  kind?: string
  /** Desktop providers are direct; Cloud providers are routed via cloud_proxy. */
  source?: 'desktop' | 'cloud'
  scope?: 'cluster' | 'project'
  scope_id?: string
  scope_name?: string
  /** true for user-configured OpenAI-compatible providers. */
  custom?: boolean
  models: ModelInfo[]
}
```

<a id="jcode-ui-quoteselectionprops"></a>

### `QuoteSelectionProps`

`interface` · `packages/jcode-ui/src/components/QuoteSelection.tsx`

```ts
export interface QuoteSelectionProps {
  /** Receives the selected plain text when the user clicks Quote. */
  onQuote: (text: string) => void
  /** Button label. Default "Quote". */
  label?: string
  /** Max characters captured. Default 2000. */
  maxLength?: number
}
```

<a id="jcode-ui-reasoningoption"></a>

### `ReasoningOption`

`interface` · `packages/jcode-ui/src/product/types.ts`

ReasoningOption mirrors models.dev's reasoning_options.

```ts
export interface ReasoningOption {
  type: string
  values?: string[]
  min?: number
  max?: number
}
```

<a id="jcode-ui-reasoningprops"></a>

### `ReasoningProps`

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

<a id="jcode-ui-remotemeta"></a>

### `RemoteMeta`

`interface` · `packages/jcode-ui/src/product/types.ts`

```ts
export interface RemoteMeta {
  /** defaults to 'ssh' for back-compat. */
  kind?: RemoteKind
  /** host:port as dialed (ssh). */
  host: string
  user: string
  port: number
  remotePath: string
  /** docker container name/id. */
  container?: string
}
```

<a id="jcode-ui-runtimetasklistprops"></a>

### `RuntimeTaskListProps`

`interface` · `packages/jcode-ui/src/components/TaskList.tsx`

```ts
export interface RuntimeTaskListProps {
  title?: string
  compact?: boolean
  hideProgress?: boolean
  className?: string
}
```

<a id="jcode-ui-slashcommandinfo"></a>

### `SlashCommandInfo`

`interface` · `packages/jcode-ui/src/product/types.ts`

Unified slash command (built-in + skill + flow).

```ts
export interface SlashCommandInfo {
  slash: string
  description: string
  type: 'builtin' | 'skill' | 'flow'
}
```

<a id="jcode-ui-sourcesprops"></a>

### `SourcesProps`

`interface` · `packages/jcode-ui/src/components/Sources.tsx`

```ts
export interface SourcesProps {
  sources: MessageSource[]
}
```

<a id="jcode-ui-speechinputprops"></a>

### `SpeechInputProps`

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

<a id="jcode-ui-stackframe"></a>

### `StackFrame`

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

<a id="jcode-ui-stacktrace"></a>

### `StackTrace`

`interface` · `packages/jcode-ui/src/toolRenderers/stackTrace.tsx`

```ts
export interface StackTrace {
  kind: 'go' | 'js'
  message: string
  frames: StackFrame[]
}
```

<a id="jcode-ui-suggestionitem"></a>

### `SuggestionItem`

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

<a id="jcode-ui-suggestionsprops"></a>

### `SuggestionsProps`

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

<a id="jcode-ui-taskcontextbreakdown"></a>

### `TaskContextBreakdown` (jcode-ui)

`interface` · `packages/jcode-ui/src/product/types.ts`

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

<a id="jcode-ui-tasklistitemprops"></a>

### `TaskListItemProps`

`interface` · `packages/jcode-ui/src/components/TaskList.tsx`

```ts
export interface TaskListItemProps {
  item: TodoItem
  compact?: boolean
}
```

<a id="jcode-ui-tasklistprops"></a>

### `TaskListProps`

`interface` · `packages/jcode-ui/src/components/TaskList.tsx`

```ts
export interface TaskListProps {
  /** Ordered task items. */
  items: TodoItem[]
  /** Optional heading shown above the progress bar. */
  title?: string
  /** Denser rows + smaller type for embedding in tool cards. */
  compact?: boolean
  /** Hide the top progress bar. Default false. */
  hideProgress?: boolean
  /** Extra classes on the root. */
  className?: string
}
```

<a id="jcode-ui-taskstats"></a>

### `TaskStats`

`interface` · `packages/jcode-ui/src/product/types.ts`

```ts
export interface TaskStats {
  uuid: string
  is_active: boolean
  context?: TaskContextBreakdown
  cache_hit_rate: number
  cache_supported: boolean
  tokens: {
    total_tokens: number
    prompt_tokens: number
    completion_tokens: number
    cached_tokens: number
    reasoning_tokens: number
    calls: number
    turns?: number
  }
}
```

<a id="jcode-ui-testcase"></a>

### `TestCase`

`interface` · `packages/jcode-ui/src/toolRenderers/testResults.tsx`

```ts
export interface TestCase {
  name: string
  status: 'pass' | 'fail' | 'skip'
  durationMs?: number
  detail?: string
}
```

<a id="jcode-ui-testsummary"></a>

### `TestSummary`

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

<a id="jcode-ui-threadlistprops"></a>

### `ThreadListProps`

`interface` · `packages/jcode-ui/src/components/ThreadList.tsx`

```ts
export interface ThreadListProps {
  /** Optional small header label above the list (e.g. "Sessions"). */
  title?: string
  /** Extra class on the root (composed after `jcode-threadlist`). */
  className?: string
}
```

<a id="jcode-ui-threadprops"></a>

### `ThreadProps` (jcode-ui)

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
  /** Label for the default pending indicator ("Thinking"). Ignored when
   *  renderPending is set. The library has no i18n; hosts pass a translated
   *  string here. */
  pendingLabel?: string
  /** className passthrough for the scroll container. */
  className?: string
  /** Extra bottom padding (px) to clear a sticky composer. */
  overscanBottom?: number
  /** Hide pending ask_user tools before activity grouping when a host presents
   *  them in a dedicated interaction dock. Resolved receipts remain visible. */
  hidePendingAskUser?: boolean
  /** Host-localized completed-turn duration label. */
  turnDurationLabel?: (durationMs: number) => string
  /** Accessible label for expanding/collapsing completed-turn work. */
  turnExpandLabel?: string
  turnCollapseLabel?: string
}
```

<a id="jcode-ui-threadwelcomeprops"></a>

### `ThreadWelcomeProps`

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

<a id="jcode-ui-toolbatchgroupcardprops"></a>

### `ToolBatchGroupCardProps`

`interface` · `packages/jcode-ui/src/components/ToolBatchGroup.tsx`

```ts
export interface ToolBatchGroupCardProps {
  group: ToolBatchGroup
  className?: string
}
```

<a id="jcode-ui-toolcallcardprops"></a>

### `ToolCallCardProps`

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

<a id="jcode-ui-toolcallcardslots"></a>

### `ToolCallCardSlots`

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

<a id="jcode-ui-toolgraph"></a>

### `ToolGraph`

`interface` · `packages/jcode-ui/src/canvas/toolTreeToGraph.ts`

```ts
export interface ToolGraph {
  nodes: JcodeStepNode[]
  edges: Edge[]
}
```

<a id="jcode-ui-toolregistryproviderprops"></a>

### `ToolRegistryProviderProps`

`interface` · `packages/jcode-ui/src/components/ToolRegistryContext.tsx`

```ts
export interface ToolRegistryProviderProps {
  /** Defaults to createDefaultToolRegistry() if omitted. */
  registry?: ToolRendererRegistry
  children: ReactNode
}
```

<a id="jcode-ui-toolrowprops"></a>

### `ToolRowProps`

`interface` · `packages/jcode-ui/src/components/ToolRow.tsx`

```ts
export interface ToolRowProps {
  tool: ToolCall
  /** Row class — defaults to the shared `.jcode-toolbatch__row` styling. */
  className?: string
}
```

<a id="jcode-ui-tooltreetographoptions"></a>

### `ToolTreeToGraphOptions`

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

<a id="jcode-ui-transcriptionprops"></a>

### `TranscriptionProps`

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

<a id="jcode-ui-transcriptsegment"></a>

### `TranscriptSegment`

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

<a id="jcode-ui-turnchangescardprops"></a>

### `TurnChangesCardProps`

`interface` · `packages/jcode-ui/src/components/TurnChangesCard.tsx`

```ts
export interface TurnChangesCardProps {
  summary: TurnChangesSummary
  className?: string
}
```

<a id="jcode-ui-voicevisualizerprops"></a>

### `VoiceVisualizerProps`

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

<a id="jcode-ui-workflowcanvasprops"></a>

### `WorkflowCanvasProps`

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

<a id="jcode-ui-workspacetaskref"></a>

### `WorkspaceTaskRef`

`interface` · `packages/jcode-ui/src/product/types.ts`

Minimal task shape the workspace picker consumes.

```ts
export interface WorkspaceTaskRef {
  uuid: string
  project: string
  workspace_kind?: WorkspaceKind
  updated_at?: string
}
```

<a id="jcode-ui-agentmode"></a>

### `AgentMode`

`type` · `packages/jcode-ui/src/product/types.ts`

Session approval mode (unified across transports).

```ts
export type AgentMode = 'approval' | 'plan' | 'auto' | 'full_access';
```

<a id="jcode-ui-codeblockhook"></a>

### `CodeBlockHook`

`type` · `packages/jcode-ui/src/lib/markdown.ts`

Return HTML to fully replace the code block, or `null` to fall through.

```ts
export type CodeBlockHook = (args: CodeBlockHookArgs) => string | null;
```

<a id="jcode-ui-generatedimagestate"></a>

### `GeneratedImageState`

`type` · `packages/jcode-ui/src/components/GeneratedImageCard.tsx`

```ts
export type GeneratedImageState = Exclude<ToolPhase, 'terminal'> | ToolOutcome;
```

<a id="jcode-ui-jcodestepdata"></a>

### `JcodeStepData`

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

<a id="jcode-ui-jcodestepnode"></a>

### `JcodeStepNode`

`type` · `packages/jcode-ui/src/canvas/WorkflowNode.tsx`

Concrete node type for this renderer.

```ts
export type JcodeStepNode = Node<JcodeStepData, 'jcodeStep'>;
```

<a id="jcode-ui-jcodestepstatus"></a>

### `JcodeStepStatus`

`type` · `packages/jcode-ui/src/canvas/WorkflowNode.tsx`

Lifecycle of a step; a superset of the runtime `ToolStatus`.

```ts
export type JcodeStepStatus = 'pending' | 'running' | 'done' | 'error';
```

<a id="jcode-ui-mathrenderer"></a>

### `MathRenderer`

`type` · `packages/jcode-ui/src/lib/markdown.ts`

Render TeX to an HTML string. `displayMode` = block (`$$…$$`) vs inline (`$…$`).

```ts
export type MathRenderer = (tex: string, displayMode: boolean) => string;
```

<a id="jcode-ui-remotekind"></a>

### `RemoteKind`

`type` · `packages/jcode-ui/src/product/types.ts`

```ts
export type RemoteKind = 'ssh' | 'docker';
```

<a id="jcode-ui-remoteprefill"></a>

### `RemotePrefill`

`type` · `packages/jcode-ui/src/product/types.ts`

```ts
export type RemotePrefill = RemoteMeta & { loadTaskUuid?: string };
```

<a id="jcode-ui-speechinputstatus"></a>

### `SpeechInputStatus`

`type` · `packages/jcode-ui/src/voice/SpeechInput.tsx`

```ts
export type SpeechInputStatus = 'idle' | 'listening' | 'recording' | 'error';
```

<a id="jcode-ui-tooliconprops"></a>

### `ToolIconProps`

`type` · `packages/jcode-ui/src/components/toolIcons.tsx`

```ts
export type ToolIconProps = SVGProps<SVGSVGElement> & {
  /** Presentation kind from ToolDisplayInfo (read | search | list | shell | edit | agent | other). */
  kind?: string
  /** Raw tool name; used as fallback when kind is absent. */
  name?: string
};
```

<a id="jcode-ui-workspacekind"></a>

### `WorkspaceKind`

`type` · `packages/jcode-ui/src/product/types.ts`

```ts
export type WorkspaceKind = 'project' | 'scratch';
```

## `jcode-ui-core`

| Symbol | Kind | Source |
|--------|------|--------|
| [`ToolRendererRegistry`](#jcode-ui-core-toolrendererregistry) | class | `packages/jcode-ui-core/src/adapters/index.ts` |
| [`appendTurnChangeSummaries`](#jcode-ui-core-appendturnchangesummaries) | function | `packages/jcode-ui-core/src/timeline/turnChanges.ts` |
| [`ApprovalBlock`](#jcode-ui-core-approvalblock) | function | `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx` |
| [`AskUserBlock`](#jcode-ui-core-askuserblock) | function | `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx` |
| [`bindApprovalsToTools`](#jcode-ui-core-bindapprovalstotools) | function | `packages/jcode-ui-core/src/timeline/turnGroups.ts` |
| [`countActivityFlags`](#jcode-ui-core-countactivityflags) | function | `packages/jcode-ui-core/src/timeline/groupActivity.ts` |
| [`createAGUIRuntime`](#jcode-ui-core-createaguiruntime) | function | `packages/jcode-ui-core/src/runtime/aguiRuntime.ts` |
| [`createExternalStoreRuntime`](#jcode-ui-core-createexternalstoreruntime) | function | `packages/jcode-ui-core/src/runtime/externalStore.ts` |
| [`createFetchTransport`](#jcode-ui-core-createfetchtransport) | function | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`createInlineImageAdapter`](#jcode-ui-core-createinlineimageadapter) | function | `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts` |
| [`createMockRuntime`](#jcode-ui-core-createmockruntime) | function | `packages/jcode-ui-core/src/runtime/mockRuntime.ts` |
| [`createMockThreadStore`](#jcode-ui-core-createmockthreadstore) | function | `packages/jcode-ui-core/src/threads/store.ts` |
| [`createToolRendererRegistry`](#jcode-ui-core-createtoolrendererregistry) | function | `packages/jcode-ui-core/src/adapters/index.ts` |
| [`diffStatForTool`](#jcode-ui-core-diffstatfortool) | function | `packages/jcode-ui-core/src/timeline/turnChanges.ts` |
| [`exportThreadMarkdown`](#jcode-ui-core-exportthreadmarkdown) | function | `packages/jcode-ui-core/src/export/markdown.ts` |
| [`formatElapsed`](#jcode-ui-core-formatelapsed) | function | `packages/jcode-ui-core/src/hooks/index.ts` |
| [`getApprovalOutcome`](#jcode-ui-core-getapprovaloutcome) | function | `packages/jcode-ui-core/src/types/index.ts` |
| [`groupActivityTimeline`](#jcode-ui-core-groupactivitytimeline) | function | `packages/jcode-ui-core/src/timeline/groupActivity.ts` |
| [`groupCompletedTurns`](#jcode-ui-core-groupcompletedturns) | function | `packages/jcode-ui-core/src/timeline/turnGroups.ts` |
| [`groupExploringTimeline`](#jcode-ui-core-groupexploringtimeline) | function | `packages/jcode-ui-core/src/timeline/groupExploring.ts` |
| [`groupToolTimeline`](#jcode-ui-core-grouptooltimeline) | function | `packages/jcode-ui-core/src/timeline/groupExploring.ts` |
| [`isActivityItem`](#jcode-ui-core-isactivityitem) | function | `packages/jcode-ui-core/src/types/index.ts` |
| [`isApprovalItem`](#jcode-ui-core-isapprovalitem) | function | `packages/jcode-ui-core/src/types/index.ts` |
| [`isBatchItem`](#jcode-ui-core-isbatchitem) | function | `packages/jcode-ui-core/src/types/index.ts` |
| [`isCollapsibleTool`](#jcode-ui-core-iscollapsibletool) | function | `packages/jcode-ui-core/src/timeline/groupExploring.ts` |
| [`isExploringItem`](#jcode-ui-core-isexploringitem) | function | `packages/jcode-ui-core/src/types/index.ts` |
| [`isFileChangeTool`](#jcode-ui-core-isfilechangetool) | function | `packages/jcode-ui-core/src/timeline/turnChanges.ts` |
| [`isMessageItem`](#jcode-ui-core-ismessageitem) | function | `packages/jcode-ui-core/src/types/index.ts` |
| [`isStandaloneTool`](#jcode-ui-core-isstandalonetool) | function | `packages/jcode-ui-core/src/timeline/groupActivity.ts` |
| [`isToolItem`](#jcode-ui-core-istoolitem) | function | `packages/jcode-ui-core/src/types/index.ts` |
| [`isTurnChangesItem`](#jcode-ui-core-isturnchangesitem) | function | `packages/jcode-ui-core/src/types/index.ts` |
| [`isTurnItem`](#jcode-ui-core-isturnitem) | function | `packages/jcode-ui-core/src/types/index.ts` |
| [`MessageView`](#jcode-ui-core-messageview) | function | `packages/jcode-ui-core/src/primitives/MessageView.tsx` |
| [`nextAttachmentId`](#jcode-ui-core-nextattachmentid) | function | `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts` |
| [`normalizeState`](#jcode-ui-core-normalizestate) | function | `packages/jcode-ui-core/src/runtime/index.ts` |
| [`parseResolvedAnswers`](#jcode-ui-core-parseresolvedanswers) | function | `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx` |
| [`RuntimeProvider`](#jcode-ui-core-runtimeprovider) | function | `packages/jcode-ui-core/src/runtime/context.tsx` |
| [`summarizeActivityCounts`](#jcode-ui-core-summarizeactivitycounts) | function | `packages/jcode-ui-core/src/timeline/groupActivity.ts` |
| [`summarizeExploringCounts`](#jcode-ui-core-summarizeexploringcounts) | function | `packages/jcode-ui-core/src/timeline/groupExploring.ts` |
| [`summarizeExploringSteps`](#jcode-ui-core-summarizeexploringsteps) | function | `packages/jcode-ui-core/src/timeline/groupExploring.ts` |
| [`summarizeTurnChanges`](#jcode-ui-core-summarizeturnchanges) | function | `packages/jcode-ui-core/src/timeline/turnChanges.ts` |
| [`Thread` (jcode-ui-core)](#jcode-ui-core-thread) | function | `packages/jcode-ui-core/src/primitives/Thread.tsx` |
| [`ThreadStoreProvider`](#jcode-ui-core-threadstoreprovider) | function | `packages/jcode-ui-core/src/threads/context.tsx` |
| [`ToolCallProvider`](#jcode-ui-core-toolcallprovider) | function | `packages/jcode-ui-core/src/primitives/ToolCallView.tsx` |
| [`toolCallToRendererProps`](#jcode-ui-core-toolcalltorendererprops) | function | `packages/jcode-ui-core/src/adapters/index.ts` |
| [`ToolCallView`](#jcode-ui-core-toolcallview) | function | `packages/jcode-ui-core/src/primitives/ToolCallView.tsx` |
| [`useAutoScroll`](#jcode-ui-core-useautoscroll) | function | `packages/jcode-ui-core/src/hooks/index.ts` |
| [`useElapsed`](#jcode-ui-core-useelapsed) | function | `packages/jcode-ui-core/src/hooks/index.ts` |
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
| [`ActivityGroup`](#jcode-ui-core-activitygroup) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`AGUIMessage`](#jcode-ui-core-aguimessage) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`AGUIPatchOp`](#jcode-ui-core-aguipatchop) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`AGUIRunInput`](#jcode-ui-core-aguiruninput) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`AGUIRuntime`](#jcode-ui-core-aguiruntime) | interface | `packages/jcode-ui-core/src/runtime/aguiRuntime.ts` |
| [`AGUIRuntimeOptions`](#jcode-ui-core-aguiruntimeoptions) | interface | `packages/jcode-ui-core/src/runtime/aguiRuntime.ts` |
| [`AGUIToolCall`](#jcode-ui-core-aguitoolcall) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`AppendTurnChangesOptions`](#jcode-ui-core-appendturnchangesoptions) | interface | `packages/jcode-ui-core/src/timeline/turnChanges.ts` |
| [`Approval`](#jcode-ui-core-approval) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ApprovalBlockProps`](#jcode-ui-core-approvalblockprops) | interface | `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx` |
| [`ApprovalBlockRenderSlots`](#jcode-ui-core-approvalblockrenderslots) | interface | `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx` |
| [`ApprovalDecisionActions`](#jcode-ui-core-approvaldecisionactions) | interface | `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx` |
| [`ApprovalOption`](#jcode-ui-core-approvaloption) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ArtifactRef`](#jcode-ui-core-artifactref) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`AskUserAnswer`](#jcode-ui-core-askuseranswer) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`AskUserBlockProps`](#jcode-ui-core-askuserblockprops) | interface | `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx` |
| [`AskUserBlockRenderSlots`](#jcode-ui-core-askuserblockrenderslots) | interface | `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx` |
| [`AskUserControls`](#jcode-ui-core-askusercontrols) | interface | `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx` |
| [`AskUserOption`](#jcode-ui-core-askuseroption) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`AskUserQuestion`](#jcode-ui-core-askuserquestion) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`AskUserState`](#jcode-ui-core-askuserstate) | interface | `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx` |
| [`AttachmentAdapter`](#jcode-ui-core-attachmentadapter) | interface | `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts` |
| [`BillableApprovalSummary`](#jcode-ui-core-billableapprovalsummary) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ChatImage`](#jcode-ui-core-chatimage) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ChatRuntime`](#jcode-ui-core-chatruntime) | interface | `packages/jcode-ui-core/src/runtime/index.ts` |
| [`CompletedTurn`](#jcode-ui-core-completedturn) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ComposerHandle`](#jcode-ui-core-composerhandle) | interface | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`ComposerProps`](#jcode-ui-core-composerprops) | interface | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`ComposerRenderSlots`](#jcode-ui-core-composerrenderslots) | interface | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`CustomEvent`](#jcode-ui-core-customevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`DictationState`](#jcode-ui-core-dictationstate) | interface | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`ExploringGroup`](#jcode-ui-core-exploringgroup) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ExportMarkdownOptions`](#jcode-ui-core-exportmarkdownoptions) | interface | `packages/jcode-ui-core/src/export/markdown.ts` |
| [`ExternalStoreRuntimeOptions`](#jcode-ui-core-externalstoreruntimeoptions) | interface | `packages/jcode-ui-core/src/runtime/externalStore.ts` |
| [`Goal`](#jcode-ui-core-goal) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`GroupCompletedTurnsOptions`](#jcode-ui-core-groupcompletedturnsoptions) | interface | `packages/jcode-ui-core/src/timeline/turnGroups.ts` |
| [`InlineImageAdapterOptions`](#jcode-ui-core-inlineimageadapteroptions) | interface | `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts` |
| [`Message` (jcode-ui-core)](#jcode-ui-core-message) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`MessageActions`](#jcode-ui-core-messageactions) | interface | `packages/jcode-ui-core/src/primitives/MessageView.tsx` |
| [`MessageSource`](#jcode-ui-core-messagesource) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`MessagesSnapshotEvent`](#jcode-ui-core-messagessnapshotevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`MessageVersion`](#jcode-ui-core-messageversion) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`MessageViewProps`](#jcode-ui-core-messageviewprops) | interface | `packages/jcode-ui-core/src/primitives/MessageView.tsx` |
| [`MessageViewRenderSlots`](#jcode-ui-core-messageviewrenderslots) | interface | `packages/jcode-ui-core/src/primitives/MessageView.tsx` |
| [`MockRuntimeOptions`](#jcode-ui-core-mockruntimeoptions) | interface | `packages/jcode-ui-core/src/runtime/mockRuntime.ts` |
| [`MockThreadStore`](#jcode-ui-core-mockthreadstore) | interface | `packages/jcode-ui-core/src/threads/store.ts` |
| [`PendingAttachment`](#jcode-ui-core-pendingattachment) | interface | `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts` |
| [`PendingAttachmentItem`](#jcode-ui-core-pendingattachmentitem) | interface | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`QueuedMessage`](#jcode-ui-core-queuedmessage) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`RawEvent`](#jcode-ui-core-rawevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`ReasoningMessageChunkEvent`](#jcode-ui-core-reasoningmessagechunkevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`ReasoningMessageContentEvent`](#jcode-ui-core-reasoningmessagecontentevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`RunErrorEvent`](#jcode-ui-core-runerrorevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`RunFinishedEvent`](#jcode-ui-core-runfinishedevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`RunStartedEvent`](#jcode-ui-core-runstartedevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`RuntimeActions`](#jcode-ui-core-runtimeactions) | interface | `packages/jcode-ui-core/src/runtime/index.ts` |
| [`RuntimeProviderProps`](#jcode-ui-core-runtimeproviderprops) | interface | `packages/jcode-ui-core/src/runtime/context.tsx` |
| [`RuntimeState`](#jcode-ui-core-runtimestate) | interface | `packages/jcode-ui-core/src/runtime/index.ts` |
| [`SlashCommand`](#jcode-ui-core-slashcommand) | interface | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`SlashMenuState`](#jcode-ui-core-slashmenustate) | interface | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`StateDeltaEvent`](#jcode-ui-core-statedeltaevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`StateSnapshotEvent`](#jcode-ui-core-statesnapshotevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`StepFinishedEvent`](#jcode-ui-core-stepfinishedevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`StepStartedEvent`](#jcode-ui-core-stepstartedevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`SummarizeTurnChangesOptions`](#jcode-ui-core-summarizeturnchangesoptions) | interface | `packages/jcode-ui-core/src/timeline/turnChanges.ts` |
| [`TaskContextBreakdown` (jcode-ui-core)](#jcode-ui-core-taskcontextbreakdown) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`TextMessageChunkEvent`](#jcode-ui-core-textmessagechunkevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`TextMessageContentEvent`](#jcode-ui-core-textmessagecontentevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`TextMessageEndEvent`](#jcode-ui-core-textmessageendevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`TextMessageStartEvent`](#jcode-ui-core-textmessagestartevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`ThreadListState`](#jcode-ui-core-threadliststate) | interface | `packages/jcode-ui-core/src/threads/store.ts` |
| [`ThreadProps` (jcode-ui-core)](#jcode-ui-core-threadprops) | interface | `packages/jcode-ui-core/src/primitives/Thread.tsx` |
| [`ThreadRenderSlots`](#jcode-ui-core-threadrenderslots) | interface | `packages/jcode-ui-core/src/primitives/Thread.tsx` |
| [`ThreadStore`](#jcode-ui-core-threadstore) | interface | `packages/jcode-ui-core/src/threads/store.ts` |
| [`ThreadStoreActions`](#jcode-ui-core-threadstoreactions) | interface | `packages/jcode-ui-core/src/threads/store.ts` |
| [`ThreadStoreProviderProps`](#jcode-ui-core-threadstoreproviderprops) | interface | `packages/jcode-ui-core/src/threads/context.tsx` |
| [`ThreadSummary`](#jcode-ui-core-threadsummary) | interface | `packages/jcode-ui-core/src/threads/store.ts` |
| [`TodoItem`](#jcode-ui-core-todoitem) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`TokenSnapshot`](#jcode-ui-core-tokensnapshot) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolBatchGroup`](#jcode-ui-core-toolbatchgroup) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolCall`](#jcode-ui-core-toolcall) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolCallArgsEvent`](#jcode-ui-core-toolcallargsevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`ToolCallChunkEvent`](#jcode-ui-core-toolcallchunkevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`ToolCallContextValue`](#jcode-ui-core-toolcallcontextvalue) | interface | `packages/jcode-ui-core/src/primitives/ToolCallView.tsx` |
| [`ToolCallEndEvent`](#jcode-ui-core-toolcallendevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`ToolCallResultEvent`](#jcode-ui-core-toolcallresultevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`ToolCallStartEvent`](#jcode-ui-core-toolcallstartevent) | interface | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`ToolCallViewProps`](#jcode-ui-core-toolcallviewprops) | interface | `packages/jcode-ui-core/src/primitives/ToolCallView.tsx` |
| [`ToolDisplayInfo`](#jcode-ui-core-tooldisplayinfo) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolMeta`](#jcode-ui-core-toolmeta) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolPresentation`](#jcode-ui-core-toolpresentation) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolRendererProps`](#jcode-ui-core-toolrendererprops) | interface | `packages/jcode-ui-core/src/adapters/index.ts` |
| [`ToolStreams`](#jcode-ui-core-toolstreams) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`TurnChangesSummary`](#jcode-ui-core-turnchangessummary) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`TurnFileChange`](#jcode-ui-core-turnfilechange) | interface | `packages/jcode-ui-core/src/types/index.ts` |
| [`AGUIEvent`](#jcode-ui-core-aguievent) | type | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`AGUIRole`](#jcode-ui-core-aguirole) | type | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`AGUITransport`](#jcode-ui-core-aguitransport) | type | `packages/jcode-ui-core/src/runtime/aguiEvents.ts` |
| [`ApprovalOptionKind`](#jcode-ui-core-approvaloptionkind) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`ApprovalOutcome`](#jcode-ui-core-approvaloutcome) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`ArtifactStorageKind`](#jcode-ui-core-artifactstoragekind) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`AskUserSubmitError`](#jcode-ui-core-askusersubmiterror) | type | `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx` |
| [`ConnectionState`](#jcode-ui-core-connectionstate) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`GoalStatus`](#jcode-ui-core-goalstatus) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`MockThreadStoreSeed`](#jcode-ui-core-mockthreadstoreseed) | type | `packages/jcode-ui-core/src/threads/store.ts` |
| [`PartialRuntimeState`](#jcode-ui-core-partialruntimestate) | type | `packages/jcode-ui-core/src/runtime/index.ts` |
| [`PendingStatus`](#jcode-ui-core-pendingstatus) | type | `packages/jcode-ui-core/src/primitives/Composer.tsx` |
| [`Role`](#jcode-ui-core-role) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`SystemLevel`](#jcode-ui-core-systemlevel) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`ThreadItem`](#jcode-ui-core-threaditem) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`ThreadItemKind`](#jcode-ui-core-threaditemkind) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolOutcome`](#jcode-ui-core-tooloutcome) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolPhase`](#jcode-ui-core-toolphase) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolRenderer`](#jcode-ui-core-toolrenderer) | type | `packages/jcode-ui-core/src/adapters/index.ts` |
| [`ToolStatus`](#jcode-ui-core-toolstatus) | type | `packages/jcode-ui-core/src/types/index.ts` |
| [`ToolSurface`](#jcode-ui-core-toolsurface) | type | `packages/jcode-ui-core/src/types/index.ts` |

<a id="jcode-ui-core-toolrendererregistry"></a>

### `ToolRendererRegistry`

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

<a id="jcode-ui-core-appendturnchangesummaries"></a>

### `appendTurnChangeSummaries`

`function` · `packages/jcode-ui-core/src/timeline/turnChanges.ts`

Insert a `turnchanges` item at the end of every completed turn.

Turn boundary = a user message up to (exclusive) the next user message.
Items before the first user message belong to no turn. The synthetic item's
seq is `last item seq + 0.5` — stable across re-renders and collision-free
against integer seqs. Intended as the LAST step of a `mapItems` pipeline
(after `groupToolTimeline`).

```ts
export function appendTurnChangeSummaries(
  items: ThreadItem[],
  opts: AppendTurnChangesOptions = {},
): ThreadItem[] { … }
```

<a id="jcode-ui-core-approvalblock"></a>

### `ApprovalBlock`

`function` · `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx`

```ts
export function ApprovalBlock({ approval, className, renderPending, renderResolved }: ApprovalBlockProps): ReactNode { … }
```

<a id="jcode-ui-core-askuserblock"></a>

### `AskUserBlock`

`function` · `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx`

```ts
export function AskUserBlock({ tool, className, renderPending, renderResolved }: AskUserBlockProps): ReactNode { … }
```

<a id="jcode-ui-core-bindapprovalstotools"></a>

### `bindApprovalsToTools`

`function` · `packages/jcode-ui-core/src/timeline/turnGroups.ts`

Attach approval items to the concrete tool-call occurrence they gate.

Host-generated approval occurrence ids are authoritative across the current
user turn. Legacy calls without one fall back to the nearest unmatched model
id + tool-name occurrence in that turn (normally the preceding tool; the
forward fallback covers transports that deliver approval before tool_call).
Matched approval items disappear from the projected list; ambiguous or
unmatched approvals remain standalone so no decision UI is lost.

```ts
export function bindApprovalsToTools(items: ThreadItem[]): ThreadItem[] { … }
```

<a id="jcode-ui-core-countactivityflags"></a>

### `countActivityFlags`

`function` · `packages/jcode-ui-core/src/timeline/groupActivity.ts`

Count the collapsed-header suffix flags of an activity group. `failed` =
errored or nonzero exit code (denied tools excluded — a user decision is
not a failure); `denied` = rejected at the approval prompt.

```ts
export function countActivityFlags(tools: ToolCall[]): { … }
```

<a id="jcode-ui-core-createaguiruntime"></a>

### `createAGUIRuntime`

`function` · `packages/jcode-ui-core/src/runtime/aguiRuntime.ts`

```ts
export function createAGUIRuntime(options: AGUIRuntimeOptions): AGUIRuntime { … }
```

<a id="jcode-ui-core-createexternalstoreruntime"></a>

### `createExternalStoreRuntime`

`function` · `packages/jcode-ui-core/src/runtime/externalStore.ts`

Wrap an external store as a ChatRuntime. The returned object's `getState`
always returns a fully-populated RuntimeState (missing slices defaulted), and
returns the SAME object reference between dispatches (so it's safe to pass to
useSyncExternalStore's getSnapshot).

```ts
export function createExternalStoreRuntime<THostState>(
  opts: ExternalStoreRuntimeOptions<THostState>,
): ChatRuntime { … }
```

<a id="jcode-ui-core-createfetchtransport"></a>

### `createFetchTransport`

`function` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

The default transport: HTTP POST + `text/event-stream`. Built from the
runtime's `url`/`headers` and closed over so the `AGUITransport` it returns
matches the `(input, signal)` shape tests use.

```ts
export function createFetchTransport(
  url: string,
  headers?: Record<string, string>,
): AGUITransport { … }
```

<a id="jcode-ui-core-createinlineimageadapter"></a>

### `createInlineImageAdapter`

`function` · `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts`

The default adapter: reads image files into base64 (the existing ChatImage
behavior) and reports an error for non-images or oversize files. Keeps the
`sendMessage(text, images)` fast-path working with zero host wiring.

```ts
export function createInlineImageAdapter(options: InlineImageAdapterOptions = {}): AttachmentAdapter { … }
```

<a id="jcode-ui-core-createmockruntime"></a>

### `createMockRuntime`

`function` · `packages/jcode-ui-core/src/runtime/mockRuntime.ts`

Create a ChatRuntime backed by an in-memory store with pub/sub. Exposes
imperative mutators (`setItems`, `push`, `appendText`, `setRunning`, `patchState`)
so a script driver (or a test / docs demo) can evolve the state over time.

```ts
export function createMockRuntime(opts: MockRuntimeOptions = {}): ChatRuntime & { … }
```

<a id="jcode-ui-core-createmockthreadstore"></a>

### `createMockThreadStore`

`function` · `packages/jcode-ui-core/src/threads/store.ts`

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

<a id="jcode-ui-core-createtoolrendererregistry"></a>

### `createToolRendererRegistry`

`function` · `packages/jcode-ui-core/src/adapters/index.ts`

Create a fresh registry. Convenience over `new` for chained registration.

```ts
export function createToolRendererRegistry(): ToolRendererRegistry { … }
```

<a id="jcode-ui-core-diffstatfortool"></a>

### `diffStatForTool`

`function` · `packages/jcode-ui-core/src/timeline/turnChanges.ts`

Derive ±line counts from edit/write args. Returns null when the args carry
no countable diff text (then the summary lists the file without counts).
Mirrors ToolCallCard's badge heuristic: line counts of old/new strings,
`write` counts content lines as additions (prior content is unknown).

```ts
export function diffStatForTool(tool: ToolCall): { … }
```

<a id="jcode-ui-core-exportthreadmarkdown"></a>

### `exportThreadMarkdown`

`function` · `packages/jcode-ui-core/src/export/markdown.ts`

```ts
exportThreadMarkdown(items: ThreadItem[], opts: ExportMarkdownOptions = {}): string { … }
```

<a id="jcode-ui-core-formatelapsed"></a>

### `formatElapsed`

`function` · `packages/jcode-ui-core/src/hooks/index.ts`

Format elapsed/duration ms as a compact badge: `2s`, `1m 05s`.

```ts
export function formatElapsed(ms: number): string { … }
```

<a id="jcode-ui-core-getapprovaloutcome"></a>

### `getApprovalOutcome`

`function` · `packages/jcode-ui-core/src/types/index.ts`

Resolve the effective approval outcome across the classic boolean contract
and host-defined option contract. A matching selected option is
authoritative because option-based hosts are not required to also populate
the optional `approved` field. Invalid resolved states fail closed.

```ts
export function getApprovalOutcome(approval: Approval): ApprovalOutcome { … }
```

<a id="jcode-ui-core-groupactivitytimeline"></a>

### `groupActivityTimeline`

`function` · `packages/jcode-ui-core/src/timeline/groupActivity.ts`

Coalesce adjacent tool items (and whole `batchId` batches) into `activity`
groups. Output contains only `'activity'` items (≥2 tools) and plain
`'tool'` items (isolated singles) — never `'exploring'` or `'batch'`.

- Batch members are anchored at the FIRST member's position even when
  approvals (or anything else) sit between them.
- Approvals never break a group; they render right after the group anchor.
- Any other item (message, turnchanges, …) closes the open group.

```ts
export function groupActivityTimeline(items: ThreadItem[]): ThreadItem[] { … }
```

<a id="jcode-ui-core-groupcompletedturns"></a>

### `groupCompletedTurns`

`function` · `packages/jcode-ui-core/src/timeline/turnGroups.ts`

Replace each settled user turn with:

  user message -> collapsed activity/duration row + final assistant summary
               -> turn-changes summary (when present)

A turn is left flat when it has no final assistant reply, contains unresolved
approval/running work, or has tool activity after its last assistant message.

```ts
export function groupCompletedTurns(
  items: ThreadItem[],
  options: GroupCompletedTurnsOptions = {},
): ThreadItem[] { … }
```

<a id="jcode-ui-core-groupexploringtimeline"></a>

### `groupExploringTimeline`

`function` · `packages/jcode-ui-core/src/timeline/groupExploring.ts`

Collapse consecutive collapsible tools into exploring groups.
Non-tool items and non-collapsible tools always break a group.
@deprecated Superseded by `groupActivityTimeline` (activity groups coalesce
ALL adjacent tools, not just read-only ones). Kept for external consumers.

```ts
export function groupExploringTimeline(items: ThreadItem[]): ThreadItem[] { … }
```

<a id="jcode-ui-core-grouptooltimeline"></a>

### `groupToolTimeline`

`function` · `packages/jcode-ui-core/src/timeline/groupExploring.ts`

Coalesce tool calls that share a `batchId` (concurrent calls from one
assistant message) into `batch` items anchored at the first member's
position. Items in between (approvals, messages) stay in place and do NOT
break a batch. Single-member batches unwrap back to plain tool cards, and
tools without a batchId (old sessions / replay) keep the existing
exploring-adjacent coalescing behavior unchanged.
@deprecated Superseded by `groupActivityTimeline`, which absorbs the batch
coalescing and merges ALL adjacent tools/batches into `activity` groups.
Kept for external consumers (and nested subagent-children rendering).

```ts
export function groupToolTimeline(items: ThreadItem[]): ThreadItem[] { … }
```

<a id="jcode-ui-core-isactivityitem"></a>

### `isActivityItem`

`function` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export function isActivityItem(i: ThreadItem): i is Extract<ThreadItem, { … }
```

<a id="jcode-ui-core-isapprovalitem"></a>

### `isApprovalItem`

`function` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export function isApprovalItem(i: ThreadItem): i is Extract<ThreadItem, { … }
```

<a id="jcode-ui-core-isbatchitem"></a>

### `isBatchItem`

`function` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export function isBatchItem(i: ThreadItem): i is Extract<ThreadItem, { … }
```

<a id="jcode-ui-core-iscollapsibletool"></a>

### `isCollapsibleTool`

`function` · `packages/jcode-ui-core/src/timeline/groupExploring.ts`

True when a tool should join an Exploring/Explored group.

```ts
export function isCollapsibleTool(tool: ToolCall): boolean { … }
```

<a id="jcode-ui-core-isexploringitem"></a>

### `isExploringItem`

`function` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export function isExploringItem(i: ThreadItem): i is Extract<ThreadItem, { … }
```

<a id="jcode-ui-core-isfilechangetool"></a>

### `isFileChangeTool`

`function` · `packages/jcode-ui-core/src/timeline/turnChanges.ts`

True when a tool is an edit/write/patch-style file mutation.

```ts
export function isFileChangeTool(tool: ToolCall): boolean { … }
```

<a id="jcode-ui-core-ismessageitem"></a>

### `isMessageItem`

`function` · `packages/jcode-ui-core/src/types/index.ts`

Type guard helpers (kept generic so consumers can narrow item arrays).

```ts
export function isMessageItem(i: ThreadItem): i is Extract<ThreadItem, { … }
```

<a id="jcode-ui-core-isstandalonetool"></a>

### `isStandaloneTool`

`function` · `packages/jcode-ui-core/src/timeline/groupActivity.ts`

Standalone tools own their complete timeline surface and are hard grouping
boundaries from the initial tool_call event onward.

```ts
export function isStandaloneTool(tool: ToolCall): boolean { … }
```

<a id="jcode-ui-core-istoolitem"></a>

### `isToolItem`

`function` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export function isToolItem(i: ThreadItem): i is Extract<ThreadItem, { … }
```

<a id="jcode-ui-core-isturnchangesitem"></a>

### `isTurnChangesItem`

`function` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export function isTurnChangesItem(i: ThreadItem): i is Extract<ThreadItem, { … }
```

<a id="jcode-ui-core-isturnitem"></a>

### `isTurnItem`

`function` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export function isTurnItem(i: ThreadItem): i is Extract<ThreadItem, { … }
```

<a id="jcode-ui-core-messageview"></a>

### `MessageView`

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

<a id="jcode-ui-core-nextattachmentid"></a>

### `nextAttachmentId`

`function` · `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts`

Monotonic, collision-resistant id for composer-assigned attachments.

```ts
export function nextAttachmentId(): string { … }
```

<a id="jcode-ui-core-normalizestate"></a>

### `normalizeState`

`function` · `packages/jcode-ui-core/src/runtime/index.ts`

Merge a partial state onto the default empty state. Missing slices get
 safe defaults so components never have to null-check.

```ts
export function normalizeState(partial: PartialRuntimeState | undefined): RuntimeState { … }
```

<a id="jcode-ui-core-parseresolvedanswers"></a>

### `parseResolvedAnswers`

`function` · `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx`

Best-effort parse of a resolved tool's output into answers (for replay).

```ts
export function parseResolvedAnswers(tool: ToolCall): AskUserAnswer[] { … }
```

<a id="jcode-ui-core-runtimeprovider"></a>

### `RuntimeProvider`

`function` · `packages/jcode-ui-core/src/runtime/context.tsx`

Provide a ChatRuntime to a subtree. Components under it read state/actions
 via `useRuntimeState` / `useRuntimeSelector` / `useRuntimeActions`.

```ts
export function RuntimeProvider({ runtime, children }: RuntimeProviderProps): ReactNode { … }
```

<a id="jcode-ui-core-summarizeactivitycounts"></a>

### `summarizeActivityCounts`

`function` · `packages/jcode-ui-core/src/timeline/groupActivity.ts`

Bucket an activity group's tools into a compact category-count header.

- Mixed groups: `Ran 3 commands · read 2 files · ran 1 agent` (verb
  phrases, first segment capitalized).
- All-read-only groups (every tool passes `isCollapsibleTool`): the
  Explored phrasing `3 files read · 2 searches · 1 list` — the card
  prefixes its own `Explored` label.

Reads and edits dedupe by file (`displayInfo.subtitle`) so re-touching the
same file counts once.

```ts
export function summarizeActivityCounts(tools: ToolCall[]): string { … }
```

<a id="jcode-ui-core-summarizeexploringcounts"></a>

### `summarizeExploringCounts`

`function` · `packages/jcode-ui-core/src/timeline/groupExploring.ts`

Bucket exploring steps into a compact category-count summary, e.g.
`3 files read · 2 searches · 1 list`. Read counts dedupe by file name
(subtitle) so re-reads of the same file count once.

```ts
export function summarizeExploringCounts(tools: ToolCall[]): string { … }
```

<a id="jcode-ui-core-summarizeexploringsteps"></a>

### `summarizeExploringSteps`

`function` · `packages/jcode-ui-core/src/timeline/groupExploring.ts`

Summarize an exploring group into action lines (Read / Search / List …).

```ts
export function summarizeExploringSteps(tools: ToolCall[]): { … }
```

<a id="jcode-ui-core-summarizeturnchanges"></a>

### `summarizeTurnChanges`

`function` · `packages/jcode-ui-core/src/timeline/turnChanges.ts`

Aggregate the file changes of one turn's items.

Returns null when the turn has no completed file changes OR any tool in the
turn is still running (work in progress — the summary only appears once the
turn settles). Denied and errored change tools are skipped (they did not
touch the file). Files dedupe by path keeping the LAST change; totals sum
over the deduped set.

```ts
export function summarizeTurnChanges(
  items: ThreadItem[],
  opts: SummarizeTurnChangesOptions = {},
): TurnChangesSummary | null { … }
```

<a id="jcode-ui-core-thread"></a>

### `Thread` (jcode-ui-core)

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

<a id="jcode-ui-core-threadstoreprovider"></a>

### `ThreadStoreProvider`

`function` · `packages/jcode-ui-core/src/threads/context.tsx`

Provide a `ThreadStore` to a subtree. `ThreadList` reads it via the hooks.

```ts
export function ThreadStoreProvider({ store, children }: ThreadStoreProviderProps): ReactNode { … }
```

<a id="jcode-ui-core-toolcallprovider"></a>

### `ToolCallProvider`

`function` · `packages/jcode-ui-core/src/primitives/ToolCallView.tsx`

```ts
export function ToolCallProvider({ value, children }: { value: ToolCallContextValue; children: ReactNode }) { … }
```

<a id="jcode-ui-core-toolcalltorendererprops"></a>

### `toolCallToRendererProps`

`function` · `packages/jcode-ui-core/src/adapters/index.ts`

Map a ToolCall to the renderer contract. Shared by the collapsible shell
and standalone timeline surfaces so lifecycle fields cannot drift.

```ts
export function toolCallToRendererProps(tool: ToolCall): ToolRendererProps { … }
```

<a id="jcode-ui-core-toolcallview"></a>

### `ToolCallView`

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

<a id="jcode-ui-core-useautoscroll"></a>

### `useAutoScroll`

`function` · `packages/jcode-ui-core/src/hooks/index.ts`

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

<a id="jcode-ui-core-useelapsed"></a>

### `useElapsed`

`function` · `packages/jcode-ui-core/src/hooks/index.ts`

Live elapsed milliseconds since `startedAt` (unix ms), ticking once per
second while `active`. When inactive (or `startedAt` is missing) the
interval is not scheduled — mount this only where a live badge is shown
(e.g. a running batch row) to keep timers scarce.

```ts
export function useElapsed(startedAt: number | undefined, active = true): number { … }
```

<a id="jcode-ui-core-usefocusonidle"></a>

### `useFocusOnIdle`

`function` · `packages/jcode-ui-core/src/hooks/index.ts`

Auto-focus a ref on mount and when `isRunning` flips false (the Vue version
refocuses the composer when a turn ends).

```ts
export function useFocusOnIdle<T extends HTMLElement>(isRunning: boolean) { … }
```

<a id="jcode-ui-core-useisatbottom"></a>

### `useIsAtBottom`

`function` · `packages/jcode-ui-core/src/hooks/index.ts`

Re-render-friendly version of the at-bottom flag: re-renders the component
when the flag flips. Use sparingly (the scroll handler runs a lot); for most
cases the imperative `getIsAtBottom` + an effect is enough.

NOTE: this intentionally tracks a coarse boolean — it only re-renders on
crossing the threshold, not on every scroll event.

```ts
export function useIsAtBottom<T extends HTMLElement>(threshold = 80) { … }
```

<a id="jcode-ui-core-usequeuedmessages"></a>

### `useQueuedMessages`

`function` · `packages/jcode-ui-core/src/hooks/index.ts`

Track + drain the type-ahead queue: returns the current queued messages.
Draining is the runtime's job (it sends the next queued message on each turn
end); this hook just surfaces the queue for rendering.

```ts
export function useQueuedMessages() { … }
```

<a id="jcode-ui-core-useruntimeactions"></a>

### `useRuntimeActions`

`function` · `packages/jcode-ui-core/src/runtime/context.tsx`

Stable handle to the action bag. Identity is owned by the runtime.

```ts
export function useRuntimeActions() { … }
```

<a id="jcode-ui-core-useruntimeselector"></a>

### `useRuntimeSelector`

`function` · `packages/jcode-ui-core/src/runtime/context.tsx`

Subscribe to a derived slice of RuntimeState. The selector MUST be stable
(memoize with useCallback) or return a primitive; otherwise React will
re-render on every store change. For object returns, also pass an `isEqual`
(e.g. shallow-equal) to avoid identity churn.

```ts
export function useRuntimeSelector<T>(
  selector: (state: RuntimeState) => T,
  isEqual: (a: T, b: T) => boolean = Object.is,
): T { … }
```

<a id="jcode-ui-core-useruntimestate"></a>

### `useRuntimeState`

`function` · `packages/jcode-ui-core/src/runtime/context.tsx`

Subscribe to the full RuntimeState. Re-renders on any store change. Prefer
`useRuntimeSelector` for granular reads to avoid re-rendering on unrelated
state changes.

```ts
export function useRuntimeState(): RuntimeState { … }
```

<a id="jcode-ui-core-usestreamfollow"></a>

### `useStreamFollow`

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

<a id="jcode-ui-core-usethreadliststate"></a>

### `useThreadListState`

`function` · `packages/jcode-ui-core/src/threads/context.tsx`

Subscribe to the full `ThreadListState`. Re-renders on any list change.

```ts
export function useThreadListState(): ThreadListState { … }
```

<a id="jcode-ui-core-usethreadstoreactions"></a>

### `useThreadStoreActions`

`function` · `packages/jcode-ui-core/src/threads/context.tsx`

Stable handle to the thread-list action bag (identity owned by the store).

```ts
export function useThreadStoreActions(): ThreadStoreActions { … }
```

<a id="jcode-ui-core-usetoolcallcontext"></a>

### `useToolCallContext`

`function` · `packages/jcode-ui-core/src/primitives/ToolCallView.tsx`

```ts
export function useToolCallContext(): ToolCallContextValue | null { … }
```

<a id="jcode-ui-core-activitygroup"></a>

### `ActivityGroup`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A UI-only group of ADJACENT tool calls in the timeline (adjacent = no
assistant/user message in between; approvals do NOT break adjacency and
render in place). Collapsed (all members settled) it shows one category-
count header line; expanded it is a bordered row-stack card whose rows
expand in place to each tool's registry-rendered body. Supersedes the
`exploring` and `batch` kinds. Does not change model-facing boundaries.

```ts
export interface ActivityGroup {
  id: string
  tools: ToolCall[]
  status: ToolStatus
  /** True when ALL tools are read-only (per `isCollapsibleTool`). */
  explorative: boolean
}
```

<a id="jcode-ui-core-aguimessage"></a>

### `AGUIMessage`

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

<a id="jcode-ui-core-aguipatchop"></a>

### `AGUIPatchOp`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

A single RFC 6902 JSON Patch operation (STATE_DELTA payload element).

```ts
export interface AGUIPatchOp {
  op: 'add' | 'replace' | 'remove' | 'move' | 'copy' | 'test' | string
  path: string
  value?: unknown
  from?: string
}
```

<a id="jcode-ui-core-aguiruninput"></a>

### `AGUIRunInput`

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

<a id="jcode-ui-core-aguiruntime"></a>

### `AGUIRuntime`

`interface` · `packages/jcode-ui-core/src/runtime/aguiRuntime.ts`

The AG-UI runtime adds read-only agent-state access to the base contract.

```ts
export interface AGUIRuntime extends ChatRuntime {
  /** The latest STATE_SNAPSHOT with STATE_DELTA patches applied, or undefined. */
  getAgentState: () => unknown
}
```

<a id="jcode-ui-core-aguiruntimeoptions"></a>

### `AGUIRuntimeOptions`

`interface` · `packages/jcode-ui-core/src/runtime/aguiRuntime.ts`

```ts
export interface AGUIRuntimeOptions {
  /** AG-UI run endpoint (POST, streams `text/event-stream`). */
  url: string
  /** Extra request headers (auth, etc.) for the default transport. */
  headers?: Record<string, string>
  /** Override the event source — inject a scripted stream in tests, or swap in
   *  WebSocket/other transports. Defaults to `createFetchTransport(url, headers)`. */
  transport?: AGUITransport
  /** Stable thread id for the whole session. Auto-generated when omitted. */
  threadId?: string
}
```

<a id="jcode-ui-core-aguitoolcall"></a>

### `AGUIToolCall`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

A tool call embedded in an AG-UI assistant message (OpenAI-shaped).

```ts
export interface AGUIToolCall {
  id: string
  type?: string
  function: { name: string; arguments: string }
}
```

<a id="jcode-ui-core-appendturnchangesoptions"></a>

### `AppendTurnChangesOptions`

`interface` · `packages/jcode-ui-core/src/timeline/turnChanges.ts`

```ts
export interface AppendTurnChangesOptions extends SummarizeTurnChangesOptions {
  /** While the runtime is streaming, the LAST turn is still open — suppress
   *  its summary even if no tool is currently running. */
  isRunning?: boolean
}
```

<a id="jcode-ui-core-approval"></a>

### `Approval`

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
  /** Backend tool_call_id of the gated call — lets the host mark the exact
   *  pending tool row as awaiting approval (warning color). */
  tool_call_id?: string
  /** Target outside the workspace root — UI flags it prominently. */
  is_external: boolean
  /** Policy class supplied by the backend (e.g. billable_external). */
  approvalClass?: string
  /** Structured, non-secret summary for billable approval copy. */
  billableSummary?: BillableApprovalSummary
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

<a id="jcode-ui-core-approvalblockprops"></a>

### `ApprovalBlockProps`

`interface` · `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx`

```ts
export interface ApprovalBlockProps extends ApprovalBlockRenderSlots {
  approval: Approval
  /** className passthrough. */
  className?: string
}
```

<a id="jcode-ui-core-approvalblockrenderslots"></a>

### `ApprovalBlockRenderSlots`

`interface` · `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx`

```ts
export interface ApprovalBlockRenderSlots {
  /** Render the pending decision card. Receives the action callbacks. */
  renderPending?: (approval: Approval, actions: ApprovalDecisionActions) => ReactNode
  /** Render the resolved inline note. */
  renderResolved?: (approval: Approval) => ReactNode
}
```

<a id="jcode-ui-core-approvaldecisionactions"></a>

### `ApprovalDecisionActions`

`interface` · `packages/jcode-ui-core/src/primitives/ApprovalBlock.tsx`

```ts
export interface ApprovalDecisionActions {
  allowOnce: () => void
  allowAllArm: () => void
  allowAllConfirm: () => void
  allowAllCancel: () => void
  deny: () => void
  armed: boolean
  /** Whether opaque option ids can be returned without boolean coercion. */
  canResolveOptions: boolean
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

<a id="jcode-ui-core-approvaloption"></a>

### `ApprovalOption`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A host-defined approval decision (e.g. an ACP permission option). The `id`
is echoed back verbatim via `resolveApprovalOption`. `custom` (and an omitted
kind) has grant semantics for the legacy boolean fallback; rejection options
must use `deny` so renderer gating and receipts remain fail-closed.

```ts
export interface ApprovalOption {
  id: string
  label: string
  kind?: ApprovalOptionKind
  description?: string
}
```

<a id="jcode-ui-core-artifactref"></a>

### `ArtifactRef`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

Safe, opaque reference to an Artifact. It never contains pixels or paths
outside the storage-kind-relative fields below.

```ts
export interface ArtifactRef {
  id: string
  storage: ArtifactStorageKind
  /** Storage-kind-relative key. Never an absolute path or provider URL. */
  key: string
  title: string
  kind: string
  media_type: string
  size: number
  width?: number
  height?: number
  provider?: string
  model?: string
  operation_id?: string
  tool_call_id?: string
  shareable?: boolean
}
```

<a id="jcode-ui-core-askuseranswer"></a>

### `AskUserAnswer`

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

<a id="jcode-ui-core-askuserblockprops"></a>

### `AskUserBlockProps`

`interface` · `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx`

```ts
export interface AskUserBlockProps extends AskUserBlockRenderSlots {
  tool: ToolCall
  /** className passthrough. */
  className?: string
}
```

<a id="jcode-ui-core-askuserblockrenderslots"></a>

### `AskUserBlockRenderSlots`

`interface` · `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx`

```ts
export interface AskUserBlockRenderSlots {
  /** Render the pending interactive card. */
  renderPending?: (questions: AskUserQuestion[], controls: AskUserControls) => ReactNode
  /** Render the resolved (replay) view. */
  renderResolved?: (tool: ToolCall, answers: AskUserAnswer[]) => ReactNode
}
```

<a id="jcode-ui-core-askusercontrols"></a>

### `AskUserControls`

`interface` · `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx`

Controls handed to the pending render-prop.

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
  /** Zero-based question currently presented by paged renderers. */
  activeIndex: number
  /** Move a paged renderer to a question (clamped to the available range). */
  setActiveIndex: (index: number) => void
  /** True while the host is accepting an answer. */
  isSubmitting: boolean
  /** Stable error code for a failed host submission. */
  submitError?: AskUserSubmitError
  /** Submit the current selections (focuses the first unanswered question). */
  submit: () => Promise<void>
  /** Submit empty answers (skip). */
  skip: () => Promise<void>
}
```

<a id="jcode-ui-core-askuseroption"></a>

### `AskUserOption`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

An option in an `ask_user` question.

```ts
export interface AskUserOption {
  label: string
  description?: string
}
```

<a id="jcode-ui-core-askuserquestion"></a>

### `AskUserQuestion`

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

<a id="jcode-ui-core-askuserstate"></a>

### `AskUserState`

`interface` · `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx`

```ts
export interface AskUserState {
  /** Per-question-header selected labels (single-select: one entry; multi: N). */
  selected: Record<string, string[]>
  /** Per-question-header free-text "Other" value. */
  other: Record<string, string>
}
```

<a id="jcode-ui-core-attachmentadapter"></a>

### `AttachmentAdapter`

`interface` · `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts`

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

<a id="jcode-ui-core-billableapprovalsummary"></a>

### `BillableApprovalSummary`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

Safe, bounded summary for billable external approvals. Full prompt/tool
args deliberately stay out of the approval DOM.

```ts
export interface BillableApprovalSummary {
  /** Stable provider capability key (for example image.generate or web.search). */
  capability?: string
  provider?: string
  model?: string
  size?: string
  aspect_ratio?: string
  resolution?: string
  count?: number
  billable?: boolean
  has_reference?: boolean
}
```

<a id="jcode-ui-core-chatimage"></a>

### `ChatImage`

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

<a id="jcode-ui-core-chatruntime"></a>

### `ChatRuntime`

`interface` · `packages/jcode-ui-core/src/runtime/index.ts`

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

<a id="jcode-ui-core-completedturn"></a>

### `CompletedTurn`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A completed user turn projected into one collapsible timeline row. The user
message remains outside this item; `activity` contains the intermediate
assistant/tool/approval work in transcript order and marks durable outcomes
that stay visible while collapsed. `summary` is the final assistant reply and
therefore remains the last visible message. This is UI-only and does not
alter transcript or model-facing message boundaries.

```ts
export interface CompletedTurn {
  id: string
  activity: Array<{
    item: ThreadItem
    alwaysVisible: boolean
  }>
  summary: Message
  durationMs: number
}
```

<a id="jcode-ui-core-composerhandle"></a>

### `ComposerHandle`

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

<a id="jcode-ui-core-composerprops"></a>

### `ComposerProps`

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

<a id="jcode-ui-core-composerrenderslots"></a>

### `ComposerRenderSlots`

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

<a id="jcode-ui-core-customevent"></a>

### `CustomEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface CustomEvent extends AGUIBaseEvent {
  type: 'CUSTOM'
  name: string
  value: unknown
}
```

<a id="jcode-ui-core-dictationstate"></a>

### `DictationState`

`interface` · `packages/jcode-ui-core/src/primitives/Composer.tsx`

Dictation (speech-to-text) UI state passed to `renderDictationButton`.

```ts
export interface DictationState {
  listening: boolean
}
```

<a id="jcode-ui-core-exploringgroup"></a>

### `ExploringGroup`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A UI-only coalesced group of collapsible read/search/list tool calls.
Does not change model-facing tool boundaries.
@deprecated Superseded by {@link ActivityGroup} (`'activity'` items). Kept
for external consumers that still feed `'exploring'` items to `Thread`.

```ts
export interface ExploringGroup {
  id: string
  tools: ToolCall[]
  status: ToolStatus
}
```

<a id="jcode-ui-core-exportmarkdownoptions"></a>

### `ExportMarkdownOptions`

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

<a id="jcode-ui-core-externalstoreruntimeoptions"></a>

### `ExternalStoreRuntimeOptions`

`interface` · `packages/jcode-ui-core/src/runtime/externalStore.ts`

```ts
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
```

<a id="jcode-ui-core-goal"></a>

### `Goal`

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

<a id="jcode-ui-core-groupcompletedturnsoptions"></a>

### `GroupCompletedTurnsOptions`

`interface` · `packages/jcode-ui-core/src/timeline/turnGroups.ts`

Options for {@link groupCompletedTurns}.

```ts
export interface GroupCompletedTurnsOptions {
  /** The final user turn stays expanded while the runtime is active. */
  isRunning?: boolean
}
```

<a id="jcode-ui-core-inlineimageadapteroptions"></a>

### `InlineImageAdapterOptions`

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

<a id="jcode-ui-core-message"></a>

### `Message` (jcode-ui-core)

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
  /** Origin channel for inbound messages (e.g. 'wechat'). Drives the compact
   *  source label and any host-provided identity chrome. */
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

<a id="jcode-ui-core-messageactions"></a>

### `MessageActions`

`interface` · `packages/jcode-ui-core/src/primitives/MessageView.tsx`

Action handles passed to `renderActions` so the caller can wire its own UI.

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

<a id="jcode-ui-core-messagesource"></a>

### `MessageSource`

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

<a id="jcode-ui-core-messagessnapshotevent"></a>

### `MessagesSnapshotEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface MessagesSnapshotEvent extends AGUIBaseEvent {
  type: 'MESSAGES_SNAPSHOT'
  messages: AGUIMessage[]
}
```

<a id="jcode-ui-core-messageversion"></a>

### `MessageVersion`

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

<a id="jcode-ui-core-messageviewprops"></a>

### `MessageViewProps`

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

<a id="jcode-ui-core-messageviewrenderslots"></a>

### `MessageViewRenderSlots`

`interface` · `packages/jcode-ui-core/src/primitives/MessageView.tsx`

```ts
export interface MessageViewRenderSlots {
  /** Render the message body (markdown → sanitized HTML). Default: raw text. */
  renderContent?: (htmlOrText: string, message: Message) => ReactNode
  /** Render the avatar glyph for a role. */
  renderAvatar?: (role: Message['role']) => ReactNode
  /**
   * Render the hover action row (copy / edit). When provided, this replaces the
   * default text buttons entirely — the styled wrapper supplies icon buttons
   * while MessageView still owns the copy/edit state and handlers.
   */
  renderActions?: (actions: MessageActions) => ReactNode
}
```

<a id="jcode-ui-core-mockruntimeoptions"></a>

### `MockRuntimeOptions`

`interface` · `packages/jcode-ui-core/src/runtime/mockRuntime.ts`

```ts
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
```

<a id="jcode-ui-core-mockthreadstore"></a>

### `MockThreadStore`

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

<a id="jcode-ui-core-pendingattachment"></a>

### `PendingAttachment`

`interface` · `packages/jcode-ui-core/src/primitives/attachmentAdapter.ts`

One attachment in the composer's pending strip.

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

<a id="jcode-ui-core-pendingattachmentitem"></a>

### `PendingAttachmentItem`

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

<a id="jcode-ui-core-queuedmessage"></a>

### `QueuedMessage`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A message composed while the agent is running; drained turn-by-turn.

```ts
export interface QueuedMessage {
  id: string
  text: string
  images?: ChatImage[]
}
```

<a id="jcode-ui-core-rawevent"></a>

### `RawEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface RawEvent extends AGUIBaseEvent {
  type: 'RAW'
  event: unknown
  source?: string
}
```

<a id="jcode-ui-core-reasoningmessagechunkevent"></a>

### `ReasoningMessageChunkEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface ReasoningMessageChunkEvent extends AGUIBaseEvent {
  type: 'REASONING_MESSAGE_CHUNK'
  messageId?: string
  delta?: string
}
```

<a id="jcode-ui-core-reasoningmessagecontentevent"></a>

### `ReasoningMessageContentEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface ReasoningMessageContentEvent extends AGUIBaseEvent {
  type: 'REASONING_MESSAGE_CONTENT'
  messageId?: string
  delta: string
}
```

<a id="jcode-ui-core-runerrorevent"></a>

### `RunErrorEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface RunErrorEvent extends AGUIBaseEvent {
  type: 'RUN_ERROR'
  message: string
  code?: string
}
```

<a id="jcode-ui-core-runfinishedevent"></a>

### `RunFinishedEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface RunFinishedEvent extends AGUIBaseEvent {
  type: 'RUN_FINISHED'
  result?: unknown
}
```

<a id="jcode-ui-core-runstartedevent"></a>

### `RunStartedEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface RunStartedEvent extends AGUIBaseEvent {
  type: 'RUN_STARTED'
  threadId?: string
  runId?: string
}
```

<a id="jcode-ui-core-runtimeactions"></a>

### `RuntimeActions`

`interface` · `packages/jcode-ui-core/src/runtime/index.ts`

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
  submitAskUser: (
    id: string,
    answers: { question_header: string; answer: string; selected?: string[] }[],
  ) => void | Promise<unknown>
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

<a id="jcode-ui-core-runtimeproviderprops"></a>

### `RuntimeProviderProps`

`interface` · `packages/jcode-ui-core/src/runtime/context.tsx`

```ts
export interface RuntimeProviderProps {
  runtime: ChatRuntime
  children: ReactNode
}
```

<a id="jcode-ui-core-runtimestate"></a>

### `RuntimeState`

`interface` · `packages/jcode-ui-core/src/runtime/index.ts`

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

<a id="jcode-ui-core-slashcommand"></a>

### `SlashCommand`

`interface` · `packages/jcode-ui-core/src/primitives/Composer.tsx`

```ts
export interface SlashCommand {
  /** The literal text inserted when chosen (e.g. '/goal'). */
  slash: string
  description?: string
}
```

<a id="jcode-ui-core-slashmenustate"></a>

### `SlashMenuState`

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

<a id="jcode-ui-core-statedeltaevent"></a>

### `StateDeltaEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface StateDeltaEvent extends AGUIBaseEvent {
  type: 'STATE_DELTA'
  delta: AGUIPatchOp[]
}
```

<a id="jcode-ui-core-statesnapshotevent"></a>

### `StateSnapshotEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface StateSnapshotEvent extends AGUIBaseEvent {
  type: 'STATE_SNAPSHOT'
  snapshot: unknown
}
```

<a id="jcode-ui-core-stepfinishedevent"></a>

### `StepFinishedEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface StepFinishedEvent extends AGUIBaseEvent {
  type: 'STEP_FINISHED'
  stepName: string
}
```

<a id="jcode-ui-core-stepstartedevent"></a>

### `StepStartedEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface StepStartedEvent extends AGUIBaseEvent {
  type: 'STEP_STARTED'
  stepName: string
}
```

<a id="jcode-ui-core-summarizeturnchangesoptions"></a>

### `SummarizeTurnChangesOptions`

`interface` · `packages/jcode-ui-core/src/timeline/turnChanges.ts`

```ts
export interface SummarizeTurnChangesOptions {
  /** Display cap before files spill into `overflow`. Default 10. */
  maxFiles?: number
}
```

<a id="jcode-ui-core-taskcontextbreakdown"></a>

### `TaskContextBreakdown` (jcode-ui-core)

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

<a id="jcode-ui-core-textmessagechunkevent"></a>

### `TextMessageChunkEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface TextMessageChunkEvent extends AGUIBaseEvent {
  type: 'TEXT_MESSAGE_CHUNK'
  messageId?: string
  role?: string
  delta?: string
}
```

<a id="jcode-ui-core-textmessagecontentevent"></a>

### `TextMessageContentEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface TextMessageContentEvent extends AGUIBaseEvent {
  type: 'TEXT_MESSAGE_CONTENT'
  messageId: string
  delta: string
}
```

<a id="jcode-ui-core-textmessageendevent"></a>

### `TextMessageEndEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface TextMessageEndEvent extends AGUIBaseEvent {
  type: 'TEXT_MESSAGE_END'
  messageId: string
}
```

<a id="jcode-ui-core-textmessagestartevent"></a>

### `TextMessageStartEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface TextMessageStartEvent extends AGUIBaseEvent {
  type: 'TEXT_MESSAGE_START'
  messageId: string
  role?: string
}
```

<a id="jcode-ui-core-threadliststate"></a>

### `ThreadListState`

`interface` · `packages/jcode-ui-core/src/threads/store.ts`

The read-side state the `ThreadList` renders from.

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

<a id="jcode-ui-core-threadprops"></a>

### `ThreadProps` (jcode-ui-core)

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

<a id="jcode-ui-core-threadrenderslots"></a>

### `ThreadRenderSlots`

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

<a id="jcode-ui-core-threadstore"></a>

### `ThreadStore`

`interface` · `packages/jcode-ui-core/src/threads/store.ts`

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

<a id="jcode-ui-core-threadstoreactions"></a>

### `ThreadStoreActions`

`interface` · `packages/jcode-ui-core/src/threads/store.ts`

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

<a id="jcode-ui-core-threadstoreproviderprops"></a>

### `ThreadStoreProviderProps`

`interface` · `packages/jcode-ui-core/src/threads/context.tsx`

```ts
export interface ThreadStoreProviderProps {
  store: ThreadStore
  children: ReactNode
}
```

<a id="jcode-ui-core-threadsummary"></a>

### `ThreadSummary`

`interface` · `packages/jcode-ui-core/src/threads/store.ts`

A single row in the thread list — a lightweight summary, not the full convo.

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

<a id="jcode-ui-core-todoitem"></a>

### `TodoItem`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A todo/goal tracking item. (id is a number — matches the Go backend.)

```ts
export interface TodoItem {
  id: number
  title: string
  status: 'pending' | 'in_progress' | 'completed' | 'cancelled'
}
```

<a id="jcode-ui-core-tokensnapshot"></a>

### `TokenSnapshot`

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

<a id="jcode-ui-core-toolbatchgroup"></a>

### `ToolBatchGroup`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A UI-only group of tool calls issued concurrently by one assistant message
(same `batchId`). Rendered as a stacked status-row list; when every member
is a collapsible read/search/list tool (`explorative`) it renders as an
upgraded Exploring card instead. Does not change model-facing boundaries.
@deprecated Superseded by {@link ActivityGroup} (`'activity'` items) — batch
members now coalesce into activity groups. Kept for external consumers that
still feed `'batch'` items to `Thread`.

```ts
export interface ToolBatchGroup {
  id: string
  batchId: string
  tools: ToolCall[]
  status: ToolStatus
  /** True when ALL tools are collapsible (read/search/list). */
  explorative: boolean
}
```

<a id="jcode-ui-core-toolcall"></a>

### `ToolCall`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A tool invocation. `args`/`output` are raw JSON strings; renderers parse
them. `children` carries subagent-nested calls (rendered recursively).

```ts
export interface ToolCall {
  id: string
  /** Backend tool_call_id for precise result matching. */
  toolCallID?: string
  /** Host-generated id of the exact permission gate that released this call. */
  approvalID?: string
  /** Runner-owned generation-operation id. Never inferred from toolCallID. */
  operationID?: string
  name: string
  args: string
  output?: string
  /** Clean output for UI display (metadata stripped). */
  displayOutput?: string
  error?: string
  status: ToolStatus
  /** Initial timeline surface. Standalone tools are hard Activity boundaries. */
  surface?: ToolSurface
  /** Monotonic operation phase. Image tools start queued. */
  phase?: ToolPhase
  /** Required for terminal image operations. */
  outcome?: ToolOutcome
  /** Typed backend error classification; the UI never parses `error`. */
  errorCode?: string
  /** Immutable provider/model snapshot for this operation. These never derive
   * from the host's currently selected image model. */
  provider?: string
  model?: string
  /** Opaque, structured result references. Duplicate ids are ignored. */
  artifacts?: ArtifactRef[]
  /** User rejected this call at the approval prompt. Rendered struck-through
   *  and muted (declined ≠ failed) — status stays 'done', not 'error'. */
  denied?: boolean
  /** True while this call sits at an unresolved approval prompt. Rendered in
   *  the warning color; cleared when the approval resolves or a result lands. */
  awaitingApproval?: boolean
  /** Approval gate bound to this concrete tool-call occurrence for timeline
   *  rendering. Hosts may keep approvals as independent ThreadItems; the
   *  UI-only timeline projection attaches the matching item by tool_call_id. */
  approval?: Approval
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
  /** Concurrent-batch id — tools issued together by one assistant message
   *  share it and coalesce into a `ToolBatchGroup` row stack. */
  batchId?: string
  /** 0-based position within the batch. */
  batchIndex?: number
  /** Total number of tools in the batch. */
  batchSize?: number
  /** Wall-clock start (unix ms) — drives the live elapsed badge while running. */
  startedAt?: number
}
```

<a id="jcode-ui-core-toolcallargsevent"></a>

### `ToolCallArgsEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface ToolCallArgsEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_ARGS'
  toolCallId: string
  delta: string
}
```

<a id="jcode-ui-core-toolcallchunkevent"></a>

### `ToolCallChunkEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface ToolCallChunkEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_CHUNK'
  toolCallId?: string
  toolCallName?: string
  parentMessageId?: string
  delta?: string
}
```

<a id="jcode-ui-core-toolcallcontextvalue"></a>

### `ToolCallContextValue`

`interface` · `packages/jcode-ui-core/src/primitives/ToolCallView.tsx`

Context the host provides to wire the registry + the subagent/askuser slots.

```ts
export interface ToolCallContextValue {
  registry: ToolRendererRegistry
  /** Render nested subagent children. Default: recurses into ToolCallView. */
  renderChild?: (child: ToolCall, depth: number) => ReactNode
  /** Render an ask_user tool (interactive question block). */
  renderAskUser?: (tool: ToolCall) => ReactNode
}
```

<a id="jcode-ui-core-toolcallendevent"></a>

### `ToolCallEndEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface ToolCallEndEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_END'
  toolCallId: string
}
```

<a id="jcode-ui-core-toolcallresultevent"></a>

### `ToolCallResultEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface ToolCallResultEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_RESULT'
  messageId?: string
  toolCallId: string
  content: unknown
  role?: string
}
```

<a id="jcode-ui-core-toolcallstartevent"></a>

### `ToolCallStartEvent`

`interface` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

```ts
export interface ToolCallStartEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_START'
  toolCallId: string
  toolCallName: string
  parentMessageId?: string
}
```

<a id="jcode-ui-core-toolcallviewprops"></a>

### `ToolCallViewProps`

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

<a id="jcode-ui-core-tooldisplayinfo"></a>

### `ToolDisplayInfo`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

Display metadata for a tool call, surfaced from the backend or extracted
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

<a id="jcode-ui-core-toolmeta"></a>

### `ToolMeta`

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

<a id="jcode-ui-core-toolpresentation"></a>

### `ToolPresentation`

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

<a id="jcode-ui-core-toolrendererprops"></a>

### `ToolRendererProps`

`interface` · `packages/jcode-ui-core/src/adapters/index.ts`

Props every tool renderer receives.

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
  surface?: ToolSurface
  phase?: ToolPhase
  outcome?: ToolOutcome
  errorCode?: string
  operationID?: string
  provider?: string
  model?: string
  artifacts?: ArtifactRef[]
  startedAt?: number
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

<a id="jcode-ui-core-toolstreams"></a>

### `ToolStreams`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

Structured stdout/stderr for execute-style tools (dual-channel UI path).

```ts
export interface ToolStreams {
  stdout?: string
  stderr?: string
  aggregated?: string
}
```

<a id="jcode-ui-core-turnchangessummary"></a>

### `TurnChangesSummary`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

A UI-only per-turn summary of file changes (opencode SessionTurn-style):
"Changed N files (+A −R)" inserted at the end of a completed turn.
`files` holds up to the display cap; `overflow` the rest ("… N more").

```ts
export interface TurnChangesSummary {
  id: string
  /** Total distinct files changed this turn (files + overflow). */
  fileCount: number
  files: TurnFileChange[]
  overflow: TurnFileChange[]
  totalAdded: number
  totalRemoved: number
  /** True when at least one file has derived ± line counts. */
  hasLineCounts: boolean
}
```

<a id="jcode-ui-core-turnfilechange"></a>

### `TurnFileChange`

`interface` · `packages/jcode-ui-core/src/types/index.ts`

One changed file inside a turn-changes summary. `added`/`removed` are
client-derived line counts (absent when the tool args carry no diff text);
`tool` is the LAST call that touched the file, kept so the UI can expand
its registry-rendered diff body.

```ts
export interface TurnFileChange {
  path: string
  added?: number
  removed?: number
  tool: ToolCall
}
```

<a id="jcode-ui-core-aguievent"></a>

### `AGUIEvent`

`type` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

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

<a id="jcode-ui-core-aguirole"></a>

### `AGUIRole`

`type` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

AG-UI role space. Not all map onto jcode's `Role` (see `toRole`).

```ts
export type AGUIRole = 'developer' | 'system' | 'assistant' | 'user' | 'tool';
```

<a id="jcode-ui-core-aguitransport"></a>

### `AGUITransport`

`type` · `packages/jcode-ui-core/src/runtime/aguiEvents.ts`

A pluggable event source. The default (`createFetchTransport`) POSTs the run
input and streams the SSE response; tests inject a scripted async iterable.

```ts
export type AGUITransport = (
  input: AGUIRunInput,
  signal: AbortSignal,
) => AsyncIterable<AGUIEvent>;
```

<a id="jcode-ui-core-approvaloptionkind"></a>

### `ApprovalOptionKind`

`type` · `packages/jcode-ui-core/src/types/index.ts`

Button treatment for a host-defined approval option. `allow_always` keeps
 the two-step arming UX; `custom` renders as a neutral choice.

```ts
export type ApprovalOptionKind = 'allow_once' | 'allow_always' | 'deny' | 'custom';
```

<a id="jcode-ui-core-approvaloutcome"></a>

### `ApprovalOutcome`

`type` · `packages/jcode-ui-core/src/types/index.ts`

Canonical result of an approval gate for rendering and timeline projection.

```ts
export type ApprovalOutcome = 'pending' | 'allowed' | 'denied';
```

<a id="jcode-ui-core-artifactstoragekind"></a>

### `ArtifactStorageKind`

`type` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export type ArtifactStorageKind = 'workspace' | 'managed';
```

<a id="jcode-ui-core-askusersubmiterror"></a>

### `AskUserSubmitError`

`type` · `packages/jcode-ui-core/src/primitives/AskUserBlock.tsx`

```ts
export type AskUserSubmitError = 'submit_failed';
```

<a id="jcode-ui-core-connectionstate"></a>

### `ConnectionState`

`type` · `packages/jcode-ui-core/src/types/index.ts`

Transport liveness surfaced by the runtime (drives ConnectionBanner).

```ts
export type ConnectionState = 'connected' | 'reconnecting' | 'disconnected';
```

<a id="jcode-ui-core-goalstatus"></a>

### `GoalStatus`

`type` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export type GoalStatus = 'active' | 'complete' | 'blocked';
```

<a id="jcode-ui-core-mockthreadstoreseed"></a>

### `MockThreadStoreSeed`

`type` · `packages/jcode-ui-core/src/threads/store.ts`

Seed for `createMockThreadStore` — an array of threads, or a partial state.

```ts
export type MockThreadStoreSeed = ThreadSummary[] | Partial<ThreadListState>;
```

<a id="jcode-ui-core-partialruntimestate"></a>

### `PartialRuntimeState`

`type` · `packages/jcode-ui-core/src/runtime/index.ts`

A `RuntimeState` where every slice is optional; useful for adapters that
 only implement part of the contract (e.g. a read-only replay runtime).

```ts
export type PartialRuntimeState = Partial<RuntimeState>;
```

<a id="jcode-ui-core-pendingstatus"></a>

### `PendingStatus`

`type` · `packages/jcode-ui-core/src/primitives/Composer.tsx`

Lifecycle status of a pending attachment slot.

```ts
export type PendingStatus = 'uploading' | 'done' | 'error';
```

<a id="jcode-ui-core-role"></a>

### `Role`

`type` · `packages/jcode-ui-core/src/types/index.ts`

Who authored a message.

```ts
export type Role = 'user' | 'assistant' | 'system';
```

<a id="jcode-ui-core-systemlevel"></a>

### `SystemLevel`

`type` · `packages/jcode-ui-core/src/types/index.ts`

Severity for `system` messages. Undefined → default neutral styling.

```ts
export type SystemLevel = 'error' | 'notice';
```

<a id="jcode-ui-core-threaditem"></a>

### `ThreadItem`

`type` · `packages/jcode-ui-core/src/types/index.ts`

The discriminated union rendered by `Thread`. A `seq` counter keeps DOM
identity stable across streaming updates and is used as the virtualizer key.

```ts
export type ThreadItem =
  | { kind: 'message'; data: Message; seq: number }
  | { kind: 'tool'; data: ToolCall; seq: number }
  | { kind: 'approval'; data: Approval; seq: number }
  | { kind: 'activity'; data: ActivityGroup; seq: number }
  | { kind: 'turn'; data: CompletedTurn; seq: number }
  | { kind: 'exploring'; data: ExploringGroup; seq: number }
  | { kind: 'batch'; data: ToolBatchGroup; seq: number }
  | { kind: 'turnchanges'; data: TurnChangesSummary; seq: number };
```

<a id="jcode-ui-core-threaditemkind"></a>

### `ThreadItemKind`

`type` · `packages/jcode-ui-core/src/types/index.ts`

Built-in thread-item kinds (activity/turn/exploring/batch/turnchanges are UI-only coalescing).

```ts
export type ThreadItemKind =
  | 'message'
  | 'tool'
  | 'approval'
  | 'activity'
  | 'turn'
  | 'exploring'
  | 'batch'
  | 'turnchanges';
```

<a id="jcode-ui-core-tooloutcome"></a>

### `ToolOutcome`

`type` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export type ToolOutcome = 'succeeded' | 'failed' | 'cancelled' | 'uncertain';
```

<a id="jcode-ui-core-toolphase"></a>

### `ToolPhase`

`type` · `packages/jcode-ui-core/src/types/index.ts`

Durable image-tool lifecycle. `terminal` is intentionally separate from the
outcome so reducers can enforce monotonic ordering without guessing whether
a terminal call succeeded, failed, was cancelled, or became uncertain.

```ts
export type ToolPhase = 'queued' | 'generating' | 'saving' | 'terminal';
```

<a id="jcode-ui-core-toolrenderer"></a>

### `ToolRenderer`

`type` · `packages/jcode-ui-core/src/adapters/index.ts`

A tool renderer is just a React component.

```ts
export type ToolRenderer = ComponentType<ToolRendererProps>;
```

<a id="jcode-ui-core-toolstatus"></a>

### `ToolStatus`

`type` · `packages/jcode-ui-core/src/types/index.ts`

```ts
export type ToolStatus = 'running' | 'done' | 'error';
```

<a id="jcode-ui-core-toolsurface"></a>

### `ToolSurface`

`type` · `packages/jcode-ui-core/src/types/index.ts`

Timeline surface requested by a tool from its initial tool_call event.

```ts
export type ToolSurface = 'activity' | 'standalone';
```

