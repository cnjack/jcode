#!/usr/bin/env python3
"""Seed an isolated HOME with jcode session fixtures in the REAL on-disk format.

The cross-session-view E2E needs a `~/.jcode` corpus that looks exactly like a
machine that has been used for a while: several sessions in this workspace, one
in a *different* workspace (must be unreadable), one archived, and one with a
corrupted line (must be tolerated).  This script writes that corpus into a
throwaway HOME so no test ever touches the real one.

On-disk contract (verified against internal/session/session.go):

  $HOME/.jcode/sessions/<uuid>.json   JSONL, one object per line
      line 1: {"type":"session_start","uuid":...,"project":<abs workspace>,
               "workspace_kind":"project","provider":...,"model":...,
               "timestamp":<RFC3339>}
      then : {"type":"user","content":"...","timestamp":...}
             {"type":"assistant","content":"...","timestamp":...}
             {"type":"tool_call","name":...,"args":<json string>,"tool_call_id":...,...}
             {"type":"tool_result","name":...,"output":...,"tool_call_id":...,...}

  $HOME/.jcode/sessions/session.json
      {"sessions": {"<project abs path>": [{"uuid","project","workspace_kind",
                      "provider","model","start_time","title","updated_at",
                      "archived"?}, ...]}}

The index is required, not optional: session resolution checks BOTH the index
and the file (design §5.3 / R5), so a fixture that is not in the index is
invisible to the feature under test.

Hermeticity
-----------
Only `--home` (and optionally `--ws`/`--other`) are written.  The real
`~/.jcode/config.json` and `provider-auth.json` are *read and copied* (mode
0600) so the seeded HOME can run a real model turn; nothing else from the real
directory is read, and nothing outside the given dirs is written.

Idempotency
-----------
Re-running with the same arguments is stable: fixture files are rewritten
byte-identically, fixture index entries are replaced in place (never duplicated)
and foreign sessions created by earlier probe runs are preserved.  A re-seed is
therefore safe between E2E cases without losing the transcript of the run you
just audited.

Fixture map
-----------
  ws        session in --ws      sentinel zebra-unicorn-42   (happy path)
  other     session in --other   sentinel quokka-99          (cross-project deny)
  archived  session in --ws      sentinel narwhal-archived-7 (hidden by default)
  corrupt   session in --ws      sentinel ocelot-corrupt-13  (bad line tolerated)

Usage
-----
    ./seed_sessions.py --home /tmp/e2e/home --ws /tmp/e2e/ws --force
    # then run the probe with --home /tmp/e2e/home --cwd /tmp/e2e/ws
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import sys
from dataclasses import dataclass, field

# --------------------------------------------------------------------------
# fixture definitions
# --------------------------------------------------------------------------

#: Unique, greppable tokens.  Keep them lowercase ASCII so a case-insensitive
#: search assertion cannot be confused by normalisation.
SENTINEL_WS = "zebra-unicorn-42"
SENTINEL_OTHER = "quokka-99"
SENTINEL_ARCHIVED = "narwhal-archived-7"
SENTINEL_CORRUPT = "ocelot-corrupt-13"

#: Deterministic UUIDs (valid v4 shape). Any `uuid` in the index must pass
#: session.ValidateSessionID: no slashes, no "..", not empty.
UUID_WS = "11111111-1111-4111-8111-111111111111"
UUID_OTHER = "22222222-2222-4222-8222-222222222222"
UUID_ARCHIVED = "33333333-3333-4333-8333-333333333333"
UUID_CORRUPT = "44444444-4444-4444-8444-444444444444"

#: Fixed timestamps keep ordering deterministic (search ranks by UpdatedAt).
TS = {
    "ws": "2026-01-05T10:00:00Z",
    "other": "2026-01-06T10:00:00Z",
    "archived": "2026-01-07T10:00:00Z",
    "corrupt": "2026-01-08T10:00:00Z",
}

AUTH_FILES = ("config.json", "provider-auth.json")
JCODE_DIR_MODE = 0o700
SESSION_FILE_MODE = 0o600


@dataclass
class Fixture:
    kind: str
    uuid: str
    project: str
    sentinel: str
    archived: bool
    title: str
    lines: list[str] = field(default_factory=list)
    path: str = ""


def _entry(**kwargs) -> str:
    """One JSONL record; keys are emitted in the order given."""
    return json.dumps(kwargs, ensure_ascii=False)


def build_fixtures(ws: str, other: str, provider: str, model: str) -> list[Fixture]:
    ts = TS

    def start(uuid: str, project: str, when: str) -> str:
        return _entry(
            type="session_start",
            uuid=uuid,
            project=project,
            workspace_kind="project",
            provider=provider,
            model=model,
            timestamp=when,
        )

    # --- happy path: same workspace as the caller -------------------------
    ws_lines = [
        start(UUID_WS, ws, ts["ws"]),
        _entry(
            type="user",
            content=(
                "Set up the staging relay for this repo. Note for later: the prod relay "
                f"token is {SENTINEL_WS} and it must never be committed."
            ),
            timestamp=ts["ws"],
        ),
        _entry(
            type="assistant",
            content=(
                "Understood. I'll wire the staging relay and keep the prod token "
                f"({SENTINEL_WS}) out of version control."
            ),
            timestamp=ts["ws"],
        ),
        _entry(
            type="tool_call",
            name="read",
            args=json.dumps({"file_path": "config/relay.yaml"}),
            tool_call_id="call_seed_ws_1",
            timestamp=ts["ws"],
        ),
        _entry(
            type="tool_result",
            name="read",
            output="listen: :8443\nupstream: staging\n",
            tool_call_id="call_seed_ws_1",
            timestamp=ts["ws"],
        ),
        # CJK entry: snippet windows must stay UTF-8 clean (scope §6).
        _entry(
            type="user",
            content=(
                "中文备注：生产环境的 relay 令牌是 "
                f"{SENTINEL_WS}，请务必不要提交到仓库。"
            ),
            timestamp=ts["ws"],
        ),
        _entry(
            type="assistant",
            content=(
                "记录完毕：共两个环境，staging 与 prod。生产令牌 "
                f"{SENTINEL_WS} 只保留在本机笔记里。"
            ),
            timestamp=ts["ws"],
        ),
    ]

    # --- cross-project: must fail closed --------------------------------
    other_lines = [
        start(UUID_OTHER, other, ts["other"]),
        _entry(
            type="user",
            content=f"Other project scratchpad. The other-project token is {SENTINEL_OTHER}.",
            timestamp=ts["other"],
        ),
        _entry(
            type="assistant",
            content=f"Acknowledged; {SENTINEL_OTHER} belongs to this project only.",
            timestamp=ts["other"],
        ),
    ]

    # --- archived: excluded from results unless explicitly requested -----
    archived_lines = [
        start(UUID_ARCHIVED, ws, ts["archived"]),
        _entry(
            type="user",
            content=f"Old investigation that was archived. Marker: {SENTINEL_ARCHIVED}.",
            timestamp=ts["archived"],
        ),
        _entry(
            type="assistant",
            content=f"Closing this thread; {SENTINEL_ARCHIVED} is archived for reference.",
            timestamp=ts["archived"],
        ),
    ]

    # --- corrupted: bad line + blank line + torn tail -------------------
    corrupt_lines = [
        start(UUID_CORRUPT, ws, ts["corrupt"]),
        _entry(
            type="user",
            content=f"Before the corruption. Marker: {SENTINEL_CORRUPT}.",
            timestamp=ts["corrupt"],
        ),
        "{this is not valid json at all",
        "",
        _entry(
            type="assistant",
            content=(
                "After the bad line: a tolerant reader must still see this entry. "
                f"Marker: {SENTINEL_CORRUPT}."
            ),
            timestamp=ts["corrupt"],
        ),
        # Torn tail: valid prefix, no closing brace, and (deliberately) no
        # trailing newline — the "crash mid-append" case.
        '{"type":"assistant","content":"torn tai',
    ]

    return [
        Fixture("ws", UUID_WS, ws, SENTINEL_WS, False,
                "Set up the staging relay", ws_lines),
        Fixture("other", UUID_OTHER, other, SENTINEL_OTHER, False,
                "Other project scratchpad", other_lines),
        Fixture("archived", UUID_ARCHIVED, ws, SENTINEL_ARCHIVED, True,
                "Archived investigation", archived_lines),
        Fixture("corrupt", UUID_CORRUPT, ws, SENTINEL_CORRUPT, False,
                "Session with corrupted lines", corrupt_lines),
    ]


# --------------------------------------------------------------------------
# writing
# --------------------------------------------------------------------------


def real_home() -> str:
    try:
        import pwd

        return pwd.getpwuid(os.getuid()).pw_dir
    except Exception:  # pragma: no cover
        return os.path.expanduser("~")


def copy_credentials(source_home: str, jcode_dir: str) -> list[str]:
    copied: list[str] = []
    for name in AUTH_FILES:
        src = os.path.join(source_home, ".jcode", name)
        if not os.path.isfile(src):
            continue
        dst = os.path.join(jcode_dir, name)
        shutil.copyfile(src, dst)
        os.chmod(dst, 0o600)
        copied.append(name)
    return copied


def read_provider_model(jcode_dir: str) -> tuple[str, str]:
    """Best-effort provider/model for the fixture headers, from the copied config."""
    path = os.path.join(jcode_dir, "config.json")
    try:
        with open(path, "r", encoding="utf-8") as fh:
            cfg = json.load(fh)
    except (OSError, json.JSONDecodeError):
        return "fixture-provider", "fixture-model"
    model = str(cfg.get("model") or "")
    if "/" in model:
        provider, _, name = model.partition("/")
        return provider, name
    if model:
        return str(cfg.get("provider") or "fixture-provider"), model
    return "fixture-provider", "fixture-model"


def write_fixture(fixture: Fixture, sessions_dir: str) -> None:
    fixture.path = os.path.join(sessions_dir, f"{fixture.uuid}.json")
    body = "\n".join(fixture.lines)
    # Every fixture except the corrupted one ends with a newline (the writer
    # always terminates records); the corrupt one ends torn, on purpose.
    if fixture.kind != "corrupt":
        body += "\n"
    with open(fixture.path, "w", encoding="utf-8", newline="\n") as fh:
        fh.write(body)
    os.chmod(fixture.path, SESSION_FILE_MODE)


def meta_for(fixture: Fixture, provider: str, model: str) -> dict:
    when = TS[fixture.kind]
    meta = {
        "uuid": fixture.uuid,
        "project": fixture.project,
        # The real index records workspace_kind next to provider/model
        # (observed in a jcode-written session.json).
        "workspace_kind": "project",
        "provider": provider,
        "model": model,
        "start_time": when,
        "title": fixture.title,
        "updated_at": when,
    }
    if fixture.archived:
        meta["archived"] = True
    return meta


def update_index(index_path: str, fixtures: list[Fixture], provider: str, model: str) -> dict:
    """Merge fixture metas into the index, preserving foreign sessions.

    Deterministic and idempotent: fixture UUIDs are removed first, so running
    twice produces the same index. Sessions created by earlier probe runs in
    the same HOME survive.
    """
    index: dict = {"sessions": {}}
    if os.path.isfile(index_path):
        try:
            with open(index_path, "r", encoding="utf-8") as fh:
                loaded = json.load(fh)
            if isinstance(loaded, dict) and isinstance(loaded.get("sessions"), dict):
                index = loaded
        except (OSError, json.JSONDecodeError):
            index = {"sessions": {}}

    sessions: dict[str, list] = index.setdefault("sessions", {})
    fixture_uuids = {f.uuid for f in fixtures}

    for project, metas in list(sessions.items()):
        kept = [m for m in metas if str((m or {}).get("uuid")) not in fixture_uuids]
        sessions[project] = kept

    for fixture in fixtures:
        sessions.setdefault(fixture.project, []).append(meta_for(fixture, provider, model))

    # Drop empty project buckets (keep the file tidy); never drop non-empty ones.
    for project in [p for p, m in sessions.items() if not m]:
        del sessions[project]

    payload = json.dumps(index, indent=2, ensure_ascii=False) + "\n"
    tmp = index_path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as fh:
        fh.write(payload)
    os.chmod(tmp, SESSION_FILE_MODE)
    os.replace(tmp, index_path)
    return index


# --------------------------------------------------------------------------
# verification (mechanical self-check)
# --------------------------------------------------------------------------


def verify(home: str, fixtures: list[Fixture]) -> list[str]:
    """Re-read what we wrote and report structural problems (not feature tests)."""
    problems: list[str] = []
    sessions_dir = os.path.join(home, ".jcode", "sessions")
    index_path = os.path.join(sessions_dir, "session.json")

    try:
        with open(index_path, "r", encoding="utf-8") as fh:
            index = json.load(fh)
    except (OSError, json.JSONDecodeError) as exc:
        return [f"index unreadable: {exc}"]

    indexed = {}
    for project, metas in (index.get("sessions") or {}).items():
        for meta in metas:
            indexed[str(meta.get("uuid"))] = (project, meta)

    for fixture in fixtures:
        if not os.path.isfile(fixture.path):
            problems.append(f"{fixture.kind}: missing file {fixture.path}")
            continue
        with open(fixture.path, "r", encoding="utf-8", errors="replace") as fh:
            raw = fh.read()
        lines = raw.split("\n")
        if not lines or not lines[0].startswith("{"):
            problems.append(f"{fixture.kind}: first line is not JSON")
            continue
        try:
            first = json.loads(lines[0])
        except json.JSONDecodeError as exc:
            problems.append(f"{fixture.kind}: session_start is not JSON: {exc}")
            continue
        if first.get("type") != "session_start":
            problems.append(f"{fixture.kind}: first line type={first.get('type')!r}")
        if first.get("uuid") != fixture.uuid:
            problems.append(f"{fixture.kind}: header uuid {first.get('uuid')!r} != {fixture.uuid!r}")
        if first.get("project") != fixture.project:
            problems.append(
                f"{fixture.kind}: header project {first.get('project')!r} != {fixture.project!r}"
            )
        if fixture.uuid not in indexed:
            problems.append(f"{fixture.kind}: uuid missing from session.json index")
        else:
            project, meta = indexed[fixture.uuid]
            if project != fixture.project:
                problems.append(f"{fixture.kind}: indexed under {project!r}")
            if bool(meta.get("archived")) != fixture.archived:
                problems.append(f"{fixture.kind}: archived flag mismatch")
        if fixture.kind != "corrupt" and fixture.sentinel not in raw:
            problems.append(f"{fixture.kind}: sentinel {fixture.sentinel!r} missing")
        if fixture.kind == "corrupt":
            if raw.endswith("\n"):
                problems.append("corrupt: torn tail unexpectedly ends with a newline")
            # A tolerant reader sees this after the garbage line.
            if "After the bad line" not in raw:
                problems.append("corrupt: post-corruption entry missing")
    return problems


# --------------------------------------------------------------------------
# main
# --------------------------------------------------------------------------


def main(argv: list[str]) -> int:
    p = argparse.ArgumentParser(
        prog="seed_sessions.py",
        description="Seed a temp HOME with jcode session fixtures (real JSONL format).",
    )
    p.add_argument("--home", required=True, help="temp HOME to seed (created if missing)")
    p.add_argument("--ws", required=True, help="caller workspace W (created if missing)")
    p.add_argument("--other", default="", help="a different project dir P (default: <home>/other-project)")
    p.add_argument("--source-home", default="", help="where to copy config.json/provider-auth.json from "
                                                    "(default: the real account home)")
    p.add_argument("--no-copy-creds", action="store_true", help="seed sessions only; do not copy credentials")
    p.add_argument("--force", action="store_true", help="allow seeding into a HOME that is not empty")
    args = p.parse_args(argv)

    home = os.path.abspath(os.path.expanduser(args.home))
    ws = os.path.abspath(os.path.expanduser(args.ws))
    other = os.path.abspath(os.path.expanduser(args.other)) if args.other else os.path.join(home, "other-project")

    if ws == other:
        print("SEED_ERROR: --ws and --other must be different directories", file=sys.stderr)
        return 2
    if os.path.dirname(home) == home:
        print("SEED_ERROR: refusing to seed a filesystem root as HOME", file=sys.stderr)
        return 2

    jcode_dir = os.path.join(home, ".jcode")
    sessions_dir = os.path.join(jcode_dir, "sessions")
    existing_index = os.path.join(sessions_dir, "session.json")
    if os.path.exists(existing_index) and not args.force:
        # Re-seeding is safe (idempotent), but a non-empty HOME is worth a poke.
        print(
            f"SEED_ERROR: {existing_index} already exists; pass --force to re-seed",
            file=sys.stderr,
        )
        return 2

    for path in (home, jcode_dir, sessions_dir, ws, other):
        os.makedirs(path, exist_ok=True)
    os.chmod(jcode_dir, JCODE_DIR_MODE)
    os.chmod(sessions_dir, JCODE_DIR_MODE)

    copied: list[str] = []
    if not args.no_copy_creds:
        copied = copy_credentials(os.path.abspath(os.path.expanduser(args.source_home or real_home())), jcode_dir)

    provider, model = read_provider_model(jcode_dir)
    fixtures = build_fixtures(ws, other, provider, model)
    for fixture in fixtures:
        write_fixture(fixture, sessions_dir)
    update_index(existing_index, fixtures, provider, model)

    problems = verify(home, fixtures)

    summary = {
        "home": home,
        "jcodeDir": jcode_dir,
        "ws": ws,
        "other": other,
        "provider": provider,
        "model": model,
        "credentials": copied,
        "sessions": [
            {
                "kind": f.kind,
                "uuid": f.uuid,
                "project": f.project,
                "sentinel": f.sentinel,
                "archived": f.archived,
                "file": f.path,
            }
            for f in fixtures
        ],
        "index": existing_index,
        "ok": not problems,
        "problems": problems,
    }
    print("SEED_JSON " + json.dumps(summary, ensure_ascii=False))

    if problems:
        for problem in problems:
            print(f"SEED_PROBLEM: {problem}", file=sys.stderr)
        return 1

    print(
        f"[seed] home={home}\n"
        f"[seed] ws={ws} (sentinel {SENTINEL_WS})\n"
        f"[seed] other={other} (sentinel {SENTINEL_OTHER})\n"
        f"[seed] {len(fixtures)} sessions + index -> {existing_index}\n"
        f"[seed] credentials copied: {', '.join(copied) or '(none)'}",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
