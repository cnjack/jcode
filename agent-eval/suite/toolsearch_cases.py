#!/usr/bin/env python3
"""Load and validate the dedicated ToolSearch evaluation matrix.

The matrix is intentionally separate from the legacy autonomous-agent suite:
it has variant-specific routing expectations and one Web-only Browser case.
This module has no third-party dependencies.  The runner can consume
``load_suite(...)["cases"]``; materialization resolves the one reused Computer
fixture and expands deterministic MCP catalog sentinels into canonical names.
"""

import argparse
import copy
import json
import re
from pathlib import Path


HERE = Path(__file__).resolve().parent
DEFAULT_MATRIX = HERE / "toolsearch_testcases.json"
DEFAULT_BASE_SUITE = HERE / "testcases.json"

VARIANTS = {"static", "deferred"}
SURFACES = {"acp", "web"}
ACTIVATION_BOUNDARIES = {
    "not_applicable",
    "strict_separate_batch",
    "no_activation_expected",
}
QUERY_MODES = {"select", "keyword"}
EMPTY_SEARCH_POLICIES = {"forbidden", "allowed", "required"}
ARG_MATCHES = {"exact", "contains"}
MCP_TOOL_COUNTS = {10, 30, 50, 100}
MCP_CATALOG_SENTINEL = "$mcp_fixture_catalog"
MCP_DISTRACTOR_SENTINEL = "$mcp_fixture_distractors"
BROWSER_FIXTURE_KIND = "driver_owned_proof_form"
BROWSER_FIXTURE_ACTIONS = [
    "browser_open",
    "browser_snapshot",
    "browser_act:fill",
    "browser_act:click",
    "browser_read",
]
BROWSER_CONFIRMATION_PREFIX = "JCODE_BROWSER_CONFIRMATION"

CASE_ID_RE = re.compile(r"^[a-z][a-z0-9_]{2,79}$")
TOOL_NAME_RE = re.compile(r"^[A-Za-z0-9_.:-]+$")
SERVER_NAME_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$")
EXTERNAL_URL_RE = re.compile(r"https?://", re.IGNORECASE)

SIMILAR_MCP_TOOLS = (
    "catalog_lookup_precise",
    "catalog_lookup_inventory",
    "catalog_lookup_metadata",
    "catalog_lookup_policy",
    "catalog_lookup_preview",
    "catalog_lookup_price",
    "catalog_lookup_recent",
    "catalog_lookup_supplier",
    "catalog_search_precise",
    "customer_catalog_lookup",
)
MCP_TARGET = "catalog_lookup_precise"

METRIC_TAGS = {
    "task_pass",
    "paired_task_pass",
    "critical_pass",
    "direct_routing",
    "deferred_call_accuracy",
    "irrelevant_search",
    "mcp_distractor_accuracy",
    "schema_disclosure",
    "full_schema_disclosure",
    "negative_search",
    "browser_routing",
    "computer_routing",
}

# Cases are allowed to describe expected tool arguments, but never runtime
# credentials or process injection.  Exact-key matching avoids false positives
# for benign prose such as "no credentials".
FORBIDDEN_CASE_KEYS = {
    "api_key",
    "authorization",
    "headers",
    "password",
    "access_token",
    "refresh_token",
    "client_secret",
    "command",
    "env",
}

REQUIRED_HARD_GATES = {
    "deferred_bypass": ("sum", "eq", 0),
    "same_batch_activation": ("sum", "eq", 0),
    "deferred_argument_success_rate": ("ratio", "gte", 0.98),
    "irrelevant_search_rate": ("ratio", "lte", 0.02),
    "deferred_task_pass_rate": ("ratio", "gte", 0.95),
    "critical_deferred_pass_rate": ("ratio", "gte", 0.90),
    "paired_noninferiority": ("difference", "gte", -0.03),
    "normal_first_visible_tools": ("max", "lte", 12),
    "first_schema_token_reduction": ("ratio", "gte", 0.50),
}

REQUIRED_GATE_SCOPES = {
    "first_schema_token_reduction": {
        "metric_tag": "full_schema_disclosure",
    },
}


class MatrixError(ValueError):
    """Raised when a matrix cannot be safely or unambiguously evaluated."""


def canonical_mcp_name(server_name, raw_tool):
    return f"mcp__{server_name}__{raw_tool}"


def mcp_fixture_tool_names(server_name, count):
    """Mirror agent-eval/fixture/mcp's deterministic sorted catalog."""
    if count not in MCP_TOOL_COUNTS:
        raise MatrixError(f"unsupported MCP fixture tool_count: {count}")
    raw = list(SIMILAR_MCP_TOOLS)
    index = 1
    while len(raw) < count:
        raw.append(f"fixture_utility_{index:03d}")
        index += 1
    return [canonical_mcp_name(server_name, name) for name in sorted(raw)]


def mcp_fixture_distractors(server_name, count):
    target = canonical_mcp_name(server_name, MCP_TARGET)
    return [name for name in mcp_fixture_tool_names(server_name, count) if name != target]


def _read_json(path, label):
    try:
        value = json.loads(Path(path).read_text())
    except (OSError, json.JSONDecodeError) as exc:
        raise MatrixError(f"cannot read {label} {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise MatrixError(f"{label} must be a JSON object")
    return value


def _base_case_map(base_suite):
    cases = base_suite.get("cases")
    if not isinstance(cases, list):
        raise MatrixError("base suite cases must be an array")
    result = {}
    for index, case in enumerate(cases):
        if not isinstance(case, dict) or not isinstance(case.get("id"), str):
            raise MatrixError(f"base suite case {index} has no string id")
        if case["id"] in result:
            raise MatrixError(f"duplicate base suite case id: {case['id']}")
        result[case["id"]] = case
    return result


def _deep_merge(base, overlay):
    result = copy.deepcopy(base)
    for key, value in overlay.items():
        if key == "base_case":
            continue
        if isinstance(value, dict) and isinstance(result.get(key), dict):
            result[key] = _deep_merge(result[key], value)
        else:
            result[key] = copy.deepcopy(value)
    return result


def _expand_mcp_case(case):
    fixture = case.get("mcp_fixture")
    if not isinstance(fixture, dict):
        return case
    server = fixture.get("server_name")
    count = fixture.get("tool_count")
    catalog = mcp_fixture_tool_names(server, count)
    distractors = mcp_fixture_distractors(server, count)

    routing = case.get("routing")
    if isinstance(routing, dict) and routing.get("deferred_tools") == MCP_CATALOG_SENTINEL:
        routing["deferred_tools"] = catalog

    expectations = case.get("expected_routing", {})
    for expectation in expectations.values():
        if not isinstance(expectation, dict):
            continue
        forbidden = expectation.get("forbidden_tool_calls")
        if isinstance(forbidden, list) and MCP_DISTRACTOR_SENTINEL in forbidden:
            expanded = []
            for value in forbidden:
                expanded.extend(distractors if value == MCP_DISTRACTOR_SENTINEL else [value])
            expectation["forbidden_tool_calls"] = expanded
    return case


def materialize_cases(document, base_suite):
    base_cases = _base_case_map(base_suite)
    materialized = []
    for raw in document.get("cases", []):
        if not isinstance(raw, dict):
            materialized.append(raw)
            continue
        base_id = raw.get("base_case")
        if base_id is not None:
            if base_id not in base_cases:
                raise MatrixError(f"case {raw.get('id', '?')} references unknown base_case {base_id!r}")
            case = _deep_merge(base_cases[base_id], raw)
        else:
            case = copy.deepcopy(raw)
        materialized.append(_expand_mcp_case(case))
    result = copy.deepcopy(document)
    result["cases"] = materialized
    return result


def _walk_forbidden_keys(value, path="matrix"):
    if isinstance(value, dict):
        for key, child in value.items():
            if str(key).lower() in FORBIDDEN_CASE_KEYS:
                raise MatrixError(f"{path} contains forbidden key {key!r}")
            _walk_forbidden_keys(child, f"{path}.{key}")
    elif isinstance(value, list):
        for index, child in enumerate(value):
            _walk_forbidden_keys(child, f"{path}[{index}]")


def _walk_external_urls(value, path="matrix"):
    if isinstance(value, str) and EXTERNAL_URL_RE.search(value):
        raise MatrixError(f"{path} contains an external URL; use a runner fixture placeholder")
    if isinstance(value, dict):
        for key, child in value.items():
            _walk_external_urls(child, f"{path}.{key}")
    elif isinstance(value, list):
        for index, child in enumerate(value):
            _walk_external_urls(child, f"{path}[{index}]")


def _require_string(case, key):
    value = case.get(key)
    if not isinstance(value, str) or not value.strip():
        raise MatrixError(f"case {case.get('id', '?')} requires non-empty string {key}")
    return value


def _validate_path_map(case, key):
    value = case.get(key, {})
    if not isinstance(value, dict):
        raise MatrixError(f"case {case['id']} {key} must be an object")
    for path, content in value.items():
        if not isinstance(path, str) or not path or Path(path).is_absolute() or ".." in Path(path).parts:
            raise MatrixError(f"case {case['id']} has unsafe {key} path {path!r}")
        if not isinstance(content, str):
            raise MatrixError(f"case {case['id']} {key}[{path!r}] must be a string")


def _validate_count(case_id, label, value):
    if not isinstance(value, dict) or set(value) != {"min", "max"}:
        raise MatrixError(f"case {case_id} {label} must contain exactly min/max")
    minimum, maximum = value["min"], value["max"]
    if (not isinstance(minimum, int) or isinstance(minimum, bool)
            or not isinstance(maximum, int) or isinstance(maximum, bool)
            or minimum < 0 or maximum < minimum):
        raise MatrixError(f"case {case_id} {label} has invalid bounds")


def _validate_call(case_id, label, call):
    if not isinstance(call, dict):
        raise MatrixError(f"case {case_id} {label} entry must be an object")
    unknown = set(call) - {"name", "min", "max", "args"}
    if unknown:
        raise MatrixError(f"case {case_id} {label} entry has unknown fields: {sorted(unknown)}")
    name = call.get("name")
    if not isinstance(name, str) or not TOOL_NAME_RE.fullmatch(name):
        raise MatrixError(f"case {case_id} {label} has invalid tool name {name!r}")
    minimum, maximum = call.get("min"), call.get("max")
    if (not isinstance(minimum, int) or isinstance(minimum, bool)
            or not isinstance(maximum, int) or isinstance(maximum, bool)
            or minimum < 0 or maximum < minimum):
        raise MatrixError(f"case {case_id} {label} {name} has invalid min/max")
    if "args" in call:
        matcher = call["args"]
        if not isinstance(matcher, dict) or set(matcher) != {"match", "value"}:
            raise MatrixError(f"case {case_id} {label} {name} args must contain match/value")
        if matcher["match"] not in ARG_MATCHES or not isinstance(matcher["value"], dict):
            raise MatrixError(f"case {case_id} {label} {name} has invalid args matcher")


def _validate_expectation(case, variant, expectation):
    case_id = case["id"]
    allowed = {
        "search_calls",
        "search_query_modes",
        "expected_search_tools",
        "empty_search",
        "activation_boundary",
        "bypass_max",
        "same_batch_max",
        "required_tool_calls",
        "optional_tool_calls",
        "forbidden_tool_calls",
        "required_call_order",
    }
    if not isinstance(expectation, dict):
        raise MatrixError(f"case {case_id} expected_routing.{variant} must be an object")
    unknown = set(expectation) - allowed
    if unknown:
        raise MatrixError(f"case {case_id} expected_routing.{variant} has unknown fields: {sorted(unknown)}")

    _validate_count(case_id, f"expected_routing.{variant}.search_calls", expectation.get("search_calls"))
    modes = expectation.get("search_query_modes")
    if (not isinstance(modes, list) or len(modes) != len(set(modes))
            or not set(modes) <= QUERY_MODES):
        raise MatrixError(f"case {case_id} expected_routing.{variant} has invalid search_query_modes")
    searched = expectation.get("expected_search_tools")
    if (not isinstance(searched, list) or len(searched) != len(set(searched))
            or any(not isinstance(name, str) or not TOOL_NAME_RE.fullmatch(name) for name in searched)):
        raise MatrixError(f"case {case_id} expected_routing.{variant} has invalid expected_search_tools")
    if expectation.get("empty_search") not in EMPTY_SEARCH_POLICIES:
        raise MatrixError(f"case {case_id} expected_routing.{variant} has invalid empty_search")
    boundary = expectation.get("activation_boundary")
    if boundary not in ACTIVATION_BOUNDARIES:
        raise MatrixError(f"case {case_id} expected_routing.{variant} has invalid activation_boundary")
    for key in ("bypass_max", "same_batch_max"):
        value = expectation.get(key)
        if not isinstance(value, int) or isinstance(value, bool) or value < 0:
            raise MatrixError(f"case {case_id} expected_routing.{variant}.{key} must be non-negative integer")

    required = expectation.get("required_tool_calls")
    optional = expectation.get("optional_tool_calls")
    if not isinstance(required, list) or not isinstance(optional, list):
        raise MatrixError(f"case {case_id} expected_routing.{variant} call lists must be arrays")
    for index, call in enumerate(required):
        _validate_call(case_id, f"expected_routing.{variant}.required_tool_calls[{index}]", call)
    for index, call in enumerate(optional):
        _validate_call(case_id, f"expected_routing.{variant}.optional_tool_calls[{index}]", call)

    forbidden = expectation.get("forbidden_tool_calls")
    if (not isinstance(forbidden, list) or len(forbidden) != len(set(forbidden))
            or any(not isinstance(name, str) or not TOOL_NAME_RE.fullmatch(name) for name in forbidden)):
        raise MatrixError(f"case {case_id} expected_routing.{variant} has invalid forbidden_tool_calls")
    order = expectation.get("required_call_order")
    if (not isinstance(order, list)
            or any(not isinstance(name, str) or not TOOL_NAME_RE.fullmatch(name) for name in order)):
        raise MatrixError(f"case {case_id} expected_routing.{variant} has invalid required_call_order")

    search_min = expectation["search_calls"]["min"]
    search_max = expectation["search_calls"]["max"]
    if variant == "static":
        if search_min != 0 or search_max != 0 or "tool_search" not in forbidden:
            raise MatrixError(f"case {case_id} static variant must forbid tool_search with zero calls")
        if modes or searched or boundary != "not_applicable":
            raise MatrixError(f"case {case_id} static variant cannot declare activation/search matches")
    elif boundary == "strict_separate_batch":
        if search_min < 1 or not searched or expectation.get("empty_search") != "forbidden":
            raise MatrixError(f"case {case_id} strict activation needs a non-empty successful search")
        if expectation["bypass_max"] != 0 or expectation["same_batch_max"] != 0:
            raise MatrixError(f"case {case_id} strict activation must set bypass/same_batch max to zero")
    elif boundary == "no_activation_expected":
        if search_min < 1 or searched or expectation.get("empty_search") != "required":
            raise MatrixError(f"case {case_id} negative search must require an empty search")


def _validate_mcp_fixture(case):
    fixture = case.get("mcp_fixture")
    if fixture is None:
        return
    if not isinstance(fixture, dict) or set(fixture) != {"server_name", "tool_count"}:
        raise MatrixError(f"case {case['id']} mcp_fixture allows only server_name/tool_count")
    if not isinstance(fixture["server_name"], str) or not SERVER_NAME_RE.fullmatch(fixture["server_name"]):
        raise MatrixError(f"case {case['id']} has invalid MCP fixture server_name")
    if fixture["tool_count"] not in MCP_TOOL_COUNTS:
        raise MatrixError(f"case {case['id']} has invalid MCP fixture tool_count")
    routing = case.get("routing")
    if not isinstance(routing, dict):
        raise MatrixError(f"case {case['id']} MCP fixture requires routing verifier spec")
    catalog = mcp_fixture_tool_names(fixture["server_name"], fixture["tool_count"])
    if routing.get("deferred_tools") != catalog:
        raise MatrixError(f"case {case['id']} routing.deferred_tools does not match fixture catalog")
    target = canonical_mcp_name(fixture["server_name"], MCP_TARGET)
    if routing.get("fixture_tools") != {target: MCP_TARGET}:
        raise MatrixError(f"case {case['id']} routing.fixture_tools must contain only the target")
    expected = routing.get("expected_calls")
    if (not isinstance(expected, list) or len(expected) != 1
            or expected[0].get("tool") != target or not isinstance(expected[0].get("args"), dict)):
        raise MatrixError(f"case {case['id']} routing.expected_calls must contain one target call")


def _validate_browser_fixture(case):
    fixture = case.get("browser_fixture")
    if fixture is None:
        return
    if case.get("surface") != "web":
        raise MatrixError(f"case {case['id']} Browser fixture must use the Web surface")
    expected_fields = {
        "kind",
        "network",
        "prompt_owner",
        "required_actions",
        "confirmation_prefix",
    }
    if set(fixture) != expected_fields:
        raise MatrixError(f"case {case['id']} Browser fixture has unsupported fields")
    if (fixture.get("kind") != BROWSER_FIXTURE_KIND
            or fixture.get("network") != "loopback"
            or fixture.get("prompt_owner") != "web_browser_driver"):
        raise MatrixError(f"case {case['id']} Browser fixture must use the Web driver proof form")
    if fixture.get("required_actions") != BROWSER_FIXTURE_ACTIONS:
        raise MatrixError(f"case {case['id']} Browser fixture action contract drifted")
    if fixture.get("confirmation_prefix") != BROWSER_CONFIRMATION_PREFIX:
        raise MatrixError(f"case {case['id']} Browser fixture confirmation contract drifted")
    browser_config = case.get("home_config", {}).get("browser")
    expected_browser_config = {
        "enabled": True,
        "backend": "managed",
        "headless": True,
        "approval": {
            "navigate": "always_allow",
            "interact": "always_allow",
        },
    }
    if browser_config != expected_browser_config:
        raise MatrixError(f"case {case['id']} Browser success approval contract drifted")
    if "{BROWSER_FIXTURE_URL}" not in case.get("prompt", ""):
        raise MatrixError(f"case {case['id']} Browser prompt must use the runner URL placeholder")

    required_by_variant = {
        variant: {
            call["name"]: (call["min"], call["max"])
            for call in expectation["required_tool_calls"]
        }
        for variant, expectation in case.get("expected_routing", {}).items()
    }
    expected_bounds = {
        "browser_open": (1, 1),
        "browser_snapshot": (1, 2),
        "browser_act": (2, 4),
        "browser_read": (1, 2),
    }
    expected_order = [
        "browser_open",
        "browser_snapshot",
        "browser_act",
        "browser_act",
        "browser_read",
    ]
    for variant, expectation in case.get("expected_routing", {}).items():
        if required_by_variant.get(variant) != expected_bounds:
            raise MatrixError(f"case {case['id']} {variant} Browser call bounds drifted")
        order = expectation.get("required_call_order", [])
        if variant == "deferred":
            if order != ["tool_search", *expected_order]:
                raise MatrixError(f"case {case['id']} deferred Browser order drifted")
            if set(expectation.get("expected_search_tools", [])) != set(expected_bounds):
                raise MatrixError(f"case {case['id']} Deferred Browser disclosure is incomplete")
        elif order != expected_order:
            raise MatrixError(f"case {case['id']} static Browser order drifted")


def _validate_case(case):
    if not isinstance(case, dict):
        raise MatrixError("every ToolSearch case must be an object")
    case_id = _require_string(case, "id")
    if not CASE_ID_RE.fullmatch(case_id):
        raise MatrixError(f"invalid ToolSearch case id: {case_id!r}")
    for key in ("title", "category", "tier", "prompt"):
        _require_string(case, key)
    if case.get("surface") not in SURFACES:
        raise MatrixError(f"case {case_id} has invalid surface")
    if not isinstance(case.get("critical"), bool):
        raise MatrixError(f"case {case_id} critical must be boolean")
    variants = case.get("variants")
    if (not isinstance(variants, list) or not variants or len(variants) != len(set(variants))
            or not set(variants) <= VARIANTS):
        raise MatrixError(f"case {case_id} has invalid variants")
    if not isinstance(case.get("timeout"), int) or isinstance(case.get("timeout"), bool) or case["timeout"] <= 0:
        raise MatrixError(f"case {case_id} timeout must be a positive integer")
    if not isinstance(case.get("expect_tool_use"), bool):
        raise MatrixError(f"case {case_id} expect_tool_use must be boolean")
    if not isinstance(case.get("oracles"), list) or not case["oracles"]:
        raise MatrixError(f"case {case_id} requires deterministic oracles")
    _validate_path_map(case, "fixtures")
    _validate_path_map(case, "home_fixtures")

    safety = case.get("safety")
    if (not isinstance(safety, dict)
            or safety.get("credentials") != "none"
            or safety.get("network") not in {"none", "loopback"}
            or safety.get("fixture_only") is not True):
        raise MatrixError(f"case {case_id} requires explicit no-credential fixture safety metadata")
    if case["surface"] == "acp" and safety["network"] == "loopback":
        raise MatrixError(f"case {case_id} ACP case cannot request loopback browser networking")

    tags = case.get("metric_tags")
    if (not isinstance(tags, list) or not tags or len(tags) != len(set(tags))
            or not set(tags) <= METRIC_TAGS or "task_pass" not in tags
            or "schema_disclosure" not in tags):
        raise MatrixError(f"case {case_id} has invalid metric_tags")
    if case["critical"] != ("critical_pass" in tags):
        raise MatrixError(f"case {case_id} critical flag/tag disagree")
    if set(variants) == VARIANTS and "paired_task_pass" not in tags:
        raise MatrixError(f"case {case_id} paired variants require paired_task_pass metric tag")
    if set(variants) != VARIANTS and "paired_task_pass" in tags:
        raise MatrixError(f"case {case_id} unpaired variants cannot use paired_task_pass")
    if "full_schema_disclosure" in tags and set(variants) != VARIANTS:
        raise MatrixError(
            f"case {case_id} full_schema_disclosure requires paired variants"
        )

    expectations = case.get("expected_routing")
    if not isinstance(expectations, dict) or set(expectations) != set(variants):
        raise MatrixError(f"case {case_id} expected_routing must exactly match variants")
    for variant, expectation in expectations.items():
        _validate_expectation(case, variant, expectation)

    required_names = {
        call["name"]
        for expectation in expectations.values()
        for call in expectation["required_tool_calls"]
    }
    if case["surface"] == "acp" and any(name.startswith("browser_") for name in required_names):
        raise MatrixError(f"case {case_id} ACP deliberately has no Browser tools; use surface=web")
    if any(name.startswith("browser_") for name in required_names) and "browser_routing" not in tags:
        raise MatrixError(f"case {case_id} Browser calls require browser_routing tag")
    if any(name.startswith("computer_") for name in required_names) and "computer_routing" not in tags:
        raise MatrixError(f"case {case_id} Computer calls require computer_routing tag")

    _validate_mcp_fixture(case)
    _validate_browser_fixture(case)


def _validate_hard_gates(document):
    gates = document.get("hard_gates")
    if not isinstance(gates, dict) or set(gates) != set(REQUIRED_HARD_GATES):
        raise MatrixError("hard_gates must contain the complete pinned acceptance gate set")
    for name, (aggregate, operator, threshold) in REQUIRED_HARD_GATES.items():
        gate = gates[name]
        if not isinstance(gate, dict):
            raise MatrixError(f"hard gate {name} must be an object")
        required = {"metric", "scope", "aggregate", "operator", "threshold"}
        if set(gate) != required:
            raise MatrixError(f"hard gate {name} must contain exactly {sorted(required)}")
        if (not isinstance(gate["metric"], str) or not gate["metric"]
                or not isinstance(gate["scope"], dict)):
            raise MatrixError(f"hard gate {name} has invalid metric/scope")
        if (gate["aggregate"], gate["operator"], gate["threshold"]) != (aggregate, operator, threshold):
            raise MatrixError(f"hard gate {name} threshold or aggregation drifted")
        tag = gate["scope"].get("metric_tag")
        if tag is not None and tag not in METRIC_TAGS:
            raise MatrixError(f"hard gate {name} references unknown metric tag {tag!r}")
        variant = gate["scope"].get("variant")
        if variant is not None and variant not in VARIANTS:
            raise MatrixError(f"hard gate {name} references unknown variant {variant!r}")
        expected_scope = REQUIRED_GATE_SCOPES.get(name)
        if expected_scope is not None and gate["scope"] != expected_scope:
            raise MatrixError(f"hard gate {name} scope drifted")


def validate_suite(document, base_suite):
    if document.get("schema_version") != 1:
        raise MatrixError("ToolSearch matrix schema_version must be 1")
    contract = document.get("runner_contract")
    if not isinstance(contract, dict):
        raise MatrixError("runner_contract must be an object")
    if contract.get("variants") != ["static", "deferred"]:
        raise MatrixError("runner_contract variants must be static,deferred")
    if contract.get("default_surface") != "acp" or contract.get("browser_surface") != "web":
        raise MatrixError("runner_contract must keep Browser on Web and all other cases on ACP")
    if contract.get("browser_on_acp") is not False:
        raise MatrixError("runner_contract must explicitly state that ACP has no Browser tools")
    if contract.get("network_policy") != "fixtures only; Browser loopback only":
        raise MatrixError("runner_contract network policy drifted")

    _validate_hard_gates(document)
    _walk_forbidden_keys(document.get("cases", []), "cases")
    _walk_external_urls(document.get("cases", []), "cases")
    materialized = materialize_cases(document, base_suite)
    cases = materialized.get("cases")
    if not isinstance(cases, list) or not cases:
        raise MatrixError("ToolSearch matrix cases must be a non-empty array")
    ids = []
    counts = {tag: 0 for tag in METRIC_TAGS}
    for case in cases:
        _validate_case(case)
        ids.append(case["id"])
        for tag in case["metric_tags"]:
            counts[tag] += 1
    duplicates = sorted({case_id for case_id in ids if ids.count(case_id) > 1})
    if duplicates:
        raise MatrixError(f"duplicate ToolSearch case ids: {duplicates}")
    for required_tag in (
        "paired_task_pass",
        "critical_pass",
        "direct_routing",
        "deferred_call_accuracy",
        "irrelevant_search",
        "mcp_distractor_accuracy",
        "full_schema_disclosure",
        "negative_search",
        "browser_routing",
        "computer_routing",
    ):
        if counts[required_tag] == 0:
            raise MatrixError(f"matrix has no cases contributing to {required_tag}")
    if counts["full_schema_disclosure"] != 1:
        raise MatrixError(
            "matrix must have exactly one full_schema_disclosure representative"
        )
    return materialized


def load_suite(matrix_path=DEFAULT_MATRIX, base_suite_path=DEFAULT_BASE_SUITE):
    document = _read_json(matrix_path, "ToolSearch matrix")
    base_suite = _read_json(base_suite_path, "base suite")
    return validate_suite(document, base_suite)


def main():
    parser = argparse.ArgumentParser(description="validate the ToolSearch case matrix")
    parser.add_argument("--matrix", default=str(DEFAULT_MATRIX))
    parser.add_argument("--base-suite", default=str(DEFAULT_BASE_SUITE))
    parser.add_argument("--summary", action="store_true")
    args = parser.parse_args()
    try:
        suite = load_suite(args.matrix, args.base_suite)
    except MatrixError as exc:
        parser.error(str(exc))
    if args.summary:
        surfaces = {}
        for case in suite["cases"]:
            surfaces[case["surface"]] = surfaces.get(case["surface"], 0) + 1
        print(json.dumps({
            "schema_version": suite["schema_version"],
            "cases": len(suite["cases"]),
            "critical": sum(1 for case in suite["cases"] if case["critical"]),
            "surfaces": surfaces,
        }, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
