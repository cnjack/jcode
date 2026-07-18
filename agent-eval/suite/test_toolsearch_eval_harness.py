import json
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock


sys.path.insert(0, str(Path(__file__).resolve().parent))

import artifact_safety
import orchestrate
import session_extract


SELECTED_SECRET = "sk-selected-provider-secret-123456"
OTHER_SECRET = "sk-other-provider-secret-987654"


class ToolSearchEvalHarnessTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def source_config(self):
        path = self.root / "source-config.json"
        path.write_text(json.dumps({
            "providers": {
                "kimi-for-coding": {
                    "api_key": SELECTED_SECRET,
                    "base_url": "https://example.invalid/v1",
                    "reasoning_effort": "high",
                    "temperature": 0.2,
                    "unrelated": "must-not-copy",
                },
                "another-provider": {
                    "api_key": OTHER_SECRET,
                    "base_url": "https://other.invalid/v1",
                },
            },
            "telemetry": {"langfuse": {"secret": "must-not-copy"}},
        }))
        return path

    def test_kimi_acceptance_mapping_is_exact_and_rejects_highspeed_drift(self):
        self.assertEqual(
            "kimi-for-coding/kimi-for-coding",
            orchestrate.resolve_model_id("kimi-for-coding"),
        )
        with mock.patch.dict(
            orchestrate.MODELS,
            {"kimi-for-coding": {"id": "kimi-coding/kimi-for-coding-highspeed"}},
        ):
            with self.assertRaisesRegex(ValueError, "non-highspeed|highspeed"):
                orchestrate.resolve_model_id("kimi-for-coding")

    def test_minimal_home_is_private_selected_only_and_has_no_temperature(self):
        home = self.root / "home"
        home.mkdir(mode=0o700)
        with (
            mock.patch.object(orchestrate, "REAL_CFG", self.source_config()),
            mock.patch.object(orchestrate, "REAL_CACHE", self.root / "missing-cache"),
        ):
            metadata = orchestrate.build_home(
                home,
                orchestrate.KIMI_ACCEPTANCE_MODEL,
                50,
                {"memory": {"enabled": False}},
                variant="deferred",
            )

        config_path = home / ".jcode" / "config.json"
        config = json.loads(config_path.read_text())
        self.assertEqual(0o600, stat.S_IMODE(config_path.stat().st_mode))
        self.assertEqual(0o700, stat.S_IMODE((home / ".jcode").stat().st_mode))
        self.assertEqual(["kimi-for-coding"], sorted(config["providers"]))
        selected = config["providers"]["kimi-for-coding"]
        self.assertNotIn("temperature", selected)
        self.assertNotIn("unrelated", selected)
        self.assertNotIn("telemetry", config)
        self.assertNotIn("models", config)
        self.assertNotIn("model_state", json.dumps(config))
        self.assertEqual({"enabled": True}, config["tool_search"])
        self.assertEqual("high", metadata["effort"])
        self.assertIn(SELECTED_SECRET, metadata["secret_values"])
        self.assertNotIn(OTHER_SECRET, metadata["secret_values"])
        self.assertNotIn("temperature", json.dumps(config).lower())

    def test_home_config_cannot_reintroduce_request_or_provider_fields(self):
        home = self.root / "home"
        home.mkdir(mode=0o700)
        with (
            mock.patch.object(orchestrate, "REAL_CFG", self.source_config()),
            mock.patch.object(orchestrate, "REAL_CACHE", self.root / "missing-cache"),
        ):
            for key in ("temperature", "providers", "model", "telemetry", "tool_search"):
                with self.subTest(key=key):
                    with self.assertRaisesRegex(ValueError, "protected fields"):
                        orchestrate.build_home(
                            home, orchestrate.KIMI_ACCEPTANCE_MODEL, 50,
                            {key: "forbidden"}, variant="static",
                        )

    def test_subprocess_environment_drops_host_credentials_and_sockets(self):
        home = self.root / "isolated-home"
        home.mkdir(mode=0o700)
        inherited = {
            "PATH": "/Users/operator/private/bin:/usr/bin",
            "OPENAI_API_KEY": "credential-canary",
            "HTTPS_PROXY": "https://user:password@proxy.invalid",
            "SSH_AUTH_SOCK": "/private/ssh-agent.sock",
            "JCODE_WEB_TOKEN": "web-token-canary",
            "LANG": "en_US.UTF-8",
        }
        with mock.patch.dict(orchestrate.os.environ, inherited, clear=True):
            environment = orchestrate.build_subprocess_env(home)

        self.assertEqual(str(home), environment["HOME"])
        self.assertEqual(str(home / "tmp"), environment["TMPDIR"])
        self.assertEqual(orchestrate.SAFE_EXEC_PATH, environment["PATH"])
        self.assertEqual("en_US.UTF-8", environment["LANG"])
        for name in (
            "OPENAI_API_KEY", "HTTPS_PROXY", "SSH_AUTH_SOCK", "JCODE_WEB_TOKEN",
        ):
            self.assertNotIn(name, environment)
        self.assertNotIn("/Users/operator", json.dumps(environment))
        self.assertEqual(0o700, stat.S_IMODE((home / "tmp").stat().st_mode))

    def test_run_wrapper_removes_config_even_when_body_raises(self):
        case = {"id": "case"}
        runs = self.root / "runs"
        runs.mkdir()
        rundir = runs / orchestrate._run_id(case, "kimi-for-coding", "deferred", 1)

        def fail_after_config(*_args, **_kwargs):
            config_path = rundir / "home" / ".jcode" / "config.json"
            config_path.parent.mkdir(parents=True)
            config_path.write_text(SELECTED_SECRET)
            config_path.chmod(0o600)
            (rundir / "work").mkdir()
            (rundir / "events_1.jsonl").write_text(SELECTED_SECRET)
            raise RuntimeError("synthetic failure")

        with mock.patch.object(orchestrate, "_run_one", side_effect=fail_after_config):
            with self.assertRaisesRegex(RuntimeError, "synthetic failure"):
                orchestrate.run_one(
                    case, "kimi-for-coding", "deferred", 1, runs,
                    "/tmp/jcode", "/tmp/harness", None, 10, 1.0, 7,
                )
        self.assertFalse((rundir / "home").exists())
        self.assertFalse((rundir / "work").exists())
        self.assertFalse((rundir / "events_1.jsonl").exists())

    def test_paired_randomization_is_seeded_deterministic_and_adjacent(self):
        cases = [
            {"id": f"case-{index}", "tier": "core"}
            for index in range(5)
        ]
        args = (cases, ["kimi-for-coding"], ["static", "deferred"])
        first = orchestrate.build_jobs(*args, seed=99, explicit_repeats=2)
        repeated = orchestrate.build_jobs(*args, seed=99, explicit_repeats=2)
        different = orchestrate.build_jobs(*args, seed=100, explicit_repeats=2)

        signature = lambda jobs: [
            (case["id"], model, variant, repeat)
            for case, model, variant, repeat in jobs
        ]
        self.assertEqual(signature(first), signature(repeated))
        self.assertNotEqual(signature(first), signature(different))
        for offset in range(0, len(first), 2):
            left, right = first[offset:offset + 2]
            self.assertEqual(
                (left[0]["id"], left[1], left[3]),
                (right[0]["id"], right[1], right[3]),
            )
            self.assertEqual({"static", "deferred"}, {left[2], right[2]})

    def test_build_jobs_respects_case_variants_without_splitting_pairs(self):
        cases = [
            {"id": "paired", "tier": "core", "variants": ["static", "deferred"]},
            {"id": "negative", "tier": "core", "variants": ["deferred"]},
        ]
        jobs = orchestrate.build_jobs(
            cases, ["kimi-for-coding"], ["static", "deferred"],
            seed=71, explicit_repeats=3,
        )
        signature = [
            (case["id"], variant, repeat)
            for case, _model, variant, repeat in jobs
        ]
        self.assertEqual(3, sum(case_id == "negative" for case_id, _variant, _repeat in signature))
        self.assertTrue(all(
            variant == "deferred"
            for case_id, variant, _repeat in signature
            if case_id == "negative"
        ))
        for repeat in range(1, 4):
            positions = [
                index for index, (case_id, _variant, actual_repeat) in enumerate(signature)
                if case_id == "paired" and actual_repeat == repeat
            ]
            self.assertEqual(2, len(positions))
            self.assertEqual(1, positions[1] - positions[0])
            self.assertEqual(
                {"static", "deferred"},
                {signature[index][1] for index in positions},
            )

    def test_toolsearch_matrix_cli_dry_run_is_acp_only_and_routes_web_explicitly(self):
        runs = self.root / "matrix-runs"
        command = [
            sys.executable,
            str(Path(orchestrate.__file__).resolve()),
            "--bin", "/usr/bin/true",
            "--harness", "/usr/bin/true",
            "--mcp-fixture", "/usr/bin/true",
            "--runs-dir", str(runs),
            "--models", "kimi-for-coding",
            "--variants", "static,deferred",
            "--repeats", "1",
            "--workers", "1",
            "--toolsearch-matrix", str(orchestrate.toolsearch_cases.DEFAULT_MATRIX),
            "--dry-run",
        ]
        completed = subprocess.run(command, capture_output=True, text=True, timeout=30)
        self.assertEqual(0, completed.returncode, completed.stderr)
        self.assertIn("requires Web runner", completed.stdout)

        plan = json.loads((runs / "plan.json").read_text())
        self.assertEqual("toolsearch", plan["suite"])
        self.assertEqual(
            [{
                "case_id": "ts_browser_loopback_read",
                "surface": "web",
                "reason": "requires_web_runner",
            }],
            plan["skipped_cases"],
        )
        self.assertFalse(any(
            job["case_id"] == "ts_browser_loopback_read" for job in plan["jobs"]
        ))
        negative = [
            job for job in plan["jobs"]
            if job["case_id"].startswith("ts_negative_")
        ]
        self.assertTrue(negative)
        self.assertEqual({"deferred"}, {job["variant"] for job in negative})

        rejected = subprocess.run(
            command[:-1] + ["--cases", "ts_browser_loopback_read", "--dry-run"],
            capture_output=True, text=True, timeout=30,
        )
        self.assertEqual(2, rejected.returncode)
        self.assertIn("require the Web runner", rejected.stderr)

    def test_formal_run_requires_one_worker_and_explicit_paired_repeats(self):
        valid = dict(
            formal=True,
            quick=False,
            models=["kimi-for-coding"],
            variants=["static", "deferred"],
            repeats=10,
            repeat_scale=1.0,
            workers=1,
        )
        self.assertIsNone(orchestrate.validate_formal_run(**valid))
        with self.assertRaisesRegex(ValueError, "workers 1"):
            orchestrate.validate_formal_run(**{**valid, "workers": 2})
        with self.assertRaisesRegex(ValueError, "explicit --repeats"):
            orchestrate.validate_formal_run(**{**valid, "repeats": None})

    def test_session_extractor_keeps_routing_metadata_not_raw_payloads(self):
        secret = "sk-session-payload-secret-123456"
        fixture_tool = "mcp__fixture__catalog_lookup_precise"
        session = self.root / "session.json"
        entries = [
            {"type": "user", "content": secret},
            {
                "type": "tool_call", "name": "tool_search", "tool_call_id": "s1",
                "args": json.dumps({"query": f"select:{fixture_tool} {secret}"}),
                "batch_id": "b1", "batch_size": 1,
            },
            {
                "type": "tool_result", "name": "tool_search", "tool_call_id": "s1",
                "output": json.dumps({"matches": [fixture_tool], "secret": secret}),
                "duration_ms": 4,
            },
            {
                "type": "tool_call", "name": "execute", "tool_call_id": "d1",
                "args": json.dumps({"command": f"echo {secret}"}),
                "batch_id": "b2", "batch_size": 1,
            },
            {
                "type": "tool_result", "name": "execute", "tool_call_id": "d1",
                "output": secret, "duration_ms": 8,
            },
            {
                "type": "tool_call", "name": fixture_tool, "tool_call_id": "f1",
                "args": json.dumps({"request_id": "req-7", "limit": 3}),
                "batch_id": "b3", "batch_size": 1,
            },
            {
                "type": "tool_result", "name": fixture_tool, "tool_call_id": "f1",
                "output": "JCODE_MCP_FIXTURE_OK:synthetic", "duration_ms": 12,
            },
            {
                "type": "tool_observation",
                "tool_observation": {
                    "kind": "model_request", "model_request_seq": 1,
                    "visible_names": ["read", "tool_search"], "visible_count": 2,
                    "schema_bytes": 800, "schema_tokens_estimate": 200,
                },
            },
        ]
        session.write_text("".join(json.dumps(entry) + "\n" for entry in entries))

        trajectory = session_extract.extract_trajectory(
            [session], fixture_arg_tools={fixture_tool},
        )
        serialized = json.dumps(trajectory)
        self.assertNotIn(secret, serialized)
        self.assertNotIn("select:", serialized)
        self.assertNotIn("JCODE_MCP_FIXTURE_OK", serialized)
        self.assertNotIn("command", serialized)
        fixture_calls = [
            entry
            for entry in trajectory["sessions"][0]["entries"]
            if entry.get("name") == fixture_tool and entry["type"] == "tool_call"
        ]
        self.assertEqual({"request_id": "req-7", "limit": 3}, fixture_calls[0]["fixture_args"])
        self.assertEqual(3, trajectory["tool_counts"]["calls_total"])
        self.assertEqual(2, trajectory["tool_counts"]["first_visible"])
        self.assertEqual(200, trajectory["tool_counts"]["first_schema_tokens_estimate"])
        default_trajectory = session_extract.extract_trajectory([session])
        self.assertFalse(any(
            "fixture_args" in entry
            for extracted_session in default_trajectory["sessions"]
            for entry in extracted_session["entries"]
        ))

    def test_artifact_redaction_and_scanner_do_not_echo_canaries(self):
        publish = self.root / "publish"
        publish.mkdir()
        host_path = "/Users/real-operator/private"
        artifact = publish / "record.json"
        artifact.write_text(json.dumps({
            "api_key": SELECTED_SECRET,
            "path": host_path + "/.jcode/config.json",
            "authorization": "Bearer another-secret-token-12345",
        }))

        report = artifact_safety.sanitize_artifacts(
            [publish], [SELECTED_SECRET], [host_path],
        )
        findings = artifact_safety.scan_artifacts(
            [publish], [SELECTED_SECRET], [host_path],
        )
        self.assertFalse(findings)
        sanitized = artifact.read_text()
        self.assertNotIn(SELECTED_SECRET, sanitized)
        self.assertNotIn(host_path, sanitized)
        self.assertIn(artifact_safety.REDACTED_CREDENTIAL, sanitized)
        report_path = publish / "redaction_report.json"
        artifact_safety.write_redaction_report(report_path, report, findings)
        report_text = report_path.read_text()
        self.assertNotIn(SELECTED_SECRET, report_text)
        self.assertNotIn(host_path, report_text)
        self.assertFalse(artifact_safety.scan_artifacts(
            [publish], [SELECTED_SECRET], [host_path],
        ))


if __name__ == "__main__":
    unittest.main()
