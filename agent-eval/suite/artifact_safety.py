#!/usr/bin/env python3
"""Credential and host-path redaction for publishable eval artifacts."""

import json
import os
import re
from collections import Counter
from pathlib import Path


REDACTED_CREDENTIAL = "[REDACTED_CREDENTIAL]"
REDACTED_HOST_PATH = "$REAL_HOME"

_CREDENTIAL_PATTERNS = (
    re.compile(r"sk-[A-Za-z0-9_-]{12,}"),
    re.compile(r"(?i)bearer\s+[A-Za-z0-9._~+/-]{12,}"),
    re.compile(
        r"(?i)([\"']?(?:api[_-]?key|access[_-]?token|authorization|client[_-]?secret)"
        r"[\"']?\s*[:=]\s*[\"']?)([A-Za-z0-9._~+/-]{8,})"
    ),
)


def _write_private_text(path, text):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        stream.write(text)
    path.chmod(0o600)


def _artifact_files(paths):
    seen = set()
    for raw in paths:
        path = Path(raw)
        candidates = path.rglob("*") if path.is_dir() else (path,)
        for candidate in candidates:
            try:
                resolved = candidate.resolve()
            except OSError:
                continue
            if resolved in seen or candidate.is_symlink() or not candidate.is_file():
                continue
            seen.add(resolved)
            yield candidate


def _read_text(path):
    try:
        data = path.read_bytes()
    except OSError:
        return None
    if b"\x00" in data:
        return None
    try:
        return data.decode("utf-8")
    except UnicodeDecodeError:
        return None


def sanitize_artifacts(paths, secret_values=(), forbidden_paths=()):
    """Redact exact runtime secrets/paths and high-confidence credentials.

    The returned report contains counts and filenames only.  It never embeds a
    matched credential or path value.
    """
    secrets = sorted(
        {value for value in secret_values if isinstance(value, str) and len(value) >= 8},
        key=len,
        reverse=True,
    )
    host_paths = sorted(
        {value for value in forbidden_paths if isinstance(value, str) and value},
        key=len,
        reverse=True,
    )
    counts = Counter()
    touched = []
    files_scanned = 0
    for path in _artifact_files(paths):
        text = _read_text(path)
        if text is None:
            continue
        files_scanned += 1
        original = text
        for secret in secrets:
            occurrences = text.count(secret)
            if occurrences:
                text = text.replace(secret, REDACTED_CREDENTIAL)
                counts["exact_credential"] += occurrences
        for host_path in host_paths:
            occurrences = text.count(host_path)
            if occurrences:
                text = text.replace(host_path, REDACTED_HOST_PATH)
                counts["host_path"] += occurrences
        for pattern in _CREDENTIAL_PATTERNS:
            if pattern.groups:
                text, n = pattern.subn(
                    lambda match: match.group(1) + REDACTED_CREDENTIAL,
                    text,
                )
            else:
                text, n = pattern.subn(REDACTED_CREDENTIAL, text)
            counts["credential_pattern"] += n
        if text != original:
            _write_private_text(path, text)
            touched.append(path.name)
    return {
        "files_scanned": files_scanned,
        "files_redacted": len(touched),
        "redacted_file_names": sorted(touched),
        "replacement_counts": dict(sorted(counts.items())),
    }


def scan_artifacts(paths, secret_values=(), forbidden_paths=()):
    """Return metadata-only findings; no matched text is included."""
    secrets = [
        value for value in secret_values if isinstance(value, str) and len(value) >= 8
    ]
    host_paths = [value for value in forbidden_paths if isinstance(value, str) and value]
    findings = []
    for path in _artifact_files(paths):
        text = _read_text(path)
        if text is None:
            continue
        categories = set()
        if any(secret in text for secret in secrets):
            categories.add("exact_credential")
        if any(host_path in text for host_path in host_paths):
            categories.add("host_path")
        if any(pattern.search(text) for pattern in _CREDENTIAL_PATTERNS):
            categories.add("credential_pattern")
        for category in sorted(categories):
            findings.append({"file_name": path.name, "category": category})
    return findings


def write_redaction_report(path, report, findings):
    value = {
        "schema_version": 1,
        **report,
        "post_redaction_findings": findings,
        "safe": not findings,
    }
    path = Path(path)
    _write_private_text(path, json.dumps(value, indent=2, sort_keys=True) + "\n")
