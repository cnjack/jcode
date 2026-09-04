# Automations v2: once + cron triggers, agent lifecycle tools

Design note for extending Automations with two new trigger shapes and closing
the agent-side lifecycle gap. Companion to `internal-doc/automations-prd.md`.

## Motivation

The v1 trigger model (`hourly | daily | weekly | manual`) cannot express the
most common natural-language asks:

- **One-shot** — "in 10 minutes run the smoke test", "remind me at 15:00 today".
  `manual` never auto-fires; the recurring cadences cannot fire once.
- **Cron schedules** — "weekdays at 9" (`0 9 * * 1-5`) needs ONE weekday per
  automation in v1 (three automations for Mon/Wed/Fri); `*/15 * * * *` is
  inexpressible (hourly fires once per hour).

The agent could create automations (`automation_create`) but could not enumerate
or cancel them, so "what automations do I have?" / "delete the weekly one" had
no agent path (UI/REST only).

## New trigger shapes

```go
TriggerType "once"   // fires exactly once at Trigger.At (RFC3339), then auto-disarms
Cadence     "cron"   // under TriggerSchedule; Trigger.Expr is a 5-field cron expression

type Trigger struct {
    // existing fields…
    Expr string  // 5-field cron expression (Type==schedule && Cadence==cron)
    At   string  // RFC3339 pinned time (Type==once)
}
```

### once semantics

- `ComputeNextRun` returns `At` while strictly future, `ok=false` once past.
- **Create-time** only: `Store.Create` rejects an `At` already before the
  current wall-clock minute (minute granularity gives the UI/form slack).
  `ValidateAutomation` only checks well-formedness so an *expired* once
  automation stays editable via `Update` (re-checking past-ness there would
  brick edits of old records).
- **Auto-disarm**: `ExecuteRun` disables the definition (Enabled=false) right
  after a *scheduled* fire is claimed. The run itself still executes; failures
  are recorded normally. Manual "Run Now" never disarms — a preview run must
  not consume the trigger. Disarm-on-claim (not on completion) keeps the
  state machine trivial: no window where a re-enable races the completion
  callback, and `TryMarkRunning` refusal (cross-process manual run in flight)
  leaves the trigger armed.

### cron semantics

Self-contained parser (`cronexpr.go`), no new dependency. Lock-step with
standard Vixie/POSIX behaviour, mirroring kimi-code's implementation:

- 5 fields `M H DoM Mon DoW`, evaluated in the host's local timezone.
- Per field: `*`, integers, ranges `a-b`, lists `a,b,c`, steps `*/n`, `a-b/n`.
- Day-of-week accepts 0–7 with 7 folded to 0 (Sunday).
- When BOTH dom and dow are restricted they combine with cron's OR rule; bare
  `*` is "unrestricted" while `*/n` is a restriction.
- Digit-only field values (no `MON`/`JAN` names, no `@daily` macros — v2 keeps
  the surface small; structured cadences remain the friendly path).
- `NextCronFire` scans day-by-day using `time.Date` construction (DST-safe:
  spring-forward normalizes, fall-back duplicates are deduped by the existing
  `SlotKey`/`LastFiredSlot` guard), bounded at 5 years. Expressions with no
  fire in the window (`0 0 31 2 *`) are rejected at validation.

## Conversation context

Automations have an explicit context policy:

- `isolated` (the backward-compatible default for old records and manual API/UI
  creation) starts a fresh headless run session for every fire.
- `conversation` stores `owner_session_id`. Agent-created automations bind to
  the current top-level session and inject an `<automation-fire>` user turn into
  that conversation when they fire. Cold conversations are restored from their
  persisted, possibly compacted history before the turn starts.

Conversation runs use a one-turn full-access agent with unattended-only tools;
the conversation's visible mode and interactive agent are not changed. A busy
conversation is serialized: the automation waits for the active turn rather
than writing history concurrently.

No-project sessions keep their scratch identity across both policies. A
conversation-bound fire stays in the owner session; a legacy/isolated run built
under a managed scratch path is recorded as `workspace_kind=scratch` and remains
visible only in Automations > Recent runs, never as a synthetic project in the
conversation sidebar. Legacy automation-run records that were persisted as
`project` are normalized to scratch when restored.

Deleting a conversation with related automations requires an explicit policy:

- `delete` removes the related definitions and run state before deleting the
  conversation;
- `detach` keeps each definition and its enabled state, clears
  `owner_session_id`, and changes it to `isolated` so no dangling context
  reference remains.

The delete API returns `409 conversation_has_automations` when no policy is
supplied. The Web UI presents both choices plus Cancel and lists every related
definition, including paused or already-fired one-shots.

## Scheduler integration

The tick loop is untouched structurally — it already advances `NextRunAt` via
`ComputeNextRun`. Two adjustments:

1. **Seeding guard**: when `NextRunAt` is missing and `ComputeNextRun` returns
   `ok=false` (an expired once, or a corrupt never-firing trigger), skip
   WITHOUT writing state — otherwise the seeding branch would rewrite
   `NextRunAt=""` every 30s tick forever.
2. **Auto-disarm** (see above) lives in `ExecuteRun`, shared by scheduler and
   any future scheduled-path caller.

## Agent tools

| Tool | Approval | Notes |
|---|---|---|
| `automation_create` (extended) | prompt | Directly exposed in normal mode. Cadence gains `cron` + `once`; new `cron_expr` / `at` params. Creates ENABLED (`source="agent"`) and binds to the current conversation when a session id is available. Its routing prompt reserves delayed/future/recurring work for automations and forbids shell sleep/background substitutes. Description retains herd-avoidance guidance (avoid `:00`/`:30` when approximate). |
| `automation_list` (new) | auto-approve | read-only; one record per automation (id, name, schedule, enabled, next/last run, mode, project, prompt preview). Added to plan-mode lists. |
| `automation_delete` (new) | prompt | deletes by id; not-found is a model-correctable error. **Excluded from automation (unattended) runs** via `interactiveToolNames` — an unattended run must never delete automations, same reasoning as `ask_user`. |

## Web UI

- `lib/automation.ts` types gain `once` / `cron` / `expr` / `at`.
- Editor form gains Once (datetime-local input, converted to local-offset
  RFC3339) and Cron (expression text input, server-validated) trigger kinds;
  editing an existing once/cron automation round-trips without corruption.
- Cadence chip + `next_run_at` display handle the new kinds (an expired once
  falls back to last-run display).
- The project field is a project picker for isolated definitions. Conversation-
  bound definitions are locked to their owner; managed scratch owners display
  only “No project” (never the internal `~/.jcode/workspace/...` path). The API
  also rejects moving a conversation-bound or no-project definition, so the UI
  lock is not the enforcement boundary.
- Conversation-bound definitions display their owner title on the card and in
  the editor. The owner can be switched to another persisted conversation; the
  server derives `project_path` from that session atomically with the owner
  change, and rejects switches while the automation itself is running. The UI
  can open the selected conversation directly.
- Editor choices use the app's token-driven select-only listbox instead of
  native `<select>` popovers, which are rendered by macOS/Tauri outside the app
  theme. The listbox supports keyboard navigation, typeahead, Escape, focus
  restoration, and opening upward near a viewport edge.
- i18n: `cadence.once` / `cadence.cron` / `editor.onceAt` / `editor.cronHint`
  in all five locales.

## Non-goals

- Mid-turn steering into an already-running conversation. Conversation-bound
  automations keep the trigger pending until the owner is idle, then inject the
  next turn into that session.
- Second-interval crons, timezone-per-automation, `@macros`, named days/months.
- Agent-side toggling of existing definitions remains out of scope. New
  automations created through `automation_create` are enabled by default; the
  host's normal write-approval policy gates their creation.

## Test plan

- `cronexpr_test.go`: parse acceptance/rejection matrix, dow fold, dom/dow OR
  rule, step/range/list expansion, next-fire boundaries, leap day, 5-year
  no-fire, DST transitions.
- `schedule_test.go` / `validate_test.go` / store tests: once past/future,
  cron wiring, create-vs-update validation split.
- `scheduler_test.go`: once fires → auto-disarmed; manual run doesn't disarm;
  expired once never fires and writes no state; cron advance math.
- `internal/tools`: create once/cron (incl. rejections), list, delete paths.
- `internal/web`: HTTP create once/cron incl. 400s; **integration e2e** —
  POST /api/automations → real Store → real Scheduler tick → fake Runner
  executes → auto-disarmed, run recorded via the API; conversation-bound runs
  retain owner history; session deletion requires delete/detach and rejects
  in-flight related automations.
