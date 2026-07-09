# jcode-ui + Zustand

Production-shaped integration: **your store owns the timeline**, jcode-ui renders via `createExternalStoreRuntime`.

## Run

```bash
# build packages once
cd packages/jcode-ui-core && pnpm build
cd ../jcode-ui && pnpm build

cd ../../examples/jcode-ui-zustand
pnpm install
pnpm dev
```

Open http://localhost:5178

## Pattern

```ts
const runtime = createExternalStoreRuntime({
  store: useChatStore,           // Zustand: getState + subscribe
  select: (s) => ({
    items: s.items,
    isRunning: s.isRunning,
  }),
  actions: {
    sendMessage: (text) => { /* mutate store + call API */ },
    // …
  },
})
```

See also: [External store guide](https://www.j-code.net/chat-ui/docs/guides/external-store)

## Files

| File | Role |
|------|------|
| `src/store.ts` | Zustand timeline + mutations |
| `src/App.tsx` | Runtime wiring + UI |
