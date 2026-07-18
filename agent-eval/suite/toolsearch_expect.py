#!/usr/bin/env python3
"""Fail-closed, metadata-only ToolSearch expectation verification.

The session JSONL is private evidence: it contains prompts, raw search queries,
tool arguments, and tool outputs.  This verifier may inspect that evidence, but
its return value deliberately contains only counters, booleans, violation
types, and canonical tool names.  Callers can therefore persist the verdict
without copying model content or credentials into evaluation artifacts.
"""

import argparse
import hashlib
import json
import os
import re
import stat
from collections import Counter, defaultdict
from dataclasses import dataclass
from pathlib import Path
from types import MappingProxyType
from typing import Mapping


TOOL_SEARCH = "tool_search"
TOOL_NAME_RE = re.compile(r"^[A-Za-z0-9_.:-]{1,160}$")
TYPE_RE = re.compile(r"^[a-z][a-z0-9_]{0,79}$")
FAILED_OUTPUT_PREFIXES = (
    "Tool execution failed:",
    "Tool execution panicked:",
    "Tool approval error:",
)


@dataclass(frozen=True)
class FixtureTarget:
    """Identity of one regular runner-owned fixture captured before execution."""

    lexical: Path
    resolved: Path
    identity: tuple[int, int, int, int]
    sha256: str


@dataclass(frozen=True)
class FixtureScope:
    """Runner-owned fixture identities captured before the model can mutate them."""

    root: Path
    root_identity: tuple[int, int, int]
    targets: Mapping[str, FixtureTarget]


class FixtureScopeError(ValueError):
    """Raised when a runner cannot establish a trusted fixture scope."""


def _safe_relative_fixture_path(value):
    if (not isinstance(value, str) or not value or "\x00" in value):
        return None
    candidate = Path(value)
    if (candidate.is_absolute() or ".." in candidate.parts
            or str(candidate) != value or value == "."):
        return None
    return candidate


def _root_identity(path):
    metadata = path.lstat()
    if not stat.S_ISDIR(metadata.st_mode):
        raise FixtureScopeError("invalid fixture scope")
    return metadata.st_dev, metadata.st_ino, stat.S_IFMT(metadata.st_mode)


def _file_identity(path):
    metadata = path.lstat()
    if not stat.S_ISREG(metadata.st_mode):
        raise FixtureScopeError("invalid fixture scope")
    return (
        metadata.st_dev,
        metadata.st_ino,
        stat.S_IFMT(metadata.st_mode),
        metadata.st_size,
    )


def _file_sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        while chunk := handle.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


def build_fixture_scope(root, fixture_paths):
    """Capture declared fixture targets without publishing their host paths."""
    try:
        root_lexical = Path(os.path.abspath(root))
        trusted_root = root_lexical.resolve(strict=True)
        root_identity = _root_identity(trusted_root)
        targets = {}
        resolved_targets = set()
        for value in fixture_paths:
            relative = _safe_relative_fixture_path(value)
            if relative is None:
                raise FixtureScopeError("invalid fixture scope")
            lexical = Path(os.path.abspath(trusted_root / relative))
            lexical.relative_to(trusted_root)
            resolved = lexical.resolve(strict=True)
            resolved.relative_to(trusted_root)
            if lexical != resolved or resolved in resolved_targets:
                raise FixtureScopeError("invalid fixture scope")
            targets[value] = FixtureTarget(
                lexical=lexical,
                resolved=resolved,
                identity=_file_identity(lexical),
                sha256=_file_sha256(lexical),
            )
            resolved_targets.add(resolved)
        return FixtureScope(
            trusted_root, root_identity, MappingProxyType(targets),
        )
    except (FixtureScopeError, OSError, RuntimeError, TypeError, ValueError):
        raise FixtureScopeError("invalid fixture scope") from None

COUNT_KEYS = (
    "session_entries",
    "invalid_session_entries",
    "tool_calls",
    "tool_results",
    "paired_results",
    "successful_results",
    "failed_results",
    "orphan_results",
    "missing_results",
    "invalid_arguments",
    "incomplete_batches",
    "search_calls",
    "search_success",
    "search_failed",
    "search_mode_select",
    "search_mode_keyword",
    "search_mode_invalid",
    "search_query_checks",
    "search_query_matches",
    "search_query_mismatches",
    "empty_searches",
    "expected_search_tools",
    "matched_expected_search_tools",
    "required_call_specs",
    "required_calls",
    "optional_call_specs",
    "optional_calls",
    "forbidden_calls",
    "argument_checks",
    "argument_matches",
    "argument_mismatches",
    "order_items",
    "order_matched",
    "deferred_calls",
    "deferred_call_success",
    "observed_bypass",
    "bypass",
    "same_batch_activation",
)

CHECK_KEYS = (
    "session_valid",
    "results_complete",
    "calls_successful",
    "batches_complete",
    "search_count",
    "search_modes",
    "search_queries",
    "search_matches",
    "empty_search_policy",
    "required_call_counts",
    "optional_call_counts",
    "forbidden_calls",
    "arguments",
    "call_order",
    "activation_boundary",
    "bypass_limit",
    "same_batch_limit",
)


def _new_counts():
    return {key: 0 for key in COUNT_KEYS}


def _new_checks():
    return {key: True for key in CHECK_KEYS}


def _safe_tool_name(value):
    return value if isinstance(value, str) and TOOL_NAME_RE.fullmatch(value) else None


def _violation(kind, tool=None):
    """Build a publication-safe violation without raw evidence or paths."""
    safe_kind = kind if isinstance(kind, str) and TYPE_RE.fullmatch(kind) else "invalid_violation_type"
    value = {"type": safe_kind}
    safe_tool = _safe_tool_name(tool)
    if safe_tool is not None:
        value["tool"] = safe_tool
    return value


def failure_verdict(kind, tool=None):
    """Return a fixed-shape fail-closed verdict for runner-level failures."""
    checks = {key: False for key in CHECK_KEYS}
    return {
        "passed": False,
        "counts": _new_counts(),
        "checks": checks,
        "violations": [_violation(kind, tool)],
    }


def _load_session(path, counts, checks, violations):
    try:
        lines = Path(path).read_text(errors="replace").splitlines()
    except OSError:
        checks["session_valid"] = False
        violations.append(_violation("missing_session"))
        return []
    entries = []
    for line in lines:
        if not line.strip():
            continue
        counts["session_entries"] += 1
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            counts["invalid_session_entries"] += 1
            checks["session_valid"] = False
            violations.append(_violation("invalid_session_json"))
            continue
        if not isinstance(value, dict):
            counts["invalid_session_entries"] += 1
            checks["session_valid"] = False
            violations.append(_violation("invalid_session_entry"))
            continue
        value["_index"] = len(entries)
        entries.append(value)
    return entries


def _parse_arguments(raw, tool, counts, checks, violations):
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, str):
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            parsed = None
        if isinstance(parsed, dict):
            return parsed
    counts["invalid_arguments"] += 1
    checks["arguments"] = False
    violations.append(_violation("invalid_tool_arguments", tool))
    return None


def _result_failed(result):
    if result is None or result.get("error") or result.get("denied"):
        return True
    output = result.get("output")
    return isinstance(output, str) and any(
        marker in output for marker in FAILED_OUTPUT_PREFIXES
    )


def _pair_entries(entries, counts, checks, violations):
    calls_by_id = {}
    calls = []
    results = {}
    for entry in entries:
        entry_type = entry.get("type")
        if entry_type == "tool_call":
            counts["tool_calls"] += 1
            call_id = entry.get("tool_call_id")
            tool = _safe_tool_name(entry.get("name"))
            duplicate = isinstance(call_id, str) and call_id in calls_by_id
            if not isinstance(call_id, str) or not call_id or call_id in calls_by_id:
                checks["results_complete"] = False
                violations.append(_violation(
                    "duplicate_tool_call" if duplicate else "missing_tool_call_id",
                    tool,
                ))
                continue
            call = dict(entry)
            call["_tool"] = tool
            call["_args"] = _parse_arguments(
                entry.get("args"), tool, counts, checks, violations,
            )
            calls_by_id[call_id] = call
            calls.append(call)
        elif entry_type == "tool_result":
            counts["tool_results"] += 1
            call_id = entry.get("tool_call_id")
            result_tool = _safe_tool_name(entry.get("name"))
            duplicate = isinstance(call_id, str) and call_id in results
            if (not isinstance(call_id, str) or not call_id
                    or call_id not in calls_by_id or call_id in results):
                counts["orphan_results"] += 1
                checks["results_complete"] = False
                kind = "duplicate_tool_result" if duplicate else "orphan_tool_result"
                violations.append(_violation(kind, result_tool))
                continue
            call = calls_by_id[call_id]
            if result_tool != call.get("_tool"):
                checks["results_complete"] = False
                violations.append(_violation("tool_result_name_mismatch", call.get("_tool")))
            result = dict(entry)
            results[call_id] = result
            counts["paired_results"] += 1
            if _result_failed(result):
                counts["failed_results"] += 1
                checks["calls_successful"] = False
                violations.append(_violation("tool_call_failed", call.get("_tool")))
            else:
                counts["successful_results"] += 1

    for call_id, call in calls_by_id.items():
        if call_id not in results:
            counts["missing_results"] += 1
            checks["results_complete"] = False
            violations.append(_violation("missing_tool_result", call.get("_tool")))
    return calls, results


def _batch_facts(calls, results, relevant_tools, counts, checks, violations):
    batches = defaultdict(list)
    for call in calls:
        batch_id = call.get("batch_id")
        batch_size = call.get("batch_size")
        if call.get("_tool") in relevant_tools and (
            not isinstance(batch_id, str) or not batch_id
            or not isinstance(batch_size, int) or isinstance(batch_size, bool)
            or batch_size <= 0
        ):
            checks["batches_complete"] = False
            violations.append(_violation("missing_batch_metadata", call.get("_tool")))
        key = batch_id if isinstance(batch_id, str) and batch_id else f"single:{call.get('tool_call_id')}"
        call["_batch_key"] = key
        batches[key].append(call)

    facts = {}
    for key, batch_calls in batches.items():
        relevant = any(call.get("_tool") in relevant_tools for call in batch_calls)
        declared = {
            call.get("batch_size")
            for call in batch_calls
            if isinstance(call.get("batch_size"), int)
            and not isinstance(call.get("batch_size"), bool)
            and call.get("batch_size") > 0
        }
        expected_size = next(iter(declared), len(batch_calls))
        indexes = [call.get("batch_index", 0) for call in batch_calls]
        valid_indexes = (
            len(declared) <= 1
            and len(set(indexes)) == len(indexes)
            and all(isinstance(index, int) and not isinstance(index, bool)
                    and 0 <= index < expected_size for index in indexes)
        )
        batch_results = [results.get(call.get("tool_call_id")) for call in batch_calls]
        complete = (
            len(declared) <= 1
            and expected_size == len(batch_calls)
            and valid_indexes
            and all(result is not None for result in batch_results)
        )
        if relevant and not complete:
            counts["incomplete_batches"] += 1
            checks["batches_complete"] = False
            violations.append(_violation("incomplete_tool_batch"))
        facts[key] = {
            "complete": complete,
            "complete_index": max(
                (result.get("_index", -1) for result in batch_results if result is not None),
                default=-1,
            ),
        }
    return facts


def _search_facts(calls, results, batches, counts, checks, violations):
    searches = []
    for call in calls:
        if call.get("_tool") != TOOL_SEARCH:
            continue
        mode = "invalid"
        arguments = call.get("_args")
        query = arguments.get("query") if isinstance(arguments, dict) else None
        if isinstance(query, str) and query.strip():
            mode = "select" if query.strip().startswith("select:") else "keyword"
        counts[f"search_mode_{mode}"] += 1

        result = results.get(call.get("tool_call_id"))
        matches = None
        if not _result_failed(result):
            try:
                payload = json.loads(result.get("output", ""))
            except (json.JSONDecodeError, TypeError):
                payload = None
            if isinstance(payload, dict) and "matches" in payload:
                candidate = payload["matches"]
                # Eino's toolSearchResult uses a nil []string when no tools
                # match, which serializes as JSON null rather than [].  Both
                # representations are successful empty searches; a missing
                # key or any other shape remains an invalid result.
                if candidate is None:
                    matches = []
                elif (isinstance(candidate, list)
                      and all(isinstance(name, str) for name in candidate)):
                    matches = candidate
        successful = matches is not None
        if successful:
            counts["search_success"] += 1
            if not matches:
                counts["empty_searches"] += 1
        else:
            counts["search_failed"] += 1
            checks["search_matches"] = False
            violations.append(_violation("invalid_or_failed_search"))
        searches.append({
            "call": call,
            "mode": mode,
            "query": query,
            "matches": matches or [],
            "successful": successful,
            "batch_complete": batches.get(call.get("_batch_key"), {}).get("complete", False),
            "complete_index": batches.get(call.get("_batch_key"), {}).get("complete_index", -1),
        })
    counts["search_calls"] = len(searches)
    return searches


def _contains(actual, expected):
    if isinstance(expected, dict):
        return isinstance(actual, dict) and all(
            key in actual and _contains(actual[key], value)
            for key, value in expected.items()
        )
    if isinstance(expected, list):
        return isinstance(actual, list) and actual == expected
    return actual == expected


def _fixture_scope_current(fixture_scope):
    if not isinstance(fixture_scope, FixtureScope):
        return False
    try:
        if _root_identity(fixture_scope.root) != fixture_scope.root_identity:
            return False
        for target in fixture_scope.targets.values():
            if (target.lexical.resolve(strict=True) != target.resolved
                    or _file_identity(target.lexical) != target.identity
                    or _file_sha256(target.lexical) != target.sha256):
                return False
        return True
    except (FixtureScopeError, OSError, RuntimeError, TypeError, ValueError):
        return False


def _fixture_path_matches(actual, expected, fixture_scope):
    if (not isinstance(fixture_scope, FixtureScope)
            or not isinstance(actual, str) or not actual or "\x00" in actual
            or not isinstance(expected, str)):
        return False
    trusted_target = fixture_scope.targets.get(expected)
    if trusted_target is None:
        return False
    raw_path = Path(actual)
    if ".." in raw_path.parts:
        return False
    try:
        candidate = raw_path if raw_path.is_absolute() else fixture_scope.root / raw_path
        lexical = Path(os.path.abspath(candidate))
        lexical.relative_to(fixture_scope.root)
        if lexical != trusted_target.lexical or not _fixture_scope_current(fixture_scope):
            return False
        resolved = lexical.resolve(strict=True)
        resolved.relative_to(fixture_scope.root)
        return resolved == trusted_target.resolved
    except (FixtureScopeError, OSError, RuntimeError, TypeError, ValueError):
        return False


def _arguments_match(actual, matcher, fixture_scope=None):
    if not isinstance(actual, dict) or not isinstance(matcher, dict):
        return False
    mode = matcher.get("match")
    expected = matcher.get("value")
    if not isinstance(expected, dict):
        return False
    if mode == "exact":
        return actual == expected
    if mode == "contains":
        return _contains(actual, expected)
    if mode == "fixture_path":
        if (not isinstance(fixture_scope, FixtureScope)
                or set(expected) != {"file_path"}):
            return False
        path = expected["file_path"]
        return (
            isinstance(path, str)
            and path in fixture_scope.targets
            and "file_path" in actual
            and _fixture_path_matches(
                actual["file_path"], path, fixture_scope,
            )
        )
    return False


def _check_call_specs(
        calls, results, expectation, counts, checks, violations,
        fixture_scope=None):
    by_tool = defaultdict(list)
    for call in calls:
        by_tool[call.get("_tool")].append(call)

    required = expectation.get("required_tool_calls", [])
    optional = expectation.get("optional_tool_calls", [])
    counts["required_call_specs"] = len(required)
    counts["optional_call_specs"] = len(optional)

    argument_ok_by_id = {}
    for label, specs, check_key, count_key in (
        ("required", required, "required_call_counts", "required_calls"),
        ("optional", optional, "optional_call_counts", "optional_calls"),
    ):
        for spec in specs:
            name = spec.get("name")
            actual = by_tool.get(name, [])
            counts[count_key] += len(actual)
            minimum, maximum = spec.get("min", 0), spec.get("max", 0)
            if not isinstance(minimum, int) or not isinstance(maximum, int) or not minimum <= len(actual) <= maximum:
                checks[check_key] = False
                violations.append(_violation(f"{label}_call_count", name))
            matcher = spec.get("args")
            for call in actual:
                call_id = call.get("tool_call_id")
                if matcher is None:
                    argument_ok_by_id[call_id] = call.get("_args") is not None
                    continue
                counts["argument_checks"] += 1
                matched = _arguments_match(
                    call.get("_args"), matcher, fixture_scope=fixture_scope,
                )
                argument_ok_by_id[call_id] = matched
                if matched:
                    counts["argument_matches"] += 1
                else:
                    counts["argument_mismatches"] += 1
                    checks["arguments"] = False
                    violations.append(_violation("argument_mismatch", name))

    for name in expectation.get("forbidden_tool_calls", []):
        actual = len(by_tool.get(name, []))
        if actual:
            counts["forbidden_calls"] += actual
            checks["forbidden_calls"] = False
            violations.append(_violation("forbidden_tool_call", name))

    deferred = set(expectation.get("expected_search_tools", []))
    for call in calls:
        if call.get("_tool") not in deferred:
            continue
        counts["deferred_calls"] += 1
        call_id = call.get("tool_call_id")
        args_ok = argument_ok_by_id.get(call_id, call.get("_args") is not None)
        if args_ok and not _result_failed(results.get(call_id)):
            counts["deferred_call_success"] += 1


def _check_order(calls, expectation, counts, checks, violations):
    expected = expectation.get("required_call_order", [])
    counts["order_items"] = len(expected)
    cursor = 0
    for name in expected:
        while cursor < len(calls) and calls[cursor].get("_tool") != name:
            cursor += 1
        if cursor >= len(calls):
            checks["call_order"] = False
            violations.append(_violation("required_call_order", name))
            break
        counts["order_matched"] += 1
        cursor += 1


def _check_searches(searches, expectation, counts, checks, violations):
    bounds = expectation.get("search_calls", {})
    minimum, maximum = bounds.get("min", -1), bounds.get("max", -1)
    if not isinstance(minimum, int) or not isinstance(maximum, int) or not minimum <= len(searches) <= maximum:
        checks["search_count"] = False
        violations.append(_violation("search_call_count", TOOL_SEARCH))

    allowed_modes = set(expectation.get("search_query_modes", []))
    for search in searches:
        if search["mode"] not in allowed_modes:
            checks["search_modes"] = False
            violations.append(_violation("search_query_mode", TOOL_SEARCH))

    query_matcher = expectation.get("search_query_matcher")
    if query_matcher is not None:
        for search in searches:
            counts["search_query_checks"] += 1
            matched = (
                isinstance(query_matcher, dict)
                and query_matcher.get("match") == "exact"
                and isinstance(query_matcher.get("value"), str)
                and search.get("query") == query_matcher["value"]
            )
            if matched:
                counts["search_query_matches"] += 1
            else:
                counts["search_query_mismatches"] += 1
                checks["search_queries"] = False
                violations.append(_violation("search_query_mismatch", TOOL_SEARCH))

    matched = set()
    for search in searches:
        if search["successful"]:
            matched.update(search["matches"])
    expected = set(expectation.get("expected_search_tools", []))
    counts["expected_search_tools"] = len(expected)
    counts["matched_expected_search_tools"] = len(expected & matched)
    for name in sorted(expected - matched):
        checks["search_matches"] = False
        violations.append(_violation("expected_search_match_missing", name))

    policy = expectation.get("empty_search")
    empties = counts["empty_searches"]
    if policy == "forbidden" and empties:
        checks["empty_search_policy"] = False
        violations.append(_violation("empty_search_forbidden", TOOL_SEARCH))
    elif policy == "required" and (not searches or empties != len(searches)):
        checks["empty_search_policy"] = False
        violations.append(_violation("empty_search_required", TOOL_SEARCH))
    elif policy not in {"forbidden", "allowed", "required"}:
        checks["empty_search_policy"] = False
        violations.append(_violation("invalid_empty_search_policy", TOOL_SEARCH))


def _check_activation(entries, calls, searches, expectation, counts, checks, violations):
    targets = set(expectation.get("expected_search_tools", []))
    activations = []
    for search in searches:
        if not search["successful"] or not search["batch_complete"]:
            continue
        for name in set(search["matches"]) & targets:
            activations.append((name, search["complete_index"], search["call"].get("_batch_key")))

    calculated_bypass = {}
    same_batch_ids = set()
    for call in calls:
        name = call.get("_tool")
        if name not in targets:
            continue
        call_id = call.get("tool_call_id")
        if any(
            search["successful"]
            and name in search["matches"]
            and search["call"].get("_batch_key") == call.get("_batch_key")
            for search in searches
        ):
            same_batch_ids.add(call_id)
            violations.append(_violation("same_batch_activation", name))
        if not any(tool == name and after < call.get("_index", -1) for tool, after, _batch in activations):
            calculated_bypass[call_id] = name

    observed_bypass = {}
    anonymous_observed = 0
    for entry in entries:
        if entry.get("type") != "tool_observation":
            continue
        observation = entry.get("tool_observation")
        if not isinstance(observation, dict) or observation.get("kind") != "deferred_bypass":
            continue
        name = _safe_tool_name(observation.get("tool_name"))
        call_id = observation.get("tool_call_id")
        if isinstance(call_id, str) and call_id:
            observed_bypass[call_id] = name
        else:
            anonymous_observed += 1
            violations.append(_violation("observed_deferred_bypass", name))
    counts["observed_bypass"] = len(observed_bypass) + anonymous_observed

    bypass_ids = set(calculated_bypass) | set(observed_bypass)
    counts["bypass"] = len(bypass_ids) + anonymous_observed
    for call_id in sorted(bypass_ids):
        name = calculated_bypass.get(call_id) or observed_bypass.get(call_id)
        violations.append(_violation("deferred_bypass", name))
    counts["same_batch_activation"] = len(same_batch_ids)

    boundary = expectation.get("activation_boundary")
    if boundary == "strict_separate_batch":
        if calculated_bypass or same_batch_ids:
            checks["activation_boundary"] = False
    elif boundary == "no_activation_expected":
        if activations:
            checks["activation_boundary"] = False
            violations.append(_violation("unexpected_activation"))
    elif boundary != "not_applicable":
        checks["activation_boundary"] = False
        violations.append(_violation("invalid_activation_boundary"))

    bypass_max = expectation.get("bypass_max", -1)
    if not isinstance(bypass_max, int) or counts["bypass"] > bypass_max:
        checks["bypass_limit"] = False
    same_batch_max = expectation.get("same_batch_max", -1)
    if not isinstance(same_batch_max, int) or counts["same_batch_activation"] > same_batch_max:
        checks["same_batch_limit"] = False


def _fixture_scope_contract_valid(expectation, fixture_scope):
    found = False
    for label in ("required_tool_calls", "optional_tool_calls"):
        specs = expectation.get(label, [])
        if not isinstance(specs, list):
            continue
        for spec in specs:
            if not isinstance(spec, dict):
                continue
            matcher = spec.get("args")
            if not isinstance(matcher, dict) or matcher.get("match") != "fixture_path":
                continue
            found = True
            expected = matcher.get("value")
            if (spec.get("name") != "read" or not isinstance(expected, dict)
                    or set(expected) != {"file_path"}
                    or not isinstance(expected["file_path"], str)
                    or not isinstance(fixture_scope, FixtureScope)
                    or expected["file_path"] not in fixture_scope.targets):
                return False
    return not found or _fixture_scope_current(fixture_scope)


def verify_expectation(session_path, expectation, fixture_scope=None):
    """Verify one variant's declarative expectation over private session JSONL."""
    if not isinstance(expectation, dict):
        return failure_verdict("invalid_expectation")
    if not _fixture_scope_contract_valid(expectation, fixture_scope):
        return failure_verdict("invalid_fixture_scope", "read")

    counts = _new_counts()
    checks = _new_checks()
    violations = []
    entries = _load_session(session_path, counts, checks, violations)
    calls, results = _pair_entries(entries, counts, checks, violations)

    strict = expectation.get("activation_boundary") == "strict_separate_batch"
    relevant = {TOOL_SEARCH} | set(expectation.get("expected_search_tools", [])) if strict else set()
    batches = _batch_facts(calls, results, relevant, counts, checks, violations)
    searches = _search_facts(calls, results, batches, counts, checks, violations)
    _check_searches(searches, expectation, counts, checks, violations)
    _check_call_specs(
        calls, results, expectation, counts, checks, violations,
        fixture_scope=fixture_scope,
    )
    _check_order(calls, expectation, counts, checks, violations)
    _check_activation(entries, calls, searches, expectation, counts, checks, violations)

    return {
        "passed": not violations and all(checks.values()),
        "counts": counts,
        "checks": checks,
        "violations": violations,
    }


def sanitize_external_verdict(verdict):
    """Project a richer private verifier result into safe publish metadata."""
    if not isinstance(verdict, dict):
        return failure_verdict("invalid_external_verdict")
    counts = {}
    raw_counts = verdict.get("counts")
    if isinstance(raw_counts, dict):
        for key, value in raw_counts.items():
            if (isinstance(key, str) and TYPE_RE.fullmatch(key)
                    and isinstance(value, int) and not isinstance(value, bool)):
                counts[key] = value
    violations = []
    for item in verdict.get("violations", []):
        if not isinstance(item, dict):
            violations.append(_violation("invalid_external_violation"))
            continue
        violations.append(_violation(item.get("type"), item.get("tool")))
    return {
        "passed": bool(verdict.get("passed")) and not violations,
        "counts": counts,
        "checks": {"external_verifier": bool(verdict.get("passed")) and not violations},
        "violations": violations,
    }


def main():
    parser = argparse.ArgumentParser(description="verify ToolSearch routing expectations")
    parser.add_argument("--session", required=True)
    parser.add_argument("--expectation", required=True)
    parser.add_argument("--out")
    args = parser.parse_args()
    try:
        expectation = json.loads(Path(args.expectation).read_text())
    except (OSError, json.JSONDecodeError):
        verdict = failure_verdict("invalid_expectation_file")
    else:
        verdict = verify_expectation(args.session, expectation)
    payload = json.dumps(verdict, indent=2, sort_keys=True) + "\n"
    if args.out:
        Path(args.out).write_text(payload)
    else:
        print(payload, end="")
    return 0 if verdict["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
