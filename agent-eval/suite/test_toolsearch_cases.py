import copy
import json
import sys
import unittest
from pathlib import Path


HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import toolsearch_cases


class ToolSearchCaseMatrixTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.raw = json.loads(toolsearch_cases.DEFAULT_MATRIX.read_text())
        cls.base = json.loads(toolsearch_cases.DEFAULT_BASE_SUITE.read_text())

    def validate(self, mutate=None):
        document = copy.deepcopy(self.raw)
        if mutate is not None:
            mutate(document)
        return toolsearch_cases.validate_suite(document, self.base)

    def test_full_matrix_validates_and_has_unique_coverage(self):
        suite = self.validate()
        cases = suite["cases"]
        ids = [case["id"] for case in cases]

        self.assertEqual(16, len(cases))
        self.assertEqual(len(ids), len(set(ids)))
        self.assertGreaterEqual(sum(case["critical"] for case in cases), 10)
        self.assertEqual({"acp", "web"}, {case["surface"] for case in cases})
        self.assertEqual(
            {10, 30, 50, 100},
            {case["mcp_fixture"]["tool_count"] for case in cases if "mcp_fixture" in case},
        )

        categories = {case["category"] for case in cases}
        for required in (
            "toolsearch-direct",
            "toolsearch-exact",
            "toolsearch-semantic-en",
            "toolsearch-semantic-zh",
            "toolsearch-multi-target",
            "toolsearch-complex-args",
            "toolsearch-mcp-scale",
            "toolsearch-computer",
            "toolsearch-browser",
            "toolsearch-negative-search",
        ):
            self.assertIn(required, categories)

    def test_mcp_catalog_and_distractor_sentinels_materialize(self):
        cases = {case["id"]: case for case in self.validate()["cases"]}
        for count in (10, 30, 50, 100):
            case = cases[f"ts_mcp_catalog_{count}"]
            catalog = case["routing"]["deferred_tools"]
            target = "mcp__toolsearch_fixture__catalog_lookup_precise"
            self.assertEqual(count, len(catalog))
            self.assertEqual(count, len(set(catalog)))
            self.assertIn(target, catalog)
            self.assertNotIn(toolsearch_cases.MCP_CATALOG_SENTINEL, catalog)

            for variant in ("static", "deferred"):
                forbidden = case["expected_routing"][variant]["forbidden_tool_calls"]
                self.assertNotIn(toolsearch_cases.MCP_DISTRACTOR_SENTINEL, forbidden)
                self.assertEqual(count - 1, sum(name in forbidden for name in catalog if name != target))

    def test_computer_reuses_deterministic_base_and_browser_is_web_only(self):
        cases = {case["id"]: case for case in self.validate()["cases"]}
        computer = cases["ts_computer_notes_click"]
        browser = cases["ts_browser_loopback_read"]

        self.assertNotIn("base_case", computer)
        self.assertTrue(computer["home_config"]["computer"]["enabled"])
        fixture = computer["home_fixtures"][".jcode/computer/fixture.json"]
        self.assertIn("com.apple.Notes", fixture)
        self.assertTrue(any(oracle["type"] == "home_file_contains" for oracle in computer["oracles"]))
        self.assertEqual("acp", computer["surface"])

        self.assertEqual("web", browser["surface"])
        self.assertEqual("driver_owned_proof_form", browser["browser_fixture"]["kind"])
        self.assertEqual("web_browser_driver", browser["browser_fixture"]["prompt_owner"])
        self.assertEqual(
            [
                "browser_open",
                "browser_snapshot",
                "browser_act:fill",
                "browser_act:click",
                "browser_read",
            ],
            browser["browser_fixture"]["required_actions"],
        )
        self.assertNotIn("body", browser["browser_fixture"])
        self.assertEqual(
            {"navigate": "always_allow", "interact": "always_allow"},
            browser["home_config"]["browser"]["approval"],
        )
        self.assertIn("{BROWSER_FIXTURE_URL}", browser["prompt"])
        self.assertNotRegex(json.dumps(browser), r"https?://")
        self.assertFalse(self.raw["runner_contract"]["browser_on_acp"])

    def test_browser_expectations_cover_every_driver_tool_and_activation(self):
        browser = {
            case["id"]: case for case in self.validate()["cases"]
        }["ts_browser_loopback_read"]
        expected_names = {
            "browser_open", "browser_snapshot", "browser_act", "browser_read",
        }
        for variant in ("static", "deferred"):
            expectation = browser["expected_routing"][variant]
            required = {call["name"]: call for call in expectation["required_tool_calls"]}
            self.assertEqual(expected_names, set(required))
            self.assertEqual((2, 4), (required["browser_act"]["min"], required["browser_act"]["max"]))
            self.assertEqual(2, expectation["required_call_order"].count("browser_act"))
        self.assertEqual(
            expected_names,
            set(browser["expected_routing"]["deferred"]["expected_search_tools"]),
        )

    def test_rejects_browser_driver_or_preapproval_contract_drift(self):
        mutations = (
            lambda case: case["home_config"]["browser"]["approval"].update(
                {"navigate": "ask"},
            ),
            lambda case: case["browser_fixture"]["required_actions"].pop(),
            lambda case: case["expected_routing"]["deferred"][
                "expected_search_tools"
            ].remove("browser_act"),
        )
        for index, mutation in enumerate(mutations):
            def mutate(document, apply=mutation):
                browser = next(
                    case for case in document["cases"]
                    if case["id"] == "ts_browser_loopback_read"
                )
                apply(browser)

            with self.subTest(index=index):
                with self.assertRaises(toolsearch_cases.MatrixError):
                    self.validate(mutate)

    def test_variant_expectations_are_explicit_and_separate_activation_is_pinned(self):
        for case in self.validate()["cases"]:
            self.assertEqual(set(case["variants"]), set(case["expected_routing"]))
            for variant, expected in case["expected_routing"].items():
                if variant == "static":
                    self.assertEqual({"min": 0, "max": 0}, expected["search_calls"])
                    self.assertIn("tool_search", expected["forbidden_tool_calls"])
                if expected["activation_boundary"] == "strict_separate_batch":
                    self.assertEqual(0, expected["bypass_max"])
                    self.assertEqual(0, expected["same_batch_max"])
                    self.assertGreaterEqual(expected["search_calls"]["min"], 1)

    def test_rejects_duplicate_case_id(self):
        def mutate(document):
            duplicate = copy.deepcopy(document["cases"][0])
            document["cases"].append(duplicate)

        with self.assertRaisesRegex(toolsearch_cases.MatrixError, "duplicate ToolSearch case ids"):
            self.validate(mutate)

    def test_rejects_mcp_process_or_credential_injection(self):
        for injected_key, value in (
            ("command", "/bin/false"),
            ("env", {"X": "Y"}),
            ("api_key", "CANARY"),
        ):
            def mutate(document, key=injected_key, injected=value):
                document["cases"][8]["mcp_fixture"][key] = injected

            with self.subTest(injected_key=injected_key):
                with self.assertRaises(toolsearch_cases.MatrixError):
                    self.validate(mutate)

    def test_rejects_external_url_and_browser_on_acp(self):
        def external_url(document):
            document["cases"][13]["prompt"] = "Open https://example.invalid"

        with self.assertRaisesRegex(toolsearch_cases.MatrixError, "external URL"):
            self.validate(external_url)

        def browser_acp(document):
            document["cases"][13]["surface"] = "acp"

        with self.assertRaisesRegex(toolsearch_cases.MatrixError, "ACP case cannot|Browser fixture must use"):
            self.validate(browser_acp)

    def test_rejects_hard_gate_drift_and_unknown_base_case(self):
        def drift(document):
            document["hard_gates"]["deferred_argument_success_rate"]["threshold"] = 0.97

        with self.assertRaisesRegex(toolsearch_cases.MatrixError, "drifted"):
            self.validate(drift)

        def unknown_base(document):
            document["cases"][12]["base_case"] = "missing_computer_fixture"

        with self.assertRaisesRegex(toolsearch_cases.MatrixError, "unknown base_case"):
            self.validate(unknown_base)


if __name__ == "__main__":
    unittest.main()
