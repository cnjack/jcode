# Usage statistics

jcode records token usage across every surface (TUI, web, ACP) and exposes two
views in the web UI:

- **Global stats** — a "Usage" tab in Settings: tokens used, sessions, turns,
  active days, current streak, most-used model, an activity heatmap, a daily
  token trend, and per-model / per-project breakdowns.
- **Per-task context capacity** — a popover on the composer's token count: how
  the current context window is split across Messages / System tools / MCP tools
  / Skills / System prompt, plus the KV cache hit rate.

## Data model

### Token tracking (`internal/model`)

`model.TokenUsage` accumulates per-call usage. Each call is recorded via
`Add(AddParams{...})`, capturing:

| field         | source (go-openai `Usage`)                       |
|---------------|--------------------------------------------------|
| Prompt        | `PromptTokens`                                   |
| Completion    | `CompletionTokens`                               |
| Total         | `TotalTokens`                                    |
| Cached        | `PromptTokensDetails.CachedTokens` (cache-read)  |
| Reasoning     | `CompletionTokensDetails.ReasoningTokens`        |
| CacheWrite    | always 0 — see below                             |

All providers go through one go-openai client. go-openai's `Usage` exposes
**cache-read** (`cached_tokens`) and **reasoning** tokens, but **not**
`cache_creation_input_tokens`. So `CacheWriteTokens` is reserved for a future
native transport and stays 0 today.

### Cache hit rate

```
cache hit rate = Σ cached / Σ prompt        (clamped to [0,1])
```

i.e. the fraction of prompt tokens served from the provider's KV cache. This is
the only provider-portable definition given the wire constraint above.
`CacheObserved()` (any cached tokens seen) drives a "—" placeholder so 0% is not
confused with "this provider doesn't report caching".

### Event log (`internal/usage`)

Global stats are persisted to an **append-only JSON-lines log** at
`~/.jcode/usage/events.jsonl`, one line per agent turn:

```json
{"ts":1750531200,"date":"2026-06-21","project":"/path","session":"<uuid>","model":"glm-5.2","prompt":1500,"completion":300,"cached":1300,"reasoning":60,"total":1800,"calls":2}
```

Append-only `O_APPEND` writes are atomic for small records, so multiple jcode
processes (TUI + web + ACP) can record concurrently without a read-modify-write
race. All derived metrics (streak, active days, heatmap, per-model/project,
cache rate) are computed at read time by `usage.Aggregate`.

Token fields are per-turn **deltas**: the runner snapshots the cumulative
tracker at the start of a turn and records the difference at the end. Subagent
and teammate tokens are rolled into the same log under the **leader** session's
UUID so multi-agent work isn't undercounted.

The session **count** is sourced from the session index
(`session.ListAllSessions`), which is authoritative; the event log owns
token/day metrics.

## API

| endpoint                     | returns                                           |
|------------------------------|---------------------------------------------------|
| `GET /api/usage/stats?days=N`| global totals, streaks, heatmap (365d), trend (Nd), by-model, by-project |
| `GET /api/tasks/{id}/stats`  | per-task context breakdown (active) or token rollup (historical) |
| `GET /api/status`            | live token snapshot (extended with cache fields)  |

The `token_update` WebSocket event carries the same per-turn token fields +
cache hit rate to the browser.

## Per-task context breakdown

The five buckets are estimated at **~4 bytes/token** (`usage.Estimate`) — there
is no bundled tokenizer, and a relative breakdown only needs a consistent
heuristic (the UI labels it "estimated"):

1. **System prompt** = estimate(systemPrompt) − estimate(skill descriptions)
2. **System tools** = Σ estimate(tool JSON) over built-in tools
3. **MCP tools** = Σ estimate(tool JSON) over MCP tools
4. **Skills** = estimate(skill descriptions)
5. **Messages** = max(0, lastPromptTokens − buckets 1-4)

The four static buckets are computed on demand from the live agent assembly
(`command/web.go`'s `breakdownFn`), which reads the captured `systemPrompt` /
`mcpTools` / `currentCM` / `skillLoader` by reference — so project switches and
MCP reloads are reflected with no cache to invalidate. The breakdown is only
meaningful for the **active** task; historical tasks return token totals + the
aggregate hit rate only (`is_active:false`).

## Known limitations / future work

- **No `cache_creation` accounting** — blocked by the shared go-openai transport.
  A native Anthropic transport could populate `CacheWriteTokens`.
- **Cost is not yet derived** — `registry.go`'s `ModelCost`
  (Input/Output/CacheRead/CacheWrite) is not multiplied into the stats. A future
  pass could price each event for a spend view.
- **Per-turn delta across process restart** — a turn that resumes in a new
  process loses the in-memory start snapshot and may mis-count once.

## Testing

Per the sandbox constraints (live servers can't bind sockets), the backend is
covered by in-process `httptest` (`internal/web/usage_test.go`) and unit tests
for aggregation/streaks (`internal/usage/usage_test.go`) and the token struct
(`internal/model/token_usage_test.go`). The frontend is verified via
`vue-tsc` + `vite build`.
