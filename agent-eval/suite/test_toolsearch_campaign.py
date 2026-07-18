import json
import os
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path


HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
sys.path.insert(0, str(HERE.parent / "analysis"))

import toolsearch_campaign as campaign
import toolsearch_cases
import toolsearch_report


class FakeClock(campaign.Clock):
    def __init__(self):
        self.wall = datetime(2026, 7, 19, tzinfo=timezone.utc)
        self.mono = 1000.0

    def utc_now(self):
        value = self.wall
        self.wall += timedelta(seconds=1)
        return value

    def monotonic(self):
        value = self.mono
        self.mono += 1.0
        return value


class FakeCommands:
    def __init__(self, *, dirty=False, post_commit=None, fail_test_id=None):
        self.dirty = dirty
        self.post_commit = post_commit
        self.fail_test_id = fail_test_id
        self.calls = []
        self.build_outputs = []
        self.rev_parse_calls = 0

    def __call__(self, argv, **kwargs):
        argv = tuple(str(value) for value in argv)
        self.calls.append((argv, kwargs))
        stdout = ""
        returncode = 0
        if argv[:3] == ("git", "rev-parse", "HEAD"):
            self.rev_parse_calls += 1
            stdout = (
                self.post_commit
                if self.post_commit and self.rev_parse_calls > 1
                else "a" * 40
            ) + "\n"
        elif argv[:2] == ("git", "status"):
            stdout = " M changed.go\n" if self.dirty else ""
        elif argv == ("go", "env", "GOVERSION"):
            stdout = "go1.26.4\n"
        elif argv == ("go", "env", "GOOS", "GOARCH"):
            stdout = "darwin\narm64\n"
        elif argv[:4] == ("go", "list", "-m", "-f={{.Version}}"):
            stdout = "v0.9.9\n"
        elif argv[:2] == ("go", "build"):
            output = Path(argv[argv.index("-o") + 1])
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_bytes(("fixed-binary:" + argv[-1]).encode())
            output.chmod(0o700)
            self.build_outputs.append(output)
        elif argv[:2] == ("go", "test"):
            if self.fail_test_id and self.fail_test_id in " ".join(argv):
                returncode = 1
        else:
            raise AssertionError(f"unexpected command: {argv}")
        return subprocess.CompletedProcess(argv, returncode, stdout=stdout, stderr="")


class FakeDispatcher:
    def __init__(
        self,
        options,
        binaries,
        *,
        fail_after=None,
        leak="",
        tool_names_drift="",
        mutate_binary=False,
        supplementary_identity_drift=False,
    ):
        self.options = options
        self.binaries = binaries
        self.fail_after = fail_after
        self.leak = leak
        self.tool_names_drift = tool_names_drift
        self.mutate_binary = mutate_binary
        self.supplementary_identity_drift = supplementary_identity_drift
        self.calls = 0

    @staticmethod
    def _tool_counts(variant):
        searches = 1 if variant == "deferred" else 0
        return {
            "calls_total": searches,
            "results_total": searches,
            "calls_by_name": {"tool_search": searches} if searches else {},
            "results_by_status": {"completed": searches} if searches else {},
            "model_requests": 1,
            "first_visible": 10 if variant == "deferred" else 24,
            "max_visible": 10 if variant == "deferred" else 24,
            "first_schema_tokens_estimate": 40 if variant == "deferred" else 100,
            "max_schema_tokens_estimate": 40 if variant == "deferred" else 100,
        }

    def _write(self, job, runs_dir, *, supplementary=False, scenario="success"):
        run_dir = runs_dir / job.run_id
        run_dir.mkdir(mode=0o700)
        counts = self._tool_counts(job.variant)
        routing_counts = {
            "bypass": 0,
            "same_batch_activation": 0,
            "deferred_calls": 1 if job.variant == "deferred" else 0,
            "deferred_call_success": 1 if job.variant == "deferred" else 0,
            "search_calls": 1 if job.variant == "deferred" else 0,
        }
        record = {
            "run_id": job.run_id,
            "case_id": job.case["id"],
            "surface": job.case["surface"],
            "model": campaign.EXACT_MODEL_LABEL,
            "model_id": campaign.EXACT_MODEL_ID,
            "effort": "",
            "request_parameters": {"temperature": "omitted"},
            "variant": job.variant,
            "repeat": job.repeat,
            "seed": self.options.seed,
            "task_passed": True,
            "contracts_passed": True,
            "error_present": False,
            "stop_reason": "end_turn",
            "wall_s": 0.5,
            "tool_counts": counts,
            "tool_names": dict(counts["calls_by_name"]),
            "routing": {
                "passed": True,
                "counts": routing_counts,
                "checks": {},
                "violations": [],
            },
            "routing_passed": True,
            "artifact_safe": True,
        }
        if supplementary:
            record.update({
                "driver_passed": True,
                "routing_applicable": scenario == "success",
            })
        if self.leak:
            record["leak"] = self.leak
        if self.tool_names_drift == "display":
            record["tool_names"] = {"Search TODO": 1}
        elif self.tool_names_drift == "path":
            record["tool_names"] = {f"Read {run_dir}/work/box": 1}
        trajectory = {
            "schema_version": 1,
            "payload_policy": "metadata_only_except_declared_fixture_args",
            "run_id": job.run_id,
            "variant": job.variant,
            "parse_error_count": 0,
            "tool_counts": counts,
            "sessions": [{
                "session_index": 1,
                "source_present": True,
                "parse_error_lines": [],
                "entries": [],
            }],
        }
        redaction = {
            "schema_version": 1,
            "files_scanned": 2,
            "files_redacted": 0,
            "redacted_file_names": [],
            "replacement_counts": {},
            "post_redaction_findings": [],
            "safe": True,
        }
        campaign._write_private_json(run_dir / "record.json", record)
        campaign._write_private_json(run_dir / "trajectory.json", trajectory)
        campaign._write_private_json(run_dir / "redaction_report.json", redaction)
        return record

    def run_matrix_job(self, job, runs_dir, _forbidden_paths):
        if self.fail_after is not None and self.calls >= self.fail_after:
            raise campaign.CampaignError("fake_dispatch_failed")
        self.calls += 1
        record = self._write(job, runs_dir)
        if self.mutate_binary and self.calls == 1:
            with self.binaries.jcode.open("ab") as stream:
                stream.write(b"mutated-after-build")
        return record

    def run_supplementary_web(self, spec, case, runs_dir, _forbidden_paths):
        class Job:
            pass

        job = Job()
        job.run_id = f"supp__{spec.record_id}"
        job.case = case
        job.variant = spec.variant
        job.repeat = 1
        record = self._write(job, runs_dir, supplementary=True, scenario=spec.scenario)
        record.update({
            "language": spec.language,
            "scenario": (
                "success" if self.supplementary_identity_drift else spec.scenario
            ),
            "real_execution": True,
        })
        campaign._write_private_json(runs_dir / job.run_id / "record.json", record)
        return record


class Factory:
    def __init__(self, **dispatcher_options):
        self.dispatcher_options = dispatcher_options
        self.instance = None

    def __call__(self, options, binaries):
        self.instance = FakeDispatcher(options, binaries, **self.dispatcher_options)
        return self.instance


class ToolSearchCampaignTest(unittest.TestCase):
    def setUp(self):
        self.repo = campaign.REPO_DEFAULT.resolve()
        self.suite = toolsearch_cases.load_suite()
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)

    def tearDown(self):
        self.temporary.cleanup()

    def options(self, name, **overrides):
        values = {
            "repo": self.repo,
            "runs_dir": self.root / name,
            "mode": "canary",
            "repeats": 1,
        }
        values.update(overrides)
        return campaign.CampaignOptions(**values)

    def test_formal_plan_exactly_covers_matrix_in_adjacent_paired_blocks(self):
        options = campaign.normalize_options(self.options(
            "formal-plan", mode="formal", repeats=10,
        ))
        jobs = campaign.build_jobs(
            self.suite["cases"], options.repeats, options.seed, options.variants,
        )
        expected = sum(len(case["variants"]) for case in self.suite["cases"]) * 10
        self.assertEqual(expected, len(jobs))
        self.assertEqual({"acp", "web"}, {job.surface for job in jobs})
        self.assertEqual(len(jobs), len({job.run_id for job in jobs}))

        offset = 0
        while offset < len(jobs):
            job = jobs[offset]
            declared = set(job.case["variants"])
            if declared == set(campaign.VARIANTS):
                pair = jobs[offset:offset + 2]
                self.assertEqual(2, len(pair))
                self.assertEqual(1, len({item.pair_id for item in pair}))
                self.assertEqual(set(campaign.VARIANTS), {item.variant for item in pair})
                offset += 2
            else:
                self.assertEqual({job.variant}, declared)
                offset += 1

        suite_hashes = campaign.pin_suite_inputs(options)
        plan = campaign.build_plan(options, jobs, suite_hashes)
        self.assertTrue(plan["formal"])
        self.assertEqual(1, plan["workers"])
        self.assertEqual(
            [{"label": campaign.EXACT_MODEL_LABEL, "id": campaign.EXACT_MODEL_ID}],
            plan["models"],
        )
        self.assertEqual({"temperature": "omitted"}, plan["request_parameters"])
        self.assertEqual(
            {"jcode_tags": ["jcode_eval"]}, plan["build"],
        )
        self.assertEqual(suite_hashes, plan["suite_inputs"])
        self.assertNotIn("temperature", plan["jobs"][0])

    def test_provenance_and_fixed_binary_hashes(self):
        commands = FakeCommands()
        provenance = campaign.collect_provenance(self.repo, commands)
        self.assertEqual("a" * 40, provenance.commit)
        self.assertFalse(provenance.dirty)
        self.assertEqual("go1.26.4", provenance.go_version)
        self.assertEqual("darwin/arm64", provenance.os_arch)
        binaries = campaign.build_binaries(self.repo, self.root / "build", commands)
        self.assertEqual(
            {"jcode_sha256", "harness_sha256", "mcp_fixture_sha256"},
            set(binaries.hashes),
        )
        self.assertTrue(all(len(value) == 64 for value in binaries.hashes.values()))
        build_calls = [argv for argv, _kwargs in commands.calls if argv[:2] == ("go", "build")]
        self.assertEqual([
            (
                "go", "build", "-tags", "jcode_eval", "-trimpath", "-o",
                str(binaries.jcode), "./cmd/jcode/",
            ),
            (
                "go", "build", "-trimpath", "-o", str(binaries.harness), ".",
            ),
            (
                "go", "build", "-trimpath", "-o", str(binaries.mcp_fixture),
                "./agent-eval/fixture/mcp",
            ),
        ], build_calls)
        self.assertTrue(all(os.access(path, os.X_OK) for path in (
            binaries.jcode, binaries.harness, binaries.mcp_fixture,
        )))

    def test_dry_run_is_nonformal_and_builds_nothing(self):
        web = next(case for case in self.suite["cases"] if case["surface"] == "web")
        commands = FakeCommands()
        result = campaign.run_campaign(
            self.options(
                "dry", mode="dry-run", repeats=1, case_ids=(web["id"],),
            ),
            command_runner=commands,
        )
        self.assertFalse(result["formal"])
        self.assertEqual(0, result["completed"])
        self.assertEqual([], commands.calls)
        plan = json.loads((self.root / "dry" / "plan.json").read_text())
        self.assertFalse(plan["formal"])
        self.assertTrue(plan["dry_run"])
        self.assertFalse((self.root / "dry" / "campaign.json").exists())

    def test_canary_collects_sequential_intervals_and_provenance(self):
        acp = next(case for case in self.suite["cases"] if case["surface"] == "acp")
        web = next(case for case in self.suite["cases"] if case["surface"] == "web")
        commands = FakeCommands()
        factory = Factory()
        result = campaign.run_campaign(
            self.options("canary", case_ids=(acp["id"], web["id"])),
            command_runner=commands,
            dispatcher_factory=factory,
            clock=FakeClock(),
            secret_values=(),
        )
        manifest = json.loads((self.root / "canary" / "campaign.json").read_text())
        plan = json.loads((self.root / "canary" / "plan.json").read_text())
        records = json.loads((self.root / "canary" / "all_records.json").read_text())
        self.assertEqual("canary_complete", manifest["status"])
        self.assertFalse(manifest["formal"])
        self.assertEqual(result["planned"], manifest["planned_run_count"])
        self.assertEqual(result["planned"], manifest["completed_run_count"])
        self.assertEqual(plan["jobs"], [job.publication_record() for job in campaign.build_jobs(
            [acp, web], 1, campaign.DEFAULT_SEED,
        )])
        self.assertEqual(len(records), len(manifest["run_intervals"]))
        self.assertTrue(all(item["real_execution"] for item in manifest["run_intervals"]))
        self.assertTrue(all(item["successful"] for item in manifest["run_intervals"]))
        for before, after in zip(
            manifest["run_intervals"], manifest["run_intervals"][1:],
        ):
            self.assertLessEqual(before["finished_at"], after["started_at"])
        self.assertEqual("a" * 40, manifest["git"]["commit"])
        self.assertFalse(manifest["git"]["dirty"])
        self.assertEqual({"temperature": "omitted"}, manifest["request_parameters"])
        self.assertEqual({"jcode_tags": ["jcode_eval"]}, plan["build"])
        self.assertEqual({"jcode_tags": ["jcode_eval"]}, manifest["build"])
        self.assertTrue(all(not path.exists() for path in commands.build_outputs))
        for record in records:
            self.assertEqual(
                record["tool_counts"]["calls_by_name"], record["tool_names"],
            )

    def test_display_tool_title_is_rejected_as_noncanonical(self):
        case_id = self.suite["cases"][0]["id"]
        with self.assertRaisesRegex(
            campaign.CampaignError, "record_tool_names_invalid",
        ):
            campaign.run_campaign(
                self.options(
                    "display-title", case_ids=(case_id,), variants=("static",),
                ),
                command_runner=FakeCommands(),
                dispatcher_factory=Factory(tool_names_drift="display"),
                clock=FakeClock(),
                secret_values=(),
            )
        manifest = json.loads(
            (self.root / "display-title" / "campaign.json").read_text()
        )
        self.assertEqual("record_tool_names_invalid", manifest["failure_code"])
        self.assertEqual(0, manifest["completed_run_count"])
        self.assertFalse((self.root / "display-title" / "all_records.json").exists())

    def test_tool_title_path_is_sanitized_and_rejected_before_publication(self):
        case_id = self.suite["cases"][0]["id"]
        with self.assertRaisesRegex(
            campaign.CampaignError, "coordinator_artifact_scan_failed",
        ):
            campaign.run_campaign(
                self.options(
                    "title-path", case_ids=(case_id,), variants=("static",),
                ),
                command_runner=FakeCommands(),
                dispatcher_factory=Factory(tool_names_drift="path"),
                clock=FakeClock(),
                secret_values=(),
            )
        raw = b"".join(
            path.read_bytes()
            for path in (self.root / "title-path").rglob("*")
            if path.is_file()
        )
        self.assertNotIn(str(self.root / "title-path").encode(), raw)
        manifest = json.loads(
            (self.root / "title-path" / "campaign.json").read_text()
        )
        self.assertEqual("coordinator_artifact_scan_failed", manifest["failure_code"])
        self.assertEqual(0, manifest["completed_run_count"])

    def test_partial_failure_never_fabricates_completion(self):
        cases = tuple(case["id"] for case in self.suite["cases"][:2])
        factory = Factory(fail_after=1)
        with self.assertRaisesRegex(campaign.CampaignError, "fake_dispatch_failed"):
            campaign.run_campaign(
                self.options("partial", case_ids=cases),
                command_runner=FakeCommands(),
                dispatcher_factory=factory,
                clock=FakeClock(),
                secret_values=(),
            )
        manifest = json.loads((self.root / "partial" / "campaign.json").read_text())
        records = json.loads((self.root / "partial" / "all_records.json").read_text())
        self.assertEqual("failed", manifest["status"])
        self.assertEqual("fake_dispatch_failed", manifest["failure_code"])
        self.assertEqual(1, manifest["completed_run_count"])
        self.assertGreater(manifest["planned_run_count"], 1)
        self.assertEqual(2, len(manifest["run_intervals"]))
        self.assertFalse(manifest["run_intervals"][-1]["successful"])
        self.assertEqual(1, len(records))

    def test_git_commit_change_after_runs_fails_the_campaign(self):
        case_id = self.suite["cases"][0]["id"]
        with self.assertRaisesRegex(
            campaign.CampaignError, "git_provenance_changed_during_campaign",
        ):
            campaign.run_campaign(
                self.options("git-changed", case_ids=(case_id,), variants=("static",)),
                command_runner=FakeCommands(post_commit="b" * 40),
                dispatcher_factory=Factory(),
                clock=FakeClock(),
                secret_values=(),
            )
        manifest = json.loads((self.root / "git-changed" / "campaign.json").read_text())
        self.assertEqual("failed", manifest["status"])
        self.assertEqual(
            "git_provenance_changed_during_campaign", manifest["failure_code"],
        )
        self.assertEqual(
            manifest["planned_run_count"], manifest["completed_run_count"],
        )

    def test_binary_hash_change_after_build_fails_the_campaign(self):
        case_id = self.suite["cases"][0]["id"]
        with self.assertRaisesRegex(
            campaign.CampaignError, "binary_hash_changed_during_campaign",
        ):
            campaign.run_campaign(
                self.options(
                    "binary-changed", case_ids=(case_id,), variants=("static",),
                ),
                command_runner=FakeCommands(),
                dispatcher_factory=Factory(mutate_binary=True),
                clock=FakeClock(),
                secret_values=(),
            )
        manifest = json.loads(
            (self.root / "binary-changed" / "campaign.json").read_text()
        )
        self.assertEqual("failed", manifest["status"])
        self.assertEqual(
            "binary_hash_changed_during_campaign", manifest["failure_code"],
        )

    def test_formal_rejects_noncanonical_suite_content(self):
        alternate = self.root / "alternate-matrix.json"
        alternate.write_bytes(
            toolsearch_cases.DEFAULT_MATRIX.read_bytes() + b"\n"
        )
        with self.assertRaisesRegex(
            campaign.CampaignError, "formal_suite_inputs_drifted",
        ):
            campaign.run_campaign(
                self.options(
                    "suite-drift",
                    mode="formal",
                    repeats=10,
                    matrix=alternate,
                ),
                command_runner=FakeCommands(),
                dispatcher_factory=Factory(),
                clock=FakeClock(),
                secret_values=(),
            )
        self.assertFalse((self.root / "suite-drift").exists())

    def test_coordinator_redacts_and_rejects_secret_or_host_path(self):
        case_id = self.suite["cases"][0]["id"]
        secret = "sk-campaign-canary-secret-123456789"
        leak = f"{secret} {self.repo}"
        factory = Factory(leak=leak)
        with self.assertRaisesRegex(
            campaign.CampaignError, "coordinator_artifact_scan_failed",
        ):
            campaign.run_campaign(
                self.options("unsafe", case_ids=(case_id,), variants=("static",)),
                command_runner=FakeCommands(),
                dispatcher_factory=factory,
                clock=FakeClock(),
                secret_values=(secret,),
            )
        raw = b"".join(
            path.read_bytes()
            for path in (self.root / "unsafe").rglob("*")
            if path.is_file()
        )
        self.assertNotIn(secret.encode(), raw)
        self.assertNotIn(str(self.repo).encode(), raw)
        manifest = json.loads((self.root / "unsafe" / "campaign.json").read_text())
        self.assertEqual("failed", manifest["status"])
        self.assertEqual("coordinator_artifact_scan_failed", manifest["failure_code"])
        self.assertEqual(0, manifest["completed_run_count"])

    def test_formal_rejects_dirty_tree_before_build(self):
        with self.assertRaisesRegex(
            campaign.CampaignError, "formal_requires_clean_git_tree",
        ):
            campaign.run_campaign(
                self.options("dirty", mode="formal", repeats=10),
                command_runner=FakeCommands(dirty=True),
                dispatcher_factory=Factory(),
                clock=FakeClock(),
                secret_values=(),
            )
        self.assertTrue((self.root / "dirty" / "plan.json").is_file())
        self.assertFalse((self.root / "dirty" / "campaign.json").exists())

    def test_supplementary_is_allowlisted_and_excluded_from_active_intervals(self):
        web = next(case for case in self.suite["cases"] if case["surface"] == "web")
        commands = FakeCommands()
        result = campaign.run_campaign(
            self.options(
                "supplementary",
                case_ids=(web["id"],),
                include_supplementary=True,
            ),
            command_runner=commands,
            dispatcher_factory=Factory(),
            clock=FakeClock(),
            secret_values=(),
        )
        manifest = json.loads((self.root / "supplementary" / "campaign.json").read_text())
        supplementary = manifest["supplementary_records"]
        self.assertEqual(
            len(campaign.SUPPLEMENTARY_COMMANDS) + len(campaign.SUPPLEMENTARY_WEB_SPECS),
            len(supplementary),
        )
        self.assertEqual(len(campaign.SUPPLEMENTARY_WEB_SPECS), result["supplementary_completed"] - 3)
        self.assertTrue(all(item["counts_toward_active_duration"] is False for item in supplementary))
        command_records = [item for item in supplementary if item["kind"] == "deterministic_command"]
        self.assertEqual(set(campaign.SUPPLEMENTARY_COMMANDS), {
            item["record_id"] for item in command_records
        })
        self.assertTrue(all("argv" not in item for item in command_records))
        self.assertTrue(all(len(item["argv_sha256"]) == 64 for item in command_records))
        self.assertTrue(all(item["passed"] for item in supplementary))
        self.assertEqual(result["planned"], len(manifest["run_intervals"]))
        self.assertTrue(all(
            not interval["run_id"].startswith("supp__")
            for interval in manifest["run_intervals"]
        ))

    def test_supplementary_web_identity_drift_fails_closed(self):
        web = next(case for case in self.suite["cases"] if case["surface"] == "web")
        with self.assertRaisesRegex(campaign.CampaignError, "supplementary_web_failed"):
            campaign.run_campaign(
                self.options(
                    "supplementary-drift",
                    case_ids=(web["id"],),
                    include_supplementary=True,
                ),
                command_runner=FakeCommands(),
                dispatcher_factory=Factory(supplementary_identity_drift=True),
                clock=FakeClock(),
                secret_values=(),
            )
        manifest = json.loads(
            (self.root / "supplementary-drift" / "campaign.json").read_text()
        )
        self.assertEqual("failed", manifest["status"])
        self.assertEqual("supplementary_web_failed", manifest["failure_code"])

    def test_fake_formal_bundle_satisfies_report_schema_but_not_duration(self):
        commands = FakeCommands()
        result = campaign.run_campaign(
            self.options("formal", mode="formal", repeats=10),
            command_runner=commands,
            dispatcher_factory=Factory(),
            clock=FakeClock(),
            secret_values=(),
        )
        self.assertEqual("complete", result["campaign_status"])
        report = toolsearch_report.generate_report(
            toolsearch_cases.DEFAULT_MATRIX,
            toolsearch_cases.DEFAULT_BASE_SUITE,
            self.root / "formal",
            secret_values=(),
            forbidden_paths=(),
        )
        # Fake clocks deliberately provide far less than 30 minutes.  Reaching
        # report generation proves schema compatibility without fabricating the
        # duration gate or making any model/network call.
        self.assertFalse(report["campaign_duration_passed"])
        self.assertFalse(report["overall_passed"])


if __name__ == "__main__":
    unittest.main()
