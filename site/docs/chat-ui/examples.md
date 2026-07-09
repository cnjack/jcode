---
title: Examples
nav_order: 9
---

# Examples

Runnable starters in the monorepo under `examples/`.

| Example | Pattern | Path |
|---------|---------|------|
| **minimal** | `createMockRuntime` — no backend | [`examples/jcode-ui-minimal`](https://github.com/cnjack/jcode/tree/main/examples/jcode-ui-minimal) |
| **zustand** | `createExternalStoreRuntime` + Zustand | [`examples/jcode-ui-zustand`](https://github.com/cnjack/jcode/tree/main/examples/jcode-ui-zustand) |

## Quick start (minimal)

```bash
# from repo root — build packages once
cd packages/jcode-ui-core && pnpm build
cd ../jcode-ui && pnpm build

cd ../../examples/jcode-ui-minimal
pnpm install
pnpm dev   # http://localhost:5177
```

## External store (Zustand)

```bash
cd examples/jcode-ui-zustand
pnpm install
pnpm dev   # http://localhost:5178
```

This is the production path: host store owns `ThreadItem[]`, the runtime only projects + binds actions. See the [external store guide](/chat-ui/docs/guides/external-store).

## Live docs previews

Every component page embeds a **Preview | Code** panel with the same components. Start at [Components](/chat-ui/docs/components) or the [marketing playground](/chat-ui).
