# Cross-session view — E2E harness

Two small, dependency-free Python tools that exercise the cross-session view
feature (`session_list` / `session_search` / `session_read`) against a **real
model** through `jcode acp`, plus the notes needed to run them.

They implement the E2E half of the frozen spec:
[`internal-doc/cross-session-view-design.md`](../../internal-doc/cross-session-view-design.md) §6.3,
against the acceptance criteria in
[`internal-doc/cross-session-view-scope.md`](../../internal-doc/cross-session-view-scope.md) §6.

| file | role |
|---|---|
| `seed_sessions.py` | writes a throwaway `HOME` whose `~/.jcode/sessions/*` corpus is in the **real on-disk format** (this workspace, another workspace, an archived session, a corrupted one) |
| `session_view_acp_probe.py` | drives one headless `jcode acp` turn, captures the assistant text + the tool-call stream, and reads back the persisted transcript for the authoritative tool-call audit |
| `run_session_view_e2e.py` | case runner: fresh seed per case, runs the probe, records a verdict plus a fingerprint of the exact tree/binary tested |

All three are **read-only with respect to the real `~/.jcode`**: they only copy
`config.json` / `provider-auth.json` out of it.

---

## Prerequisites

- `python3` ≥ 3.9 (stdlib only — no venv, no pip).
- A working model configuration in `~/.jcode/config.json` (the repo default is
  used by the tests; `~/.jcode/provider-auth.json` is copied too when present,
  which account-type providers need).
- A freshly built binary (see below). Do not point these scripts at an
  installed `jcode` you do not control.

## 1. Build the binary

```bash
cd <repo>
go build -o /tmp/jcode-e2e-bin ./cmd/jcode
```

`make build` also works but additionally regenerates the model registry and the
web bundle; `go build ./cmd/jcode` is enough for the ACP transport.

> The task template uses `/tmp/jcode-e2e`; note that path may already be a
> directory in some environments, so the examples below use
> `/tmp/jcode-e2e-bin`. Set `JCODE_BIN` instead of repeating `--bin`:

```bash
export JCODE_BIN=/tmp/jcode-e2e-bin
```

## 2. Seed a HOME

```bash
./script/e2e/seed_sessions.py \
  --home /tmp/e2e/home \
  --ws   /tmp/e2e/ws \
  --force
```

Creates (or reuses) `/tmp/e2e/home` and `/tmp/e2e/ws`, copies the credentials,
writes four fixture sessions plus the `session.json` index, then re-reads what
it wrote and fails loudly if any structural invariant is broken.

### Fixtures

| kind | project | sentinel | purpose |
|---|---|---|---|
| `ws` | `--ws` | `zebra-unicorn-42` | happy path: same-project session the caller must be able to search + read (contains CJK text and a `tool_call`/`tool_result` pair) |
| `other` | `--other` (default `<home>/other-project`) | `quokka-99` | cross-project: must fail closed with the *same* error text as a missing session |
| `archived` | `--ws` | `narwhal-archived-7` | archived sessions are excluded unless explicitly requested |
| `corrupt` | `--ws` | `ocelot-corrupt-13` | bad line + blank line + torn tail (no trailing newline); a tolerant reader still sees the entry *after* the corruption |

Deterministic UUIDs (`1111…`, `2222…`, `3333…`, `4444…`) and fixed timestamps
make ordering and assertions reproducible. Other flags:

| flag | meaning |
|---|---|
| `--other DIR` | explicit directory for the cross-project fixture |
| `--source-home DIR` | where to read `config.json`/`provider-auth.json` from (default: the account's real home) |
| `--no-copy-creds` | seed sessions only (useful for pure parsing checks) |
| `--force` | required when an index already exists; re-seeding is idempotent and preserves sessions created by earlier probe runs |

Output is a single `SEED_JSON {...}` line on stdout (paths, UUIDs, sentinels)
plus a human summary on stderr. Exit `0` = seeded and self-verified, `1` =
verification problem, `2` = usage error.

### On-disk format this relies on

```
$HOME/.jcode/sessions/<uuid>.json      JSONL
  {"type":"session_start","uuid":...,"project":<abs>,"workspace_kind":"project",
   "provider":...,"model":...,"timestamp":<RFC3339>}
  {"type":"user","content":"...","timestamp":...}
  {"type":"assistant","content":"...","timestamp":...}
  {"type":"tool_call","name":...,"args":"<json string>","tool_call_id":...}
  {"type":"tool_result","name":...,"output":...,"tool_call_id":...}

$HOME/.jcode/sessions/session.json
  {"sessions": {"<project abs path>": [{"uuid","project","workspace_kind",
                "provider","model","start_time","title","updated_at","archived"?}]}}
```

Both halves are required: session resolution checks the index **and** the file,
so a fixture missing from the index is invisible to the feature.

## 3. Run the probe

Smoke test (no tools involved — proves the transport + model wiring):

```bash
./script/e2e/session_view_acp_probe.py \
  --bin "$JCODE_BIN" --home /tmp/e2e/home --cwd /tmp/e2e/ws \
  --timeout 180 \
  --prompt "Reply with exactly: PONG"
```

Output:

- **stdout**: exactly one `PROBE_JSON {...}` line (machine readable).
- **stderr**: `[probe]` progress, the trimmed assistant text, the ACP tool-call
  list, and the persisted `tool_call` audit (`name(args) -> ok/ERROR/DENIED`).

### Wire protocol

Newline-delimited JSON-RPC 2.0 over stdio to `<bin> acp`:

```
initialize       {protocolVersion:1, clientCapabilities:{fs:{readTextFile:false,
                  writeTextFile:false}, terminal:false}}
session/new      {cwd, mcpServers:[]}
session/set_mode {sessionId, modeId}            # optional, --mode
session/prompt   {sessionId, prompt:[{type:"text", text:"..."}]}
session/cancel   {sessionId}                    # notification, sent on timeout
```

Server → client: `session/update` notifications (`agent_message_chunk` carries
assistant text; `tool_call`/`tool_call_update` carry the tool stream) and
`session/request_permission` **requests**, which the probe answers with an
allow option (or a reject option with `--permission-mode deny`). Filesystem and
terminal client capabilities are advertised as `false`, so the agent must do all
I/O itself — that is precisely what makes "did it read `~/.jcode` with `read`/
`grep`/`execute` instead of `session_*`" observable.

### What the probe returns

| field | meaning |
|---|---|
| `finalText` | concatenated `agent_message_chunk` text — the answer |
| `thoughtText` | reasoning chunks, if the model streams them |
| `toolCalls` | ACP tool stream folded by `toolCallId` (`title`, `kind`, `status`, `rawInput`, `rawOutput`) |
| `persisted` | the caller's own JSONL transcript read back from `$HOME/.jcode/sessions/<uuid>.json` — one row per `tool_call` with **the real tool name**, args, and the matching `tool_result` output/error |
| `permissionRequests` | every permission prompt (must stay empty for read-only `session_*` tools) |
| `stopReason`, `elapsedMs`, `counts`, `childExit`, `stderrTail`, `failures` | diagnostics |

`persisted` is the authoritative audit for design §6.3: the ACP `title` of a
tool call is a human label (`Counts markdown files in current directory`) whose
first word is not the tool name for `execute`, while the transcript row is
`{"type":"tool_call","name":"execute",...}`.

It also exposes `persisted.visibleTools` — the tool-name list the recorder
journalled for the last model request (`tool_observation` with
`kind:"model_request"`). That makes "were the `session_*` tools attached?"
a deterministic fact: `--forbid-attached-tool session_search` is the real
off-switch assertion, not a guess based on what the model decided to call.

Note that the tools return *policy* failures as ordinary output (e.g.
`ERROR[SESSION_VIEW_NOT_FOUND]: …` with `error: ""`), so the summary line reads
`session_read({...}) -> ok`; use `--expect-tool-result-contains` to assert on
that text rather than expecting a non-`ok` verdict.

### Flags

| flag | default | meaning |
|---|---|---|
| `--bin` | `$JCODE_BIN` | binary to run |
| `--cwd` | current dir | `session/new` working directory = project/Local identity |
| `--home` | *(temp)* | HOME to run under; without it a throwaway HOME is created and credentials are copied in |
| `--source-home` | real account home | where credentials are copied from for a temp HOME |
| `--prompt` / `--prompt-file` | — | prompt text |
| `--mode` | *(unset)* | `approval` \| `plan` \| `auto` \| `full_access` via `session/set_mode`; failure is recorded, not fatal |
| `--timeout` | `600` | prompt-turn wall clock (reasoning models are slow; be generous) |
| `--setup-timeout` | `120` | `initialize` + `session/new` |
| `--permission-mode` | `allow` | how to answer `session/request_permission` |
| `--json-out PATH` | — | full result incl. all events, pretty-printed |
| `--events-out PATH` | — | raw notification stream as JSONL |
| `--keep-home` | off | keep an auto-created temp HOME (for debugging) |
| `--summary-chars` | `1200` | trim final text in the stderr summary |
| `--quiet` / `--verbose` | off | less / more stderr |
| `--expect-contains` / `--expect-not-contains` | — | repeatable, case-insensitive, on `finalText` |
| `--expect-tool` / `--forbid-tool` | — | repeatable; matched against ACP titles **and** persisted tool names |
| `--expect-tool-result-contains` / `--forbid-tool-result-contains` | — | repeatable; asserts some / no persisted `tool_result` output contains the string |
| `--expect-attached-tool` / `--forbid-attached-tool` | — | repeatable; asserts the tool was / was not in the schema list sent to the model (`persisted.visibleTools`, deterministic — no dependence on model behaviour) |
| `--expect-no-permission` | off | asserts zero permission prompts |

Exit codes: `0` ok · `2` usage/precondition · `3` initialize/session-new
failure or timeout · `4` prompt error / child died · `5` assertion mismatch.
On any failure the child's stderr tail is dumped.

## 4. Phase-2 case runner

```bash
./script/e2e/run_session_view_e2e.py \
  --bin "$JCODE_BIN" --out-dir /tmp/e2e-run --timeout 300
```

Every case gets a **fresh** `<out>/<case>/{home,ws}` seed, so no case can be
influenced by a session an earlier case created. Each case writes
`probe.json`, `events.jsonl` and the probe's stderr; the run writes
`summary.json` + `summary.md`.

| case | what it proves |
|---|---|
| `positive` | same-project search + read; sentinel reported; `session_*` attached; zero permission prompts |
| `negative` | other-project read fails with the same text as "not found"; `quokka-99` never appears in the answer |
| `disabled` | with `session_view.enabled=false` the three tools are **not attached** (deterministic) and are never called |
| `archived_default` | a default search does not surface the archived fixture |
| `archived_included` | the same search with `include_archived:true` does |
| `corrupt` | the fixture with garbage/blank/torn lines is still searchable |

Flags: `--only positive,negative` to run a subset, `--mode` (default
`full_access`), `--timeout`, `--dry-run` (print commands, seed nothing),
`--out-dir` (existing case dirs are replaced). Exit `0` all pass, `1` a case
failed, `2` usage.

The summary records the tested state — branch, `git rev-parse HEAD`, sha256 of
`git status --porcelain`, sha256 of `git diff`, changed paths, and the binary's
sha256/mtime — so the report can be tied to the PR commit.

Individual cases can still be run by hand:

```bash
# positive
./script/e2e/session_view_acp_probe.py --bin "$JCODE_BIN" \
  --home /tmp/e2e/home --cwd /tmp/e2e/ws --mode full_access --timeout 300 \
  --prompt "Use only session_search and session_read. Do not use execute, grep, glob or read on ~/.jcode. Search prior sessions for zebra-unicorn-42, then read around the hit and quote the full line." \
  --expect-contains zebra-unicorn-42 --expect-tool session_search --expect-tool session_read

# negative
./script/e2e/session_view_acp_probe.py --bin "$JCODE_BIN" \
  --home /tmp/e2e/home --cwd /tmp/e2e/ws --mode full_access --timeout 300 \
  --prompt "Use only the session_read tool. Do not use execute, grep, glob or read on ~/.jcode. Read session_id=22222222-2222-4222-8222-222222222222 and tell me what it contains." \
  --expect-tool session_read \
  --expect-tool-result-contains 'ERROR[SESSION_VIEW_NOT_FOUND]' \
  --expect-not-contains quokka-99
```

Judgement rules from the spec: if the model bypasses the tools with
`grep`/`execute`, that is a **product finding** (risk B1), not a test failure —
but the tool-call audit must still pass. The probe therefore reports both
signals and only fails on the assertions you pass it.

## 5. Notes / gotchas

- **Isolation.** The child only ever sees `HOME=<temp>`. Run the probe from a
  shell whose real `~/.jcode` you are willing to have *read* (credentials only);
  never seed into the real home.
- **`execute` is partly pre-approved.** Safe prefixes (`ls`, `cat`, …) run
  without a permission prompt in `approval` mode, so a negative case must
  *instruct* the model not to use it (as the spec does) and audit the calls.
- **`tool_search`.** With `tool_search` enabled in config the model may first
  call `tool_search` to discover tools; that call is visible in the audit and is
  not a failure.
- **Reasoning models.** The default model reasons before answering; keep
  `--timeout` ≥ 300s for tool-using prompts.
- **Temp HOME cleanup.** Without `--home` the probe deletes its temp HOME on
  every exit path (success, failure, timeout); pass `--keep-home` when you need
  to inspect `debug.log`.

## 6. Phase-1 validation log (harness only — no verdict)

**A. Transport validated against a clean `HEAD` build** (taken while the
implementers' worktree did not compile, i.e. *without* the feature):

```bash
git archive HEAD | tar -x -C /tmp/jcode-head-src
go build -o /tmp/jcode-e2e-bin ./cmd/jcode          # in that extract
./script/e2e/seed_sessions.py --home /tmp/e2e-p1/home --ws /tmp/e2e-p1/ws --force
./script/e2e/session_view_acp_probe.py --bin /tmp/jcode-e2e-bin \
  --home /tmp/e2e-p1/home --cwd /tmp/e2e-p1/ws --timeout 180 \
  --prompt "Reply with exactly: PONG"
```

Observed: `stopReason=end_turn`, `finalText="PONG"`, 2 `agent_message_chunk`s,
0 tool calls, 0 permission prompts, exit `0`, ~5s wall clock. Fixtures were
parsed by the real `session` package (`ListSessions` + `LoadSession`): 7 / 3 / 3
/ 3 entries, the corrupted one dropping exactly the garbage, blank and torn
lines. Re-running the seeder produced byte-identical files.

**B. Harness validated against the feature build** (same commands, binary
rebuilt from the working tree once it compiled — smoke only, the verdict is
phase 2):

| run | outcome |
|---|---|
| `PONG` prompt | exit 0, `finalText="PONG"` |
| positive prompt (search `zebra-unicorn-42`, then `session_read`) | exit 0; audit: `session_search({"query":"zebra-unicorn-42"})`, `session_read({"session_id":"1111…","start":6})`; answer quoted the CJK entry containing the sentinel |
| negative prompt (read the `other` project's session) | exit 0 with `--expect-tool session_read --expect-tool-result-contains 'ERROR[SESSION_VIEW_NOT_FOUND]' --expect-not-contains quokka-99` |
| tool-stream check (`execute`/`ls`) | ACP title was `Counts markdown files…` while the persisted row correctly said `execute` — confirms why the audit reads the transcript |
| stub binary that hangs (`sleep 30`) | exit 3, `initialize: no response within 2s`, child stderr dumped |
| stub binary that exits immediately | exit 3, `agent closed the connection (exit=7)`, child stderr dumped |
| `--expect-attached-tool` × 3 / `--forbid-attached-tool` | exit 0 / exit 5 respectively, against a feature build whose transcript showed `session_list, session_read, session_search` in `visible_names` |
| `run_session_view_e2e.py --dry-run` | prints all six case commands and writes `summary.md` with branch/rev/status-sha/diff-sha/binary-sha fingerprints |

