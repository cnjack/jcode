# jcode Agent — Autonomous Execution Test Harness

A fully-automated, **unattended** test rig that stress-tests jcode's coding agent and
produces a showcase-quality HTML report plus a ranked defect list. Built to answer one
question before an SDK ships: **can we trust the agent to run on its own?**

Everything is judged by **deterministic verification** against the sandbox end-state and
the recorded protocol trajectory — never the agent's own "done".

## Why it's built the way it is

We first convened a five-seat design round-table (QA architect, eval methodologist, SRE,
security, SDK/DX — see [`roundtable/roundtable.json`](roundtable/roundtable.json)). Their
synthesis drove every design decision:

- **Drive the ACP surface, not the TTY.** `jcode acp` (JSON-RPC over stdio) is the exact
  headless surface a future SDK sits on, and it streams a structured event trajectory we
  can record and grade. The harness ([`harness/main.go`](harness/main.go)) is a real ACP
  client that runs one prompt turn, auto-approves permissions, and logs every event.
- **Double isolation per run.** Each run gets (1) a throwaway `HOME` — a copied config with
  a *pinned model* so the agent can never touch the operator's real `~/.jcode` (which holds
  live API keys), and (2) a throwaway sandbox `cwd` with fixtures and a canary file just
  outside it to detect filesystem escape.
- **Deterministic oracles + ACP contract checks.** File bytes, subprocess exit codes,
  grep-over-tree, mutation checks, read-only discipline — plus per-run contract assertions
  (one terminal StopReason, no orphan tool calls, pure-protocol stdout, usage reported).
- **Repeat for stability.** Cases repeat across models; we report pass@n, flakiness, and
  Wilson 95% CIs — not anecdotes.

## Layout

```
agent-eval/
  harness/         ACP client that drives one `jcode acp` prompt turn (Go, standalone module)
  suite/           testcases.json (declarative cases) · verify.py (oracles) · orchestrate.py (runner)
  analysis/        analyze.py (aggregation + log mining) · findings.json · report.py (HTML)
  roundtable/      the five expert perspectives that shaped the design
  runs/            per-run artifacts (git-ignored; regenerated)
  report/          the generated report.html
  site/            **new styled website** — open `site/index.html` to browse everything
  showcase/        legacy landing page + data generator; projects also mirrored under site/
```

## Browse the results

The easiest way to read everything is to open **`site/index.html`** in a browser.
It is a self-contained, warm-cream website (inspired by open-design.ai) that links to:

- the full Phase 1 report (`site/report.html`)
- the six discovered defects (`site/findings.html`)
- the five-seat round-table methodology (`site/roundtable.html`)
- the running docs (`site/docs.html`)
- the live frontend showcase with **large, usable iframes** (`site/showcase.html`)

## Run it

Requires a jcode binary and Go (to build the harness). **On macOS 26 the binary must be
built with `CGO_ENABLED=0`** — see finding F1.

```bash
# 1. build a working jcode + the ACP harness
CGO_ENABLED=0 go build -tags jcode_eval -o /tmp/jcode-nocgo ./cmd/jcode
( cd agent-eval/harness && go build -o /tmp/acp-harness . )

# 2. run the matrix (isolated, unattended)
python3 agent-eval/suite/orchestrate.py \
  --bin /tmp/jcode-nocgo --harness /tmp/acp-harness \
  --runs-dir agent-eval/runs --models glm-5.1,glm-5.2 --workers 5

# 3. analyze + render the report
python3 agent-eval/analysis/analyze.py --runs-dir agent-eval/runs --out agent-eval/runs/analysis.json
python3 agent-eval/analysis/report.py \
  --analysis agent-eval/runs/analysis.json \
  --roundtable agent-eval/roundtable/roundtable.json \
  --findings agent-eval/analysis/findings.json \
  --runs-dir agent-eval/runs --out agent-eval/report/report.html
```

Add `--quick` to `orchestrate.py` for a 1-repeat, single-model smoke pass, or
`--cases id1,id2` / `--tiers smoke,core` to scope it.

## Test cases

15 cases across four tiers (`suite/testcases.json`): **smoke** (file create, read-only Q&A),
**core** (fizzbuzz, targeted edit, bug-fix-from-failing-test, multi-file refactor, test
authoring, Go build/run, search/enumerate), **stress** (ambiguous → must clarify,
impossible → must halt cleanly, long-horizon multi-step), and **safety** (destructive-command
scoping, prompt-injection via file content, planted-secret handling).

## Headline findings

See the generated report and [`analysis/findings.json`](analysis/findings.json). The
highest-severity ones: a cgo build that **SIGABRTs on subprocess fork** on macOS 26 (F1);
model/API errors **masked as a successful `end_turn`** (F2); **no runner-level timeout** so
the agent can hang (F3, observed running `find /`); and an **unenforced filesystem/exec
boundary** (F4).
