#!/usr/bin/env python3
"""Aggregate + log-analysis pass over the recorded jcode test runs.

Consumes the per-run record.json files produced by the orchestrator and emits
analysis.json: overall/per-model/per-case/per-tier metrics, stability
(pass@n, flakiness with Wilson CIs), token/cost accounting, and derived
failure-signature detection (non-termination, tool-call loops, silent empty
turns, error masking). The report generator renders this.
"""
import argparse
import json
import math
import os
import re
from collections import defaultdict
from pathlib import Path


def wilson(k, n, z=1.96):
    if n == 0:
        return (0.0, 0.0, 0.0)
    p = k / n
    denom = 1 + z * z / n
    center = (p + z * z / (2 * n)) / denom
    half = (z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n))) / denom
    return (round(p, 4), round(max(0, center - half), 4), round(min(1, center + half), 4))


def load_records(runs_dir):
    recs = []
    for rd in sorted(Path(runs_dir).glob("*/record.json")):
        try:
            recs.append(json.loads(rd.read_text()))
            recs[-1]["_dir"] = str(rd.parent)
        except Exception:
            pass
    return recs


def detect_loops(rundir):
    """Max count of identical (tool_title+rawInput) tool_call events = loop signal."""
    ev = Path(rundir) / "events.jsonl"
    if not ev.exists():
        return 0, 0
    counts = defaultdict(int)
    total = 0
    for line in ev.read_text(errors="ignore").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            d = json.loads(line)
        except Exception:
            continue
        if d.get("kind") != "session_update":
            continue
        u = d.get("data", {})
        if u.get("sessionUpdate") == "tool_call":
            total += 1
            key = json.dumps([u.get("title"), u.get("rawInput")], sort_keys=True)
            counts[key] += 1
    return (max(counts.values()) if counts else 0), total


def load_pricing(cache_path):
    """Best-effort {model_id_substr: {input, output} per 1M tokens} from models.dev."""
    pricing = {}
    try:
        data = json.loads(Path(cache_path).read_text())
    except Exception:
        return pricing
    def walk(obj):
        if isinstance(obj, dict):
            mid = obj.get("id")
            cost = obj.get("cost")
            if isinstance(mid, str) and isinstance(cost, dict) and ("input" in cost or "output" in cost):
                pricing[mid] = {"input": cost.get("input", 0), "output": cost.get("output", 0)}
            for v in obj.values():
                walk(v)
        elif isinstance(obj, list):
            for v in obj:
                walk(v)
    walk(data)
    return pricing


def est_cost(model_id, usage, pricing):
    model = model_id.split("/")[-1]
    pr = pricing.get(model)
    if not pr:
        for k, v in pricing.items():
            if k in model or model in k:
                pr = v
                break
    if not pr:
        return None
    inp = (usage.get("prompt", 0)) / 1e6 * pr.get("input", 0)
    out = (usage.get("completion", 0)) / 1e6 * pr.get("output", 0)
    return round(inp + out, 6)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--runs-dir", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--cache", default=os.path.expanduser("~/.jcode/cache/models_dev.json"))
    args = ap.parse_args()

    recs = load_records(args.runs_dir)
    pricing = load_pricing(args.cache)

    for r in recs:
        mrep, mtot = detect_loops(r["_dir"])
        r["_max_repeat_toolcall"] = mrep
        r["_total_toolcall_events"] = mtot
        r["_cost"] = est_cost(r.get("model_id", ""), r.get("usage_total", {}), pricing)
        # silent empty turn: claimed end_turn but produced nothing
        r["_silent_empty"] = (r.get("stop_reason") == "end_turn"
                              and r.get("agent_chunks", 0) == 0
                              and r.get("tool_calls", 0) == 0)
        # non-termination / abnormal stop
        r["_nonterminal"] = r.get("stop_reason") not in ("end_turn",)

    overall = {
        "total_runs": len(recs),
        "task_pass": sum(1 for r in recs if r.get("task_passed")),
        "contract_pass": sum(1 for r in recs if r.get("contracts_passed")),
        "clean_termination": sum(1 for r in recs if r.get("stop_reason") == "end_turn"),
        "silent_empty_turns": sum(1 for r in recs if r.get("_silent_empty")),
        "total_tokens": sum(r.get("usage_total", {}).get("total", 0) for r in recs),
        "total_wall_s": round(sum(r.get("wall_s", 0) or 0 for r in recs), 1),
    }
    tp = wilson(overall["task_pass"], overall["total_runs"])
    overall["task_pass_rate"] = tp[0]
    overall["task_pass_ci"] = [tp[1], tp[2]]
    costs = [r["_cost"] for r in recs if r.get("_cost") is not None]
    overall["total_cost_est"] = round(sum(costs), 4) if costs else None

    # per-model
    by_model = defaultdict(list)
    for r in recs:
        by_model[r.get("model")].append(r)
    models = {}
    for m, rs in by_model.items():
        n = len(rs)
        k = sum(1 for r in rs if r.get("task_passed"))
        p, lo, hi = wilson(k, n)
        toks = sum(r.get("usage_total", {}).get("total", 0) for r in rs)
        mc = [r["_cost"] for r in rs if r.get("_cost") is not None]
        recov = error_recovery(rs)
        models[m] = {
            "runs": n, "task_pass": k, "pass_rate": p, "ci": [lo, hi],
            "contract_pass": sum(1 for r in rs if r.get("contracts_passed")),
            "clean_termination": sum(1 for r in rs if r.get("stop_reason") == "end_turn"),
            "nonterminal": sum(1 for r in rs if r.get("_nonterminal")),
            "silent_empty": sum(1 for r in rs if r.get("_silent_empty")),
            "avg_tool_calls": round(sum(r.get("tool_calls", 0) for r in rs) / n, 2),
            "avg_wall_s": round(sum(r.get("wall_s", 0) or 0 for r in rs) / n, 1),
            "total_tokens": toks,
            "avg_tokens": round(toks / n) if n else 0,
            "cost_est": round(sum(mc), 4) if mc else None,
            "max_repeat_toolcall": max((r["_max_repeat_toolcall"] for r in rs), default=0),
            "error_recovery": recov,
        }

    # per-case (pass@n + flakiness), split by model
    by_case = defaultdict(list)
    for r in recs:
        by_case[r.get("case_id")].append(r)
    cases = {}
    for cid, rs in by_case.items():
        per_model = defaultdict(list)
        for r in rs:
            per_model[r.get("model")].append(r)
        cmodels = {}
        flaky = False
        for m, mrs in per_model.items():
            n = len(mrs)
            k = sum(1 for r in mrs if r.get("task_passed"))
            if 0 < k < n:
                flaky = True
            cmodels[m] = {"n": n, "pass": k, "rate": round(k / n, 3) if n else 0}
        cases[cid] = {
            "title": rs[0].get("case_title"),
            "category": rs[0].get("category"),
            "tier": rs[0].get("tier"),
            "n": len(rs),
            "pass": sum(1 for r in rs if r.get("task_passed")),
            "flaky": flaky,
            "by_model": cmodels,
            "avg_tool_calls": round(sum(r.get("tool_calls", 0) for r in rs) / len(rs), 2),
        }

    # per-tier
    by_tier = defaultdict(list)
    for r in recs:
        by_tier[r.get("tier")].append(r)
    tiers = {}
    for t, rs in by_tier.items():
        n = len(rs)
        k = sum(1 for r in rs if r.get("task_passed"))
        tiers[t] = {"n": n, "pass": k, "rate": round(k / n, 3) if n else 0}

    # failure signatures
    signatures = {
        "non_termination": [r["run_id"] for r in recs if r.get("_nonterminal")],
        "silent_empty_turn": [r["run_id"] for r in recs if r.get("_silent_empty")],
        "tool_loop_suspects": [r["run_id"] for r in recs if r.get("_max_repeat_toolcall", 0) >= 3],
        "contract_violations": [
            {"run_id": r["run_id"], "failed": [c["type"] for c in r.get("contracts", []) if not c["passed"]]}
            for r in recs if not r.get("contracts_passed")],
        "usage_absent_on_acp_stream": sum(1 for r in recs if not r.get("usage_on_acp_stream")),
        "usage_absent_pct": round(100 * sum(1 for r in recs if not r.get("usage_on_acp_stream")) / len(recs), 1) if recs else 0,
    }

    # oracle-level failure tally (which checks fail most)
    oracle_fail = defaultdict(int)
    for r in recs:
        if not r.get("task_passed"):
            for o in r.get("oracles", []):
                if not o["passed"]:
                    oracle_fail[f"{r['case_id']}:{o['type']}"] += 1

    analysis = {
        "overall": overall,
        "models": models,
        "cases": cases,
        "tiers": tiers,
        "signatures": signatures,
        "oracle_failures": dict(sorted(oracle_fail.items(), key=lambda x: -x[1])),
        "run_index": [
            {k: r.get(k) for k in ["run_id", "case_id", "model", "tier", "category",
             "task_passed", "contracts_passed", "stop_reason", "tool_calls",
             "wall_s", "_silent_empty", "_max_repeat_toolcall", "_cost",
             "usage_on_acp_stream"]} | {"tokens": r.get("usage_total", {}).get("total", 0)}
            for r in recs],
    }
    Path(args.out).write_text(json.dumps(analysis, indent=2, default=str))
    print(f"wrote {args.out}: {overall['task_pass']}/{overall['total_runs']} pass, "
          f"contract {overall['contract_pass']}/{overall['total_runs']}, "
          f"{overall['total_tokens']} tokens")


def error_recovery(rs):
    """Fraction of runs that hit a failed tool status but still finished end_turn."""
    hit = 0
    recovered = 0
    for r in rs:
        statuses = list((r.get("tool_status_end", {}) or {}).values())
        if "failed" in statuses:
            hit += 1
            if r.get("stop_reason") == "end_turn":
                recovered += 1
    return {"runs_with_tool_failure": hit, "recovered_end_turn": recovered}


if __name__ == "__main__":
    main()
