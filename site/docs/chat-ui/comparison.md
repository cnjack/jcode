---
title: vs. Alternatives
nav_order: 10
---

# jcode-ui vs. the alternatives

Honest guidance on when to pick jcode-ui — and when not to. The other three
libraries are excellent; they optimize for different things.

## TL;DR

| | jcode-ui | assistant-ui | Vercel AI Elements | CopilotKit |
|---|---|---|---|---|
| **Sweet spot** | coding/ops agents: tool runs, approvals, subagents | ChatGPT-style product chat | AI apps on Next.js + AI SDK | agent ↔ app state sync (AG-UI) |
| **Distribution** | npm package | npm primitives + shadcn copy-in | shadcn copy-in only | npm package |
| **Styling** | scoped `--jcode-*` tokens; no Tailwind required in your app | Tailwind + shadcn | your shadcn theme | shadcn-style tokens |
| **Backend coupling** | none (runtime seam; AG-UI adapter built in) | runtime seam (AI SDK, LangGraph, AG-UI, …) | Vercel AI SDK required | AG-UI / CopilotKit runtime |
| **Default look** | branded (warm accent, flat editorial layout) | ChatGPT-like | neutral shadcn | neutral shadcn |

## Pick jcode-ui when…

- Your agent **runs tools** and users need to *trust* what it did: dual-channel
  terminal output (stdout/stderr/exit/duration), diffs, file viewers, search
  results, nested subagent trees, and "Exploring…" coalescing are first-class,
  runtime-wired renderers — not static display components.
- You need **human-in-the-loop safety**: two-step armed "allow all", external-
  path flagging, and arbitrary host-defined approval options (ACP-compatible).
- Your app **isn't a Tailwind/shadcn app** (or isn't even React-styled the same
  way): components ship self-contained scoped CSS; the host needs zero build
  integration beyond one CSS import.
- You want the **conversation loop without a platform**: branching, regenerate,
  feedback, retry, quote, export, welcome/suggestions are all optional actions —
  implement what your backend supports, the UI hides the rest (fail-visible).

## Pick something else when…

- **assistant-ui** — you're building a general-purpose ChatGPT-style product
  and want the deepest conversation UX ecosystem (voice sessions, message
  queueing UI, selection toolbars, DevTools, React Native/terminal ports) plus
  a hosted persistence cloud. It's the most complete general chat kit.
- **AI Elements** — you're already on Next.js + AI SDK + shadcn and want
  maximum component breadth (48+ components incl. workflow canvas, voice
  persona, web previews) as editable source in your repo.
- **CopilotKit** — your product is an *app the agent co-drives* (shared state,
  frontend tools, generative UI) rather than a chat transcript. AG-UI protocol
  depth is the moat.

## Interop notes

- Speaking **AG-UI** already? `createAGUIRuntime` drives jcode-ui from any
  AG-UI backend (LangGraph, CrewAI, Mastra, …) with zero glue.
- Living in **shadcn**? `import 'jcode-ui/shadcn.css'` maps your theme onto
  jcode-ui tokens automatically.
- Migrating from assistant-ui? The [component mapping table](/chat-ui/docs/components)
  lists equivalents for every assistant-ui component we cover.
