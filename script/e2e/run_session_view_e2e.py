#!/usr/bin/env python3
"""Run the cross-session-view real-model E2E cases, one fresh HOME each.

This is the phase-2 driver that the test report is generated from.  It does not
implement any transport logic itself: it seeds a HOME, calls
`session_view_acp_probe.py`, and turns the probe's `PROBE_JSON` result into a
verdict per case.

Cases (design §6.3 + scope §6):

  positive            same-project search + read, the sentinel must be reported
  negative            other-project read must fail closed with the *same* text
                      as a missing session, and must not leak the content
  disabled            `session_view.enabled=false`: the three tools must not be
                      attached to the model at all (deterministic evidence from
                      the recorder's model_request observation)
  archived_default    archived session hidden unless requested
  archived_included   ... and visible when `include_archived` is used
  corrupt             a session with garbage/blank/torn lines is still readable

Every case gets its own `<out>/<case>/home` + `<out>/<case>/ws` seed, so no case
can be influenced by a session an earlier case created.

The runner also fingerprints the exact tree it tested (git rev, hash of
`git status --porcelain`, hash of `git diff`, binary sha256) so the report can be
tied to the PR commit.

Usage
-----
    ./run_session_view_e2e.py --bin /tmp/jcode-e2e-bin --out-dir /tmp/e2e-run
    ./run_session_view_e2e.py --bin ... --only positive,negative --timeout 300
    ./run_session_view_e2e.py --dry-run --bin /tmp/jcode-e2e-bin

Exit codes: 0 all cases pass, 1 at least one case failed, 2 usage/precondition.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
SEED = os.path.join(HERE, "seed_sessions.py")
PROBE = os.path.join(HERE, "session_view_acp_probe.py")

# Fixture constants (must stay in sync with seed_sessions.py).
SENTINEL_WS = "zebra-unicorn-42"
SENTINEL_OTHER = "quokka-99"
SENTINEL_ARCHIVED = "narwhal-archived-7"
SENTINEL_CORRUPT = "ocelot-corrupt-13"
UUID_OTHER = "22222222-2222-4222-8222-222222222222"
#: A syntactically valid id that has no session file and no index entry.
UUID_MISSING = "00000000-0000-4000-8000-000000000000"
UUID_ARCHIVED = "33333333-3333-4333-8333-333333333333"

SESSION_TOOLS = ("session_list", "session_search", "session_read")

#: Guard rail quoted from the spec: the bypass tools are out of scope for the
#: feature, but they must be *forbidden in the prompt* so the audit means
#: something.
NO_BYPASS = "Do not use execute, grep, glob or read on ~/.jcode."


def case_definitions() -> dict[str, dict]:
    """One entry per case: seed preconditions, prompt, probe assertions."""
    return {
        "positive": {
            "seed": True,
            "prompt": (
                "Use only the session_search and session_read tools. "
                f"{NO_BYPASS} "
                f"Search prior sessions in this workspace for {SENTINEL_WS}, then read the "
                "session that contains it and quote the exact line containing that token."
            ),
            "expect": [
                "--expect-tool", "session_search",
                "--expect-tool", "session_read",
                "--expect-contains", SENTINEL_WS,
                "--expect-no-permission",
                "--expect-attached-tool", "session_search",
                "--expect-attached-tool", "session_read",
                "--expect-attached-tool", "session_list",
            ],
        },
        "negative": {
            "seed": True,
            "prompt": (
                "Use only the session_read tool. "
                f"{NO_BYPASS} "
                f"Read session_id={UUID_OTHER} and tell me exactly what it contains."
            ),
            "expect": [
                "--expect-tool", "session_read",
                "--expect-tool-result-contains", "ERROR[SESSION_VIEW_NOT_FOUND]",
                "--expect-not-contains", SENTINEL_OTHER,
                "--expect-not-contains", "other-project",
                "--expect-no-permission",
            ],
        },
        "missing": {
            "seed": True,
            "prompt": (
                "Use only the session_read tool. "
                f"{NO_BYPASS} "
                f"Read session_id={UUID_MISSING} and tell me exactly what it contains."
            ),
            # Paired with `negative`: the two tool outputs must be byte-identical
            # (scope §6: fail closed 且与"不存在"同文案).  The runner compares
            # them automatically after the run.
            "expect": [
                "--expect-tool", "session_read",
                "--expect-tool-result-contains", "ERROR[SESSION_VIEW_NOT_FOUND]",
                "--expect-no-permission",
            ],
        },
        "disabled": {
            "seed": True,
            "config_patch": {"session_view": {"enabled": False}},
            "prompt": (
                f"Search this workspace's prior sessions for {SENTINEL_WS} using the "
                "session_search tool. If no session tools are available to you, say "
                "explicitly which tools you have and stop."
            ),
            "expect": [
                "--forbid-attached-tool", "session_list",
                "--forbid-attached-tool", "session_search",
                "--forbid-attached-tool", "session_read",
                "--forbid-tool", "session_list",
                "--forbid-tool", "session_search",
                "--forbid-tool", "session_read",
            ],
        },
        "archived_default": {
            "seed": True,
            "prompt": (
                "Use only the session_search tool. "
                f"{NO_BYPASS} "
                f"Search prior sessions in this workspace for {SENTINEL_ARCHIVED} "
                "with default options and report what you find."
            ),
            # Careful: a no-match reply echoes the QUERY back
            # (`No prior session matches found for "<query>".`), so the sentinel
            # can never be a forbidden marker here.  Only content that exists
            # solely in the archived session is meaningful: its UUID and title.
            "expect": [
                "--expect-tool", "session_search",
                "--forbid-tool-result-contains", UUID_ARCHIVED,
                "--forbid-tool-result-contains", "Archived investigation",
                "--expect-no-permission",
            ],
        },
        "archived_included": {
            "seed": True,
            "prompt": (
                "Use only the session_search tool. "
                f"{NO_BYPASS} "
                f"Search prior sessions in this workspace for {SENTINEL_ARCHIVED} and set "
                "include_archived to true so the user's archived sessions are searched too. "
                "Quote the matching line."
            ),
            "expect": [
                "--expect-tool", "session_search",
                "--expect-tool-result-contains", SENTINEL_ARCHIVED,
                "--expect-no-permission",
            ],
        },
        "corrupt": {
            "seed": True,
            "prompt": (
                "Use only the session_search tool. "
                f"{NO_BYPASS} "
                f"Search prior sessions in this workspace for {SENTINEL_CORRUPT} and quote "
                "the matching line. That session file may contain corrupted lines; report "
                "what you can still read rather than giving up."
            ),
            "expect": [
                "--expect-tool", "session_search",
                "--expect-tool-result-contains", SENTINEL_CORRUPT,
                "--expect-no-permission",
            ],
        },
    }


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def sha256_file(path: str) -> str:
    try:
        with open(path, "rb") as fh:
            return hashlib.sha256(fh.read()).hexdigest()
    except OSError:
        return ""


def git(repo: str, *args: str) -> tuple[int, str]:
    try:
        proc = subprocess.run(
            ["git", "-C", repo, *args], capture_output=True, text=True, check=False
        )
        return proc.returncode, proc.stdout
    except OSError as exc:  # git missing
        return 1, f"<git error: {exc}>"


def worktree_content_sha(repo: str) -> tuple[str, int]:
    """Hash the CONTENT of every modified/untracked file, from ONE status snapshot.

    `git status --porcelain` only lists paths, so a content edit inside an
    untracked directory (e.g. `internal/sessionview/`) would not change its
    hash.  Paths are therefore taken from a single `git status -z` snapshot
    (the same snapshot as `statusPorcelainSha256`, so the two can never
    disagree) and each file's content is hashed.  Untracked directories are
    expanded by walking them; gitignored artifacts never appear.
    """
    _, status_z = git(repo, "status", "--porcelain=v1", "-z")
    paths: set[str] = set()
    for entry in status_z.split("\0"):
        if len(entry) < 4:
            continue
        rel = entry[3:]
        full = os.path.join(repo, rel)
        if rel.endswith("/") or os.path.isdir(full):
            for root, _dirs, files in os.walk(full):
                for name in files:
                    paths.add(os.path.relpath(os.path.join(root, name), repo))
        else:
            paths.add(rel)

    # Python byte-code caches are build noise, not source: exclude them so the
    # fingerprint is reproducible after a plain checkout.
    paths = {p for p in paths if "__pycache__" not in p and not p.endswith(".pyc")}

    digest = hashlib.sha256()
    count = 0
    for rel in sorted(paths):
        full = os.path.join(repo, rel)
        digest.update(rel.encode("utf-8"))
        digest.update(b"\0")
        if os.path.isfile(full):
            digest.update(sha256_file(full).encode("ascii") or b"<unreadable>")
            count += 1
    return digest.hexdigest(), count


def default_repo_root() -> str:
    """Resolve the repo root (NOT `script/` — `git -C` walks up, so a wrong
    root makes every `os.path.join(root, rel)` point at nothing)."""
    rc, out = git(HERE, "rev-parse", "--show-toplevel")
    if rc == 0 and out.strip():
        return out.strip()
    return os.path.dirname(os.path.dirname(HERE))


def tree_fingerprint(repo: str, bin_path: str) -> dict:
    """Record the exact state under test so the report can be matched to the PR."""
    _, rev = git(repo, "rev-parse", "HEAD")
    _, status = git(repo, "status", "--porcelain=v1")
    _, diff = git(repo, "diff")
    _, diff_stat = git(repo, "diff", "--stat")
    _, branch = git(repo, "rev-parse", "--abbrev-ref", "HEAD")
    changed = [line for line in status.splitlines() if line.strip()]
    worktree_sha, worktree_files = worktree_content_sha(repo)
    return {
        "branch": branch.strip(),
        "rev": rev.strip(),
        "statusPorcelainSha256": sha256_bytes(status.encode("utf-8")),
        "diffSha256": sha256_bytes(diff.encode("utf-8")),
        "worktreeSha256": worktree_sha,
        "worktreeFileCount": worktree_files,
        "diffStat": diff_stat.strip(),
        "changedPaths": changed,
        "binary": bin_path,
        "binarySha256": sha256_file(bin_path),
        "binaryMtime": time.strftime(
            "%Y-%m-%dT%H:%M:%S%z", time.localtime(os.path.getmtime(bin_path))
        )
        if os.path.exists(bin_path)
        else "",
    }


def patch_config(home: str, patch: dict) -> None:
    """Merge a small patch into the seeded `config.json` (temp HOME only)."""
    path = os.path.join(home, ".jcode", "config.json")
    with open(path, "r", encoding="utf-8") as fh:
        cfg = json.load(fh)
    for key, value in patch.items():
        if isinstance(value, dict) and isinstance(cfg.get(key), dict):
            cfg[key].update(value)
        else:
            cfg[key] = value
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(cfg, fh, indent=2, ensure_ascii=False)
    os.chmod(path, 0o600)


def run(cmd: list[str], cwd: str | None = None) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, capture_output=True, text=True, check=False, cwd=cwd)


def run_case(name: str, spec: dict, args, out_dir: str) -> dict:
    case_dir = os.path.join(out_dir, name)
    home = os.path.join(case_dir, "home")
    ws = os.path.join(case_dir, "ws")
    if os.path.isdir(case_dir):
        shutil.rmtree(case_dir)
    os.makedirs(case_dir, exist_ok=True)

    record: dict = {
        "case": name,
        "status": "error",
        "startedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "seed": {"home": home, "ws": ws},
        "commands": [],
        "probe": {},
        "failures": [],
    }

    seed_cmd = [
        sys.executable, SEED, "--home", home, "--ws", ws, "--force",
    ]
    record["commands"].append(" ".join(seed_cmd))
    if args.dry_run:
        probe_cmd = [
            sys.executable, PROBE, "--bin", args.bin, "--home", home, "--cwd", ws,
            "--timeout", str(args.timeout), "--prompt", spec["prompt"],
            *spec.get("expect", []),
        ]
        record["commands"].append(" ".join(probe_cmd))
        record["status"] = "dry-run"
        return record

    started = time.time()
    seeded = run(seed_cmd)
    if seeded.returncode != 0:
        record["failures"].append(f"seed failed (exit {seeded.returncode}): {seeded.stderr[-1200:]}")
        record["stderr"] = seeded.stderr[-2000:]
        return record

    if spec.get("config_patch"):
        patch_config(home, spec["config_patch"])
        record["configPatch"] = spec["config_patch"]

    json_out = os.path.join(case_dir, "probe.json")
    events_out = os.path.join(case_dir, "events.jsonl")
    probe_cmd = [
        sys.executable, PROBE,
        "--bin", args.bin,
        "--home", home,
        "--cwd", ws,
        "--timeout", str(args.timeout),
        "--setup-timeout", str(args.setup_timeout),
        "--mode", args.mode,
        "--prompt", spec["prompt"],
        "--json-out", json_out,
        "--events-out", events_out,
        *spec.get("expect", []),
    ]
    record["commands"].append(" ".join(probe_cmd))
    proc = run(probe_cmd)
    record["searchViewExit"] = proc.returncode
    record["elapsedMs"] = int((time.time() - started) * 1000)
    record["stderrTail"] = proc.stderr[-4000:]
    record["stdoutBytes"] = len(proc.stdout)

    probe = {}
    if os.path.isfile(json_out):
        try:
            with open(json_out, "r", encoding="utf-8") as fh:
                probe = json.load(fh)
        except (OSError, json.JSONDecodeError) as exc:
            record["failures"].append(f"probe.json unreadable: {exc}")
    record["probe"] = summarize_probe(probe)
    record["failures"].extend(probe.get("failures") or [])

    if proc.returncode == 0 and not record["failures"]:
        record["status"] = "pass"
    elif proc.returncode in (2, 3, 4):
        record["status"] = "error"
        if not record["failures"]:
            record["failures"].append(f"probe failed at the transport level (exit {proc.returncode})")
    else:
        record["status"] = "fail"
    if proc.returncode == 5:
        record["status"] = "fail"
    return record


def summarize_probe(probe: dict) -> dict:
    """Trim the probe result down to what the report and the summary table need."""
    persisted = probe.get("persisted") or {}
    tools = [
        {"name": t.get("name"), "args": str(t.get("args") or "")[:300],
         "resultFirstLine": (str(t.get("result") or "").splitlines() or [""])[0][:300]}
        for t in (persisted.get("tools") or [])
    ]
    attached = persisted.get("visibleTools") or []
    # Full session_read output, kept for the negative-vs-missing text comparison.
    read_results = [
        str(t.get("result") or "")[:4000]
        for t in (persisted.get("tools") or [])
        if t.get("name") == "session_read"
    ]
    return {
        "ok": probe.get("ok"),
        "stopReason": probe.get("stopReason"),
        "sessionId": probe.get("sessionId"),
        "elapsedMs": probe.get("elapsedMs"),
        "finalText": probe.get("finalText") or "",
        "toolCalls": tools,
        "toolNames": sorted({t["name"] for t in tools if t.get("name")}),
        "sessionToolsAttached": sorted(n for n in attached if n.startswith("session_")),
        "attachedToolCount": len(attached),
        "modelRequests": persisted.get("modelRequests"),
        "permissionRequests": len(probe.get("permissionRequests") or []),
        "counts": probe.get("counts"),
        "sessionReadResults": read_results,
        "failures": probe.get("failures") or [],
        "persistedFile": persisted.get("file") or "",
    }


def compare_not_found(records: list[dict]) -> dict:
    """Scope §6: an unauthorized read and a missing id must be indistinguishable.

    Compares the persisted `session_read` output text of the `negative` and
    `missing` cases byte-for-byte.
    """
    by_case = {record["case"]: record for record in records}
    if "negative" not in by_case or "missing" not in by_case:
        return {"checked": False, "reason": "negative/missing cases not both run"}
    def first_read(case: str) -> str:
        results = (by_case[case].get("probe") or {}).get("sessionReadResults") or []
        return results[0] if results else ""
    unauthorized = first_read("negative")
    missing = first_read("missing")
    if not unauthorized or not missing:
        return {"checked": False, "reason": "session_read produced no output in one of the cases"}
    return {
        "checked": True,
        "identical": unauthorized == missing,
        "unauthorizedText": unauthorized,
        "missingText": missing,
    }


def write_summary(out_dir: str, records: list[dict], fingerprint: dict, args) -> dict:
    not_found = compare_not_found(records)
    payload = {
        "generatedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "binary": args.bin,
        "tree": fingerprint,
        "mode": args.mode,
        "timeout": args.timeout,
        "notFoundIndistinguishable": not_found,
        "cases": records,
    }
    with open(os.path.join(out_dir, "summary.json"), "w", encoding="utf-8") as fh:
        json.dump(payload, fh, indent=1, ensure_ascii=False)

    lines = [
        "# cross-session view E2E — run summary",
        "",
        f"- generated: {payload['generatedAt']}",
        f"- branch: `{fingerprint['branch']}` @ `{fingerprint['rev']}`",
        f"- status sha256: `{fingerprint['statusPorcelainSha256']}`",
        f"- diff sha256: `{fingerprint['diffSha256']}`",
        f"- worktree content sha256: `{fingerprint['worktreeSha256']}` "
        f"({fingerprint['worktreeFileCount']} modified/untracked files)",
        f"- worktree stable during run: **{fingerprint.get('stableDuringRun')}**",
        f"- binary: `{fingerprint['binary']}` sha256 `{fingerprint['binarySha256']}`",
        f"- session mode: `{args.mode}`, turn timeout: {args.timeout}s",
    ]
    if not_found.get("checked"):
        verdict = "YES (byte-identical)" if not_found.get("identical") else "NO — TEXT DIFFERS"
        lines.append(f"- not-found vs unauthorized indistinguishable: **{verdict}**")
    lines += [
        "",
        "| case | verdict | session_* attached | tool calls | notes |",
        "|---|---|---|---|---|",
    ]
    for rec in records:
        probe = rec.get("probe") or {}
        attached = ",".join(probe.get("sessionToolsAttached") or []) or "—"
        calls = ", ".join(probe.get("toolNames") or []) or "—"
        notes = "; ".join(rec.get("failures") or [])[:220] or "—"
        lines.append(f"| {rec['case']} | **{rec['status']}** | {attached} | {calls} | {notes} |")
    lines.append("")
    with open(os.path.join(out_dir, "summary.md"), "w", encoding="utf-8") as fh:
        fh.write("\n".join(lines))
    return not_found


def main(argv: list[str]) -> int:
    p = argparse.ArgumentParser(
        prog="run_session_view_e2e.py",
        description="Run the cross-session-view real-model E2E cases (fresh seed per case).",
    )
    p.add_argument("--bin", default=os.environ.get("JCODE_BIN", ""), help="jcode binary (or $JCODE_BIN)")
    p.add_argument("--out-dir", default="", help="where to write per-case artifacts "
                                                 "(default: /tmp/session-view-e2e-<ts>)")
    p.add_argument("--repo", default=default_repo_root(), help="repo root used for the fingerprint")
    p.add_argument("--mode", default="full_access", help="session mode for every case (default full_access)")
    p.add_argument("--timeout", type=float, default=300.0, help="per-turn timeout, seconds (default 300)")
    p.add_argument("--setup-timeout", type=float, default=120.0, help="initialize/session-new timeout")
    p.add_argument("--only", default="", help="comma-separated subset of case names")
    p.add_argument("--dry-run", action="store_true", help="print the commands and exit")
    args = p.parse_args(argv)

    if not args.bin:
        print("E2E_ERROR: missing --bin (or $JCODE_BIN)", file=sys.stderr)
        return 2
    if not os.path.isfile(args.bin):
        print(f"E2E_ERROR: --bin is not a file: {args.bin}", file=sys.stderr)
        return 2
    args.bin = os.path.abspath(args.bin)
    args.repo = os.path.abspath(args.repo)

    out_dir = args.out_dir or os.path.join(
        "/tmp", "session-view-e2e-" + time.strftime("%Y%m%d-%H%M%S")
    )
    out_dir = os.path.abspath(out_dir)
    os.makedirs(out_dir, exist_ok=True)

    cases = case_definitions()
    if args.only:
        wanted = [name.strip() for name in args.only.split(",") if name.strip()]
        unknown = [name for name in wanted if name not in cases]
        if unknown:
            print(f"E2E_ERROR: unknown case(s): {unknown}; known: {sorted(cases)}", file=sys.stderr)
            return 2
        cases = {name: cases[name] for name in wanted}

    fingerprint = tree_fingerprint(args.repo, args.bin)
    print(f"[e2e] out-dir: {out_dir}")
    print(f"[e2e] tree: {fingerprint['branch']} @ {fingerprint['rev'][:12]} "
          f"(status sha {fingerprint['statusPorcelainSha256'][:12]})")
    print(f"[e2e] bin:  {args.bin} (sha {fingerprint['binarySha256'][:12]})")
    print(f"[e2e] cases: {', '.join(cases)}")

    records: list[dict] = []
    for index, (name, spec) in enumerate(cases.items(), 1):
        print(f"[e2e] ({index}/{len(cases)}) {name} ...", flush=True)
        record = run_case(name, spec, args, out_dir)
        records.append(record)
        probe = record.get("probe") or {}
        print(
            f"[e2e]     {record['status']}: attached={probe.get('sessionToolsAttached') or '—'} "
            f"tools={probe.get('toolNames') or '—'} exit={record.get('searchViewExit')}",
            flush=True,
        )

    # Prove the tree did not move under us while the cases ran: the report's
    # attribution depends on every case having run against the same sources.
    after = tree_fingerprint(args.repo, args.bin)
    fingerprint["afterRun"] = {
        "statusPorcelainSha256": after["statusPorcelainSha256"],
        "diffSha256": after["diffSha256"],
        "worktreeSha256": after["worktreeSha256"],
    }
    fingerprint["stableDuringRun"] = (
        after["statusPorcelainSha256"] == fingerprint["statusPorcelainSha256"]
        and after["diffSha256"] == fingerprint["diffSha256"]
        and after["worktreeSha256"] == fingerprint["worktreeSha256"]
    )
    if not fingerprint["stableDuringRun"]:
        print("[e2e] WARNING: the worktree changed during the run; results are not attributable "
              "to a single tree (re-run required)", file=sys.stderr)

    not_found = write_summary(out_dir, records, fingerprint, args)
    print(f"[e2e] wrote {os.path.join(out_dir, 'summary.md')} and summary.json")

    if args.dry_run:
        return 0
    failed = [rec["case"] for rec in records if rec["status"] != "pass"]
    if failed:
        print(f"[e2e] FAILED cases: {failed}", file=sys.stderr)
        return 1
    if not_found.get("checked") and not not_found.get("identical"):
        print("[e2e] FAILED: unauthorized and missing-id outputs differ (scope §6)", file=sys.stderr)
        return 1
    print("[e2e] all cases passed")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
