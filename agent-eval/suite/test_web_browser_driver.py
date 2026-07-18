import json
import os
import stat
import sys
import tempfile
import unittest
from pathlib import Path


sys.path.insert(0, str(Path(__file__).resolve().parent))

import web_browser_driver as driver


SESSION_ID = "11111111-2222-3333-4444-555555555555"
WEB_TOKEN = "web-eval-token-never-publish"
CONFIG_SECRET = "sk-config-secret-never-publish"
STATUS_SECRET = "/Applications/Private Chrome.app/Contents/MacOS/Chrome"
PROOF_VALUE = "proof-value-never-publish"
RECEIPT = "receipt-never-publish"


class FakeClock:
    def __init__(self):
        self.now = 0.0

    def monotonic(self):
        return self.now

    def sleep(self, seconds):
        self.now += seconds


class FakeProcess:
    def __init__(self):
        self.pid = 9001
        self.cleaned = False
        self.exited = False

    def poll(self):
        return 0 if self.exited else None


class FakeProofServer:
    def __init__(self, expected_value, receipt, snapshot):
        self.expected_value = expected_value
        self.receipt = receipt
        self.base_url = "http://127.0.0.1:43123/"
        self._snapshot = dict(snapshot)
        self.started = False
        self.closed = False

    def start(self):
        self.started = True

    def snapshot(self):
        return dict(self._snapshot)

    def close(self):
        self.closed = True


class FakeAPI:
    def __init__(self, scenario, variant, tasks=None):
        self.scenario = scenario
        self.variant = variant
        self.tasks = list(tasks or [True, False, False])
        self.requests = []
        self.approval_pending_served = False
        self.denied = False
        self.stop_called = False
        self.prompt = ""

    def request(self, method, path, *, authorized, payload=None, timeout=5.0):
        self.requests.append({
            "method": method,
            "path": path,
            "authorized": authorized,
            "payload": payload,
        })
        route = path.split("?", 1)[0]
        if route == "/api/health":
            return 200, {
                "status": "ok",
                "provider": "kimi-for-coding",
                "model": "kimi-for-coding",
                "auth_required": True,
            }
        if route == "/api/status":
            if not authorized:
                return 401, {"error": "unauthorized", "secret": STATUS_SECRET}
            return 200, {
                "provider": "kimi-for-coding",
                "model": "kimi-for-coding",
                "pwd": "/private/operator/workspace",
            }
        if route == "/api/browser/status":
            enabled = self.scenario != "browser_disabled"
            return 200, {
                "available": True,
                "status": {
                    "enabled": enabled,
                    "backend": "managed" if enabled else "",
                    "chrome_found": enabled,
                    "chrome_path": STATUS_SECRET,
                    "chrome_version": "private-version",
                    "extension_online": False,
                    "dev_mode": False,
                },
                "site_permissions": [{"origin": "private-origin"}],
                "approval": {"private": CONFIG_SECRET},
            }
        if route == "/api/chat":
            self.prompt = payload["message"]
            return 202, {"status": "processing", "session_id": SESSION_ID}
        if route == "/api/tasks":
            running = self.tasks.pop(0) if len(self.tasks) > 1 else self.tasks[0]
            return 200, [{
                "uuid": SESSION_ID,
                "running": running,
                "project": "/private/operator/workspace",
            }]
        if route == "/api/approval/pending":
            if self.scenario == "approval_deny" and not self.approval_pending_served:
                self.approval_pending_served = True
                return 200, [{
                    "id": "approval_1",
                    "tool_name": "browser_open",
                    "tool_args": CONFIG_SECRET,
                }]
            return 200, []
        if route == "/api/ask/pending":
            return 200, []
        if route == "/api/approval":
            self.denied = payload == {
                "id": "approval_1",
                "task_id": SESSION_ID,
                "approved": False,
                "approve_all": False,
            }
            return 200, {"status": "ok"}
        if route == "/api/stop":
            self.stop_called = True
            return 200, {"status": "stopped"}
        raise AssertionError(f"unexpected fake route: {route}")


def fake_session_evidence(variant, scenario):
    if scenario == "success":
        return {
            "source_present": True,
            "parse_error_count": 0,
            "tool_call_count": 6,
            "browser_call_count": 5,
            "tool_search_call_count": 1 if variant == "deferred" else 0,
            "execute_call_count": 0,
            "browser_result_success_count": 5,
            "browser_result_denied_count": 0,
            "browser_result_failed_count": 0,
            "open_call_verified": True,
            "snapshot_call_verified": True,
            "fill_call_verified": True,
            "click_call_verified": True,
            "read_confirmation_verified": True,
            "proof_order_verified": True,
            "scenario": scenario,
        }
    if scenario == "approval_deny":
        return {
            "source_present": True,
            "parse_error_count": 0,
            "tool_call_count": 2,
            "browser_call_count": 1,
            "tool_search_call_count": 1 if variant == "deferred" else 0,
            "execute_call_count": 0,
            "browser_result_success_count": 0,
            "browser_result_denied_count": 1,
            "browser_result_failed_count": 0,
            "open_call_verified": True,
            "snapshot_call_verified": False,
            "fill_call_verified": False,
            "click_call_verified": False,
            "read_confirmation_verified": False,
            "proof_order_verified": False,
            "scenario": scenario,
        }
    return {
        "source_present": True,
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
        "scenario": scenario,
    }


class WebBrowserDriverTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.binary = self.root / "fixed-jcode"
        self.binary.write_bytes(b"fake fixed binary")
        self.binary.chmod(0o755)
        self.home = self.root / "home"
        self.work = self.root / "work"
        (self.home / ".jcode").mkdir(parents=True, mode=0o700)
        self.work.mkdir()

    def tearDown(self):
        self.tmp.cleanup()

    def write_config(self, variant="static", scenario="success", model=None):
        browser = {
            "enabled": scenario != "browser_disabled",
            "backend": "managed",
            "headless": True,
            "approval": {
                "navigate": "ask" if scenario == "approval_deny" else "always_allow",
                "interact": "ask" if scenario == "approval_deny" else "always_allow",
            },
            "chrome_path": STATUS_SECRET,
        }
        config = {
            "providers": {
                "kimi-for-coding": {"api_key": CONFIG_SECRET},
            },
            "model": model or driver.EXACT_MODEL_ID,
            "tool_search": {"enabled": variant == "deferred"},
            "browser": browser,
            "auto_approve": scenario != "approval_deny",
            "default_mode": "build" if scenario == "approval_deny" else "full_access",
        }
        path = self.home / ".jcode" / "config.json"
        path.write_text(json.dumps(config))
        path.chmod(0o600)

    def run_fake(
        self,
        *,
        variant="static",
        language="en",
        scenario="success",
        task_states=None,
        callback_snapshot=None,
        timeout_s=10.0,
    ):
        self.write_config(variant, scenario)
        api = FakeAPI(scenario, variant, task_states)
        process = FakeProcess()
        launches = []
        proof_holder = []
        clock = FakeClock()
        callback_snapshot = callback_snapshot or (
            {
                "open_count": 1,
                "submit_count": 1,
                "matching_submit_count": 1,
                "confirmation_count": 1,
            }
            if scenario == "success"
            else {
                "open_count": 0,
                "submit_count": 0,
                "matching_submit_count": 0,
                "confirmation_count": 0,
            }
        )

        def launch(argv, **kwargs):
            launches.append((list(argv), kwargs))
            return process

        def cleanup(value):
            value.cleaned = True

        def callback_factory(expected_value, receipt):
            proof = FakeProofServer(expected_value, receipt, callback_snapshot)
            proof_holder.append(proof)
            return proof

        def session_analyzer(path, target_url, proof_value, receipt, actual_scenario):
            self.assertEqual(PROOF_VALUE, proof_value)
            self.assertEqual(RECEIPT, receipt)
            self.assertEqual(scenario, actual_scenario)
            self.assertEqual("http://127.0.0.1:43123/", target_url)
            evidence = fake_session_evidence(variant, scenario)
            # The driver must enforce its own allowlist even when an injected
            # analyzer accidentally returns raw material.
            evidence["raw_secret"] = CONFIG_SECRET
            evidence["raw_session_path"] = str(path)
            return evidence

        # The production driver checks that the authoritative session exists
        # before handing it to the injected analyzer.
        sessions = self.home / ".jcode" / "sessions"
        sessions.mkdir(mode=0o700)
        session_path = sessions / f"{SESSION_ID}.json"
        session_path.write_text("{}\n")
        session_path.chmod(0o600)

        hooks = driver.DriverHooks(
            api_factory=lambda base_url, token: (
                api
                if base_url == "http://127.0.0.1:41234" and token == WEB_TOKEN
                else (_ for _ in ()).throw(AssertionError("bad api factory args"))
            ),
            process_launcher=launch,
            process_cleanup=cleanup,
            callback_factory=callback_factory,
            reserve_port=lambda: 41234,
            session_analyzer=session_analyzer,
            monotonic=clock.monotonic,
            sleep=clock.sleep,
            token_factory=lambda: WEB_TOKEN,
            proof_value_factory=lambda: PROOF_VALUE,
            receipt_factory=lambda: RECEIPT,
        )
        case = driver.WebBrowserCase(
            case_id=f"browser-{variant}-{language}-{scenario}",
            binary=self.binary,
            home=self.home,
            workdir=self.work,
            variant=variant,
            language=language,
            scenario=scenario,
            timeout_s=timeout_s,
            startup_timeout_s=2,
            poll_interval_s=0.5,
            request_timeout_s=1,
        )
        result = driver.run_web_browser_case(case, hooks)
        return result, api, process, launches, proof_holder, session_path

    def test_success_record_is_sanitized_and_token_is_env_only(self):
        result, api, process, launches, proofs, session_path = self.run_fake()

        self.assertTrue(result.record["passed"], result.record)
        self.assertEqual(session_path.resolve(), result.session_path)
        serialized = json.dumps(result.publication_record(), sort_keys=True)
        for forbidden in (
            WEB_TOKEN,
            CONFIG_SECRET,
            STATUS_SECRET,
            PROOF_VALUE,
            RECEIPT,
            str(session_path),
            "/private/operator/workspace",
            "Authorization",
        ):
            self.assertNotIn(forbidden, serialized)
        self.assertNotIn("chrome_path", serialized)
        self.assertNotIn("chrome_version", serialized)
        self.assertNotIn("site_permissions", serialized)
        self.assertTrue(process.cleaned)
        self.assertTrue(proofs[0].started)
        self.assertTrue(proofs[0].closed)

        argv, kwargs = launches[0]
        self.assertEqual(str(self.binary.resolve()), argv[0])
        self.assertEqual(
            ["web", "--host", "127.0.0.1", "--port", "41234", "--open=false"],
            argv[1:],
        )
        self.assertNotIn(WEB_TOKEN, " ".join(argv))
        self.assertEqual(WEB_TOKEN, kwargs["env"]["JCODE_WEB_TOKEN"])
        self.assertNotIn("--auth-token", argv)
        self.assertNotIn("API_KEY", kwargs["env"])
        self.assertEqual(driver.SAFE_EXEC_PATH, kwargs["env"]["PATH"])
        self.assertEqual(str(self.home / "tmp"), kwargs["env"]["TMPDIR"])
        for host_only in (
            "SSL_CERT_FILE", "SSL_CERT_DIR", "SSH_AUTH_SOCK", "HTTPS_PROXY",
            "HTTP_PROXY", "ALL_PROXY",
        ):
            self.assertNotIn(host_only, kwargs["env"])
        self.assertEqual(0o700, stat.S_IMODE((self.home / "tmp").stat().st_mode))
        self.assertIn("Use only browser_* tools", api.prompt)

    def test_deferred_chinese_prompt_and_tool_search_evidence(self):
        result, api, *_rest = self.run_fake(variant="deferred", language="zh")
        self.assertTrue(result.record["passed"], result.record)
        self.assertEqual(1, result.record["session_evidence"]["tool_search_call_count"])
        self.assertIn("只使用 browser_* 工具", api.prompt)
        self.assertIn("tool_search", api.prompt)
        self.assertNotIn(PROOF_VALUE, json.dumps(result.record))

    def test_deferred_success_fails_without_tool_search_call(self):
        result, _api, *_rest = self.run_fake(variant="deferred")
        result.record["session_evidence"]["tool_search_call_count"] = 0
        self.assertFalse(driver._passed(result.record))

    def test_static_success_fails_with_tool_search_call(self):
        result, _api, *_rest = self.run_fake(variant="static")
        result.record["session_evidence"]["tool_search_call_count"] = 1
        self.assertFalse(driver._passed(result.record))

    def test_success_requires_exactly_one_matching_form_submission(self):
        result, _api, *_rest = self.run_fake(callback_snapshot={
            "open_count": 1,
            "submit_count": 2,
            "matching_submit_count": 1,
            "confirmation_count": 1,
        })
        self.assertFalse(result.record["passed"])
        self.assertEqual(2, result.record["callback_proof"]["submit_count"])

    def test_approval_deny_hook_posts_deny_and_requires_denied_result(self):
        result, api, _process, _launches, _proofs, _session = self.run_fake(
            variant="deferred",
            scenario="approval_deny",
            task_states=[True, True, False, False],
        )
        self.assertTrue(result.record["passed"], result.record)
        self.assertTrue(api.denied)
        self.assertEqual(1, result.record["chat"]["pending_approval_detected"])
        self.assertEqual(1, result.record["chat"]["approval_denied"])
        serialized = json.dumps(result.record)
        self.assertNotIn("approval_1", serialized)
        self.assertNotIn(CONFIG_SECRET, serialized)

    def test_browser_disabled_hook_requires_no_browser_or_callback_activity(self):
        result, _api, *_rest = self.run_fake(
            variant="deferred",
            language="zh",
            scenario="browser_disabled",
        )
        self.assertTrue(result.record["passed"], result.record)
        self.assertFalse(result.record["browser_status"]["enabled"])
        self.assertEqual(0, result.record["session_evidence"]["browser_call_count"])
        self.assertFalse(any(result.record["callback_proof"].values()))

    def test_timeout_sends_stop_and_still_cleans_process(self):
        result, api, process, *_rest = self.run_fake(
            task_states=[True], timeout_s=1.0,
        )
        self.assertFalse(result.record["passed"])
        self.assertEqual(["run_timeout"], result.record["errors"])
        self.assertTrue(result.record["chat"]["timed_out"])
        self.assertTrue(result.record["chat"]["stop_sent"])
        self.assertTrue(api.stop_called)
        self.assertTrue(process.cleaned)

    def test_exact_model_validation_rejects_highspeed_before_launch(self):
        self.write_config(
            model="kimi-for-coding/kimi-for-coding-highspeed",
        )
        launched = []
        case = driver.WebBrowserCase(
            case_id="highspeed-rejected",
            binary=self.binary,
            home=self.home,
            workdir=self.work,
            variant="static",
            language="en",
        )
        result = driver.run_web_browser_case(
            case,
            driver.DriverHooks(process_launcher=lambda *_a, **_k: launched.append(True)),
        )
        self.assertFalse(result.record["passed"])
        self.assertEqual(["highspeed_model_forbidden"], result.record["errors"])
        self.assertFalse(launched)

    def test_callback_fixture_pages_are_pure_html_and_escape_receipt(self):
        form = driver._proof_form_html()
        confirmed = driver._proof_confirmation_html("receipt<script>alert(1)</script>")
        self.assertIn("<form method=\"post\" action=\"/submit\">", form)
        self.assertIn("name=\"proof_value\"", form)
        self.assertNotIn("<script", form.lower())
        self.assertIn("JCODE_BROWSER_CONFIRMATION", confirmed)
        self.assertNotIn("<script", confirmed.lower())
        self.assertIn("&lt;script&gt;", confirmed)

    def test_session_analyzer_uses_raw_proof_but_returns_metadata_only(self):
        path = self.root / "session.json"
        target_url = "http://127.0.0.1:54321/"
        raw_secret = "raw-session-secret-never-publish"
        entries = [
            {"type": "user", "content": raw_secret},
            {
                "type": "tool_call", "name": "tool_search", "tool_call_id": "s1",
                "args": json.dumps({"query": raw_secret}),
            },
            {"type": "tool_result", "name": "tool_search", "tool_call_id": "s1", "output": raw_secret},
            {
                "type": "tool_call", "name": "browser_open", "tool_call_id": "b1",
                "args": json.dumps({"url": target_url}),
            },
            {"type": "tool_result", "name": "browser_open", "tool_call_id": "b1", "output": raw_secret},
            {"type": "tool_call", "name": "browser_snapshot", "tool_call_id": "b2", "args": "{}"},
            {"type": "tool_result", "name": "browser_snapshot", "tool_call_id": "b2", "output": raw_secret},
            {
                "type": "tool_call", "name": "browser_act", "tool_call_id": "b3",
                "args": json.dumps({"action": "fill", "uid": "e1", "value": PROOF_VALUE}),
            },
            {"type": "tool_result", "name": "browser_act", "tool_call_id": "b3", "output": raw_secret},
            {
                "type": "tool_call", "name": "browser_act", "tool_call_id": "b4",
                "args": json.dumps({"action": "click", "uid": "e2"}),
            },
            {"type": "tool_result", "name": "browser_act", "tool_call_id": "b4", "output": raw_secret},
            {"type": "tool_call", "name": "browser_read", "tool_call_id": "b5", "args": "{}"},
            {
                "type": "tool_result", "name": "browser_read", "tool_call_id": "b5",
                "output": f"JCODE_BROWSER_CONFIRMATION {RECEIPT} {raw_secret}",
            },
        ]
        path.write_text("".join(json.dumps(entry) + "\n" for entry in entries))

        evidence = driver.analyze_browser_session(
            path, target_url, PROOF_VALUE, RECEIPT, "success",
        )
        self.assertTrue(evidence["proof_order_verified"])
        self.assertTrue(evidence["read_confirmation_verified"])
        self.assertEqual(5, evidence["browser_call_count"])
        self.assertEqual(1, evidence["tool_search_call_count"])
        serialized = json.dumps(evidence)
        for forbidden in (raw_secret, PROOF_VALUE, RECEIPT, target_url, str(path)):
            self.assertNotIn(forbidden, serialized)

    def test_config_and_stderr_files_remain_private(self):
        result, _api, _process, _launches, _proofs, _session = self.run_fake()
        self.assertTrue(result.record["passed"])
        config_path = self.home / ".jcode" / "config.json"
        self.assertEqual(0o600, stat.S_IMODE(config_path.stat().st_mode))
        runtime_dir = self.work / ".jcode-web-eval"
        self.assertEqual([], list(runtime_dir.glob("stderr-*.log")))
        self.assertEqual(0o700, stat.S_IMODE(runtime_dir.stat().st_mode))

    def test_session_path_maps_public_prefix_and_rejects_invalid_ids(self):
        sessions = self.home / ".jcode" / "sessions"
        expected = (sessions / f"{SESSION_ID}.json").resolve()
        self.assertEqual(expected, driver._session_path(self.home, SESSION_ID))
        self.assertEqual(expected, driver._session_path(self.home, f"sess_{SESSION_ID}"))

        for invalid in (
            "",
            "sess_",
            "session-name",
            "../11111111-2222-3333-4444-555555555555",
            "sess_../11111111-2222-3333-4444-555555555555",
            "sess_11111111-2222-3333-4444-555555555555/../../secret",
            "{11111111-2222-3333-4444-555555555555}",
            "sess_11111111222233334444555555555555",
        ):
            with self.subTest(invalid=invalid):
                with self.assertRaisesRegex(driver.DriverFailure, "session_id_invalid"):
                    driver._session_path(self.home, invalid)


if __name__ == "__main__":
    unittest.main()
