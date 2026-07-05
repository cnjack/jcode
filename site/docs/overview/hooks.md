---
title: Hooks
parent: Overview
nav_order: 14
---

# Hooks

Hooks let you run your own commands at key points in the agent loop — before a
tool runs, after it finishes, when you submit a prompt, or when the agent is
about to stop. Unlike an instruction in a prompt (which the model *may* follow),
a hook runs **deterministically** every time its event fires. Use them to block
dangerous commands, auto-format files, gate on tests, redact secrets, or get
pinged when a long task finishes.

{: .note }
> Hooks are plain programs. jcode passes each one a small JSON object on stdin
> and reads its exit code (and optional JSON on stdout). Any language works —
> shell, Python, Node, a compiled binary.

## Quick start

Auto-format Go files the moment the agent writes or edits one. Create
`~/.jcode/hooks.json`:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "write|edit",
        "hooks": [
          { "type": "command", "command": "f=$(jq -r .tool_input.file_path); case \"$f\" in *.go) gofmt -w \"$f\";; esac" }
        ]
      }
    ]
  }
}
```

That's it — start jcode and every file the agent writes stays gofmt-clean. No
restart needed for later edits to `hooks.json`; the config is re-read each turn.

## Events

Six events fire during a run. Three of them are **blockable** — the hook can
stop the action.

| Event | Fires | Blockable | Typical use |
|---|---|---|---|
| `SessionStart` | when a session begins | no | inject project conventions |
| `UserPromptSubmit` | after you send a message, before the model sees it | **yes** | rewrite/augment or reject prompts |
| `PreToolUse` | before a tool runs | **yes** | block, rewrite args, or auto-approve |
| `PostToolUse` | after a tool succeeds | no | format, lint, redact, log |
| `PostToolUseFailure` | after a tool returns an error | no | log failures, notify, add retry hints |
| `Stop` | when the agent is about to finish | **yes** | quality gate, "ping me when done" |

## Where hooks live

Hooks are configured in a `hooks.json` file:

- **`~/.jcode/hooks.json`** — your personal hooks, loaded on every project. This
  is the recommended place.
- **`.jcode/hooks.json`** and **`.jcode/hooks.local.json`** — project hooks,
  checked into (or ignored by) the repo.

{: .warning }
> **Project hooks are disabled by default.** A hook runs arbitrary commands the
> instant its event fires — a `SessionStart` hook in a cloned repo would execute
> the moment you open jcode there. So jcode only loads `~/.jcode/hooks.json`
> unless you explicitly opt in per shell with
> `export JCODE_HOOKS_TRUST_PROJECT=1`. Only enable it for repositories you
> trust. Put your own hooks in `~/.jcode/hooks.json` and this never bites you.

When multiple layers are enabled, all of their matching hooks run (they add up,
they don't override each other).

## Configuration format

```json
{
  "hooks": {
    "<EventName>": [
      {
        "matcher": "<tool matcher — optional>",
        "hooks": [
          { "type": "command", "command": "<shell command>", "timeout": 30 }
        ]
      }
    ]
  }
}
```

- `matcher` scopes the group to certain tools (see [Matchers](#matchers)). Omit
  it (or use `"*"`) to match every tool. It's ignored by events that have no
  tool (`SessionStart`, `UserPromptSubmit`, `Stop`).
- `command` runs via `sh -c`, so pipes and `&&` work.
- `timeout` is in **seconds** (default 60). A hook that times out is treated as
  "allow" and never blocks the agent.

## The hook contract

**Input (stdin).** Every hook receives a JSON object. Fields depend on the event:

```json
{
  "hook_event_name": "PreToolUse",
  "session_id": "…",
  "cwd": "/path/to/project",
  "tool_name": "execute",
  "tool_input": { "command": "rm -rf build" },
  "tool_response": "…",
  "prompt": "…",
  "stop_hook_active": false
}
```

`tool_name`/`tool_input` are present for tool events, `tool_response` for
`PostToolUse*`, `prompt` for `UserPromptSubmit`, `stop_hook_active` for `Stop`.
The same values are also exported as environment variables —
`JCODE_TOOL_NAME`, `JCODE_CWD`, `JCODE_SESSION_ID`, `JCODE_HOOK_EVENT` — so a
simple hook can skip parsing stdin.

**Output (exit code).**

| Exit code | Meaning |
|---|---|
| `0` | allow — and parse stdout JSON if present |
| `2` | **block** (blockable events only); stderr is fed back to the agent as the reason |
| other | non-blocking error; stderr is logged, the action proceeds |

**Output (stdout JSON, on exit 0).** For fine-grained control, print a JSON
object:

```json
{
  "hookSpecificOutput": {
    "permissionDecision": "allow",
    "permissionDecisionReason": "read-only tool",
    "updatedInput": { "file_path": "safe.txt" },
    "modifiedResult": "…scrubbed output…",
    "additionalContext": "extra context for the model"
  }
}
```

- `permissionDecision` (PreToolUse): `allow` skips the approval prompt, `deny`
  blocks, `ask` falls through to normal approval.
- `updatedInput` (PreToolUse): replaces the tool's arguments before it runs.
- `modifiedResult` (PostToolUse): replaces what the model sees as the result.
- `additionalContext`: extra text handed to the model.

For `Stop`, a top-level `{ "continue": false, "reason": "…" }` forces the agent
to keep working instead of stopping.

## Matchers

A matcher is **exact by default**, split on `|`:

- `"write"` matches only the `write` tool — not `todowrite`.
- `"write|edit"` matches `write` or `edit`.
- A part containing regex characters is a regex: `"mcp__.*"` matches every MCP
  tool, `"^execute$"` is an anchored exact match.

jcode's built-in tools are `read`, `write`, `edit`, `execute` (shell), `grep`,
`glob`. For convenience the Claude Code names — `Read`, `Write`, `Edit`, `Bash`,
`Grep`, `Glob` — match the same tools, so configs written for Claude Code work
unchanged.

## Use cases

The examples below use inline commands for brevity. For anything longer, point
`command` at a script (e.g. `~/.jcode/hooks/guard.sh`) and keep the logic there.

### Block dangerous commands

Stop the agent from running destructive shell commands, no matter how it phrases
them:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "execute",
        "hooks": [
          { "type": "command", "command": "jq -r .tool_input.command | grep -Eq 'rm -rf|git push .*--force|DROP TABLE' && { echo 'blocked by policy' >&2; exit 2; } || exit 0" }
        ]
      }
    ]
  }
}
```

### Format on save

Run a formatter after every write/edit (see [Quick start](#quick-start)). Swap
`gofmt` for `prettier --write "$f"`, `ruff format "$f"`, etc.

### Test gate before finishing

Don't let the agent stop until the build and tests pass:

```json
{
  "hooks": {
    "Stop": [
      { "hooks": [ { "type": "command", "command": "~/.jcode/hooks/test-gate.sh", "timeout": 300 } ] }
    ]
  }
}
```

```bash
#!/usr/bin/env bash
# ~/.jcode/hooks/test-gate.sh
input=$(cat)
# Prevent an infinite loop: if we already forced a continue once, let it stop.
[ "$(echo "$input" | jq -r .stop_hook_active)" = "true" ] && exit 0
if ! go build ./... 2>&1; then
  echo "build is broken — fix it before finishing" >&2
  exit 2
fi
if ! go test ./... 2>&1; then
  echo "tests are failing — make them pass before finishing" >&2
  exit 2
fi
exit 0
```

{: .important }
> A `Stop` hook that blocks makes the agent keep going, which fires `Stop` again.
> Always check `stop_hook_active` and `exit 0` when it's `true`, or you'll loop.

### Auto-approve read-only tools

Skip the approval prompt for tools that can't change anything:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "read|grep|glob",
        "hooks": [
          { "type": "command", "command": "echo '{\"hookSpecificOutput\":{\"permissionDecision\":\"allow\"}}'" }
        ]
      }
    ]
  }
}
```

### Redact secrets from tool output

Scrub anything that looks like a key before the model ever sees it:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "read|execute",
        "hooks": [
          { "type": "command", "command": "jq -r .tool_response | sed -E 's/(sk-[A-Za-z0-9]{16,}|ghp_[A-Za-z0-9]{20,})/[REDACTED]/g' | jq -Rs '{hookSpecificOutput:{modifiedResult:.}}'" }
        ]
      }
    ]
  }
}
```

### Inject project conventions every session

Teach the agent your project's rules without repeating them in every prompt:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          { "type": "command", "command": "echo '{\"hookSpecificOutput\":{\"additionalContext\":\"Use pnpm, never npm. Target Go 1.26. Run gofmt before committing.\"}}'" }
        ]
      }
    ]
  }
}
```

`UserPromptSubmit` works the same way if you'd rather attach context (like the
current git branch) to every message.

### Ping me when the agent finishes

Get a desktop notification when a long run wraps up (macOS):

```json
{
  "hooks": {
    "Stop": [
      { "hooks": [ { "type": "command", "command": "osascript -e 'display notification \"jcode is done\" with title \"jcode\"'" } ] }
    ]
  }
}
```

### Audit every tool call

Append a log of everything the agent does:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "hooks": [
          { "type": "command", "command": "jq -c '{t: now, tool: .tool_name, input: .tool_input}' >> \"$JCODE_CWD/.jcode-audit.log\"" }
        ]
      }
    ]
  }
}
```

## Safety & gotchas

- **Fail-safe by design.** A hook that times out, crashes, or returns an
  unexpected exit code never blocks the agent — it's treated as "allow" and
  logged. Only a clean `exit 2` blocks.
- **`Stop` loops.** Always honor `stop_hook_active` (see the test-gate example).
- **Order & precedence.** For one event, all matching hooks run; if any of them
  denies, the action is blocked. `PreToolUse` runs before approval, so a `deny`
  never even reaches the prompt.
- **Keep them fast.** Hooks run inline on the tool path. Heavy work belongs in a
  background job the hook kicks off, not in the hook itself.
