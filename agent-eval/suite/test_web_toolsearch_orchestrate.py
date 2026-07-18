import json
import stat
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock


HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
sys.path.insert(0, str(HERE.parent / "analysis"))

import artifact_safety
import toolsearch_cases
import toolsearch_report
import web_browser_driver
import web_toolsearch_orchestrate as runner


CONFIG_SECRET = "sk-selected-provider-secret-never-publish"
UNRELATED_SECRET = "sk-unrelated-provider-secret-never-copy"
HOST_PRIVATE_PATH = "/Users/private/operator/project"


def _call(entries, name, call_id, batch_id, args, output="ok", denied=False):
    entries.extend([
        {
            "type": "tool_call",
            "name": name,
            "tool_call_id": call_id,
            "batch_id": batch_id,
            "batch_index": 0,
            "batch_size": 1,
            "args": json.dumps(args),
        },
        {
            "type": "tool_result",
            "name": name,
            "tool_call_id": call_id,
            "output": output,
            "denied": denied,
        },
    ])


def _success_session(variant, omit_act_disclosure=False):
    entries = [{
        "type": "tool_observation",
        "tool_observation": {
            "kind": "model_request",
            "model_request_seq": 1,
            "visible_count": 8 if variant == "deferred" else 24,
            "schema_tokens_estimate": 400 if variant == "deferred" else 1200,
        },
    }]
    if variant == "deferred":
        matches = ["browser_open", "browser_read"] if omit_act_disclosure else [
            "browser_open", "browser_snapshot", "browser_act", "browser_read",
        ]
        _call(
            entries,
            "tool_search",
            "search-1",
            "batch-search",
            {"query": "select:" + ",".join(matches)},
            json.dumps({"matches": matches}),
        )
    _call(entries, "browser_open", "open-1", "batch-open", {"url": "http://127.0.0.1:1/"})
    _call(entries, "browser_snapshot", "snapshot-1", "batch-snapshot", {})
    _call(
        entries,
        "browser_act",
        "act-fill",
        "batch-fill",
        {"action": "fill", "ref": "e1", "value": "synthetic-proof"},
    )
    _call(
        entries,
        "browser_act",
        "act-click",
        "batch-click",
        {"action": "click", "ref": "e2"},
    )
    _call(
        entries,
        "browser_read",
        "read-1",
        "batch-read",
        {},
        "JCODE_BROWSER_CONFIRMATION synthetic-receipt",
    )
    return entries


def _negative_session(variant, scenario):
    entries = [{
        "type": "tool_observation",
        "tool_observation": {
            "kind": "model_request",
            "model_request_seq": 1,
            "visible_count": 8 if variant == "deferred" else 24,
            "schema_tokens_estimate": 400 if variant == "deferred" else 1200,
        },
    }]
    if scenario == "browser_disabled":
        return entries
    if variant == "deferred":
        _call(
            entries,
            "tool_search",
            "search-1",
            "batch-search",
            {"query": "select:browser_open"},
            json.dumps({"matches": ["browser_open"]}),
        )
    _call(
        entries,
        "browser_open",
        "open-denied",
        "batch-open",
        {"url": "http://127.0.0.1:1/"},
        "Tool approval error: denied",
        denied=True,
    )
    return entries


def _driver_record(case):
    success = case.scenario == "success"
    denied = case.scenario == "approval_deny"
    disabled = case.scenario == "browser_disabled"
    calls = 6 if success and case.variant == "deferred" else 5 if success else 0
    if denied:
        calls = 2 if case.variant == "deferred" else 1
    return {
        "passed": True,
        "errors": [],
        "health": {"ready": True, "auth_required": True, "model_exact": True},
        "auth": {"unauthorized_401": True, "bearer_200": True},
        "browser_status": {
            "available": True,
            "enabled": not disabled,
            "backend": "managed" if not disabled else "",
            "chrome_found": not disabled,
            "extension_online": False,
            "dev_mode": False,
            "private_path": HOST_PRIVATE_PATH,
        },
        "chat": {
            "accepted": True,
            "saw_running": True,
            "consecutive_idle_polls": 2,
            "pending_approval_detected": 1 if denied else 0,
            "approval_denied": 1 if denied else 0,
            "pending_ask_detected": 0,
            "timed_out": False,
            "stop_sent": False,
        },
        "callback_proof": {
            "opened": success,
            "submitted": success,
            "value_matched": success,
            "confirmation_served": success,
            "open_count": 1 if success else 0,
            "submit_count": 1 if success else 0,
            "matching_submit_count": 1 if success else 0,
            "confirmation_count": 1 if success else 0,
        },
        "session_evidence": {
            "source_present": True,
            "parse_error_count": 0,
            "tool_call_count": calls,
            "browser_call_count": calls - (1 if case.variant == "deferred" else 0),
            "tool_search_call_count": 1 if case.variant == "deferred" and not disabled else 0,
            "execute_call_count": 0,
            "browser_result_success_count": 5 if success else 0,
            "browser_result_denied_count": 1 if denied else 0,
            "browser_result_failed_count": 0,
            "open_call_verified": success or denied,
            "snapshot_call_verified": success,
            "fill_call_verified": success,
            "click_call_verified": success,
            "read_confirmation_verified": success,
            "proof_order_verified": success,
        },
        "runtime": {
            "loopback_only": True,
            "token_env_only": True,
            "stdout_discarded": True,
            "stderr_discarded": True,
            "process_group_cleanup": True,
        },
        "untrusted_extra": CONFIG_SECRET,
    }


class FakeDriver:
    def __init__(self, omit_act_disclosure=False, fail=False, external_session=False):
        self.omit_act_disclosure = omit_act_disclosure
        self.fail = fail
        self.external_session = external_session
        self.calls = []
        self.configs = []
        self.homes = []

    def __call__(self, case):
        self.calls.append(case)
        self.homes.append(case.home)
        config = json.loads((case.home / ".jcode" / "config.json").read_text())
        self.configs.append(config)
        if self.fail:
            raise RuntimeError(CONFIG_SECRET)
        session_dir = case.home / ".jcode" / "sessions"
        usage_dir = case.home / ".jcode" / "usage"
        session_dir.mkdir(mode=0o700)
        usage_dir.mkdir(mode=0o700)
        session = session_dir / "11111111-2222-3333-4444-555555555555.json"
        entries = (
            _success_session(case.variant, self.omit_act_disclosure)
            if case.scenario == "success"
            else _negative_session(case.variant, case.scenario)
        )
        session.write_text("".join(json.dumps(entry) + "\n" for entry in entries))
        session.chmod(0o600)
        usage = usage_dir / "events.jsonl"
        usage.write_text(json.dumps({
            "prompt": 10,
            "completion": 5,
            "cached": 0,
            "reasoning": 0,
            "total": 15,
            "calls": 1,
        }) + "\n")
        usage.chmod(0o600)
        returned_session = case.binary if self.external_session else session
        return web_browser_driver.DriverResult(
            record=_driver_record(case),
            _session_path=returned_session,
        )


class WebToolSearchOrchestrateTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.binary = self.root / "fixed-jcode"
        self.binary.write_bytes(b"fixed binary")
        self.binary.chmod(0o755)
        self.provider_config = self.root / "provider-config.json"
        self.provider_config.write_text(json.dumps({
            "providers": {
                "kimi-for-coding": {
                    "api_key": CONFIG_SECRET,
                    "reasoning_effort": "high",
                },
                "other-provider": {"api_key": UNRELATED_SECRET},
            },
            "model": "unrelated/model",
            "temperature": 0.9,
        }))
        self.provider_config.chmod(0o600)
        self.cache = self.root / "models_dev.json"
        self.cache.write_text("{}\n")

    def tearDown(self):
        self.tmp.cleanup()

    def options(self, name="runs", **overrides):
        values = {
            "binary": self.binary,
            "runs_dir": self.root / name,
            "provider_config": self.provider_config,
            "model_cache": self.cache,
        }
        values.update(overrides)
        return runner.CampaignOptions(**values)

    def test_formal_dry_run_is_exact_paired_web_plan_without_credentials(self):
        options = self.options(
            "dry",
            binary=self.root / "not-built",
            formal=True,
            repeats=2,
            dry_run=True,
        )
        summary = runner.run_campaign(options, driver_fn=lambda _case: self.fail())

        self.assertEqual(4, summary["planned"])
        plan_path = options.runs_dir / "plan.json"
        plan = json.loads(plan_path.read_text())
        self.assertEqual(
            [{"label": runner.EXACT_MODEL_LABEL, "id": runner.EXACT_MODEL_ID}],
            plan["models"],
        )
        self.assertEqual({"static", "deferred"}, {job["variant"] for job in plan["jobs"]})
        self.assertTrue(all(job["surface"] == "web" for job in plan["jobs"]))
        self.assertTrue(all(job["model"] == runner.EXACT_MODEL_LABEL for job in plan["jobs"]))
        self.assertFalse((options.runs_dir / "index.jsonl").exists())
        self.assertFalse((options.runs_dir / "all_records.json").exists())
        self.assertEqual(0o600, stat.S_IMODE(plan_path.stat().st_mode))
        serialized = plan_path.read_text()
        self.assertNotIn(CONFIG_SECRET, serialized)
        self.assertNotIn(str(self.provider_config), serialized)

    def test_success_pair_writes_report_compatible_allowlisted_artifacts(self):
        fake = FakeDriver()
        options = self.options("success", formal=True)
        summary = runner.run_campaign(options, driver_fn=fake)

        self.assertTrue(summary["all_passed"], summary)
        self.assertEqual(2, len(fake.calls))
        self.assertEqual({"static", "deferred"}, {call.variant for call in fake.calls})
        for home in fake.homes:
            self.assertFalse(home.exists())
        for config in fake.configs:
            self.assertEqual(runner.EXACT_MODEL_ID, config["model"])
            self.assertEqual({"kimi-for-coding"}, set(config["providers"]))
            self.assertNotIn("temperature", config)
            self.assertEqual("always_allow", config["browser"]["approval"]["navigate"])
            self.assertEqual("full_access", config["default_mode"])

        records = json.loads((options.runs_dir / "all_records.json").read_text())
        plan = json.loads((options.runs_dir / "plan.json").read_text())
        jobs = {job["run_id"]: job for job in plan["jobs"]}
        browser_case = {
            case["id"]: case for case in toolsearch_cases.load_suite()["cases"]
        }["ts_browser_loopback_read"]
        self.assertEqual(2, len(records))
        for record in records:
            trajectory = json.loads(
                (options.runs_dir / record["run_id"] / "trajectory.json").read_text()
            )
            self.assertEqual("kimi-for-coding", record["model"])
            self.assertTrue(record["contracts_passed"])
            self.assertFalse(record["error_present"])
            self.assertEqual("end_turn", record["stop_reason"])
            self.assertGreaterEqual(record["wall_s"], 0.001)
            self.assertEqual(
                "metadata_only_except_declared_fixture_args",
                trajectory["payload_policy"],
            )
            self.assertTrue(trajectory["sessions"])
            self.assertIn("parse_error_lines", trajectory["sessions"][0])
            self.assertIn("entries", trajectory["sessions"][0])
            self.assertEqual(record["tool_counts"], trajectory["tool_counts"])
            self.assertEqual(
                record["tool_counts"]["calls_by_name"], record["tool_names"],
            )
            _trajectory, counts = toolsearch_report._validate_trajectory(
                trajectory, jobs[record["run_id"]],
            )
            toolsearch_report._validate_record(
                record,
                jobs[record["run_id"]],
                browser_case,
                plan["seed"],
                counts,
            )
            self.assertTrue(record["routing_applicable"])
            self.assertTrue(record["routing_passed"])
            self.assertTrue(record["artifact_safe"])
        published = [options.runs_dir]
        self.assertFalse(artifact_safety.scan_artifacts(
            published,
            secret_values=(CONFIG_SECRET, UNRELATED_SECRET),
            forbidden_paths=(str(self.provider_config), HOST_PRIVATE_PATH),
        ))
        self.assertEqual(2, len((options.runs_dir / "index.jsonl").read_text().splitlines()))

    def test_publication_path_scope_includes_repo_run_and_build_roots(self):
        runs = self.root / "scope-runs"
        rundir = runs / "one-run"
        build = self.root / "scope-build"
        binary = build / "bin" / "jcode"
        extra = self.root / "coordinator-extra"
        paths = runner._artifact_paths(
            self.provider_config,
            runs,
            rundir,
            binary,
            build,
            extra_paths=(extra,),
        )
        for expected in (
            runner.REPO_ROOT.resolve(),
            runs.resolve(),
            rundir.resolve(),
            build.resolve(),
            extra.resolve(),
        ):
            self.assertIn(str(expected), paths)

    def test_post_scan_failure_revokes_artifact_safe_marker(self):
        options = self.options("post-scan", variants=("static",))
        finding = {"file_name": "record.json", "category": "host_path"}
        with mock.patch.object(
            runner.artifact_safety,
            "scan_artifacts",
            side_effect=[[], [finding]],
        ):
            with self.assertRaisesRegex(runner.RunnerError, "artifact_post_scan_failed"):
                runner.run_campaign(options, driver_fn=FakeDriver())

        plan = json.loads((options.runs_dir / "plan.json").read_text())
        run_id = plan["jobs"][0]["run_id"]
        record = json.loads(
            (options.runs_dir / run_id / "record.json").read_text()
        )
        self.assertFalse(record["artifact_safe"])

    def test_deferred_routing_fails_when_act_and_snapshot_were_not_disclosed(self):
        fake = FakeDriver(omit_act_disclosure=True)
        options = self.options("missing-disclosure", variants=("deferred",))
        summary = runner.run_campaign(options, driver_fn=fake)

        self.assertFalse(summary["all_passed"])
        record = json.loads((options.runs_dir / "all_records.json").read_text())[0]
        self.assertTrue(record["driver_passed"])
        self.assertFalse(record["routing_passed"])
        self.assertFalse(record["task_passed"])
        missing = {
            item.get("tool")
            for item in record["routing"]["violations"]
            if item["type"] == "expected_search_match_missing"
        }
        self.assertEqual({"browser_snapshot", "browser_act"}, missing)

    def test_supplementary_deny_and_disabled_scenarios_use_driver_truth(self):
        for scenario in ("approval_deny", "browser_disabled"):
            with self.subTest(scenario=scenario):
                fake = FakeDriver()
                options = self.options(
                    f"negative-{scenario}",
                    variants=("deferred",),
                    scenario=scenario,
                )
                summary = runner.run_campaign(options, driver_fn=fake)
                self.assertTrue(summary["all_passed"], summary)
                record = json.loads((options.runs_dir / "all_records.json").read_text())[0]
                self.assertFalse(record["routing_applicable"])
                self.assertTrue(record["routing_passed"])
                self.assertTrue(record["task_passed"])
                self.assertEqual(
                    {"scenario_driver_verifier": True},
                    record["routing"]["checks"],
                )
                config = fake.configs[0]
                self.assertEqual("approval", config["default_mode"])
                self.assertFalse(config["auto_approve"])
                self.assertEqual("ask", config["browser"]["approval"]["navigate"])
                self.assertEqual(
                    scenario != "browser_disabled",
                    config["browser"]["enabled"],
                )

    def test_explicit_acp_case_is_rejected(self):
        options = self.options(
            "reject-acp",
            case_ids=("ts_direct_read",),
            dry_run=True,
        )
        with self.assertRaisesRegex(runner.RunnerError, "selected_case_not_web"):
            runner.run_campaign(options)
        self.assertFalse(options.runs_dir.exists())

    def test_formal_mode_rejects_nonpaired_parallel_or_multilingual_runs(self):
        for overrides, code in (
            ({"variants": ("deferred",)}, "formal_requires_paired_variants"),
            ({"workers": 2}, "workers_must_equal_one"),
            ({"languages": ("en", "zh")}, "formal_requires_single_language"),
            ({"scenario": "approval_deny"}, "formal_requires_success_scenario"),
        ):
            with self.subTest(code=code):
                options = self.options(
                    "formal-reject-" + code,
                    formal=True,
                    dry_run=True,
                    **overrides,
                )
                with self.assertRaisesRegex(runner.RunnerError, code):
                    runner.run_campaign(options)

    def test_driver_exception_is_fail_closed_and_home_is_removed(self):
        fake = FakeDriver(fail=True)
        options = self.options("driver-failure", variants=("static",))
        summary = runner.run_campaign(options, driver_fn=fake)

        self.assertFalse(summary["all_passed"])
        self.assertTrue(fake.homes)
        self.assertTrue(all(not path.exists() for path in fake.homes))
        record = json.loads((options.runs_dir / "all_records.json").read_text())[0]
        self.assertTrue(record["error_present"])
        self.assertEqual("driver_exception", record["stop_reason"])
        self.assertNotIn(CONFIG_SECRET, json.dumps(record))

    def test_session_evidence_must_remain_inside_isolated_home(self):
        fake = FakeDriver(external_session=True)
        options = self.options("external-session", variants=("static",))
        summary = runner.run_campaign(options, driver_fn=fake)

        self.assertFalse(summary["all_passed"])
        record = json.loads((options.runs_dir / "all_records.json").read_text())[0]
        self.assertEqual("session_scope_invalid", record["stop_reason"])
        self.assertFalse(record["session_valid"])

    def test_existing_nonempty_runs_directory_is_rejected(self):
        runs = self.root / "not-empty"
        runs.mkdir()
        (runs / "owned.txt").write_text("keep")
        options = self.options("ignored", runs_dir=runs, dry_run=True)
        with self.assertRaisesRegex(runner.RunnerError, "runs_dir_not_empty"):
            runner.run_campaign(options)
        self.assertEqual("keep", (runs / "owned.txt").read_text())


if __name__ == "__main__":
    unittest.main()
