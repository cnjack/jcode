#!/usr/bin/env python3
"""Run the Web-only Browser ToolSearch matrix against the authenticated UI.

This runner is deliberately independent from the ACP orchestrator.  It accepts
only ``surface=web`` cases, pins the exact non-highspeed Kimi model, executes
static/deferred variants as sequential pairs, and delegates the real browser
interaction to :mod:`web_browser_driver`.

Raw HOME, usage events, provider configuration, and session JSONL are private
runtime evidence.  They are inspected before cleanup; only fixed-shape driver
evidence, the standard metadata-only trajectory, and metadata-only routing
verdicts are written to the publication directory.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import random
import re
import shutil
import sys
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable


sys.path.insert(0, str(Path(__file__).resolve().parent))

import artifact_safety  # noqa: E402
import session_extract  # noqa: E402
import toolsearch_cases  # noqa: E402
import toolsearch_expect  # noqa: E402
import web_browser_driver  # noqa: E402


EXACT_MODEL_LABEL = "kimi-for-coding"
EXACT_MODEL_ID = "kimi-for-coding/kimi-for-coding"
EXACT_PROVIDER = "kimi-for-coding"
VARIANTS = ("static", "deferred")
LANGUAGES = ("en", "zh")
SCENARIOS = ("success", "approval_deny", "browser_disabled")
DEFAULT_SEED = 20260718
DEFAULT_REAL_HOME = Path(os.path.expanduser("~"))
DEFAULT_PROVIDER_CONFIG = DEFAULT_REAL_HOME / ".jcode" / "config.json"
DEFAULT_MODEL_CACHE = DEFAULT_REAL_HOME / ".jcode" / "cache" / "models_dev.json"
MAX_CONFIG_BYTES = 4 << 20
MAX_USAGE_BYTES = 16 << 20
SAFE_ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,159}$")
SAFE_CODE_RE = re.compile(r"^[a-z][a-z0-9_]{0,79}$")
PROVIDER_RUNTIME_FIELDS = {
    "api_key",
    "base_url",
    "headers",
    "vision",
    "thinking",
    "reasoning_effort",
    "custom_models",
}
USAGE_FIELD_MAP = {
    "prompt": "prompt_tokens",
    "completion": "completion_tokens",
    "cached": "cached_tokens",
    "reasoning": "reasoning_tokens",
    "total": "total_tokens",
    "calls": "model_calls",
}


class RunnerError(ValueError):
    """A stable, non-sensitive runner configuration or execution failure."""


@dataclass(frozen=True)
class CampaignOptions:
    binary: Path
    runs_dir: Path
    matrix: Path = toolsearch_cases.DEFAULT_MATRIX
    base_suite: Path = toolsearch_cases.DEFAULT_BASE_SUITE
    case_ids: tuple[str, ...] = ()
    variants: tuple[str, ...] = VARIANTS
    languages: tuple[str, ...] = ("en",)
    scenario: str = "success"
    repeats: int = 1
    seed: int = DEFAULT_SEED
    workers: int = 1
    formal: bool = False
    dry_run: bool = False
    provider_config: Path = DEFAULT_PROVIDER_CONFIG
    model_cache: Path = DEFAULT_MODEL_CACHE
    max_iterations: int = 40
    startup_timeout_s: float = 30.0
    poll_interval_s: float = 0.25
    request_timeout_s: float = 5.0


@dataclass(frozen=True)
class Job:
    case: dict[str, Any]
    variant: str
    language: str
    scenario: str
    repeat: int
    ordinal: int
    run_id: str
    pair_id: str

    def publication_record(self) -> dict[str, Any]:
        return {
            "ordinal": self.ordinal,
            "run_id": self.run_id,
            "pair_id": self.pair_id,
            "case_id": self.case["id"],
            "surface": "web",
            "model": EXACT_MODEL_LABEL,
            "model_id": EXACT_MODEL_ID,
            "variant": self.variant,
            "language": self.language,
            "scenario": self.scenario,
            "repeat": self.repeat,
            "timeout_s": self.case["timeout"],
        }


def _write_private_json(path: Path, value: Any) -> None:
    payload = json.dumps(value, indent=2, sort_keys=True) + "\n"
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        stream.write(payload)
    path.chmod(0o600)


def _append_private_jsonl(path: Path, value: Any) -> None:
    payload = json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n"
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    with os.fdopen(descriptor, "a") as stream:
        stream.write(payload)
    path.chmod(0o600)


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        while chunk := stream.read(1 << 20):
            digest.update(chunk)
    return digest.hexdigest()


def _json_clone(value: Any) -> Any:
    return json.loads(json.dumps(value))


def _load_selected_provider(config_path: Path) -> tuple[dict[str, Any], list[str]]:
    try:
        raw = config_path.read_bytes()
    except OSError as error:
        raise RunnerError("provider_config_unavailable") from error
    if len(raw) > MAX_CONFIG_BYTES:
        raise RunnerError("provider_config_too_large")
    try:
        config = json.loads(raw)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise RunnerError("provider_config_invalid") from error
    if not isinstance(config, dict):
        raise RunnerError("provider_config_invalid")
    providers = config.get("providers") or config.get("models")
    source = providers.get(EXACT_PROVIDER) if isinstance(providers, dict) else None
    if not isinstance(source, dict):
        raise RunnerError("exact_provider_unavailable")
    selected = {
        key: _json_clone(source[key])
        for key in PROVIDER_RUNTIME_FIELDS
        if key in source
    }
    custom_models = selected.get("custom_models")
    if isinstance(custom_models, list):
        selected["custom_models"] = [
            item
            for item in custom_models
            if isinstance(item, dict) and item.get("id") == EXACT_PROVIDER
        ]
        if not selected["custom_models"]:
            selected.pop("custom_models")

    secrets = []
    if isinstance(selected.get("api_key"), str) and selected["api_key"]:
        secrets.append(selected["api_key"])
    headers = selected.get("headers")
    if isinstance(headers, dict):
        secrets.extend(
            value for value in headers.values() if isinstance(value, str) and value
        )
    return selected, secrets


def _browser_config(case: dict[str, Any], scenario: str) -> tuple[dict[str, Any], bool, str]:
    home_config = case.get("home_config", {})
    if not isinstance(home_config, dict) or set(home_config) != {"browser"}:
        raise RunnerError("web_home_config_unsupported")
    browser = home_config.get("browser")
    expected = {
        "enabled": True,
        "backend": "managed",
        "headless": True,
        "approval": {
            "navigate": "always_allow",
            "interact": "always_allow",
        },
    }
    if not isinstance(browser, dict) or browser != expected:
        raise RunnerError("browser_config_contract_drift")

    configured = _json_clone(browser)
    if scenario == "success":
        return configured, True, "full_access"
    if scenario == "approval_deny":
        configured["approval"] = {"navigate": "ask", "interact": "ask"}
        return configured, False, "approval"
    if scenario == "browser_disabled":
        configured["enabled"] = False
        configured["approval"] = {"navigate": "ask", "interact": "ask"}
        return configured, False, "approval"
    raise RunnerError("scenario_invalid")


def build_eval_home(
    home: Path,
    *,
    provider_config: Path,
    model_cache: Path,
    case: dict[str, Any],
    variant: str,
    scenario: str,
    max_iterations: int,
) -> dict[str, Any]:
    """Create an owner-only HOME containing only the exact selected provider."""

    if variant not in VARIANTS:
        raise RunnerError("variant_invalid")
    if max_iterations <= 0:
        raise RunnerError("max_iterations_invalid")
    selected_provider, secrets = _load_selected_provider(provider_config)
    browser, auto_approve, default_mode = _browser_config(case, scenario)

    home.mkdir(parents=True, mode=0o700, exist_ok=False)
    home.chmod(0o700)
    jcode = home / ".jcode"
    cache = jcode / "cache"
    cache.mkdir(parents=True, mode=0o700)
    jcode.chmod(0o700)
    cache.chmod(0o700)
    output = {
        "providers": {EXACT_PROVIDER: selected_provider},
        "model": EXACT_MODEL_ID,
        "auto_approve": auto_approve,
        "default_mode": default_mode,
        "max_iterations": max_iterations,
        "tool_search": {"enabled": variant == "deferred"},
        "memory": {"generate": False},
        "browser": browser,
    }
    config_path = jcode / "config.json"
    _write_private_json(config_path, output)
    if model_cache.is_file():
        copied_cache = cache / "models_dev.json"
        shutil.copyfile(model_cache, copied_cache)
        copied_cache.chmod(0o600)
    effort = selected_provider.get("reasoning_effort")
    return {
        "secret_values": secrets,
        "effort": effort if isinstance(effort, str) else "",
    }


def load_web_cases(
    matrix: Path,
    base_suite: Path,
    selected_ids: tuple[str, ...] = (),
) -> list[dict[str, Any]]:
    suite = toolsearch_cases.load_suite(matrix, base_suite)
    cases = suite["cases"]
    by_id = {case["id"]: case for case in cases}
    if len(set(selected_ids)) != len(selected_ids):
        raise RunnerError("duplicate_case_selection")
    if selected_ids:
        selected = []
        for case_id in selected_ids:
            case = by_id.get(case_id)
            if case is None:
                raise RunnerError("selected_case_unknown")
            if case.get("surface") != "web":
                raise RunnerError("selected_case_not_web")
            selected.append(case)
    else:
        selected = [case for case in cases if case.get("surface") == "web"]
    if not selected:
        raise RunnerError("no_web_cases_selected")
    if any(case.get("surface") != "web" for case in selected):
        raise RunnerError("selected_case_not_web")
    return selected


def _bounded_run_id(value: str) -> str:
    if len(value) <= 150 and SAFE_ID_RE.fullmatch(value):
        return value
    digest = hashlib.sha256(value.encode()).hexdigest()[:16]
    prefix = re.sub(r"[^A-Za-z0-9_.-]", "-", value)[:120].rstrip(".-")
    candidate = f"{prefix}-{digest}"
    if not SAFE_ID_RE.fullmatch(candidate):
        raise RunnerError("run_id_invalid")
    return candidate


def build_jobs(
    cases: list[dict[str, Any]],
    options: CampaignOptions,
) -> list[Job]:
    if options.workers != 1:
        raise RunnerError("workers_must_equal_one")
    if options.repeats <= 0:
        raise RunnerError("repeats_invalid")
    if options.scenario not in SCENARIOS:
        raise RunnerError("scenario_invalid")
    if not options.variants or len(set(options.variants)) != len(options.variants):
        raise RunnerError("variants_invalid")
    if not set(options.variants) <= set(VARIANTS):
        raise RunnerError("variants_invalid")
    if not options.languages or len(set(options.languages)) != len(options.languages):
        raise RunnerError("languages_invalid")
    if not set(options.languages) <= set(LANGUAGES):
        raise RunnerError("languages_invalid")
    if options.formal and set(options.variants) != set(VARIANTS):
        raise RunnerError("formal_requires_paired_variants")
    if options.formal and len(options.languages) != 1:
        raise RunnerError("formal_requires_single_language")
    if options.formal and options.scenario != "success":
        raise RunnerError("formal_requires_success_scenario")

    rng = random.Random(options.seed)
    jobs = []
    ordinal = 0
    for case in cases:
        unsupported = set(options.variants) - set(case["variants"])
        if unsupported:
            raise RunnerError("case_variant_unsupported")
        for language in options.languages:
            for repeat in range(1, options.repeats + 1):
                pair_id = _bounded_run_id(
                    f"{case['id']}--{language}--{options.scenario}--r{repeat:03d}"
                )
                variant_order = list(options.variants)
                rng.shuffle(variant_order)
                for variant in variant_order:
                    ordinal += 1
                    run_id = _bounded_run_id(f"{pair_id}--{variant}")
                    jobs.append(Job(
                        case=case,
                        variant=variant,
                        language=language,
                        scenario=options.scenario,
                        repeat=repeat,
                        ordinal=ordinal,
                        run_id=run_id,
                        pair_id=pair_id,
                    ))
    return jobs


def _binary_metadata(binary: Path, dry_run: bool) -> tuple[Path, dict[str, Any]]:
    try:
        resolved = binary.resolve(strict=True)
    except OSError:
        if dry_run:
            return binary, {"executable_verified": False, "sha256": None}
        raise RunnerError("binary_unavailable")
    executable = resolved.is_file() and os.access(resolved, os.X_OK)
    if not executable and not dry_run:
        raise RunnerError("binary_not_executable")
    return resolved, {
        "executable_verified": executable,
        "sha256": _sha256(resolved) if resolved.is_file() else None,
    }


def _prepare_runs_dir(path: Path) -> Path:
    if path.is_symlink():
        raise RunnerError("runs_dir_symlink_forbidden")
    if path.exists():
        if not path.is_dir():
            raise RunnerError("runs_dir_not_directory")
        if any(path.iterdir()):
            raise RunnerError("runs_dir_not_empty")
    else:
        path.mkdir(parents=True, mode=0o700)
    path.chmod(0o700)
    return path.resolve()


def _build_plan(
    options: CampaignOptions,
    jobs: list[Job],
    binary_metadata: dict[str, Any],
) -> dict[str, Any]:
    return {
        "schema_version": 1,
        "runner": "web_browser_toolsearch",
        "surface": "web",
        "model_label": EXACT_MODEL_LABEL,
        "model_id": EXACT_MODEL_ID,
        "models": [{"label": EXACT_MODEL_LABEL, "id": EXACT_MODEL_ID}],
        "request_parameters": {"temperature": "omitted"},
        "workers": 1,
        "formal": options.formal,
        "dry_run": options.dry_run,
        "seed": options.seed,
        "paired": set(options.variants) == set(VARIANTS),
        "variants": list(options.variants),
        "languages": list(options.languages),
        "scenario": options.scenario,
        "browser_contract": {
            "driver_owned_proof_form": True,
            "loopback_only": True,
            "success_preapproval_declared_by_matrix": True,
        },
        "repeats": options.repeats,
        "run_count": len(jobs),
        "binary": binary_metadata,
        "jobs": [job.publication_record() for job in jobs],
    }


def _safe_int(value: Any) -> int:
    return value if isinstance(value, int) and not isinstance(value, bool) and value >= 0 else 0


def _bool_fields(value: Any, names: tuple[str, ...]) -> dict[str, bool]:
    source = value if isinstance(value, dict) else {}
    return {name: source.get(name) is True for name in names}


def _count_fields(value: Any, names: tuple[str, ...]) -> dict[str, int]:
    source = value if isinstance(value, dict) else {}
    return {name: _safe_int(source.get(name)) for name in names}


def _project_driver_record(raw: Any, job: Job) -> dict[str, Any]:
    """Keep a fixed schema even if an injected/future driver returns extras."""

    source = raw if isinstance(raw, dict) else {}
    browser_status = source.get("browser_status")
    browser_status = browser_status if isinstance(browser_status, dict) else {}
    backend = browser_status.get("backend")
    if backend not in {"", "auto", "managed", "extension", "unknown"}:
        backend = "unknown"
    errors = []
    raw_errors = source.get("errors")
    if isinstance(raw_errors, list):
        for value in raw_errors:
            errors.append(value if isinstance(value, str) and SAFE_CODE_RE.fullmatch(value)
                          else "invalid_driver_error")

    evidence_counts = _count_fields(source.get("session_evidence"), (
        "parse_error_count",
        "tool_call_count",
        "browser_call_count",
        "tool_search_call_count",
        "execute_call_count",
        "browser_result_success_count",
        "browser_result_denied_count",
        "browser_result_failed_count",
    ))
    evidence_checks = _bool_fields(source.get("session_evidence"), (
        "source_present",
        "open_call_verified",
        "snapshot_call_verified",
        "fill_call_verified",
        "click_call_verified",
        "read_confirmation_verified",
        "proof_order_verified",
    ))
    return {
        "schema_version": 1,
        "surface": "web_browser",
        "model_id": EXACT_MODEL_ID,
        "variant": job.variant,
        "language": job.language,
        "scenario": job.scenario,
        "request_parameters": {"temperature": "omitted"},
        "health": _bool_fields(source.get("health"), (
            "ready", "auth_required", "model_exact",
        )),
        "auth": _bool_fields(source.get("auth"), (
            "unauthorized_401", "bearer_200",
        )),
        "browser_status": {
            **_bool_fields(browser_status, (
                "available", "enabled", "chrome_found", "extension_online", "dev_mode",
            )),
            "backend": backend or "",
        },
        "chat": {
            **_bool_fields(source.get("chat"), (
                "accepted", "saw_running", "timed_out", "stop_sent",
            )),
            **_count_fields(source.get("chat"), (
                "consecutive_idle_polls",
                "pending_approval_detected",
                "approval_denied",
                "pending_ask_detected",
            )),
        },
        "callback_proof": {
            **_bool_fields(source.get("callback_proof"), (
                "opened", "submitted", "value_matched", "confirmation_served",
            )),
            **_count_fields(source.get("callback_proof"), (
                "open_count", "submit_count", "matching_submit_count", "confirmation_count",
            )),
        },
        "session_evidence": {
            **evidence_counts,
            **evidence_checks,
            "scenario": job.scenario,
        },
        "runtime": _bool_fields(source.get("runtime"), (
            "loopback_only",
            "token_env_only",
            "stdout_discarded",
            "stderr_discarded",
            "process_group_cleanup",
        )),
        "errors": errors,
        "passed": source.get("passed") is True,
    }


def read_usage_metadata(home: Path) -> dict[str, Any]:
    path = home / ".jcode" / "usage" / "events.jsonl"
    totals = {published: 0 for published in USAGE_FIELD_MAP.values()}
    if not path.is_file():
        return {
            "source_present": False,
            "parse_error_count": 0,
            "event_count": 0,
            "totals": totals,
        }
    try:
        raw = path.read_bytes()
    except OSError:
        return {
            "source_present": False,
            "parse_error_count": 1,
            "event_count": 0,
            "totals": totals,
        }
    if len(raw) > MAX_USAGE_BYTES:
        return {
            "source_present": True,
            "parse_error_count": 1,
            "event_count": 0,
            "totals": totals,
        }
    parse_errors = 0
    event_count = 0
    for line in raw.decode("utf-8", errors="replace").splitlines():
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            parse_errors += 1
            continue
        if not isinstance(event, dict):
            parse_errors += 1
            continue
        event_count += 1
        for source, published in USAGE_FIELD_MAP.items():
            value = event.get(source, 0)
            if isinstance(value, int) and not isinstance(value, bool) and value >= 0:
                totals[published] += value
            elif value not in (None, 0):
                parse_errors += 1
    return {
        "source_present": True,
        "parse_error_count": parse_errors,
        "event_count": event_count,
        "totals": totals,
    }


def _safe_cleanup(rundir: Path) -> None:
    resolved_run = rundir.resolve()
    for name in ("home", "work"):
        target = (resolved_run / name).resolve()
        if target.parent != resolved_run or target.name != name:
            raise RunnerError("sandbox_cleanup_scope_invalid")
        shutil.rmtree(target, ignore_errors=True)


def _driver_case_id(job: Job) -> str:
    digest = hashlib.sha256(job.run_id.encode()).hexdigest()[:12]
    return f"web-{job.variant}-{job.language}-{job.scenario}-{job.repeat}-{digest}"


def _validated_session_path(candidate: Any, home: Path) -> Path | None:
    if candidate is None:
        return None
    try:
        path = Path(candidate).resolve(strict=True)
        sessions_root = (home / ".jcode" / "sessions").resolve(strict=True)
    except (OSError, TypeError, ValueError):
        return None
    if not path.is_file() or path.parent != sessions_root:
        return None
    return path


def _routing_verdict(session_path: Path | None, job: Job) -> dict[str, Any]:
    if job.scenario != "success":
        return {
            "passed": True,
            "counts": {},
            "checks": {"scenario_driver_verifier": True},
            "violations": [],
        }
    expectation = job.case.get("expected_routing", {}).get(job.variant)
    if expectation is None:
        private = toolsearch_expect.failure_verdict("missing_variant_expectation")
    elif session_path is None:
        private = toolsearch_expect.failure_verdict("routing_session_count")
    else:
        private = toolsearch_expect.verify_expectation(session_path, expectation)
    return toolsearch_expect.sanitize_external_verdict(private)


def _artifact_paths(provider_config: Path) -> list[str]:
    values = [str(DEFAULT_REAL_HOME.resolve())]
    try:
        values.append(str(provider_config.resolve()))
    except OSError:
        pass
    return values


def run_job(
    job: Job,
    options: CampaignOptions,
    binary: Path,
    *,
    driver_fn: Callable[[web_browser_driver.WebBrowserCase], Any],
) -> dict[str, Any]:
    runs_root = options.runs_dir.resolve()
    rundir = (runs_root / job.run_id).resolve()
    if rundir.parent != runs_root or not SAFE_ID_RE.fullmatch(job.run_id):
        raise RunnerError("run_directory_scope_invalid")
    rundir.mkdir(mode=0o700)
    work = rundir / "work"
    work.mkdir(mode=0o700)
    home = rundir / "home"
    started_at = datetime.now(timezone.utc).isoformat()
    started_monotonic = time.monotonic()
    home_metadata: dict[str, Any] | None = None
    driver_raw: dict[str, Any] = {"errors": ["driver_not_run"], "passed": False}
    session_path: Path | None = None
    routing = toolsearch_expect.sanitize_external_verdict(
        toolsearch_expect.failure_verdict("driver_not_run")
    )
    trajectory = session_extract.extract_trajectory([])
    trajectory["run_id"] = job.run_id
    trajectory["variant"] = job.variant
    usage = {
        "source_present": False,
        "parse_error_count": 0,
        "event_count": 0,
        "totals": {published: 0 for published in USAGE_FIELD_MAP.values()},
    }
    try:
        home_metadata = build_eval_home(
            home,
            provider_config=options.provider_config,
            model_cache=options.model_cache,
            case=job.case,
            variant=job.variant,
            scenario=job.scenario,
            max_iterations=options.max_iterations,
        )
        driver_case = web_browser_driver.WebBrowserCase(
            case_id=_driver_case_id(job),
            binary=binary,
            home=home,
            workdir=work,
            variant=job.variant,
            language=job.language,
            scenario=job.scenario,
            timeout_s=float(job.case["timeout"]),
            startup_timeout_s=options.startup_timeout_s,
            poll_interval_s=options.poll_interval_s,
            request_timeout_s=options.request_timeout_s,
        )
        try:
            result = driver_fn(driver_case)
            publication = result.publication_record()
            driver_raw = publication if isinstance(publication, dict) else {}
            candidate = result.session_path
            session_path = _validated_session_path(candidate, home)
            if candidate is not None and session_path is None:
                errors = driver_raw.get("errors")
                errors = list(errors) if isinstance(errors, list) else []
                driver_raw = {
                    **driver_raw,
                    "errors": [*errors, "session_scope_invalid"],
                    "passed": False,
                }
        except Exception:
            driver_raw = {"errors": ["driver_exception"], "passed": False}
            session_path = None

        routing = _routing_verdict(session_path, job)
        paths = [session_path] if session_path is not None else []
        trajectory = session_extract.extract_trajectory(paths)
        trajectory["run_id"] = job.run_id
        trajectory["variant"] = job.variant
        usage = read_usage_metadata(home)
    finally:
        _safe_cleanup(rundir)

    finished_at = datetime.now(timezone.utc).isoformat()
    elapsed = time.monotonic() - started_monotonic
    driver = _project_driver_record(driver_raw, job)
    usage_reported = (
        usage["source_present"]
        and usage["parse_error_count"] == 0
        and usage["totals"]["total_tokens"] > 0
    )
    session_valid = (
        trajectory["session_count"] == 1
        and trajectory["parse_error_count"] == 0
        and len(trajectory["sessions"]) == 1
        and trajectory["sessions"][0]["source_present"]
    )
    routing_applicable = job.scenario == "success"
    contracts_passed = bool(driver["passed"] and usage_reported and session_valid)
    task_passed = bool(
        contracts_passed and (not routing_applicable or routing["passed"])
    )
    stop_reason = (
        "end_turn"
        if driver["passed"]
        else (driver["errors"][0] if driver["errors"] else "web_driver_failed")
    )
    wall_s = round(max(0.001, elapsed), 3)
    record = {
        "schema_version": 1,
        "runner": "web_browser_toolsearch",
        "run_id": job.run_id,
        "pair_id": job.pair_id,
        "case_id": job.case["id"],
        "category": job.case["category"],
        "tier": job.case["tier"],
        "surface": "web",
        "critical": job.case["critical"],
        "metric_tags": list(job.case["metric_tags"]),
        "model_label": EXACT_MODEL_LABEL,
        "model": EXACT_MODEL_LABEL,
        "model_id": EXACT_MODEL_ID,
        "effort": home_metadata["effort"] if home_metadata else "",
        "request_parameters": {"temperature": "omitted"},
        "variant": job.variant,
        "language": job.language,
        "scenario": job.scenario,
        "repeat": job.repeat,
        "seed": options.seed,
        "started_at": started_at,
        "finished_at": finished_at,
        "monotonic_elapsed_s": round(max(0.0, elapsed), 3),
        "wall_s": wall_s,
        "real_execution": True,
        "browser_fixture": {
            "kind": toolsearch_cases.BROWSER_FIXTURE_KIND,
            "network": "loopback",
            "prompt_owner": "web_browser_driver",
            "contract_matched": True,
            "success_preapproval_declared_by_matrix": True,
        },
        "driver": driver,
        "driver_passed": driver["passed"],
        "routing": routing,
        "routing_passed": routing["passed"],
        "routing_applicable": routing_applicable,
        "usage": usage,
        "usage_reported": usage_reported,
        "session_valid": session_valid,
        "tool_counts": trajectory["tool_counts"],
        "contracts_passed": contracts_passed,
        "error_present": bool(driver["errors"]),
        "stop_reason": stop_reason,
        "task_passed": task_passed,
        "artifact_safe": False,
    }
    session_extract.write_trajectory(rundir / "trajectory.json", trajectory)
    _write_private_json(rundir / "record.json", record)

    secrets = home_metadata["secret_values"] if home_metadata else []
    forbidden_paths = _artifact_paths(options.provider_config)
    redaction = artifact_safety.sanitize_artifacts(
        [rundir], secret_values=secrets, forbidden_paths=forbidden_paths,
    )
    findings = artifact_safety.scan_artifacts(
        [rundir], secret_values=secrets, forbidden_paths=forbidden_paths,
    )
    artifact_safety.write_redaction_report(
        rundir / "redaction_report.json", redaction, findings,
    )
    if findings or any(redaction["replacement_counts"].values()):
        raise RunnerError("artifact_allowlist_violation")

    record["artifact_safe"] = True
    _write_private_json(rundir / "record.json", record)
    final_findings = artifact_safety.scan_artifacts(
        [rundir], secret_values=secrets, forbidden_paths=forbidden_paths,
    )
    if final_findings:
        raise RunnerError("artifact_post_scan_failed")
    return record


def _index_record(record: dict[str, Any]) -> dict[str, Any]:
    return {
        key: record[key]
        for key in (
            "run_id",
            "pair_id",
            "case_id",
            "surface",
            "variant",
            "language",
            "scenario",
            "repeat",
            "model_id",
            "driver_passed",
            "routing_passed",
            "routing_applicable",
            "usage_reported",
            "session_valid",
            "task_passed",
            "contracts_passed",
            "error_present",
            "stop_reason",
            "artifact_safe",
            "wall_s",
            "monotonic_elapsed_s",
        )
    }


def run_campaign(
    options: CampaignOptions,
    *,
    driver_fn: Callable[[web_browser_driver.WebBrowserCase], Any] = (
        web_browser_driver.run_web_browser_case
    ),
) -> dict[str, Any]:
    if EXACT_MODEL_ID != web_browser_driver.EXACT_MODEL_ID:
        raise RunnerError("driver_model_contract_drift")
    cases = load_web_cases(options.matrix, options.base_suite, options.case_ids)
    jobs = build_jobs(cases, options)
    binary, binary_metadata = _binary_metadata(options.binary, options.dry_run)
    runs_dir = _prepare_runs_dir(options.runs_dir)
    normalized = CampaignOptions(**{
        **options.__dict__,
        "binary": binary,
        "runs_dir": runs_dir,
    })
    plan = _build_plan(normalized, jobs, binary_metadata)
    _write_private_json(runs_dir / "plan.json", plan)
    if options.dry_run:
        return {
            "planned": len(jobs),
            "completed": 0,
            "passed": 0,
            "failed": 0,
            "all_passed": True,
            "dry_run": True,
        }

    index = runs_dir / "index.jsonl"
    index.touch(mode=0o600)
    index.chmod(0o600)
    records = []
    for job in jobs:
        record = run_job(job, normalized, binary, driver_fn=driver_fn)
        _append_private_jsonl(index, _index_record(record))
        records.append(record)

    _write_private_json(runs_dir / "all_records.json", records)

    try:
        _selected, secret_values = _load_selected_provider(options.provider_config)
    except RunnerError:
        secret_values = []
    final_findings = artifact_safety.scan_artifacts(
        [runs_dir / "plan.json", index, runs_dir / "all_records.json"],
        secret_values=secret_values,
        forbidden_paths=_artifact_paths(options.provider_config),
    )
    if final_findings:
        raise RunnerError("campaign_index_scan_failed")
    passed = sum(record["task_passed"] for record in records)
    return {
        "planned": len(jobs),
        "completed": len(records),
        "passed": passed,
        "failed": len(records) - passed,
        "all_passed": passed == len(records),
        "dry_run": False,
    }


def _parse_csv(value: str, allowed: tuple[str, ...], label: str) -> tuple[str, ...]:
    items = tuple(item.strip() for item in value.split(",") if item.strip())
    if not items or len(set(items)) != len(items) or not set(items) <= set(allowed):
        raise argparse.ArgumentTypeError(f"invalid {label}")
    return items


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="run Web Browser ToolSearch static/deferred evaluation pairs",
    )
    parser.add_argument("--bin", required=True, type=Path)
    parser.add_argument("--runs-dir", required=True, type=Path)
    parser.add_argument("--matrix", type=Path, default=toolsearch_cases.DEFAULT_MATRIX)
    parser.add_argument("--base-suite", type=Path, default=toolsearch_cases.DEFAULT_BASE_SUITE)
    parser.add_argument("--case", dest="case_ids", action="append", default=[])
    parser.add_argument("--variants", default="static,deferred")
    parser.add_argument("--languages", default="en")
    parser.add_argument("--scenario", choices=SCENARIOS, default="success")
    parser.add_argument("--repeats", type=int, default=1)
    parser.add_argument("--seed", type=int, default=DEFAULT_SEED)
    parser.add_argument("--workers", type=int, default=1)
    parser.add_argument("--formal", action="store_true")
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--provider-config", type=Path, default=DEFAULT_PROVIDER_CONFIG)
    parser.add_argument("--model-cache", type=Path, default=DEFAULT_MODEL_CACHE)
    parser.add_argument("--max-iterations", type=int, default=40)
    parser.add_argument("--startup-timeout", type=float, default=30.0)
    parser.add_argument("--poll-interval", type=float, default=0.25)
    parser.add_argument("--request-timeout", type=float, default=5.0)
    return parser


def _options_from_args(args: argparse.Namespace) -> CampaignOptions:
    try:
        variants = _parse_csv(args.variants, VARIANTS, "variants")
        languages = _parse_csv(args.languages, LANGUAGES, "languages")
    except argparse.ArgumentTypeError as error:
        raise RunnerError("csv_option_invalid") from error
    return CampaignOptions(
        binary=args.bin,
        runs_dir=args.runs_dir,
        matrix=args.matrix,
        base_suite=args.base_suite,
        case_ids=tuple(args.case_ids),
        variants=variants,
        languages=languages,
        scenario=args.scenario,
        repeats=args.repeats,
        seed=args.seed,
        workers=args.workers,
        formal=args.formal,
        dry_run=args.dry_run,
        provider_config=args.provider_config,
        model_cache=args.model_cache,
        max_iterations=args.max_iterations,
        startup_timeout_s=args.startup_timeout,
        poll_interval_s=args.poll_interval,
        request_timeout_s=args.request_timeout,
    )


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        summary = run_campaign(_options_from_args(args))
    except (RunnerError, toolsearch_cases.MatrixError) as error:
        parser.error(str(error))
    print(json.dumps(summary, sort_keys=True))
    return 0 if summary["all_passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
