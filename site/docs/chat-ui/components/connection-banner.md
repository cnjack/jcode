---
title: ConnectionBanner
parent: Components
nav_order: 14
---

# ConnectionBanner

A sticky transport-liveness strip. Reads `state.connection` from the runtime and surfaces reconnecting / disconnected states — plus a brief "Reconnected" success flash on recovery.

<div data-jcode-demo="connection" data-height="200"></div>

The buttons call `runtime.patchState({ connection })`. Note that **`connected` renders nothing**; recovering from a non-connected state flashes a 2-second "Reconnected".

## Usage

```tsx
import { RuntimeProvider, createMockRuntime, ConnectionBanner } from 'jcode-ui'
import 'jcode-ui/styles.css'

const runtime = createMockRuntime({ state: { connection: 'reconnecting' } })

<RuntimeProvider runtime={runtime}>
  <ConnectionBanner />
  {/* …Thread / ChatInput… */}
</RuntimeProvider>
```

`ConnectionBanner` takes **no props** — it reads everything from the runtime. A real host drives `connection` from its transport (WebSocket close/reopen, SSE retry, etc.).

## States

| `state.connection` | UI |
|--------------------|-----|
| `connected` | Renders nothing (steady state). |
| `reconnecting` | Warning tint + spinning icon, "Reconnecting…". |
| `disconnected` | Error tint + explanatory copy. |
| `connected` (just recovered) | 2s success flash: ✓ "Reconnected", then fades. |

```ts
type ConnectionState = 'connected' | 'reconnecting' | 'disconnected'
```

`role="status"` / `aria-live="polite"` announce transitions to assistive tech without stealing focus.

## Related

- [Runtime state](/chat-ui/docs/runtime)
