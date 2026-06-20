---
title: Goals
nav_order: 9
---

# Goals

A **goal** is a persistent objective for your session. Instead of answering one
prompt and handing control back to you, jcode keeps working — turn after turn —
until the objective is done. Set it once and let the agent drive itself to the
finish line.

{: .note }
> Use a goal for substantial work with a verifiable end state — "make every test
> in `test/auth` pass", "migrate this module to the new API", "split this file
> until each piece is under 300 lines". For a quick question, just ask normally.

## Set a goal

In the TUI input area, type `/goal` followed by what you want done:

```text
/goal all tests in ./internal/auth pass and `go vet ./...` is clean
```

Setting a goal **immediately starts working** — you don't need to send a
separate prompt. While a goal is active, a `🎯` indicator above the input shows
its status, objective, and token usage.

Setting a new goal replaces any existing one.

## How it works

After each turn, jcode checks whether the goal is done. If not, it injects a
short reminder and starts another turn on its own, instead of returning control
to you. The agent verifies completion against the real state of your project —
files, command output, test results — and only then marks the goal complete and
stops.

A goal keeps running until one of these happens:

- The agent **verifies it's complete** and stops.
- The agent decides it is **genuinely blocked** and stops.
- It hits the **safety cap** of 25 automatic turns.
- You **cancel** it (press <kbd>Esc</kbd>) or run `/goal clear`.

## Check status and clear

Run `/goal` with no arguments to see the current objective, status, and how many
tokens it has used:

```text
/goal
```

Remove an active goal before it finishes:

```text
/goal clear
```

## Writing a good objective

The agent proves a goal is done by checking your project's real state, so phrase
the objective around something it can verify:

- **A measurable end state** — tests pass, a build exits 0, a file count, an
  empty queue.
- **How to check it** — e.g. "`go test ./...` exits 0" or "`git status` is
  clean".
- **Constraints that must hold** — e.g. "without changing any test files".

The objective can be up to 4,000 characters.

## Goals vs. todos

They work together but mean different things:

| | Purpose |
|---|---|
| **Goal** | The overall end state you want reached — the "where". Keeps the agent going. |
| **Todos** | The step-by-step checklist *within* the work — the "how". |

A single goal usually spawns several todos as the agent breaks the work down.

## In the web UI

Open the web interface (`jcode web`) and click the **🎯 Goal** toggle in the
input toolbar. The prompt box is now armed: the next message you send becomes
the session goal and work starts immediately. While a goal is active a banner
shows its status and token usage, with a **Clear** button to remove it.
Typing `/goal …` in the chat box works too.

## In editors (ACP)

When jcode runs as a headless agent (`jcode acp`), `goal` appears in the editor's
slash-command list. Use it the same way: `/goal <objective>`, `/goal status`, or
`/goal clear`.

## Resuming a session

A goal that is still active when you close a session is restored when you resume
that session — with `--resume`, the in-TUI `/resume` command, the web session
list, or an editor's session restore. The objective and recorded token usage are
preserved.

## When to use a goal

**Good fits** — big jobs that need to be driven to completion:

- Fix every lint error across the project
- Add tests for a module until coverage hits a target
- Migrate an old API to a new framework
- Work through a backlog of flagged issues until the queue is empty

**Skip it** for one-line questions, or for open-ended exploration with no clear
"done" ("see what could be improved") — without a verifiable finish line the
agent has no way to know when to stop.

## Command summary

| Command | Action |
|---|---|
| `/goal <objective>` | Set a goal and start working toward it |
| `/goal` or `/goal status` | Show the current goal's status |
| `/goal clear` | Remove the active goal |
