import json
import sys
import tempfile
import unittest
from pathlib import Path


HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import toolsearch_expect


TARGET = "goal_get"


def call(name, call_id, args, batch, size=1, index=0):
    value = {
        "type": "tool_call",
        "name": name,
        "tool_call_id": call_id,
        "args": json.dumps(args),
        "batch_id": batch,
        "batch_size": size,
    }
    if index:
        value["batch_index"] = index
    return value


def result(name, call_id, output="OK", **extra):
    return {
        "type": "tool_result",
        "name": name,
        "tool_call_id": call_id,
        "output": output,
        **extra,
    }


def expectation(*, deferred=False, order=None, matcher=None):
    required = [{"name": TARGET, "min": 1, "max": 1}]
    if matcher is not None:
        required[0]["args"] = matcher
    return {
        "search_calls": {"min": 1 if deferred else 0, "max": 1 if deferred else 0},
        "search_query_modes": ["select"] if deferred else [],
        "expected_search_tools": [TARGET] if deferred else [],
        "empty_search": "forbidden",
        "activation_boundary": "strict_separate_batch" if deferred else "not_applicable",
        "bypass_max": 0,
        "same_batch_max": 0,
        "required_tool_calls": required,
        "optional_tool_calls": [],
        "forbidden_tool_calls": [] if deferred else ["tool_search"],
        "required_call_order": order or (["tool_search", TARGET] if deferred else [TARGET]),
    }


class ToolSearchExpectationTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def verify(self, entries, expected):
        path = self.root / "session.json"
        path.write_text("".join(json.dumps(entry) + "\n" for entry in entries))
        return toolsearch_expect.verify_expectation(path, expected)

    def deferred_entries(self, target_args=None):
        target_args = target_args or {}
        return [
            call("tool_search", "search-1", {"query": f"select:{TARGET}"}, "search-batch"),
            result("tool_search", "search-1", json.dumps({"matches": [TARGET]})),
            call(TARGET, "target-1", target_args, "target-batch"),
            result(TARGET, "target-1"),
        ]

    def violation_types(self, verdict):
        return {item["type"] for item in verdict["violations"]}

    def test_static_pass(self):
        verdict = self.verify(
            [call(TARGET, "target-1", {}, "target-batch"), result(TARGET, "target-1")],
            expectation(),
        )
        self.assertTrue(verdict["passed"])
        self.assertEqual(0, verdict["counts"]["search_calls"])
        self.assertEqual(1, verdict["counts"]["required_calls"])

    def test_deferred_pass_has_separate_activation_and_safe_counts(self):
        verdict = self.verify(self.deferred_entries(), expectation(deferred=True))
        self.assertTrue(verdict["passed"])
        self.assertEqual(1, verdict["counts"]["search_calls"])
        self.assertEqual(1, verdict["counts"]["matched_expected_search_tools"])
        self.assertEqual(1, verdict["counts"]["deferred_call_success"])
        self.assertEqual(0, verdict["counts"]["bypass"])
        self.assertEqual(0, verdict["counts"]["same_batch_activation"])

    def test_same_batch_search_and_target_fails(self):
        entries = [
            call("tool_search", "search-1", {"query": f"select:{TARGET}"}, "same", 2, 0),
            call(TARGET, "target-1", {}, "same", 2, 1),
            result("tool_search", "search-1", json.dumps({"matches": [TARGET]})),
            result(TARGET, "target-1"),
        ]
        verdict = self.verify(entries, expectation(deferred=True))
        self.assertFalse(verdict["passed"])
        self.assertEqual(1, verdict["counts"]["same_batch_activation"])
        self.assertIn("same_batch_activation", self.violation_types(verdict))

    def test_target_before_search_is_bypass(self):
        entries = [
            call(TARGET, "target-1", {}, "target-batch"),
            result(TARGET, "target-1"),
            call("tool_search", "search-1", {"query": f"select:{TARGET}"}, "search-batch"),
            result("tool_search", "search-1", json.dumps({"matches": [TARGET]})),
        ]
        verdict = self.verify(entries, expectation(deferred=True))
        self.assertFalse(verdict["passed"])
        self.assertEqual(1, verdict["counts"]["bypass"])
        self.assertIn("deferred_bypass", self.violation_types(verdict))

    def test_exact_argument_mismatch_fails_without_copying_arguments(self):
        expected = expectation(
            matcher={"match": "exact", "value": {"status": "complete"}},
        )
        verdict = self.verify(
            [
                call(TARGET, "target-1", {"status": "blocked"}, "target-batch"),
                result(TARGET, "target-1"),
            ],
            expected,
        )
        self.assertFalse(verdict["passed"])
        self.assertEqual(1, verdict["counts"]["argument_mismatches"])
        self.assertIn("argument_mismatch", self.violation_types(verdict))
        self.assertNotIn("blocked", json.dumps(verdict))

    def test_required_call_order_fails(self):
        expected = expectation(order=["alpha", "beta"])
        expected["required_tool_calls"] = [
            {"name": "alpha", "min": 1, "max": 1},
            {"name": "beta", "min": 1, "max": 1},
        ]
        entries = [
            call("beta", "b", {}, "batch-b"), result("beta", "b"),
            call("alpha", "a", {}, "batch-a"), result("alpha", "a"),
        ]
        verdict = self.verify(entries, expected)
        self.assertFalse(verdict["passed"])
        self.assertIn("required_call_order", self.violation_types(verdict))

    def test_orphan_result_fails_closed(self):
        entries = [
            result("other", "missing"),
            call(TARGET, "target-1", {}, "target-batch"),
            result(TARGET, "target-1"),
        ]
        verdict = self.verify(entries, expectation())
        self.assertFalse(verdict["passed"])
        self.assertEqual(1, verdict["counts"]["orphan_results"])
        self.assertIn("orphan_tool_result", self.violation_types(verdict))

    def test_secret_canary_never_appears_in_verdict_or_external_projection(self):
        secret = "sk-private-query-argument-output-canary-123456"
        expected = expectation(
            deferred=True,
            matcher={"match": "exact", "value": {"safe": True}},
        )
        entries = [
            call("tool_search", "search-1", {"query": f"select:{TARGET},{secret}"}, "search-batch"),
            result("tool_search", "search-1", json.dumps({"matches": [TARGET], "leak": secret})),
            call(TARGET, "target-1", {"secret": secret}, "target-batch"),
            result(TARGET, "target-1", secret),
        ]
        verdict = self.verify(entries, expected)
        projected = toolsearch_expect.sanitize_external_verdict({
            "passed": False,
            "counts": {"tool_calls": 1},
            "violations": [{
                "type": "fixture_session_mismatch",
                "detail": secret,
                "arguments": {"secret": secret},
            }],
        })
        rendered = json.dumps({"verdict": verdict, "projected": projected})
        self.assertNotIn(secret, rendered)
        self.assertNotIn("select:", rendered)
        self.assertNotIn('"safe"', rendered)
        self.assertNotIn('"detail"', rendered)


if __name__ == "__main__":
    unittest.main()
