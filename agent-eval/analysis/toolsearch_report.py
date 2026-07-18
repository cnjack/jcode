#!/usr/bin/env python3
"""Generate the publication-safe ToolSearch acceptance report.

Only allowlisted metadata is rendered.  The report never opens private session
JSONL, config files, debug logs, prompts, raw arguments, or raw tool output.
Every planned run must instead provide the three sanitized artifacts produced
by the ToolSearch campaign: ``record.json``, ``trajectory.json``, and
``redaction_report.json``.

The acceptance calculation is deliberately fail-closed.  Missing fields,
non-exact Kimi model selection, unsafe redaction reports, incomplete pairs, or
campaign-duration evidence that cannot prove 30 minutes all prevent PASS.
"""

import argparse
import hashlib
import html
import json
import math
import os
import re
import sys
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import quote


HERE = Path(__file__).resolve().parent
SUITE_DIR = HERE.parent / "suite"
if str(SUITE_DIR) not in sys.path:
    sys.path.insert(0, str(SUITE_DIR))

import toolsearch_cases  # noqa: E402


EXACT_MODEL_LABEL = "kimi-for-coding"
EXACT_MODEL_ID = "kimi-for-coding/kimi-for-coding"
MIN_CAMPAIGN_SECONDS = 1800.0
MIN_FORMAL_REPEATS = 10
EXPECTED_VARIANTS = {"static", "deferred"}

RUN_ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,220}$")
NAME_RE = re.compile(r"^[A-Za-z0-9_.:-]{1,180}$")
COMMIT_RE = re.compile(r"^[0-9a-f]{7,64}$")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
CONTROL_RE = re.compile(r"[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]")
ABSOLUTE_PATH_RE = re.compile(
    r"(?:^|[\s\"'=])(?:/Users/|/home/|/private/var/|[A-Za-z]:\\\\Users\\\\)"
)
CREDENTIAL_PATTERNS = (
    re.compile(r"sk-[A-Za-z0-9_-]{12,}"),
    re.compile(r"(?i)bearer\s+[A-Za-z0-9._~+/-]{12,}"),
    re.compile(
        r"(?i)(?:api[_-]?key|access[_-]?token|authorization|client[_-]?secret)"
        r"[\"']?\s*[:=]\s*[\"']?[A-Za-z0-9._~+/-]{8,}"
    ),
)
RAW_PAYLOAD_KEYS = {
    "prompt",
    "args",
    "arguments",
    "output",
    "content",
    "final_text",
    "raw_session",
    "raw_config",
    "debug_log",
    "api_key",
    "authorization",
    "access_token",
    "client_secret",
    "password",
}

REQUIRED_CAMPAIGN_HASHES = (
    "jcode_sha256",
    "harness_sha256",
    "mcp_fixture_sha256",
)
REQUIRED_ENVIRONMENT_FIELDS = ("go_version", "os_arch", "eino_version")
SUITE_HASH_KEYS = ("matrix_sha256", "base_suite_sha256")
EXPECTED_SUPPLEMENTARY_COMMANDS = {
    "transport_mode_catalogs",
    "web_mcp_reload_mode_switch",
    "deferred_revoke_failure_recovery",
}
EXPECTED_SUPPLEMENTARY_WEB = {
    "web_approval_deny_en": {
        "variant": "deferred", "language": "en", "scenario": "approval_deny",
        "routing_applicable": False,
    },
    "web_browser_disabled_en": {
        "variant": "deferred", "language": "en", "scenario": "browser_disabled",
        "routing_applicable": False,
    },
    "web_success_zh_static": {
        "variant": "static", "language": "zh", "scenario": "success",
        "routing_applicable": True,
    },
    "web_success_zh_deferred": {
        "variant": "deferred", "language": "zh", "scenario": "success",
        "routing_applicable": True,
    },
}


class ReportError(ValueError):
    """Raised when report evidence is missing, unsafe, or ambiguous."""


def _file_sha256(path):
    digest = hashlib.sha256()
    try:
        with Path(path).open("rb") as stream:
            while chunk := stream.read(1 << 20):
                digest.update(chunk)
    except OSError as exc:
        raise ReportError("formal suite input is unavailable") from exc
    return digest.hexdigest()


def _suite_input_hashes(matrix_path, base_suite_path):
    return {
        "matrix_sha256": _file_sha256(matrix_path),
        "base_suite_sha256": _file_sha256(base_suite_path),
    }


def _locked_suite_input_hashes(matrix_path, base_suite_path):
    selected = _suite_input_hashes(matrix_path, base_suite_path)
    canonical = _suite_input_hashes(
        toolsearch_cases.DEFAULT_MATRIX,
        toolsearch_cases.DEFAULT_BASE_SUITE,
    )
    if selected != canonical:
        raise ReportError("formal suite inputs drifted from the pinned defaults")
    return selected


def _read_json(path, label):
    try:
        value = json.loads(Path(path).read_text())
    except OSError as exc:
        raise ReportError(f"missing {label}") from exc
    except json.JSONDecodeError as exc:
        raise ReportError(f"invalid JSON in {label}") from exc
    return value


def _require_dict(value, label):
    if not isinstance(value, dict):
        raise ReportError(f"{label} must be an object")
    return value


def _require_list(value, label):
    if not isinstance(value, list):
        raise ReportError(f"{label} must be an array")
    return value


def _require_bool(value, label):
    if not isinstance(value, bool):
        raise ReportError(f"{label} must be boolean")
    return value


def _require_int(value, label, minimum=None):
    if not isinstance(value, int) or isinstance(value, bool):
        raise ReportError(f"{label} must be an integer")
    if minimum is not None and value < minimum:
        raise ReportError(f"{label} is below its minimum")
    return value


def _require_number(value, label, minimum=None):
    if (not isinstance(value, (int, float)) or isinstance(value, bool)
            or not math.isfinite(float(value))):
        raise ReportError(f"{label} must be a finite number")
    result = float(value)
    if minimum is not None and result < minimum:
        raise ReportError(f"{label} is below its minimum")
    return result


def _require_string(value, label, pattern=None, maximum=240):
    if not isinstance(value, str) or not value or len(value) > maximum:
        raise ReportError(f"{label} must be a bounded non-empty string")
    if CONTROL_RE.search(value):
        raise ReportError(f"{label} contains control characters")
    if pattern is not None and not pattern.fullmatch(value):
        raise ReportError(f"{label} has an invalid format")
    return value


def _require_optional_string(value, label, maximum=240):
    if not isinstance(value, str) or len(value) > maximum:
        raise ReportError(f"{label} must be a bounded string")
    if CONTROL_RE.search(value):
        raise ReportError(f"{label} contains control characters")
    return value


def _parse_utc(value, label):
    text = _require_string(value, label, maximum=80)
    try:
        parsed = datetime.fromisoformat(text.replace("Z", "+00:00"))
    except ValueError as exc:
        raise ReportError(f"{label} must be ISO-8601") from exc
    if parsed.tzinfo is None or parsed.utcoffset() is None:
        raise ReportError(f"{label} must include a UTC offset")
    parsed = parsed.astimezone(timezone.utc)
    return parsed


def _walk_artifact_safety(value, label):
    """Reject payload-shaped keys, credentials, and host paths.

    The validated testcase matrix is intentionally not passed here: it owns the
    prompts used by the runner.  Runtime publication artifacts are metadata-only
    and therefore have no reason to contain any raw payload key.
    """
    if isinstance(value, dict):
        for key, child in value.items():
            folded = str(key).lower()
            if folded in RAW_PAYLOAD_KEYS:
                raise ReportError(f"{label} contains forbidden raw payload field")
            _walk_artifact_safety(child, label)
    elif isinstance(value, list):
        for child in value:
            _walk_artifact_safety(child, label)
    elif isinstance(value, str):
        if ABSOLUTE_PATH_RE.search(value):
            raise ReportError(f"{label} contains a host path")
        if any(pattern.search(value) for pattern in CREDENTIAL_PATTERNS):
            raise ReportError(f"{label} contains credential-shaped data")


def _load_matrix(matrix_path, base_suite_path):
    try:
        suite = toolsearch_cases.load_suite(matrix_path, base_suite_path)
    except (toolsearch_cases.MatrixError, OSError, json.JSONDecodeError) as exc:
        raise ReportError("ToolSearch matrix validation failed") from exc
    gates = _require_dict(suite.get("hard_gates"), "matrix hard_gates")
    if set(gates) != set(toolsearch_cases.REQUIRED_HARD_GATES):
        raise ReportError("matrix does not contain the fixed nine hard gates")
    cases = _require_list(suite.get("cases"), "matrix cases")
    case_map = {case["id"]: case for case in cases}
    if len(case_map) != len(cases):
        raise ReportError("matrix case IDs are not unique")
    return suite, case_map


def _validate_plan(plan, case_map, suite_hashes):
    plan = _require_dict(plan, "plan.json")
    if plan.get("schema_version") != 1:
        raise ReportError("plan schema_version must be 1")
    if plan.get("formal") is not True:
        raise ReportError("plan must be a formal run")
    if plan.get("mode") != "formal" or plan.get("dry_run") is not False:
        raise ReportError("plan mode must be formal and non-dry-run")
    if plan.get("suite") != "toolsearch":
        raise ReportError("plan suite must be toolsearch")
    if plan.get("supplementary_planned") is not True:
        raise ReportError("formal plan must include supplementary coverage")
    parameters = _require_dict(plan.get("request_parameters"), "plan request parameters")
    if parameters != {"temperature": "omitted"}:
        raise ReportError("formal plan must prove temperature was omitted")
    if _require_dict(plan.get("suite_inputs"), "plan suite_inputs") != suite_hashes:
        raise ReportError("plan suite input hashes drifted")
    if _require_int(plan.get("workers"), "plan workers", 1) != 1:
        raise ReportError("formal plan must use workers=1")
    if set(_require_list(plan.get("variants"), "plan variants")) != EXPECTED_VARIANTS:
        raise ReportError("plan must include exactly static and deferred")
    repeats = _require_int(plan.get("repeats"), "plan repeats", MIN_FORMAL_REPEATS)
    seed = _require_int(plan.get("seed"), "plan seed")

    models = _require_list(plan.get("models"), "plan models")
    if models != [{"label": EXACT_MODEL_LABEL, "id": EXACT_MODEL_ID}]:
        raise ReportError("plan must use exact kimi-for-coding/kimi-for-coding")

    jobs = _require_list(plan.get("jobs"), "plan jobs")
    expected_keys = {
        (case_id, variant, repeat)
        for case_id, case in case_map.items()
        for variant in case["variants"]
        for repeat in range(1, repeats + 1)
    }
    actual_keys = set()
    run_ids = set()
    normalized = []
    for index, raw in enumerate(jobs):
        job = _require_dict(raw, f"plan job {index + 1}")
        run_id = _require_string(job.get("run_id"), "plan run_id", RUN_ID_RE)
        case_id = _require_string(job.get("case_id"), "plan case_id", RUN_ID_RE)
        variant = job.get("variant")
        repeat = _require_int(job.get("repeat"), "plan repeat", 1)
        if case_id not in case_map or variant not in EXPECTED_VARIANTS:
            raise ReportError("plan job references an unknown case or variant")
        if job.get("model") != EXACT_MODEL_LABEL or job.get("model_id") != EXACT_MODEL_ID:
            raise ReportError("plan job model drifted from exact Kimi")
        key = (case_id, variant, repeat)
        if key in actual_keys or run_id in run_ids:
            raise ReportError("plan contains duplicate jobs or run IDs")
        actual_keys.add(key)
        run_ids.add(run_id)
        normalized.append({
            "run_id": run_id,
            "case_id": case_id,
            "variant": variant,
            "repeat": repeat,
        })
    if actual_keys != expected_keys:
        raise ReportError("plan does not exactly cover every matrix case and pair")
    offset = 0
    while offset < len(normalized):
        item = normalized[offset]
        case_variants = set(case_map[item["case_id"]]["variants"])
        if case_variants == EXPECTED_VARIANTS:
            pair = normalized[offset:offset + 2]
            signatures = {(entry["case_id"], entry["repeat"]) for entry in pair}
            variants = {entry["variant"] for entry in pair}
            if len(pair) != 2 or len(signatures) != 1 or variants != EXPECTED_VARIANTS:
                raise ReportError("static/deferred jobs must remain adjacent paired blocks")
            offset += 2
        else:
            if case_variants != {item["variant"]}:
                raise ReportError("unpaired plan block differs from matrix variants")
            offset += 1
    return normalized, repeats, seed


def _validate_redaction(value, run_id):
    report = _require_dict(value, f"redaction report for {run_id}")
    _walk_artifact_safety(report, "redaction report")
    if report.get("schema_version") != 1:
        raise ReportError("redaction report schema_version must be 1")
    if report.get("safe") is not True:
        raise ReportError("redaction report is not safe")
    findings = _require_list(
        report.get("post_redaction_findings"), "post-redaction findings",
    )
    if findings:
        raise ReportError("redaction report contains post-redaction findings")
    _require_int(report.get("files_scanned"), "redaction files_scanned", 1)
    _require_int(report.get("files_redacted"), "redaction files_redacted", 0)
    _require_list(report.get("redacted_file_names"), "redacted file names")
    _require_dict(report.get("replacement_counts"), "redaction replacement counts")
    return report


def _validate_tool_counts(value, run_id):
    counts = _require_dict(value, f"tool_counts for {run_id}")
    for name in ("calls_total", "results_total", "model_requests"):
        _require_int(counts.get(name), f"tool_counts.{name}", 0)
    calls = _require_dict(counts.get("calls_by_name"), "tool calls_by_name")
    for name, count in calls.items():
        _require_string(name, "tool name", NAME_RE)
        _require_int(count, "tool call count", 0)
    for name in ("first_visible", "first_schema_tokens_estimate"):
        _require_int(counts.get(name), f"tool_counts.{name}", 0)
    return counts


def _core_tool_counts(counts):
    """Fields shared by record.json and trajectory.json.

    Records additionally carry declared catalog sizes; those are safe but are
    not generated by the trajectory extractor itself.
    """
    return {
        key: counts.get(key)
        for key in (
            "calls_total",
            "results_total",
            "calls_by_name",
            "results_by_status",
            "model_requests",
            "first_visible",
            "max_visible",
            "first_schema_tokens_estimate",
            "max_schema_tokens_estimate",
        )
    }


def _validate_trajectory(value, job):
    trajectory = _require_dict(value, f"trajectory for {job['run_id']}")
    _walk_artifact_safety(trajectory, "trajectory")
    if trajectory.get("schema_version") != 1:
        raise ReportError("trajectory schema_version must be 1")
    if trajectory.get("payload_policy") != "metadata_only_except_declared_fixture_args":
        raise ReportError("trajectory payload policy is not publication-safe")
    if trajectory.get("run_id") != job["run_id"] or trajectory.get("variant") != job["variant"]:
        raise ReportError("trajectory identity does not match its plan job")
    if _require_int(trajectory.get("parse_error_count"), "trajectory parse errors", 0) != 0:
        raise ReportError("trajectory contains parse errors")
    counts = _validate_tool_counts(trajectory.get("tool_counts"), job["run_id"])
    sessions = _require_list(trajectory.get("sessions"), "trajectory sessions")
    if not sessions:
        raise ReportError("trajectory has no session evidence")
    for session in sessions:
        session = _require_dict(session, "trajectory session")
        if session.get("source_present") is not True:
            raise ReportError("trajectory source session was missing")
        if _require_list(session.get("parse_error_lines"), "parse error lines"):
            raise ReportError("trajectory session contains parse errors")
        entries = _require_list(session.get("entries"), "trajectory entries")
        for entry in entries:
            _require_dict(entry, "trajectory entry")
    return trajectory, counts


def _validate_routing(record, variant, run_id):
    routing = _require_dict(record.get("routing"), f"routing for {run_id}")
    _walk_artifact_safety(routing, "routing verdict")
    counts = _require_dict(routing.get("counts"), "routing counts")
    if variant == "deferred":
        _require_bool(routing.get("passed"), "deferred routing passed")
        required = (
            "bypass",
            "same_batch_activation",
            "deferred_calls",
            "deferred_call_success",
            "search_calls",
        )
        for name in required:
            _require_int(counts.get(name), f"routing counts.{name}", 0)
        if counts["deferred_call_success"] > counts["deferred_calls"]:
            raise ReportError("deferred success count exceeds deferred calls")
    violations = _require_list(routing.get("violations"), "routing violations")
    for violation in violations:
        violation = _require_dict(violation, "routing violation")
        _require_string(violation.get("type"), "routing violation type", RUN_ID_RE)
        if "tool" in violation:
            _require_string(violation["tool"], "routing violation tool", NAME_RE)
    return routing, counts


def _validate_record(value, job, case, seed, trajectory_counts):
    record = _require_dict(value, f"record for {job['run_id']}")
    _walk_artifact_safety(record, "record")
    identity = (
        record.get("run_id") == job["run_id"]
        and record.get("case_id") == job["case_id"]
        and record.get("variant") == job["variant"]
        and record.get("repeat") == job["repeat"]
        and record.get("seed") == seed
    )
    if not identity:
        raise ReportError("record identity does not match its plan job")
    if record.get("model") != EXACT_MODEL_LABEL or record.get("model_id") != EXACT_MODEL_ID:
        raise ReportError("record model drifted from exact Kimi")
    _require_optional_string(record.get("effort"), "record effort", maximum=40)
    parameters = _require_dict(record.get("request_parameters"), "request parameters")
    if parameters.get("temperature") != "omitted":
        raise ReportError("record must prove temperature was omitted")
    if record.get("artifact_safe") is not True:
        raise ReportError("record is not marked artifact-safe")
    _require_bool(record.get("task_passed"), "record task_passed")
    _require_bool(record.get("contracts_passed"), "record contracts_passed")
    _require_bool(record.get("error_present"), "record error_present")
    _require_string(record.get("stop_reason"), "record stop_reason", RUN_ID_RE)
    _require_number(record.get("wall_s"), "record wall_s", 0.001)
    record_counts = _validate_tool_counts(record.get("tool_counts"), job["run_id"])
    if _core_tool_counts(record_counts) != _core_tool_counts(trajectory_counts):
        raise ReportError("record and trajectory tool counts differ")
    routing, routing_counts = _validate_routing(record, job["variant"], job["run_id"])
    if job["variant"] == "deferred":
        trajectory_searches = trajectory_counts["calls_by_name"].get("tool_search", 0)
        if routing_counts["search_calls"] != trajectory_searches:
            raise ReportError("routing and trajectory search counts differ")
    if "surface" in record and record["surface"] != case["surface"]:
        raise ReportError("record surface differs from the matrix")
    return record, routing, routing_counts


def _validate_supplementary(
    campaign,
    runs_dir,
    case_map,
    seed,
    campaign_started,
    campaign_finished,
):
    if campaign.get("supplementary_counts_toward_active_duration") is not False:
        raise ReportError("supplementary coverage must not count toward active duration")
    raw_records = _require_list(
        campaign.get("supplementary_records"), "campaign supplementary_records",
    )
    expected_ids = EXPECTED_SUPPLEMENTARY_COMMANDS | set(EXPECTED_SUPPLEMENTARY_WEB)
    if len(raw_records) != len(expected_ids):
        raise ReportError("formal supplementary coverage is incomplete")
    by_id = {}
    for raw in raw_records:
        item = _require_dict(raw, "supplementary record")
        record_id = _require_string(
            item.get("record_id"), "supplementary record_id", RUN_ID_RE,
        )
        if record_id in by_id:
            raise ReportError("supplementary coverage contains duplicate records")
        by_id[record_id] = item
    if set(by_id) != expected_ids:
        raise ReportError("formal supplementary coverage IDs drifted")

    web_cases = [case for case in case_map.values() if case["surface"] == "web"]
    if len(web_cases) != 1:
        raise ReportError("formal supplementary coverage requires one Web case")
    web_case = web_cases[0]

    for record_id, item in by_id.items():
        if item.get("passed") is not True or item.get("real_execution") is not True:
            raise ReportError("supplementary coverage did not fully pass as a real execution")
        if item.get("counts_toward_active_duration") is not False:
            raise ReportError("supplementary record must not count toward active duration")
        begin = _parse_utc(item.get("started_at"), "supplementary started_at")
        end = _parse_utc(item.get("finished_at"), "supplementary finished_at")
        if begin < campaign_started or end > campaign_finished or end <= begin:
            raise ReportError("supplementary interval is outside the campaign wall window")
        _require_number(item.get("wall_s"), "supplementary wall_s", 0)

        if record_id in EXPECTED_SUPPLEMENTARY_COMMANDS:
            if item.get("kind") != "deterministic_command":
                raise ReportError("supplementary deterministic command kind drifted")
            if _require_int(item.get("exit_code"), "supplementary exit_code") != 0:
                raise ReportError("supplementary deterministic command failed")
            _require_string(
                item.get("argv_sha256"), "supplementary argv hash", SHA256_RE,
            )
            continue

        expected = EXPECTED_SUPPLEMENTARY_WEB[record_id]
        if item.get("kind") != "web_browser_canary":
            raise ReportError("supplementary Web record kind drifted")
        for name in ("variant", "language", "scenario", "routing_applicable"):
            if item.get(name) != expected[name]:
                raise ReportError("supplementary Web identity drifted")
        for name in ("driver_passed", "task_passed", "artifact_safe", "identity_matches"):
            if item.get(name) is not True:
                raise ReportError("supplementary Web evidence did not fully pass")

        run_id = f"supp__{record_id}"
        run_dir = Path(runs_dir) / "supplementary" / run_id
        record_value = _read_json(run_dir / "record.json", "supplementary record.json")
        trajectory_value = _read_json(
            run_dir / "trajectory.json", "supplementary trajectory.json",
        )
        redaction_value = _read_json(
            run_dir / "redaction_report.json", "supplementary redaction_report.json",
        )
        record_hash = _require_string(
            item.get("record_sha256"), "supplementary record hash", SHA256_RE,
        )
        if _file_sha256(run_dir / "record.json") != record_hash:
            raise ReportError("supplementary Web record hash drifted")
        _validate_redaction(redaction_value, run_id)
        job = {
            "run_id": run_id,
            "case_id": web_case["id"],
            "variant": expected["variant"],
            "repeat": 1,
        }
        _trajectory, trajectory_counts = _validate_trajectory(trajectory_value, job)
        record, _routing, _routing_counts = _validate_record(
            record_value, job, web_case, seed, trajectory_counts,
        )
        if (
            record.get("language") != expected["language"]
            or record.get("scenario") != expected["scenario"]
            or record.get("routing_applicable") is not expected["routing_applicable"]
            or record.get("real_execution") is not True
            or record.get("driver_passed") is not True
            or record.get("task_passed") is not True
            or record.get("contracts_passed") is not True
            or record.get("error_present") is not False
        ):
            raise ReportError("supplementary Web artifact identity or result drifted")


def _validate_campaign(
    campaign,
    jobs,
    records_by_id,
    runs_dir,
    case_map,
    seed,
    suite_hashes,
):
    campaign = _require_dict(campaign, "campaign.json")
    _walk_artifact_safety(campaign, "campaign")
    if campaign.get("schema_version") != 1:
        raise ReportError("campaign schema_version must be 1")
    if (
        campaign.get("status") != "complete"
        or campaign.get("formal") is not True
        or campaign.get("mode") != "formal"
        or "failure_code" in campaign
    ):
        raise ReportError("campaign must be a complete formal run without failure")
    if _require_int(campaign.get("workers"), "campaign workers", 1) != 1:
        raise ReportError("campaign must use workers=1")
    if campaign.get("model_label") != EXACT_MODEL_LABEL or campaign.get("model_id") != EXACT_MODEL_ID:
        raise ReportError("campaign model drifted from exact Kimi")
    parameters = _require_dict(campaign.get("request_parameters"), "campaign request parameters")
    if parameters.get("temperature") != "omitted":
        raise ReportError("campaign must prove temperature was omitted")
    if _require_dict(campaign.get("suite_inputs"), "campaign suite_inputs") != suite_hashes:
        raise ReportError("campaign suite input hashes drifted")

    git = _require_dict(campaign.get("git"), "campaign git")
    _require_string(git.get("commit"), "git commit", COMMIT_RE)
    if _require_bool(git.get("dirty"), "git dirty"):
        raise ReportError("formal campaign must be built from a clean git tree")
    binaries = _require_dict(campaign.get("binaries"), "campaign binaries")
    for name in REQUIRED_CAMPAIGN_HASHES:
        _require_string(binaries.get(name), f"binary hash {name}", SHA256_RE)
    environment = _require_dict(campaign.get("environment"), "campaign environment")
    for name in REQUIRED_ENVIRONMENT_FIELDS:
        _require_string(environment.get(name), f"environment {name}", maximum=120)

    started_at = _parse_utc(campaign.get("started_at"), "campaign started_at")
    finished_at = _parse_utc(campaign.get("finished_at"), "campaign finished_at")
    if finished_at <= started_at:
        raise ReportError("campaign finish must be after its start")
    _validate_supplementary(
        campaign,
        runs_dir,
        case_map,
        seed,
        started_at,
        finished_at,
    )
    monotonic = _require_number(
        campaign.get("monotonic_elapsed_s"), "campaign monotonic_elapsed_s", 0,
    )
    planned = _require_int(campaign.get("planned_run_count"), "planned run count", 0)
    completed = _require_int(campaign.get("completed_run_count"), "completed run count", 0)
    if planned != len(jobs) or completed != len(records_by_id) or completed != planned:
        raise ReportError("campaign plan/completion counts do not match artifacts")

    expected_ids = {job["run_id"] for job in jobs}
    intervals = _require_list(campaign.get("run_intervals"), "campaign run_intervals")
    if len(intervals) != len(jobs):
        raise ReportError("campaign must provide exactly one interval per planned run")
    normalized = []
    seen = set()
    for raw in intervals:
        interval = _require_dict(raw, "campaign run interval")
        run_id = _require_string(interval.get("run_id"), "interval run_id", RUN_ID_RE)
        if run_id not in expected_ids or run_id in seen or run_id not in records_by_id:
            raise ReportError("campaign interval does not map one-to-one to complete records")
        seen.add(run_id)
        begin = _parse_utc(interval.get("started_at"), "interval started_at")
        end = _parse_utc(interval.get("finished_at"), "interval finished_at")
        if begin < started_at or end > finished_at or end <= begin:
            raise ReportError("campaign interval is outside the campaign wall window")
        real = _require_bool(interval.get("real_execution"), "interval real_execution")
        successful = _require_bool(interval.get("successful"), "interval successful")
        interval_seconds = (end - begin).total_seconds()
        record_wall = _require_number(
            records_by_id[run_id]["record"].get("wall_s"),
            "interval record wall_s", 0.001,
        )
        normalized.append({
            "run_id": run_id,
            "start": begin,
            "end": end,
            "real_execution": real,
            "successful": successful,
            "interval_seconds": interval_seconds,
            "record_wall_seconds": record_wall,
        })
    if seen != expected_ids:
        raise ReportError("campaign intervals do not cover every planned run")

    normalized.sort(key=lambda item: (item["start"], item["end"]))
    overlap_count = 0
    prior_end = None
    for interval in normalized:
        if prior_end is not None and interval["start"] < prior_end:
            overlap_count += 1
        prior_end = max(prior_end, interval["end"]) if prior_end is not None else interval["end"]

    successful_real = [
        {
            **item,
            # Count no more than the per-run wall evidence. Wrapper setup,
            # cleanup, sleeps, and filler can therefore never satisfy the
            # 30-minute active-execution requirement.
            "end": min(
                item["end"],
                item["start"] + (item["end"] - item["start"])
                * min(1.0, item["record_wall_seconds"] / item["interval_seconds"]),
            ),
        }
        for item in normalized
        if item["real_execution"] and item["successful"]
    ]
    union_seconds = _interval_union_seconds(successful_real)
    wall_seconds = (finished_at - started_at).total_seconds()
    duration_checks = {
        "wall_at_least_1800s": wall_seconds >= MIN_CAMPAIGN_SECONDS,
        "monotonic_at_least_1800s": monotonic >= MIN_CAMPAIGN_SECONDS,
        "successful_real_union_at_least_1800s": union_seconds >= MIN_CAMPAIGN_SECONDS,
        "workers_one_no_interval_overlap": overlap_count == 0,
        "every_interval_real_execution": all(item["real_execution"] for item in normalized),
        "every_interval_successful": all(item["successful"] for item in normalized),
        "record_wall_fits_run_interval": all(
            item["record_wall_seconds"] <= item["interval_seconds"] + 2.0
            for item in normalized
        ),
    }
    return {
        "raw": campaign,
        "started_at": campaign["started_at"],
        "finished_at": campaign["finished_at"],
        "wall_seconds": wall_seconds,
        "monotonic_seconds": monotonic,
        "active_union_seconds": union_seconds,
        "overlap_count": overlap_count,
        "checks": duration_checks,
        "passed": all(duration_checks.values()),
    }


def _interval_union_seconds(intervals):
    if not intervals:
        return 0.0
    ordered = sorted(intervals, key=lambda item: (item["start"], item["end"]))
    total = 0.0
    start = ordered[0]["start"]
    end = ordered[0]["end"]
    for item in ordered[1:]:
        if item["start"] <= end:
            end = max(end, item["end"])
        else:
            total += (end - start).total_seconds()
            start, end = item["start"], item["end"]
    return total + (end - start).total_seconds()


def _operator_pass(value, operator, threshold):
    if operator == "eq":
        return value == threshold
    if operator == "gte":
        return value >= threshold
    if operator == "lte":
        return value <= threshold
    raise ReportError("matrix gate has an unsupported operator")


def _ratio(numerator, denominator, label):
    if denominator <= 0:
        raise ReportError(f"{label} has no eligible observations")
    return numerator / denominator


def _scoped(records, case_map, variant=None, tag=None):
    return [
        item for item in records
        if (variant is None or item["job"]["variant"] == variant)
        and (tag is None or tag in case_map[item["job"]["case_id"]]["metric_tags"])
    ]


def _gate(name, value, spec, numerator=None, denominator=None, extra_pass=True):
    threshold = spec["threshold"]
    passed = _operator_pass(value, spec["operator"], threshold) and extra_pass
    return {
        "name": name,
        "value": value,
        "threshold": threshold,
        "operator": spec["operator"],
        "aggregate": spec["aggregate"],
        "numerator": numerator,
        "denominator": denominator,
        "passed": passed,
    }


def _evaluate_gates(matrix, case_map, records, repeats):
    specs = matrix["hard_gates"]
    deferred_accuracy = _scoped(
        records, case_map, variant="deferred", tag="deferred_call_accuracy",
    )
    bypass = sum(item["routing_counts"]["bypass"] for item in deferred_accuracy)
    same_batch = sum(
        item["routing_counts"]["same_batch_activation"]
        for item in deferred_accuracy
    )
    deferred_calls = sum(
        item["routing_counts"]["deferred_calls"] for item in deferred_accuracy
    )
    deferred_success = sum(
        item["routing_counts"]["deferred_call_success"]
        for item in deferred_accuracy
    )

    irrelevant = _scoped(
        records, case_map, variant="deferred", tag="irrelevant_search",
    )
    irrelevant_searches = sum(
        item["tool_counts"]["calls_by_name"].get("tool_search", 0)
        for item in irrelevant
    )

    deferred_task = _scoped(records, case_map, variant="deferred", tag="task_pass")
    deferred_task_passes = sum(item["record"]["task_passed"] for item in deferred_task)
    critical = _scoped(records, case_map, variant="deferred", tag="critical_pass")
    critical_passes = sum(item["record"]["task_passed"] for item in critical)
    critical_by_case = defaultdict(list)
    for item in critical:
        critical_by_case[item["job"]["case_id"]].append(item)
    critical_rows_valid = all(
        len(items) == repeats
        and sum(item["record"]["task_passed"] for item in items) >= math.ceil(0.9 * repeats)
        for items in critical_by_case.values()
    ) and len(critical_by_case) == sum(
        1 for case in case_map.values() if "critical_pass" in case["metric_tags"]
    )

    paired = _scoped(records, case_map, tag="paired_task_pass")
    static_paired = [item for item in paired if item["job"]["variant"] == "static"]
    deferred_paired = [item for item in paired if item["job"]["variant"] == "deferred"]
    paired_keys_static = {
        (item["job"]["case_id"], item["job"]["repeat"]) for item in static_paired
    }
    paired_keys_deferred = {
        (item["job"]["case_id"], item["job"]["repeat"]) for item in deferred_paired
    }
    if paired_keys_static != paired_keys_deferred:
        raise ReportError("paired metric has incomplete static/deferred pairs")
    static_rate = _ratio(
        sum(item["record"]["task_passed"] for item in static_paired),
        len(static_paired), "static paired task pass rate",
    )
    deferred_rate = _ratio(
        sum(item["record"]["task_passed"] for item in deferred_paired),
        len(deferred_paired), "deferred paired task pass rate",
    )

    disclosure = _scoped(
        records, case_map, variant="deferred", tag="schema_disclosure",
    )
    first_visible = [item["tool_counts"]["first_visible"] for item in disclosure]
    if not first_visible:
        raise ReportError("schema disclosure gate has no observations")

    schema_scope = specs["first_schema_token_reduction"]["scope"]
    schema_tag = schema_scope.get("metric_tag")
    if not isinstance(schema_tag, str) or not schema_tag:
        raise ReportError("schema token reduction gate has no metric tag scope")
    schema_records = _scoped(records, case_map, tag=schema_tag)
    static_tokens = []
    deferred_tokens = []
    by_pair = defaultdict(dict)
    for item in schema_records:
        key = (item["job"]["case_id"], item["job"]["repeat"])
        token_count = item["tool_counts"]["first_schema_tokens_estimate"]
        if token_count <= 0:
            raise ReportError("schema token evidence must be positive")
        by_pair[key][item["job"]["variant"]] = token_count
    if not by_pair:
        raise ReportError("schema token reduction gate has no scoped pairs")
    for pair in by_pair.values():
        if set(pair) != EXPECTED_VARIANTS:
            raise ReportError("schema metric has incomplete static/deferred pairs")
        static_tokens.append(pair["static"])
        deferred_tokens.append(pair["deferred"])
    schema_reduction = 1.0 - (sum(deferred_tokens) / sum(static_tokens))

    gates = [
        _gate("deferred_bypass", bypass, specs["deferred_bypass"]),
        _gate(
            "same_batch_activation", same_batch,
            specs["same_batch_activation"],
        ),
        _gate(
            "deferred_argument_success_rate",
            _ratio(deferred_success, deferred_calls, "deferred argument success"),
            specs["deferred_argument_success_rate"],
            deferred_success, deferred_calls,
        ),
        _gate(
            "irrelevant_search_rate",
            _ratio(irrelevant_searches, len(irrelevant), "irrelevant search rate"),
            specs["irrelevant_search_rate"],
            irrelevant_searches, len(irrelevant),
        ),
        _gate(
            "deferred_task_pass_rate",
            _ratio(deferred_task_passes, len(deferred_task), "deferred task pass rate"),
            specs["deferred_task_pass_rate"],
            deferred_task_passes, len(deferred_task),
        ),
        _gate(
            "critical_deferred_pass_rate",
            _ratio(critical_passes, len(critical), "critical deferred pass rate"),
            specs["critical_deferred_pass_rate"],
            critical_passes, len(critical), extra_pass=critical_rows_valid,
        ),
        _gate(
            "paired_noninferiority", deferred_rate - static_rate,
            specs["paired_noninferiority"],
        ),
        _gate(
            "normal_first_visible_tools", max(first_visible),
            specs["normal_first_visible_tools"],
        ),
        _gate(
            "first_schema_token_reduction", schema_reduction,
            specs["first_schema_token_reduction"],
        ),
    ]
    if len(gates) != 9:
        raise AssertionError("the acceptance report must keep exactly nine hard gates")
    return gates


def _case_summaries(records, case_map, repeats):
    grouped = defaultdict(lambda: defaultdict(list))
    for item in records:
        grouped[item["job"]["case_id"]][item["job"]["variant"]].append(item)
    summaries = []
    for case_id, case in case_map.items():
        variants = {}
        for variant in ("static", "deferred"):
            items = grouped[case_id][variant]
            passed = sum(item["record"]["task_passed"] for item in items)
            searches = sum(
                item["tool_counts"]["calls_by_name"].get("tool_search", 0)
                for item in items
            )
            variants[variant] = {
                "runs": len(items),
                "passed": passed,
                "pass_rate": (passed / len(items)) if items else None,
                "searches": searches,
                "bypass": sum(item["routing_counts"].get("bypass", 0) for item in items),
                "same_batch": sum(
                    item["routing_counts"].get("same_batch_activation", 0)
                    for item in items
                ),
                "deferred_calls": sum(
                    item["routing_counts"].get("deferred_calls", 0) for item in items
                ),
                "deferred_success": sum(
                    item["routing_counts"].get("deferred_call_success", 0)
                    for item in items
                ),
                "first_visible_max": max(
                    (item["tool_counts"]["first_visible"] for item in items),
                    default=None,
                ),
                "schema_tokens_avg": (sum(
                    item["tool_counts"]["first_schema_tokens_estimate"]
                    for item in items
                ) / len(items)) if items else None,
                "wall_avg": (
                    sum(item["record"]["wall_s"] for item in items) / len(items)
                    if items else None
                ),
            }
        static_tokens = variants["static"]["schema_tokens_avg"]
        deferred_tokens = variants["deferred"]["schema_tokens_avg"]
        reduction = (
            1 - deferred_tokens / static_tokens
            if static_tokens is not None and deferred_tokens is not None else None
        )
        critical_pass_at_n = (
            variants["deferred"]["passed"] >= math.ceil(0.9 * repeats)
            if case["critical"] else None
        )
        summaries.append({
            "case_id": case_id,
            "title": case["title"],
            "category": case["category"],
            "surface": case["surface"],
            "critical": case["critical"],
            "metric_tags": list(case["metric_tags"]),
            "variants": variants,
            "paired_delta": (
                variants["deferred"]["pass_rate"] - variants["static"]["pass_rate"]
                if variants["deferred"]["pass_rate"] is not None
                and variants["static"]["pass_rate"] is not None else None
            ),
            "schema_reduction": reduction,
            "critical_pass_at_n": critical_pass_at_n,
        })
    return summaries


def _failure_rows(records, case_map):
    rows = []
    for item in records:
        record = item["record"]
        kinds = []
        if not record["task_passed"]:
            kinds.append("task_failed")
        if not record["contracts_passed"]:
            kinds.append("contract_failed")
        if record["error_present"]:
            kinds.append("model_or_runner_error")
        if item["job"]["variant"] == "deferred" and not item["routing"]["passed"]:
            kinds.append("routing_failed")
        for violation in item["routing"].get("violations", []):
            kind = violation.get("type")
            if isinstance(kind, str) and NAME_RE.fullmatch(kind):
                kinds.append(kind)
        if kinds:
            rows.append({
                "run_id": item["job"]["run_id"],
                "case_id": item["job"]["case_id"],
                "surface": case_map[item["job"]["case_id"]]["surface"],
                "variant": item["job"]["variant"],
                "repeat": item["job"]["repeat"],
                "classes": sorted(set(kinds)),
                "stop_reason": record["stop_reason"],
            })
    return rows


def evaluate(matrix_path, base_suite_path, runs_dir, campaign_path=None):
    """Load all sanitized artifacts and return the acceptance calculation."""
    runs_dir = Path(runs_dir)
    suite_hashes = _locked_suite_input_hashes(matrix_path, base_suite_path)
    matrix, case_map = _load_matrix(matrix_path, base_suite_path)
    plan = _read_json(runs_dir / "plan.json", "plan.json")
    jobs, repeats, seed = _validate_plan(plan, case_map, suite_hashes)
    all_records = _require_list(
        _read_json(runs_dir / "all_records.json", "all_records.json"),
        "all_records.json",
    )
    _walk_artifact_safety(all_records, "all_records.json")
    all_records_by_id = {}
    for item in all_records:
        item = _require_dict(item, "all_records entry")
        run_id = _require_string(item.get("run_id"), "all_records run_id", RUN_ID_RE)
        if run_id in all_records_by_id:
            raise ReportError("all_records contains duplicate run IDs")
        all_records_by_id[run_id] = item
    if set(all_records_by_id) != {job["run_id"] for job in jobs}:
        raise ReportError("all_records does not exactly match the plan")

    records = []
    for job in jobs:
        run_id = job["run_id"]
        run_dir = runs_dir / run_id
        record_value = _read_json(run_dir / "record.json", "per-run record")
        trajectory_value = _read_json(run_dir / "trajectory.json", "per-run trajectory")
        redaction_value = _read_json(
            run_dir / "redaction_report.json", "per-run redaction report",
        )
        if record_value != all_records_by_id[run_id]:
            raise ReportError("per-run record differs from all_records")
        _validate_redaction(redaction_value, run_id)
        trajectory, trajectory_counts = _validate_trajectory(trajectory_value, job)
        case = case_map[job["case_id"]]
        record, routing, routing_counts = _validate_record(
            record_value, job, case, seed, trajectory_counts,
        )
        records.append({
            "job": job,
            "case": case,
            "record": record,
            "trajectory": trajectory,
            "tool_counts": trajectory_counts,
            "routing": routing,
            "routing_counts": routing_counts,
        })

    campaign_value = _read_json(
        campaign_path or runs_dir / "campaign.json", "campaign.json",
    )
    campaign = _validate_campaign(
        campaign_value,
        jobs,
        {item["job"]["run_id"]: item for item in records},
        runs_dir,
        case_map,
        seed,
        suite_hashes,
    )
    if _suite_input_hashes(matrix_path, base_suite_path) != suite_hashes:
        raise ReportError("formal suite inputs changed while generating the report")
    gates = _evaluate_gates(matrix, case_map, records, repeats)
    summaries = _case_summaries(records, case_map, repeats)
    failures = _failure_rows(records, case_map)
    contracts_passed = all(item["record"]["contracts_passed"] for item in records)
    efforts = {item["record"]["effort"] for item in records}
    if len(efforts) != 1:
        raise ReportError("record effort must remain consistent across the campaign")
    effort = next(iter(efforts))
    overall = all(gate["passed"] for gate in gates) and campaign["passed"] and contracts_passed
    return {
        "schema_version": 1,
        "overall_passed": overall,
        "gates": gates,
        "campaign": campaign,
        "contracts_passed": contracts_passed,
        "repeats": repeats,
        "run_count": len(records),
        "case_count": len(case_map),
        "request_parameters": {
            "temperature": "omitted",
            "effort": effort or "omitted",
        },
        "case_summaries": summaries,
        "failure_rows": failures,
        "records": records,
    }


def _fmt_metric(gate):
    value = gate["value"]
    if gate["name"] in {
        "deferred_argument_success_rate",
        "irrelevant_search_rate",
        "deferred_task_pass_rate",
        "critical_deferred_pass_rate",
        "paired_noninferiority",
        "first_schema_token_reduction",
    }:
        return f"{value * 100:.2f}%"
    if isinstance(value, int):
        return str(value)
    return f"{value:.2f}"


def _fmt_threshold(gate):
    signs = {"eq": "=", "gte": "≥", "lte": "≤"}
    threshold = gate["threshold"]
    if gate["name"] in {
        "deferred_argument_success_rate",
        "irrelevant_search_rate",
        "deferred_task_pass_rate",
        "critical_deferred_pass_rate",
        "paired_noninferiority",
        "first_schema_token_reduction",
    }:
        value = f"{threshold * 100:.0f}%"
    else:
        value = str(threshold)
    return f"{signs[gate['operator']]} {value}"


def _safe_names(value):
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, str) and NAME_RE.fullmatch(item)]


def _trajectory_lines(trajectory):
    lines = []
    for session in trajectory["sessions"]:
        for entry in session["entries"]:
            entry_type = entry.get("type")
            if entry_type == "tool_call":
                name = entry.get("name")
                if isinstance(name, str) and NAME_RE.fullmatch(name):
                    batch_size = entry.get("batch_size", 0)
                    batch_index = entry.get("batch_index", 0)
                    lines.append(f"CALL {name} · batch {batch_index + 1}/{batch_size}")
            elif entry_type == "tool_result":
                name = entry.get("name")
                status = entry.get("status")
                duration = entry.get("duration_ms")
                if (isinstance(name, str) and NAME_RE.fullmatch(name)
                        and isinstance(status, str) and NAME_RE.fullmatch(status)
                        and isinstance(duration, (int, float))):
                    lines.append(f"RESULT {name} · {status} · {duration:g} ms")
            elif entry_type == "tool_observation":
                kind = entry.get("kind")
                if kind == "model_request":
                    visible = entry.get("visible_count", 0)
                    tokens = entry.get("schema_tokens_estimate", 0)
                    names = _safe_names(entry.get("newly_visible_deferred"))
                    suffix = f" · activated {', '.join(names)}" if names else ""
                    lines.append(f"MODEL REQUEST · visible {visible} · schema ≈{tokens} tok{suffix}")
                elif kind == "tool_search":
                    mode = entry.get("query_mode")
                    matches = _safe_names(entry.get("match_names"))
                    success = entry.get("success") is True
                    lines.append(
                        f"SEARCH · {mode if mode in ('select', 'keyword') else 'invalid'} · "
                        f"{'ok' if success else 'failed'} · matches {', '.join(matches) or 'none'}"
                    )
                elif kind == "deferred_bypass":
                    tool = entry.get("tool_name")
                    if isinstance(tool, str) and NAME_RE.fullmatch(tool):
                        lines.append(f"BYPASS · {tool}")
    return lines or ["No tool events (metadata-only trajectory)."]


def _artifact_href(run_id, name):
    return f"{quote(run_id, safe='._-')}/{quote(name, safe='._-')}"


def render_html(evaluation):
    """Render one self-contained HTML document from validated metadata."""
    esc = lambda value: html.escape(str(value), quote=True)
    status = "PASS" if evaluation["overall_passed"] else "FAIL"
    status_class = "pass" if evaluation["overall_passed"] else "fail"
    campaign = evaluation["campaign"]
    campaign_raw = campaign["raw"]

    gate_rows = []
    for gate in evaluation["gates"]:
        ratio = ""
        if gate["numerator"] is not None:
            ratio = f"{gate['numerator']}/{gate['denominator']}"
        gate_rows.append(
            "<tr>"
            f"<td><code>{esc(gate['name'])}</code></td>"
            f"<td>{esc(_fmt_metric(gate))}</td>"
            f"<td>{esc(_fmt_threshold(gate))}</td>"
            f"<td>{esc(ratio)}</td>"
            f"<td><span class=\"pill {'pass' if gate['passed'] else 'fail'}\">"
            f"{'PASS' if gate['passed'] else 'FAIL'}</span></td></tr>"
        )

    duration_rows = "".join(
        "<tr>"
        f"<td>{esc(name)}</td>"
        f"<td><span class=\"pill {'pass' if passed else 'fail'}\">"
        f"{'PASS' if passed else 'FAIL'}</span></td></tr>"
        for name, passed in campaign["checks"].items()
    )

    case_rows = []
    for summary in evaluation["case_summaries"]:
        static = summary["variants"]["static"]
        deferred = summary["variants"]["deferred"]
        static_pass = f"{static['passed']}/{static['runs']}" if static["runs"] else "—"
        deferred_pass = f"{deferred['passed']}/{deferred['runs']}" if deferred["runs"] else "—"
        first_visible = (
            str(deferred["first_visible_max"])
            if deferred["first_visible_max"] is not None else "—"
        )
        schema_reduction = (
            f"{summary['schema_reduction'] * 100:.1f}%"
            if summary["schema_reduction"] is not None else "—"
        )
        wall_comparison = (
            f"{static['wall_avg']:.1f}s / {deferred['wall_avg']:.1f}s"
            if static["wall_avg"] is not None and deferred["wall_avg"] is not None
            else f"— / {deferred['wall_avg']:.1f}s"
        )
        critical = "yes" if summary["critical"] else "no"
        critical_result = "—"
        if summary["critical"]:
            critical_result = (
                f"{deferred['passed']}/{deferred['runs']} "
                f"({'PASS' if summary['critical_pass_at_n'] else 'FAIL'})"
            )
        row_bad = (
            deferred["bypass"] > 0 or deferred["same_batch"] > 0
            or (summary["critical"] and not summary["critical_pass_at_n"])
            or (static["runs"] and static["passed"] < static["runs"])
            or deferred["passed"] < deferred["runs"]
        )
        case_rows.append(
            f"<tr class=\"{'warn-row' if row_bad else ''}\">"
            f"<td><code>{esc(summary['case_id'])}</code><small>{esc(summary['title'])}</small></td>"
            f"<td>{esc(summary['surface'])}</td><td>{critical}</td>"
            f"<td>{static_pass}</td>"
            f"<td>{deferred_pass}</td>"
            f"<td>{esc(critical_result)}</td>"
            f"<td>{deferred['deferred_success']}/{deferred['deferred_calls']}</td>"
            f"<td>{deferred['searches']}</td><td>{deferred['bypass']}</td>"
            f"<td>{deferred['same_batch']}</td>"
            f"<td>{first_visible}</td>"
            f"<td>{schema_reduction}</td>"
            f"<td>{wall_comparison}</td></tr>"
        )

    failure_rows = []
    for failure in evaluation["failure_rows"]:
        failure_rows.append(
            "<tr>"
            f"<td><a href=\"{esc(_artifact_href(failure['run_id'], 'record.json'))}\">"
            f"{esc(failure['run_id'])}</a></td>"
            f"<td>{esc(failure['surface'])}</td><td>{esc(failure['variant'])}</td>"
            f"<td>{esc(', '.join(failure['classes']))}</td>"
            f"<td>{esc(failure['stop_reason'])}</td></tr>"
        )
    if not failure_rows:
        failure_rows.append("<tr><td colspan=\"5\">No run-level failures.</td></tr>")

    trajectories = []
    for item in evaluation["records"]:
        job = item["job"]
        links = " · ".join(
            f"<a href=\"{esc(_artifact_href(job['run_id'], name))}\">{esc(label)}</a>"
            for name, label in (
                ("record.json", "record"),
                ("trajectory.json", "trajectory"),
                ("redaction_report.json", "redaction"),
            )
        )
        lines = "".join(
            f"<li>{esc(line)}</li>" for line in _trajectory_lines(item["trajectory"])
        )
        trajectories.append(
            "<details><summary>"
            f"<code>{esc(job['run_id'])}</code> · {esc(item['case']['surface'])} · "
            f"{'PASS' if item['record']['task_passed'] else 'FAIL'}"
            f"</summary><p class=\"artifact-links\">{links}</p><ol>{lines}</ol></details>"
        )

    environment = campaign_raw["environment"]
    binaries = campaign_raw["binaries"]
    git = campaign_raw["git"]
    gate_failures = [gate["name"] for gate in evaluation["gates"] if not gate["passed"]]
    conclusion = (
        "All nine pinned ToolSearch gates, artifact integrity checks, and the "
        "30-minute real-execution proof passed."
        if evaluation["overall_passed"] else
        "Acceptance failed. Fix every failed gate or integrity condition and rerun the full campaign."
    )

    return f"""<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>JCode ToolSearch acceptance report — {status}</title>
<style>
:root{{--bg:#0b1020;--panel:#121a2e;--ink:#e8edf8;--muted:#9aa8c2;--line:#2a3550;
--good:#3ddc97;--bad:#ff6b7a;--warn:#ffcc66;--accent:#7aa2ff}}*{{box-sizing:border-box}}
body{{margin:0;background:var(--bg);color:var(--ink);font:14px/1.55 ui-sans-serif,system-ui,-apple-system,sans-serif}}
main{{max-width:1500px;margin:auto;padding:32px}}h1{{font-size:36px;margin:.2em 0}}h2{{margin-top:36px}}
p,small{{color:var(--muted)}}a{{color:#9bb7ff}}code{{font-family:ui-monospace,SFMono-Regular,monospace}}
.hero,.panel{{background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:22px;margin:18px 0}}
.hero{{display:grid;grid-template-columns:auto 1fr;gap:22px;align-items:center}}
.verdict{{font-size:28px;font-weight:800;border:2px solid;padding:16px 22px;border-radius:12px}}
.pass{{color:var(--good)}}.fail{{color:var(--bad)}}.pill{{font-weight:800}}
.grid{{display:grid;grid-template-columns:repeat(auto-fit,minmax(250px,1fr));gap:12px}}
.metric{{background:#0d1528;border:1px solid var(--line);border-radius:10px;padding:14px}}
.metric b{{font-size:22px;display:block}}table{{width:100%;border-collapse:collapse;display:block;overflow:auto}}
th,td{{text-align:left;border-bottom:1px solid var(--line);padding:9px 11px;vertical-align:top;white-space:nowrap}}
th{{color:#b8c5dc;background:#10192d;position:sticky;top:0}}td small{{display:block;white-space:normal;max-width:340px}}
.warn-row{{background:#2b2114}}details{{border-top:1px solid var(--line);padding:10px 2px}}
summary{{cursor:pointer}}ol{{color:#c8d2e7}}.artifact-links{{margin-left:20px}}
.provenance code{{word-break:break-all}}footer{{margin:40px 0;color:var(--muted)}}
</style></head><body><main>
<section class="hero"><div class="verdict {status_class}">{status}</div><div>
<h1>ToolSearch acceptance report</h1><p>{esc(conclusion)}</p>
<p>{evaluation['case_count']} cases · {evaluation['run_count']} real runs · paired static/deferred · workers=1</p></div></section>

<section class="panel"><h2>Acceptance gates</h2><table><thead><tr>
<th>Fixed gate</th><th>Observed</th><th>Threshold</th><th>Evidence</th><th>Status</th>
</tr></thead><tbody>{''.join(gate_rows)}</tbody></table>
<p>Failed fixed gates: {esc(', '.join(gate_failures) or 'none')}.</p></section>

<section class="panel"><h2>30-minute campaign proof</h2><div class="grid">
<div class="metric"><span>Wall elapsed</span><b>{campaign['wall_seconds']:.1f}s</b></div>
<div class="metric"><span>Monotonic elapsed</span><b>{campaign['monotonic_seconds']:.1f}s</b></div>
<div class="metric"><span>Successful real-run interval union</span><b>{campaign['active_union_seconds']:.1f}s</b></div>
<div class="metric"><span>Interval overlaps</span><b>{campaign['overlap_count']}</b></div></div>
<p>{esc(campaign['started_at'])} → {esc(campaign['finished_at'])}</p>
<table><tbody>{duration_rows}</tbody></table></section>

<section class="panel provenance"><h2>Reproducibility</h2><div class="grid">
<div><b>Model</b><p><code>{esc(EXACT_MODEL_ID)}</code><br>
temperature={esc(evaluation['request_parameters']['temperature'])}<br>
effort={esc(evaluation['request_parameters']['effort'])}</p></div>
<div><b>Git</b><p><code>{esc(git['commit'])}</code><br>dirty={str(git['dirty']).lower()}</p></div>
<div><b>Environment</b><p>{esc(environment['go_version'])}<br>{esc(environment['os_arch'])}<br>Eino {esc(environment['eino_version'])}</p></div>
<div><b>Binary SHA-256</b><p>jcode <code>{esc(binaries['jcode_sha256'])}</code><br>
harness <code>{esc(binaries['harness_sha256'])}</code><br>
MCP fixture <code>{esc(binaries['mcp_fixture_sha256'])}</code></p></div></div>
<p>Artifacts: <a href="plan.json">plan</a> · <a href="all_records.json">all records</a> · <a href="campaign.json">campaign</a>.</p></section>

<section class="panel"><h2>Design and evidence boundary</h2>
<p>The static arm exposes the complete schema set. The deferred arm starts with the normal Direct set plus ToolSearch,
then discloses matched schemas on a later model request. The report uses only the validated nine-gate matrix,
metadata-only trajectories, deterministic routing verdicts, and safe redaction reports. It does not open or render
private sessions, prompts, configs, debug logs, raw arguments, or raw outputs.</p></section>

<section class="panel"><h2>Scenario matrix — every case remains visible</h2><table><thead><tr>
<th>Case</th><th>Transport</th><th>Critical</th><th>Static pass</th><th>Deferred pass</th>
<th>Critical pass@{evaluation['repeats']}</th><th>Deferred args/result</th><th>Searches</th><th>Bypass</th><th>Same batch</th>
<th>First visible max</th><th>Schema reduction</th><th>Avg wall static/deferred</th>
</tr></thead><tbody>{''.join(case_rows)}</tbody></table></section>

<section class="panel"><h2>Failure classification</h2><table><thead><tr>
<th>Run</th><th>Transport</th><th>Variant</th><th>Classes</th><th>Stop reason</th>
</tr></thead><tbody>{''.join(failure_rows)}</tbody></table></section>

<section class="panel"><h2>Sanitized tool trajectories</h2>
<p>Names, order, batch position, status, duration, visible counts, schema estimates, and search match names only.</p>
{''.join(trajectories)}</section>
<footer>Generated from publication-safe artifacts. Overall verdict also requires every contract check to pass: {str(evaluation['contracts_passed']).lower()}.</footer>
</main></body></html>"""


def scan_report(path, secret_values=(), forbidden_paths=()):
    """Return metadata-only safety findings for a rendered report."""
    try:
        text = Path(path).read_text()
    except OSError:
        return [{"category": "missing_report"}]
    findings = set()
    for secret in secret_values:
        if isinstance(secret, str) and secret and secret in text:
            findings.add("exact_credential")
    for host_path in forbidden_paths:
        if isinstance(host_path, str) and host_path and host_path in text:
            findings.add("host_path")
    if ABSOLUTE_PATH_RE.search(text):
        findings.add("host_path_pattern")
    if any(pattern.search(text) for pattern in CREDENTIAL_PATTERNS):
        findings.add("credential_pattern")
    return [{"category": name} for name in sorted(findings)]


def generate_report(matrix_path, base_suite_path, runs_dir, output_path=None,
                    campaign_path=None, secret_values=(), forbidden_paths=()):
    """Evaluate, render, safety-scan, and return report metadata."""
    runs_dir = Path(runs_dir).resolve()
    output = Path(output_path).resolve() if output_path else runs_dir / "toolsearch-report.html"
    if output.parent != runs_dir:
        raise ReportError("HTML output must live directly in the runs directory")
    evaluation = evaluate(matrix_path, base_suite_path, runs_dir, campaign_path)
    document = render_html(evaluation)
    descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        stream.write(document)
    output.chmod(0o600)
    findings = scan_report(output, secret_values, forbidden_paths)
    if findings:
        try:
            output.unlink()
        except FileNotFoundError:
            pass
        raise ReportError("rendered HTML failed the publication safety scan")
    return {
        "overall_passed": evaluation["overall_passed"],
        "output_name": output.name,
        "sha256": hashlib.sha256(output.read_bytes()).hexdigest(),
        "gate_results": {
            gate["name"]: gate["passed"] for gate in evaluation["gates"]
        },
        "campaign_duration_passed": evaluation["campaign"]["passed"],
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--matrix", default=str(toolsearch_cases.DEFAULT_MATRIX))
    parser.add_argument("--base-suite", default=str(toolsearch_cases.DEFAULT_BASE_SUITE))
    parser.add_argument("--runs-dir", required=True)
    parser.add_argument("--campaign", default="")
    parser.add_argument("--output", default="")
    args = parser.parse_args(argv)
    try:
        result = generate_report(
            args.matrix,
            args.base_suite,
            args.runs_dir,
            output_path=args.output or None,
            campaign_path=args.campaign or None,
        )
    except ReportError as exc:
        print(f"FAIL: {exc}", file=sys.stderr)
        return 2
    print(json.dumps(result, sort_keys=True))
    return 0 if result["overall_passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
