# jcode industry code-review PoC

This is a progressive benchmark and workflow prototype for one product question:
can jcode produce evidence-backed PR findings instead of turning incomplete or
uncertain review work into `OK`?

The reviewer is evaluated independently of any previous jcode PR. It receives
only the normal PR title/description, a frozen diff manifest, and bounded
repository access. Benchmark gold comments are never shown until the reviewer
has finished.

## Review standard

[`RUBRIC.md`](RUBRIC.md) combines Google's general reviewer dimensions with
OWASP's security, state, business-logic, concurrency, and failure-path focus.
Scoring uses the Martian Code Review Benchmark `Core` profile:

- included: bug, security, concurrency, data, API, performance, concrete test
  gaps, and documentation defects;
- excluded: style-only and speculative comments;
- primary metrics: precision, recall, F1/F2, signal-to-noise, completed manifest
  coverage, wall time, reviewer tokens, and judge tokens.

## Protocol

1. Python resolves immutable base/head commits and builds a deterministic
   manifest: selected files, explicit waivers, right-side added-line anchors,
   patch hashes, and bounded review units.
2. Each unit runs through two independent discovery lanes:
   - a bounded read-only context lane;
   - a no-tool adversarial-risk lane that constructs concrete error,
     concurrency, state, and boundary counterexamples from the patch.
3. The harness rejects incomplete unit acknowledgements and invalid line
   anchors. Provider failures and budget exhaustion are first-class incomplete
   states; neither may produce a clean conclusion.
4. A no-tool verifier confirms or rejects candidates. If discovery consumed the
   available target budget, `resume-verify` restores the frozen manifest and
   candidates without repeating discovery.
5. Only after publication decisions are frozen does a different model compare
   each finding semantically with every human gold comment.

The workflow's `budget` remains observational in the current jcode engine: calls
already in flight can overshoot it. `maxIterations` bounds each individual
agent, while the benchmark records the resulting overshoot and blocks `OK` when
verification is incomplete.

## Progressive result (2026-08-13)

Reviewer: `zhipuai-coding-plan/glm-5.2`  
Judge: `tencent-tokenhub/kimi-k3`  
Gold source: `withmartian/code-review-benchmark` at
`fbc5425c5eec52932aa1303708873d341968fa1c`

| Case | Role | Added lines | TP / FP / FN | Precision | Recall | Reviewer tokens | Reviewer wall time |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Grafana #90939 | development | 13 | 0 / 0 / 2 | 0% | 0% | 17,450 | 169s |
| Cal.com #14943 | frozen eval | 38 | 1 / 0 / 1 | 100% | 50% | 47,387 | 716s |
| Sentry #67876 | frozen eval + resume | 247 | 1 / 2 / 2 | 33.3% | 33.3% | 140,411 | 751s |
| **Aggregate** | smoke only | **298** | **2 / 2 / 5** | **50%** | **28.6%** | **205,248** | **1,635s** |

Aggregate F1 is 36.4%, F2 is 31.3%, severity-weighted recall is 33.3%,
signal-to-noise is 1.0, and reviewer cost is 688,752 tokens per 1,000 covered
added lines. The judge used another 7,498 tokens. Full artifacts and limitations
are in [`results/industry-core-2026-08-13.json`](results/industry-core-2026-08-13.json).

This is not a competitive ranking: three selected PRs are too few, Grafana was
used while developing the workflow, and the test checkouts were sparse to the
changed subsystems. It is sufficient to falsify the current `OK` behavior and
identify the next engineering bottlenecks.

## What the runs exposed

- Candidate generation works better than final recall suggests. Cal.com's
  non-SMS deletion issue was independently raised twice, then rejected by the
  verifier. Qwen's development probe found Grafana's missing cache re-check,
  then its verifier rejected it. A single conservative verifier is a recall
  bottleneck.
- Sentry discovery produced seven visible candidates but overshot the 120k
  target before verification. The run was correctly partial; checkpointed
  verification later confirmed three findings without spending discovery
  tokens again.
- Model/provider availability is part of review correctness. The Copilot
  GPT-5.6 Terra probe hit a structured-output adapter error, while Kimi for
  Coding returned a quota 403. Both are unscored failures, never clean reviews.
- Full manifest completion is necessary but not sufficient: all three effective
  runs delivered 100% of selected added lines, yet aggregate gold recall was
  only 28.6%.

## OpenCodeReview migration decision

Do not replace jcode's runtime with OpenCodeReview wholesale. Port its
deterministic control-plane ideas and keep jcode's model registry, tools,
permissions, sessions, and dynamic orchestration:

1. frozen input identity and a selected/completed/reused/failed/waived run
   manifest with typed terminal states;
2. deterministic file selection, related-file bundling, language/path rule
   matching, and exact changed-line positioning;
3. dispatch-time token reservation rather than a post-hoc target;
4. phase/file checkpoints and resume;
5. a separately observable candidate/reflection stage, without allowing an
   incomplete verifier to erase candidates or report clean.

OpenCodeReview is Apache-2.0, so direct code reuse is legally possible with the
required notices. The current PoC reimplements only protocol concepts. If code
is imported later, preserve attribution and isolate it behind jcode-native
interfaces.

Dynamic workflow is suitable for experiments and review policy, but not yet the
entire production control plane. The PoC needed an external git manifest and
checkpoint harness; jcode's workflow schema is JSON-parsed rather than fully
JSON-Schema enforced; aggregate budgets do not reserve tokens before parallel
dispatch; and one configured provider failed structured output. Productization
should add native review-run state and budget/checkpoint primitives, then leave
lane composition and model routing in workflow JavaScript.

A no-LLM delegation probe also succeeded: OpenCodeReview resolved Grafana's
exact merge base, selected the one changed Go file, and returned its 10,843-byte
Go rule pack for a host agent. The compact artifact is
[`results/opencodereview-delegation-grafana.json`](results/opencodereview-delegation-grafana.json).
This makes `ocr delegate preview/rule -> jcode workflow` a viable short-term
adapter, but shipping an external CLI as the permanent Cloud control plane would
duplicate jcode's session, provider, and lifecycle responsibilities.

## Reproduce

Run deterministic validation:

```bash
python3 -m unittest discover -s agent-eval/code-review/tests -v
jcode flow validate agent-eval/code-review/workflows/pr-review-v2.js
jcode flow validate agent-eval/code-review/workflows/verify-candidates.js
jcode flow validate agent-eval/code-review/workflows/judge-review.js
```

Run one prepared checkout:

```bash
python3 agent-eval/code-review/benchmark.py run \
  --repo /path/to/grafana-at-b1613e3 \
  --case agent-eval/code-review/cases/martian-grafana-90939.json \
  --jcode /path/to/jcode \
  --output agent-eval/code-review/runs/industry-core \
  --model zhipuai-coding-plan/glm-5.2 \
  --judge-model tencent-tokenhub/kimi-k3 \
  --profile core
```

Resume a run that reached `verifier_status=skipped_budget`:

```bash
python3 agent-eval/code-review/benchmark.py resume-verify \
  --run-dir agent-eval/code-review/runs/industry-core/<case>/<timestamp> \
  --case agent-eval/code-review/cases/<case>.json \
  --jcode /path/to/jcode \
  --model zhipuai-coding-plan/glm-5.2 \
  --judge-model tencent-tokenhub/kimi-k3
```

## Primary references

- [Google: What to look for in a code review](https://google.github.io/eng-practices/review/reviewer/looking-for.html)
- [OWASP Secure Code Review Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Secure_Code_Review_Cheat_Sheet.html)
- [Martian Code Review Benchmark](https://github.com/withmartian/code-review-benchmark)
- [CR-Bench](https://arxiv.org/abs/2603.11078)
- [OpenCodeReview](https://github.com/alibaba/open-code-review)
- [OpenAI Cookbook: Build Code Review with the Codex SDK](https://github.com/openai/openai-cookbook/blob/main/examples/codex/build_code_review_with_codex_sdk.md)
- [Codex code-review context skill](https://github.com/openai/codex/blob/main/.codex/skills/code-review-context/SKILL.md)
