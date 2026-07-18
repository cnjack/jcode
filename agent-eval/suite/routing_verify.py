#!/usr/bin/env python3
"""Strict ToolSearch routing verification over authoritative session JSONL.

ACP presentations intentionally trade protocol identity for UI readability. This
verifier instead consumes jcode's session log, where canonical tool names, batch
metadata, arguments, results, denial state, and duration are recorded together.
"""
import argparse
import hashlib
import json
from collections import Counter, defaultdict
from pathlib import Path


TOOL_SEARCH = "tool_search"
FIXTURE_MARKER_PREFIX = "JCODE_MCP_FIXTURE_OK:"


def _violation(kind, detail, **fields):
    return {"type": kind, "detail": detail, **fields}


def _load_jsonl(path, label, violations):
    path = Path(path)
    if not path.is_file():
        violations.append(_violation(f"missing_{label}", f"{label} file not found: {path}"))
        return []
    entries = []
    for line_no, line in enumerate(path.read_text(errors="replace").splitlines(), 1):
        if not line.strip():
            continue
        try:
            value = json.loads(line)
        except json.JSONDecodeError as exc:
            violations.append(_violation(
                f"invalid_{label}_json", f"line {line_no}: {exc}", line=line_no,
            ))
            continue
        if not isinstance(value, dict):
            violations.append(_violation(
                f"invalid_{label}_entry", f"line {line_no}: expected object", line=line_no,
            ))
            continue
        value["_line"] = line_no
        entries.append(value)
    return entries


def _parse_args(raw, call_id, violations):
    if isinstance(raw, dict):
        return raw
    if not isinstance(raw, str):
        violations.append(_violation(
            "invalid_tool_args", "tool args must be a JSON object string",
            tool_call_id=call_id,
        ))
        return None
    try:
        parsed = json.loads(raw)
    except json.JSONDecodeError as exc:
        violations.append(_violation(
            "invalid_tool_args", f"tool args are invalid JSON: {exc}",
            tool_call_id=call_id,
        ))
        return None
    if not isinstance(parsed, dict):
        violations.append(_violation(
            "invalid_tool_args", "tool args JSON must be an object",
            tool_call_id=call_id,
        ))
        return None
    return parsed


def _canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


def _fixture_marker(raw_tool, arguments):
    digest = hashlib.sha256(_canonical(arguments).encode()).hexdigest()[:16]
    request_id = arguments.get("request_id", "")
    return f"{FIXTURE_MARKER_PREFIX}{raw_tool}:{request_id}:{digest}"


def _string_values(value):
    if isinstance(value, str):
        yield value
    elif isinstance(value, list):
        for item in value:
            yield from _string_values(item)
    elif isinstance(value, dict):
        for item in value.values():
            yield from _string_values(item)


def _result_contains_marker(output, marker):
    if not isinstance(output, str):
        return False
    try:
        value = json.loads(output)
    except json.JSONDecodeError:
        value = output
    return marker in set(_string_values(value))


def _result_failed(result):
    if result is None or result.get("error") or result.get("denied"):
        return True
    output = result.get("output")
    return isinstance(output, str) and any(marker in output for marker in (
        "Tool execution failed:",
        "Tool execution panicked:",
        "Tool approval error:",
    ))


def _pair_session_entries(entries, violations):
    calls = {}
    ordered_calls = []
    results = {}
    for index, entry in enumerate(entries):
        entry_type = entry.get("type")
        if entry_type == "tool_call":
            call_id = entry.get("tool_call_id")
            if not isinstance(call_id, str) or not call_id:
                violations.append(_violation(
                    "missing_tool_call_id", "tool_call has no tool_call_id", line=entry["_line"],
                ))
                continue
            if call_id in calls:
                violations.append(_violation(
                    "duplicate_tool_call_id", f"duplicate tool_call_id {call_id}",
                    tool_call_id=call_id,
                ))
                continue
            call = {**entry, "_index": index}
            call["_args"] = _parse_args(entry.get("args"), call_id, violations)
            calls[call_id] = call
            ordered_calls.append(call)
        elif entry_type == "tool_result":
            call_id = entry.get("tool_call_id")
            if not isinstance(call_id, str) or not call_id:
                violations.append(_violation(
                    "missing_result_tool_call_id", "tool_result has no tool_call_id",
                    line=entry["_line"],
                ))
                continue
            if call_id in results:
                violations.append(_violation(
                    "duplicate_tool_result", f"duplicate result for {call_id}",
                    tool_call_id=call_id,
                ))
                continue
            if call_id not in calls:
                violations.append(_violation(
                    "orphan_tool_result", f"result precedes or lacks call {call_id}",
                    tool_call_id=call_id,
                ))
                continue
            result = {**entry, "_index": index}
            results[call_id] = result
            if result.get("name") != calls[call_id].get("name"):
                violations.append(_violation(
                    "tool_result_name_mismatch",
                    f"call name {calls[call_id].get('name')!r} != result name {result.get('name')!r}",
                    tool_call_id=call_id,
                ))
    for call_id, call in calls.items():
        if call_id not in results:
            violations.append(_violation(
                "missing_tool_result", f"no result for {call_id}",
                tool_call_id=call_id, tool=call.get("name"),
            ))
    return ordered_calls, results


def _batch_facts(calls, results, relevant_names, violations):
    batches = defaultdict(list)
    for call in calls:
        batch_id = call.get("batch_id")
        batch_size = call.get("batch_size")
        if call.get("name") in relevant_names and (not batch_id or not isinstance(batch_size, int) or batch_size <= 0):
            violations.append(_violation(
                "missing_batch_metadata", "relevant tool call lacks batch_id/batch_size",
                tool_call_id=call.get("tool_call_id"), tool=call.get("name"),
            ))
        key = batch_id or f"__single__:{call.get('tool_call_id')}"
        call["_batch_key"] = key
        batches[key].append(call)

    facts = {}
    for key, batch_calls in batches.items():
        declared = {c.get("batch_size") for c in batch_calls if isinstance(c.get("batch_size"), int) and c.get("batch_size") > 0}
        if len(declared) > 1:
            violations.append(_violation(
                "inconsistent_batch_size", f"batch {key} declares sizes {sorted(declared)}", batch_id=key,
            ))
        expected = next(iter(declared), len(batch_calls))
        if expected != len(batch_calls):
            violations.append(_violation(
                "incomplete_batch", f"batch {key} has {len(batch_calls)} calls, expected {expected}",
                batch_id=key,
            ))
        # Go omits batch_index=0 from JSONL (`omitempty`). Once batch_id and a
        # positive batch_size are present, an absent index therefore means the
        # first call, not missing metadata. Duplicate zeroes still fail below.
        indexes = [c.get("batch_index", 0) for c in batch_calls]
        if declared and (len(set(indexes)) != len(indexes) or any(not isinstance(i, int) or i < 0 or i >= expected for i in indexes)):
            violations.append(_violation(
                "invalid_batch_indexes", f"batch {key} indexes {indexes}, expected 0..{expected - 1}",
                batch_id=key,
            ))
        batch_results = [results.get(c.get("tool_call_id")) for c in batch_calls]
        complete = expected == len(batch_calls) and all(r is not None for r in batch_results)
        facts[key] = {
            "complete": complete,
            "complete_index": max((r["_index"] for r in batch_results if r is not None), default=None),
            "calls": batch_calls,
        }
    return facts


def _search_facts(calls, results, batches, deferred, violations):
    searches = []
    for call in calls:
        if call.get("name") != TOOL_SEARCH:
            continue
        call_id = call.get("tool_call_id")
        result = results.get(call_id)
        fact = {
            "tool_call_id": call_id,
            "batch_id": call.get("batch_id"),
            "batch_key": call.get("_batch_key"),
            "call_index": call["_index"],
            "query_mode": "invalid",
            "query_bytes": 0,
            "matches": [],
            "successful": False,
            "complete_index": batches.get(call.get("_batch_key"), {}).get("complete_index"),
        }
        query = (call.get("_args") or {}).get("query")
        if isinstance(query, str):
            fact["query_mode"] = "select" if query.strip().startswith("select:") else "keyword"
            fact["query_bytes"] = len(query.strip().encode())
        if _result_failed(result):
            violations.append(_violation(
                "search_failed", "tool_search did not produce a successful result",
                tool_call_id=call_id,
            ))
            searches.append(fact)
            continue
        try:
            output = json.loads(result.get("output", ""))
        except (json.JSONDecodeError, TypeError) as exc:
            violations.append(_violation(
                "invalid_search_result", f"tool_search result is invalid JSON: {exc}",
                tool_call_id=call_id,
            ))
            searches.append(fact)
            continue
        matches = output.get("matches") if isinstance(output, dict) else None
        if not isinstance(matches, list) or any(not isinstance(name, str) for name in matches):
            violations.append(_violation(
                "invalid_search_result", "tool_search result must contain a string matches array",
                tool_call_id=call_id,
            ))
            searches.append(fact)
            continue
        outside = sorted(set(matches) - deferred)
        if outside:
            violations.append(_violation(
                "search_returned_non_deferred",
                f"tool_search returned tools outside the case Deferred set: {outside}",
                tool_call_id=call_id, tools=outside,
            ))
        if not batches.get(call.get("_batch_key"), {}).get("complete"):
            violations.append(_violation(
                "search_batch_incomplete", "tool_search batch did not complete",
                tool_call_id=call_id,
            ))
            searches.append(fact)
            continue
        fact["matches"] = matches
        fact["successful"] = True
        searches.append(fact)
    return searches


def _verify_fixture(calls, results, fixture_entries, fixture_tools, expected_calls, violations):
    session_calls = [c for c in calls if c.get("name") in fixture_tools]
    remaining = []
    for entry in fixture_entries:
        arguments = entry.get("arguments")
        if not isinstance(entry.get("tool"), str) or not isinstance(arguments, dict) or not isinstance(entry.get("marker"), str):
            violations.append(_violation(
                "invalid_fixture_log_entry", f"fixture line {entry.get('_line')} lacks tool/arguments/marker",
            ))
            continue
        expected_marker = _fixture_marker(entry["tool"], arguments)
        if entry["marker"] != expected_marker:
            violations.append(_violation(
                "fixture_log_marker_mismatch",
                "fixture marker does not match the deterministic recomputation",
                line=entry.get("_line"),
            ))
        remaining.append(entry)

    matched = 0
    for call in session_calls:
        canonical_name = call.get("name")
        raw_name = fixture_tools[canonical_name]
        arguments = call.get("_args")
        result = results.get(call.get("tool_call_id"))
        match_index = None
        if arguments is not None and result is not None:
            for i, entry in enumerate(remaining):
                if entry.get("tool") != raw_name or entry.get("arguments") != arguments:
                    continue
                if not _result_contains_marker(result.get("output"), entry.get("marker")):
                    continue
                match_index = i
                break
        if match_index is None:
            violations.append(_violation(
                "fixture_session_mismatch",
                "no fixture log entry has the session tool's raw endpoint, args, and result marker",
                tool_call_id=call.get("tool_call_id"), tool=canonical_name,
            ))
            continue
        remaining.pop(match_index)
        matched += 1

    for entry in remaining:
        violations.append(_violation(
            "unexpected_fixture_call",
            "fixture logged a call with no matching session call/result",
            tool=entry.get("tool"), line=entry.get("_line"),
        ))

    if expected_calls is not None:
        actual = Counter(
            (c.get("name"), _canonical(c.get("_args")))
            for c in session_calls if c.get("_args") is not None
        )
        expected = Counter()
        for item in expected_calls:
            if not isinstance(item, dict) or not isinstance(item.get("tool"), str) or not isinstance(item.get("args"), dict):
                violations.append(_violation(
                    "invalid_expected_call", "expected_calls entries require tool and args object",
                ))
                continue
            expected[(item["tool"], _canonical(item["args"]))] += 1
        if actual != expected:
            violations.append(_violation(
                "expected_calls_mismatch",
                "actual fixture call multiset differs from routing.expected_calls",
                actual_count=sum(actual.values()), expected_count=sum(expected.values()),
            ))
    return {"session_calls": len(session_calls), "matched": matched, "logged": len(fixture_entries)}


def verify_routing(session_path, fixture_log_path, spec, require_activation=True):
    """Return a structured, fail-closed routing verdict for one session.

    ``require_activation=False`` keeps the deterministic fixture/session/marker
    cross-checks for the eager static A/B arm without pretending that a static
    tool needed ToolSearch activation.  The default preserves the strict
    Deferred behavior used by all existing callers.
    """
    violations = []
    deferred_value = spec.get("deferred_tools") if isinstance(spec, dict) else None
    if not isinstance(deferred_value, list) or not deferred_value or any(not isinstance(v, str) or not v for v in deferred_value):
        return {
            "passed": False,
            "violations": [_violation(
                "invalid_deferred_set", "case routing.deferred_tools must be a non-empty string array",
            )],
            "counts": {},
        }
    if len(set(deferred_value)) != len(deferred_value):
        violations.append(_violation("invalid_deferred_set", "deferred_tools contains duplicates"))
    deferred = set(deferred_value)

    fixture_tools = spec.get("fixture_tools", {})
    if not isinstance(fixture_tools, dict) or any(
        canonical not in deferred or not isinstance(raw, str) or not raw
        for canonical, raw in fixture_tools.items()
    ):
        violations.append(_violation(
            "invalid_fixture_tools", "fixture_tools must map Deferred canonical names to raw names",
        ))
        fixture_tools = {}

    entries = _load_jsonl(session_path, "session", violations)
    calls, results = _pair_session_entries(entries, violations)
    relevant_names = deferred | {TOOL_SEARCH}
    batches = _batch_facts(calls, results, relevant_names, violations)
    searches = _search_facts(calls, results, batches, deferred, violations)

    activated = set()
    activations = []
    if require_activation:
        for search in sorted(
            (s for s in searches if s["successful"]),
            key=lambda item: (item["complete_index"], item["call_index"]),
        ):
            matched_deferred = set(search["matches"]) & deferred
            new_tools = sorted(matched_deferred - activated)
            search["new_tools"] = new_tools
            if not new_tools:
                violations.append(_violation(
                    "redundant_search", "successful tool_search activated no new Deferred tools",
                    tool_call_id=search["tool_call_id"],
                ))
            for name in new_tools:
                activations.append({
                    "tool": name,
                    "after_index": search["complete_index"],
                    "search_tool_call_id": search["tool_call_id"],
                })
            activated.update(new_tools)

    deferred_calls = [c for c in calls if c.get("name") in deferred]
    bypass = 0
    same_batch = 0
    deferred_success = 0
    for call in deferred_calls:
        name = call.get("name")
        result = results.get(call.get("tool_call_id"))
        if _result_failed(result):
            violations.append(_violation(
                "deferred_call_failed", "Deferred target did not produce a successful result",
                tool_call_id=call.get("tool_call_id"), tool=name,
            ))
        else:
            deferred_success += 1
        if require_activation:
            matching_same_batch = [
                search for search in searches
                if search["successful"] and search["batch_key"] == call.get("_batch_key")
                and name in search["matches"]
            ]
            if matching_same_batch:
                same_batch += 1
                violations.append(_violation(
                    "same_batch_activation",
                    "Deferred target was called in the same batch as the search that returned it",
                    tool_call_id=call.get("tool_call_id"), tool=name,
                ))
            active_before = any(
                event["tool"] == name and event["after_index"] < call["_index"]
                for event in activations
            )
            if not active_before:
                bypass += 1
                violations.append(_violation(
                    "bypass", "Deferred target was called before a completed successful activation batch",
                    tool_call_id=call.get("tool_call_id"), tool=name,
                ))

    fixture_entries = _load_jsonl(fixture_log_path, "fixture_log", violations) if fixture_tools else []
    fixture_stats = _verify_fixture(
        calls, results, fixture_entries, fixture_tools,
        spec.get("expected_calls") if "expected_calls" in spec else None,
        violations,
    ) if fixture_tools else {"session_calls": 0, "matched": 0, "logged": 0}

    return {
        "passed": not violations,
        "counts": {
            "tool_calls": len(calls),
            "paired_tool_calls": sum(1 for c in calls if c.get("tool_call_id") in results),
            "search_calls": len(searches),
            "deferred_calls": len(deferred_calls),
            "deferred_call_success": deferred_success,
            "bypass": bypass,
            "same_batch_activation": same_batch,
            "redundant_search": sum(1 for v in violations if v["type"] == "redundant_search"),
            "fixture_calls": fixture_stats["session_calls"],
            "fixture_matches": fixture_stats["matched"],
        },
        "deferred_tools": sorted(deferred),
        "searches": searches,
        "activations": activations,
        "fixture": fixture_stats,
        "violations": violations,
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--session", required=True, help="jcode session JSONL path")
    parser.add_argument("--fixture-log", required=True, help="MCP fixture call JSONL path")
    parser.add_argument("--spec", required=True, help="routing spec JSON path")
    parser.add_argument("--out", help="write verdict JSON here (default stdout)")
    args = parser.parse_args()

    spec = json.loads(Path(args.spec).read_text())
    result = verify_routing(args.session, args.fixture_log, spec)
    rendered = json.dumps(result, indent=2, ensure_ascii=False)
    if args.out:
        Path(args.out).write_text(rendered + "\n")
    else:
        print(rendered)
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
