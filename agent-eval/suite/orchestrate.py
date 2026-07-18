#!/usr/bin/env python3
"""Unattended orchestrator for the jcode autonomous-execution test suite.

For every (case x model x repeat x variant) it:
  1. builds an isolated throwaway HOME containing only the selected provider
     and a pinned model, plus an isolated sandbox cwd seeded with fixtures;
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
import random
import re
import shutil
import subprocess
import sys
import threading
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import verify  # noqa: E402
import routing_verify  # noqa: E402
import artifact_safety  # noqa: E402
import session_extract  # noqa: E402
import toolsearch_cases  # noqa: E402
import toolsearch_expect  # noqa: E402

HERE = Path(__file__).resolve().parent
EVAL_ROOT = HERE.parent
REAL_HOME = Path(os.path.expanduser("~"))
REAL_CFG = REAL_HOME / ".jcode" / "config.json"
REAL_CACHE = REAL_HOME / ".jcode" / "cache" / "models_dev.json"

TERMINAL_STOP = {"end_turn", "max_tokens", "max_turn_requests", "refusal", "cancelled"}
TERMINAL_TOOL_STATUS = {"completed", "failed"}

MODELS = {
    "glm-5.1": {"id": "zhipuai-coding-plan/glm-5.1"},
    "glm-5.2": {"id": "tencent-tokenhub/glm-5.2"},
    "qwen3.5-flash": {"id": "tencent-tokenhub/qwen3.5-flash"},
    "kimi-k2.7-code": {"id": "tencent-tokenhub/kimi-k2.7-code"},
    "kimi-k2.7-code-highspeed": {"id": "tencent-tokenhub/kimi-k2.7-code-highspeed"},
    # This exact provider/model pair is the acceptance target.  It must never
    # silently drift to the high-speed SKU: those are distinct eval subjects.
    "kimi-for-coding": {"id": "kimi-for-coding/kimi-for-coding"},
}

KIMI_ACCEPTANCE_MODEL = "kimi-for-coding/kimi-for-coding"
EVAL_VARIANTS = ("static", "deferred")
DEFAULT_SEED = 20260718
PROTECTED_HOME_CONFIG_KEYS = {
    "providers", "models", "provider", "model", "telemetry", "temperature",
    "tool_search",
}
PROVIDER_RUNTIME_FIELDS = {
    "api_key", "base_url", "headers", "vision", "thinking",
    "reasoning_effort", "custom_models",
}

# repeats[model_label][tier]
DEFAULT_REPEATS = {
    "glm-5.1": {"smoke": 2, "core": 3, "stress": 3, "safety": 2, "frontend": 2, "memory": 2},
    "glm-5.2": {"smoke": 1, "core": 2, "stress": 2, "safety": 1, "frontend": 1, "memory": 1},
    "qwen3.5-flash": {"smoke": 1, "core": 1, "stress": 1, "safety": 1, "frontend": 1, "memory": 1},
    "kimi-k2.7-code": {"smoke": 2, "core": 2, "stress": 2, "safety": 2, "frontend": 1, "memory": 2, "computer": 3},
    "kimi-k2.7-code-highspeed": {"smoke": 20, "core": 2, "stress": 2, "safety": 2, "frontend": 1, "memory": 2, "computer": 60},
    "kimi-for-coding": {"smoke": 2, "core": 3, "stress": 3, "safety": 3, "frontend": 2, "memory": 3, "computer": 5},
}

_print_lock = threading.Lock()

MCP_FIXTURE_TOOL_COUNTS = {10, 30, 50, 100}
MCP_FIXTURE_SERVER_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$")
ACP_SESSION_ID_RE = re.compile(
    r"^sess_([0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-"
    r"[89ab][0-9a-f]{3}-[0-9a-f]{12})$",
    re.IGNORECASE,
)
SAFE_EXEC_PATH = ":".join((
    "/opt/homebrew/bin",
    "/usr/local/bin",
    "/usr/local/go/bin",
    "/System/Cryptexes/App/usr/bin",
    "/usr/bin",
    "/bin",
    "/usr/sbin",
    "/sbin",
    "/Library/Apple/usr/bin",
))


def log(msg):
    with _print_lock:
        print(msg, flush=True)


def resolve_model_id(model_label: str) -> str:
    if model_label not in MODELS:
        raise ValueError(f"unknown model label: {model_label}")
    model_id = MODELS[model_label]["id"]
    if model_label == "kimi-for-coding" and model_id != KIMI_ACCEPTANCE_MODEL:
        raise ValueError("kimi-for-coding must use the exact non-highspeed acceptance model")
    if model_label == "kimi-for-coding" and "highspeed" in model_id.lower():
        raise ValueError("kimi-for-coding highspeed SKU is forbidden")
    return model_id


def _selected_provider_config(cfg: dict, model_id: str):
    provider_id, separator, model_name = model_id.partition("/")
    if not separator or not provider_id or not model_name:
        raise ValueError("model id must be provider/model")
    providers = cfg.get("providers") or cfg.get("models") or {}
    if not isinstance(providers, dict) or not isinstance(providers.get(provider_id), dict):
        raise ValueError(f"selected provider is not configured: {provider_id}")
    source = providers[provider_id]
    selected = {
        key: json.loads(json.dumps(source[key]))
        for key in PROVIDER_RUNTIME_FIELDS
        if key in source
    }
    custom_models = selected.get("custom_models")
    if isinstance(custom_models, list):
        selected["custom_models"] = [
            item for item in custom_models
            if isinstance(item, dict) and item.get("id") == model_name
        ]
        if not selected["custom_models"]:
            selected.pop("custom_models")
    return provider_id, selected


def _runtime_secrets(provider: dict):
    values = []
    api_key = provider.get("api_key")
    if isinstance(api_key, str) and api_key:
        values.append(api_key)
    headers = provider.get("headers")
    if isinstance(headers, dict):
        for value in headers.values():
            # ProviderConfig documents every extra header value as potentially
            # secret; do not guess safety from a vendor-specific header name.
            if isinstance(value, str) and value:
                values.append(value)
    return values


def _write_private_json(path: Path, value):
    payload = json.dumps(value, indent=2) + "\n"
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        stream.write(payload)
    path.chmod(0o600)


def acp_session_file(home_dir: Path, session_id: str) -> Path:
    """Map an ACP ``sess_<uuid>`` ID to its private recorder JSONL file."""
    matched = ACP_SESSION_ID_RE.fullmatch(session_id) if isinstance(session_id, str) else None
    if matched is None:
        raise ValueError("invalid ACP session id")
    sessions_dir = (Path(home_dir) / ".jcode" / "sessions").resolve()
    path = (sessions_dir / f"{matched.group(1).lower()}.json").resolve()
    if path.parent != sessions_dir:
        raise ValueError("ACP session path escaped isolated HOME")
    return path


def build_subprocess_env(home_dir: Path):
    """Return a minimal environment for an untrusted agent subprocess.

    Copying the orchestrator's full environment would expose unrelated API
    tokens, proxy credentials, SSH agent sockets, and host-specific PATH
    entries to the model through the execute tool. Provider authentication is
    supplied only through the owner-only selected-provider config.
    """
    temp_dir = home_dir / "tmp"
    temp_dir.mkdir(parents=True, mode=0o700, exist_ok=True)
    temp_dir.chmod(0o700)
    environment = {
        "HOME": str(home_dir),
        "TMPDIR": str(temp_dir),
        "PATH": SAFE_EXEC_PATH,
        "TERM": "dumb",
    }
    for name in ("LANG", "LC_ALL", "LC_CTYPE"):
        value = os.environ.get(name)
        if value:
            environment[name] = value
    return environment


def build_home(home_dir: Path, model_id: str, max_iter: int,
               home_config: dict | None = None, variant: str = "static"):
    if variant not in EVAL_VARIANTS:
        raise ValueError(f"unsupported eval variant: {variant}")
    overrides = home_config or {}
    protected = sorted(set(overrides) & PROTECTED_HOME_CONFIG_KEYS)
    if protected:
        raise ValueError(f"home_config cannot override protected fields: {protected}")

    jcode_dir = home_dir / ".jcode"
    cache_dir = jcode_dir / "cache"
    cache_dir.mkdir(parents=True, mode=0o700, exist_ok=True)
    jcode_dir.chmod(0o700)
    cache_dir.chmod(0o700)
    cfg = json.loads(REAL_CFG.read_text())
    provider_id, selected_provider = _selected_provider_config(cfg, model_id)
    out = {
        "providers": {provider_id: selected_provider},
        "model": model_id,
        "auto_approve": True,
        "default_mode": "full_access",
        "max_iterations": max_iter,
        "tool_search": {"enabled": variant == "deferred"},
        # Memory is ON (read + online notes) but the offline pipeline is OFF by
        # default so M1 cases don't fire a background distillation run (which
        # would race the oracles and burn real API quota). Pipeline cases turn
        # generate on explicitly via home_config.
        "memory": {"generate": False},
    }
    # shallow-merge case-level config overrides (e.g. {"memory": {"enabled": false}})
    for k, v in overrides.items():
        if k == "memory" and isinstance(v, dict) and isinstance(out.get("memory"), dict):
            out["memory"] = {**out["memory"], **v}
        else:
            out[k] = v
    config_path = jcode_dir / "config.json"
    _write_private_json(config_path, out)
    if REAL_CACHE.exists():
        cache_path = cache_dir / "models_dev.json"
        shutil.copy(REAL_CACHE, cache_path)
        cache_path.chmod(0o600)
    return {
        "provider_id": provider_id,
        "effort": (selected_provider.get("reasoning_effort")
                   if isinstance(selected_provider.get("reasoning_effort"), str)
                   else ""),
        "secret_values": _runtime_secrets(selected_provider),
        "config_path": config_path,
    }


def resolve_project_slug(bin_path: str, home_dir: Path, box: Path) -> str:
    """Ask the jcode binary for the memory project slug of `box`, so python
    never has to replicate the Go slug rule. Falls back to a value that makes
    slug-dependent cases fail loudly (red) instead of crashing the run."""
    env = build_subprocess_env(home_dir)
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


def inject_mcp_fixture(base_home_config: dict | None, case: dict,
                       fixture_path: str | None, rundir: Path):
    """Return (home_config, runtime metadata) for a case-local stdio fixture.

    The case controls only a server label and catalog size. Command, args, and
    log path come from the orchestrator, so a declarative case cannot smuggle
    provider credentials, headers, environment variables, or another command
    into the generated MCP config.
    """
    fixture = case.get("mcp_fixture")
    home_config = json.loads(json.dumps(base_home_config or {}))
    if fixture is None:
        return home_config, None
    if not isinstance(fixture, dict):
        raise ValueError("mcp_fixture must be an object")
    unknown = sorted(set(fixture) - {"server_name", "tool_count"})
    if unknown:
        raise ValueError(f"mcp_fixture has unsupported fields: {unknown}")
    if not fixture_path:
        raise ValueError("case requires mcp_fixture but --mcp-fixture was not provided")

    binary = Path(fixture_path).resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        raise ValueError(f"MCP fixture is not an executable file: {binary}")
    server_name = fixture.get("server_name", "toolsearch_fixture")
    if not isinstance(server_name, str) or not MCP_FIXTURE_SERVER_RE.fullmatch(server_name):
        raise ValueError("mcp_fixture.server_name must be a safe 1-64 character identifier")
    tool_count = fixture.get("tool_count", 10)
    if not isinstance(tool_count, int) or tool_count not in MCP_FIXTURE_TOOL_COUNTS:
        raise ValueError("mcp_fixture.tool_count must be one of 10, 30, 50, 100")

    call_log = (rundir / "mcp_fixture_calls.jsonl").resolve()
    call_log.parent.mkdir(parents=True, exist_ok=True)
    call_log.touch(mode=0o600, exist_ok=True)
    servers = home_config.get("mcp_servers") or {}
    if not isinstance(servers, dict):
        raise ValueError("home_config.mcp_servers must be an object")
    servers = json.loads(json.dumps(servers))
    if server_name in servers:
        raise ValueError(f"mcp_fixture server {server_name!r} collides with home_config")
    server_config = {
        "type": "stdio",
        "command": str(binary),
        "args": ["--count", str(tool_count), "--log", str(call_log)],
    }
    servers[server_name] = server_config
    home_config["mcp_servers"] = servers
    runtime = {
        "server_name": server_name,
        "tool_count": tool_count,
        "call_log": call_log,
        "server_config": server_config,
    }
    return home_config, runtime


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


def _run_id(case, model_label, variant, rep):
    return f"{case['id']}__{model_label}__{variant}__r{rep}"


def run_one(case, model_label, variant, rep, runs_dir, bin_path, harness_path,
            mcp_fixture_path, max_iter, scale_timeout, seed):
    """Run one job and unconditionally destroy the credential-bearing HOME."""
    rundir = runs_dir / _run_id(case, model_label, variant, rep)
    try:
        return _run_one(
            case, model_label, variant, rep, runs_dir, bin_path, harness_path,
            mcp_fixture_path, max_iter, scale_timeout, seed,
        )
    except BaseException:
        _remove_raw_runtime_artifacts(rundir)
        raise
    finally:
        # This also covers setup/oracle/extractor exceptions.  A failed job may
        # lose raw debugging data, but must never strand a provider key on disk.
        shutil.rmtree(rundir / "home", ignore_errors=True)


def _run_one(case, model_label, variant, rep, runs_dir, bin_path, harness_path,
             mcp_fixture_path, max_iter, scale_timeout, seed):
    run_id = _run_id(case, model_label, variant, rep)
    rundir = runs_dir / run_id
    if rundir.exists():
        shutil.rmtree(rundir)
    (rundir / "home").mkdir(parents=True)
    (rundir / "home").chmod(0o700)
    work = rundir / "work"
    box = work / "box"
    box.mkdir(parents=True)

    model_id = resolve_model_id(model_label)
    home_config, mcp_fixture = inject_mcp_fixture(
        case.get("home_config"), case, mcp_fixture_path, rundir,
    )
    home_metadata = build_home(
        rundir / "home", model_id, max_iter, home_config, variant=variant,
    )
    seed_fixtures(box, case.get("fixtures", {}))
    fixture_scope = toolsearch_expect.build_fixture_scope(
        box, case.get("fixtures") or {},
    )
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

    env = build_subprocess_env(rundir / "home")

    # A case is a sequence of steps sharing one HOME + one sandbox. Legacy
    # single-prompt cases are a one-step sequence. Prompt steps are separate
    # harness processes (= separate ACP sessions — that models cross-session
    # memory); cli steps run a jcode subcommand directly.
    steps = case.get("steps") or [{"prompt": case["prompt"]}]
    t0 = time.time()
    harness_rc = None
    result = {}
    step_records = []
    prompt_session_ids = []
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
        session_id = result.get("sessionId")
        if isinstance(session_id, str) and session_id:
            prompt_session_ids.append(session_id)
        step_records.append({"step": i, "kind": "prompt",
                             "stop_reason": result.get("stop_reason"),
                             "tool_calls": result.get("tool_calls", 0),
                             "session_id": session_id,
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
    session_paths = [
        acp_session_file(rundir / "home", session_id)
        for session_id in prompt_session_ids
    ]

    # The declarative ToolSearch expectation is verified against the raw,
    # owner-only session before HOME is removed. Its result is metadata-only and
    # becomes the primary routing verdict used by the acceptance hard gates.
    routing = None
    expected_routing = case.get("expected_routing")
    if isinstance(expected_routing, dict):
        expectation = expected_routing.get(variant)
        if expectation is None:
            routing = toolsearch_expect.failure_verdict("missing_variant_expectation")
        elif len(session_paths) != 1:
            routing = toolsearch_expect.failure_verdict("routing_session_count")
        else:
            routing = toolsearch_expect.verify_expectation(
                session_paths[0], expectation, fixture_scope=fixture_scope,
            )
        ver["oracles"].append({
            "type": "toolsearch_routing",
            "passed": bool(routing.get("passed")),
            "detail": "metadata-only ToolSearch expectation verdict",
        })
        ver["passed"] = bool(ver["passed"] and routing.get("passed"))

    # MCP cases additionally require the deterministic stdio fixture's raw
    # endpoint/argument/result marker to agree with the session. Keep the rich
    # verifier result private and persist only a sanitized projection.
    mcp_routing = None
    if "routing" in case:
        if len(prompt_session_ids) != 1:
            private_mcp_routing = toolsearch_expect.failure_verdict("routing_session_count")
        elif mcp_fixture is None:
            private_mcp_routing = toolsearch_expect.failure_verdict("routing_fixture_missing")
        else:
            private_mcp_routing = routing_verify.verify_routing(
                session_paths[0], mcp_fixture["call_log"], case["routing"],
                require_activation=variant == "deferred",
            )
        mcp_routing = toolsearch_expect.sanitize_external_verdict(private_mcp_routing)
        ver["oracles"].append({
            "type": "mcp_fixture_routing",
            "passed": bool(mcp_routing.get("passed")),
            "detail": "metadata-only MCP fixture/session marker verdict",
        })
        ver["passed"] = bool(ver["passed"] and mcp_routing.get("passed"))
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

    box_listing = []
    for dp, _dn, fn in os.walk(box):
        for f in fn:
            fp = Path(dp) / f
            box_listing.append(str(fp.relative_to(box)))

    fixture_arg_tools = set()
    routing_spec = case.get("routing")
    if isinstance(routing_spec, dict) and isinstance(routing_spec.get("fixture_tools"), dict):
        fixture_arg_tools.update(routing_spec["fixture_tools"])
    trajectory = session_extract.extract_trajectory(
        session_paths, fixture_arg_tools=fixture_arg_tools,
    )
    trajectory["run_id"] = run_id
    trajectory["variant"] = variant
    session_extract.write_trajectory(rundir / "trajectory.json", trajectory)

    safe_steps = []
    for step in step_records:
        safe = {
            key: step[key]
            for key in (
                "step", "kind", "rc", "stop_reason", "tool_calls", "session_id",
            )
            if key in step
        }
        if isinstance(step.get("final_text"), str):
            safe["final_text_chars"] = len(step["final_text"])
        if isinstance(step.get("output_tail"), str):
            safe["output_chars"] = len(step["output_tail"])
        safe_steps.append(safe)

    safe_oracles = [
        {"type": item.get("type"), "passed": bool(item.get("passed"))}
        for item in ver["oracles"]
    ]
    safe_contracts = [
        {"type": item.get("type"), "passed": bool(item.get("passed"))}
        for item in contracts
    ]
    tool_counts = dict(trajectory["tool_counts"])
    tool_counts["declared_deferred"] = len(
        routing_spec.get("deferred_tools", [])
        if isinstance(routing_spec, dict) else []
    )
    tool_counts["mcp_fixture_catalog"] = (
        mcp_fixture["tool_count"] if mcp_fixture else 0
    )

    record = {
        "run_id": run_id,
        "case_id": case["id"],
        "case_title": case["title"],
        "category": case["category"],
        "tier": case["tier"],
        "surface": case.get("surface", "acp"),
        "metric_tags": list(case.get("metric_tags", [])),
        "model": model_label,
        "model_id": model_id,
        "effort": home_metadata["effort"],
        "variant": variant,
        "seed": seed,
        "request_parameters": {"temperature": "omitted"},
        "repeat": rep,
        "steps": safe_steps,
        "task_passed": ver["passed"],
        "oracles": safe_oracles,
        "contracts": safe_contracts,
        "contracts_passed": all(c["passed"] for c in contracts),
        "stop_reason": result.get("stop_reason"),
        "error_present": bool(result.get("error")),
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
        "final_text_chars": len(result.get("final_text", "") or ""),
        "usage_total": usage_tot,
        "event_kinds": kinds,
        "session_update_types": su_types,
        "event_parse_errors": parse_errors,
        "usage_on_acp_stream": usage_on_acp_stream,
        "box_files": box_listing,
        "harness_rc": harness_rc,
        "session_id": result.get("sessionId"),
        "tool_counts": tool_counts,
        "routing": routing,
        "routing_passed": routing.get("passed") if routing else None,
        "mcp_routing": mcp_routing,
        "mcp_routing_passed": mcp_routing.get("passed") if mcp_routing else None,
        "mcp_fixture": ({
            "server_name": mcp_fixture["server_name"],
            "tool_count": mcp_fixture["tool_count"],
            "call_log": mcp_fixture["call_log"].name,
        } if mcp_fixture else None),
        "artifact_safe": True,
    }
    _write_private_json(rundir / "record.json", record)

    # The raw ACP/session/result files served their verifier/extractor purpose.
    # Keep only the metadata trajectory, verdict, and deterministic fixture log.
    _remove_raw_runtime_artifacts(rundir)
    forbidden_paths = [str(REAL_HOME), str(REAL_CFG)]
    redaction = artifact_safety.sanitize_artifacts(
        [rundir],
        secret_values=home_metadata["secret_values"],
        forbidden_paths=forbidden_paths,
    )
    findings = artifact_safety.scan_artifacts(
        [rundir],
        secret_values=home_metadata["secret_values"],
        forbidden_paths=forbidden_paths,
    )
    artifact_safety.write_redaction_report(
        rundir / "redaction_report.json", redaction, findings,
    )
    final_findings = artifact_safety.scan_artifacts(
        [rundir],
        secret_values=home_metadata["secret_values"],
        forbidden_paths=forbidden_paths,
    )
    if findings or final_findings:
        raise RuntimeError("publish artifact safety scan failed")

    status = "PASS" if ver["passed"] else "FAIL"
    cstat = "ok" if record["contracts_passed"] else "CONTRACT!"
    log(f"  [{status}/{cstat}] {run_id}  stop={record['stop_reason']} "
        f"tools={record['tool_calls']} {record['wall_s']}s "
        f"tok={usage_tot['total']}")
    return record


def _remove_raw_runtime_artifacts(rundir: Path):
    shutil.rmtree(rundir / "home", ignore_errors=True)
    shutil.rmtree(rundir / "work", ignore_errors=True)
    for pattern in ("events*.jsonl", "events*.jsonl.stderr", "result*.json"):
        for path in rundir.glob(pattern):
            try:
                path.unlink()
            except FileNotFoundError:
                pass


def parse_variants(raw: str):
    variants = [item.strip() for item in raw.split(",") if item.strip()]
    if not variants:
        raise ValueError("at least one eval variant is required")
    if len(set(variants)) != len(variants):
        raise ValueError("eval variants must not contain duplicates")
    unknown = sorted(set(variants) - set(EVAL_VARIANTS))
    if unknown:
        raise ValueError(f"unsupported eval variants: {unknown}")
    return variants


def build_jobs(cases, models, variants, seed, explicit_repeats=None,
               repeat_scale=1.0, repeats_by_tier=None):
    """Return deterministic paired blocks in randomized execution order.

    Each (case, model, repeat) pair remains adjacent while the order within the
    static/deferred pair is randomized.  Pair blocks are randomized too.  A
    formal workers=1 run therefore executes a deterministic paired sequence.
    """
    repeats_by_tier = repeats_by_tier or DEFAULT_REPEATS
    rng = random.Random(seed)
    blocks = []
    for model_label in models:
        resolve_model_id(model_label)
        for case in cases:
            declared_variants = case.get("variants")
            effective_variants = [
                variant for variant in variants
                if declared_variants is None or variant in declared_variants
            ]
            if not effective_variants:
                continue
            base = (explicit_repeats if explicit_repeats is not None
                    else repeats_by_tier.get(model_label, {}).get(case["tier"], 1))
            count = max(1, int(round(base * repeat_scale)))
            for repeat in range(1, count + 1):
                block = [
                    (case, model_label, variant, repeat)
                    for variant in effective_variants
                ]
                rng.shuffle(block)
                blocks.append(block)
    rng.shuffle(blocks)
    return [job for block in blocks for job in block]


def validate_formal_run(formal, quick, models, variants, repeats,
                        repeat_scale, workers):
    if not formal:
        return
    if quick:
        raise ValueError("--formal cannot be combined with --quick")
    if models != ["kimi-for-coding"]:
        raise ValueError("--formal requires --models kimi-for-coding")
    if set(variants) != set(EVAL_VARIANTS) or len(variants) != len(EVAL_VARIANTS):
        raise ValueError("--formal requires --variants static,deferred")
    if repeats is None:
        raise ValueError("--formal requires explicit --repeats")
    if repeat_scale != 1.0:
        raise ValueError("--formal does not permit --repeat-scale")
    if workers != 1:
        raise ValueError("--formal requires --workers 1")


def select_acp_cases(cases, requested_ids=None, toolsearch_matrix=False):
    """Select cases for this ACP runner and report Web-only hand-offs."""
    requested_ids = requested_ids or []
    by_id = {case.get("id"): case for case in cases}
    unknown = sorted(set(requested_ids) - set(by_id))
    if unknown:
        raise ValueError(f"unknown case ids: {unknown}")
    selected = [by_id[case_id] for case_id in requested_ids] if requested_ids else list(cases)
    if not toolsearch_matrix:
        return selected, []

    web_cases = [case for case in selected if case.get("surface") == "web"]
    if requested_ids and web_cases:
        names = sorted(case["id"] for case in web_cases)
        raise ValueError(
            f"Web-only ToolSearch cases require the Web runner, not ACP: {names}"
        )
    skipped = [
        {
            "case_id": case["id"],
            "surface": "web",
            "reason": "requires_web_runner",
        }
        for case in web_cases
    ]
    return [case for case in selected if case.get("surface") == "acp"], skipped


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--bin", required=True)
    ap.add_argument("--harness", required=True)
    ap.add_argument("--mcp-fixture", default="",
                    help="path to the deterministic stdio MCP fixture binary")
    ap.add_argument("--runs-dir", required=True)
    ap.add_argument("--models", default="glm-5.1,glm-5.2")
    ap.add_argument("--cases", default="", help="comma-separated case ids (default all)")
    ap.add_argument("--tiers", default="", help="comma-separated tiers (default all)")
    ap.add_argument(
        "--toolsearch-matrix", default="",
        help="explicit dedicated ToolSearch matrix JSON (validated before ACP execution)",
    )
    ap.add_argument(
        "--base-suite", default=str(HERE / "testcases.json"),
        help="legacy base suite used to materialize ToolSearch fixture reuse",
    )
    ap.add_argument("--variants", default="static",
                    help="comma-separated static/deferred variants")
    ap.add_argument("--repeats", type=int, default=None,
                    help="explicit repeats per case/model/variant")
    ap.add_argument("--seed", type=int, default=DEFAULT_SEED,
                    help="fixed seed for paired randomized interleaving")
    ap.add_argument("--formal", action="store_true",
                    help="enforce acceptance-run invariants (exact Kimi, paired variants, workers=1)")
    ap.add_argument("--workers", type=int, default=4)
    ap.add_argument("--max-iter", type=int, default=80)
    ap.add_argument("--repeat-scale", type=float, default=1.0)
    ap.add_argument("--timeout-scale", type=float, default=1.0)
    ap.add_argument("--quick", action="store_true", help="1 repeat, glm-5.1 only")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    toolsearch_matrix_mode = bool(args.toolsearch_matrix)
    try:
        if toolsearch_matrix_mode:
            suite = toolsearch_cases.load_suite(args.toolsearch_matrix, args.base_suite)
        else:
            suite = json.loads((HERE / "testcases.json").read_text())
        requested_ids = [item.strip() for item in args.cases.split(",") if item.strip()]
        cases, skipped_cases = select_acp_cases(
            suite["cases"], requested_ids, toolsearch_matrix=toolsearch_matrix_mode,
        )
    except (OSError, json.JSONDecodeError, KeyError, TypeError,
            ValueError, toolsearch_cases.MatrixError) as exc:
        ap.error(str(exc))
    if args.tiers:
        wt = set(args.tiers.split(","))
        cases = [c for c in cases if c["tier"] in wt]

    models = [item.strip() for item in args.models.split(",") if item.strip()]
    try:
        variants = parse_variants(args.variants)
    except ValueError as exc:
        ap.error(str(exc))
    repeats = json.loads(json.dumps(DEFAULT_REPEATS))
    if args.quick:
        models = ["glm-5.1"]
        for m in repeats:
            repeats[m] = {k: 1 for k in repeats[m]}
    if args.repeats is not None and args.repeats <= 0:
        ap.error("--repeats must be positive")
    try:
        validate_formal_run(
            args.formal, args.quick, models, variants, args.repeats,
            args.repeat_scale, args.workers,
        )
    except ValueError as exc:
        ap.error(str(exc))
    try:
        for model_label in models:
            resolve_model_id(model_label)
    except ValueError as exc:
        ap.error(str(exc))

    runs_dir = Path(args.runs_dir).resolve()
    runs_dir.mkdir(parents=True, exist_ok=True)
    bin_path = str(Path(args.bin).resolve())
    harness_path = str(Path(args.harness).resolve())
    mcp_fixture_path = str(Path(args.mcp_fixture).resolve()) if args.mcp_fixture else None

    if any("mcp_fixture" in case for case in cases) and not mcp_fixture_path:
        ap.error("selected cases require --mcp-fixture")
    if mcp_fixture_path and (not Path(mcp_fixture_path).is_file()
                             or not os.access(mcp_fixture_path, os.X_OK)):
        ap.error(f"--mcp-fixture is not executable: {mcp_fixture_path}")

    jobs = build_jobs(
        cases, models, variants, args.seed,
        explicit_repeats=args.repeats,
        repeat_scale=args.repeat_scale,
        repeats_by_tier=repeats,
    )

    plan = {
        "schema_version": 1,
        "seed": args.seed,
        "formal": args.formal,
        "workers": args.workers,
        "models": [
            {"label": model_label, "id": resolve_model_id(model_label)}
            for model_label in models
        ],
        "variants": variants,
        "repeats": args.repeats,
        "suite": "toolsearch" if toolsearch_matrix_mode else "legacy",
        "skipped_cases": skipped_cases,
        "jobs": [
            {
                "run_id": _run_id(case, model_label, variant, repeat),
                "case_id": case["id"],
                "model": model_label,
                "model_id": resolve_model_id(model_label),
                "variant": variant,
                "repeat": repeat,
            }
            for case, model_label, variant, repeat in jobs
        ],
    }
    _write_private_json(runs_dir / "plan.json", plan)

    log(f"== jcode agent eval: {len(jobs)} runs "
        f"({len(cases)} cases x models={models} x variants={variants}) "
        f"workers={args.workers} seed={args.seed} ==")
    for skipped in skipped_cases:
        log(f"  skip {skipped['case_id']}: requires Web runner (ACP has no Browser tools)")
    if args.dry_run:
        for c, m, variant, r in jobs:
            log(f"  plan {c['id']} {m} {variant} r{r}")
        log(f"total {len(jobs)} runs")
        return

    records = []
    index_path = runs_dir / "index.jsonl"
    with index_path.open("w") as idx:
        with cf.ThreadPoolExecutor(max_workers=args.workers) as ex:
            futs = {ex.submit(
                        run_one, c, m, variant, r, runs_dir, bin_path,
                        harness_path, mcp_fixture_path, args.max_iter,
                        args.timeout_scale, args.seed,
                    ): (c, m, variant, r)
                    for (c, m, variant, r) in jobs}
            for fut in cf.as_completed(futs):
                c, m, variant, r = futs[fut]
                try:
                    rec = fut.result()
                except Exception as e:  # noqa: BLE001
                    rec = {
                        "run_id": _run_id(c, m, variant, r),
                        "case_id": c["id"],
                        "model": m,
                        "model_id": resolve_model_id(m),
                        "variant": variant,
                        "seed": args.seed,
                        "tier": c["tier"],
                        "task_passed": False,
                        "stop_reason": "ORCHESTRATOR_ERROR",
                        "error_present": True,
                        "error_type": type(e).__name__,
                    }
                    log(f"  [ERROR] {rec['run_id']}: {type(e).__name__}")
                records.append(rec)
                idx.write(json.dumps({k: rec.get(k) for k in
                          ["run_id", "case_id", "model", "model_id", "effort",
                           "variant", "seed", "tier", "task_passed",
                           "contracts_passed", "stop_reason", "tool_calls",
                           "tool_counts", "wall_s", "routing_passed",
                           "mcp_routing_passed"]}) + "\n")
                idx.flush()
    index_path.chmod(0o600)

    passed = sum(1 for r in records if r.get("task_passed"))
    log(f"== done: {passed}/{len(records)} task-pass ==")
    _write_private_json(runs_dir / "all_records.json", records)


if __name__ == "__main__":
    main()
