---
title: ModelSelector
parent: Components
nav_order: 13
---

# ModelSelector

A pure, presentational model picker: a trigger button (current label + chevron) with an upward popup — search filter, grouped by provider, ✓ on the active model. Backend-agnostic; the host owns the catalog and the switch.

<div data-jcode-demo="model-selector" data-height="280"></div>

## Usage

Typically the composer's `leadingControls`. No runtime coupling — you pass `models` + `value` + `onChange`.

```tsx
import { useState } from 'react'
import { ChatInput, ModelSelector } from 'jcode-ui'
import 'jcode-ui/styles.css'

const models = [
  { id: 'claude-opus-4', label: 'Claude Opus 4', provider: 'Anthropic', description: 'Most capable' },
  { id: 'claude-sonnet-4', label: 'Claude Sonnet 4', provider: 'Anthropic' },
  { id: 'gpt-5', label: 'GPT-5', provider: 'OpenAI' },
  { id: 'gemini-2.5-pro', label: 'Gemini 2.5 Pro', provider: 'Google' },
]

function Composer() {
  const [model, setModel] = useState('claude-opus-4')
  return (
    <ChatInput
      leadingControls={<ModelSelector models={models} value={model} onChange={setModel} />}
    />
  )
}
```

## Props

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `models` | `ModelSelectorOption[]` | — | The catalog. Grouped by `provider`, first-seen order preserved. |
| `value` | `string` | — | Selected model id. |
| `onChange` | `(id: string) => void` | — | Called with the chosen model id. |
| `disabled` | `boolean` | `false` | Disable the trigger. |
| `placeholder` | `string` | `'Select model'` | Trigger label when nothing is selected. |
| `className` | `string` | — | Extra class on the root. |

```ts
interface ModelSelectorOption {
  id: string
  label: string
  provider?: string     // grouping + search
  description?: string  // one-line, shown under the label
}
```

## Behavior

- Opens **upward** (composer sits at the bottom of the thread).
- Search filters by `label`, `id`, and `provider`.
- Keyboard: ↑/↓ to move, Enter to choose, Esc to close; closes on outside click.
- ✓ marks the active model.

## Related

- [ChatInput](/chat-ui/docs/components/chat-input) — `leadingControls` slot
