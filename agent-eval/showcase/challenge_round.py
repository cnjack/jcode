#!/usr/bin/env python3
"""Multi-round driver for jcode showcase challenges.

Unlike suite/orchestrate.py (one throwaway sandbox per run), a challenge keeps a
persistent working directory across rounds so jcode can iterate on its own code:

  <root>/<id>/home/     isolated HOME (pinned model, auto_approve, full_access)
  <root>/<id>/box/      persistent project dir the agent works in
  <root>/<id>/rounds/   rN.events.jsonl / rN.result.json / rN.summary.json

Every round is one ACP prompt turn. The prompt for round N>1 should reference the
existing code and state the new requirements. All paths are resolved to absolute
(a relative HOME breaks os.UserHomeDir in the agent — see agent-eval memory).
"""
import argparse
import json
import os
import shutil
import subprocess
import sys
import time
from pathlib import Path

REAL_HOME = Path(os.path.expanduser("~"))
REAL_CFG = REAL_HOME / ".jcode" / "config.json"
REAL_CACHE = REAL_HOME / ".jcode" / "cache" / "models_dev.json"
REAL_MODELSTATE = REAL_HOME / ".jcode" / "model_state.json"

MODELS = {
    "glm-5.1": "zhipuai-coding-plan/glm-5.1",
    "glm-5.2": "tencent-tokenhub/glm-5.2",
}


def build_home(home_dir: Path, model_id: str, max_iter: int):
    (home_dir / ".jcode" / "cache").mkdir(parents=True, exist_ok=True)
    cfg = json.loads(REAL_CFG.read_text())
    provs = cfg.get("providers") or cfg.get("models") or {}
    out = {
        "providers": provs,
        "model": model_id,
        "auto_approve": True,
        "default_mode": "full_access",
        "max_iterations": max_iter,
    }
    (home_dir / ".jcode" / "config.json").write_text(json.dumps(out, indent=2))
    if REAL_CACHE.exists():
        shutil.copy(REAL_CACHE, home_dir / ".jcode" / "cache" / "models_dev.json")
    if REAL_MODELSTATE.exists():
        shutil.copy(REAL_MODELSTATE, home_dir / ".jcode" / "model_state.json")


def tree_summary(box: Path, limit=200):
    files = []
    for p in sorted(box.rglob("*")):
        if p.is_file():
            rel = str(p.relative_to(box))
            if any(seg in rel.split(os.sep) for seg in ("node_modules", ".git")):
                continue
            files.append({"path": rel, "bytes": p.stat().st_size})
            if len(files) >= limit:
                break
    return files


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", required=True, help="challenges root dir")
    ap.add_argument("--id", required=True)
    ap.add_argument("--round", type=int, required=True)
    ap.add_argument("--prompt-file", required=True)
    ap.add_argument("--model", default="glm-5.1", choices=sorted(MODELS))
    ap.add_argument("--timeout", type=int, default=2400, help="prompt turn timeout (s)")
    ap.add_argument("--max-iter", type=int, default=200)
    ap.add_argument("--bin", required=True)
    ap.add_argument("--harness", required=True)
    args = ap.parse_args()

    root = Path(args.root).resolve()
    bin_path = Path(args.bin).resolve()
    harness = Path(args.harness).resolve()
    prompt_file = Path(args.prompt_file).resolve()
    for p, what in ((bin_path, "bin"), (harness, "harness"), (prompt_file, "prompt-file")):
        if not p.exists():
            print(json.dumps({"error": f"{what} not found: {p}"}))
            sys.exit(2)

    chal = root / args.id
    home = chal / "home"
    box = chal / "box"
    rounds = chal / "rounds"
    rounds.mkdir(parents=True, exist_ok=True)
    box.mkdir(parents=True, exist_ok=True)
    if not (home / ".jcode" / "config.json").exists():
        build_home(home, MODELS[args.model], args.max_iter)

    events = rounds / f"r{args.round}.events.jsonl"
    result_path = rounds / f"r{args.round}.result.json"
    summary_path = rounds / f"r{args.round}.summary.json"

    env = dict(os.environ)
    env["HOME"] = str(home)
    cmd = [
        "timeout", str(args.timeout + 90),
        str(harness),
        "-bin", str(bin_path),
        "-cwd", str(box),
        "-promptfile", str(prompt_file),
        "-out", str(events),
        "-model", args.model,
        "-timeout", str(args.timeout),
        "-setup-timeout", "120",
    ]
    t0 = time.time()
    try:
        p = subprocess.run(cmd, env=env, capture_output=True, text=True,
                           timeout=args.timeout + 150)
        rc = p.returncode
        result_path.write_text(p.stdout.strip() or "{}")
        if p.stderr:
            (rounds / f"r{args.round}.harness.stderr").write_text(p.stderr)
    except subprocess.TimeoutExpired:
        rc = 124
        result_path.write_text(json.dumps({"stop_reason": "HARNESS_TIMEOUT"}))
    wall = time.time() - t0

    try:
        result = json.loads(result_path.read_text() or "{}")
    except Exception:
        result = {"stop_reason": "RESULT_PARSE_ERROR"}

    summary = {
        "id": args.id,
        "round": args.round,
        "model": args.model,
        "stop_reason": result.get("stop_reason"),
        "harness_rc": rc,
        "wall_seconds": round(wall, 1),
        "box": str(box),
        "files": tree_summary(box),
    }
    summary_path.write_text(json.dumps(summary, indent=2))
    print(json.dumps(summary, indent=2))


if __name__ == "__main__":
    main()
