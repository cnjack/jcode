---
title: Dynamic Workflows
parent: Overview
nav_order: 18
---

# Dynamic Workflows

A dynamic workflow is a small **JavaScript script that orchestrates subagents at
scale**. Instead of the model deciding turn-by-turn what to spawn — with every
intermediate result piling into its context window — the plan moves into code: a
script holds the loop, the branching, and the intermediate results, and only the
final answer comes back to your session.

Workflows are for tasks that are *larger than a single agent call*: repository
audits, broad multi-file sweeps, multi-perspective reviews, migration planning.
The script fans work out across many agents, groups it into phases, and merges
the results — deterministically, so a stopped run can resume.

{: .note }
> **The plan lives in code.** The workflow script itself does no file, shell, or
> network I/O. Every side effect happens inside an `agent()` call, and those
> agents still go through jcode's normal tools, permissions, and hooks.

## When to use what

jcode gives you an escalating ladder for delegating work:

| Tool | What it is | Reach for it when |
|---|---|---|
| [Subagent](subagents.html) | One focused side task that returns a summary | A single self-contained lookup or task |
| [Skill](skills.html) | Reusable instructions the main agent follows | You want a repeatable *process*, run by the main agent |
| [Agent team](agent-teams.html) | Long-lived teammates that message each other | Ongoing collaboration between roles |
| **Workflow** | Repeatable orchestration across many agents, phases, and branches | A big task that needs fan-out, phases, or verification passes |

## Authoring a workflow

A saved workflow is a single `.js` file. It starts with an exported `meta` object,
followed by a plain-JavaScript body with top-level `await`:

```javascript
export const meta = {
  name: "repo-audit",
  description: "Audit a repository area and summarize risks",
  whenToUse: "Use when the user asks for a structured repository audit",
  phases: [
    { title: "Scan", detail: "Find relevant files" },
    { title: "Analyze", detail: "Run focused analysis agents" },
    { title: "Summarize", detail: "Merge findings into a report" },
  ],
};

// args carries per-run input, e.g. { area: "internal/auth" }
const area = (args && args.area) ? args.area : "the repository";

phase("Scan");
const map = await agent("Map " + area + ": key files and responsibilities.");

phase("Analyze");
const findings = await parallel(
  ["correctness", "security", "performance"].map((dim) => () =>
    agent("Audit " + area + " for " + dim + " issues.\n\n" + map, { label: "audit:" + dim })
  )
);

phase("Summarize");
return await agent("Merge these findings into a ranked report:\n\n" + findings.join("\n\n"));
```

Save it under:

- **`<project>/.jcode/workflows/`** — travels with the repo, shared with your team.
- **`~/.jcode/workflows/`** — personal, available in every project.

On a name collision, project workflows win over user workflows, which win over
built-ins.

## The primitives

The body is plain JavaScript with top-level `await`. These helpers are injected:

| Primitive | Description |
|---|---|
| `agent(prompt, opts?)` | Spawn **one** subagent. Returns its final text, or a validated object when `opts.schema` (a JSON Schema) is set. `opts`: `{ label, phase, model: "provider/model", agentType: "explore"｜"general"｜"coordinator", schema }`. |
| `parallel(thunks)` | Run an array of `() => agent(...)` thunks concurrently. **Barrier** — waits for all, returns results in order. A thunk that throws resolves to `null`, so `.filter(Boolean)`. |
| `pipeline(items, ...stages)` | Run each item through the stages independently, **no barrier** between stages. Each stage receives `(prevResult, originalItem, index)`. |
| `phase(title, detail?)` | Mark a progress group shown in the UI. |
| `log(msg, level?)` | Emit a progress line. |
| `workflow(name, args?)` | Run another saved workflow inline (one level deep). |
| `args` | The input passed at launch (`--args` on the CLI, or the tool's `args` field). |
| `budget` | `{ total, spent(), remaining() }` — token accounting for self-limiting loops. |

Use normal JavaScript for control flow — `if`, `for`, `.map`, `.filter`. End the
script by `return`-ing the final result.

{: .note }
> **`parallel` vs `pipeline`.** `parallel` is a barrier: reach for it only when the
> next step needs *every* result at once (e.g. a final merge). `pipeline` streams —
> item A can be in stage 3 while item B is still in stage 1 — so it finishes faster
> when stages are independent.

### Structured output

Pass a JSON Schema as `opts.schema` and `agent()` returns a validated object
instead of text:

```javascript
const rating = await agent("Rate this design 1-10 and explain.", {
  schema: { type: "object", properties: { score: { type: "number" }, why: { type: "string" } } },
});
if (rating.score < 5) log("low score: " + rating.why);
```

### Determinism

Workflows journal every `agent()` call so a run can resume. Non-determinism would
corrupt that cache, so **`Date.now()`, `Math.random()`, and argless `new Date()`
throw** inside a workflow. Pass timestamps via `args`, and vary prompts by index
for variety.

## Running a workflow

**From the terminal:**

```bash
jcode flow list                              # list builtin + user + project workflows
jcode flow show repo-audit --source          # inspect metadata and the script
jcode flow run repo-audit --args '{"area":"internal/auth"}'
jcode flow run ./my-workflow.js              # run a script by path
```

Progress streams to stderr; the final result prints to stdout.

**From a chat (TUI / Web / ACP):** just ask — "audit the auth module with a
workflow". The agent uses the `workflow_run` tool to either run a saved workflow
by name or write an inline script for the task and run it. Intermediate agent work
stays out of the conversation; only the result comes back.

## Built-in workflows

jcode ships a few workflows you can run or copy:

- **`repo-audit`** — audit an area across correctness / security / performance /
  maintainability, then merge into a ranked report.
- **`pr-review`** — review the current git diff across correctness, security, and
  tests, verify each finding, and give a go / no-go.
- **`roundtable`** — convene several independent expert agents on a question, have
  them critique each other, then synthesize one balanced recommendation.

```bash
jcode flow run roundtable --args '{"question":"Should we adopt gRPC internally?"}'
```

## Caps and safety

- **Concurrency** is capped (16 agents at a time by default); a hard per-run cap
  (1000 agents) backstops runaway loops. A per-run wall-clock timeout applies.
- **The script is sandboxed** — no shell, filesystem, or network access. All side
  effects go through `agent()`, and those agents obey your permission mode and
  hooks.
- Workflows can run many agents and **consume tokens quickly**. Start with a narrow
  target when validating an expensive workflow.
