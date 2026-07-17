#!/usr/bin/env python3
"""Summarize a computer-use campaign.

Two rules this enforces, because the harness does not:

1. **Discard runs that never happened.** A run with usage_total.total == 0 hit a
   provider error and never reached the model. Many oracles assert an *absence*,
   which an agent that never ran satisfies perfectly, so including these inflates
   every rate. A 2026-07-15 campaign scored 310 such runs as PASSING.

2. **Report an interval, not just a ratio.** 6/6 and 60/60 are both "100%" and
   are not the same claim. Wilson gives the honest width.

Usage: computer_report.py <runs-dir>
"""
import collections
import glob
import json
import math
import sys


def tokens(rec):
    return (rec.get("usage_total") or {}).get("total", 0)


def wilson(k, n, z=1.96):
    """Wilson score interval — behaves at k==n, unlike the normal approximation,
    which reports a zero-width interval for 6/6 and is simply lying."""
    if n == 0:
        return 0.0, 0.0
    p = k / n
    d = 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return max(0.0, c - h), min(1.0, c + h)


def main(runs_dir):
    files = glob.glob(f"{runs_dir}/*/record.json")
    if not files:
        print(f"no runs under {runs_dir}")
        return 1
    allr = [json.load(open(f)) for f in files]
    real = [r for r in allr if tokens(r) > 0]
    dead = [r for r in allr if tokens(r) == 0]
    phantom = sum(1 for r in dead if r.get("task_passed"))

    print(f"runs recorded : {len(allr)}")
    print(f"  real        : {len(real)}")
    print(f"  never ran   : {len(dead)}  (provider error; excluded from every rate below)")
    if dead:
        print(f"  of those, scored PASS by the harness: {phantom}"
              + ("  <- the gates are working" if phantom == 0 else "  <- PHANTOM PASSES"))
    if not real:
        print("\nnothing actually ran.")
        return 1

    print(f"\ntokens        : {sum(tokens(r) for r in real):,}")
    print(f"agent wall     : {sum(r.get('wall_s', 0) for r in real) / 3600:.2f} h")

    by_case = collections.defaultdict(lambda: [0, 0])
    for r in real:
        c = r["case_id"]
        by_case[c][1] += 1
        if r.get("task_passed"):
            by_case[c][0] += 1

    print(f"\n{'case':36s} {'pass':>9s}  {'rate':>6s}  95% CI (Wilson)")
    print("-" * 78)
    for c, (k, n) in sorted(by_case.items()):
        lo, hi = wilson(k, n)
        print(f"{c:36s} {k:4d}/{n:<4d} {100*k/n:5.1f}%  [{100*lo:5.1f}%, {100*hi:5.1f}%]")

    tot_k = sum(v[0] for v in by_case.values())
    tot_n = sum(v[1] for v in by_case.values())
    lo, hi = wilson(tot_k, tot_n)
    print("-" * 78)
    print(f"{'TOTAL':36s} {tot_k:4d}/{tot_n:<4d} {100*tot_k/tot_n:5.1f}%  [{100*lo:5.1f}%, {100*hi:5.1f}%]")

    # Flakiness is the point of repeating: a case that is sometimes-green is a
    # different problem from one that is always-red, and an aggregate hides both.
    flaky = {c: (k, n) for c, (k, n) in by_case.items() if 0 < k < n}
    print("\nflaky cases (neither always-pass nor always-fail):")
    if not flaky:
        print("  none — every case was deterministic across its repeats")
    for c, (k, n) in sorted(flaky.items()):
        print(f"  {c:36s} {k}/{n}  ({n-k} failure{'s' if n-k > 1 else ''})")

    # Tool-call distribution: a containment case that passes with zero tool calls
    # graded the model's judgment, not the enforcement path.
    print("\ntool calls per case (0 ⇒ the model declined before calling anything,")
    print("so the case graded the prompt rather than the gate):")
    for c in sorted(by_case):
        counts = collections.Counter(r.get("tool_calls", 0) for r in real if r["case_id"] == c)
        dist = " ".join(f"{k}×{v}" for k, v in sorted(counts.items()))
        print(f"  {c:36s} {dist}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1] if len(sys.argv) > 1 else "/tmp/cu-final"))
