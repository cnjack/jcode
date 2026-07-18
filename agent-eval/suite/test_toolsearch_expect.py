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

    def verify(self, entries, expected, fixture_scope=None):
        path = self.root / "session.json"
        path.write_text("".join(json.dumps(entry) + "\n" for entry in entries))
        return toolsearch_expect.verify_expectation(
            path, expected, fixture_scope=fixture_scope,
        )

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

    def test_query_mode_contract_remains_independent_from_successful_match(self):
        entries = [
            call("tool_search", "search-1", {
                "query": "read the current persistent objective",
            }, "search-batch"),
            result("tool_search", "search-1", json.dumps({"matches": [TARGET]})),
            call(TARGET, "target-1", {}, "target-batch"),
            result(TARGET, "target-1"),
        ]
        select_only = self.verify(entries, expectation(deferred=True))
        self.assertFalse(select_only["passed"])
        self.assertIn("search_query_mode", self.violation_types(select_only))

        both = expectation(deferred=True)
        both["search_query_modes"] = ["select", "keyword"]
        accepted = self.verify(entries, both)
        self.assertTrue(accepted["passed"])
        self.assertEqual(1, accepted["counts"]["search_mode_keyword"])

    def test_exact_query_contract_is_private_and_independent_from_mode(self):
        expected = expectation(deferred=True)
        expected["search_query_matcher"] = {
            "match": "exact",
            "value": "select:goal_get",
        }
        accepted = self.verify(self.deferred_entries(), expected)
        self.assertTrue(accepted["passed"])
        self.assertEqual(1, accepted["counts"]["search_query_matches"])

        entries = self.deferred_entries()
        entries[0] = call("tool_search", "search-1", {
            "query": "select:goal_get,goal_update",
        }, "search-batch")
        rejected = self.verify(entries, expected)
        self.assertFalse(rejected["passed"])
        self.assertEqual(1, rejected["counts"]["search_query_mismatches"])
        self.assertIn("search_query_mismatch", self.violation_types(rejected))
        self.assertNotIn("goal_update", json.dumps(rejected))

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

    def test_fixture_path_accepts_only_equivalent_runner_owned_spellings(self):
        box = self.root / "box"
        box.mkdir()
        fixture = box / "direct_fixture.txt"
        fixture.write_text("DIRECT_TOOL_OK\n")
        scope = toolsearch_expect.build_fixture_scope(
            box, {"direct_fixture.txt": "ignored-content"},
        )
        expected = expectation(matcher={
            "match": "fixture_path",
            "value": {"file_path": "direct_fixture.txt"},
        })
        expected["required_tool_calls"][0]["name"] = "read"
        expected["required_call_order"] = ["read"]
        for spelling in (
            "direct_fixture.txt",
            "./direct_fixture.txt",
            str(fixture.resolve()),
        ):
            with self.subTest(spelling=spelling):
                verdict = self.verify(
                    [
                        call("read", "target-1", {
                            "file_path": spelling,
                            "limit": 20,
                        }, "target-batch"),
                        result("read", "target-1"),
                    ],
                    expected,
                    fixture_scope=scope,
                )
                self.assertTrue(verdict["passed"])
                self.assertEqual(1, verdict["counts"]["argument_matches"])

    def test_fixture_path_rejects_untrusted_or_ambiguous_paths_without_leaking(self):
        box = self.root / "box"
        box.mkdir()
        fixture = box / "direct_fixture.txt"
        fixture.write_text("DIRECT_TOOL_OK\n")
        nested = box / "nested"
        nested.mkdir()
        collision = nested / "direct_fixture.txt"
        collision.write_text("WRONG\n")
        outside = self.root / "direct_fixture.txt"
        outside.write_text("WRONG\n")
        scope = toolsearch_expect.build_fixture_scope(box, ["direct_fixture.txt"])
        expected = expectation(matcher={
            "match": "fixture_path",
            "value": {"file_path": "direct_fixture.txt"},
        })
        expected["required_tool_calls"][0]["name"] = "read"
        expected["required_call_order"] = ["read"]
        rejected = (
            "nested/direct_fixture.txt",
            "nested/../direct_fixture.txt",
            "../direct_fixture.txt",
            str(collision.resolve()),
            str(outside.resolve()),
            17,
            "bad\x00path",
        )
        rendered_verdicts = []
        for spelling in rejected:
            with self.subTest(spelling=repr(spelling)):
                verdict = self.verify(
                    [
                        call("read", "target-1", {"file_path": spelling}, "target-batch"),
                        result("read", "target-1"),
                    ],
                    expected,
                    fixture_scope=scope,
                )
                self.assertFalse(verdict["passed"])
                self.assertIn("argument_mismatch", self.violation_types(verdict))
                rendered_verdicts.append(json.dumps(verdict))
        published = "".join(rendered_verdicts)
        self.assertNotIn(str(outside.resolve()), published)
        self.assertNotIn("nested/../direct_fixture.txt", published)
        self.assertNotIn("bad\\u0000path", published)

        missing_scope = self.verify(
            [
                call("read", "target-1", {"file_path": str(fixture)}, "target-batch"),
                result("read", "target-1"),
            ],
            expected,
        )
        undeclared = expectation(matcher={
            "match": "fixture_path",
            "value": {"file_path": "missing.txt"},
        })
        missing_target = self.verify(
            [
                call("read", "target-1", {"file_path": str(fixture)}, "target-batch"),
                result("read", "target-1"),
            ],
            undeclared,
            fixture_scope=scope,
        )
        self.assertFalse(missing_scope["passed"])
        self.assertFalse(missing_target["passed"])

        fixture.unlink()
        try:
            fixture.symlink_to(outside)
        except OSError:
            pass
        else:
            replaced = self.verify(
                [
                    call("read", "target-1", {
                        "file_path": "direct_fixture.txt",
                    }, "target-batch"),
                    result("read", "target-1"),
                ],
                expected,
                fixture_scope=scope,
            )
            self.assertFalse(replaced["passed"])
            self.assertIn("invalid_fixture_scope", self.violation_types(replaced))

    def test_fixture_scope_fails_closed_for_unsafe_or_missing_targets(self):
        box = self.root / "box"
        box.mkdir()
        (box / "direct_fixture.txt").write_text("DIRECT_TOOL_OK\n")
        for paths in (
            ["../direct_fixture.txt"],
            [str((box / "direct_fixture.txt").resolve())],
            ["missing.txt"],
        ):
            with self.subTest(paths=paths):
                with self.assertRaises(toolsearch_expect.FixtureScopeError):
                    toolsearch_expect.build_fixture_scope(box, paths)

        real = box / "real.txt"
        real.write_text("REAL\n")
        for name, target in (
            ("inside-link.txt", real),
            ("outside-link.txt", self.root / "outside.txt"),
        ):
            target.write_text("REAL\n")
            link = box / name
            try:
                link.symlink_to(target)
            except OSError:
                continue
            with self.subTest(name=name):
                with self.assertRaises(toolsearch_expect.FixtureScopeError):
                    toolsearch_expect.build_fixture_scope(box, [name])

        for paths in (["./direct_fixture.txt"], ["."], ["direct_fixture.txt", "./direct_fixture.txt"]):
            with self.subTest(paths=paths):
                with self.assertRaises(toolsearch_expect.FixtureScopeError):
                    toolsearch_expect.build_fixture_scope(box, paths)

    def test_fixture_scope_is_required_even_when_matching_call_is_optional(self):
        box = self.root / "box"
        box.mkdir()
        (box / "direct_fixture.txt").write_text("DIRECT_TOOL_OK\n")
        scope = toolsearch_expect.build_fixture_scope(box, ["direct_fixture.txt"])
        matcher = {
            "match": "fixture_path",
            "value": {"file_path": "direct_fixture.txt"},
        }
        for label in ("required_tool_calls", "optional_tool_calls"):
            expected = expectation()
            expected["required_tool_calls"] = []
            expected["optional_tool_calls"] = []
            expected[label] = [{
                "name": "read", "min": 0, "max": 1, "args": matcher,
            }]
            expected["required_call_order"] = []
            with self.subTest(label=label):
                missing = self.verify([], expected)
                self.assertFalse(missing["passed"])
                self.assertIn("invalid_fixture_scope", self.violation_types(missing))
                present = self.verify([], expected, fixture_scope=scope)
                self.assertTrue(present["passed"])

    def test_fixture_matcher_rejects_non_read_or_non_file_path_contracts(self):
        box = self.root / "box"
        box.mkdir()
        (box / "direct_fixture.txt").write_text("DIRECT_TOOL_OK\n")
        scope = toolsearch_expect.build_fixture_scope(box, ["direct_fixture.txt"])
        for tool_name, value in (
            ("grep", {"file_path": "direct_fixture.txt"}),
            ("read", {"other": "direct_fixture.txt"}),
            ("read", {
                "file_path": "direct_fixture.txt",
                "other": "direct_fixture.txt",
            }),
        ):
            expected = expectation(matcher={
                "match": "fixture_path",
                "value": value,
            })
            expected["required_tool_calls"][0]["name"] = tool_name
            expected["required_call_order"] = [tool_name]
            with self.subTest(tool=tool_name, value=value):
                verdict = self.verify([], expected, fixture_scope=scope)
                self.assertFalse(verdict["passed"])
                self.assertIn("invalid_fixture_scope", self.violation_types(verdict))

    def test_fixture_scope_detects_same_path_and_root_identity_replacement(self):
        box = self.root / "box"
        box.mkdir()
        fixture = box / "direct_fixture.txt"
        fixture.write_text("DIRECT_TOOL_OK\n")
        scope = toolsearch_expect.build_fixture_scope(box, ["direct_fixture.txt"])
        expected = expectation(matcher={
            "match": "fixture_path",
            "value": {"file_path": "direct_fixture.txt"},
        })
        expected["required_tool_calls"][0]["name"] = "read"
        expected["required_call_order"] = ["read"]

        fixture.unlink()
        fixture.write_text("DIRECT_TOOL_OK\n")
        replaced_file = self.verify([], expected, fixture_scope=scope)
        self.assertFalse(replaced_file["passed"])
        self.assertIn("invalid_fixture_scope", self.violation_types(replaced_file))

        replacement_scope = toolsearch_expect.build_fixture_scope(box, ["direct_fixture.txt"])
        old_box = self.root / "old-box"
        box.rename(old_box)
        box.mkdir()
        (box / "direct_fixture.txt").write_text("DIRECT_TOOL_OK\n")
        replaced_root = self.verify([], expected, fixture_scope=replacement_scope)
        self.assertFalse(replaced_root["passed"])
        self.assertIn("invalid_fixture_scope", self.violation_types(replaced_root))

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
