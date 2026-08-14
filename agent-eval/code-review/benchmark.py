#!/usr/bin/env python3
"""Progressive code-review benchmark for jcode dynamic workflows.

The harness freezes a deterministic review manifest from git, runs a workflow,
then grades coverage and known-finding recall without trusting the model's own
claim that it reviewed everything.
"""

from __future__ import annotations

import argparse
import copy
import hashlib
import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path, PurePosixPath
from typing import Any


ROOT = Path(__file__).resolve().parent
DEFAULT_WORKFLOW = ROOT / "workflows" / "pr-review-v2.js"
DEFAULT_JUDGE_WORKFLOW = ROOT / "workflows" / "judge-review.js"
DEFAULT_VERIFY_WORKFLOW = ROOT / "workflows" / "verify-candidates.js"
TOKEN_RE = re.compile(r"\((\d+) tok\)")
HUNK_RE = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@")

PROFILE_CATEGORIES = {
    "strict": frozenset({"bug", "security", "concurrency", "data", "api"}),
    "core": frozenset(
        {"bug", "security", "concurrency", "data", "api", "perf", "test_gap", "doc_defect"}
    ),
    "all": frozenset(
        {
            "bug",
            "security",
            "concurrency",
            "data",
            "api",
            "perf",
            "test_gap",
            "doc_defect",
            "style",
            "speculative",
        }
    ),
}
SEVERITY_WEIGHTS = {"critical": 4, "high": 3, "medium": 2, "low": 1}

WAIVE_BASENAMES = {
    "package-lock.json",
    "pnpm-lock.yaml",
    "yarn.lock",
    "go.sum",
    "cargo.lock",
}
WAIVE_PARTS = {"vendor", "dist"}


def run_git(repo: Path, *args: str) -> str:
    proc = subprocess.run(
        ["git", "-C", str(repo), *args],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"git {' '.join(args)} failed: {proc.stderr.strip()}")
    return proc.stdout


def added_lines_from_patch(patch: str) -> list[int]:
    lines: list[int] = []
    for raw in patch.splitlines():
        match = HUNK_RE.match(raw)
        if not match:
            continue
        start = int(match.group(1))
        count = int(match.group(2) or "1")
        lines.extend(range(start, start + count))
    return lines


def review_area(path: str) -> str:
    parts = PurePosixPath(path).parts
    if len(parts) >= 2 and parts[0] == "packages":
        return f"packages/{parts[1]}"
    return parts[0] if parts else "root"


def waiver_reason(path: str, additions: str) -> str | None:
    posix = PurePosixPath(path)
    lower = path.lower()
    if additions == "-":
        return "binary"
    if posix.name.lower() in WAIVE_BASENAMES:
        return "lockfile"
    if any(part.lower() in WAIVE_PARTS for part in posix.parts):
        return "generated_or_vendored"
    if lower.endswith((".min.js", ".min.css", ".map")):
        return "generated_asset"
    return None


def build_manifest(
    repo: Path,
    base: str,
    head: str,
    *,
    max_unit_patch_chars: int = 24_000,
) -> dict[str, Any]:
    base_oid = run_git(repo, "rev-parse", base).strip()
    head_oid = run_git(repo, "rev-parse", head).strip()
    merge_base = run_git(repo, "merge-base", base_oid, head_oid).strip()
    numstat = run_git(
        repo,
        "diff",
        "--numstat",
        "--no-renames",
        f"{merge_base}...{head_oid}",
    )

    files: list[dict[str, Any]] = []
    file_patches: dict[str, str] = {}
    for row in numstat.splitlines():
        if not row.strip():
            continue
        additions_raw, deletions_raw, path = row.split("\t", 2)
        reason = waiver_reason(path, additions_raw)
        additions = 0 if additions_raw == "-" else int(additions_raw)
        deletions = 0 if deletions_raw == "-" else int(deletions_raw)
        patch = run_git(
            repo,
            "diff",
            "--unified=0",
            "--no-color",
            "--no-ext-diff",
            "--no-renames",
            f"{merge_base}...{head_oid}",
            "--",
            path,
        )
        added_lines = added_lines_from_patch(patch)
        record = {
            "path": path,
            "additions": additions,
            "deletions": deletions,
            "added_lines": added_lines,
            "area": review_area(path),
            "eligible": reason is None,
            "waiver_reason": reason,
        }
        files.append(record)
        file_patches[path] = patch

    units: list[dict[str, Any]] = []
    eligible = [file for file in files if file["eligible"]]
    by_area: dict[str, list[dict[str, Any]]] = {}
    for file in eligible:
        by_area.setdefault(file["area"], []).append(file)

    for area in sorted(by_area):
        chunks: list[list[dict[str, Any]]] = []
        current: list[dict[str, Any]] = []
        current_chars = 0
        for file in sorted(by_area[area], key=lambda item: item["path"]):
            patch_chars = len(file_patches[file["path"]])
            if current and current_chars + patch_chars > max_unit_patch_chars:
                chunks.append(current)
                current = []
                current_chars = 0
            current.append(file)
            current_chars += patch_chars
        if current:
            chunks.append(current)

        for index, chunk in enumerate(chunks, 1):
            paths = [file["path"] for file in chunk]
            patch = "\n".join(file_patches[path] for path in paths)
            slug = re.sub(r"[^a-z0-9]+", "-", area.lower()).strip("-") or "root"
            units.append(
                {
                    "id": f"{slug}-{index:02d}",
                    "area": area,
                    "files": paths,
                    "changed_lines": sum(file["additions"] for file in chunk),
                    "patch": patch,
                    "patch_sha256": hashlib.sha256(patch.encode()).hexdigest(),
                }
            )

    total_additions = sum(file["additions"] for file in files)
    eligible_additions = sum(file["additions"] for file in eligible)
    return {
        "schema_version": "jcode-review-manifest/v1",
        "repo": str(repo.resolve()),
        "base": base_oid,
        "head": head_oid,
        "merge_base": merge_base,
        "files": files,
        "units": units,
        "counts": {
            "changed_files": len(files),
            "eligible_files": len(eligible),
            "waived_files": len(files) - len(eligible),
            "changed_lines": total_additions,
            "eligible_changed_lines": eligible_additions,
        },
    }


def validate_result(manifest: dict[str, Any], result: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    expected_units = {unit["id"]: unit for unit in manifest["units"]}
    reported_units = result.get("units")
    if not isinstance(reported_units, list):
        return ["result.units must be an array"]

    seen: set[str] = set()
    for unit in reported_units:
        unit_id = unit.get("unit_id") if isinstance(unit, dict) else None
        if unit_id not in expected_units:
            errors.append(f"unexpected unit: {unit_id!r}")
            continue
        if unit_id in seen:
            errors.append(f"duplicate unit: {unit_id}")
        seen.add(unit_id)
        if unit.get("status") != "complete":
            errors.append(f"unit {unit_id} did not complete: {unit.get('status')!r}")
        else:
            want = set(expected_units[unit_id]["files"])
            got = set(unit.get("files_reviewed") or [])
            if got != want:
                errors.append(f"unit {unit_id} marked complete with files {sorted(got)}, want {sorted(want)}")
    for unit_id in expected_units.keys() - seen:
        errors.append(f"missing unit: {unit_id}")

    changed = {file["path"]: set(file["added_lines"]) for file in manifest["files"]}
    for finding in result.get("findings") or []:
        path = finding.get("path")
        line = finding.get("line")
        if path not in changed:
            errors.append(f"finding path is not changed: {path!r}")
        elif not isinstance(line, (int, float)) or int(line) not in changed[path]:
            errors.append(f"finding is not anchored to an added line: {path}:{line}")
    return errors


def validate_judgment(
    gold_findings: list[dict[str, Any]],
    findings: list[dict[str, Any]],
    judgment: dict[str, Any] | None,
) -> list[str]:
    if judgment is None:
        return ["semantic judgment is required for a complete gold set"]
    evaluations = judgment.get("evaluations")
    if not isinstance(evaluations, list):
        return ["judgment.evaluations must be an array"]

    expected = {
        (str(gold.get("id")), str(finding.get("id")))
        for gold in gold_findings
        for finding in findings
    }
    seen: set[tuple[str, str]] = set()
    errors: list[str] = []
    for evaluation in evaluations:
        if not isinstance(evaluation, dict):
            errors.append("judgment evaluation must be an object")
            continue
        pair = (str(evaluation.get("gold_id")), str(evaluation.get("candidate_id")))
        if pair not in expected:
            errors.append(f"unexpected judgment pair: {pair[0]}/{pair[1]}")
            continue
        if pair in seen:
            errors.append(f"duplicate judgment pair: {pair[0]}/{pair[1]}")
        seen.add(pair)
        if not isinstance(evaluation.get("match"), bool):
            errors.append(f"judgment pair {pair[0]}/{pair[1]} has non-boolean match")
    for gold_id, candidate_id in sorted(expected - seen):
        errors.append(f"missing judgment pair: {gold_id}/{candidate_id}")
    return errors


def fbeta(precision: float, recall: float, beta: float) -> float:
    beta_squared = beta * beta
    denominator = beta_squared * precision + recall
    return (1 + beta_squared) * precision * recall / denominator if denominator else 0.0


def delivery_status(result: dict[str, Any], validation_errors: list[str]) -> dict[str, Any]:
    verifier_status = result.get("verifier_status")
    complete = (
        not validation_errors
        and not (result.get("unverified_findings") or [])
        and verifier_status in {"complete", "complete_resumed", "not_needed"}
    )
    return {
        "complete": complete,
        "clean_conclusion_allowed": complete,
        "verifier_status": verifier_status,
        "unverified_findings": len(result.get("unverified_findings") or []),
    }


def semantic_quality(
    gold_findings: list[dict[str, Any]],
    findings: list[dict[str, Any]],
    judgment: dict[str, Any],
    profile: str,
) -> dict[str, Any]:
    categories = PROFILE_CATEGORIES[profile]
    evaluations = judgment.get("evaluations") or []
    matches = {
        (str(item.get("gold_id")), str(item.get("candidate_id"))): item
        for item in evaluations
        if item.get("match") is True
    }
    applicable = [gold for gold in gold_findings if gold.get("category") in categories]
    included_ids = {str(gold.get("id")) for gold in applicable}

    gold_hits: list[dict[str, Any]] = []
    true_positive_ids: set[str] = set()
    for gold in applicable:
        gold_id = str(gold.get("id"))
        matched = [
            (candidate_id, evaluation)
            for (evaluated_gold_id, candidate_id), evaluation in matches.items()
            if evaluated_gold_id == gold_id
        ]
        matched.sort(key=lambda item: float(item[1].get("confidence") or 0), reverse=True)
        if matched:
            true_positive_ids.add(gold_id)
        gold_hits.append(
            {
                "id": gold_id,
                "category": gold.get("category"),
                "severity": gold.get("severity"),
                "matched": bool(matched),
                "candidate_id": matched[0][0] if matched else None,
                "confidence": matched[0][1].get("confidence") if matched else None,
                "reason": matched[0][1].get("reason") if matched else None,
            }
        )

    candidate_matches: dict[str, list[str]] = {str(finding.get("id")): [] for finding in findings}
    for (gold_id, candidate_id), _evaluation in matches.items():
        if candidate_id in candidate_matches:
            candidate_matches[candidate_id].append(gold_id)
    false_positive_ids = sorted(candidate_id for candidate_id, ids in candidate_matches.items() if not ids)
    excluded_match_ids = sorted(
        candidate_id
        for candidate_id, ids in candidate_matches.items()
        if ids and not any(gold_id in included_ids for gold_id in ids)
    )

    tp = len(true_positive_ids)
    fp = len(false_positive_ids)
    fn = len(applicable) - tp
    precision = tp / (tp + fp) if tp + fp else (1.0 if not applicable else 0.0)
    recall = tp / len(applicable) if applicable else 1.0

    weighted_total = sum(SEVERITY_WEIGHTS.get(str(gold.get("severity", "")).lower(), 1) for gold in applicable)
    weighted_hit = sum(
        SEVERITY_WEIGHTS.get(str(gold.get("severity", "")).lower(), 1)
        for gold in applicable
        if str(gold.get("id")) in true_positive_ids
    )
    return {
        "profile": profile,
        "profile_categories": sorted(categories),
        "findings": len(findings),
        "gold_findings": len(applicable),
        "excluded_gold_findings": len(gold_findings) - len(applicable),
        "true_positives": tp,
        "false_positives": fp,
        "false_negatives": fn,
        "excluded_match_findings": excluded_match_ids,
        "false_positive_finding_ids": false_positive_ids,
        "gold_hits": gold_hits,
        "gold_precision": precision,
        "gold_recall": recall,
        "gold_f1": fbeta(precision, recall, 1.0),
        "gold_f2": fbeta(precision, recall, 2.0),
        "severity_weighted_recall": weighted_hit / weighted_total if weighted_total else 1.0,
        "signal_to_noise": tp / fp if fp else None,
        "signal_rate": tp / (tp + fp) if tp + fp else None,
    }


def score_result(
    manifest: dict[str, Any],
    result: dict[str, Any],
    case: dict[str, Any],
    stderr: str = "",
    *,
    judgment: dict[str, Any] | None = None,
    judge_stderr: str = "",
    profile: str = "core",
) -> dict[str, Any]:
    unit_by_id = {unit["id"]: unit for unit in manifest["units"]}
    completed_ids = {
        unit["unit_id"]
        for unit in result.get("units") or []
        if isinstance(unit, dict) and unit.get("status") == "complete" and unit.get("unit_id") in unit_by_id
    }
    covered_lines = sum(unit_by_id[unit_id]["changed_lines"] for unit_id in completed_ids)
    eligible_lines = manifest["counts"]["eligible_changed_lines"]
    reviewer_tokens = sum(int(value) for value in TOKEN_RE.findall(stderr))
    if reviewer_tokens == 0:
        reviewer_tokens = int((result.get("metrics") or {}).get("total_tokens") or 0)
    judge_tokens = sum(int(value) for value in TOKEN_RE.findall(judge_stderr))
    if judge_tokens == 0 and judgment is not None:
        judge_tokens = int((judgment.get("metrics") or {}).get("total_tokens") or 0)

    findings = result.get("findings") or []
    candidates = result.get("candidates") or findings
    gold_findings = case.get("gold_findings") or []
    gold_set_complete = bool(case.get("gold_set_complete", False))
    validation_errors = validate_result(manifest, result)
    delivery = delivery_status(result, validation_errors)
    quality: dict[str, Any] = {
        "profile": profile,
        "findings": len(findings),
        "candidates": len(candidates),
        "gold_findings": len(gold_findings),
        "gold_set_complete": gold_set_complete,
        "scored": False,
    }
    if validation_errors:
        quality["unscored_reason"] = "review_protocol_invalid"
    elif not delivery["complete"]:
        quality["unscored_reason"] = "review_delivery_incomplete"
    elif gold_set_complete:
        judgment_errors = validate_judgment(gold_findings, findings, judgment)
        validation_errors.extend(judgment_errors)
        if not judgment_errors and judgment is not None:
            quality = {
                **semantic_quality(gold_findings, findings, judgment, profile),
                "candidates": len(candidates),
                "gold_set_complete": True,
                "scored": True,
            }

    return {
        "schema_version": "jcode-review-score/v1",
        "validation_errors": validation_errors,
        "delivery": delivery,
        "coverage": {
            "completed_units": len(completed_ids),
            "planned_units": len(unit_by_id),
            "covered_changed_lines": covered_lines,
            "eligible_changed_lines": eligible_lines,
            "line_coverage": covered_lines / eligible_lines if eligible_lines else 1.0,
        },
        "quality": quality,
        "token_efficiency": {
            "reviewer_tokens": reviewer_tokens,
            "judge_tokens": judge_tokens,
            "total_evaluation_tokens": reviewer_tokens + judge_tokens,
            "reviewer_tokens_per_covered_changed_line": reviewer_tokens / covered_lines if covered_lines else None,
            "reviewer_tokens_per_1k_covered_changed_lines": (reviewer_tokens * 1000 / covered_lines) if covered_lines else None,
            "reviewer_tokens_per_completed_unit": reviewer_tokens / len(completed_ids) if completed_ids else None,
            "reviewer_tokens_per_gold_hit": reviewer_tokens / quality.get("true_positives") if quality.get("true_positives") else None,
        },
    }


def load_json(path: Path) -> dict[str, Any]:
    with path.open() as handle:
        value = json.load(handle)
    if not isinstance(value, dict):
        raise ValueError(f"{path} must contain a JSON object")
    return value


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n")


def apply_case_context(manifest: dict[str, Any], case: dict[str, Any]) -> None:
    manifest["context"] = {
        "title": str(case.get("title") or ""),
        "description": str(case.get("description") or ""),
        "source_url": str(case.get("pull_request") or case.get("url") or ""),
    }


def run_workflow(
    *,
    jcode: Path,
    workflow: Path,
    repo: Path,
    args: dict[str, Any],
    model: str,
    budget: int,
    timeout: str,
    concurrency: int,
) -> tuple[subprocess.CompletedProcess[str], float]:
    command = [
        str(jcode),
        "flow",
        "run",
        str(workflow.resolve()),
        "--args",
        json.dumps(args, separators=(",", ":")),
        "--budget",
        str(budget),
        "--timeout",
        timeout,
        "--concurrency",
        str(concurrency),
    ]
    env = os.environ.copy()
    if model:
        env["JCODE_MODEL"] = model
    started = time.monotonic()
    proc = subprocess.run(
        command,
        cwd=repo,
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        env=env,
    )
    return proc, round(time.monotonic() - started, 3)


def apply_verification(result: dict[str, Any], verification: dict[str, Any]) -> dict[str, Any]:
    merged = copy.deepcopy(result)
    candidates = merged.get("candidates") or []
    expected = {str(candidate.get("id")): candidate for candidate in candidates}
    decisions = verification.get("decisions")
    if not isinstance(decisions, list):
        raise ValueError("verification.decisions must be an array")
    by_id: dict[str, dict[str, Any]] = {}
    for decision in decisions:
        decision_id = str(decision.get("id")) if isinstance(decision, dict) else ""
        if decision_id not in expected:
            raise ValueError(f"unexpected verifier decision: {decision_id}")
        if decision_id in by_id:
            raise ValueError(f"duplicate verifier decision: {decision_id}")
        by_id[decision_id] = decision
    missing = sorted(expected.keys() - by_id.keys())
    if missing:
        raise ValueError(f"missing verifier decisions: {', '.join(missing)}")

    findings: list[dict[str, Any]] = []
    rejected = [
        item
        for item in (merged.get("rejected_findings") or [])
        if item.get("id") not in expected
    ]
    for candidate_id, candidate in expected.items():
        decision = by_id[candidate_id]
        if decision.get("verdict") == "confirmed":
            finding = copy.deepcopy(candidate)
            finding["verification_confidence"] = float(decision.get("confidence") or 0)
            finding["verification_reason"] = str(decision.get("reason") or "")
            findings.append(finding)
        else:
            rejected.append(
                {"id": candidate_id, "verdict": "rejected", "reason": str(decision.get("reason") or "")}
            )

    merged["findings"] = findings
    merged["rejected_findings"] = rejected
    merged["unverified_findings"] = []
    merged["verifier_status"] = "complete_resumed"
    metrics = dict(merged.get("metrics") or {})
    metrics["agent_calls"] = int(metrics.get("agent_calls") or 0) + 1
    metrics["total_tokens"] = int(metrics.get("total_tokens") or 0) + int(
        (verification.get("metrics") or {}).get("total_tokens") or 0
    )
    merged["metrics"] = metrics
    return merged


def command_prepare(ns: argparse.Namespace) -> int:
    case = load_json(ns.case)
    manifest = build_manifest(ns.repo, case["base"], case["head"], max_unit_patch_chars=ns.max_unit_patch_chars)
    apply_case_context(manifest, case)
    write_json(ns.output, manifest)
    print(ns.output)
    return 0


def command_score(ns: argparse.Namespace) -> int:
    score = score_result(
        load_json(ns.manifest),
        load_json(ns.result),
        load_json(ns.case),
        ns.stderr.read_text() if ns.stderr else "",
        judgment=load_json(ns.judgment) if ns.judgment else None,
        judge_stderr=ns.judge_stderr.read_text() if ns.judge_stderr else "",
        profile=ns.profile,
    )
    write_json(ns.output, score)
    print(ns.output)
    return 1 if score["validation_errors"] or not score["delivery"]["complete"] else 0


def command_run(ns: argparse.Namespace) -> int:
    case = load_json(ns.case)
    manifest = build_manifest(ns.repo, case["base"], case["head"], max_unit_patch_chars=ns.max_unit_patch_chars)
    apply_case_context(manifest, case)
    workflow = ns.workflow.resolve()
    case_slug = re.sub(r"[^a-z0-9]+", "-", str(case.get("id") or "case").lower()).strip("-")
    run_dir = ns.output / case_slug / time.strftime("%Y%m%d-%H%M%S")
    run_dir.mkdir(parents=True, exist_ok=False)
    write_json(run_dir / "manifest.json", manifest)

    proc, elapsed_seconds = run_workflow(
        jcode=ns.jcode,
        workflow=workflow,
        repo=ns.repo,
        args={"review": manifest, "model": ns.model or ""},
        model=ns.model,
        budget=ns.budget,
        timeout=ns.timeout,
        concurrency=ns.concurrency,
    )
    (run_dir / "stdout.log").write_text(proc.stdout)
    (run_dir / "stderr.log").write_text(proc.stderr)
    write_json(
        run_dir / "run.json",
        {
            "case": case.get("id"),
            "model": ns.model or "session-default",
            "budget_target": ns.budget,
            "elapsed_seconds": elapsed_seconds,
            "exit_code": proc.returncode,
            "workflow": str(workflow),
            "jcode": str(ns.jcode),
        },
    )
    if proc.returncode != 0:
        print(run_dir)
        return proc.returncode
    try:
        result = json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        (run_dir / "parse-error.txt").write_text(str(exc) + "\n")
        print(run_dir)
        return 2
    write_json(run_dir / "result.json", result)

    judgment: dict[str, Any] | None = None
    judge_stderr = ""
    judge_elapsed_seconds: float | None = None
    gold_findings = case.get("gold_findings") or []
    review_validation_errors = validate_result(manifest, result)
    review_delivery = delivery_status(result, review_validation_errors)
    if review_delivery["complete"] and case.get("gold_set_complete") and ns.judge_model:
        judge_workflow = ns.judge_workflow.resolve()
        judge_proc, judge_elapsed_seconds = run_workflow(
            jcode=ns.jcode,
            workflow=judge_workflow,
            repo=ns.repo,
            args={"golden": gold_findings, "candidates": result.get("findings") or [], "model": ns.judge_model},
            model=ns.judge_model,
            budget=ns.judge_budget,
            timeout=ns.timeout,
            concurrency=1,
        )
        (run_dir / "judge.stdout.log").write_text(judge_proc.stdout)
        (run_dir / "judge.stderr.log").write_text(judge_proc.stderr)
        write_json(
            run_dir / "judge-run.json",
            {
                "model": ns.judge_model,
                "budget_target": ns.judge_budget,
                "elapsed_seconds": judge_elapsed_seconds,
                "exit_code": judge_proc.returncode,
                "workflow": str(judge_workflow),
            },
        )
        if judge_proc.returncode != 0:
            print(run_dir)
            return judge_proc.returncode
        judge_stderr = judge_proc.stderr
        try:
            judgment = json.loads(judge_proc.stdout)
        except json.JSONDecodeError as exc:
            (run_dir / "judge-parse-error.txt").write_text(str(exc) + "\n")
            print(run_dir)
            return 2
        write_json(run_dir / "judgment.json", judgment)

    score = score_result(
        manifest,
        result,
        case,
        proc.stderr,
        judgment=judgment,
        judge_stderr=judge_stderr,
        profile=ns.profile,
    )
    score["elapsed_seconds"] = {"reviewer": elapsed_seconds, "judge": judge_elapsed_seconds}
    write_json(run_dir / "score.json", score)
    print(run_dir)
    return 1 if score["validation_errors"] or not score["delivery"]["complete"] else 0


def command_resume_verify(ns: argparse.Namespace) -> int:
    case = load_json(ns.case)
    manifest = load_json(ns.run_dir / "manifest.json")
    original = load_json(ns.run_dir / "result.json")
    candidates = original.get("candidates") or []
    repo = Path(manifest["repo"])

    verify_proc, verify_elapsed = run_workflow(
        jcode=ns.jcode,
        workflow=ns.workflow,
        repo=repo,
        args={"review": manifest, "candidates": candidates, "model": ns.model},
        model=ns.model,
        budget=ns.budget,
        timeout=ns.timeout,
        concurrency=1,
    )
    (ns.run_dir / "resume-verify.stdout.log").write_text(verify_proc.stdout)
    (ns.run_dir / "resume-verify.stderr.log").write_text(verify_proc.stderr)
    if verify_proc.returncode != 0:
        print(ns.run_dir)
        return verify_proc.returncode
    verification = json.loads(verify_proc.stdout)
    write_json(ns.run_dir / "verification.json", verification)
    resumed = apply_verification(original, verification)
    write_json(ns.run_dir / "resumed-result.json", resumed)

    judgment: dict[str, Any] | None = None
    judge_stderr = ""
    judge_elapsed: float | None = None
    if case.get("gold_set_complete") and ns.judge_model:
        judge_proc, judge_elapsed = run_workflow(
            jcode=ns.jcode,
            workflow=ns.judge_workflow,
            repo=repo,
            args={
                "golden": case.get("gold_findings") or [],
                "candidates": resumed.get("findings") or [],
                "model": ns.judge_model,
            },
            model=ns.judge_model,
            budget=ns.judge_budget,
            timeout=ns.timeout,
            concurrency=1,
        )
        (ns.run_dir / "resume-judge.stdout.log").write_text(judge_proc.stdout)
        (ns.run_dir / "resume-judge.stderr.log").write_text(judge_proc.stderr)
        if judge_proc.returncode != 0:
            print(ns.run_dir)
            return judge_proc.returncode
        judge_stderr = judge_proc.stderr
        judgment = json.loads(judge_proc.stdout)
        write_json(ns.run_dir / "resumed-judgment.json", judgment)

    original_stderr = (ns.run_dir / "stderr.log").read_text()
    score = score_result(
        manifest,
        resumed,
        case,
        original_stderr + verify_proc.stderr,
        judgment=judgment,
        judge_stderr=judge_stderr,
        profile=ns.profile,
    )
    score["elapsed_seconds"] = {"resume_verifier": verify_elapsed, "judge": judge_elapsed}
    write_json(ns.run_dir / "resumed-score.json", score)
    write_json(
        ns.run_dir / "resume-run.json",
        {
            "model": ns.model,
            "judge_model": ns.judge_model,
            "verifier_elapsed_seconds": verify_elapsed,
            "judge_elapsed_seconds": judge_elapsed,
            "workflow": str(ns.workflow.resolve()),
        },
    )
    print(ns.run_dir)
    return 1 if score["validation_errors"] or not score["delivery"]["complete"] else 0


def parser() -> argparse.ArgumentParser:
    main = argparse.ArgumentParser(description=__doc__)
    sub = main.add_subparsers(dest="command", required=True)

    prepare = sub.add_parser("prepare", help="freeze a deterministic git review manifest")
    prepare.add_argument("--repo", type=Path, required=True)
    prepare.add_argument("--case", type=Path, required=True)
    prepare.add_argument("--output", type=Path, required=True)
    prepare.add_argument("--max-unit-patch-chars", type=int, default=24_000)
    prepare.set_defaults(func=command_prepare)

    score = sub.add_parser("score", help="grade a completed workflow result")
    score.add_argument("--case", type=Path, required=True)
    score.add_argument("--manifest", type=Path, required=True)
    score.add_argument("--result", type=Path, required=True)
    score.add_argument("--stderr", type=Path)
    score.add_argument("--judgment", type=Path)
    score.add_argument("--judge-stderr", type=Path)
    score.add_argument("--profile", choices=sorted(PROFILE_CATEGORIES), default="core")
    score.add_argument("--output", type=Path, required=True)
    score.set_defaults(func=command_score)

    run = sub.add_parser("run", help="prepare, run jcode flow, and score one case")
    run.add_argument("--repo", type=Path, required=True)
    run.add_argument("--case", type=Path, required=True)
    run.add_argument("--jcode", type=Path, default=Path("jcode"))
    run.add_argument("--workflow", type=Path, default=DEFAULT_WORKFLOW)
    run.add_argument("--output", type=Path, required=True)
    run.add_argument("--model", default="")
    run.add_argument("--judge-model", default="")
    run.add_argument("--judge-workflow", type=Path, default=DEFAULT_JUDGE_WORKFLOW)
    run.add_argument("--profile", choices=sorted(PROFILE_CATEGORIES), default="core")
    run.add_argument("--budget", type=int, default=60_000)
    run.add_argument("--judge-budget", type=int, default=30_000)
    run.add_argument("--timeout", default="30m")
    run.add_argument("--concurrency", type=int, default=3)
    run.add_argument("--max-unit-patch-chars", type=int, default=24_000)
    run.set_defaults(func=command_run)

    resume = sub.add_parser("resume-verify", help="resume a run whose discovery completed before verification")
    resume.add_argument("--run-dir", type=Path, required=True)
    resume.add_argument("--case", type=Path, required=True)
    resume.add_argument("--jcode", type=Path, default=Path("jcode"))
    resume.add_argument("--workflow", type=Path, default=DEFAULT_VERIFY_WORKFLOW)
    resume.add_argument("--model", required=True)
    resume.add_argument("--judge-model", default="")
    resume.add_argument("--judge-workflow", type=Path, default=DEFAULT_JUDGE_WORKFLOW)
    resume.add_argument("--profile", choices=sorted(PROFILE_CATEGORIES), default="core")
    resume.add_argument("--budget", type=int, default=60_000)
    resume.add_argument("--judge-budget", type=int, default=30_000)
    resume.add_argument("--timeout", default="15m")
    resume.set_defaults(func=command_resume_verify)
    return main


def main() -> int:
    ns = parser().parse_args()
    return ns.func(ns)


if __name__ == "__main__":
    sys.exit(main())
