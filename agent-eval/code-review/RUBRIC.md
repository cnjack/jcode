# Industry code-review rubric

The reviewer evaluates the change independently; benchmark gold comments are
never included in reviewer context.

## Review dimensions

1. Design and user-visible functionality.
2. Correctness, edge cases, and regressions.
3. API, data, persistence, and backward-compatibility contracts.
4. Authentication, authorization, validation, secrets, and unsafe data flow.
5. State transitions, concurrency, transactions, recovery, and error handling.
6. Resource use and material performance regressions.
7. Tests that would fail for the changed behavior's concrete failure modes.
8. Documentation defects that can cause incorrect use.

These dimensions are based on Google's reviewer guide and OWASP's secure code
review guidance. Style, naming, praise, speculative future concerns, and generic
requests for tests are not publishable findings in this benchmark.

## Finding acceptance contract

A publishable finding must:

- be introduced by the reviewed diff;
- describe a concrete causal path to an observable failure or violated contract;
- be anchored to a right-side added line;
- contain enough evidence for another engineer to verify it;
- survive an independent verifier; and
- not duplicate another finding.

## Benchmark profile

The primary profile is Martian Code Review Benchmark `Core`:

- Included: `bug`, `security`, `concurrency`, `data`, `api`, `perf`,
  `test_gap`, `doc_defect`.
- Excluded from primary scoring: `style`, `speculative`.

Report precision, recall, F1, F2, signal-to-noise ratio, manifest completion,
elapsed time, reviewer tokens, and judge tokens. Gold matching is semantic and
performed after review by a different model.

Sources:

- https://google.github.io/eng-practices/review/reviewer/looking-for.html
- https://cheatsheetseries.owasp.org/cheatsheets/Secure_Code_Review_Cheat_Sheet.html
- https://github.com/withmartian/code-review-benchmark
- https://arxiv.org/abs/2603.11078
