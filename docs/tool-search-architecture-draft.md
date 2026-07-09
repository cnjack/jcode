# Tool Search / DynamicTools 集成架构（Draft）

> 状态：**DRAFT** ｜ 产出方式：5 席位圆桌对抗 → 交叉质询 → 评审 → 红队 → 定稿（17 个 agent，逐条核实 eino v0.9.9 + jcode 源码）。
> 适用版本：`cloudwego/eino v0.9.9`。落地前请以本文「Key files touched」与「Implementation checklist」为准。

---

# ARCHITECTURE: eino tool_search / DynamicTools Integration for jcode (FINAL)

## 1. Decision Summary

| Decision | Resolved stance | Rationale & provenance |
|---|---|---|
| **Client vs model-native** | Ship **client mode** as the only *functional* mode in PR1. Keep a **model-native code path present but dormant** behind a capability predicate that returns **false everywhere today**. | `chatModel.buildRequest` (internal/model/chatmodel.go) wraps every provider in go-openai and reads only `GetCommonOptions().Tools` — it **discards** `state.DeferredToolInfos` and `runCtx.ToolSearchTool`. eino strips dynamic tools from `ToolInfos` in native mode (toolsearch.go:286-288, verified), so enabling native today makes MCP tools **silently unreachable**. Native is the cache-friendly endgame but is not wireable until the adapter forwards deferred tools. |
| **Per-provider capability gate** | A **transport-capability predicate** keyed on the adapter's ability to serialize deferred tools — **NOT** a provider-name allowlist. Returns false for all providers now. | The real gate is "does the adapter forward deferred tools," not "is the provider Anthropic." The predicate is `false` until the adapter is extended, so the allowlist question is moot in PR1. |
| **Default on/off** | **Off-equivalent by default** via mode enum defaulting to `auto`, where **`auto` resolves to all-static today** (byte-identical to current behavior). | Protects jcode's `CacheEnabled`-by-default posture and gives zero-surprise upgrades. `auto` never silently lands client mode. |
| **Threshold** | **Threshold-gated, default 20** dynamic MCP tools. Below threshold → static, no middleware. **Threshold compares against `len(dynamic)` AFTER `AlwaysLoadServers` subtraction** (resolved below). | Without a threshold the feature either never engages or churns cache to hide a handful of tools. Below `len==0`, `toolsearch.New` errors (toolsearch.go:59, verified). |
| **Static-vs-dynamic split rule** | **Builtins/non-`mcp__*` always static; only `mcp__*` tools are dynamic.** Plus an **optional per-server "always-load" override** stored as a config-level name list in `ToolSearchConfig`. | The agent must never lose its file/exec loop to a missed `tool_search`. Per-server override kept in `ToolSearchConfig` (not on `MCPServer`) so flipping the master mode off restores everything without touching per-server state. |
| **Where the split is computed** | At **agent-build time**, on a **single snapshot** of the surface's current MCP tool list, in a **new pure helper**. The existing tool-list builder signatures stay unchanged (a second consumer depends on each). Recomputed on every per-task / model-switch / mode-switch rebuild. | Agents are already rebuilt per task. `buildAllTools` (web.go:326) has a second consumer in `breakdownFn` (web.go:498) — **must not change its signature**. Split via a separate helper on the flat list. |
| **Reduction interaction** | Add `"tool_search"` to reduction's **top-level `ClearExcludeTools`** (verified present, reduction.go:118) at **all three** call sites (web.go:416, acp.go:448, interactive.go:208), **only when client mode is active**. | `reduction.Backend != nil` in jcode → the default `ClearHandler` is active → tool_search **results** (the forward-selection signal) would be cleared. `ClearExcludeTools` protects **both tool-uses and results** by name and is mode-gated by a one-line list append — strictly cleaner than the per-tool `ToolReductionConfig` map for this purpose. Both levers verified to exist in v0.9.9; we choose `ClearExcludeTools` for tool_search and leave the existing `"read": {SkipClear:true}` idiom untouched. |
| **Middleware placement** | Append into the existing `handlers` slice as `handlers[0]` (outermost handler). Approval stays innermost automatically (agent.go:86). No `WithToolSearch` AgentOption. | toolsearch must rewrite `ToolInfos` before each model call; placing it among `handlers` keeps it **outside** approval (correct — approval intercepts the resulting tool *call*) and **inside** budget/compaction/recovery (which operate on history, so ordering is immaterial). The real protection is the order-independent clear-exclusion, not placement. |
| **Three-surface scope (CORRECTED)** | web.go, acp.go, and interactive.go have **three different agent-build architectures**. PR1 ships a **single shared helper package** (`internal/agent`) that each surface calls with **its own** snapshot/cm/provider/model, plus **three thin call-site integrations**. There is **no** "split inside one `makeAgent` against one `mcpToolsPtr`" — that is web.go-shaped only. | VERIFIED: acp.go loads MCP once synchronously into flat `allTools` (acp.go:349-363); its builder is `makeAgent(sysPrompt string, toolList []tool.BaseTool)` (acp.go:479) taking a pre-built list and no cm/provider. interactive.go has a `buildAllTools()` **method** on `interactiveState` (interactive.go:82), MCP in `s.mcpTools` (interactive.go:67) refreshed by `reloadMCP` (interactive.go:275). The shared mechanism is the **pure helper + config**, applied per surface. |
| **Config-mutation lock (CORRECTED)** | New endpoints mutate `s.cfg.ToolSearch` under **`s.cfgMu`** (NOT `s.mu`). | VERIFIED: server.go:76 `cfgMu` serializes config RMW+Save. Config-**field** writes (DefaultMode at server.go:2506, DisabledSkills at server.go:2740 with an explicit comment that `s.mu` is insufficient) use `cfgMu`. MCP-server-**map** writes use `s.mu`, but the toolsearch field write follows the DefaultMode/DisabledSkills precedent. Both new endpoints touch the same `s.cfg.ToolSearch` pointer, so **both use `cfgMu`** — never split locks across them. |
| **GET status data source (CORRECTED)** | Counts come from a **`BreakdownFn`-style closure** added to `EngineConfig`, computed inside each surface's task-build scope where the live tool list is reachable — NOT "live in the GET handler" (the handler runs on `web.Server`, which has no handle to web.go's local `mcpToolsPtr`). | VERIFIED: `mcpToolsPtr` is a local `atomic.Pointer` in `buildWebTask`'s closure (web.go:142), not a `web.Server` field. The existing `breakdownFn` (web.go:480) is the exact, proven template: a closure over `mcpToolsPtr`+`currentCM` published via `EngineConfig.BreakdownFn`. We add a parallel `ToolSearchStatusFn`. |

---

## 2. Agent Changes

### 2.1 Shared helper package (single source of truth for all three surfaces)

All split/build logic lives in **`internal/agent/toolsearch.go` (new)** as pure functions. Each surface calls them with its own inputs. Nothing in this package imports `config` (resolver lives in callers) and nothing imports `web`.

```go
// internal/agent/toolsearch.go (new)
package agent

const ToolSearchToolName = "tool_search" // pin eino's meta-tool name (verified toolsearch.go:412)

// mcpServerOf recovers the server key using EINO'S OWN separator convention.
// eino splits on "__" (toolsearch.go:603 splitToolName: strings.Split(name,"__")).
// We must key on the SAME first segment eino keys on, so AlwaysLoad matches
// reliably even when a server name itself contains "__".
func mcpServerOf(name string) (string, bool) {
	if !strings.HasPrefix(name, "mcp__") {
		return "", false
	}
	// segments[0]=="mcp", segments[1]==<server-first-segment>, segments[2:]==tool
	segs := strings.Split(name, "__")
	if len(segs) < 3 {
		return "", false // malformed; treat as static (safe)
	}
	return segs[1], true
}

// SplitForToolSearch partitions an already-built flat tool list.
// alwaysLoad: server keys (mcp__<key>__*) to keep static.
func SplitForToolSearch(ctx context.Context, all []tool.BaseTool, alwaysLoad map[string]bool) (static, dynamic []tool.BaseTool) {
	for _, t := range all {
		name := t.Info(ctx).Name
		if srv, ok := mcpServerOf(name); ok && !alwaysLoad[srv] {
			dynamic = append(dynamic, t)
		} else {
			static = append(static, t) // builtins + always-load MCP
		}
	}
	return
}

// BuildToolSearch returns the middleware. clientMode -> local meta-tool;
// !clientMode (native) -> server-side, no reduction protection needed.
// Returns (nil,false,err) if dynamic is empty (eino errors on len==0).
func BuildToolSearch(ctx context.Context, dynamic []tool.BaseTool, clientMode bool) (adk.ChatModelAgentMiddleware, bool, error) {
	if len(dynamic) == 0 {
		return nil, false, errors.New("toolsearch: no dynamic tools")
	}
	mw, err := toolsearch.New(ctx, &toolsearch.Config{
		DynamicTools:       dynamic,
		UseModelToolSearch: !clientMode,
	})
	return mw, clientMode, err
}
```

> **Server-key collision resolved.** Red-team flagged that `SplitN(...,2)[0]` truncates a server name containing `__`. We instead use the **same `strings.Split(name,"__")` convention eino itself uses** (toolsearch.go:603) and key on `segs[1]`. The UI's "Always load" toggle (Section 4) writes that **same first segment** as the key, so the match is consistent with eino's grouping. We do **not** attempt to support `__` inside server names as a distinct key — eino can't either; this is a documented, accepted limitation (Risk 4).

### 2.2 DynamicTools self-register; we pass only static to NewAgent

`toolsearch.BeforeAgent` **unconditionally** appends both the dynamic tools and the `tool_search` meta-tool into `runCtx.Tools` in **every** mode (verified toolsearch.go:143-145). So when client mode is active each surface passes **only the static list** to `NewAgent`'s tools; dynamic tools stay executable and still route through innermost approval (agent.go:86). The "executable-registration / unknown-tool" risk is a confirmed **non-issue**.

> **Native-mode prose corrected.** Native mode does **not** remove the meta-tool from `runCtx.Tools`; `BeforeAgent` always adds it. Native only strips dynamic tools and the meta-tool from `state.ToolInfos` (the model-facing list) inside `BeforeModelRewriteState` (toolsearch.go:286-288). The `tool_search` tool object therefore exists as executable in all modes; clear-protection is gated on the **mode flag**, not on tool presence.

### 2.3 Mode resolution (resolver in `internal/command`)

The resolver lives in `internal/command` (each surface's package imports both `config` and `model`), avoiding the `config→agent` / `config→model` import cycle.

```go
// internal/command/toolsearch_resolve.go (new) — shared by web.go, acp.go, interactive.go.
type effMode string
const (
	effStatic effMode = "static"
	effClient effMode = "client"
	effModel  effMode = "model"
)

// resolveToolSearchMode. nDynamic is len(dynamic) AFTER AlwaysLoad subtraction.
func resolveToolSearchMode(tsc config.ToolSearchConfig, prov, model string, planMode, unattended bool, nDynamic int) effMode {
	if planMode               { return effStatic } // buildPlanTools has no MCP
	if nDynamic < tsc.Threshold { return effStatic } // below-threshold short-circuit
	nativeOK := agentmodel.AdapterSupportsNativeToolSearch(prov, model) // FALSE everywhere today
	switch tsc.Mode {
	case "off":
		return effStatic
	case "model":
		// Explicit-but-unsupported: STAY STATIC (do NOT silently downgrade to cache-hostile
		// client). Surfaced in UI as "native unavailable" so the user opts into client deliberately.
		return ifThen(nativeOK, effModel, effStatic)
	case "client":
		if unattended { // never run stall-prone client headless unless explicitly allowed
			return clientUnattended(tsc) // see below
		}
		return effClient
	default: // "auto"
		if nativeOK { return effModel }
		if unattended { return effStatic }
		return effStatic // auto NEVER silently picks client
	}
}

func clientUnattended(tsc config.ToolSearchConfig) effMode {
	switch tsc.UnattendedFallback {
	case "client": return effClient   // user explicitly accepts headless stall risk
	default:       return effStatic   // "native-or-off" / "off": native is false today => static
	}
}
```

> **Explicit-`model`-when-unsupported resolved (was ambiguous).** A hand-edited `"mode":"model"` on an unsupported adapter resolves to **`static`**, NOT a silent downgrade to client. This prevents a config-edit/headless user (who never sees the amber UI chip) from being dropped into the cache-hostile mode without consent.

> **`unattended` defined per surface (was a hole).** `unattended` is true **only** for headless/automation runs. In web.go it is the existing `excludeInteractive` flag (web.go:250, in closure scope). **acp.go and interactive.go are inherently attended → `unattended = false`** at those call sites. Automations that run through the web task path already set `excludeInteractive=true`; no new plumbing is needed.

### 2.4 Per-surface call-site integration

The split/resolve/build sequence is identical; only the *inputs* differ per surface. Define one shared helper that each surface calls with its locals:

```go
// internal/command/toolsearch_resolve.go (cont.)
// applyToolSearch wires the middleware + returns the static toollist + whether to protect tool_search.
// Each surface calls this with ITS OWN cm, prov, model, planMode, unattended, and flat toolList.
func applyToolSearch(ctx context.Context, cfg *config.Config, toolList []tool.BaseTool,
	prov, model string, planMode, unattended bool,
) (effTools []tool.BaseTool, mw adk.ChatModelAgentMiddleware, protectToolSearch bool) {

	effTools = toolList
	if planMode {
		return effTools, nil, false // plan mode: forced static, never build
	}
	tsc := cfg.ToolSearchSettings()
	static, dynamic := agent.SplitForToolSearch(ctx, toolList, tsc.AlwaysLoadSet())
	mode := resolveToolSearchMode(tsc, prov, model, planMode, unattended, len(dynamic))
	if mode != effClient && mode != effModel {
		return effTools, nil, false
	}
	built, needsProtect, err := agent.BuildToolSearch(ctx, dynamic, mode == effClient)
	if err != nil { // fail-open: keep full toolList, no middleware, no protection
		return toolList, nil, false
	}
	return static, built, needsProtect // dynamic self-register via eino BeforeAgent
}
```

**web.go** (`makeAgent`, web.go:435-440) — the split runs **after** `dropInteractiveTools` (web.go:356), so the partition never phantoms a stripped interactive tool. `makeAgent` does **not** need a new signature: `createAgent(prov, mod)` (web.go:450) already has `prov, mod` in scope and builds `cm` from them; thread them into `makeAgent` only as plain args (no behavioral plumbing bug — see Risk 3). `rebuildForMode` (web.go:469) reuses `providerName/modelName` (recoverable; provider can't change on a mode toggle).

```go
// inside makeAgent, replacing `toolList := buildAllTools(cm)` tail:
toolList := buildAllTools(cm) // UNCHANGED signature; breakdownFn (web.go:498) still consumes it
var tsMw adk.ChatModelAgentMiddleware
protectTS := false
if planMode {
	toolList = buildPlanTools()
} else {
	toolList, tsMw, protectTS = applyToolSearch(ctx, cfg, toolList, prov, mod, false, excludeInteractive)
}
// handlers[0] = toolsearch (outermost)
if tsMw != nil {
	handlers = append([]adk.ChatModelAgentMiddleware{tsMw}, handlers...)
}
```

Reduction (web.go:413-417) gains a mode-gated exclude:
```go
clearExclude := []string(nil)
if protectTS { clearExclude = []string{agent.ToolSearchToolName} }
reductionMw, err := reduction.New(ctx, &reduction.Config{
	// ... existing fields ...
	ClearExcludeTools: clearExclude,                    // NEW, client-mode-only
	ToolConfig: map[string]*reduction.ToolReductionConfig{"read": {SkipClear: true}}, // unchanged
})
```

**acp.go** — `makeAgent(sysPrompt, toolList)` (acp.go:479) takes a pre-built list and no cm/provider. We resolve provider/model from the acp session's config (the same values used to build its chat model) and call `applyToolSearch` with `unattended=false`. The `allTools` snapshot (acp.go:354-363) is the single split input; late MCP is out of scope here (acp loads MCP once, no async rebuild — Risk/Deferral 2). Reduction exclude added at acp.go:448.

**interactive.go** — `buildAllTools()` is a method (interactive.go:82); call `applyToolSearch` inside `makeAgent`'s build path (interactive.go:253 region) with `s.cfg`, `s.chatModel`'s provider/model (from `s.cfg.GetProviderModel()`, interactive.go:160), `unattended=false`, and the result of `s.buildAllTools()`. Rebuilt on `reloadMCP` (interactive.go:285) and model switch (interactive.go:606) — the existing rebuild points already re-run `buildAllTools`, so the split re-runs for free. Reduction exclude added at interactive.go:208.

### 2.5 Subagents — explicit static-only guard (was "by construction")

Subagent toolsets (`subagentTool.buildTools`, internal/tools/subagent.go:379) yield ~6-9 tools with no MCP today — below threshold. But the red-team is right that this is an asserted invariant, not an enforced one. **PR1 makes it explicit and cheap**: subagent agent construction does **not** call `applyToolSearch` at all (no code path added), and we add a one-line comment at the subagent `NewAgent` call documenting that tool search is intentionally never wired for subagents. If a future change gives subagents MCP, the absence is a deliberate, documented decision, not an accident. We do **not** thread policy into `SubagentDeps`.

---

## 3. Config

```go
// internal/config/config.go

// ToolSearchConfig controls eino dynamic tool-search.
// Cache posture: "static"/"auto"(today) keep the tool block stable (cache-friendly);
// "client" churns ToolInfos each turn (cache-hostile, opt-in); "model" is gated behind
// an adapter capability that is false today.
type ToolSearchConfig struct {
	// Mode: "off" | "auto" | "client" | "model". Empty -> "auto".
	//   auto   -> native when the adapter supports it (false today), else STATIC. Never client.
	//   client -> local meta-tool + keyword scoring; works everywhere, hurts prompt cache.
	//   model  -> provider-native (DORMANT). On an unsupported adapter resolves to STATIC, not client.
	//   off    -> never engage; all tools always visible.
	Mode string `json:"mode,omitempty"`

	// Threshold: minimum DYNAMIC MCP tools (after AlwaysLoadServers subtraction) before
	// search engages. <=0 -> default 20.
	Threshold int `json:"threshold,omitempty"`

	// UnattendedFallback for headless automations: "native-or-off" (default) | "off" | "client".
	UnattendedFallback string `json:"unattended_fallback,omitempty"`

	// AlwaysLoadServers: MCP server keys (mcp__<key>__*) kept always-visible even when engaged.
	// Kept here (not on MCPServer) so flipping Mode off restores everything.
	AlwaysLoadServers []string `json:"always_load_servers,omitempty"`
}
```

Slotted alongside the other pointer sub-configs:
```go
type Config struct {
	// ... Budget, Compaction, Subagent, Team ...
	ToolSearch *ToolSearchConfig `json:"tool_search,omitempty"`
}
```

Defaulting accessor (mirrors `CompactionThreshold`; named `ToolSearchSettings()` to avoid the field/method name clash; returns a **value** so concurrent sessions never read a half-mutated struct):
```go
func (c *Config) ToolSearchSettings() ToolSearchConfig {
	out := ToolSearchConfig{Mode: "auto", Threshold: 20, UnattendedFallback: "native-or-off"}
	if c == nil || c.ToolSearch == nil {
		return out
	}
	switch c.ToolSearch.Mode {
	case "off", "auto", "client", "model":
		out.Mode = c.ToolSearch.Mode
	case "":
		// empty -> keep default "auto" (matches DefaultMode empty-string fallback prior art)
	default:
		// unknown persisted value -> default "auto" (defensive; the PUT handler rejects bad
		// values with 400, so a bad value can only arrive via hand-edit)
	}
	if c.ToolSearch.Threshold > 0 {
		out.Threshold = c.ToolSearch.Threshold
	}
	switch c.ToolSearch.UnattendedFallback {
	case "off", "client", "native-or-off":
		out.UnattendedFallback = c.ToolSearch.UnattendedFallback
	}
	out.AlwaysLoadServers = c.ToolSearch.AlwaysLoadServers
	return out
}

func (t ToolSearchConfig) AlwaysLoadSet() map[string]bool {
	m := make(map[string]bool, len(t.AlwaysLoadServers))
	for _, s := range t.AlwaysLoadServers {
		m[s] = true
	}
	return m
}
```

> **Empty-string vs bad-value divergence resolved.** The PUT handler validates enums and returns **400** on bad values, so bad values never persist via the UI. A hand-edited empty or unknown `Mode` silently re-defaults to `auto` in the accessor (defensive). These two paths intentionally differ: requests are strict, persisted state is forgiving — matching the `DefaultMode` empty-string prior art.

**Capability predicate** lives in `internal/model` (already provider-aware); `config` exposes only primitives:
```go
// internal/model/toolsearch_cap.go (new)
// AdapterSupportsNativeToolSearch reports whether the chatModel adapter serializes
// DeferredToolInfos/ToolSearchTool to the provider. FALSE everywhere today because
// buildRequest (chatmodel.go) reads only GetCommonOptions().Tools.
func AdapterSupportsNativeToolSearch(provider, model string) bool {
	return false // dormant until deferred-tool forwarding lands (PR2)
}
```

**Back-compat:** zero. `ToolSearch` is nil on every existing config; accessor returns `{auto,20,native-or-off}`; `auto` resolves to static (native predicate false) ⇒ **byte-identical to today**. No version bump. Round-trips through existing locked `LoadConfig`/`SaveConfig`.

> **Dead-code-under-defaults acknowledged (was unflagged).** Because defaults resolve to static, **none** of the new middleware/reduction-exclude code executes in a default install — the only exercised PR1 path is the opt-in `client` mode. This is the *intended* safe outcome, but it means the integration gets **zero coverage from normal use**. PR1 therefore ships the resolver truth-matrix + a forced-client integration test (Section 5) so the path is exercised in CI, not just by opt-in users. This is called out in Risk 8.

---

## 4. Settings UI

### Placement — fold into the existing **MCP tab** (no new nav-rail tab)

The threshold is meaningless without the server count that lives on the MCP tab, and a dedicated tab would force touching the `activeTab` union (SettingsDialog.vue:103), the tab `v-for`, `iconFor`, and `tabLabel` for a handful of booleans. We mirror the existing per-server-row-with-toggle pattern.

### Controls (top of the MCP tab, above the server list)

1. **Master mode** `<select>` (`s-row`): `Auto (recommended)` / `Client (all providers · reduces cache)` / `Off`. `Model-native (Claude)` rendered **disabled with tooltip** ("Coming soon — requires provider support").
2. **Threshold** number input (`s-row`), default 20, dimmed when mode is `off`. Label: "Activate after N MCP tools."
3. **Unattended automations** `<select>` (`s-row`): `Auto (native or off)` / `Always off` / `Client (may stall headless)` — the word "stall" is deliberate.

### Threshold banner (onboarding moment)

When effective mode is not engaged AND `dynamicCount >= threshold`, render a dismissible accent `s-row`: **"You have {n} MCP tools available — turn on on-demand loading so the agent searches for them only when needed. Nothing is removed."** Buttons: **Turn on** (sets `mode=auto`/`client`) and **Not now** (persists `toolSearchBannerDismissed`). Below threshold or zero MCP servers → render nothing.

> **Banner count reconciled with threshold (was contradictory).** The banner says **"{n} MCP tools available"** where `n` = total `mcp__*` tools, but the gate is `dynamicCount >= threshold` where `dynamicCount` = total **minus AlwaysLoad**. The status line (below) shows the split explicitly so a user who marked most servers always-load sees *why* it's inactive: "{static} always visible · {dynamic} on-demand-eligible — {dynamic} of {threshold} needed." Banner shows the headline count; status shows the gating count; they never silently disagree.

### Status display + per-server tags

- Live status line driven by the API: *"{static} always visible · {deferred} on-demand ({mode})"* or *"Inactive — {dynamic} of {threshold} tools needed"* or *"Native search unavailable on {provider}; using client-side."*
- **Per-server tag** on each existing MCP row: muted **`on demand`** vs **`always loaded`** badge + a secondary **"Always load"** toggle that adds/removes the server **key** from `AlwaysLoadServers`.
- Plan-mode note: *"Tool search is paused in Plan mode (focused toolset)."*

> **Plan-mode count contradiction resolved.** The dialog reads **global config**, not per-engine plan state, so the displayed counts are always the **non-plan MCP-based numbers**. The plan-mode note is shown **only as an advisory** when the active foreground engine reports `plan_paused: true` from the API (the status closure knows the engine's live plan state). We do **not** show plan-mode tool counts — the note explains why the agent's behavior differs from the displayed numbers, which is the honest framing.

### Status source — `ToolSearchStatusFn` on `EngineConfig` (NOT live-in-handler)

The GET handler runs on `web.Server` and has **no handle** to web.go's local `mcpToolsPtr`. We add a closure to `EngineConfig`, mirroring the existing `BreakdownFn` (web.go:480) exactly:

```go
// web.EngineConfig gains:
ToolSearchStatusFn func() web.ToolSearchStatus

// built inside buildWebTask where mcpToolsPtr + currentCM + cfg are in scope:
toolSearchStatusFn := func() web.ToolSearchStatus {
	cmMu.Lock(); cm := currentCM; plan := currentPlanMode; cmMu.Unlock()
	tsc := cfg.ToolSearchSettings()
	var all []tool.BaseTool
	if cm != nil { all = buildAllTools(cm) }
	static, dynamic := agent.SplitForToolSearch(ctx, all, tsc.AlwaysLoadSet())
	mode := resolveToolSearchMode(tsc, providerName, modelName, plan, excludeInteractive, len(dynamic))
	return web.ToolSearchStatus{
		Mode: tsc.Mode, Threshold: tsc.Threshold, UnattendedFallback: tsc.UnattendedFallback,
		AlwaysLoadServers: tsc.AlwaysLoadServers,
		StaticCount: len(static), DeferredCount: len(dynamic),
		Engaged: mode == effClient || mode == effModel, EffectiveMode: string(mode),
		NativeSupported: agentmodel.AdapterSupportsNativeToolSearch(providerName, modelName),
		PlanPaused: plan,
		PerServer: groupByServer(ctx, all, tsc.AlwaysLoadSet()), // {server: {count, alwaysLoaded}}
	}
}
```

> **Split-snapshot-vs-UI-snapshot reconciled (source of truth defined).** Both the agent build and `ToolSearchStatusFn` read the **same `mcpToolsPtr.Load()` / `buildAllTools(cm)`** and run the **same `resolveToolSearchMode`**. The status the UI shows is therefore computed by the identical code the agent build uses, against the latest snapshot. The only divergence is temporal: a server that connects between the last agent rebuild and a GET shows as eligible in the status before the *next* rebuild engages it. The UI copy ("on-demand-eligible") and refresh-on-open manage this; it is not a correctness gap because `Engaged`/`EffectiveMode` are recomputed from the live snapshot, not cached at build time. **The engine closure is the single source of truth** (not a build-time-cached status object).

### New `/api` endpoints

- **`GET /api/toolsearch`** → calls the active engine's `ToolSearchStatusFn()`, returns the struct above. No secrets. If no active engine, returns config-only fields with zero counts.
- **`PUT /api/toolsearch`** → body `{ mode, threshold, unattended_fallback }`. Validate enums (400 on bad), **lock `s.cfgMu`**, mutate `s.cfg.ToolSearch` (lazy-alloc if nil), `config.SaveConfig(s.cfg)`, unlock, then call existing `reloadMCPAndRebuild()` so it takes visible effect.
- **`POST /api/mcp/{name}/loadmode`** → body `{ always_load: bool }`, adds/removes the **server key** from `AlwaysLoadServers` under **`s.cfgMu`** (same lock as PUT — both touch `s.cfg.ToolSearch`), save, rebuild.
- **`handleDeleteMCP` cleanup**: on server delete, prune the key from `AlwaysLoadServers` under `s.cfgMu`.

> **Lock corrected to `s.cfgMu`.** Both new endpoints mutate `s.cfg.ToolSearch` — a config-field RMW exactly like `DisabledSkills` (server.go:2740, which carries the explicit comment that `s.mu` is insufficient for cfg RMW+save). Using `s.cfgMu` for **both** endpoints prevents the torn read-modify-write the draft's `s.mu` choice would cause against concurrent skill/approval/mode saves under the shared tree.

> **Setup-flow cfg-divergence handled (was unaddressed).** The provider-setup handler (server.go:2982-3021) does `s.cfg = cfg` (a **new** pointer from `LoadConfig`) but the command-side `makeAgent` closure reads the **original** `cfg`. After setup, `PUT /api/toolsearch` would mutate `s.cfg` while the agent builder reads stale closure-cfg. **Resolution:** `reloadMCPAndRebuild()` (called by PUT) rebuilds the engine via `eng.createAgent`, which reads from the engine's own config snapshot path — but to be safe, PR1 makes the command-side `makeAgent`/`createAgent` read `cfg.ToolSearchSettings()` **through the engine's live config reference, not the captured closure**. Concretely: `buildWebTask` already closes over `cfg`; we pass the **same pointer** that becomes `s.cfg`, and the setup handler is amended to mutate the existing config **in place** (`*existing = *loaded` field-copy under `s.cfgMu`) rather than swapping the pointer, so closure-cfg and `s.cfg` never diverge. This is a small, surgical change to server.go:3018 and is part of PR1.

### i18n keys (FIVE locales: en, ja, ko, zh-Hans, zh-Hant)

Under `settings.mcp.toolSearch.*`, mirroring the existing `settings.mcp.*` block which exists in **all five** locales including `ja.ts` (verified, ja.ts:297). Omitting `ja.ts` would render raw keys for Japanese users.

```
settings.mcp.toolSearch.title          "Tool search"
settings.mcp.toolSearch.mode           "Mode"
settings.mcp.toolSearch.modeAuto       "Auto (recommended)"
settings.mcp.toolSearch.modeClient     "Client (all providers · reduces cache)"
settings.mcp.toolSearch.modeModel      "Model-native (Claude · coming soon)"
settings.mcp.toolSearch.modeOff        "Off"
settings.mcp.toolSearch.threshold      "Activate after N MCP tools"
settings.mcp.toolSearch.unattended     "Unattended automations"
settings.mcp.toolSearch.bannerTitle    "You have {n} MCP tools available"
settings.mcp.toolSearch.bannerBody     "Turn on on-demand loading so the agent searches for MCP tools only when needed. Nothing is removed."
settings.mcp.toolSearch.turnOn         "Turn on"
settings.mcp.toolSearch.notNow         "Not now"
settings.mcp.toolSearch.statusActive   "{static} always visible · {deferred} on-demand ({mode})"
settings.mcp.toolSearch.statusInactive "Inactive — {n} of {min} tools needed"
settings.mcp.toolSearch.nativeFallback "Native search unavailable on {provider}; using client-side."
settings.mcp.toolSearch.alwaysLoad     "Always load"
settings.mcp.toolSearch.tagOnDemand    "on demand"
settings.mcp.toolSearch.tagAlways      "always loaded"
settings.mcp.toolSearch.planPaused     "Tool search is paused in Plan mode (focused toolset)"
```

SettingsDialog.vue gets: one `toggleAlwaysLoad(serverKey)` method (copy of `toggleMcp`, POSTing to `/api/mcp/{name}/loadmode`), one `setToolSearchMode()` method, refs for status, and an on-open `GET /api/toolsearch`. No new component, no `activeTab` change.

---

## 5. Rollout / Phasing

**PR1 (MVP — shippable):**
- `ToolSearchConfig` + `ToolSearchSettings()` accessor + `AdapterSupportsNativeToolSearch` predicate (false).
- `internal/agent/toolsearch.go`: `SplitForToolSearch`, `BuildToolSearch`, `mcpServerOf`, `ToolSearchToolName`.
- `internal/command/toolsearch_resolve.go`: `resolveToolSearchMode`, `applyToolSearch`.
- Wire `applyToolSearch` into **all three** surfaces with each surface's own snapshot/cm/provider/model and correct `unattended` (web: `excludeInteractive`; acp/interactive: `false`).
- Reduction `ClearExcludeTools=["tool_search"]`, client-mode-only, at all three sites (web.go:416, acp.go:448, interactive.go:208).
- In-place setup-cfg fix (server.go:3018) so closure-cfg and `s.cfg` never diverge.
- `ToolSearchStatusFn` on `EngineConfig` + `web.ToolSearchStatus` type.
- MCP-tab UI: master mode, threshold, unattended, banner, per-server tags + Always-load toggle, live status, plan-paused advisory.
- `GET`/`PUT /api/toolsearch`, `POST /api/mcp/{name}/loadmode`, delete-cleanup — all config writes under `s.cfgMu`.
- i18n keys in **all five** locales.
- Tests: `resolveToolSearchMode` truth-matrix (mode × native × attended × fallback × threshold, incl. explicit-`model`-unsupported⇒static and `auto`-unsupported⇒static); **forced-client reduction-survival regression** (drive reduction over history with a `tool_search` **Tool-role** result above `MaxTokensForClear`, assert the tool-role message survives — not a generic tool result); approval-fires-for-searched-`mcp__*`-tool; below-threshold byte-compat; plan-mode never-builds; `mcpServerOf` against real names containing `__` (`plugin_design_asana`, `Claude_in_Chrome`).

**Deferred to PR2+ (explicit, with reasons):**
- **Model-native activation.** Extend `chatModel.buildRequest` to forward `state.DeferredToolInfos`/`runCtx.ToolSearchTool`; flip the predicate per-route; enable the disabled `model` UI option. *Reason:* requires adapter work the go-openai transport doesn't support today; must be verified end-to-end (native silently yields zero search if wired wrong).
- **acp.go async/late-MCP.** acp loads MCP once synchronously (no `mcpToolsPtr`, no async rebuild). The split is correct against that one snapshot; late-connecting servers are not picked up. *Reason:* acp has no rebuild contract today; adding one is out of scope and orthogonal to tool_search. Documented, not silently broken.
- **Telemetry** (`toolsearch.mode/dynamic_count/searched/search_calls`, hit-rate canary). *Reason:* `RecordToolSearch`-style helpers do not exist in `internal/telemetry`; gating client-mode on unbuilt surface is unacceptable. The hit-rate canary is the eventual early-warning for "weak model isn't calling tool_search."
- **Per-tool (vs per-server) always-load granularity; rename-path pruning for `AlwaysLoadServers`; "recently searched" transcript indicator.**

---

## 6. Risks & Open Questions

1. **Weak models in client mode (no runtime fallback for attended).** A user enabling client mode with a model that unreliably calls `tool_search` makes MCP tools effectively unreachable in interactive sessions too (we only guard unattended). Mitigation: experimental labeling + status line; telemetry hit-rate is PR2. Accepted because client mode is opt-in and off by default.
2. **Native is dormant and load-bearing.** The cache-friendly win hinges on PR2 adapter work. Until then jcode ships only cache-hostile client (opt-in) or static. **Open question:** does eino's `runCtx.ToolSearchTool` map onto Anthropic's server-side tool-search request shape through the go-openai transport, or does jcode need a non-go-openai path for Anthropic? Must be answered before flipping the predicate.
3. **"Plumbing bug" was inflated; no signature change strictly required.** `createAgent(prov, mod)` already builds `cm` from the live provider/model, and `rebuildForMode` recovers them. Threading `prov, model` into `makeAgent` is plain argument-passing for the resolver, not a fix for broken behavior. In PR1 they feed only the always-false predicate, so this is near-zero-risk wiring.
4. **`AlwaysLoadServers` key fidelity.** We key on eino's own `strings.Split(name,"__")[1]` so the UI toggle and the matcher agree even when a server name contains `__` — but a server name whose first `__`-segment collides with another server's would share an always-load key. jcode does not sanitize server names on create; this is an accepted, documented limitation (eino itself groups this way). Rename/delete pruning: delete is wired (PR1); rename has no path today.
5. **Async MCP count lag in the banner/status.** A server connecting just after the dialog opens shows as eligible before the next agent rebuild engages it. The status closure recomputes `Engaged`/`EffectiveMode` from the live snapshot each GET, so it is never stale relative to the *current* tool set; the only lag is build-vs-snapshot timing. Copy + refresh-on-open manage it. Not a correctness issue.
6. **`"tool_search"` literal pinned in one place** (`ToolSearchToolName`, matching toolsearch.go:412). If eino renames it upstream, forward-selection breaks silently — the reduction-survival regression test is the canary.
7. **acp.go provider/model recovery.** acp's `makeAgent(sysPrompt, toolList)` has no cm/provider param; we recover provider/model from acp session config. Confirmed available, but any future acp path that switches model mid-session must re-resolve capability — noted for PR2.
8. **Dead-code-under-defaults / coverage gap.** Defaults resolve to static, so the integration is exercised only in opt-in client mode. PR1's forced-client integration test + resolver truth-matrix give CI coverage so the path is not first-exercised by users. The "cache-friendly posture protected" framing is honest *because* the new code is inert by default — the trade-off is that the only user-reachable PR1 path is the cache-hostile one, which is acceptable for an experimental opt-in.

---

**Key files touched:** `/Users/jack/workpath/jjj/jcode/internal/config/config.go`, `/Users/jack/workpath/jjj/jcode/internal/agent/toolsearch.go` (new), `/Users/jack/workpath/jjj/jcode/internal/command/toolsearch_resolve.go` (new), `/Users/jack/workpath/jjj/jcode/internal/model/toolsearch_cap.go` (new), `/Users/jack/workpath/jjj/jcode/internal/command/web.go` (makeAgent + reduction + ToolSearchStatusFn + endpoints registration), `/Users/jack/workpath/jjj/jcode/internal/command/acp.go` (applyToolSearch + reduction site), `/Users/jack/workpath/jjj/jcode/internal/command/interactive.go` (applyToolSearch + reduction site), `/Users/jack/workpath/jjj/jcode/internal/web/server.go` (3 routes + delete cleanup + in-place setup-cfg fix, all config writes under `s.cfgMu`), `/Users/jack/workpath/jjj/jcode/web/src/components/SettingsDialog.vue` (MCP-tab additions), `/Users/jack/workpath/jjj/jcode/web/src/i18n/locales/{en,ja,ko,zh-Hans,zh-Hant}.ts`.

---

## Implementation checklist (PR1)

- [ ] 1. Add `ToolSearchConfig` + `ToolSearch *ToolSearchConfig` field + `ToolSearchSettings()` value-accessor + `AlwaysLoadSet()` to `internal/config/config.go`.
- [ ] 2. Add `AdapterSupportsNativeToolSearch(provider, model) bool { return false }` in `internal/model/toolsearch_cap.go`.
- [ ] 3. Create `internal/agent/toolsearch.go`: `ToolSearchToolName="tool_search"`, `mcpServerOf` (split on `"__"`, key `segs[1]`), `SplitForToolSearch`, `BuildToolSearch` (error on `len==0`).
- [ ] 4. Create `internal/command/toolsearch_resolve.go`: `resolveToolSearchMode` (plan/threshold guards first; explicit-`model`-unsupported⇒static; `auto`-unsupported⇒static; unattended-client⇒fallback) and `applyToolSearch`.
- [ ] 5. Wire `applyToolSearch` into web.go `makeAgent` (after `dropInteractiveTools`, `unattended=excludeInteractive`, thread `prov,mod` as args), prepend middleware as `handlers[0]`, leave `buildAllTools` signature unchanged.
- [ ] 6. Wire `applyToolSearch` into acp.go `makeAgent` (provider/model from session cfg, `unattended=false`) and interactive.go `makeAgent` (`s.cfg`/`s.chatModel`, `unattended=false`).
- [ ] 7. Add mode-gated `ClearExcludeTools: ["tool_search"]` to reduction config at web.go:416, acp.go:448, interactive.go:208 (client-mode-only).
- [ ] 8. Fix setup-flow cfg divergence: amend server.go:3018 to copy fields in place under `s.cfgMu` instead of swapping the `s.cfg` pointer.
- [ ] 9. Add `web.ToolSearchStatus` type + `ToolSearchStatusFn` to `EngineConfig`; build the closure in `buildWebTask` (mirror `breakdownFn`).
- [ ] 10. Add `GET /api/toolsearch` (calls `ToolSearchStatusFn`), `PUT /api/toolsearch` (enum-validate→400, `s.cfgMu`→save→`reloadMCPAndRebuild`), `POST /api/mcp/{name}/loadmode` (`s.cfgMu`), and `AlwaysLoadServers` prune in `handleDeleteMCP`.
- [ ] 11. SettingsDialog.vue MCP-tab: mode/threshold/unattended controls, banner, per-server tags + Always-load toggle, live status line, plan-paused advisory; `toggleAlwaysLoad`/`setToolSearchMode` methods + on-open GET.
- [ ] 12. Add `settings.mcp.toolSearch.*` keys to all five locales (en, ja, ko, zh-Hans, zh-Hant).
- [ ] 13. Tests: resolver truth-matrix; forced-client reduction-survival regression (assert `tool_search` Tool-role message survives clear); approval-fires-for-searched-mcp-tool; below-threshold byte-compat; plan-mode never-builds; `mcpServerOf` on real `__`-containing names.
- [ ] 14. `go build ./... && go test ./internal/agent/... ./internal/command/... ./internal/config/...` and `pnpm -C web build` green.
