---
title: Writing Workflows
parent: Overview
nav_order: 19
---

# Writing Workflows

This is the authoring reference for [dynamic workflows](workflows.html) — the exact
file format, every primitive, the control-flow patterns, and how a script is
validated. If you just want the concept and when to use one, read
[Dynamic Workflows](workflows.html) first.

## Anatomy of a workflow file

A workflow is a single `.js` file with two parts: an exported `meta` object, then a
plain-JavaScript body with top-level `await`.

```javascript
// 1. meta — a PURE literal object (no computed values, no function calls).
export const meta = {
  name: "triage-issues",
  description: "Triage a batch of issues and propose labels",
  whenToUse: "Use when the user asks to triage several issues at once",
  phases: [
    { title: "Read", detail: "Read each issue" },
    { title: "Label", detail: "Propose labels" },
  ],
};

// 2. body — plain JavaScript. Top-level await and top-level return are allowed
//    (the runtime wraps the body in an async function for you).
const ids = (args && args.ids) || [];
const results = await parallel(ids.map((id) => () => agent(`Triage issue ${id}`)));
return results;
```

The runtime strips the `export` keyword and wraps the body in an `async` function,
which is why **top-level `await` and top-level `return` both work**. You never write
the wrapper yourself.

### Where files live

| Location | Scope |
|---|---|
| `<project>/.jcode/workflows/` | Project — travels with the repo, shared with the team |
| `~/.jcode/workflows/` | Personal — available in every project |
| (built-in) | Ships with jcode |

On a name collision: **project > user > built-in**. The workflow's name is
`meta.name` (not the filename), and it becomes the `/<name>` trigger.

## `meta` reference

`meta` **must be a pure object literal** — no variables, function calls, or
computed keys. It is read without running the body, so anything dynamic will fail
to parse.

| Field | Type | Required | Notes |
|---|---|---|---|
| `name` | string | ✅ | Identifier; becomes the `/<name>` command |
| `description` | string | ✅ | One line, shown in `flow list` |
| `whenToUse` | string | — | Hint for matching a natural-language request to this workflow |
| `phases` | `{title, detail}[]` | — | Named progress groups shown in the UI |

## Primitive reference

The body runs in a sandbox: **no filesystem, shell, network, or Node APIs**. The
only capabilities are these injected globals. All side effects happen inside
`agent()`.

### `agent(prompt, opts?) → Promise`

Spawns one subagent with a fresh context window. Returns its final text, or a
validated object when `opts.schema` is set.

```javascript
const text = await agent("Summarize internal/auth");

const rating = await agent("Rate this design 1-10.", {
  label: "rate",              // display name in progress
  phase: "Analyze",           // progress group
  model: "openai/gpt-5",      // "provider/model" override; default = session model
  agentType: "explore",       // explore (read-only, default) | general | coordinator
  schema: {                   // JSON Schema → returns a validated object
    type: "object",
    properties: { score: { type: "number" }, why: { type: "string" } },
  },
});
if (rating.score < 5) log(rating.why);
```

| `opts` field | Meaning |
|---|---|
| `label` | Name shown in progress (defaults to a prompt snippet) |
| `phase` | Progress group this agent belongs to |
| `model` | `"provider/model"` override for this call only |
| `agentType` | `explore` (read-only), `general` (can edit/write), `coordinator` |
| `schema` | JSON Schema; when set, `agent()` returns a parsed object instead of text |

### `parallel(thunks) → Promise`

Runs an array of `() => Promise` thunks concurrently and **waits for all** (a
barrier). Results come back in thunk order. A thunk that throws resolves to `null`,
so filter:

```javascript
const results = await parallel([
  () => agent("task A"),
  () => agent("task B"),
]);
const ok = results.filter(Boolean);
```

Reach for `parallel` only when the next step needs **every** result at once (e.g. a
final merge).

### `pipeline(items, ...stages) → Promise`

Runs each item through the stages independently, with **no barrier between
stages** — item A can be in stage 3 while item B is still in stage 1. Each stage
callback receives `(prevResult, originalItem, index)`. A stage that throws drops
that item to `null`.

```javascript
const reviewed = await pipeline(
  files,
  (f) => agent(`Review ${f}`),                 // stage 1
  (review, f) => agent(`Verify: ${review}`),   // stage 2, sees the file too
);
```

`pipeline` finishes faster than a barrier when stages are independent.

### `phase(title, detail?)` and `log(msg, level?)`

Progress markers. `phase` starts a named group; `log` emits a line (`level` is
`info` / `warn` / `error`).

```javascript
phase("Analyze", "auditing 4 dimensions");
log("3/4 done");
```

### `workflow(name, args?) → Promise`

Runs another saved workflow inline and returns its result. **One level deep** — a
`workflow()` call inside a nested workflow throws.

### `args` and `budget`

- `args` — the input passed at launch (`--args` on the CLI, or the tool's `args`
  field). Access defensively: `const x = (args && args.x) || defaultX`.
- `budget` — `{ total, spent(), remaining() }` for self-limiting loops:

```javascript
while (budget.total && budget.remaining() > 50000) {
  const more = await agent("Find another bug.");
  log(`${budget.remaining()} tokens left`);
}
```

## Control-flow patterns

Use ordinary JavaScript (`if`, `for`, `.map`, `.filter`) plus the primitives.

**Fan-out → reduce** — map work out, merge the results:

```javascript
const parts = await parallel(items.map((x) => () => agent(`Analyze ${x}`)));
return await agent("Merge:\n" + parts.filter(Boolean).join("\n\n"));
```

**Find → adversarially verify** (streaming, no wasted wall-clock):

```javascript
const verified = await pipeline(
  areas,
  (a) => agent(`Find issues in ${a}`, { schema: FINDINGS }),
  (found) => parallel(found.findings.map((f) => () =>
    agent(`Try to refute: ${f.title}`, { schema: VERDICT }).then((v) => ({ ...f, v }))
  )),
);
```

**Loop-until** — accumulate to a target (guard on `budget.total` or a count):

```javascript
const bugs = [];
while (bugs.length < 10) {
  const r = await agent("Find a bug.", { schema: BUGS });
  bugs.push(...r.bugs);
}
```

## Determinism

Workflows journal every `agent()` call so a run can resume; non-determinism would
corrupt that cache. Therefore these **throw** inside a workflow:

- `Date.now()`
- `Math.random()`
- argless `new Date()`

Pass timestamps via `args`, and vary prompts/labels by index (`` `agent ${i}` ``)
for variety.

## Validation

A script is checked **before any agent runs**, so a syntax error costs zero tokens:

- The agent's `workflow_run` tool and `jcode flow run` both parse the `meta` block
  and **compile the whole script** first; a bad script comes back as an error to
  fix, and nothing spawns.
- Validate a file yourself without running it:

```bash
jcode flow validate ./my-workflow.js
# ✓ my-workflow is valid (2 phase(s))
# — or —
# workflow script does not compile: SyntaxError: ... Unexpected token
```

## Common mistakes

| Mistake | Fix |
|---|---|
| `meta` uses a variable or `[...spread]` | Make `meta` a pure literal; compute inside the body instead |
| `parallel([agent("a"), agent("b")])` | Pass **thunks**: `parallel([() => agent("a"), () => agent("b")])` |
| Reading a file / running a command in the script | The script has no I/O — do it inside an `agent()` call |
| `new Date()` / `Math.random()` for ids | Deterministic only — vary by index, pass time via `args` |
| Assuming `args` exists | `const x = (args && args.x) ?? fallback` |
| A huge `parallel` of hundreds of agents | Fine — the runtime caps concurrency (16) and total (1000) for you |

## A complete example

`.jcode/workflows/repo-audit.js` (a built-in you can copy):

```javascript
export const meta = {
  name: "repo-audit",
  description: "Audit a repository area across dimensions and merge into a ranked report",
  whenToUse: "Use when the user asks for a structured repository audit",
  phases: [
    { title: "Scan", detail: "Find the files in the target area" },
    { title: "Analyze", detail: "One audit agent per dimension" },
    { title: "Summarize", detail: "Merge into a ranked report" },
  ],
};

const area = (args && args.area) ? args.area : "the repository";

phase("Scan", "Mapping " + area);
const map = await agent("Map " + area + ": key files and responsibilities.", { agentType: "explore" });

phase("Analyze");
const findings = await parallel(
  ["correctness", "security", "performance"].map((d) => () =>
    agent("Audit " + area + " for " + d + " issues.\n\n" + map, { label: "audit:" + d, phase: "Analyze" })
      .then((text) => ({ dimension: d, text }))
  )
);

phase("Summarize");
return await agent(
  "Merge into one ranked report:\n\n" +
  findings.filter(Boolean).map((f) => "## " + f.dimension + "\n" + f.text).join("\n\n")
);
```
