# jcode-ui examples

Cloneable starters for the React chat component library.

| Example | Port | Pattern |
|---------|------|---------|
| [jcode-ui-minimal](./jcode-ui-minimal) | 5177 | `createMockRuntime` only — fastest start |
| [jcode-ui-zustand](./jcode-ui-zustand) | 5178 | `createExternalStoreRuntime` + Zustand |

## Prerequisites

```bash
cd packages/jcode-ui-core && pnpm build
cd ../jcode-ui && pnpm build
```

Each example is a **standalone pnpm workspace** (`ignore-workspace` via `.npmrc`) using
`file:../../packages/jcode-ui`, so installs stay local and always link monorepo packages.

If pnpm blocks esbuild postinstall, the example `pnpm-workspace.yaml` already allows it.

## Docs

- https://www.j-code.net/chat-ui/docs
- https://www.j-code.net/chat-ui/docs/installation
- https://www.j-code.net/chat-ui/docs/guides/external-store
