# jcode-ui minimal example

Drop-in chat UI with **`createMockRuntime`** — no backend, no store library.

## Run

From the monorepo root (packages must be built first):

```bash
# once
cd packages/jcode-ui-core && pnpm build
cd ../jcode-ui && pnpm build

cd ../../examples/jcode-ui-minimal
pnpm install
pnpm dev
```

Open http://localhost:5177

## What it shows

- `Thread` + `ChatInput`
- Default tool registry (seeded with one `execute` card)
- Interactive send → fake assistant echo

## Next

- Wire a real store: see [`../jcode-ui-zustand`](../jcode-ui-zustand)
- Docs: https://www.j-code.net/chat-ui/docs
