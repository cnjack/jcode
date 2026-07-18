import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import orchestrate
import routing_verify


TARGET = "mcp__toolsearch_fixture__catalog_lookup_precise"
RAW_TARGET = "catalog_lookup_precise"
ARGS = {"request_id": "req-42", "query": "sku-42", "limit": 3}


def tool_call(name, call_id, batch_id, batch_index=0, batch_size=1, args=None):
    entry = {
        "type": "tool_call",
        "name": name,
        "args": json.dumps(args or {}, separators=(",", ":")),
        "tool_call_id": call_id,
        "batch_id": batch_id,
        "batch_size": batch_size,
    }
    # Match Go's session JSON: batch_index=0 is omitted by `omitempty`.
    if batch_index:
        entry["batch_index"] = batch_index
    return entry


def tool_result(name, call_id, output, error="", denied=False):
    return {
        "type": "tool_result",
        "name": name,
        "output": output,
        "error": error,
        "denied": denied,
        "tool_call_id": call_id,
    }


def search_result(target=TARGET):
    return json.dumps({"matches": [target]}, separators=(",", ":"))


def fixture_result(marker, arguments=ARGS):
    return json.dumps({
        "content": [{"type": "text", "text": marker}],
        "structuredContent": {
            "status": "found",
            "complete": True,
            "authoritative": True,
            "request_id": arguments["request_id"],
            "query": arguments["query"],
            "requested_limit": arguments["limit"],
            "record": {
                "external_sku": arguments["query"],
                "source": "jcode-toolsearch-fixture",
            },
            "marker": marker,
        },
    }, separators=(",", ":"))


class RoutingVerifierTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def write_jsonl(self, name, entries):
        path = self.root / name
        path.write_text("".join(json.dumps(entry, separators=(",", ":")) + "\n" for entry in entries))
        return path

    def spec(self, expected_args=None):
        spec = {
            "deferred_tools": [TARGET],
            "fixture_tools": {TARGET: RAW_TARGET},
        }
        if expected_args is not None:
            spec["expected_calls"] = [{"tool": TARGET, "args": expected_args}]
        return spec

    def routing_only_spec(self):
        return {"deferred_tools": [TARGET]}

    def fixture_log(self, arguments=ARGS, marker=None):
        marker = marker or routing_verify._fixture_marker(RAW_TARGET, arguments)
        return self.write_jsonl("fixture.jsonl", [{
            "sequence": 1,
            "tool": RAW_TARGET,
            "arguments": arguments,
            "marker": marker,
        }])

    def target_pair(self, batch_id="target-batch", args=ARGS, marker=None):
        marker = marker or routing_verify._fixture_marker(RAW_TARGET, args)
        return [
            tool_call(TARGET, "target-1", batch_id, args=args),
            tool_result(TARGET, "target-1", fixture_result(marker)),
        ]

    def search_pair(self, call_id="search-1", batch_id="search-batch"):
        return [
            tool_call("tool_search", call_id, batch_id,
                      args={"query": f"select:{TARGET}"}),
            tool_result("tool_search", call_id, search_result()),
        ]

    def violation_types(self, result):
        return [item["type"] for item in result["violations"]]

    def test_normal_activation_and_fixture_evidence_pass(self):
        session = self.write_jsonl("session.jsonl", self.search_pair() + self.target_pair())
        result = routing_verify.verify_routing(session, self.fixture_log(), self.spec(ARGS))

        self.assertTrue(result["passed"], result["violations"])
        self.assertEqual(0, result["counts"]["bypass"])
        self.assertEqual(0, result["counts"]["same_batch_activation"])
        self.assertEqual(1, result["counts"]["fixture_matches"])
        self.assertNotIn(f"select:{TARGET}", json.dumps(result))

    def test_static_arm_keeps_fixture_marker_cross_check_without_activation(self):
        session = self.write_jsonl("session.jsonl", self.target_pair())
        result = routing_verify.verify_routing(
            session, self.fixture_log(), self.spec(ARGS), require_activation=False,
        )

        self.assertTrue(result["passed"], result["violations"])
        self.assertEqual(0, result["counts"]["search_calls"])
        self.assertEqual(0, result["counts"]["bypass"])
        self.assertEqual(1, result["counts"]["fixture_matches"])

    def test_deferred_call_before_search_is_bypass(self):
        session = self.write_jsonl("session.jsonl", self.target_pair() + self.search_pair())
        result = routing_verify.verify_routing(session, self.fixture_log(), self.spec(ARGS))

        self.assertFalse(result["passed"])
        self.assertIn("bypass", self.violation_types(result))
        self.assertEqual(1, result["counts"]["bypass"])

    def test_search_and_target_in_same_batch_is_rejected(self):
        marker = routing_verify._fixture_marker(RAW_TARGET, ARGS)
        session = self.write_jsonl("session.jsonl", [
            tool_call("tool_search", "search-1", "batch-1", 0, 2,
                      {"query": f"select:{TARGET}"}),
            tool_call(TARGET, "target-1", "batch-1", 1, 2, ARGS),
            tool_result("tool_search", "search-1", search_result()),
            tool_result(TARGET, "target-1", fixture_result(marker)),
        ])
        result = routing_verify.verify_routing(session, self.fixture_log(), self.spec(ARGS))

        self.assertFalse(result["passed"])
        self.assertIn("same_batch_activation", self.violation_types(result))
        self.assertIn("bypass", self.violation_types(result))
        self.assertEqual(1, result["counts"]["same_batch_activation"])

    def test_search_with_no_new_activation_is_redundant(self):
        entries = self.search_pair("search-1", "search-batch-1")
        entries += self.search_pair("search-2", "search-batch-2")
        entries += self.target_pair()
        session = self.write_jsonl("session.jsonl", entries)
        result = routing_verify.verify_routing(session, self.fixture_log(), self.spec(ARGS))

        self.assertFalse(result["passed"])
        self.assertIn("redundant_search", self.violation_types(result))
        self.assertEqual(1, result["counts"]["redundant_search"])

    def test_fixture_args_and_marker_mismatch_is_rejected(self):
        wrong_args = {**ARGS, "query": "wrong-sku"}
        session = self.write_jsonl("session.jsonl", self.search_pair() + self.target_pair())
        result = routing_verify.verify_routing(
            session, self.fixture_log(wrong_args), self.spec(ARGS),
        )

        self.assertFalse(result["passed"])
        types = self.violation_types(result)
        self.assertIn("fixture_session_mismatch", types)
        self.assertIn("unexpected_fixture_call", types)

    def test_strict_call_result_pairing_rejects_name_mismatch(self):
        entries = self.search_pair()
        marker = routing_verify._fixture_marker(RAW_TARGET, ARGS)
        entries += [
            tool_call(TARGET, "target-1", "target-batch", args=ARGS),
            tool_result("mcp__toolsearch_fixture__wrong", "target-1", fixture_result(marker),
                        error="fixture endpoint failed"),
        ]
        session = self.write_jsonl("session.jsonl", entries)
        result = routing_verify.verify_routing(session, self.fixture_log(), self.spec(ARGS))

        self.assertFalse(result["passed"])
        self.assertIn("tool_result_name_mismatch", self.violation_types(result))
        self.assertIn("deferred_call_failed", self.violation_types(result))

    def test_orphan_tool_call_without_result_is_rejected(self):
        entries = self.search_pair() + [
            tool_call(TARGET, "orphan-call", "target-batch", args=ARGS),
        ]
        session = self.write_jsonl("session.jsonl", entries)
        result = routing_verify.verify_routing(
            session, self.root / "unused-fixture.jsonl", self.routing_only_spec(),
        )

        self.assertFalse(result["passed"])
        self.assertIn("missing_tool_result", self.violation_types(result))
        self.assertEqual(1, result["counts"]["deferred_calls"])
        self.assertEqual(0, result["counts"]["deferred_call_success"])

    def test_orphan_tool_result_before_its_call_is_rejected(self):
        marker = routing_verify._fixture_marker(RAW_TARGET, ARGS)
        entries = self.search_pair() + [
            tool_result(TARGET, "orphan-result", fixture_result(marker)),
            tool_call(TARGET, "orphan-result", "target-batch", args=ARGS),
        ]
        session = self.write_jsonl("session.jsonl", entries)
        result = routing_verify.verify_routing(
            session, self.root / "unused-fixture.jsonl", self.routing_only_spec(),
        )

        self.assertFalse(result["passed"])
        types = self.violation_types(result)
        self.assertIn("orphan_tool_result", types)
        self.assertIn("missing_tool_result", types)
        self.assertEqual(1, result["counts"]["paired_tool_calls"])

    def test_agent_visible_folded_failure_is_not_counted_as_success(self):
        entries = self.search_pair()
        entries += [
            tool_call(TARGET, "target-1", "target-batch", args=ARGS),
            tool_result(TARGET, "target-1", "Tool execution failed: deterministic fixture error"),
        ]
        session = self.write_jsonl("session.jsonl", entries)
        result = routing_verify.verify_routing(session, self.fixture_log(), self.spec(ARGS))

        self.assertFalse(result["passed"])
        self.assertIn("deferred_call_failed", self.violation_types(result))
        self.assertEqual(0, result["counts"]["deferred_call_success"])


class MCPFixtureInjectionTest(unittest.TestCase):
    def test_injection_is_local_and_has_no_credential_fields(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            binary = root / "mcp-fixture"
            binary.write_text("fixture")
            binary.chmod(0o700)
            case = {"mcp_fixture": {"server_name": "toolsearch_fixture", "tool_count": 50}}

            config, runtime = orchestrate.inject_mcp_fixture({}, case, str(binary), root / "run")

            server = config["mcp_servers"]["toolsearch_fixture"]
            self.assertEqual({"type", "command", "args"}, set(server))
            self.assertEqual(str(binary.resolve()), server["command"])
            self.assertEqual(50, runtime["tool_count"])
            rendered = json.dumps(config).lower()
            for forbidden in ("api_key", "authorization", "headers", "token", "secret", "password"):
                self.assertNotIn(forbidden, rendered)

    def test_injection_rejects_case_supplied_command_or_env(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            binary = root / "mcp-fixture"
            binary.write_text("fixture")
            binary.chmod(0o700)
            for field, value in (("command", "/tmp/other"), ("env", ["TOKEN=x"]), ("headers", {"Authorization": "x"})):
                case = {"mcp_fixture": {"tool_count": 10, field: value}}
                with self.subTest(field=field):
                    with self.assertRaises(ValueError):
                        orchestrate.inject_mcp_fixture({}, case, str(binary), root / "run")


if __name__ == "__main__":
    unittest.main()
