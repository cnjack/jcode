import copy
import json
import sys
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path


HERE = Path(__file__).resolve().parent
ANALYSIS = HERE.parent / "analysis"
sys.path.insert(0, str(ANALYSIS))
sys.path.insert(0, str(HERE))

import toolsearch_cases
import toolsearch_report


SECRET_CANARY = "sk-report-canary-secret-123456789"
HOST_CANARY = "/Users/report-owner/private-worktree"


class SyntheticCampaign:
    def __init__(self, root):
        self.root = Path(root)
        self.runs = self.root / "runs"
        self.runs.mkdir()
        self.suite = toolsearch_cases.load_suite()
        self.repeats = 10
        self.seed = 20260718
        self.jobs = []
        self.records = []
        self.intervals = []
        self._build()

    @staticmethod
    def write(path, value):
        path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")
        path.chmod(0o600)

    @staticmethod
    def tool_counts(searches, variant, full_schema=False):
        if variant == "static":
            schema_tokens = 100
        else:
            # Ordinary cases mirror the calibrated ~18% canary reduction.
            # Only the representative complete 100-tool catalog is expected
            # to meet the pinned >=50% full-feature gate.
            schema_tokens = 40 if full_schema else 82
        visible = 24 if variant == "static" else 10
        calls = {"tool_search": searches} if searches else {}
        return {
            "calls_total": searches,
            "results_total": searches,
            "calls_by_name": calls,
            "results_by_status": ({"completed": searches} if searches else {}),
            "model_requests": 1,
            "first_visible": visible,
            "max_visible": visible,
            "first_schema_tokens_estimate": schema_tokens,
            "max_schema_tokens_estimate": schema_tokens,
        }

    def _build(self):
        started = datetime(2026, 7, 19, 0, 0, tzinfo=timezone.utc)
        cursor = started + timedelta(seconds=20)
        for case in self.suite["cases"]:
            tags = set(case["metric_tags"])
            for repeat in range(1, self.repeats + 1):
                for variant in case["variants"]:
                    run_id = f"{case['id']}__kimi-for-coding__{variant}__r{repeat}"
                    job = {
                        "run_id": run_id,
                        "case_id": case["id"],
                        "model": toolsearch_report.EXACT_MODEL_LABEL,
                        "model_id": toolsearch_report.EXACT_MODEL_ID,
                        "variant": variant,
                        "repeat": repeat,
                    }
                    self.jobs.append(job)

                    searches = 0
                    deferred_calls = 0
                    if variant == "deferred" and "deferred_call_accuracy" in tags:
                        searches = 1
                        deferred_calls = 1
                    elif variant == "deferred" and "negative_search" in tags:
                        searches = 1
                    tool_counts = self.tool_counts(
                        searches,
                        variant,
                        full_schema="full_schema_disclosure" in tags,
                    )
                    routing_counts = {
                        "bypass": 0,
                        "same_batch_activation": 0,
                        "deferred_calls": deferred_calls,
                        "deferred_call_success": deferred_calls,
                        "search_calls": searches,
                    }
                    routing = {
                        "passed": True if variant == "deferred" else None,
                        "counts": routing_counts,
                        "checks": {"arguments": True},
                        "violations": [],
                    }
                    record_counts = {
                        **tool_counts,
                        "declared_deferred": 8,
                        "mcp_fixture_catalog": (
                            case.get("mcp_fixture", {}).get("tool_count", 0)
                        ),
                    }
                    record = {
                        "run_id": run_id,
                        "case_id": case["id"],
                        "case_title": case["title"],
                        "category": case["category"],
                        "tier": case["tier"],
                        "surface": case["surface"],
                        "model": toolsearch_report.EXACT_MODEL_LABEL,
                        "model_id": toolsearch_report.EXACT_MODEL_ID,
                        "effort": "",
                        "variant": variant,
                        "seed": self.seed,
                        "request_parameters": {"temperature": "omitted"},
                        "repeat": repeat,
                        "task_passed": True,
                        "contracts_passed": True,
                        "stop_reason": "end_turn",
                        "error_present": False,
                        "wall_s": 6.0,
                        "tool_counts": record_counts,
                        "tool_names": dict(tool_counts["calls_by_name"]),
                        "routing": routing,
                        "routing_passed": routing["passed"],
                        "artifact_safe": True,
                        "usage_total": {
                            "prompt": 1000,
                            "completion": 100,
                            "total": 1100,
                        },
                    }
                    trajectory = {
                        "schema_version": 1,
                        "payload_policy": "metadata_only_except_declared_fixture_args",
                        "run_id": run_id,
                        "variant": variant,
                        "session_count": 1,
                        "parse_error_count": 0,
                        "tool_counts": tool_counts,
                        "sessions": [{
                            "session_index": 1,
                            "source_present": True,
                            "parse_error_lines": [],
                            "entries": [{
                                "sequence": 1,
                                "type": "tool_observation",
                                "kind": "model_request",
                                "model_request_seq": 1,
                                "visible_count": tool_counts["first_visible"],
                                "schema_tokens_estimate": tool_counts[
                                    "first_schema_tokens_estimate"
                                ],
                                "newly_visible_deferred": [],
                            }],
                        }],
                    }
                    redaction = {
                        "schema_version": 1,
                        "files_scanned": 3,
                        "files_redacted": 0,
                        "redacted_file_names": [],
                        "replacement_counts": {},
                        "post_redaction_findings": [],
                        "safe": True,
                    }
                    run_dir = self.runs / run_id
                    run_dir.mkdir()
                    self.write(run_dir / "record.json", record)
                    self.write(run_dir / "trajectory.json", trajectory)
                    self.write(run_dir / "redaction_report.json", redaction)
                    self.records.append(record)

                    interval_end = cursor + timedelta(seconds=6)
                    self.intervals.append({
                        "run_id": run_id,
                        "started_at": cursor.isoformat().replace("+00:00", "Z"),
                        "finished_at": interval_end.isoformat().replace("+00:00", "Z"),
                        "real_execution": True,
                        "successful": True,
                    })
                    cursor = interval_end

        suite_hashes = toolsearch_report._suite_input_hashes(
            toolsearch_cases.DEFAULT_MATRIX,
            toolsearch_cases.DEFAULT_BASE_SUITE,
        )
        plan = {
            "schema_version": 1,
            "suite": "toolsearch",
            "mode": "formal",
            "dry_run": False,
            "seed": self.seed,
            "formal": True,
            "workers": 1,
            "models": [{
                "label": toolsearch_report.EXACT_MODEL_LABEL,
                "id": toolsearch_report.EXACT_MODEL_ID,
            }],
            "variants": ["static", "deferred"],
            "repeats": self.repeats,
            "request_parameters": {"temperature": "omitted"},
            "build": {"jcode_tags": ["jcode_eval"]},
            "suite_inputs": suite_hashes,
            "supplementary_planned": True,
            "jobs": self.jobs,
        }
        supplementary = self._build_supplementary(started)
        finished = started + timedelta(seconds=2000)
        campaign = {
            "schema_version": 1,
            "status": "complete",
            "formal": True,
            "mode": "formal",
            "started_at": started.isoformat().replace("+00:00", "Z"),
            "finished_at": finished.isoformat().replace("+00:00", "Z"),
            "monotonic_elapsed_s": 2000.0,
            "planned_run_count": len(self.jobs),
            "completed_run_count": len(self.records),
            "workers": 1,
            "model_label": toolsearch_report.EXACT_MODEL_LABEL,
            "model_id": toolsearch_report.EXACT_MODEL_ID,
            "request_parameters": {"temperature": "omitted"},
            "build": {"jcode_tags": ["jcode_eval"]},
            "git": {"commit": "a" * 40, "dirty": False},
            "binaries": {
                "jcode_sha256": "1" * 64,
                "harness_sha256": "2" * 64,
                "mcp_fixture_sha256": "3" * 64,
            },
            "suite_inputs": suite_hashes,
            "environment": {
                "go_version": "go1.26.4",
                "os_arch": "darwin/arm64",
                "eino_version": "v0.9.9",
            },
            "run_intervals": self.intervals,
            "supplementary_records": supplementary,
            "supplementary_counts_toward_active_duration": False,
        }
        self.write(self.runs / "plan.json", plan)
        self.write(self.runs / "all_records.json", self.records)
        self.write(self.runs / "campaign.json", campaign)

    def _build_supplementary(self, campaign_started):
        records = []
        cursor = campaign_started + timedelta(seconds=1)
        for index, record_id in enumerate(
            sorted(toolsearch_report.EXPECTED_SUPPLEMENTARY_COMMANDS), 1,
        ):
            finished = cursor + timedelta(seconds=1)
            records.append({
                "kind": "deterministic_command",
                "record_id": record_id,
                "argv_sha256": f"{index}" * 64,
                "started_at": cursor.isoformat().replace("+00:00", "Z"),
                "finished_at": finished.isoformat().replace("+00:00", "Z"),
                "wall_s": 1.0,
                "exit_code": 0,
                "passed": True,
                "real_execution": True,
                "counts_toward_active_duration": False,
            })
            cursor = finished

        web_case = next(case for case in self.suite["cases"] if case["surface"] == "web")
        for index, (record_id, expected) in enumerate(
            toolsearch_report.EXPECTED_SUPPLEMENTARY_WEB.items(), 4,
        ):
            source_id = (
                f"{web_case['id']}__kimi-for-coding__{expected['variant']}__r1"
            )
            source_dir = self.runs / source_id
            run_id = f"supp__{record_id}"
            run_dir = self.runs / "supplementary" / run_id
            run_dir.mkdir(parents=True)

            record = json.loads((source_dir / "record.json").read_text())
            record.update({
                "run_id": run_id,
                "repeat": 1,
                "language": expected["language"],
                "scenario": expected["scenario"],
                "routing_applicable": expected["routing_applicable"],
                "real_execution": True,
                "driver_passed": True,
                "task_passed": True,
                "contracts_passed": True,
                "error_present": False,
            })
            trajectory = json.loads((source_dir / "trajectory.json").read_text())
            trajectory["run_id"] = run_id
            redaction = json.loads((source_dir / "redaction_report.json").read_text())
            self.write(run_dir / "record.json", record)
            self.write(run_dir / "trajectory.json", trajectory)
            self.write(run_dir / "redaction_report.json", redaction)

            finished = cursor + timedelta(seconds=1)
            records.append({
                "kind": "web_browser_canary",
                "record_id": record_id,
                "scenario": expected["scenario"],
                "language": expected["language"],
                "variant": expected["variant"],
                "record_sha256": toolsearch_report._file_sha256(
                    run_dir / "record.json"
                ),
                "started_at": cursor.isoformat().replace("+00:00", "Z"),
                "finished_at": finished.isoformat().replace("+00:00", "Z"),
                "wall_s": 1.0,
                "driver_passed": True,
                "routing_applicable": expected["routing_applicable"],
                "task_passed": True,
                "artifact_safe": True,
                "identity_matches": True,
                "passed": True,
                "real_execution": True,
                "counts_toward_active_duration": False,
            })
            cursor = finished
        return records

    def update_record(self, run_id, mutate):
        record_path = self.runs / run_id / "record.json"
        record = json.loads(record_path.read_text())
        mutate(record)
        self.write(record_path, record)
        records = json.loads((self.runs / "all_records.json").read_text())
        for index, candidate in enumerate(records):
            if candidate["run_id"] == run_id:
                records[index] = copy.deepcopy(record)
                break
        self.write(self.runs / "all_records.json", records)

    def update_schema_tokens(self, case_id, variant, value):
        records = json.loads((self.runs / "all_records.json").read_text())
        for record in records:
            if record["case_id"] != case_id or record["variant"] != variant:
                continue
            run_id = record["run_id"]
            record["tool_counts"]["first_schema_tokens_estimate"] = value
            record["tool_counts"]["max_schema_tokens_estimate"] = value
            self.write(self.runs / run_id / "record.json", record)

            trajectory_path = self.runs / run_id / "trajectory.json"
            trajectory = json.loads(trajectory_path.read_text())
            trajectory["tool_counts"]["first_schema_tokens_estimate"] = value
            trajectory["tool_counts"]["max_schema_tokens_estimate"] = value
            for session in trajectory["sessions"]:
                for entry in session["entries"]:
                    if entry.get("kind") == "model_request":
                        entry["schema_tokens_estimate"] = value
            self.write(trajectory_path, trajectory)
        self.write(self.runs / "all_records.json", records)


class ToolSearchReportTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.synthetic = SyntheticCampaign(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def generate(self):
        return toolsearch_report.generate_report(
            toolsearch_cases.DEFAULT_MATRIX,
            toolsearch_cases.DEFAULT_BASE_SUITE,
            self.synthetic.runs,
            secret_values=[SECRET_CANARY],
            forbidden_paths=[HOST_CANARY],
        )

    def test_synthetic_pass_generates_one_self_contained_safe_report(self):
        result = self.generate()
        self.assertTrue(result["overall_passed"])
        self.assertEqual(9, len(result["gate_results"]))
        self.assertTrue(all(result["gate_results"].values()))
        self.assertTrue(result["campaign_duration_passed"])

        report = self.synthetic.runs / result["output_name"]
        document = report.read_text()
        self.assertIn("<style>", document)
        self.assertNotIn("<script", document)
        self.assertIn("Critical pass@10", document)
        self.assertIn("ts_browser_loopback_read", document)
        self.assertIn("successful_real_union_at_least_1800s", document)
        self.assertIn("effort=omitted", document)
        self.assertIn("JCode tags <code>jcode_eval</code>", document)
        self.assertIn('href="plan.json"', document)
        self.assertNotIn(str(self.synthetic.root), document)
        self.assertEqual([], toolsearch_report.scan_report(
            report, [SECRET_CANARY], [HOST_CANARY],
        ))

    def test_full_schema_gate_uses_only_full_catalog_and_keeps_50_percent(self):
        evaluation = toolsearch_report.evaluate(
            toolsearch_cases.DEFAULT_MATRIX,
            toolsearch_cases.DEFAULT_BASE_SUITE,
            self.synthetic.runs,
        )
        summaries = {item["case_id"]: item for item in evaluation["case_summaries"]}
        self.assertAlmostEqual(0.18, summaries["ts_exact_select_goal_get"]["schema_reduction"])
        self.assertAlmostEqual(0.60, summaries["ts_mcp_catalog_100"]["schema_reduction"])
        gate = next(
            item for item in evaluation["gates"]
            if item["name"] == "first_schema_token_reduction"
        )
        self.assertAlmostEqual(0.60, gate["value"])
        self.assertEqual(0.50, gate["threshold"])
        self.assertTrue(gate["passed"])

        # A full-catalog regression cannot be rescued by ordinary cases.
        self.synthetic.update_schema_tokens("ts_mcp_catalog_100", "deferred", 60)
        result = self.generate()
        self.assertFalse(result["overall_passed"])
        self.assertFalse(result["gate_results"]["first_schema_token_reduction"])

    def test_synthetic_gate_failure_is_visible_and_fails_overall(self):
        run_id = "ts_exact_select_goal_get__kimi-for-coding__deferred__r1"

        def introduce_bypass(record):
            record["routing"]["counts"]["bypass"] = 1
            record["routing"]["passed"] = False
            record["routing"]["violations"] = [{"type": "deferred_bypass"}]

        self.synthetic.update_record(run_id, introduce_bypass)
        result = self.generate()
        self.assertFalse(result["overall_passed"])
        self.assertFalse(result["gate_results"]["deferred_bypass"])
        document = (self.synthetic.runs / result["output_name"]).read_text()
        self.assertIn("deferred_bypass", document)
        self.assertIn("routing_failed", document)
        self.assertIn("Acceptance failed", document)

    def test_missing_field_or_artifact_fails_closed(self):
        campaign_path = self.synthetic.runs / "campaign.json"
        campaign = json.loads(campaign_path.read_text())
        del campaign["monotonic_elapsed_s"]
        self.synthetic.write(campaign_path, campaign)
        with self.assertRaisesRegex(toolsearch_report.ReportError, "monotonic"):
            self.generate()

        # Restore the campaign, then prove a missing per-run artifact also fails.
        campaign["monotonic_elapsed_s"] = 2000.0
        self.synthetic.write(campaign_path, campaign)
        first = self.synthetic.jobs[0]["run_id"]
        (self.synthetic.runs / first / "trajectory.json").unlink()
        with self.assertRaisesRegex(toolsearch_report.ReportError, "trajectory"):
            self.generate()

    def test_display_or_drifted_tool_names_fail_closed(self):
        run_id = "ts_exact_select_goal_get__kimi-for-coding__deferred__r1"

        def replace_with_display_title(record):
            record["tool_names"] = {"Search TODO": 1}

        self.synthetic.update_record(run_id, replace_with_display_title)
        with self.assertRaisesRegex(
            toolsearch_report.ReportError, "record tool_names",
        ):
            self.generate()

        # A syntactically valid tool name is still invalid when it is not the
        # canonical count map extracted from the private session.
        def replace_with_other_canonical_name(record):
            record["tool_names"] = {"read_other": 1}

        self.synthetic.update_record(run_id, replace_with_other_canonical_name)
        with self.assertRaisesRegex(
            toolsearch_report.ReportError, "canonical tool counts",
        ):
            self.generate()

    def test_missing_or_wrong_jcode_eval_build_tag_fails_closed(self):
        plan_path = self.synthetic.runs / "plan.json"
        plan = json.loads(plan_path.read_text())
        del plan["build"]
        self.synthetic.write(plan_path, plan)
        with self.assertRaisesRegex(toolsearch_report.ReportError, "build provenance"):
            self.generate()

        plan["build"] = {"jcode_tags": ["jcode_eval"]}
        self.synthetic.write(plan_path, plan)
        campaign_path = self.synthetic.runs / "campaign.json"
        campaign = json.loads(campaign_path.read_text())
        campaign["build"] = {"jcode_tags": []}
        self.synthetic.write(campaign_path, campaign)
        with self.assertRaisesRegex(toolsearch_report.ReportError, "jcode_eval build tag"):
            self.generate()

    def test_failed_or_nonformal_campaign_is_rejected(self):
        campaign_path = self.synthetic.runs / "campaign.json"
        campaign = json.loads(campaign_path.read_text())
        campaign["status"] = "failed"
        campaign["failure_code"] = "git_provenance_changed_during_campaign"
        self.synthetic.write(campaign_path, campaign)
        with self.assertRaisesRegex(toolsearch_report.ReportError, "complete formal"):
            self.generate()

        campaign["status"] = "complete"
        campaign.pop("failure_code")
        campaign["formal"] = False
        campaign["mode"] = "canary"
        self.synthetic.write(campaign_path, campaign)
        with self.assertRaisesRegex(toolsearch_report.ReportError, "complete formal"):
            self.generate()

    def test_supplementary_coverage_is_exact_and_fully_verified(self):
        campaign_path = self.synthetic.runs / "campaign.json"
        campaign = json.loads(campaign_path.read_text())
        complete_campaign = copy.deepcopy(campaign)
        campaign["supplementary_records"].pop()
        self.synthetic.write(campaign_path, campaign)
        with self.assertRaisesRegex(toolsearch_report.ReportError, "supplementary"):
            self.generate()

        campaign = complete_campaign
        web_record = next(
            item for item in campaign["supplementary_records"]
            if item["kind"] == "web_browser_canary"
        )
        web_record["scenario"] = "success"
        self.synthetic.write(campaign_path, campaign)
        with self.assertRaisesRegex(toolsearch_report.ReportError, "Web identity"):
            self.generate()

    def test_formal_suite_inputs_are_content_locked(self):
        alternate = self.synthetic.root / "alternate-matrix.json"
        alternate.write_bytes(toolsearch_cases.DEFAULT_MATRIX.read_bytes() + b"\n")
        with self.assertRaisesRegex(toolsearch_report.ReportError, "pinned defaults"):
            toolsearch_report.generate_report(
                alternate,
                toolsearch_cases.DEFAULT_BASE_SUITE,
                self.synthetic.runs,
            )

        campaign_path = self.synthetic.runs / "campaign.json"
        campaign = json.loads(campaign_path.read_text())
        campaign["suite_inputs"]["matrix_sha256"] = "f" * 64
        self.synthetic.write(campaign_path, campaign)
        with self.assertRaisesRegex(toolsearch_report.ReportError, "suite input hashes"):
            self.generate()

    def test_overlap_or_less_than_30_minutes_fails_duration_proof(self):
        campaign_path = self.synthetic.runs / "campaign.json"
        campaign = json.loads(campaign_path.read_text())
        campaign["monotonic_elapsed_s"] = 1799.0
        campaign["run_intervals"][1]["started_at"] = (
            campaign["run_intervals"][0]["started_at"]
        )
        self.synthetic.write(campaign_path, campaign)

        result = self.generate()
        self.assertFalse(result["overall_passed"])
        self.assertFalse(result["campaign_duration_passed"])
        document = (self.synthetic.runs / result["output_name"]).read_text()
        self.assertIn("workers_one_no_interval_overlap", document)
        self.assertIn("monotonic_at_least_1800s", document)

    def test_canary_payload_is_rejected_and_scanner_never_echoes_match(self):
        run_id = self.synthetic.jobs[0]["run_id"]

        def inject_raw_prompt(record):
            record["prompt"] = f"do not publish {SECRET_CANARY} {HOST_CANARY}"

        self.synthetic.update_record(run_id, inject_raw_prompt)
        with self.assertRaisesRegex(toolsearch_report.ReportError, "raw payload"):
            self.generate()
        self.assertFalse((self.synthetic.runs / "toolsearch-report.html").exists())

        unsafe = self.synthetic.runs / "unsafe.html"
        unsafe.write_text(f"credential={SECRET_CANARY}\npath={HOST_CANARY}\n")
        findings = toolsearch_report.scan_report(
            unsafe, [SECRET_CANARY], [HOST_CANARY],
        )
        serialized = json.dumps(findings)
        self.assertIn("exact_credential", serialized)
        self.assertIn("host_path", serialized)
        self.assertNotIn(SECRET_CANARY, serialized)
        self.assertNotIn(HOST_CANARY, serialized)

    def test_safe_metric_names_require_exact_path_and_type(self):
        run_id = self.synthetic.jobs[0]["run_id"]

        def replace_prompt_token_count_with_payload(record):
            record["usage_total"]["prompt"] = "raw prompt payload"

        self.synthetic.update_record(run_id, replace_prompt_token_count_with_payload)
        with self.assertRaisesRegex(toolsearch_report.ReportError, "raw payload"):
            self.generate()

        with self.assertRaisesRegex(toolsearch_report.ReportError, "raw payload"):
            toolsearch_report._walk_artifact_safety(
                {"arguments": True}, "wrong-path metadata",
            )
        with self.assertRaisesRegex(toolsearch_report.ReportError, "raw payload"):
            toolsearch_report._walk_artifact_safety(
                {"nested": {"usage_total": {"prompt": 1}}},
                "nested prompt metric",
            )
        with self.assertRaisesRegex(toolsearch_report.ReportError, "raw payload"):
            toolsearch_report._walk_artifact_safety(
                {"nested": {"routing": {"checks": {"arguments": True}}}},
                "nested argument check",
            )

    def test_highspeed_model_and_unsafe_redaction_report_are_rejected(self):
        campaign_path = self.synthetic.runs / "campaign.json"
        campaign = json.loads(campaign_path.read_text())
        campaign["model_id"] += "-highspeed"
        self.synthetic.write(campaign_path, campaign)
        with self.assertRaisesRegex(toolsearch_report.ReportError, "exact Kimi"):
            self.generate()

        campaign["model_id"] = toolsearch_report.EXACT_MODEL_ID
        campaign["git"]["dirty"] = True
        self.synthetic.write(campaign_path, campaign)
        with self.assertRaisesRegex(toolsearch_report.ReportError, "clean git tree"):
            self.generate()

        campaign["git"]["dirty"] = False
        self.synthetic.write(campaign_path, campaign)
        first = self.synthetic.jobs[0]["run_id"]
        redaction_path = self.synthetic.runs / first / "redaction_report.json"
        redaction = json.loads(redaction_path.read_text())
        redaction["safe"] = False
        redaction["post_redaction_findings"] = [{"category": "credential_pattern"}]
        self.synthetic.write(redaction_path, redaction)
        with self.assertRaisesRegex(toolsearch_report.ReportError, "not safe"):
            self.generate()


if __name__ == "__main__":
    unittest.main()
