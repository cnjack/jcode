#!/usr/bin/env python3
"""Coordinate the formal, single-worker ToolSearch acceptance campaign.

The surface-specific runners remain the owners of raw runtime evidence.  This
module builds one pinned set of binaries, interleaves every ACP and Web matrix
job in deterministic adjacent static/deferred blocks, and publishes only the
metadata artifacts consumed by ``analysis/toolsearch_report.py``.

Formal mode is deliberately strict: it requires a clean Git tree, the exact
non-highspeed ``kimi-for-coding/kimi-for-coding`` model, at least ten repeats,
one worker, every matrix case, and a result directory outside the repository.
Canary and dry-run plans are explicitly non-formal and therefore cannot be
mistaken for acceptance evidence.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import random
import re
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Protocol


HERE = Path(__file__).resolve().parent
REPO_DEFAULT = HERE.parents[1]
sys.path.insert(0, str(HERE))

import artifact_safety  # noqa: E402
import orchestrate  # noqa: E402
import toolsearch_cases  # noqa: E402


EXACT_MODEL_LABEL = "kimi-for-coding"
EXACT_MODEL_ID = "kimi-for-coding/kimi-for-coding"
VARIANTS = ("static", "deferred")
MODES = ("formal", "canary", "dry-run")
DEFAULT_SEED = 20260718
MIN_FORMAL_REPEATS = 10
SAFE_RUN_ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,220}$")
COMMIT_RE = re.compile(r"^[0-9a-f]{40}$")
VERSION_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9.+/_-]{0,119}$")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
MAX_CAPTURE_BYTES = 1 << 16
PUBLICATION_FILES = ("record.json", "trajectory.json", "redaction_report.json")
SUITE_HASH_KEYS = ("matrix_sha256", "base_suite_sha256")


class CampaignError(RuntimeError):
    """Stable, non-sensitive campaign failure."""

    def __init__(self, code: str):
        super().__init__(code)
        self.code = code


@dataclass(frozen=True)
class CampaignOptions:
    repo: Path
    runs_dir: Path
    mode: str
    matrix: Path = toolsearch_cases.DEFAULT_MATRIX
    base_suite: Path = toolsearch_cases.DEFAULT_BASE_SUITE
    repeats: int | None = None
    seed: int = DEFAULT_SEED
    case_ids: tuple[str, ...] = ()
    variants: tuple[str, ...] = VARIANTS
    max_iterations: int = 80
    timeout_scale: float = 1.0
    include_supplementary: bool = False


@dataclass(frozen=True)
class CampaignJob:
    case: dict[str, Any]
    variant: str
    repeat: int
    run_id: str
    pair_id: str
    ordinal: int

    @property
    def surface(self) -> str:
        return self.case["surface"]

    def publication_record(self) -> dict[str, Any]:
        return {
            "run_id": self.run_id,
            "case_id": self.case["id"],
            "surface": self.surface,
            "model": EXACT_MODEL_LABEL,
            "model_id": EXACT_MODEL_ID,
            "variant": self.variant,
            "repeat": self.repeat,
        }


@dataclass(frozen=True)
class BuiltBinaries:
    jcode: Path
    harness: Path
    mcp_fixture: Path
    hashes: dict[str, str]


@dataclass(frozen=True)
class Provenance:
    commit: str
    dirty: bool
    go_version: str
    os_arch: str
    eino_version: str


@dataclass(frozen=True)
class SupplementaryWebSpec:
    record_id: str
    variant: str
    language: str
    scenario: str


SUPPLEMENTARY_WEB_SPECS = (
    SupplementaryWebSpec("web_approval_deny_en", "deferred", "en", "approval_deny"),
    SupplementaryWebSpec("web_browser_disabled_en", "deferred", "en", "browser_disabled"),
    SupplementaryWebSpec("web_success_zh_static", "static", "zh", "success"),
    SupplementaryWebSpec("web_success_zh_deferred", "deferred", "zh", "success"),
)

# These IDs are the only accepted deterministic commands.  There is no CLI
# escape hatch for arbitrary argv, package, regex, environment, or working dir.
SUPPLEMENTARY_COMMANDS = {
    "transport_mode_catalogs": (
        "go", "test", "./internal/command", "-count=1", "-run",
        "^(TestBuildCommandToolPlanMatrix|TestCommandToolLifecycleRebuildMatrix)$",
    ),
    "web_mcp_reload_mode_switch": (
        "go", "test", "./internal/web", "-count=1", "-run",
        "^(TestMCPReloadRebuildsEveryLiveTask|TestMCPReloadDoesNotOverwriteConcurrentModeSwitch)$",
    ),
    "deferred_revoke_failure_recovery": (
        "go", "test", "./internal/agent", "-count=1", "-run",
        "^(TestToolSearchLifecycleRevokedEndpointsCannotBeCalledByGuessing|"
        "TestToolSearchLifecycleCompactedHistoryRequiresFreshSelection|"
        "TestApprovalMiddleware_EnhancedNonFatalErrorFolded)$",
    ),
}


class Clock:
    def utc_now(self) -> datetime:
        return datetime.now(timezone.utc)

    def monotonic(self) -> float:
        return time.monotonic()


class JobDispatcher(Protocol):
    def run_matrix_job(self, job: CampaignJob, runs_dir: Path) -> dict[str, Any]: ...

    def run_supplementary_web(
        self,
        spec: SupplementaryWebSpec,
        case: dict[str, Any],
        runs_dir: Path,
    ) -> dict[str, Any]: ...


def _iso_utc(value: datetime) -> str:
    return value.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        while chunk := stream.read(1 << 20):
            digest.update(chunk)
    return digest.hexdigest()


def suite_input_hashes(matrix: Path, base_suite: Path) -> dict[str, str]:
    try:
        hashes = {
            "matrix_sha256": _sha256(matrix),
            "base_suite_sha256": _sha256(base_suite),
        }
    except OSError as error:
        raise CampaignError("suite_input_unavailable") from error
    if set(hashes) != set(SUITE_HASH_KEYS) or any(
        not SHA256_RE.fullmatch(value) for value in hashes.values()
    ):
        raise CampaignError("suite_input_hash_invalid")
    return hashes


def pin_suite_inputs(options: CampaignOptions) -> dict[str, str]:
    selected = suite_input_hashes(options.matrix, options.base_suite)
    if options.mode == "formal":
        canonical = suite_input_hashes(
            toolsearch_cases.DEFAULT_MATRIX.resolve(strict=True),
            toolsearch_cases.DEFAULT_BASE_SUITE.resolve(strict=True),
        )
        if selected != canonical:
            raise CampaignError("formal_suite_inputs_drifted")
    return selected


def verify_suite_inputs(
    options: CampaignOptions,
    expected: dict[str, str],
) -> None:
    if suite_input_hashes(options.matrix, options.base_suite) != expected:
        raise CampaignError("suite_inputs_changed_during_campaign")
    if options.mode == "formal":
        canonical = suite_input_hashes(
            toolsearch_cases.DEFAULT_MATRIX.resolve(strict=True),
            toolsearch_cases.DEFAULT_BASE_SUITE.resolve(strict=True),
        )
        if canonical != expected:
            raise CampaignError("suite_inputs_changed_during_campaign")


def _value_sha256(value: Any) -> str:
    payload = json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(payload).hexdigest()


def _write_private_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    path.parent.chmod(0o700)
    payload = json.dumps(value, indent=2, sort_keys=True) + "\n"
    temporary = path.with_name(path.name + ".tmp")
    try:
        descriptor = os.open(
            temporary,
            os.O_WRONLY | os.O_CREAT | os.O_EXCL,
            0o600,
        )
        with os.fdopen(descriptor, "w") as stream:
            stream.write(payload)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        path.chmod(0o600)
    finally:
        try:
            temporary.unlink()
        except FileNotFoundError:
            pass


def _append_private_jsonl(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        stream.write(json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n")
        stream.flush()
        os.fsync(stream.fileno())
    path.chmod(0o600)


def _is_within(path: Path, parent: Path) -> bool:
    try:
        path.relative_to(parent)
        return True
    except ValueError:
        return False


def prepare_runs_dir(raw_path: Path, repo: Path) -> Path:
    if not raw_path.is_absolute():
        raise CampaignError("runs_dir_must_be_absolute")
    if raw_path.is_symlink():
        raise CampaignError("runs_dir_symlink_forbidden")
    resolved_repo = repo.resolve(strict=True)
    candidate = raw_path.resolve(strict=False)
    if candidate == Path("/") or _is_within(candidate, resolved_repo):
        raise CampaignError("runs_dir_must_be_outside_repo")
    if candidate.exists():
        if not candidate.is_dir():
            raise CampaignError("runs_dir_not_directory")
        if any(candidate.iterdir()):
            raise CampaignError("runs_dir_not_empty")
    else:
        candidate.mkdir(parents=True, mode=0o700)
    candidate.chmod(0o700)
    return candidate


def _captured_command(
    command_runner: Callable[..., subprocess.CompletedProcess[str]],
    argv: tuple[str, ...],
    cwd: Path,
) -> str:
    result = command_runner(
        list(argv), cwd=str(cwd), text=True, capture_output=True, check=False,
    )
    if result.returncode != 0:
        raise CampaignError("provenance_command_failed")
    output = result.stdout or ""
    if len(output.encode(errors="ignore")) > MAX_CAPTURE_BYTES:
        raise CampaignError("provenance_output_too_large")
    return output.strip()


def collect_provenance(
    repo: Path,
    command_runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
) -> Provenance:
    commit = _captured_command(
        command_runner, ("git", "rev-parse", "HEAD"), repo,
    )
    if not COMMIT_RE.fullmatch(commit):
        raise CampaignError("git_commit_invalid")
    status = _captured_command(
        command_runner,
        ("git", "status", "--porcelain=v1", "--untracked-files=all"),
        repo,
    )
    go_version = _captured_command(
        command_runner, ("go", "env", "GOVERSION"), repo,
    )
    os_arch_parts = _captured_command(
        command_runner, ("go", "env", "GOOS", "GOARCH"), repo,
    ).splitlines()
    eino_version = _captured_command(
        command_runner,
        ("go", "list", "-m", "-f={{.Version}}", "github.com/cloudwego/eino"),
        repo,
    )
    os_arch = "/".join(os_arch_parts)
    if len(os_arch_parts) != 2 or any(
        not VERSION_RE.fullmatch(value) for value in (go_version, os_arch, eino_version)
    ):
        raise CampaignError("environment_provenance_invalid")
    return Provenance(
        commit=commit,
        dirty=bool(status),
        go_version=go_version,
        os_arch=os_arch,
        eino_version=eino_version,
    )


def _run_build_command(
    command_runner: Callable[..., subprocess.CompletedProcess[str]],
    argv: tuple[str, ...],
    cwd: Path,
    failure_code: str,
) -> None:
    result = command_runner(
        list(argv),
        cwd=str(cwd),
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=False,
    )
    if result.returncode != 0:
        raise CampaignError(failure_code)


def build_binaries(
    repo: Path,
    build_dir: Path,
    command_runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
) -> BuiltBinaries:
    build_dir.mkdir(parents=True, mode=0o700, exist_ok=False)
    build_dir.chmod(0o700)
    targets = {
        "jcode": build_dir / "jcode",
        "harness": build_dir / "acp-harness",
        "mcp_fixture": build_dir / "mcp-fixture",
    }
    commands = (
        (
            ("go", "build", "-trimpath", "-o", str(targets["jcode"]), "./cmd/jcode/"),
            repo,
            "jcode_build_failed",
        ),
        (
            ("go", "build", "-trimpath", "-o", str(targets["harness"]), "."),
            repo / "agent-eval" / "harness",
            "harness_build_failed",
        ),
        (
            (
                "go", "build", "-trimpath", "-o", str(targets["mcp_fixture"]),
                "./agent-eval/fixture/mcp",
            ),
            repo,
            "mcp_fixture_build_failed",
        ),
    )
    for argv, cwd, failure_code in commands:
        _run_build_command(command_runner, argv, cwd, failure_code)
    for path in targets.values():
        if not path.is_file() or path.stat().st_size <= 0:
            raise CampaignError("built_binary_missing")
        path.chmod(0o700)
        if not os.access(path, os.X_OK):
            raise CampaignError("built_binary_not_executable")
    hashes = {
        "jcode_sha256": _sha256(targets["jcode"]),
        "harness_sha256": _sha256(targets["harness"]),
        "mcp_fixture_sha256": _sha256(targets["mcp_fixture"]),
    }
    if any(not SHA256_RE.fullmatch(value) for value in hashes.values()):
        raise CampaignError("binary_hash_invalid")
    return BuiltBinaries(
        jcode=targets["jcode"],
        harness=targets["harness"],
        mcp_fixture=targets["mcp_fixture"],
        hashes=hashes,
    )


def verify_binary_hashes(binaries: BuiltBinaries) -> None:
    paths = {
        "jcode_sha256": binaries.jcode,
        "harness_sha256": binaries.harness,
        "mcp_fixture_sha256": binaries.mcp_fixture,
    }
    if set(binaries.hashes) != set(paths):
        raise CampaignError("binary_hash_contract_invalid")
    for name, path in paths.items():
        try:
            valid = (
                not path.is_symlink()
                and path.is_file()
                and path.stat().st_size > 0
                and os.access(path, os.X_OK)
                and _sha256(path) == binaries.hashes[name]
            )
        except OSError:
            valid = False
        if not valid:
            raise CampaignError("binary_hash_changed_during_campaign")


def normalize_options(options: CampaignOptions) -> CampaignOptions:
    if options.mode not in MODES:
        raise CampaignError("mode_invalid")
    repeats = options.repeats
    if repeats is None:
        repeats = MIN_FORMAL_REPEATS if options.mode == "formal" else 1
    if not isinstance(repeats, int) or isinstance(repeats, bool) or repeats <= 0:
        raise CampaignError("repeats_invalid")
    if options.mode == "formal" and repeats < MIN_FORMAL_REPEATS:
        raise CampaignError("formal_repeats_below_ten")
    if options.mode == "formal" and options.case_ids:
        raise CampaignError("formal_case_filter_forbidden")
    if not options.variants or len(set(options.variants)) != len(options.variants):
        raise CampaignError("variants_invalid")
    if not set(options.variants) <= set(VARIANTS):
        raise CampaignError("variants_invalid")
    if options.mode == "formal" and set(options.variants) != set(VARIANTS):
        raise CampaignError("formal_requires_both_variants")
    if options.max_iterations <= 0 or options.timeout_scale <= 0:
        raise CampaignError("runtime_limit_invalid")
    include_supplementary = options.include_supplementary or options.mode == "formal"
    return CampaignOptions(
        **{
            **options.__dict__,
            "repo": options.repo.resolve(strict=True),
            "matrix": options.matrix.resolve(strict=True),
            "base_suite": options.base_suite.resolve(strict=True),
            "repeats": repeats,
            "include_supplementary": include_supplementary,
        }
    )


def select_cases(suite: dict[str, Any], options: CampaignOptions) -> list[dict[str, Any]]:
    cases = list(suite["cases"])
    by_id = {case["id"]: case for case in cases}
    if len(set(options.case_ids)) != len(options.case_ids):
        raise CampaignError("duplicate_case_filter")
    if options.case_ids:
        unknown = set(options.case_ids) - set(by_id)
        if unknown:
            raise CampaignError("unknown_case_filter")
        cases = [by_id[case_id] for case_id in options.case_ids]
    if not cases:
        raise CampaignError("no_cases_selected")
    return cases


def build_jobs(
    cases: list[dict[str, Any]],
    repeats: int,
    seed: int,
    variants: tuple[str, ...] = VARIANTS,
) -> list[CampaignJob]:
    rng = random.Random(seed)
    blocks: list[list[tuple[dict[str, Any], str, int, str]]] = []
    for case in cases:
        declared = set(case["variants"])
        effective = [variant for variant in variants if variant in declared]
        if not effective:
            continue
        for repeat in range(1, repeats + 1):
            pair_id = f"{case['id']}__{EXACT_MODEL_LABEL}__r{repeat}"
            block = [(case, variant, repeat, pair_id) for variant in effective]
            rng.shuffle(block)
            blocks.append(block)
    rng.shuffle(blocks)
    jobs = []
    for block in blocks:
        for case, variant, repeat, pair_id in block:
            run_id = f"{case['id']}__{EXACT_MODEL_LABEL}__{variant}__r{repeat}"
            if not SAFE_RUN_ID_RE.fullmatch(run_id):
                raise CampaignError("run_id_invalid")
            jobs.append(CampaignJob(
                case=case,
                variant=variant,
                repeat=repeat,
                run_id=run_id,
                pair_id=pair_id,
                ordinal=len(jobs) + 1,
            ))
    if not jobs or len({job.run_id for job in jobs}) != len(jobs):
        raise CampaignError("job_plan_invalid")
    return jobs


def build_plan(
    options: CampaignOptions,
    jobs: list[CampaignJob],
    suite_hashes: dict[str, str],
) -> dict[str, Any]:
    formal = options.mode == "formal"
    return {
        "schema_version": 1,
        "suite": "toolsearch",
        "mode": options.mode,
        "formal": formal,
        "dry_run": options.mode == "dry-run",
        "seed": options.seed,
        "workers": 1,
        "models": [{"label": EXACT_MODEL_LABEL, "id": EXACT_MODEL_ID}],
        "variants": list(options.variants),
        "repeats": options.repeats,
        "request_parameters": {"temperature": "omitted"},
        "suite_inputs": dict(suite_hashes),
        "supplementary_planned": options.include_supplementary,
        "jobs": [job.publication_record() for job in jobs],
    }


def _load_json(path: Path, code: str) -> dict[str, Any]:
    try:
        raw = path.read_bytes()
        value = json.loads(raw)
    except (OSError, json.JSONDecodeError, UnicodeDecodeError) as error:
        raise CampaignError(code) from error
    if not isinstance(value, dict):
        raise CampaignError(code)
    return value


def _validate_tool_counts(counts: Any) -> None:
    if not isinstance(counts, dict):
        raise CampaignError("tool_counts_invalid")
    for name in (
        "calls_total", "results_total", "model_requests", "first_visible",
        "max_visible", "first_schema_tokens_estimate", "max_schema_tokens_estimate",
    ):
        if not isinstance(counts.get(name), int) or isinstance(counts.get(name), bool):
            raise CampaignError("tool_counts_invalid")
    if not isinstance(counts.get("calls_by_name"), dict):
        raise CampaignError("tool_counts_invalid")
    if not isinstance(counts.get("results_by_status"), dict):
        raise CampaignError("tool_counts_invalid")


def _sanitize_then_scan(
    paths: list[Path],
    secret_values: tuple[str, ...],
    forbidden_paths: tuple[str, ...],
) -> None:
    redaction = artifact_safety.sanitize_artifacts(
        paths, secret_values=secret_values, forbidden_paths=forbidden_paths,
    )
    findings = artifact_safety.scan_artifacts(
        paths, secret_values=secret_values, forbidden_paths=forbidden_paths,
    )
    if findings or any(redaction["replacement_counts"].values()):
        raise CampaignError("coordinator_artifact_scan_failed")


def validate_run_artifacts(
    job: CampaignJob,
    runs_dir: Path,
    record: dict[str, Any],
    secret_values: tuple[str, ...],
    forbidden_paths: tuple[str, ...],
) -> None:
    rundir = runs_dir / job.run_id
    if rundir.is_symlink() or not rundir.is_dir() or rundir.resolve().parent != runs_dir:
        raise CampaignError("run_directory_invalid")
    rundir.chmod(0o700)
    for name in PUBLICATION_FILES:
        path = rundir / name
        if path.is_symlink() or not path.is_file():
            raise CampaignError("publication_artifact_missing")
        path.chmod(0o600)

    stored = _load_json(rundir / "record.json", "record_invalid")
    trajectory = _load_json(rundir / "trajectory.json", "trajectory_invalid")
    redaction = _load_json(rundir / "redaction_report.json", "redaction_invalid")
    identity_ok = (
        stored.get("run_id") == job.run_id
        and stored.get("case_id") == job.case["id"]
        and stored.get("surface") == job.surface
        and stored.get("variant") == job.variant
        and stored.get("repeat") == job.repeat
        and stored.get("model") == EXACT_MODEL_LABEL
        and stored.get("model_id") == EXACT_MODEL_ID
        and stored.get("request_parameters") == {"temperature": "omitted"}
    )
    if not identity_ok or stored != record:
        raise CampaignError("record_identity_invalid")
    for name in ("task_passed", "contracts_passed", "error_present", "artifact_safe"):
        if not isinstance(stored.get(name), bool):
            raise CampaignError("record_contract_invalid")
    if stored["artifact_safe"] is not True:
        raise CampaignError("record_not_artifact_safe")
    if not isinstance(stored.get("stop_reason"), str) or not stored["stop_reason"]:
        raise CampaignError("record_contract_invalid")
    wall = stored.get("wall_s")
    if (not isinstance(wall, (int, float)) or isinstance(wall, bool) or wall <= 0):
        raise CampaignError("record_contract_invalid")
    _validate_tool_counts(stored.get("tool_counts"))

    if (
        trajectory.get("schema_version") != 1
        or trajectory.get("payload_policy")
        != "metadata_only_except_declared_fixture_args"
        or trajectory.get("run_id") != job.run_id
        or trajectory.get("variant") != job.variant
        or trajectory.get("parse_error_count") != 0
        or not isinstance(trajectory.get("sessions"), list)
        or not trajectory["sessions"]
    ):
        raise CampaignError("trajectory_contract_invalid")
    _validate_tool_counts(trajectory.get("tool_counts"))
    for session in trajectory["sessions"]:
        if (
            not isinstance(session, dict)
            or session.get("source_present") is not True
            or session.get("parse_error_lines") != []
            or not isinstance(session.get("entries"), list)
        ):
            raise CampaignError("trajectory_contract_invalid")
    if redaction.get("schema_version") != 1 or redaction.get("safe") is not True:
        raise CampaignError("redaction_contract_invalid")
    if redaction.get("post_redaction_findings") != []:
        raise CampaignError("redaction_contract_invalid")
    _sanitize_then_scan([rundir], secret_values, forbidden_paths)


def _index_record(record: dict[str, Any]) -> dict[str, Any]:
    return {
        key: record.get(key)
        for key in (
            "run_id", "case_id", "surface", "model", "model_id", "variant",
            "repeat", "task_passed", "contracts_passed", "stop_reason", "wall_s",
            "routing_passed", "artifact_safe",
        )
    }


def load_runtime_secrets() -> tuple[str, ...]:
    try:
        config = json.loads(orchestrate.REAL_CFG.read_text())
        _provider_id, provider = orchestrate._selected_provider_config(config, EXACT_MODEL_ID)
        return tuple(orchestrate._runtime_secrets(provider))
    except (OSError, json.JSONDecodeError, TypeError, ValueError) as error:
        raise CampaignError("exact_provider_credentials_unavailable") from error


class DefaultDispatcher:
    """Surface adapter that calls each runner's single-job API directly."""

    def __init__(
        self,
        options: CampaignOptions,
        binaries: BuiltBinaries,
    ) -> None:
        self.options = options
        self.binaries = binaries

    def run_matrix_job(self, job: CampaignJob, runs_dir: Path) -> dict[str, Any]:
        if job.surface == "acp":
            return orchestrate.run_one(
                job.case,
                EXACT_MODEL_LABEL,
                job.variant,
                job.repeat,
                runs_dir,
                str(self.binaries.jcode),
                str(self.binaries.harness),
                str(self.binaries.mcp_fixture),
                self.options.max_iterations,
                self.options.timeout_scale,
                self.options.seed,
            )
        if job.surface != "web":
            raise CampaignError("surface_not_supported")
        import web_browser_driver
        import web_toolsearch_orchestrate as web_runner

        web_options = web_runner.CampaignOptions(
            binary=self.binaries.jcode,
            runs_dir=runs_dir,
            matrix=self.options.matrix,
            base_suite=self.options.base_suite,
            case_ids=(job.case["id"],),
            variants=(job.variant,),
            languages=("en",),
            scenario="success",
            repeats=1,
            seed=self.options.seed,
            workers=1,
            formal=False,
            max_iterations=self.options.max_iterations,
        )
        web_job = web_runner.Job(
            case=job.case,
            variant=job.variant,
            language="en",
            scenario="success",
            repeat=job.repeat,
            ordinal=job.ordinal,
            run_id=job.run_id,
            pair_id=job.pair_id,
        )
        return web_runner.run_job(
            web_job,
            web_options,
            self.binaries.jcode,
            driver_fn=web_browser_driver.run_web_browser_case,
        )

    def run_supplementary_web(
        self,
        spec: SupplementaryWebSpec,
        case: dict[str, Any],
        runs_dir: Path,
    ) -> dict[str, Any]:
        import web_browser_driver
        import web_toolsearch_orchestrate as web_runner

        run_id = f"supp__{spec.record_id}"
        web_options = web_runner.CampaignOptions(
            binary=self.binaries.jcode,
            runs_dir=runs_dir,
            matrix=self.options.matrix,
            base_suite=self.options.base_suite,
            case_ids=(case["id"],),
            variants=(spec.variant,),
            languages=(spec.language,),
            scenario=spec.scenario,
            repeats=1,
            seed=self.options.seed,
            workers=1,
            formal=False,
            max_iterations=self.options.max_iterations,
        )
        job = web_runner.Job(
            case=case,
            variant=spec.variant,
            language=spec.language,
            scenario=spec.scenario,
            repeat=1,
            ordinal=1,
            run_id=run_id,
            pair_id=f"supp__{spec.record_id}",
        )
        return web_runner.run_job(
            job,
            web_options,
            self.binaries.jcode,
            driver_fn=web_browser_driver.run_web_browser_case,
        )


def _run_supplementary_command(
    command_id: str,
    argv: tuple[str, ...],
    repo: Path,
    command_runner: Callable[..., subprocess.CompletedProcess[str]],
    clock: Clock,
) -> dict[str, Any]:
    started_utc = clock.utc_now()
    started_mono = clock.monotonic()
    result = command_runner(
        list(argv),
        cwd=str(repo),
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=False,
    )
    finished_mono = clock.monotonic()
    finished_utc = clock.utc_now()
    return {
        "kind": "deterministic_command",
        "record_id": command_id,
        "argv_sha256": _value_sha256(list(argv)),
        "started_at": _iso_utc(started_utc),
        "finished_at": _iso_utc(finished_utc),
        "wall_s": round(max(0.0, finished_mono - started_mono), 3),
        "exit_code": result.returncode,
        "passed": result.returncode == 0,
        "real_execution": True,
        "counts_toward_active_duration": False,
    }


def run_supplementary_coverage(
    dispatcher: JobDispatcher,
    web_case: dict[str, Any],
    runs_dir: Path,
    repo: Path,
    command_runner: Callable[..., subprocess.CompletedProcess[str]],
    clock: Clock,
    secret_values: tuple[str, ...],
    forbidden_paths: tuple[str, ...],
    records: list[dict[str, Any]] | None = None,
) -> list[dict[str, Any]]:
    records = records if records is not None else []
    for command_id, argv in SUPPLEMENTARY_COMMANDS.items():
        record = _run_supplementary_command(
            command_id, argv, repo, command_runner, clock,
        )
        records.append(record)
        if not record["passed"]:
            raise CampaignError("supplementary_command_failed")

    web_root = runs_dir / "supplementary"
    web_root.mkdir(mode=0o700)
    for spec in SUPPLEMENTARY_WEB_SPECS:
        started_utc = clock.utc_now()
        started_mono = clock.monotonic()
        run_id = f"supp__{spec.record_id}"
        run_dir = web_root / run_id
        try:
            record = dispatcher.run_supplementary_web(spec, web_case, web_root)
            synthetic_job = CampaignJob(
                case=web_case,
                variant=spec.variant,
                repeat=1,
                run_id=run_id,
                pair_id=run_id,
                ordinal=1,
            )
            validate_run_artifacts(
                synthetic_job, web_root, record, secret_values, forbidden_paths,
            )
            routing_applicable = spec.scenario == "success"
            identity_matches = (
                record.get("language") == spec.language
                and record.get("scenario") == spec.scenario
                and record.get("routing_applicable") is routing_applicable
                and record.get("real_execution") is True
            )
            expected_pass = bool(
                identity_matches
                and record.get("driver_passed")
                and record.get("artifact_safe")
                and record.get("task_passed")
                and record.get("contracts_passed")
                and record.get("error_present") is False
            )
            finished_mono = clock.monotonic()
            finished_utc = clock.utc_now()
            projected = {
                "kind": "web_browser_canary",
                "record_id": spec.record_id,
                "scenario": spec.scenario,
                "language": spec.language,
                "variant": spec.variant,
                "record_sha256": _sha256(run_dir / "record.json"),
                "started_at": _iso_utc(started_utc),
                "finished_at": _iso_utc(finished_utc),
                "wall_s": round(max(0.0, finished_mono - started_mono), 3),
                "driver_passed": record.get("driver_passed") is True,
                "routing_applicable": record.get("routing_applicable") is True,
                "task_passed": record.get("task_passed") is True,
                "artifact_safe": record.get("artifact_safe") is True,
                "identity_matches": identity_matches,
                "passed": expected_pass,
                "real_execution": True,
                "counts_toward_active_duration": False,
            }
            records.append(projected)
            if not projected["passed"]:
                raise CampaignError("supplementary_web_failed")
        except BaseException:
            if not records or records[-1].get("record_id") != spec.record_id:
                finished_mono = clock.monotonic()
                finished_utc = clock.utc_now()
                records.append({
                    "kind": "web_browser_canary",
                    "record_id": spec.record_id,
                    "scenario": spec.scenario,
                    "language": spec.language,
                    "variant": spec.variant,
                    "started_at": _iso_utc(started_utc),
                    "finished_at": _iso_utc(finished_utc),
                    "wall_s": round(max(0.0, finished_mono - started_mono), 3),
                    "passed": False,
                    "real_execution": True,
                    "counts_toward_active_duration": False,
                })
            raise
    return records


def _campaign_manifest(
    *,
    status: str,
    options: CampaignOptions,
    provenance: Provenance,
    binary_hashes: dict[str, str],
    suite_hashes: dict[str, str],
    started_at: datetime,
    finished_at: datetime,
    monotonic_elapsed_s: float,
    planned_count: int,
    completed_count: int,
    intervals: list[dict[str, Any]],
    supplementary_records: list[dict[str, Any]],
    failure_code: str | None = None,
) -> dict[str, Any]:
    manifest = {
        "schema_version": 1,
        "status": status,
        "formal": options.mode == "formal",
        "mode": options.mode,
        "started_at": _iso_utc(started_at),
        "finished_at": _iso_utc(finished_at),
        "monotonic_elapsed_s": round(max(0.0, monotonic_elapsed_s), 3),
        "planned_run_count": planned_count,
        "completed_run_count": completed_count,
        "workers": 1,
        "model_label": EXACT_MODEL_LABEL,
        "model_id": EXACT_MODEL_ID,
        "request_parameters": {"temperature": "omitted"},
        "git": {"commit": provenance.commit, "dirty": provenance.dirty},
        "binaries": binary_hashes,
        "suite_inputs": suite_hashes,
        "environment": {
            "go_version": provenance.go_version,
            "os_arch": provenance.os_arch,
            "eino_version": provenance.eino_version,
        },
        "run_intervals": intervals,
        "supplementary_records": supplementary_records,
        "supplementary_counts_toward_active_duration": False,
    }
    if failure_code is not None:
        manifest["failure_code"] = failure_code
    return manifest


def run_campaign(
    raw_options: CampaignOptions,
    *,
    command_runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
    dispatcher_factory: Callable[
        [CampaignOptions, BuiltBinaries], JobDispatcher
    ] = DefaultDispatcher,
    clock: Clock | None = None,
    secret_values: tuple[str, ...] | None = None,
) -> dict[str, Any]:
    clock = clock or Clock()
    options = normalize_options(raw_options)
    suite_hashes = pin_suite_inputs(options)
    runs_dir = prepare_runs_dir(options.runs_dir, options.repo)
    suite = toolsearch_cases.load_suite(options.matrix, options.base_suite)
    cases = select_cases(suite, options)
    jobs = build_jobs(cases, options.repeats or 1, options.seed, options.variants)
    plan = build_plan(options, jobs, suite_hashes)
    _write_private_json(runs_dir / "plan.json", plan)
    if options.mode == "dry-run":
        return {
            "mode": "dry-run",
            "formal": False,
            "planned": len(jobs),
            "completed": 0,
            "plan": plan,
        }

    provenance = collect_provenance(options.repo, command_runner)
    if options.mode == "formal" and provenance.dirty:
        raise CampaignError("formal_requires_clean_git_tree")
    secrets = secret_values if secret_values is not None else load_runtime_secrets()
    forbidden = tuple({
        str(orchestrate.REAL_HOME.resolve()),
        str(orchestrate.REAL_CFG.resolve()),
        str(options.repo),
        str(runs_dir),
    })
    started_at = clock.utc_now()
    started_mono = clock.monotonic()
    intervals: list[dict[str, Any]] = []
    records: list[dict[str, Any]] = []
    supplementary_records: list[dict[str, Any]] = []
    binary_hashes: dict[str, str] = {}
    failure_code: str | None = None

    try:
        with tempfile.TemporaryDirectory(
            prefix="jcode-toolsearch-binaries-", dir=str(runs_dir.parent),
        ) as raw_build_dir:
            build_dir = Path(raw_build_dir)
            build_dir.chmod(0o700)
            binaries = build_binaries(
                options.repo, build_dir / "bin", command_runner,
            )
            binary_hashes = binaries.hashes
            verify_binary_hashes(binaries)
            forbidden = tuple({*forbidden, str(build_dir)})
            dispatcher = dispatcher_factory(options, binaries)

            if options.include_supplementary:
                web_cases = [case for case in suite["cases"] if case["surface"] == "web"]
                if len(web_cases) != 1:
                    raise CampaignError("supplementary_web_case_count_invalid")
                run_supplementary_coverage(
                    dispatcher,
                    web_cases[0],
                    runs_dir,
                    options.repo,
                    command_runner,
                    clock,
                    secrets,
                    forbidden,
                    records=supplementary_records,
                )

            index_path = runs_dir / "index.jsonl"
            for job in jobs:
                interval_start_utc = clock.utc_now()
                interval_start_mono = clock.monotonic()
                successful = False
                try:
                    record = dispatcher.run_matrix_job(job, runs_dir)
                    validate_run_artifacts(
                        job, runs_dir, record, secrets, forbidden,
                    )
                    records.append(record)
                    _append_private_jsonl(index_path, _index_record(record))
                    _write_private_json(runs_dir / "all_records.json", records)
                    successful = True
                finally:
                    interval_finish_mono = clock.monotonic()
                    interval_finish_utc = clock.utc_now()
                    intervals.append({
                        "run_id": job.run_id,
                        "started_at": _iso_utc(interval_start_utc),
                        "finished_at": _iso_utc(interval_finish_utc),
                        "monotonic_elapsed_s": round(
                            max(0.0, interval_finish_mono - interval_start_mono), 3,
                        ),
                        "real_execution": True,
                        "successful": successful,
                    })
                    progress = _campaign_manifest(
                        status="running" if successful else "failed",
                        options=options,
                        provenance=provenance,
                        binary_hashes=binary_hashes,
                        suite_hashes=suite_hashes,
                        started_at=started_at,
                        finished_at=interval_finish_utc,
                        monotonic_elapsed_s=interval_finish_mono - started_mono,
                        planned_count=len(jobs),
                        completed_count=len(records),
                        intervals=intervals,
                        supplementary_records=supplementary_records,
                        failure_code=None if successful else "matrix_job_failed",
                    )
                    _write_private_json(runs_dir / "campaign.json", progress)

            verify_binary_hashes(binaries)
            verify_suite_inputs(options, suite_hashes)
            post = collect_provenance(options.repo, command_runner)
            if post.commit != provenance.commit or post.dirty != provenance.dirty:
                raise CampaignError("git_provenance_changed_during_campaign")
            _sanitize_then_scan(
                [runs_dir], secrets, forbidden,
            )
    except KeyboardInterrupt:
        failure_code = "interrupted"
        raise
    except CampaignError as error:
        failure_code = error.code
        raise
    except BaseException:
        failure_code = "unexpected_failure"
        raise
    finally:
        if options.mode != "dry-run":
            finished_at = clock.utc_now()
            finished_mono = clock.monotonic()
            complete = len(records) == len(jobs) and failure_code is None
            status = (
                "complete" if complete and options.mode == "formal"
                else "canary_complete" if complete
                else "failed"
            )
            manifest = _campaign_manifest(
                status=status,
                options=options,
                provenance=provenance,
                binary_hashes=binary_hashes,
                suite_hashes=suite_hashes,
                started_at=started_at,
                finished_at=finished_at,
                monotonic_elapsed_s=finished_mono - started_mono,
                planned_count=len(jobs),
                completed_count=len(records),
                intervals=intervals,
                supplementary_records=supplementary_records,
                failure_code=failure_code,
            )
            _write_private_json(runs_dir / "campaign.json", manifest)

    return {
        "mode": options.mode,
        "formal": options.mode == "formal",
        "planned": len(jobs),
        "completed": len(records),
        "task_passed": sum(record.get("task_passed") is True for record in records),
        "campaign_status": "complete" if options.mode == "formal" else "canary_complete",
        "supplementary_completed": len(supplementary_records),
    }


def _parse_csv(value: str, allowed: tuple[str, ...], code: str) -> tuple[str, ...]:
    values = tuple(item.strip() for item in value.split(",") if item.strip())
    if not values or len(set(values)) != len(values) or not set(values) <= set(allowed):
        raise argparse.ArgumentTypeError(code)
    return values


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="coordinate the exact-Kimi ACP+Web ToolSearch campaign",
    )
    parser.add_argument("--repo", type=Path, default=REPO_DEFAULT)
    parser.add_argument("--runs-dir", type=Path, required=True)
    parser.add_argument("--matrix", type=Path, default=toolsearch_cases.DEFAULT_MATRIX)
    parser.add_argument("--base-suite", type=Path, default=toolsearch_cases.DEFAULT_BASE_SUITE)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--formal", action="store_true")
    mode.add_argument("--canary", action="store_true")
    mode.add_argument("--dry-run", action="store_true")
    parser.add_argument("--repeats", type=int)
    parser.add_argument("--seed", type=int, default=DEFAULT_SEED)
    parser.add_argument("--case", dest="case_ids", action="append", default=[])
    parser.add_argument("--variants", default="static,deferred")
    parser.add_argument("--max-iterations", type=int, default=80)
    parser.add_argument("--timeout-scale", type=float, default=1.0)
    parser.add_argument(
        "--with-supplementary",
        action="store_true",
        help="also run fixed negative/zh Browser and deterministic coverage (automatic in formal)",
    )
    return parser


def _options_from_args(args: argparse.Namespace) -> CampaignOptions:
    mode = "formal" if args.formal else "canary" if args.canary else "dry-run"
    variants = _parse_csv(args.variants, VARIANTS, "variants_invalid")
    return CampaignOptions(
        repo=args.repo,
        runs_dir=args.runs_dir,
        mode=mode,
        matrix=args.matrix,
        base_suite=args.base_suite,
        repeats=args.repeats,
        seed=args.seed,
        case_ids=tuple(args.case_ids),
        variants=variants,
        max_iterations=args.max_iterations,
        timeout_scale=args.timeout_scale,
        include_supplementary=args.with_supplementary,
    )


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    try:
        options = _options_from_args(parser.parse_args(argv))
        summary = run_campaign(options)
    except KeyboardInterrupt:
        print(json.dumps(
            {"status": "failed", "failure_code": "interrupted"}, sort_keys=True,
        ))
        return 130
    except (CampaignError, toolsearch_cases.MatrixError) as error:
        code = error.code if isinstance(error, CampaignError) else "matrix_invalid"
        print(json.dumps({"status": "failed", "failure_code": code}, sort_keys=True))
        return 2
    except Exception:
        # Surface-specific runners intentionally keep rich diagnostics private.
        # The CLI must not turn an unexpected exception into a host-path or
        # credential-bearing traceback in an unattended campaign log.
        print(json.dumps(
            {"status": "failed", "failure_code": "unexpected_failure"}, sort_keys=True,
        ))
        return 2
    print(json.dumps(summary, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
