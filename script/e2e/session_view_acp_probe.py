#!/usr/bin/env python3
"""Drive a headless `jcode acp` run with a REAL model and capture the whole turn.

This is the transport layer for the cross-session-view E2E (design doc
internal-doc/cross-session-view-design.md §6.3).  It is deliberately a *dumb
driver*: it speaks ACP, returns the assistant's final text plus the emitted
tool-call stream, and performs only the assertions you explicitly ask for with
`--expect-*` / `--forbid-*`.  Deciding what "pass" means for the feature is the
job of the caller (phase 2 harness / test report).

Wire protocol (verified against internal/command/acp.go + the coder/acp-go-sdk
v0.13.5 schema):

    <bin> acp                       # newline-delimited JSON-RPC 2.0 over stdio
    initialize          {protocolVersion:1, clientCapabilities:{fs:{...},terminal:false}}
    session/new         {cwd, mcpServers:[]}
    session/set_mode    {sessionId, modeId}            # optional (--mode)
    session/prompt      {sessionId, prompt:[{type:"text",text:"..."}]}
    session/cancel      {sessionId}                    # notification, on timeout

Server -> client notifications:

    session/update       {sessionId, update:{sessionUpdate:"agent_message_chunk",
                                             content:{type:"text",text:"..."}}}
    session/update       {sessionId, update:{sessionUpdate:"tool_call" | "tool_call_update",
                                             toolCallId, title, kind, status,
                                             rawInput, rawOutput, content}}
    session/request_permission {sessionId, toolCall, options:[{optionId,kind,name}]}

`session/request_permission` is a *request* (has an id) and MUST be answered or
the agent blocks forever.  Read-only tools (the whole point of session_view)
should never trigger it; the probe counts them so a test can assert that.

Output contract
---------------
stdout : exactly one line, `PROBE_JSON {json}` — machine readable summary.
stderr : human readable progress + trimmed transcript (safe to eyeball / paste).
exit   : 0 ok, 2 usage/precondition, 3 initialize/session-new failure or timeout,
         4 prompt error / child died, 5 assertion mismatch.

Result fields worth knowing
---------------------------
  finalText        concatenated `agent_message_chunk` text (the answer)
  toolCalls        ACP `tool_call`/`tool_call_update` folded by toolCallId
                   (title/kind/status/rawInput/rawOutput) — live view, but the
                   title is a human label for some tools
  persisted        the caller's own JSONL transcript read back from
                   `$HOME/.jcode/sessions/<uuid>.json`: one row per tool_call
                   with its args and tool_result output/error.  This is the
                   authoritative tool-call audit used by design §6.3, because
                   it carries the real tool name (e.g. `execute`).
                   It also carries `visibleTools` / `modelRequests`, i.e. the
                   exact tool schema list the recorder journalled for the last
                   model request — deterministic evidence that the session_*
                   tools were (or were not) attached, independent of what the
                   model chose to call.  Assert with `--expect-attached-tool` /
                   `--forbid-attached-tool`.
  permissionRequests  every `session/request_permission` (should stay empty for
                   read-only session_* tools)
  stopReason, elapsedMs, counts, childExit, stderrTail, failures

Hermeticity: the child only ever sees the HOME we hand it.  With `--home` we use
a caller-provided directory (e.g. one produced by seed_sessions.py); otherwise we
create a throwaway HOME and copy `~/.jcode/config.json` +
`~/.jcode/provider-auth.json` into it.  The real `~/.jcode` is only ever read.

Examples
--------
    # smoke: real model, no tools needed
    ./session_view_acp_probe.py --bin /tmp/jcode-e2e --cwd /tmp/ws \
        --timeout 180 --prompt "Reply with exactly: PONG"

    # phase-2 positive case against a seeded HOME
    ./session_view_acp_probe.py --bin /tmp/jcode-e2e --home /tmp/e2e/home \
        --cwd /tmp/e2e/ws --mode full_access --timeout 300 \
        --prompt "Use session_search for zebra-unicorn-42, then session_read the hit" \
        --expect-contains zebra-unicorn-42 --expect-tool session_search

    # phase-2 negative case: must fail closed with the exact error string
    ./session_view_acp_probe.py --bin /tmp/jcode-e2e --home /tmp/e2e/home \
        --cwd /tmp/e2e/ws --timeout 300 \
        --prompt "Read session 22222222-2222-4222-8222-222222222222 with session_read" \
        --expect-tool session_read \
        --expect-tool-result-contains 'ERROR[SESSION_VIEW_NOT_FOUND]' \
        --expect-not-contains quokka-99
"""

from __future__ import annotations

import argparse
import atexit
import json
import os
import pwd
import queue
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time
import traceback

PROBE_VERSION = "0.1.0"

# Exit codes (documented in the module docstring and README).
EXIT_OK = 0
EXIT_USAGE = 2
EXIT_SETUP = 3
EXIT_PROMPT = 4
EXIT_ASSERT = 5

AUTH_FILES = ("config.json", "provider-auth.json")

# We claim no filesystem/terminal capabilities: the agent must do all I/O itself.
# (This is also what makes "the model used grep/read instead of session_*"
# observable: it can only do that through its own tools, never through us.)
CLIENT_CAPABILITIES = {
    "fs": {"readTextFile": False, "writeTextFile": False},
    "terminal": False,
}


# --------------------------------------------------------------------------
# credentials / HOME handling
# --------------------------------------------------------------------------


def real_home() -> str:
    """The account's real home directory, independent of $HOME."""
    try:
        return pwd.getpwuid(os.getuid()).pw_dir
    except Exception:  # pragma: no cover - exotic platforms
        return os.path.expanduser("~")


def prepare_temp_home(source_home: str) -> str:
    """Create a throwaway HOME and copy model credentials into it (read-only src)."""
    home = tempfile.mkdtemp(prefix="jcode-e2e-home-")
    jcode_dir = os.path.join(home, ".jcode")
    os.makedirs(jcode_dir, mode=0o700, exist_ok=True)
    for name in AUTH_FILES:
        src = os.path.join(source_home, ".jcode", name)
        if not os.path.exists(src):
            continue
        dst = os.path.join(jcode_dir, name)
        shutil.copyfile(src, dst)
        os.chmod(dst, 0o600)
    return home


def child_env(home: str) -> dict:
    env = dict(os.environ)
    env["HOME"] = home
    # Keep the run quiet and deterministic; the ACP server logs to $HOME/.jcode.
    env.setdefault("NO_COLOR", "1")
    env.pop("JCODE_CONFIG", None)
    return env


# --------------------------------------------------------------------------
# ACP client
# --------------------------------------------------------------------------


class ACPError(RuntimeError):
    """Protocol-level failure (bad response, rpc error, premature EOF)."""


class ACPClient:
    """Minimal, dependency-free ACP client over stdio.

    One reader thread owns the child's stdout.  Requests are matched to
    responses by id; notifications are appended to `events` and folded into
    `agent_text` / `tool_calls` / `permission_requests`.
    """

    def __init__(self, bin_path: str, cwd: str, env: dict, log=print):
        self.bin_path = bin_path
        self.cwd = cwd
        self.env = env
        self.log = log
        self.stdin = None
        self.stderr_chunks: list[str] = []
        self.events: list[dict] = []
        self.agent_text_parts: list[str] = []
        self.thought_text_parts: list[str] = []
        self.tool_calls: dict[str, dict] = {}
        self.tool_call_order: list[str] = []
        self.permission_requests: list[dict] = []
        self.unknown_notifications: dict[str, int] = {}
        self._next_id = 0
        self._pending: dict[int, queue.Queue] = {}
        self._lock = threading.Lock()
        self._reader: threading.Thread | None = None
        self._stderr_reader: threading.Thread | None = None
        self._eof = threading.Event()
        self.proc: subprocess.Popen | None = None
        self.permission_mode = "allow"

    # -- lifecycle ---------------------------------------------------------

    def start(self) -> None:
        self.proc = subprocess.Popen(
            [self.bin_path, "acp"],
            cwd=self.cwd,
            env=self.env,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            bufsize=0,
        )
        self.stdin = self.proc.stdin
        self._reader = threading.Thread(target=self._read_stdout, name="acp-stdout", daemon=True)
        self._reader.start()
        self._stderr_reader = threading.Thread(
            target=self._read_stderr, name="acp-stderr", daemon=True
        )
        self._stderr_reader.start()

    def close(self, grace: float = 5.0) -> int | None:
        """Shut stdin, wait for the child, kill if it lingers. Returns exit code."""
        if self.proc is None:
            return None
        try:
            if self.stdin and not self.stdin.closed:
                self.stdin.close()
        except OSError:
            pass
        try:
            return self.proc.wait(timeout=grace)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            try:
                return self.proc.wait(timeout=grace)
            except subprocess.TimeoutExpired:  # pragma: no cover
                return None

    def kill(self) -> None:
        if self.proc and self.proc.poll() is None:
            try:
                self.proc.kill()
            except OSError:
                pass

    # -- io ----------------------------------------------------------------

    def _read_stderr(self) -> None:
        assert self.proc and self.proc.stderr
        for raw in iter(self.proc.stderr.readline, b""):
            line = raw.decode("utf-8", "replace").rstrip()
            self.stderr_chunks.append(line)
            if len(self.stderr_chunks) > 4000:  # bounded: keep the tail
                del self.stderr_chunks[:2000]

    def _read_stdout(self) -> None:
        assert self.proc and self.proc.stdout
        try:
            for raw in iter(self.proc.stdout.readline, b""):
                line = raw.decode("utf-8", "replace").strip()
                if not line:
                    continue
                try:
                    msg = json.loads(line)
                except json.JSONDecodeError:
                    self.log(f"[probe] non-JSON on stdout (ignored): {line[:200]}")
                    continue
                self._dispatch(msg)
        finally:
            self._eof.set()
            with self._lock:
                pending = list(self._pending.values())
            for q in pending:
                q.put(("__eof__", None))

    def _dispatch(self, msg: dict) -> None:
        if "id" in msg and msg["id"] is not None and ("result" in msg or "error" in msg):
            with self._lock:
                q = self._pending.pop(int(msg["id"]), None)
            if q:
                q.put(("response", msg))
            return

        method = msg.get("method")
        if method is None:
            self.unknown_notifications["<no method>"] = (
                self.unknown_notifications.get("<no method>", 0) + 1
            )
            return

        # Server -> client *request*: must answer, or the agent hangs.
        if "id" in msg and msg["id"] is not None:
            self._handle_server_request(msg, method)
            return

        # Notification.
        self.events.append(msg)
        if method == "session/update":
            self._fold_session_update(msg.get("params") or {})
        else:
            self.unknown_notifications[method] = self.unknown_notifications.get(method, 0) + 1

    def _handle_server_request(self, msg: dict, method: str) -> None:
        rid = msg["id"]
        params = msg.get("params") or {}
        if method == "session/request_permission":
            req = {
                "id": rid,
                "sessionId": params.get("sessionId"),
                "toolCall": params.get("toolCall") or {},
                "options": params.get("options") or [],
            }
            self.permission_requests.append(req)
            title = req["toolCall"].get("title") or req["toolCall"].get("toolCallId")
            self.log(f"[probe] permission requested for {title!r} (answering: {self.permission_mode})")
            option = self._choose_permission_option(req["options"])
            if option is None:
                self._send_error(rid, -32602, "no permission options offered")
                return
            self._send_result(rid, {"outcome": {"outcome": "selected", "optionId": option}})
            return
        if method == "fs/read_text_file":
            self._send_error(rid, -32601, "fs/read_text_file: client capability disabled")
            return
        if method == "fs/write_text_file":
            self._send_error(rid, -32601, "fs/write_text_file: client capability disabled")
            return
        if method.startswith("terminal/"):
            self._send_error(rid, -32601, f"{method}: client capability disabled")
            return
        self.log(f"[probe] unsupported server request {method!r}; replying method-not-found")
        self._send_error(rid, -32601, f"method not found: {method}")

    def _choose_permission_option(self, options: list[dict]) -> str | None:
        kind_rank = {"allow_always": 0, "allow_once": 1}
        candidates = [o for o in options if o.get("kind") in kind_rank]
        if not candidates:
            # protocol allows arbitrary kinds; fall back to "allow" by label
            candidates = [o for o in options if "allow" in str(o.get("name", "")).lower()]
        if not candidates:
            candidates = options
        if not candidates:
            return None
        if self.permission_mode == "deny":
            deny = [o for o in options if str(o.get("kind", "")).startswith("reject")]
            if deny:
                return str(deny[0].get("optionId"))
        candidates.sort(key=lambda o: kind_rank.get(o.get("kind"), 9))
        return str(candidates[0].get("optionId"))

    def _fold_session_update(self, params: dict) -> None:
        update = params.get("update") or {}
        kind = update.get("sessionUpdate")
        if kind == "agent_message_chunk":
            content = update.get("content") or {}
            if content.get("type") == "text":
                self.agent_text_parts.append(content.get("text") or "")
            return
        if kind == "agent_thought_chunk":
            content = update.get("content") or {}
            if content.get("type") == "text":
                self.thought_text_parts.append(content.get("text") or "")
            return
        if kind in ("tool_call", "tool_call_update"):
            call_id = str(update.get("toolCallId") or "")
            if not call_id:
                return
            if call_id not in self.tool_calls:
                self.tool_calls[call_id] = {
                    "toolCallId": call_id,
                    "title": "",
                    "kind": "",
                    "status": "",
                    "rawInput": None,
                    "rawOutput": None,
                    "content": [],
                }
                self.tool_call_order.append(call_id)
            entry = self.tool_calls[call_id]
            for key in ("title", "kind", "status", "rawInput", "rawOutput"):
                if update.get(key) is not None:
                    entry[key] = update[key]
            if update.get("content"):
                entry["content"] = update["content"]

    # -- rpc ---------------------------------------------------------------

    def _write(self, payload: dict) -> None:
        if not self.stdin:
            raise ACPError("stdin closed")
        data = (json.dumps(payload, ensure_ascii=False) + "\n").encode("utf-8")
        try:
            self.stdin.write(data)
            self.stdin.flush()
        except (BrokenPipeError, ValueError) as exc:
            raise ACPError(f"child stdin closed: {exc}") from exc

    def _send_result(self, rid, result) -> None:
        self._write({"jsonrpc": "2.0", "id": rid, "result": result})

    def _send_error(self, rid, code: int, message: str) -> None:
        self._write({"jsonrpc": "2.0", "id": rid, "error": {"code": code, "message": message}})

    def request(self, method: str, params: dict, timeout: float, label: str | None = None) -> dict:
        with self._lock:
            self._next_id += 1
            rid = self._next_id
            q: queue.Queue = queue.Queue()
            self._pending[rid] = q
        self._write({"jsonrpc": "2.0", "id": rid, "method": method, "params": params})
        tag = label or method
        deadline = time.time() + timeout
        while True:
            remaining = deadline - time.time()
            if remaining <= 0:
                with self._lock:
                    self._pending.pop(rid, None)
                raise TimeoutError(f"{tag}: no response within {timeout:.0f}s")
            try:
                kind, msg = q.get(timeout=remaining)
            except queue.Empty:
                continue
            if kind == "__eof__":
                code = self.proc.poll() if self.proc else None
                raise ACPError(f"{tag}: agent closed the connection (exit={code})")
            if msg.get("error"):
                err = msg["error"]
                raise ACPError(f"{tag}: rpc error {err.get('code')}: {err.get('message')}")
            return msg.get("result") or {}

    def notify(self, method: str, params: dict) -> None:
        self._write({"jsonrpc": "2.0", "method": method, "params": params})

    # -- protocol conveniences --------------------------------------------

    def initialize(self, timeout: float) -> dict:
        return self.request(
            "initialize",
            {
                "protocolVersion": 1,
                "clientCapabilities": CLIENT_CAPABILITIES,
                "clientInfo": {"name": "jcode-e2e-probe", "version": PROBE_VERSION},
            },
            timeout=timeout,
            label="initialize",
        )

    def new_session(self, cwd: str, timeout: float) -> dict:
        return self.request(
            "session/new",
            {"cwd": cwd, "mcpServers": []},
            timeout=timeout,
            label="session/new",
        )

    def set_mode(self, session_id: str, mode: str, timeout: float) -> dict:
        return self.request(
            "session/set_mode",
            {"sessionId": session_id, "modeId": mode},
            timeout=timeout,
            label="session/set_mode",
        )

    def prompt(self, session_id: str, text: str, timeout: float) -> dict:
        # Fire the prompt, then block. The reader thread keeps folding updates,
        # so agent text / tool calls keep accumulating while we wait.
        with self._lock:
            self._next_id += 1
            rid = self._next_id
            q: queue.Queue = queue.Queue()
            self._pending[rid] = q
        self._write(
            {
                "jsonrpc": "2.0",
                "id": rid,
                "method": "session/prompt",
                "params": {
                    "sessionId": session_id,
                    "prompt": [{"type": "text", "text": text}],
                },
            }
        )
        deadline = time.time() + timeout
        while True:
            remaining = deadline - time.time()
            if remaining <= 0:
                try:
                    self.notify("session/cancel", {"sessionId": session_id})
                except ACPError:
                    pass
                with self._lock:
                    self._pending.pop(rid, None)
                raise TimeoutError(f"session/prompt: turn exceeded {timeout:.0f}s")
            try:
                kind, msg = q.get(timeout=min(remaining, 1.0))
            except queue.Empty:
                if self.proc and self.proc.poll() is not None:
                    with self._lock:
                        self._pending.pop(rid, None)
                    raise ACPError(
                        f"session/prompt: agent exited mid-turn (exit={self.proc.returncode})"
                    )
                continue
            if kind == "__eof__":
                code = self.proc.poll() if self.proc else None
                raise ACPError(f"session/prompt: agent closed the connection (exit={code})")
            if msg.get("error"):
                err = msg["error"]
                raise ACPError(f"session/prompt: rpc error {err.get('code')}: {err.get('message')}")
            return msg.get("result") or {}

    # -- accessors ---------------------------------------------------------

    @property
    def agent_text(self) -> str:
        return "".join(self.agent_text_parts)

    @property
    def thought_text(self) -> str:
        return "".join(self.thought_text_parts)

    def tool_call_list(self) -> list[dict]:
        return [self.tool_calls[cid] for cid in self.tool_call_order]


# --------------------------------------------------------------------------
# assertions
# --------------------------------------------------------------------------


def tool_name(call: dict) -> str:
    """Best-effort tool identity from an ACP tool_call entry.

    jcode titles generic tools with the bare tool name (`session_search`) and
    parameterised ones with `Name <detail>` (`Read /path`), so both shapes map
    back to the tool name.
    """
    title = str(call.get("title") or "").strip()
    return title.split(" ", 1)[0] if title else ""


def check_expectations(result: dict, args) -> list[str]:
    """Return a list of human-readable failures for the requested assertions."""
    failures: list[str] = []
    text = result["finalText"]
    haystack = text.lower()
    for needle in args.expect_contains:
        if needle.lower() not in haystack:
            failures.append(f"expected final text to contain {needle!r}")
    for needle in args.expect_not_contains:
        if needle.lower() in haystack:
            failures.append(f"expected final text to NOT contain {needle!r}")

    seen = {tool_name(c) for c in result["toolCalls"]}
    seen.discard("")
    # The persisted transcript is the authoritative tool-call audit
    # (design §6.3): ACP titles are lossy for tools like `execute`, whose title
    # is a human sentence rather than the tool name.
    persisted = result.get("persisted") or {}
    for tool in persisted.get("tools") or []:
        if tool.get("name"):
            seen.add(str(tool["name"]))
    for name in args.expect_tool:
        if name not in seen:
            failures.append(f"expected a tool call named {name!r}; saw {sorted(seen)}")
    for name in args.forbid_tool:
        if name in seen:
            failures.append(f"forbade tool call {name!r} but it was called")
    for needle in args.expect_tool_result_contains:
        hits = [
            tool for tool in (persisted.get("tools") or [])
            if needle.lower() in str(tool.get("result") or "").lower()
        ]
        if not hits:
            failures.append(f"no tool result contained {needle!r}")
    for needle in args.forbid_tool_result_contains:
        hits = [
            tool for tool in (persisted.get("tools") or [])
            if needle.lower() in str(tool.get("result") or "").lower()
        ]
        if hits:
            failures.append(
                f"tool result contained forbidden {needle!r} "
                f"(tools: {[h.get('name') for h in hits]})"
            )
    if args.expect_no_permission and result["permissionRequests"]:
        failures.append(
            f"expected no permission prompts, saw {len(result['permissionRequests'])}"
        )

    # Transport-level tool registration evidence, straight from the recorder's
    # model_request observation — independent of what the model chose to call.
    attached = set(persisted.get("visibleTools") or [])
    if (args.expect_attached_tool or args.forbid_attached_tool) and not persisted.get("modelRequests"):
        failures.append(
            "no model_request observation in the transcript; cannot verify the attached tool list"
        )
    for name in args.expect_attached_tool:
        if name not in attached:
            failures.append(f"expected {name!r} in the attached tool list; saw {sorted(attached)}")
    for name in args.forbid_attached_tool:
        if name in attached:
            failures.append(f"{name!r} is attached but must not be; saw {sorted(attached)}")
    return failures


# --------------------------------------------------------------------------
# persisted transcript (authoritative tool-call audit)
# --------------------------------------------------------------------------


def load_persisted_session(home: str, session_id: str, cwd: str) -> dict:
    """Read back the caller's own JSONL transcript produced by this run.

    ACP session ids are `sess_<recorder-uuid>` and the recorder writes
    `$HOME/.jcode/sessions/<uuid>.json`, so the file is derivable.  A scan is
    used as a fallback for robustness.  Parsing is deliberately tolerant: a
    half-written tail must not break the audit.
    """
    sessions_dir = os.path.join(home, ".jcode", "sessions")
    path = ""
    if session_id.startswith("sess_"):
        candidate = os.path.join(sessions_dir, session_id[len("sess_"):] + ".json")
        if os.path.isfile(candidate):
            path = candidate
    if not path and os.path.isdir(sessions_dir):
        newest = 0.0
        for name in os.listdir(sessions_dir):
            if not name.endswith(".json") or name == "session.json":
                continue
            full = os.path.join(sessions_dir, name)
            try:
                mtime = os.path.getmtime(full)
            except OSError:
                continue
            if mtime > newest:
                try:
                    with open(full, "r", encoding="utf-8", errors="replace") as fh:
                        first = json.loads(fh.readline() or "{}")
                except (OSError, json.JSONDecodeError):
                    continue
                if first.get("project") == cwd:
                    newest = mtime
                    path = full
    if not path:
        return {
            "file": "",
            "entries": 0,
            "badLines": 0,
            "tools": [],
            "turns": [],
            "visibleTools": [],
            "modelRequests": 0,
        }

    entries: list[dict] = []
    bad = 0
    with open(path, "r", encoding="utf-8", errors="replace") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                entries.append(json.loads(line))
            except json.JSONDecodeError:
                bad += 1

    tools: list[dict] = []
    by_id: dict[str, dict] = {}
    turns: list[dict] = []
    visible_tools: list[str] = []
    model_requests = 0
    for entry in entries:
        kind = entry.get("type")
        if kind == "user":
            turns.append({"role": "user", "content": str(entry.get("content") or "")[:200]})
        elif kind == "assistant":
            turns.append({"role": "assistant", "content": str(entry.get("content") or "")[:200]})
        elif kind == "tool_observation":
            # The recorder journals the exact tool schema list sent to the model
            # (`visible_names`). That makes "were the session_* tools attached?"
            # a deterministic fact instead of a guess from model behaviour.
            obs = entry.get("tool_observation") or {}
            if obs.get("kind") == "model_request":
                names = obs.get("visible_names") or []
                if names:
                    visible_tools = [str(n) for n in names]
                    model_requests += 1
        elif kind == "tool_call":
            call = {
                "toolCallId": entry.get("tool_call_id") or "",
                "name": entry.get("name") or "",
                "args": entry.get("args") or "",
                "result": "",
                "error": "",
                "denied": bool(entry.get("denied")),
            }
            tools.append(call)
            if call["toolCallId"]:
                by_id[call["toolCallId"]] = call
        elif kind == "tool_result":
            call = by_id.get(entry.get("tool_call_id") or "")
            if call is None:
                call = {
                    "toolCallId": entry.get("tool_call_id") or "",
                    "name": entry.get("name") or "",
                    "args": "",
                    "result": "",
                    "error": "",
                    "denied": bool(entry.get("denied")),
                }
                tools.append(call)
            call["result"] = entry.get("output") or ""
            call["error"] = entry.get("error") or ""
            call["denied"] = call["denied"] or bool(entry.get("denied"))

    return {
        "file": path,
        "entries": len(entries),
        "badLines": bad,
        "tools": tools,
        "turns": turns,
        "visibleTools": visible_tools,
        "modelRequests": model_requests,
    }


# --------------------------------------------------------------------------
# main
# --------------------------------------------------------------------------


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog="session_view_acp_probe.py",
        description="Drive `jcode acp` with a real model and capture text + tool calls.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    p.add_argument("--bin", default=os.environ.get("JCODE_BIN", ""),
                   help="path to the jcode binary (default: $JCODE_BIN; build with "
                        "`go build -o /tmp/jcode-e2e ./cmd/jcode`)")
    p.add_argument("--cwd", default="", help="session working directory / project (absolute or resolvable)")
    p.add_argument("--home", default="", help="use this HOME instead of a fresh temp one (e.g. from seed_sessions.py)")
    p.add_argument("--source-home", default="", help="where to read config.json/provider-auth.json from "
                                                    "when making a temp HOME (default: the real account home)")
    p.add_argument("--prompt", default="", help="prompt text")
    p.add_argument("--prompt-file", default="", help="read the prompt from a file instead")
    p.add_argument("--mode", default="", help="session mode to set: approval|plan|auto|full_access")
    p.add_argument("--timeout", type=float, default=600.0, help="prompt turn timeout, seconds (default 600)")
    p.add_argument("--setup-timeout", type=float, default=120.0,
                   help="initialize + session/new timeout, seconds (default 120)")
    p.add_argument("--drain-ms", type=int, default=500,
                   help="ms to keep reading notifications after the turn ends (default 500)")
    p.add_argument("--permission-mode", choices=("allow", "deny"), default="allow",
                   help="how to answer session/request_permission (default allow)")
    p.add_argument("--json-out", default="", help="write the full result (incl. all events) as JSON here")
    p.add_argument("--events-out", default="", help="write the raw notification stream as JSONL here")
    p.add_argument("--keep-home", action="store_true", help="do not delete an auto-created temp HOME")
    p.add_argument("--summary-chars", type=int, default=1200, help="trim final text in the stderr summary")
    p.add_argument("--quiet", action="store_true", help="suppress the stderr transcript summary")
    p.add_argument("--verbose", action="store_true", help="log every notification kind")
    p.add_argument("--expect-contains", action="append", default=[], metavar="STR",
                   help="assert final text contains STR (case-insensitive); repeatable")
    p.add_argument("--expect-not-contains", action="append", default=[], metavar="STR",
                   help="assert final text does NOT contain STR; repeatable")
    p.add_argument("--expect-tool", action="append", default=[], metavar="NAME",
                   help="assert a tool call named NAME was emitted; repeatable")
    p.add_argument("--forbid-tool", action="append", default=[], metavar="NAME",
                   help="assert tool NAME was never called; repeatable")
    p.add_argument("--expect-tool-result-contains", action="append", default=[], metavar="STR",
                   help="assert some persisted tool_result output contains STR; repeatable")
    p.add_argument("--forbid-tool-result-contains", action="append", default=[], metavar="STR",
                   help="assert NO persisted tool_result output contains STR; repeatable")
    p.add_argument("--expect-attached-tool", action="append", default=[], metavar="NAME",
                   help="assert NAME was in the tool schema list sent to the model "
                        "(deterministic, from the recorder's model_request observation)")
    p.add_argument("--forbid-attached-tool", action="append", default=[], metavar="NAME",
                   help="assert NAME was NOT in the tool schema list sent to the model")
    p.add_argument("--expect-no-permission", action="store_true",
                   help="assert the agent asked for no permission prompts")
    return p


def fail(code: int, message: str, client: ACPClient | None = None) -> int:
    print(f"PROBE_ERROR: {message}", file=sys.stderr)
    if client is not None and client.stderr_chunks:
        print("--- child stderr (tail) ---", file=sys.stderr)
        for line in client.stderr_chunks[-40:]:
            print(line, file=sys.stderr)
    return code


def main(argv: list[str]) -> int:
    args = build_parser().parse_args(argv)

    if not args.bin:
        return fail(EXIT_USAGE, "missing --bin (or $JCODE_BIN)")
    if not os.path.isfile(args.bin) or not os.access(args.bin, os.X_OK):
        return fail(EXIT_USAGE, f"--bin is not an executable file: {args.bin}")

    prompt = args.prompt
    if args.prompt_file:
        try:
            with open(args.prompt_file, "r", encoding="utf-8") as fh:
                prompt = fh.read()
        except OSError as exc:
            return fail(EXIT_USAGE, f"cannot read --prompt-file: {exc}")
    if not prompt.strip():
        return fail(EXIT_USAGE, "missing --prompt/--prompt-file")

    cwd = os.path.abspath(os.path.expanduser(args.cwd or os.getcwd()))
    if not os.path.isdir(cwd):
        return fail(EXIT_USAGE, f"--cwd is not a directory: {cwd}")

    temp_home = ""
    if args.home:
        home = os.path.abspath(os.path.expanduser(args.home))
        if not os.path.isdir(home):
            return fail(EXIT_USAGE, f"--home is not a directory: {home}")
        if not os.path.isdir(os.path.join(home, ".jcode")):
            return fail(EXIT_USAGE, f"--home has no .jcode dir: {home}")
    else:
        source = os.path.abspath(os.path.expanduser(args.source_home or real_home()))
        config = os.path.join(source, ".jcode", "config.json")
        if not os.path.exists(config):
            return fail(EXIT_USAGE, f"no model credentials at {config}; pass --source-home or seed one")
        home = prepare_temp_home(source)
        temp_home = home
        # Always remove the throwaway HOME (also on failure/exit paths).
        if not args.keep_home:
            atexit.register(shutil.rmtree, temp_home, True)

    def log(msg: str) -> None:
        print(msg, file=sys.stderr, flush=True)

    log(f"[probe] bin={args.bin}")
    log(f"[probe] home={home}{' (temp)' if temp_home else ''}")
    log(f"[probe] cwd={cwd}")

    client = ACPClient(args.bin, cwd, child_env(home), log=log if args.verbose else (lambda _m: None))
    client.permission_mode = args.permission_mode
    started = time.time()
    result: dict = {
        "probeVersion": PROBE_VERSION,
        "ok": False,
        "bin": args.bin,
        "cwd": cwd,
        "home": home,
        "mode": args.mode or None,
        "prompt": prompt,
        "finalText": "",
        "thoughtText": "",
        "toolCalls": [],
        "permissionRequests": [],
        "counts": {},
        "stopReason": None,
        "sessionId": None,
        "elapsedMs": 0,
        "childExit": None,
        "stderrTail": [],
        "failures": [],
    }

    try:
        client.start()
        init = client.initialize(args.setup_timeout)
        result["initialize"] = init
        log(f"[probe] initialized: agent={((init.get('agentInfo') or {}).get('name'))!r} "
            f"protocol={init.get('protocolVersion')}")
        if init.get("authMethods"):
            result["failures"].append(
                f"agent advertised auth methods: {[m.get('id') for m in init['authMethods']]}"
            )

        new_sess = client.new_session(cwd, args.setup_timeout)
        session_id = str(new_sess.get("sessionId") or "")
        if not session_id:
            raise ACPError("session/new returned no sessionId")
        result["sessionId"] = session_id
        result["newSession"] = new_sess
        log(f"[probe] session={session_id}")

        if args.mode:
            try:
                client.set_mode(session_id, args.mode, args.setup_timeout)
                log(f"[probe] mode set to {args.mode}")
            except (ACPError, TimeoutError) as exc:
                # The Go harness treats set_mode failure as non-fatal too.
                result["setModeError"] = str(exc)
                log(f"[probe] WARNING set_mode failed (continuing): {exc}")

        log(f"[probe] prompting ({len(prompt)} chars, timeout {args.timeout:.0f}s) ...")
        prompt_resp = client.prompt(session_id, prompt, args.timeout)
        result["stopReason"] = prompt_resp.get("stopReason")
        log(f"[probe] turn done: stopReason={result['stopReason']}")

        if args.drain_ms > 0:
            time.sleep(args.drain_ms / 1000.0)
    except TimeoutError as exc:
        client.kill()
        result["failures"].append(f"timeout: {exc}")
        result["elapsedMs"] = int((time.time() - started) * 1000)
        result.update(_snapshot(client))
        result["childExit"] = client.close(grace=3)
        attach_persisted(result, home, cwd)
        _emit(result, args, log)
        return fail(EXIT_SETUP if not result["sessionId"] else EXIT_PROMPT, str(exc), client)
    except ACPError as exc:
        client.kill()
        result["failures"].append(str(exc))
        result["elapsedMs"] = int((time.time() - started) * 1000)
        result.update(_snapshot(client))
        result["childExit"] = client.close(grace=3)
        attach_persisted(result, home, cwd)
        _emit(result, args, log)
        code = EXIT_SETUP if not result.get("sessionId") else EXIT_PROMPT
        return fail(code, str(exc), client)
    except Exception as exc:  # pragma: no cover - defensive
        client.kill()
        traceback.print_exc()
        return fail(EXIT_PROMPT, f"unexpected error: {exc}", client)

    result.update(_snapshot(client))
    result["elapsedMs"] = int((time.time() - started) * 1000)
    result["childExit"] = client.close(grace=5)
    attach_persisted(result, home, cwd)

    failures = check_expectations(result, args)
    if not result["finalText"].strip():
        failures.append("agent produced no assistant text")
    result["failures"] = failures
    result["ok"] = not failures

    if args.events_out:
        with open(args.events_out, "w", encoding="utf-8") as fh:
            for ev in client.events:
                fh.write(json.dumps(ev, ensure_ascii=False) + "\n")

    _emit(result, args, log)
    return EXIT_OK if result["ok"] else EXIT_ASSERT


def attach_persisted(result: dict, home: str, cwd: str) -> None:
    session_id = str(result.get("sessionId") or "")
    if not session_id:
        result["persisted"] = {"file": "", "entries": 0, "badLines": 0, "tools": [], "turns": []}
        return
    try:
        result["persisted"] = load_persisted_session(home, session_id, cwd)
    except OSError as exc:  # never let a readback problem mask the real result
        result["persisted"] = {
            "file": "",
            "entries": 0,
            "badLines": 0,
            "tools": [],
            "turns": [],
            "error": str(exc),
        }


def _snapshot(client: ACPClient) -> dict:
    calls = client.tool_call_list()
    return {
        "finalText": client.agent_text,
        "thoughtText": client.thought_text,
        "toolCalls": calls,
        "permissionRequests": client.permission_requests,
        "counts": {
            "agentChunks": len(client.agent_text_parts),
            "thoughtChunks": len(client.thought_text_parts),
            "toolCalls": len(calls),
            "permissionRequests": len(client.permission_requests),
            "events": len(client.events),
            "unknownNotifications": client.unknown_notifications,
        },
        "stderrTail": client.stderr_chunks[-30:],
    }


def _emit(result: dict, args, log) -> None:
    if args.json_out:
        with open(args.json_out, "w", encoding="utf-8") as fh:
            json.dump(result, fh, indent=1, ensure_ascii=False)

    if not args.quiet:
        text = result.get("finalText") or ""
        trimmed = text if len(text) <= args.summary_chars else text[: args.summary_chars] + "… [trimmed]"
        log("--- assistant final text ---")
        log(trimmed if trimmed.strip() else "<empty>")
        calls = result.get("toolCalls") or []
        if calls:
            log(f"--- tool calls over ACP ({len(calls)}) ---")
            for call in calls:
                args_repr = json.dumps(call.get("rawInput"), ensure_ascii=False)
                if len(args_repr) > 240:
                    args_repr = args_repr[:240] + "…"
                log(f"  [{call.get('status') or '?'}] {call.get('title') or call.get('toolCallId')} {args_repr}")
        persisted = result.get("persisted") or {}
        tools = persisted.get("tools") or []
        if tools:
            log(f"--- persisted tool_call audit ({len(tools)}) from {persisted.get('file')} ---")
            for tool in tools:
                args_repr = str(tool.get("args") or "").replace("\n", " ")
                if len(args_repr) > 200:
                    args_repr = args_repr[:200] + "…"
                verdict = "ERROR" if tool.get("error") else ("DENIED" if tool.get("denied") else "ok")
                log(f"  {tool.get('name')}({args_repr}) -> {verdict}")
        attached = persisted.get("visibleTools") or []
        if attached:
            session_tools = sorted(n for n in attached if n.startswith("session_"))
            log(
                f"--- attached tools ({len(attached)}, {persisted.get('modelRequests')} model requests); "
                f"session_* = {session_tools or 'none'} ---"
            )
        if result.get("permissionRequests"):
            log(f"--- permission prompts: {len(result['permissionRequests'])} ---")
        if result.get("failures"):
            log("--- FAILURES ---")
            for item in result["failures"]:
                log(f"  * {item}")

    print("PROBE_JSON " + json.dumps(result, ensure_ascii=False, default=str))


def _sigint(_signum, _frame):
    raise KeyboardInterrupt


if __name__ == "__main__":
    signal.signal(signal.SIGINT, _sigint)
    try:
        sys.exit(main(sys.argv[1:]))
    except KeyboardInterrupt:  # pragma: no cover
        print("PROBE_ERROR: interrupted", file=sys.stderr)
        sys.exit(130)
