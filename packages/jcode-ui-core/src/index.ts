/**
 * jcode-ui-core — the framework-agnostic core of jcode-ui.
 *
 * Layers:
 *   - types       : Message / ToolCall / Approval / ThreadItem / TokenSnapshot …
 *   - runtime     : ChatRuntime interface + ExternalStoreRuntime + MockRuntime
 *                   + React <RuntimeProvider> + useRuntimeState/useRuntimeSelector hooks
 *   - adapters    : ToolRendererRegistry (the tool-call plugin seam)
 *   - primitives  : headless React components (Thread/MessageView/Composer/…)
 *   - hooks       : useAutoScroll / useStreamFollow / useFocusOnIdle …
 *
 * This entry re-exports everything for convenience. For tree-shaking, prefer
 * the subpath imports: `jcode-ui-core/runtime`, `jcode-ui-core/primitives`, …
 */

export * from './types/index.js'
export * from './runtime/index.js'
export * from './adapters/index.js'
export * from './hooks/index.js'
export * from './primitives/index.js'
export * from './timeline/groupExploring.js'
export * from './timeline/groupActivity.js'
export * from './timeline/turnChanges.js'
export * from './threads/index.js'
export * from './export/markdown.js'
