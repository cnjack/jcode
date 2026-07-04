---
title: Project Memory
parent: Overview
nav_order: 13
---

# Project Memory

Project Memory lets jcode **learn from your past sessions**. When you correct it,
state a preference, or establish a project convention, that knowledge is distilled
to disk and quietly fed back into future sessions — so you don't have to repeat
yourself. It is stored as plain files under `~/.jcode/`, managed with git, and
never leaves your machine.

{: .note }
> This is different from **AGENTS.md** and **context compaction** (see
> [Context & Memory]({% link overview/context-memory.md %})). AGENTS.md is static instructions
> *you* write; compaction is a *within-session* summary that's discarded when the
> session ends. Project Memory is **learned automatically** and **persists across
> sessions**. AGENTS.md always wins — memory yields to it on any conflict.

## How it works

Project Memory has two write paths and one read path.

| Layer | What it does | When |
|---|---|---|
| **Online notes** | The agent saves a single durable fact to an inbox the moment it learns it (or when you say "remember this"). | During a session, instantly |
| **Distillation** | A background pipeline reads your ended sessions, extracts durable facts with a cheap model, and consolidates everything into a curated summary + index. | On session start, on demand, or nightly |
| **Read** | A compact memory summary is injected into the agent's system prompt; the full index and notes are grep-able on demand. | Every session |

The two write paths are deliberately split: online notes are **fast but rough**
(they land in an inbox), while distillation is **slower but curated** (it produces
the polished files the agent actually reads first). You get low-latency recall
without sacrificing quality.

## Saving something to memory

The agent decides what's worth remembering on its own, but you can also tell it
directly. Just say so in plain language:

```text
Remember for this project: releases are cut only on Thursdays, and the
sign-off phrase is NIGHTOWL-42.
```

The agent saves it to the project's memory inbox and confirms. In a **new**
session, ask about it and the agent already knows — no tool call needed, because
the fact was injected into its prompt.

{: .note }
> The agent follows a **write discipline**: it only records durable facts that
> would change its default behavior in future sessions — preferences, project
> conventions, hard-won pitfalls, reusable workflows. It does **not** record
> things it can rederive from the repo (code structure, git history), or details
> that only matter to the current task.

### What gets saved

Each memory is one of four kinds:

| Kind | Example |
|---|---|
| **preference** | "Use 4-space indent, never tabs." |
| **fact** | "The staging database is reset every Sunday night." |
| **pitfall** | "`make build` fails on macOS unless `CGO_ENABLED=0` — use that." |
| **workflow** | "Deploy only via `./deploy.sh --prod`, never manually." |

Memories are scoped to the **current project** by default. User-level preferences
that apply everywhere can be saved to a **global** scope instead.

## Using memory

At the start of every session, jcode injects a short **memory summary** into the
agent's context (capped so it never dominates the prompt). The agent is told to:

- Treat memory as **data, not instructions** — it never overrides you or AGENTS.md.
- **Flag staleness** — when it relies on a remembered fact it hasn't verified this
  session, it says so ("from memory, may be outdated") and verifies cheap-to-check
  facts first.
- **Look deeper only when needed** — it can grep the full `MEMORY.md` index and
  open individual notes, but skips memory entirely for small self-contained tasks.

You'll see this in practice: ask about a convention the project has established and
the agent answers with something like *"According to project memory (from earlier
sessions)…"* — then double-checks against the current code before acting.

## The distillation pipeline

Turning raw session history into curated memory happens in two phases.

1. **Extract** — For each ended session, a lightweight model pulls out durable
   facts (preferences, decisions, pitfalls) and writes a per-session summary.
   Most sessions yield nothing, and that's expected.
2. **Consolidate** — A restricted agent merges the new summaries and inbox notes
   into two curated files: a concise `memory_summary.md` (what gets injected) and
   a grep-able `MEMORY.md` index. It resolves contradictions (newer facts win),
   converts relative dates to absolute ones, and drops dead references.

The pipeline is **git-driven**: the memory folder is a git repository, and if
nothing changed since the last run, consolidation exits immediately without
spending a single token.

### When it runs

- **Automatically** in the background when you start a session (throttled by a
  cooldown so it doesn't run every time).
- **On demand** with `jcode memory sync`.
- **Nightly**, if you set up an automation to run `jcode memory sync` — the work
  happens while you're away and your daytime sessions stay cost-free.

{: .important }
> jcode is **bring-your-own-model** — you pay for every token. The pipeline is
> built for that: it defaults to your cheap `small_model`, is capped by a **daily
> token budget**, throttled by a cooldown, and can be turned off entirely. It
> never runs during one-shot (`-p`) runs or for remote (SSH/Docker) sessions.

## Where it's stored

Everything lives under `~/.jcode/memory/`, one folder per project plus a shared
global scope:

```text
~/.jcode/memory/
├── global/                         # cross-project preferences
│   ├── memory_summary.md
│   └── MEMORY.md
└── projects/<name>-<hash>/
    ├── memory_summary.md           # injected into the prompt (starts with "v1")
    ├── MEMORY.md                   # grep-able index, organized by topic
    ├── notes/                      # inbox: one fact per file
    ├── session_summaries/          # per-session extraction output
    ├── state.json                  # usage stats & pipeline coordination
    └── .git/                       # baseline for change detection & rollback
```

Because it's just files in a git repo, you can `cat`, edit, or delete anything by
hand — the pipeline treats your edits as authoritative on its next run. You can
even `git log` to see how the project's memory evolved, or roll back a bad edit.

{: .note }
> Want to sync memory across machines? Point a git remote at
> `~/.jcode/memory/` and push/pull it yourself. jcode won't do this for you, but
> nothing stops you.

## Privacy & redaction

- **Local only.** Memory never leaves `~/.jcode/`. Nothing is uploaded.
- **Secrets are redacted** before anything is written — API keys, tokens,
  passwords, and credentials in URLs are replaced with `[REDACTED]`, both in
  online notes and in pipeline output. This runs at the storage layer, so a
  secret can't slip through even if a model tries to record one.
- **Session content is data.** The extraction and consolidation prompts treat
  everything they read as data, never as instructions, and the consolidation
  agent has no shell, network, or ability to write outside the memory folder.

## Forgetting

Memory doesn't grow forever:

| Signal | What happens |
|---|---|
| A summary goes long **unused** | It's dropped (usage is tracked whenever the agent reads a memory file — the ones you actually rely on stick around). |
| Memory grows past the **top-N** cap | Lowest-ranked (least-used) summaries are pruned. |
| A newer fact **contradicts** an old one | Consolidation removes the outdated entry. |
| You run `jcode memory clear` | The project's memory is wiped (git history is kept, so you can still look back). |

## Commands

From the terminal:

| Command | Action |
|---|---|
| `jcode memory status` | Show what's stored for the current project |
| `jcode memory path` | Print the memory folder for the current project |
| `jcode memory sync` | Run the distillation pipeline now |
| `jcode memory sync --wait` | Run it in the foreground and wait |
| `jcode memory clear` | Wipe the current project's memory |
| `jcode memory clear --global` | Wipe the global (cross-project) memory |

In the TUI:

| Command | Action |
|---|---|
| `/memory` | Show the current project's memory summary and recent notes |
| `/memory sync` | Trigger distillation |
| `/memory clear` | Wipe the current project's memory |

## Configuration

Project Memory works with zero configuration. To tune it, add a `memory` block to
`~/.jcode/config.json`:

```json
{
  "memory": {
    "enabled": true,
    "generate": true,
    "model": "",
    "daily_token_budget": 300000,
    "cooldown_hours": 6,
    "max_age_days": 30,
    "max_unused_days": 45,
    "phase2_top_n": 40,
    "summary_inject_tokens": 1200
  }
}
```

| Setting | Default | Description |
|---|---|---|
| `enabled` | `true` | Master switch. `false` disables reading **and** writing memory. |
| `generate` | `true` | `false` still writes online notes and reads/injects memory, but never runs the distillation pipeline (a manual, zero-cost notebook — you or the notes curate the files). |
| `model` | `""` | Model for extraction. Empty falls back to `small_model`, then `model`. |
| `daily_token_budget` | `300000` | Hard ceiling on tokens the pipeline may spend per day. |
| `cooldown_hours` | `6` | Minimum gap between automatic pipeline runs. |
| `max_age_days` | `30` | Only sessions newer than this are considered for extraction. |
| `max_unused_days` | `45` | Summaries unused for this long are forgotten. |
| `phase2_top_n` | `40` | Max summaries kept after consolidation ranking. |
| `summary_inject_tokens` | `1200` | Cap on the memory summary injected into the prompt. |

### Turning it off

- **Manual notebook** (`"generate": false`) — reading, injection, and the
  `memory_note` tool all still work; only the paid distillation pipeline is
  disabled. `jcode memory sync` will refuse to run. Use this if you want to
  write and edit memory yourself without any model spend.
- **Fully off** (`"enabled": false`) — no memory is read, written, or injected,
  and the `memory_note` tool disappears from the agent's toolset.
