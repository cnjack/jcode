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

The agent can also only *propose* automations (`automation_create`): it cannot
enumerate or cancel them, so "what automations do I have?" / "delete the weekly
one" have no agent path (UI/REST only).

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
| `automation_create` (extended) | prompt | cadence gains `cron` + `once`; new `cron_expr` / `at` params. Still proposes DISABLED (`source="agent"`) — the human-in-the-loop gate is unchanged. Description gains herd-avoidance guidance (avoid `:00`/`:30` when approximate). |
| `automation_list` (new) | auto-approve | read-only; one record per automation (id, name, schedule, enabled, next/last run, mode, project, prompt preview). Added to plan-mode lists. |
| `automation_delete` (new) | prompt | deletes by id; not-found is a model-correctable error. **Excluded from automation (unattended) runs** via `interactiveToolNames` — an unattended run must never delete automations, same reasoning as `ask_user`. |

## Web UI

- `lib/automation.ts` types gain `once` / `cron` / `expr` / `at`.
- Editor form gains Once (datetime-local input, converted to local-offset
  RFC3339) and Cron (expression text input, server-validated) trigger kinds;
  editing an existing once/cron automation round-trips without corruption.
- Cadence chip + `next_run_at` display handle the new kinds (an expired once
  falls back to last-run display).
- i18n: `cadence.once` / `cadence.cron` / `editor.onceAt` / `editor.cronHint`
  in all five locales.

## Non-goals

- In-session prompt re-injection (kimi-style session reminders) — a different
  transport problem; would go through the background/steer path, not the
  automation store.
- Second-interval crons, timezone-per-automation, `@macros`, named days/months.
- Agent-side enable/disable — deliberately out of scope: arming stays a
  human-only action (PRD human-in-the-loop gate).

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
  executes → auto-disarmed, run recorded via the API.
