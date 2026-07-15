#!/usr/bin/env python3
"""Unattended orchestrator for the jcode autonomous-execution test suite.

For every (case x model x repeat) it:
  1. builds an isolated throwaway HOME (real provider keys, pinned model,
     copied registry cache) so the run cannot touch the operator's real
     ~/.jcode, and an isolated sandbox cwd seeded with the case fixtures;
  2. drives one prompt turn through the ACP harness under an OS-level timeout;
  3. applies the case's deterministic oracles (verify.py) plus ACP
     contract-level checks computed from the recorded event stream;
  4. records a self-contained per-run record.json and appends to index.jsonl.

Nothing here trusts the agent's self-report — pass/fail comes from the sandbox
end-state, subprocess exit codes, and structural facts of the trajectory.
"""
import argparse
import concurrent.futures as cf
import json
import os
import shutil
import subprocess
import sys
import threading
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import verify  # noqa: E402

HERE = Path(__file__).resolve().parent
EVAL_ROOT = HERE.parent
REAL_HOME = Path(os.path.expanduser("~"))
REAL_CFG = REAL_HOME / ".jcode" / "config.json"
REAL_CACHE = REAL_HOME / ".jcode" / "cache" / "models_dev.json"
REAL_MODELSTATE = REAL_HOME / ".jcode" / "model_state.json"

TERMINAL_STOP = {"end_turn", "max_tokens", "max_turn_requests", "refusal", "cancelled"}
TERMINAL_TOOL_STATUS = {"completed", "failed"}

MODELS = {
    "glm-5.1": {"id": "zhipuai-coding-plan/glm-5.1"},
    "glm-5.2": {"id": "tencent-tokenhub/glm-5.2"},
    "qwen3.5-flash": {"id": "tencent-tokenhub/qwen3.5-flash"},
    "kimi-k2.7-code": {"id": "tencent-tokenhub/kimi-k2.7-code"},
    "kimi-k2.7-code-highspeed": {"id": "tencent-tokenhub/kimi-k2.7-code-highspeed"},
}

# repeats[model_label][tier]
DEFAULT_REPEATS = {
    "glm-5.1": {"smoke": 2, "core": 3, "stress": 3, "safety": 2, "frontend": 2, "memory": 2},
    "glm-5.2": {"smoke": 1, "core": 2, "stress": 2, "safety": 1, "frontend": 1, "memory": 1},
    "qwen3.5-flash": {"smoke": 1, "core": 1, "stress": 1, "safety": 1, "frontend": 1, "memory": 1},
    "kimi-k2.7-code": {"smoke": 2, "core": 2, "stress": 2, "safety": 2, "frontend": 1, "memory": 2, "computer": 3},
    "kimi-k2.7-code-highspeed": {"smoke": 2, "core": 2, "stress": 2, "safety": 2, "frontend": 1, "memory": 2, "computer": 3},
}

_print_lock = threading.Lock()


def log(msg):
    with _print_lock:
        print(msg, flush=True)


def build_home(home_dir: Path, model_id: str, max_iter: int, home_config: dict | None = None):
    (home_dir / ".jcode" / "cache").mkdir(parents=True, exist_ok=True)
    cfg = json.loads(REAL_CFG.read_text())
    provs = cfg.get("providers") or cfg.get("models") or {}
    out = {
        "providers": provs,
        "model": model_id,
        "auto_approve": True,
        "default_mode": "full_access",
        "max_iterations": max_iter,
        # Memory is ON (read + online notes) but the offline pipeline is OFF by
        # default so M1 cases don't fire a background distillation run (which
        # would race the oracles and burn real API quota). Pipeline cases turn
        # generate on explicitly via home_config.
        "memory": {"generate": False},
    }
    # shallow-merge case-level config overrides (e.g. {"memory": {"enabled": false}})
    for k, v in (home_config or {}).items():
        if k == "memory" and isinstance(v, dict) and isinstance(out.get("memory"), dict):
            out["memory"] = {**out["memory"], **v}
        else:
            out[k] = v
    (home_dir / ".jcode" / "config.json").write_text(json.dumps(out, indent=2))
    if REAL_CACHE.exists():
        shutil.copy(REAL_CACHE, home_dir / ".jcode" / "cache" / "models_dev.json")
    if REAL_MODELSTATE.exists():
        shutil.copy(REAL_MODELSTATE, home_dir / ".jcode" / "model_state.json")


def resolve_project_slug(bin_path: str, home_dir: Path, box: Path) -> str:
    """Ask the jcode binary for the memory project slug of `box`, so python
    never has to replicate the Go slug rule. Falls back to a value that makes
    slug-dependent cases fail loudly (red) instead of crashing the run."""
    env = dict(os.environ)
    env["HOME"] = str(home_dir)
    try:
        p = subprocess.run([bin_path, "memory", "path", "--format=slug"],
                           env=env, cwd=str(box), capture_output=True,
                           text=True, timeout=30)
        slug = (p.stdout or "").strip().splitlines()[-1] if p.stdout.strip() else ""
        if p.returncode == 0 and slug and "/" not in slug:
            return slug
    except Exception:
        pass
    return "UNRESOLVED-SLUG"


def seed_home_fixtures(bin_path: str, home_dir: Path, box: Path, home_fixtures: dict):
    """Write files into the isolated HOME. Keys/values may contain the
    {PROJECT_SLUG} placeholder, resolved via the jcode binary itself."""
    if not home_fixtures:
        return
    slug = None
    for rel, content in home_fixtures.items():
        if "{PROJECT_SLUG}" in rel or "{PROJECT_SLUG}" in content:
            if slug is None:
                slug = resolve_project_slug(bin_path, home_dir, box)
            rel = rel.replace("{PROJECT_SLUG}", slug)
            content = content.replace("{PROJECT_SLUG}", slug)
        fp = home_dir / rel
        fp.parent.mkdir(parents=True, exist_ok=True)
        fp.write_text(content)


def seed_fixtures(box: Path, fixtures: dict):
    for rel, content in fixtures.items():
        fp = box / rel
        fp.parent.mkdir(parents=True, exist_ok=True)
        fp.write_text(content)


def contract_checks(result: dict, events_path: Path, jcode_stderr: Path, usage_tot: dict):
    """ACP contract-level assertions independent of task success."""
    checks = []

    def add(name, ok, detail=""):
        checks.append({"type": name, "passed": bool(ok), "detail": detail})

    stop = result.get("stop_reason", "")
    add("stop_reason_terminal", stop in TERMINAL_STOP or stop in {"TIMEOUT"},
        f"stop_reason={stop}")
    # clean termination = a real end_turn (not timeout/prompt-error/harness-error)
    add("terminated_cleanly", stop == "end_turn", f"stop_reason={stop}")

    tse = result.get("tool_status_end", {}) or {}
    orphans = [tid for tid, st in tse.items() if st not in TERMINAL_TOOL_STATUS]
    add("no_orphan_tool_calls", len(orphans) == 0, f"non_terminal={orphans}")

    # stdout purity: the jcode process must keep stdout as pure JSON-RPC. Any
    # panic/log leakage would land on its stderr file; a parse error would have
    # broken the harness before it produced a result.
    noise = ""
    if jcode_stderr.exists():
        noise = jcode_stderr.read_text(errors="ignore").strip()
    bad = any(k in noise for k in ["panic:", "fatal error:", "runtime.", "goroutine "])
    add("stdout_pure_protocol", not bad, f"stderr_noise={noise[:120]!r}")

    # usage reported at all (ACP stream OR on-disk usage events). jcode does NOT
    # emit usage on the ACP stream today, so on-disk is the only source — tracked
    # separately below as a known gap.
    on_stream = bool(result.get("usage_update") or result.get("prompt_usage"))
    on_disk = (usage_tot or {}).get("total", 0) > 0
    add("usage_reported", on_stream or on_disk,
        f"acp_stream={on_stream} on_disk={on_disk}")

    return checks


def read_usage(home_dir: Path):
    p = home_dir / ".jcode" / "usage" / "events.jsonl"
    events = []
    if p.exists():
        for line in p.read_text(errors="ignore").splitlines():
            line = line.strip()
            if line:
                try:
                    events.append(json.loads(line))
                except Exception:
                    pass
    tot = {"prompt": 0, "completion": 0, "cached": 0, "reasoning": 0, "total": 0, "calls": 0}
    for e in events:
        for k in tot:
            tot[k] += e.get(k, 0) or 0
    return tot, events


def event_kind_counts(events_path: Path):
    counts = {}
    su_types = {}
    parse_errors = 0
    if events_path.exists():
        for line in events_path.read_text(errors="ignore").splitlines():
            line = line.strip()
            if not line:
                continue
            try:
                d = json.loads(line)
            except Exception:
                parse_errors += 1
                continue
            counts[d.get("kind")] = counts.get(d.get("kind"), 0) + 1
            if d.get("kind") == "session_update":
                u = d.get("data", {})
                key = u.get("sessionUpdate") or next(iter(u.keys()), "?")
                su_types[key] = su_types.get(key, 0) + 1
    return counts, su_types, parse_errors


def run_one(case, model_label, rep, runs_dir, bin_path, harness_path, max_iter, scale_timeout):
    run_id = f"{case['id']}__{model_label}__r{rep}"
    rundir = runs_dir / run_id
    if rundir.exists():
        shutil.rmtree(rundir)
    (rundir / "home").mkdir(parents=True)
    work = rundir / "work"
    box = work / "box"
    box.mkdir(parents=True)

    model_id = MODELS[model_label]["id"]
    build_home(rundir / "home", model_id, max_iter, case.get("home_config"))
    seed_fixtures(box, case.get("fixtures", {}))
    seed_home_fixtures(str(bin_path), rundir / "home", box, case.get("home_fixtures", {}))

    # safety scaffolding: canary just outside the sandbox cwd + parent snapshot
    canary_path = work / "CANARY_OUTSIDE.tmp"
    canary_path.write_text("do-not-touch canary\n")
    canary_sha = verify._sha(canary_path)
    parent_pre = verify.snapshot_tree(str(work))
    prerun = verify.snapshot_tree(str(box))

    case_timeout = int(case.get("timeout", 180) * scale_timeout)
    events_path = rundir / "events.jsonl"
    result_path = rundir / "result.json"
    stderr_path = Path(str(events_path) + ".stderr")

    env = dict(os.environ)
    env["HOME"] = str(rundir / "home")

    # A case is a sequence of steps sharing one HOME + one sandbox. Legacy
    # single-prompt cases are a one-step sequence. Prompt steps are separate
    # harness processes (= separate ACP sessions — that models cross-session
    # memory); cli steps run a jcode subcommand directly.
    steps = case.get("steps") or [{"prompt": case["prompt"]}]
    t0 = time.time()
    harness_rc = None
    result = {}
    step_records = []
    prompt_contract_sets = []
    last_events, last_stderr = events_path, stderr_path
    for i, step in enumerate(steps, 1):
        step_timeout = int(step.get("timeout", case_timeout))
        if "cli" in step:
            cli_cmd = ["timeout", str(step_timeout + 15), str(bin_path)] + list(step["cli"])
            try:
                p = subprocess.run(cli_cmd, env=env, cwd=str(box),
                                   capture_output=True, text=True,
                                   timeout=step_timeout + 30)
                rc = p.returncode
                tail = (p.stdout + "\n" + p.stderr)[-2000:]
            except subprocess.TimeoutExpired:
                rc, tail = 124, "CLI_TIMEOUT"
            step_records.append({"step": i, "kind": "cli", "argv": step["cli"],
                                 "rc": rc, "output_tail": tail})
            if rc != 0:
                result = {"stop_reason": "CLI_STEP_FAILED", "model": model_label,
                          "error": f"step {i} cli rc={rc}"}
                harness_rc = rc
                break
            continue

        step_events = rundir / f"events_{i}.jsonl"
        step_result_path = rundir / f"result_{i}.json"
        step_stderr = Path(str(step_events) + ".stderr")
        cmd = [
            "timeout", str(step_timeout + 45),
            str(harness_path),
            "-bin", str(bin_path),
            "-cwd", str(box),
            "-prompt", step["prompt"],
            "-out", str(step_events),
            "-model", model_label,
            "-timeout", str(step_timeout),
        ]
        try:
            p = subprocess.run(cmd, env=env, capture_output=True, text=True,
                               timeout=step_timeout + 90)
            harness_rc = p.returncode
            step_result_path.write_text(p.stdout.strip() or "{}")
        except subprocess.TimeoutExpired:
            harness_rc = 124
            step_result_path.write_text(json.dumps({"stop_reason": "HARNESS_TIMEOUT",
                                                    "model": model_label}))
        try:
            result = json.loads(step_result_path.read_text() or "{}")
        except Exception:
            result = {"stop_reason": "RESULT_PARSE_ERROR", "model": model_label}
        last_events, last_stderr = step_events, step_stderr
        usage_now, _ = read_usage(rundir / "home")
        prompt_contract_sets.append(
            contract_checks(result, step_events, step_stderr, usage_now))
        step_records.append({"step": i, "kind": "prompt",
                             "stop_reason": result.get("stop_reason"),
                             "tool_calls": result.get("tool_calls", 0),
                             "final_text": (result.get("final_text", "") or "")[:1000]})
        if result.get("stop_reason") not in TERMINAL_STOP:
            break  # later steps are meaningless after a broken turn

    # keep legacy filenames pointing at the last prompt step (analyze.py reads them)
    if last_events != events_path and last_events.exists():
        shutil.copy(last_events, events_path)
        if last_stderr.exists():
            shutil.copy(last_stderr, stderr_path)
    result_path.write_text(json.dumps(result, indent=2))
    wall = time.time() - t0

    ctx = {
        "sandbox": str(box), "result": result, "prerun": prerun,
        "parent_dir": str(work), "parent_pre": parent_pre,
        "canary_path": str(canary_path), "canary_sha": canary_sha,
        "rundir": str(rundir), "home": str(rundir / "home"),
        "step_records": step_records,
    }
    usage_tot, usage_events = read_usage(rundir / "home")
    ctx["usage_total"] = usage_tot
    ver = verify.verify_case(case, ctx)
    # contracts: every prompt step must satisfy the ACP contract, not just the last
    if prompt_contract_sets:
        contracts = []
        for i, cs in enumerate(prompt_contract_sets, 1):
            for c in cs:
                contracts.append({**c, "type": (f"s{i}:{c['type']}"
                                                if len(prompt_contract_sets) > 1 else c["type"])})
    else:
        contracts = [{"type": "no_prompt_step_ran", "passed": False,
                      "detail": "all steps were cli or step 1 failed"}]
    kinds, su_types, parse_errors = event_kind_counts(events_path)
    usage_on_acp_stream = bool(result.get("usage_update") or result.get("prompt_usage"))

    # collect the isolated debug.log (trimmed) + session transcript path
    dbg = rundir / "home" / ".jcode" / "debug.log"
    dbg_tail = ""
    if dbg.exists():
        lines = dbg.read_text(errors="ignore").splitlines()
        dbg_tail = "\n".join(lines[-60:])

    box_listing = []
    for dp, _dn, fn in os.walk(box):
        for f in fn:
            fp = Path(dp) / f
            box_listing.append(str(fp.relative_to(box)))

    record = {
        "run_id": run_id,
        "case_id": case["id"],
        "case_title": case["title"],
        "category": case["category"],
        "tier": case["tier"],
        "model": model_label,
        "model_id": model_id,
        "repeat": rep,
        "prompt": case.get("prompt") or " || ".join(
            s.get("prompt", "cli:" + " ".join(s.get("cli", []))) for s in steps),
        "steps": step_records,
        "task_passed": ver["passed"],
        "oracles": ver["oracles"],
        "contracts": contracts,
        "contracts_passed": all(c["passed"] for c in contracts),
        "stop_reason": result.get("stop_reason"),
        "error": result.get("error"),
        "wall_s": round(wall, 1),
        "elapsed_ms": result.get("elapsed_ms"),
        "tool_calls": result.get("tool_calls", 0),
        "tool_updates": result.get("tool_updates", 0),
        "tool_names": result.get("tool_names", {}),
        "tool_kind": result.get("tool_kind", {}),
        "tool_status_end": result.get("tool_status_end", {}),
        "thought_chunks": result.get("thought_chunks", 0),
        "agent_chunks": result.get("agent_chunks", 0),
        "permission_reqs": result.get("permission_reqs", 0),
        "plans": result.get("plans", 0),
        "final_text": (result.get("final_text", "") or "")[:4000],
        "usage_total": usage_tot,
        "event_kinds": kinds,
        "session_update_types": su_types,
        "event_parse_errors": parse_errors,
        "usage_on_acp_stream": usage_on_acp_stream,
        "box_files": box_listing,
        "harness_rc": harness_rc,
        "debug_tail": dbg_tail,
        "session_id": result.get("sessionId"),
    }
    (rundir / "record.json").write_text(json.dumps(record, indent=2))

    # prune the big isolated HOME to save space, keep the small evidence files
    _prune_home(rundir / "home")

    status = "PASS" if ver["passed"] else "FAIL"
    cstat = "ok" if record["contracts_passed"] else "CONTRACT!"
    log(f"  [{status}/{cstat}] {run_id}  stop={record['stop_reason']} "
        f"tools={record['tool_calls']} {record['wall_s']}s "
        f"tok={usage_tot['total']}")
    return record


def _prune_home(home_dir: Path):
    keep = {"usage", "sessions", "debug.log", "config.json", "memory"}
    jc = home_dir / ".jcode"
    if not jc.exists():
        return
    for child in jc.iterdir():
        if child.name not in keep:
            try:
                if child.is_dir():
                    shutil.rmtree(child)
                else:
                    child.unlink()
            except Exception:
                pass


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--bin", required=True)
    ap.add_argument("--harness", required=True)
    ap.add_argument("--runs-dir", required=True)
    ap.add_argument("--models", default="glm-5.1,glm-5.2")
    ap.add_argument("--cases", default="", help="comma-separated case ids (default all)")
    ap.add_argument("--tiers", default="", help="comma-separated tiers (default all)")
    ap.add_argument("--workers", type=int, default=4)
    ap.add_argument("--max-iter", type=int, default=80)
    ap.add_argument("--repeat-scale", type=float, default=1.0)
    ap.add_argument("--timeout-scale", type=float, default=1.0)
    ap.add_argument("--quick", action="store_true", help="1 repeat, glm-5.1 only")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    suite = json.loads((HERE / "testcases.json").read_text())
    cases = suite["cases"]
    if args.cases:
        want = set(args.cases.split(","))
        cases = [c for c in cases if c["id"] in want]
    if args.tiers:
        wt = set(args.tiers.split(","))
        cases = [c for c in cases if c["tier"] in wt]

    models = args.models.split(",")
    repeats = json.loads(json.dumps(DEFAULT_REPEATS))
    if args.quick:
        models = ["glm-5.1"]
        for m in repeats:
            repeats[m] = {k: 1 for k in repeats[m]}

    runs_dir = Path(args.runs_dir).resolve()
    runs_dir.mkdir(parents=True, exist_ok=True)
    bin_path = str(Path(args.bin).resolve())
    harness_path = str(Path(args.harness).resolve())

    jobs = []
    for m in models:
        for c in cases:
            n = max(1, int(round(repeats.get(m, {}).get(c["tier"], 1) * args.repeat_scale)))
            for r in range(1, n + 1):
                jobs.append((c, m, r))

    log(f"== jcode agent eval: {len(jobs)} runs "
        f"({len(cases)} cases x models={models}) workers={args.workers} ==")
    if args.dry_run:
        for c, m, r in jobs:
            log(f"  plan {c['id']} {m} r{r}")
        log(f"total {len(jobs)} runs")
        return

    records = []
    index_path = runs_dir / "index.jsonl"
    with index_path.open("w") as idx:
        with cf.ThreadPoolExecutor(max_workers=args.workers) as ex:
            futs = {ex.submit(run_one, c, m, r, runs_dir, bin_path, harness_path,
                              args.max_iter, args.timeout_scale): (c, m, r)
                    for (c, m, r) in jobs}
            for fut in cf.as_completed(futs):
                c, m, r = futs[fut]
                try:
                    rec = fut.result()
                except Exception as e:  # noqa: BLE001
                    rec = {"run_id": f"{c['id']}__{m}__r{r}", "case_id": c["id"],
                           "model": m, "tier": c["tier"], "task_passed": False,
                           "stop_reason": "ORCHESTRATOR_ERROR", "error": str(e)}
                    log(f"  [ERROR] {rec['run_id']}: {e}")
                records.append(rec)
                idx.write(json.dumps({k: rec.get(k) for k in
                          ["run_id", "case_id", "model", "tier", "task_passed",
                           "contracts_passed", "stop_reason", "tool_calls",
                           "wall_s"]}) + "\n")
                idx.flush()

    passed = sum(1 for r in records if r.get("task_passed"))
    log(f"== done: {passed}/{len(records)} task-pass ==")
    (runs_dir / "all_records.json").write_text(json.dumps(records, indent=2))


if __name__ == "__main__":
    main()
