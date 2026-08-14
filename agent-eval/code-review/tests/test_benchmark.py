import importlib.util
import json
import subprocess
import tempfile
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).resolve().parents[1] / "benchmark.py"
SPEC = importlib.util.spec_from_file_location("code_review_benchmark", MODULE_PATH)
benchmark = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(benchmark)


def git(repo: Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repo), *args],
        check=True,
        stdout=subprocess.PIPE,
        text=True,
    ).stdout.strip()


class ManifestTest(unittest.TestCase):
    def test_added_lines_from_zero_context_patch(self):
        patch = """@@ -10,0 +11,2 @@
+one
+two
@@ -20 +23 @@
-old
+new
"""
        self.assertEqual(benchmark.added_lines_from_patch(patch), [11, 12, 23])

    def test_manifest_groups_areas_and_keeps_waivers_visible(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            git(repo, "init", "-q")
            git(repo, "config", "user.name", "Benchmark")
            git(repo, "config", "user.email", "benchmark@example.com")
            (repo / "web/src").mkdir(parents=True)
            (repo / "packages/ui/src").mkdir(parents=True)
            (repo / "web/src/app.ts").write_text("export const value = 1;\n")
            (repo / "packages/ui/src/view.ts").write_text("export const view = 1;\n")
            (repo / "pnpm-lock.yaml").write_text("lockfileVersion: 9\n")
            git(repo, "add", ".")
            git(repo, "commit", "-qm", "base")
            base = git(repo, "rev-parse", "HEAD")
            (repo / "web/src/app.ts").write_text("export const value = 2;\nexport const next = 3;\n")
            (repo / "packages/ui/src/view.ts").write_text("export const view = 2;\n")
            (repo / "pnpm-lock.yaml").write_text("lockfileVersion: 9\nchanged: true\n")
            git(repo, "add", ".")
            git(repo, "commit", "-qm", "head")
            head = git(repo, "rev-parse", "HEAD")

            manifest = benchmark.build_manifest(repo, base, head)
            self.assertEqual(manifest["counts"]["changed_files"], 3)
            self.assertEqual(manifest["counts"]["eligible_files"], 2)
            self.assertEqual(manifest["counts"]["waived_files"], 1)
            self.assertEqual({unit["area"] for unit in manifest["units"]}, {"web", "packages/ui"})
            lock = next(file for file in manifest["files"] if file["path"] == "pnpm-lock.yaml")
            self.assertEqual(lock["waiver_reason"], "lockfile")
            self.assertTrue(all(unit["patch_sha256"] for unit in manifest["units"]))


class ScoreTest(unittest.TestCase):
    def test_score_uses_semantic_judgment_profile_and_real_token_events(self):
        manifest = {
            "units": [{"id": "web-01", "files": ["web/app.ts"], "changed_lines": 10}],
            "files": [{"path": "web/app.ts", "added_lines": [7, 8], "eligible": True}],
            "counts": {"eligible_changed_lines": 10},
        }
        result = {
            "units": [{"unit_id": "web-01", "status": "complete", "files_reviewed": ["web/app.ts"]}],
            "findings": [
                {
                    "id": "web-01/c1",
                    "path": "web/app.ts",
                    "line": 7,
                    "title": "Wrong request correlation",
                    "rationale": "Multiple requests can attach in reverse order",
                },
                {
                    "id": "web-01/c2",
                    "path": "web/app.ts",
                    "line": 8,
                    "title": "Naming could be clearer",
                    "rationale": "This is a style suggestion",
                },
            ],
            "verifier_status": "complete",
            "metrics": {"total_tokens": 999},
        }
        case = {
            "gold_set_complete": True,
            "gold_findings": [
                {
                    "id": "correlation",
                    "category": "bug",
                    "severity": "High",
                    "comment": "Multiple requests are correlated in reverse order",
                },
                {
                    "id": "naming",
                    "category": "style",
                    "severity": "Low",
                    "comment": "The variable name is unclear",
                },
            ],
        }
        judgment = {
            "evaluations": [
                {"gold_id": "correlation", "candidate_id": "web-01/c1", "match": True, "confidence": 0.95},
                {"gold_id": "correlation", "candidate_id": "web-01/c2", "match": False, "confidence": 0.99},
                {"gold_id": "naming", "candidate_id": "web-01/c1", "match": False, "confidence": 0.99},
                {"gold_id": "naming", "candidate_id": "web-01/c2", "match": True, "confidence": 0.95},
            ],
            "metrics": {"total_tokens": 77},
        }
        score = benchmark.score_result(
            manifest,
            result,
            case,
            "✓ discover (120 tok)\n✓ verify (80 tok)\n",
            judgment=judgment,
            profile="core",
        )
        self.assertEqual(score["validation_errors"], [])
        self.assertEqual(score["coverage"]["line_coverage"], 1.0)
        self.assertEqual(score["quality"]["gold_recall"], 1.0)
        self.assertEqual(score["quality"]["gold_precision"], 1.0)
        self.assertEqual(score["quality"]["excluded_match_findings"], ["web-01/c2"])
        self.assertEqual(score["token_efficiency"]["reviewer_tokens"], 200)
        self.assertEqual(score["token_efficiency"]["judge_tokens"], 77)
        self.assertEqual(score["token_efficiency"]["reviewer_tokens_per_1k_covered_changed_lines"], 20_000)

        case["gold_set_complete"] = False
        partial = benchmark.score_result(manifest, result, case)
        self.assertFalse(partial["quality"]["scored"])
        self.assertNotIn("gold_precision", partial["quality"])

    def test_score_counts_unmatched_confirmed_finding_as_false_positive(self):
        manifest = {
            "units": [{"id": "web-01", "files": ["web/app.ts"], "changed_lines": 2}],
            "files": [{"path": "web/app.ts", "added_lines": [7, 8], "eligible": True}],
            "counts": {"eligible_changed_lines": 2},
        }
        findings = [
            {"id": "c1", "path": "web/app.ts", "line": 7},
            {"id": "c2", "path": "web/app.ts", "line": 8},
        ]
        result = {
            "units": [{"unit_id": "web-01", "status": "complete", "files_reviewed": ["web/app.ts"]}],
            "findings": findings,
            "verifier_status": "complete",
        }
        case = {
            "gold_set_complete": True,
            "gold_findings": [{"id": "g1", "category": "bug", "severity": "High", "comment": "bug"}],
        }
        judgment = {
            "evaluations": [
                {"gold_id": "g1", "candidate_id": "c1", "match": True},
                {"gold_id": "g1", "candidate_id": "c2", "match": False},
            ]
        }
        score = benchmark.score_result(manifest, result, case, judgment=judgment)
        self.assertEqual(score["quality"]["true_positives"], 1)
        self.assertEqual(score["quality"]["false_positives"], 1)
        self.assertEqual(score["quality"]["gold_precision"], 0.5)
        self.assertEqual(score["quality"]["gold_recall"], 1.0)

    def test_judgment_validation_requires_every_pair(self):
        errors = benchmark.validate_judgment(
            [{"id": "g1"}],
            [{"id": "c1"}, {"id": "c2"}],
            {"evaluations": [{"gold_id": "g1", "candidate_id": "c1", "match": True}]},
        )
        self.assertEqual(errors, ["missing judgment pair: g1/c2"])

    def test_apply_verification_resumes_without_repeating_discovery(self):
        result = {
            "candidates": [{"id": "c1", "title": "bug"}, {"id": "c2", "title": "noise"}],
            "findings": [],
            "rejected_findings": [],
            "unverified_findings": [{"id": "c1"}, {"id": "c2"}],
            "verifier_status": "skipped_budget",
            "metrics": {"agent_calls": 4, "total_tokens": 100},
        }
        verification = {
            "decisions": [
                {"id": "c1", "verdict": "confirmed", "confidence": 0.8, "reason": "causal path"},
                {"id": "c2", "verdict": "rejected", "confidence": 0.9, "reason": "speculative"},
            ],
            "metrics": {"total_tokens": 20},
        }
        merged = benchmark.apply_verification(result, verification)
        self.assertEqual([finding["id"] for finding in merged["findings"]], ["c1"])
        self.assertEqual([finding["id"] for finding in merged["rejected_findings"]], ["c2"])
        self.assertEqual(merged["unverified_findings"], [])
        self.assertEqual(merged["verifier_status"], "complete_resumed")
        self.assertEqual(merged["metrics"], {"agent_calls": 5, "total_tokens": 120})

    def test_validation_rejects_silent_unit_loss_and_non_added_anchor(self):
        manifest = {
            "units": [{"id": "web-01", "files": ["web/app.ts"], "changed_lines": 1}],
            "files": [{"path": "web/app.ts", "added_lines": [7], "eligible": True}],
            "counts": {"eligible_changed_lines": 1},
        }
        result = {"units": [], "findings": [{"path": "web/app.ts", "line": 8}]}
        errors = benchmark.validate_result(manifest, result)
        self.assertIn("missing unit: web-01", errors)
        self.assertIn("finding is not anchored to an added line: web/app.ts:8", errors)

        failed = {"units": [{"unit_id": "web-01", "status": "failed", "files_reviewed": []}], "findings": []}
        self.assertIn("unit web-01 did not complete: 'failed'", benchmark.validate_result(manifest, failed))
        failed_score = benchmark.score_result(
            manifest,
            failed,
            {"gold_set_complete": True, "gold_findings": [{"id": "g1", "category": "bug"}]},
        )
        self.assertFalse(failed_score["quality"]["scored"])
        self.assertEqual(failed_score["quality"]["unscored_reason"], "review_protocol_invalid")
        self.assertNotIn("gold_recall", failed_score["quality"])
        self.assertFalse(failed_score["delivery"]["clean_conclusion_allowed"])

        skipped = {
            "units": [{"unit_id": "web-01", "status": "complete", "files_reviewed": ["web/app.ts"]}],
            "findings": [],
            "unverified_findings": [{"id": "c1"}],
            "verifier_status": "skipped_budget",
        }
        skipped_score = benchmark.score_result(manifest, skipped, {"gold_set_complete": False})
        self.assertFalse(skipped_score["delivery"]["complete"])
        self.assertEqual(skipped_score["delivery"]["unverified_findings"], 1)
        self.assertEqual(skipped_score["quality"]["unscored_reason"], "review_delivery_incomplete")


if __name__ == "__main__":
    unittest.main()
