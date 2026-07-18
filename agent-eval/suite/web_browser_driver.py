#!/usr/bin/env python3
"""Drive a real Browser-use turn through jcode's authenticated Web API.

The driver deliberately keeps two representations of a run separate:

* ``DriverResult.record`` is publication-safe, metadata-only evidence.
* ``DriverResult.session_path`` is an internal hand-off for
  ``session_extract``/``routing_verify`` and must never be published directly.

The caller owns the isolated HOME and its minimal provider configuration.  This
module validates that configuration, but never copies it or exposes provider
credentials.  Browser proof is grounded in both an in-memory loopback HTTP
fixture (open/fill/click) and the authoritative private session JSONL
(``browser_read``).
"""

from __future__ import annotations

import copy
import html
import json
import os
import re
import secrets
import signal
import socket
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any, Callable


EXACT_MODEL_ID = "kimi-for-coding/kimi-for-coding"
EXACT_PROVIDER = "kimi-for-coding"
EXACT_MODEL = "kimi-for-coding"
VARIANTS = frozenset({"static", "deferred"})
LANGUAGES = frozenset({"en", "zh"})
SCENARIOS = frozenset({"success", "approval_deny", "browser_disabled"})
SAFE_CASE_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,95}$")
RECORDER_SESSION_ID = re.compile(
    r"^(?:sess_)?(?P<uuid>[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-"
    r"[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$"
)
MAX_API_BODY = 1 << 20
MAX_SESSION_BYTES = 16 << 20
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


class DriverFailure(Exception):
    """A failure represented by a stable, non-sensitive code."""

    def __init__(self, code: str):
        super().__init__(code)
        self.code = code


@dataclass(frozen=True)
class WebBrowserCase:
    """One Web/Browser evaluation case.

    ``home`` must already contain ``.jcode/config.json`` and ``workdir`` is the
    sandbox cwd exposed to the agent.  No model argument exists by design: the
    acceptance target is pinned to :data:`EXACT_MODEL_ID`.
    """

    case_id: str
    binary: Path
    home: Path
    workdir: Path
    variant: str
    language: str
    scenario: str = "success"
    timeout_s: float = 240.0
    startup_timeout_s: float = 30.0
    poll_interval_s: float = 0.25
    request_timeout_s: float = 5.0


@dataclass(frozen=True)
class DriverResult:
    """Sanitized result plus a deliberately non-serialized session hand-off."""

    record: dict[str, Any]
    _session_path: Path | None = field(default=None, repr=False)

    @property
    def session_path(self) -> Path | None:
        """Private raw session path for in-process verifiers/extractors only."""

        return self._session_path

    def publication_record(self) -> dict[str, Any]:
        """Return an independent JSON-safe copy that cannot contain the path."""

        return copy.deepcopy(self.record)


@dataclass
class DriverHooks:
    """Dependency seams used by stdlib tests; production callers omit these."""

    api_factory: Callable[[str, str], Any] | None = None
    process_launcher: Callable[..., Any] | None = None
    process_cleanup: Callable[[Any], None] | None = None
    callback_factory: Callable[[str, str], Any] | None = None
    reserve_port: Callable[[], int] | None = None
    session_analyzer: Callable[[Path, str, str, str, str], dict[str, Any]] | None = None
    monotonic: Callable[[], float] = time.monotonic
    sleep: Callable[[float], None] = time.sleep
    token_factory: Callable[[], str] | None = None
    proof_value_factory: Callable[[], str] | None = None
    receipt_factory: Callable[[], str] | None = None


class _ProofState:
    def __init__(self, expected_value: str, receipt: str):
        self.expected_value = expected_value
        self.receipt = receipt
        self._lock = threading.Lock()
        self.open_count = 0
        self.submit_count = 0
        self.matching_submit_count = 0
        self.confirmation_count = 0

    def opened(self) -> None:
        with self._lock:
            self.open_count += 1

    def submitted(self, value: str) -> bool:
        matched = secrets.compare_digest(value, self.expected_value)
        with self._lock:
            self.submit_count += 1
            if matched:
                self.matching_submit_count += 1
        return matched

    def confirmed(self) -> None:
        with self._lock:
            self.confirmation_count += 1

    def snapshot(self) -> dict[str, int]:
        with self._lock:
            return {
                "open_count": self.open_count,
                "submit_count": self.submit_count,
                "matching_submit_count": self.matching_submit_count,
                "confirmation_count": self.confirmation_count,
            }


def _proof_form_html() -> str:
    return """<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>JCode Browser Eval</title></head>
<body><main><h1>JCode Browser Evaluation</h1>
<form method="post" action="/submit">
<label for="proof-value">Proof value</label>
<input id="proof-value" name="proof_value" type="text" autocomplete="off" required>
<button id="submit-proof" type="submit">Submit proof</button>
</form></main></body></html>"""


def _proof_confirmation_html(receipt: str) -> str:
    escaped = html.escape(receipt, quote=True)
    return (
        "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\">"
        "<title>Confirmed</title></head><body><main><h1>Browser proof confirmed</h1>"
        f"<p id=\"confirmation\">JCODE_BROWSER_CONFIRMATION {escaped}</p>"
        "</main></body></html>"
    )


class ProofHTTPServer:
    """A loopback-only, JavaScript-free HTML form used as browser ground truth."""

    def __init__(self, expected_value: str, receipt: str):
        self.state = _ProofState(expected_value, receipt)
        state = self.state

        class Handler(BaseHTTPRequestHandler):
            server_version = "JCodeEval"
            sys_version = ""

            def log_message(self, _format: str, *_args: Any) -> None:
                return

            def _headers(self, status: int, length: int) -> None:
                self.send_response(status)
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.send_header("Content-Length", str(length))
                self.send_header("Cache-Control", "no-store")
                self.send_header("X-Content-Type-Options", "nosniff")
                self.end_headers()

            def _html(self, status: int, body: str) -> None:
                raw = body.encode("utf-8")
                self._headers(status, len(raw))
                self.wfile.write(raw)

            def do_GET(self) -> None:  # noqa: N802 - stdlib handler API
                parsed = urllib.parse.urlparse(self.path)
                if parsed.path == "/":
                    state.opened()
                    self._html(200, _proof_form_html())
                    return
                if parsed.path == "/confirmed":
                    query = urllib.parse.parse_qs(parsed.query, keep_blank_values=True)
                    supplied = (query.get("receipt") or [""])[0]
                    if not secrets.compare_digest(supplied, state.receipt):
                        self._html(404, "<!doctype html><title>Not found</title>")
                        return
                    state.confirmed()
                    self._html(200, _proof_confirmation_html(state.receipt))
                    return
                self._html(404, "<!doctype html><title>Not found</title>")

            def do_POST(self) -> None:  # noqa: N802 - stdlib handler API
                if urllib.parse.urlparse(self.path).path != "/submit":
                    self._html(404, "<!doctype html><title>Not found</title>")
                    return
                try:
                    length = int(self.headers.get("Content-Length", "0"))
                except ValueError:
                    length = -1
                if length < 0 or length > 4096:
                    self._html(413, "<!doctype html><title>Too large</title>")
                    return
                raw = self.rfile.read(length)
                try:
                    form = urllib.parse.parse_qs(
                        raw.decode("utf-8"), keep_blank_values=True,
                        strict_parsing=True,
                    )
                except (UnicodeDecodeError, ValueError):
                    self._html(400, "<!doctype html><title>Invalid form</title>")
                    return
                value = (form.get("proof_value") or [""])[0]
                if not state.submitted(value):
                    self._html(400, "<!doctype html><title>Proof mismatch</title>")
                    return
                location = "/confirmed?" + urllib.parse.urlencode({"receipt": state.receipt})
                self.send_response(303)
                self.send_header("Location", location)
                self.send_header("Content-Length", "0")
                self.send_header("Cache-Control", "no-store")
                self.end_headers()

        self._server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self._server.daemon_threads = True
        self._thread: threading.Thread | None = None

    @property
    def base_url(self) -> str:
        return f"http://127.0.0.1:{self._server.server_port}/"

    def start(self) -> None:
        if self._thread is not None:
            return
        self._thread = threading.Thread(
            target=self._server.serve_forever,
            name="jcode-browser-eval-proof",
            daemon=True,
        )
        self._thread.start()

    def snapshot(self) -> dict[str, int]:
        return self.state.snapshot()

    def close(self) -> None:
        self._server.shutdown()
        self._server.server_close()
        if self._thread is not None:
            self._thread.join(timeout=3)


class _UrllibAPI:
    def __init__(self, base_url: str, token: str):
        self._base_url = base_url.rstrip("/")
        self._token = token

    def request(
        self,
        method: str,
        path: str,
        *,
        authorized: bool,
        payload: Any = None,
        timeout: float = 5.0,
    ) -> tuple[int, Any]:
        headers = {"Accept": "application/json"}
        data = None
        if payload is not None:
            data = json.dumps(payload, separators=(",", ":")).encode("utf-8")
            headers["Content-Type"] = "application/json"
        if authorized:
            headers["Authorization"] = "Bearer " + self._token
        request = urllib.request.Request(
            self._base_url + path,
            data=data,
            headers=headers,
            method=method,
        )
        try:
            with urllib.request.urlopen(request, timeout=timeout) as response:
                status = response.status
                raw = response.read(MAX_API_BODY + 1)
        except urllib.error.HTTPError as error:
            status = error.code
            raw = error.read(MAX_API_BODY + 1)
        except (OSError, TimeoutError, urllib.error.URLError) as error:
            raise DriverFailure("web_api_unreachable") from error
        if len(raw) > MAX_API_BODY:
            raise DriverFailure("web_api_response_too_large")
        if not raw:
            return status, None
        try:
            return status, json.loads(raw)
        except (UnicodeDecodeError, json.JSONDecodeError) as error:
            raise DriverFailure("web_api_invalid_json") from error


def _reserve_loopback_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def _minimal_child_env(home: Path, token: str) -> dict[str, str]:
    # Match the ACP evaluator's closed environment: no host PATH/TMPDIR, proxy,
    # certificate override, SSH socket, or unrelated token is inherited.  Model
    # authentication lives only in the owner-only selected-provider config.
    temp_dir = home / "tmp"
    temp_dir.mkdir(parents=True, mode=0o700, exist_ok=True)
    temp_dir.chmod(0o700)
    return {
        "HOME": str(home),
        "TMPDIR": str(temp_dir),
        "PATH": SAFE_EXEC_PATH,
        "TERM": "dumb",
        "JCODE_WEB_TOKEN": token,
        "NO_PROXY": "127.0.0.1,localhost",
        "no_proxy": "127.0.0.1,localhost",
    }


def _launch_process(
    argv: list[str],
    *,
    cwd: Path,
    env: dict[str, str],
    stderr_stream: Any,
) -> subprocess.Popen[bytes]:
    return subprocess.Popen(
        argv,
        cwd=str(cwd),
        env=env,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=stderr_stream,
        start_new_session=True,
    )


def _cleanup_process(process: Any) -> None:
    if process is None or process.poll() is not None:
        return
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except (OSError, ProcessLookupError):
        try:
            process.terminate()
        except (OSError, ProcessLookupError):
            return
    try:
        process.wait(timeout=5)
        return
    except subprocess.TimeoutExpired:
        pass
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except (OSError, ProcessLookupError):
        try:
            process.kill()
        except (OSError, ProcessLookupError):
            return
    try:
        process.wait(timeout=3)
    except subprocess.TimeoutExpired:
        return


def _confirmation_text(receipt: str) -> str:
    return "JCODE_BROWSER_CONFIRMATION " + receipt


def _build_prompt(language: str, target_url: str, proof_value: str) -> str:
    if language == "zh":
        return (
            "只使用 browser_* 工具完成下面的真实网页操作；不要使用 execute、curl、"
            "脚本或其他网络客户端。打开这个本机测试页面：\n"
            f"{target_url}\n"
            "先调用 browser_open，再调用 browser_snapshot。把下面的值完整填写到 "
            "Proof value 输入框，点击 Submit proof 按钮，然后调用 browser_read 读取确认页。"
            "只有读到 JCODE_BROWSER_CONFIRMATION 后才结束：\n"
            f"{proof_value}\n"
            "如果工具需要先通过 tool_search 披露，搜索与新工具调用必须分开发送。"
        )
    return (
        "Use only browser_* tools for this real page interaction; do not use execute, "
        "curl, scripts, or any other network client. Open this local test page:\n"
        f"{target_url}\n"
        "Call browser_open first, then browser_snapshot. Fill the Proof value field "
        "with the exact value below, click Submit proof, then call browser_read on the "
        "confirmation page. Finish only after reading JCODE_BROWSER_CONFIRMATION:\n"
        f"{proof_value}\n"
        "If tool_search must disclose the browser tools, search and the newly found "
        "tool call must be sent in separate turns."
    )


def _read_config(case: WebBrowserCase) -> dict[str, Any]:
    path = case.home / ".jcode" / "config.json"
    try:
        raw = path.read_bytes()
    except OSError as error:
        raise DriverFailure("config_unavailable") from error
    if len(raw) > MAX_API_BODY:
        raise DriverFailure("config_too_large")
    try:
        value = json.loads(raw)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise DriverFailure("config_invalid_json") from error
    if not isinstance(value, dict):
        raise DriverFailure("config_invalid_shape")
    return value


def _validate_case(case: WebBrowserCase) -> tuple[Path, dict[str, Any]]:
    if not SAFE_CASE_ID.fullmatch(case.case_id):
        raise DriverFailure("case_id_invalid")
    if case.variant not in VARIANTS:
        raise DriverFailure("variant_invalid")
    if case.language not in LANGUAGES:
        raise DriverFailure("language_invalid")
    if case.scenario not in SCENARIOS:
        raise DriverFailure("scenario_invalid")
    if case.timeout_s <= 0 or case.startup_timeout_s <= 0:
        raise DriverFailure("timeout_invalid")
    if case.poll_interval_s <= 0 or case.request_timeout_s <= 0:
        raise DriverFailure("polling_invalid")
    try:
        binary = case.binary.resolve(strict=True)
    except OSError as error:
        raise DriverFailure("binary_unavailable") from error
    if not binary.is_file() or not os.access(binary, os.X_OK):
        raise DriverFailure("binary_not_executable")
    if not case.home.is_dir() or not case.workdir.is_dir():
        raise DriverFailure("sandbox_unavailable")

    cfg = _read_config(case)
    if "highspeed" in str(cfg.get("model", "")).lower():
        raise DriverFailure("highspeed_model_forbidden")
    if cfg.get("model") != EXACT_MODEL_ID:
        raise DriverFailure("model_not_exact")
    providers = cfg.get("providers")
    if not isinstance(providers, dict) or not isinstance(providers.get(EXACT_PROVIDER), dict):
        raise DriverFailure("provider_not_selected")
    search = cfg.get("tool_search")
    actual_deferred = isinstance(search, dict) and search.get("enabled") is True
    if actual_deferred != (case.variant == "deferred"):
        raise DriverFailure("tool_search_variant_mismatch")

    browser = cfg.get("browser")
    browser = browser if isinstance(browser, dict) else {}
    enabled = browser.get("enabled") is True
    if case.scenario == "browser_disabled":
        if enabled:
            raise DriverFailure("browser_expected_disabled")
    else:
        if not enabled:
            raise DriverFailure("browser_expected_enabled")
        if browser.get("backend") != "managed" or browser.get("headless") is not True:
            raise DriverFailure("browser_not_managed_headless")
    approval = browser.get("approval")
    approval = approval if isinstance(approval, dict) else {}
    if case.scenario == "success":
        if approval.get("navigate") != "always_allow" or approval.get("interact") != "always_allow":
            raise DriverFailure("browser_not_preapproved")
    if case.scenario == "approval_deny":
        if approval.get("navigate") == "always_allow":
            raise DriverFailure("approval_deny_not_enforced")
        if cfg.get("auto_approve") is True or cfg.get("default_mode") == "full_access":
            raise DriverFailure("approval_deny_mode_not_manual")
    return binary, cfg


def _safe_browser_status(payload: Any) -> dict[str, Any]:
    if not isinstance(payload, dict):
        raise DriverFailure("browser_status_invalid")
    status = payload.get("status")
    status = status if isinstance(status, dict) else {}
    backend = status.get("backend")
    if backend not in {None, "", "auto", "managed", "extension"}:
        backend = "unknown"
    return {
        "available": payload.get("available") is True,
        "enabled": status.get("enabled") is True,
        "backend": backend or "",
        "chrome_found": status.get("chrome_found") is True,
        "extension_online": status.get("extension_online") is True,
        "dev_mode": status.get("dev_mode") is True,
    }


def _safe_session_evidence(payload: Any, scenario: str) -> dict[str, Any]:
    if not isinstance(payload, dict):
        raise DriverFailure("session_evidence_invalid")
    integer_fields = {
        "parse_error_count",
        "tool_call_count",
        "browser_call_count",
        "tool_search_call_count",
        "execute_call_count",
        "browser_result_success_count",
        "browser_result_denied_count",
        "browser_result_failed_count",
    }
    boolean_fields = {
        "source_present",
        "open_call_verified",
        "snapshot_call_verified",
        "fill_call_verified",
        "click_call_verified",
        "read_confirmation_verified",
        "proof_order_verified",
    }
    safe: dict[str, Any] = {"scenario": scenario}
    for key in integer_fields:
        value = payload.get(key, 0)
        safe[key] = value if isinstance(value, int) and not isinstance(value, bool) and value >= 0 else 0
    for key in boolean_fields:
        safe[key] = payload.get(key) is True
    return safe


def _validate_proof_url(value: Any) -> str:
    if not isinstance(value, str):
        raise DriverFailure("callback_url_invalid")
    parsed = urllib.parse.urlparse(value)
    if (
        parsed.scheme != "http"
        or parsed.hostname != "127.0.0.1"
        or parsed.username is not None
        or parsed.password is not None
        or parsed.port is None
        or parsed.path != "/"
        or parsed.params
        or parsed.query
        or parsed.fragment
    ):
        raise DriverFailure("callback_not_loopback")
    return value


def _json_args(raw: Any) -> dict[str, Any] | None:
    if isinstance(raw, dict):
        return raw
    if not isinstance(raw, str):
        return None
    try:
        value = json.loads(raw)
    except json.JSONDecodeError:
        return None
    return value if isinstance(value, dict) else None


def analyze_browser_session(
    session_path: Path,
    target_url: str,
    proof_value: str,
    receipt: str,
    scenario: str,
) -> dict[str, Any]:
    """Read raw session evidence and return only bounded routing/proof metadata."""

    try:
        size = session_path.stat().st_size
    except OSError as error:
        raise DriverFailure("session_unavailable") from error
    if size > MAX_SESSION_BYTES:
        raise DriverFailure("session_too_large")
    try:
        lines = session_path.read_text(errors="strict").splitlines()
    except (OSError, UnicodeError) as error:
        raise DriverFailure("session_unreadable") from error

    calls: list[tuple[int, str, str, dict[str, Any] | None]] = []
    results: dict[str, dict[str, Any]] = {}
    parse_errors = 0
    for index, line in enumerate(lines):
        if not line.strip():
            continue
        try:
            entry = json.loads(line)
        except json.JSONDecodeError:
            parse_errors += 1
            continue
        if not isinstance(entry, dict):
            parse_errors += 1
            continue
        if entry.get("type") == "tool_call":
            name = entry.get("name") if isinstance(entry.get("name"), str) else ""
            call_id = entry.get("tool_call_id") if isinstance(entry.get("tool_call_id"), str) else ""
            calls.append((index, name, call_id, _json_args(entry.get("args"))))
        elif entry.get("type") == "tool_result":
            call_id = entry.get("tool_call_id")
            if isinstance(call_id, str) and call_id:
                results[call_id] = entry

    browser_calls = [call for call in calls if call[1].startswith("browser_")]
    search_calls = [call for call in calls if call[1] == "tool_search"]
    disallowed_network_calls = [call for call in calls if call[1] == "execute"]
    positions: dict[str, int] = {}
    successful_results = 0
    denied_results = 0
    failed_results = 0
    read_confirmed = False

    for position, name, call_id, args in browser_calls:
        result = results.get(call_id)
        if result is not None:
            output = result.get("output")
            folded_failure = isinstance(output, str) and any(
                marker in output
                for marker in (
                    "Tool execution failed:",
                    "Tool execution panicked:",
                    "Tool approval error:",
                )
            )
            if result.get("denied"):
                denied_results += 1
            elif result.get("error") or folded_failure:
                failed_results += 1
            else:
                successful_results += 1
        if name == "browser_open" and args and args.get("url") == target_url:
            positions.setdefault("open", position)
        elif name == "browser_snapshot":
            positions.setdefault("snapshot", position)
        elif name == "browser_act" and args:
            if args.get("action") == "fill" and args.get("value") == proof_value:
                positions.setdefault("fill", position)
            elif args.get("action") == "click":
                positions.setdefault("click", position)
        elif name == "browser_read" and result is not None:
            output = result.get("output")
            if isinstance(output, str) and _confirmation_text(receipt) in output:
                positions.setdefault("read", position)
                read_confirmed = True

    order = [positions.get(key) for key in ("open", "snapshot", "fill", "click", "read")]
    ordered_success = all(item is not None for item in order) and order == sorted(order)
    return {
        "source_present": True,
        "parse_error_count": parse_errors,
        "tool_call_count": len(calls),
        "browser_call_count": len(browser_calls),
        "tool_search_call_count": len(search_calls),
        "execute_call_count": len(disallowed_network_calls),
        "browser_result_success_count": successful_results,
        "browser_result_denied_count": denied_results,
        "browser_result_failed_count": failed_results,
        "open_call_verified": "open" in positions,
        "snapshot_call_verified": "snapshot" in positions,
        "fill_call_verified": "fill" in positions,
        "click_call_verified": "click" in positions,
        "read_confirmation_verified": read_confirmed,
        "proof_order_verified": ordered_success,
        "scenario": scenario,
    }


def _session_path(home: Path, session_id: str) -> Path:
    match = RECORDER_SESSION_ID.fullmatch(session_id)
    if match is None:
        raise DriverFailure("session_id_invalid")
    # ACP-style public ids use sess_<uuid>, while the authoritative recorder is
    # always <uuid>.json. Bare canonical UUIDs remain supported for the Web API.
    recorder_uuid = match.group("uuid").lower()
    root = (home / ".jcode" / "sessions").resolve()
    path = (root / f"{recorder_uuid}.json").resolve()
    if path.parent != root:
        raise DriverFailure("session_path_invalid")
    return path


def _task_running(tasks: Any, session_id: str) -> bool | None:
    if not isinstance(tasks, list):
        raise DriverFailure("tasks_response_invalid")
    for item in tasks:
        if isinstance(item, dict) and item.get("uuid") == session_id:
            return item.get("running") is True
    return None


def _pending_ids(payload: Any, kind: str) -> list[str]:
    if not isinstance(payload, list):
        raise DriverFailure(f"pending_{kind}_invalid")
    result = []
    for item in payload:
        if not isinstance(item, dict) or not isinstance(item.get("id"), str):
            raise DriverFailure(f"pending_{kind}_invalid")
        result.append(item["id"])
    return result


def _initial_record(case: WebBrowserCase) -> dict[str, Any]:
    return {
        "schema_version": 1,
        "surface": "web_browser",
        "case_id": case.case_id if SAFE_CASE_ID.fullmatch(case.case_id) else "invalid",
        "model_id": EXACT_MODEL_ID,
        "variant": case.variant if case.variant in VARIANTS else "invalid",
        "language": case.language if case.language in LANGUAGES else "invalid",
        "scenario": case.scenario if case.scenario in SCENARIOS else "invalid",
        "request_parameters": {"temperature": "omitted"},
        "health": {
            "ready": False,
            "auth_required": False,
            "model_exact": False,
        },
        "auth": {"unauthorized_401": False, "bearer_200": False},
        "browser_status": {
            "available": False,
            "enabled": False,
            "backend": "",
            "chrome_found": False,
            "extension_online": False,
            "dev_mode": False,
        },
        "chat": {
            "accepted": False,
            "saw_running": False,
            "consecutive_idle_polls": 0,
            "pending_approval_detected": 0,
            "approval_denied": 0,
            "pending_ask_detected": 0,
            "timed_out": False,
            "stop_sent": False,
        },
        "callback_proof": {
            "opened": False,
            "submitted": False,
            "value_matched": False,
            "confirmation_served": False,
            "open_count": 0,
            "submit_count": 0,
            "matching_submit_count": 0,
            "confirmation_count": 0,
        },
        "session_evidence": {
            "source_present": False,
            "parse_error_count": 0,
            "tool_call_count": 0,
            "browser_call_count": 0,
            "tool_search_call_count": 0,
            "execute_call_count": 0,
            "browser_result_success_count": 0,
            "browser_result_denied_count": 0,
            "browser_result_failed_count": 0,
            "open_call_verified": False,
            "snapshot_call_verified": False,
            "fill_call_verified": False,
            "click_call_verified": False,
            "read_confirmation_verified": False,
            "proof_order_verified": False,
            "scenario": case.scenario if case.scenario in SCENARIOS else "invalid",
        },
        "runtime": {
            "loopback_only": True,
            "token_env_only": True,
            "stdout_discarded": True,
            "stderr_discarded": True,
            "process_group_cleanup": True,
        },
        "errors": [],
        "passed": False,
    }


def _record_callback(record: dict[str, Any], snapshot: dict[str, Any]) -> None:
    counts = {}
    for key in (
        "open_count", "submit_count", "matching_submit_count", "confirmation_count",
    ):
        value = snapshot.get(key, 0)
        counts[key] = value if isinstance(value, int) and not isinstance(value, bool) and value >= 0 else 0
    record["callback_proof"] = {
        "opened": counts["open_count"] > 0,
        "submitted": counts["submit_count"] > 0,
        "value_matched": counts["matching_submit_count"] > 0,
        "confirmation_served": counts["confirmation_count"] > 0,
        **counts,
    }


def _passed(record: dict[str, Any]) -> bool:
    common = (
        not record["errors"]
        and record["health"]["ready"]
        and record["health"]["auth_required"]
        and record["health"]["model_exact"]
        and record["auth"]["unauthorized_401"]
        and record["auth"]["bearer_200"]
        and record["chat"]["accepted"]
        and record["chat"]["saw_running"]
        and record["chat"]["consecutive_idle_polls"] >= 2
        and record["chat"]["pending_ask_detected"] == 0
        and not record["chat"]["timed_out"]
        and record["session_evidence"]["source_present"]
        and record["session_evidence"]["parse_error_count"] == 0
        and record["session_evidence"]["execute_call_count"] == 0
        and (
            record["variant"] != "static"
            or record["session_evidence"]["tool_search_call_count"] == 0
        )
    )
    if not common:
        return False
    scenario = record["scenario"]
    if scenario == "success":
        callback = record["callback_proof"]
        evidence = record["session_evidence"]
        return (
            record["browser_status"]["available"]
            and record["browser_status"]["enabled"]
            and record["browser_status"]["backend"] == "managed"
            and record["browser_status"]["chrome_found"]
            and record["chat"]["pending_approval_detected"] == 0
            and callback["opened"]
            and callback["submitted"]
            and callback["value_matched"]
            and callback["confirmation_served"]
            and callback["submit_count"] == 1
            and callback["matching_submit_count"] == 1
            and evidence["browser_result_failed_count"] == 0
            and evidence["browser_result_denied_count"] == 0
            and evidence["open_call_verified"]
            and evidence["snapshot_call_verified"]
            and evidence["fill_call_verified"]
            and evidence["click_call_verified"]
            and evidence["read_confirmation_verified"]
            and evidence["proof_order_verified"]
            and (
                record["variant"] == "static"
                or evidence["tool_search_call_count"] > 0
            )
        )
    if scenario == "approval_deny":
        return (
            record["browser_status"]["available"]
            and record["browser_status"]["enabled"]
            and record["browser_status"]["backend"] == "managed"
            and record["browser_status"]["chrome_found"]
            and record["chat"]["pending_approval_detected"] > 0
            and record["chat"]["approval_denied"] > 0
            and not record["callback_proof"]["value_matched"]
            and record["session_evidence"]["browser_result_denied_count"] > 0
            and (
                record["variant"] == "static"
                or record["session_evidence"]["tool_search_call_count"] > 0
            )
        )
    if scenario == "browser_disabled":
        return (
            record["browser_status"]["available"]
            and not record["browser_status"]["enabled"]
            and not any(record["callback_proof"].values())
            and record["session_evidence"]["browser_call_count"] == 0
        )
    return False


def run_web_browser_case(
    case: WebBrowserCase,
    hooks: DriverHooks | None = None,
) -> DriverResult:
    """Run one authenticated Web/Browser case and return sanitized evidence."""

    hooks = hooks or DriverHooks()
    api_factory = hooks.api_factory or _UrllibAPI
    process_launcher = hooks.process_launcher or _launch_process
    process_cleanup = hooks.process_cleanup or _cleanup_process
    callback_factory = hooks.callback_factory or ProofHTTPServer
    reserve_port = hooks.reserve_port or _reserve_loopback_port
    session_analyzer = hooks.session_analyzer or analyze_browser_session
    token_factory = hooks.token_factory or (lambda: secrets.token_urlsafe(32))
    proof_value_factory = hooks.proof_value_factory or (
        lambda: "jcode-browser-proof-" + secrets.token_hex(12)
    )
    receipt_factory = hooks.receipt_factory or (
        lambda: "receipt-" + secrets.token_hex(12)
    )

    record = _initial_record(case)
    session_path: Path | None = None
    proof_server: Any = None
    process: Any = None
    stderr_stream: Any = None
    stderr_path: Path | None = None
    try:
        binary, _cfg = _validate_case(case)
        token = token_factory()
        if not isinstance(token, str) or not token or any(char.isspace() for char in token):
            raise DriverFailure("token_factory_invalid")
        proof_value = proof_value_factory()
        receipt = receipt_factory()
        if not isinstance(proof_value, str) or not proof_value:
            raise DriverFailure("proof_value_invalid")
        if not isinstance(receipt, str) or not receipt:
            raise DriverFailure("receipt_invalid")

        proof_server = callback_factory(proof_value, receipt)
        proof_server.start()
        target_url = _validate_proof_url(proof_server.base_url)
        prompt = _build_prompt(case.language, target_url, proof_value)

        web_port = reserve_port()
        if not isinstance(web_port, int) or not (1 <= web_port <= 65535):
            raise DriverFailure("web_port_invalid")
        runtime_dir = case.workdir / ".jcode-web-eval"
        runtime_dir.mkdir(mode=0o700, exist_ok=True)
        runtime_dir.chmod(0o700)
        fd, raw_stderr_path = tempfile.mkstemp(
            prefix="stderr-", suffix=".log", dir=runtime_dir,
        )
        os.fchmod(fd, 0o600)
        stderr_path = Path(raw_stderr_path)
        stderr_stream = os.fdopen(fd, "wb")
        argv = [
            str(binary), "web", "--host", "127.0.0.1",
            "--port", str(web_port), "--open=false",
        ]
        process = process_launcher(
            argv,
            cwd=case.workdir,
            env=_minimal_child_env(case.home, token),
            stderr_stream=stderr_stream,
        )
        api = api_factory(f"http://127.0.0.1:{web_port}", token)

        startup_deadline = hooks.monotonic() + case.startup_timeout_s
        health = None
        while hooks.monotonic() < startup_deadline:
            if process.poll() is not None:
                raise DriverFailure("web_process_exited_early")
            try:
                status, candidate = api.request(
                    "GET", "/api/health", authorized=False,
                    timeout=case.request_timeout_s,
                )
            except DriverFailure as error:
                if error.code != "web_api_unreachable":
                    raise
                hooks.sleep(min(case.poll_interval_s, 0.25))
                continue
            if status == 200 and isinstance(candidate, dict):
                health = candidate
                break
            hooks.sleep(min(case.poll_interval_s, 0.25))
        if health is None:
            raise DriverFailure("web_startup_timeout")
        provider = health.get("provider")
        model = health.get("model")
        model_exact = provider == EXACT_PROVIDER and model == EXACT_MODEL
        record["health"] = {
            "ready": health.get("status") == "ok",
            "auth_required": health.get("auth_required") is True,
            "model_exact": model_exact,
        }
        if not record["health"]["ready"]:
            raise DriverFailure("health_not_ready")
        if not record["health"]["auth_required"]:
            raise DriverFailure("health_auth_not_required")
        if not model_exact:
            raise DriverFailure("health_model_not_exact")

        unauthorized_status, _ = api.request(
            "GET", "/api/status", authorized=False,
            timeout=case.request_timeout_s,
        )
        authorized_status, authorized_payload = api.request(
            "GET", "/api/status", authorized=True,
            timeout=case.request_timeout_s,
        )
        record["auth"] = {
            "unauthorized_401": unauthorized_status == 401,
            "bearer_200": authorized_status == 200,
        }
        if unauthorized_status != 401:
            raise DriverFailure("unauthorized_request_not_rejected")
        if authorized_status != 200 or not isinstance(authorized_payload, dict):
            raise DriverFailure("authorized_request_rejected")
        if (
            authorized_payload.get("provider") != EXACT_PROVIDER
            or authorized_payload.get("model") != EXACT_MODEL
        ):
            raise DriverFailure("authorized_status_model_not_exact")

        browser_code, browser_payload = api.request(
            "GET", "/api/browser/status", authorized=True,
            timeout=case.request_timeout_s,
        )
        if browser_code != 200:
            raise DriverFailure("browser_status_rejected")
        record["browser_status"] = _safe_browser_status(browser_payload)
        expected_enabled = case.scenario != "browser_disabled"
        if record["browser_status"]["enabled"] != expected_enabled:
            raise DriverFailure("browser_status_enabled_mismatch")

        chat_code, chat_payload = api.request(
            "POST", "/api/chat", authorized=True,
            payload={"message": prompt, "mode": "build"},
            timeout=case.request_timeout_s,
        )
        if chat_code != 202 or not isinstance(chat_payload, dict):
            raise DriverFailure("chat_not_accepted")
        session_id = chat_payload.get("session_id")
        if not isinstance(session_id, str) or RECORDER_SESSION_ID.fullmatch(session_id) is None:
            raise DriverFailure("chat_session_id_invalid")
        record["chat"]["accepted"] = chat_payload.get("status") == "processing"
        if not record["chat"]["accepted"]:
            raise DriverFailure("chat_status_invalid")
        session_path = _session_path(case.home, session_id)

        deadline = hooks.monotonic() + case.timeout_s
        idle_polls = 0
        saw_running = False
        denied_ids: set[str] = set()
        terminal_failure: str | None = None
        while hooks.monotonic() < deadline:
            if process.poll() is not None:
                terminal_failure = "web_process_exited_during_run"
                break
            task_code, tasks = api.request(
                "GET", "/api/tasks", authorized=True,
                timeout=case.request_timeout_s,
            )
            if task_code != 200:
                terminal_failure = "tasks_request_failed"
                break
            running = _task_running(tasks, session_id)
            if running is True:
                saw_running = True
                idle_polls = 0
            elif running is False and saw_running:
                idle_polls += 1
            else:
                idle_polls = 0

            approval_code, approvals = api.request(
                "GET",
                "/api/approval/pending?" + urllib.parse.urlencode({"task_id": session_id}),
                authorized=True,
                timeout=case.request_timeout_s,
            )
            ask_code, asks = api.request(
                "GET",
                "/api/ask/pending?" + urllib.parse.urlencode({"task_id": session_id}),
                authorized=True,
                timeout=case.request_timeout_s,
            )
            if approval_code != 200 or ask_code != 200:
                terminal_failure = "pending_request_failed"
                break
            approval_ids = _pending_ids(approvals, "approval")
            ask_ids = _pending_ids(asks, "ask")
            record["chat"]["pending_approval_detected"] += len(
                [item for item in approval_ids if item not in denied_ids]
            )
            if ask_ids:
                record["chat"]["pending_ask_detected"] += len(ask_ids)
                terminal_failure = "pending_ask_detected"
                break
            if approval_ids:
                if case.scenario != "approval_deny":
                    terminal_failure = "pending_approval_detected"
                    break
                for approval_id in approval_ids:
                    if approval_id in denied_ids:
                        continue
                    deny_code, _ = api.request(
                        "POST", "/api/approval", authorized=True,
                        payload={
                            "id": approval_id,
                            "task_id": session_id,
                            "approved": False,
                            "approve_all": False,
                        },
                        timeout=case.request_timeout_s,
                    )
                    if deny_code != 200:
                        terminal_failure = "approval_deny_failed"
                        break
                    denied_ids.add(approval_id)
                    record["chat"]["approval_denied"] += 1
                if terminal_failure:
                    break
            if saw_running and idle_polls >= 2:
                break
            hooks.sleep(case.poll_interval_s)
        else:
            record["chat"]["timed_out"] = True
            terminal_failure = "run_timeout"

        record["chat"]["saw_running"] = saw_running
        record["chat"]["consecutive_idle_polls"] = idle_polls
        if terminal_failure:
            if saw_running and idle_polls < 2:
                stop_code, _ = api.request(
                    "POST", "/api/stop", authorized=True,
                    payload={"task_id": session_id},
                    timeout=case.request_timeout_s,
                )
                record["chat"]["stop_sent"] = stop_code == 200
            raise DriverFailure(terminal_failure)

        if session_path is None or not session_path.is_file():
            raise DriverFailure("session_unavailable")
        record["session_evidence"] = _safe_session_evidence(
            session_analyzer(
                session_path, target_url, proof_value, receipt, case.scenario,
            ),
            case.scenario,
        )
    except DriverFailure as error:
        record["errors"].append(error.code)
    except Exception:
        # No exception detail: HTTP payloads, provider errors, filesystem paths,
        # and process diagnostics are intentionally absent from publish records.
        record["errors"].append("driver_internal_error")
    finally:
        if proof_server is not None:
            try:
                _record_callback(record, proof_server.snapshot())
            except Exception:
                if "callback_snapshot_failed" not in record["errors"]:
                    record["errors"].append("callback_snapshot_failed")
            try:
                proof_server.close()
            except Exception:
                if "callback_cleanup_failed" not in record["errors"]:
                    record["errors"].append("callback_cleanup_failed")
        if process is not None:
            try:
                process_cleanup(process)
            except Exception:
                if "process_cleanup_failed" not in record["errors"]:
                    record["errors"].append("process_cleanup_failed")
        if stderr_stream is not None:
            try:
                stderr_stream.close()
            except OSError:
                pass
        if stderr_path is not None:
            try:
                stderr_path.unlink()
            except FileNotFoundError:
                pass
            except OSError:
                if "stderr_cleanup_failed" not in record["errors"]:
                    record["errors"].append("stderr_cleanup_failed")

    record["passed"] = _passed(record)
    return DriverResult(record=record, _session_path=session_path)


__all__ = [
    "DriverHooks",
    "DriverResult",
    "EXACT_MODEL_ID",
    "ProofHTTPServer",
    "WebBrowserCase",
    "analyze_browser_session",
    "run_web_browser_case",
]
