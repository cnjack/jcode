#!/usr/bin/env python3
"""Build a publication-safe tool trajectory from jcode session JSONL.

The session recorder is intentionally authoritative and intentionally rich: it
contains user/model text plus raw tool arguments and results.  Eval reports do
not need those payloads.  This module copies only routing and timing metadata.
Arguments are included solely for deterministic fixture tools explicitly
declared by the testcase; tool_search queries and ordinary tool payloads are
never copied.
"""

import json
import os
from collections import Counter
from pathlib import Path


TOOL_OBSERVATION_FIELDS = {
    "kind",
    "model_request_seq",
    "visible_names",
    "visible_count",
    "schema_bytes",
    "schema_tokens_estimate",
    "newly_visible_deferred",
    "tool_call_id",
    "query_mode",
    "query_bytes",
    "term_count",
    "required_term_count",
    "max_results",
    "validated_select_names",
    "unknown_select_count",
    "match_names",
    "new_match_names",
    "repeated_query",
    "redundant",
    "success",
    "tool_name",
    "reason",
}


def _read_jsonl(path: Path):
    entries = []
    parse_errors = []
    for line_no, line in enumerate(path.read_text(errors="replace").splitlines(), 1):
        if not line.strip():
            continue
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            parse_errors.append(line_no)
            continue
        if not isinstance(value, dict):
            parse_errors.append(line_no)
            continue
        entries.append((line_no, value))
    return entries, parse_errors


def _fixture_args(raw):
    if isinstance(raw, dict):
        value = raw
    elif isinstance(raw, str):
        try:
            value = json.loads(raw)
        except json.JSONDecodeError:
            return None
    else:
        return None
    return value if isinstance(value, dict) else None


def _result_status(entry):
    if entry.get("denied"):
        return "denied"
    output = entry.get("output")
    folded_failure = isinstance(output, str) and any(
        marker in output
        for marker in (
            "Tool execution failed:",
            "Tool execution panicked:",
            "Tool approval error:",
        )
    )
    if entry.get("error") or folded_failure:
        return "failed"
    return "completed"


def _safe_observation(raw):
    if not isinstance(raw, dict):
        return None
    return {key: raw[key] for key in TOOL_OBSERVATION_FIELDS if key in raw}


def extract_trajectory(session_paths, fixture_arg_tools=()):
    """Extract routing evidence without copying model or tool payloads.

    ``fixture_arg_tools`` is an explicit allowlist of canonical tool names whose
    arguments are synthetic test data.  Even for those tools results remain
    metadata-only.  Passing no allowlist is the safe default.
    """
    fixture_arg_tools = set(fixture_arg_tools)
    sessions = []
    call_names = Counter()
    result_statuses = Counter()
    visible_counts = []
    schema_tokens = []
    parse_error_count = 0
    total_entries = 0

    for session_index, raw_path in enumerate(session_paths, 1):
        path = Path(raw_path)
        if not path.is_file():
            sessions.append({
                "session_index": session_index,
                "source_present": False,
                "parse_error_lines": [],
                "entries": [],
            })
            continue
        source_entries, parse_errors = _read_jsonl(path)
        parse_error_count += len(parse_errors)
        safe_entries = []
        for line_no, entry in source_entries:
            entry_type = entry.get("type")
            safe = None
            if entry_type == "tool_call":
                name = entry.get("name") if isinstance(entry.get("name"), str) else ""
                safe = {
                    "type": "tool_call",
                    "name": name,
                    "tool_call_id": entry.get("tool_call_id", ""),
                    "batch_id": entry.get("batch_id", ""),
                    "batch_index": entry.get("batch_index", 0),
                    "batch_size": entry.get("batch_size", 0),
                }
                call_names[name] += 1
                if name in fixture_arg_tools:
                    args = _fixture_args(entry.get("args"))
                    safe["fixture_args_valid"] = args is not None
                    if args is not None:
                        safe["fixture_args"] = args
            elif entry_type == "tool_result":
                status = _result_status(entry)
                output = entry.get("output")
                safe = {
                    "type": "tool_result",
                    "name": entry.get("name", ""),
                    "tool_call_id": entry.get("tool_call_id", ""),
                    "status": status,
                    "duration_ms": entry.get("duration_ms", 0),
                    "output_bytes": len(output.encode()) if isinstance(output, str) else 0,
                }
                result_statuses[status] += 1
            elif entry_type == "tool_observation":
                observation = _safe_observation(entry.get("tool_observation"))
                if observation is not None:
                    safe = {"type": "tool_observation", **observation}
                    if observation.get("kind") == "model_request":
                        visible_counts.append(int(observation.get("visible_count", 0) or 0))
                        schema_tokens.append(
                            int(observation.get("schema_tokens_estimate", 0) or 0)
                        )
            if safe is None:
                continue
            total_entries += 1
            safe["sequence"] = total_entries
            safe["source_line"] = line_no
            safe_entries.append(safe)
        sessions.append({
            "session_index": session_index,
            "source_present": True,
            "parse_error_lines": parse_errors,
            "entries": safe_entries,
        })

    tool_counts = {
        "calls_total": sum(call_names.values()),
        "results_total": sum(result_statuses.values()),
        "calls_by_name": dict(sorted(call_names.items())),
        "results_by_status": dict(sorted(result_statuses.items())),
        "model_requests": len(visible_counts),
        "first_visible": visible_counts[0] if visible_counts else None,
        "max_visible": max(visible_counts) if visible_counts else None,
        "first_schema_tokens_estimate": schema_tokens[0] if schema_tokens else None,
        "max_schema_tokens_estimate": max(schema_tokens) if schema_tokens else None,
    }
    return {
        "schema_version": 1,
        "payload_policy": "metadata_only_except_declared_fixture_args",
        "session_count": len(sessions),
        "parse_error_count": parse_error_count,
        "tool_counts": tool_counts,
        "sessions": sessions,
    }


def write_trajectory(path, trajectory):
    path = Path(path)
    payload = json.dumps(trajectory, indent=2, sort_keys=True) + "\n"
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        stream.write(payload)
    path.chmod(0o600)
