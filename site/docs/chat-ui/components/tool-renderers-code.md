---
title: Code tool renderers
parent: Components
nav_order: 18
---

# Code tool renderers

Three renderers for code-shaped tool output, shipped from `jcode-ui/tool-renderers`: a collapsible **file tree**, a **test-results** summary, and a structured **stack trace**.

<div data-jcode-demo="tool-gallery-2" data-height="320"></div>

Each card is a real `ToolCallCard` driven by a `ToolRendererRegistry` — click a header to expand.

## The three renderers

| Export | Recognizes | Renders |
|--------|-----------|---------|
| `FileTreeRenderer` | `list_dir` / `glob` — a line-separated path list | Collapsible tree; directories fold, files get an extension-colored dot. |
| `TestResultsRenderer` | Go (`--- PASS/FAIL/SKIP`) and Vitest/Jest output | A pass · fail · skip summary bar + expandable failing cases. |
| `StackTraceRenderer` | Go panic stacks and JS `Error` stacks | Frame list (function + `file:line`); runtime frames collapse behind a toggle. |

## Registration

`FileTreeRenderer` is already in the default registry (`list_dir`, `glob`). `TestResultsRenderer` and `StackTraceRenderer` are **not** — a host maps them onto the tool names it emits (in the demo, `go_test` and `panic`; a common alternative is a single `execute` wrapper that inspects args).

```tsx
import {
  RuntimeProvider,
  ToolRegistryProvider,
  createDefaultToolRegistry,
  createMockRuntime,
  ToolCallCard,
} from 'jcode-ui'
import { TestResultsRenderer, StackTraceRenderer } from 'jcode-ui/tool-renderers'
import 'jcode-ui/styles.css'

const registry = createDefaultToolRegistry() // FileTree already wired for list_dir/glob
registry.register('go_test', TestResultsRenderer)
registry.register('panic', StackTraceRenderer)

const goTest = {
  id: '1', name: 'go_test', args: '{}', status: 'error' as const, timestamp: 0,
  output: '--- FAIL: TestSubtraction (0.01s)\n    math_test.go:24: got 1; want 2\nFAIL',
}

<RuntimeProvider runtime={createMockRuntime()}>
  <ToolRegistryProvider registry={registry}>
    <ToolCallCard tool={goTest} />
  </ToolRegistryProvider>
</RuntimeProvider>
```

Every renderer receives `ToolRendererProps` (`name` / `args` / `output` / `displayOutput` / `error` / `status` / …) and falls back to the raw text in a `<pre>` when the output isn't recognizable — nothing is ever lost.

## Notes per renderer

**FileTreeRenderer** — fully expanded by default; when the tree exceeds 200 nodes only the top level stays open. Parsing is tolerant of leading tree glyphs / bullets.

**TestResultsRenderer** — summary bar shows passed/failed/skipped and the framework tag; each failing case expands to its captured error detail.

**StackTraceRenderer** — frames with a resolvable `file:line` are clickable and dispatch a bubbling `CustomEvent('jcode-ui:open-file', { detail: { path, line } })`, so an editor-embedded host can jump to the location. `node_modules` / language-runtime frames are collapsed behind an "N runtime frames" toggle.

The parsers are exported for reuse: `parsePathList`, `parseTestOutput`, `parseStackTrace`.

## Related

- [ToolCallCard](/chat-ui/docs/components/tool-call-card)
- [Artifact](/chat-ui/docs/components/artifact)
- [Custom tool renderers guide](/chat-ui/docs/guides/tool-renderers)
