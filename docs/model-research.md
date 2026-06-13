# Long-Context Model Research & Window-Size Management

> Last researched: **2026-06-13**. This file is the cached result of a per-provider
> survey of long-context (≈1M token) models so we don't have to re-research every
> time the model lineup shifts. Referenced from `internal/model/registry.go` and
> `internal/model/chatmodel.go`.

## TL;DR — corrections to common assumptions

| Claim | Reality (2026-06) |
|-------|-------------------|
| "Kimi K2.7 supports 1M context" | ❌ **256K** (262,144). Moonshot's K2.6 and K2.7-Code are both 256K. |
| "GLM-5.2 is out with 1M context" | ✅ **Confirmed 2026-06-13.** Official Z.ai DevPack config publishes `contextWindow: 1000000, maxTokens: 131072`. Not on models.dev/aggregators yet → injected via `additionalModels`. (GLM-5/5.1 remain ~200K.) |
| "MiniMax-M3 is 1M context" | ✅ **Yes, 1,048,576.** But models.dev records only **512K** (the *guaranteed minimum*), which under-sized our window management. Corrected via override. |
| "DeepSeek-V4-Pro is 1M context" | ✅ **Yes, 1,000,000.** Already correct in the registry. |
| "Qwen 3.6 Plus is the latest Alibaba flagship" | ⚠️ Superseded — **Qwen 3.7 Max / 3.7 Plus** (both 1M) shipped May–Jun 2026. |

## Per-provider survey

Context = max input window in tokens. Values are from the **models.dev** registry
(regenerated into `registry_generated.go`) unless noted. "Recommended" = what jcode
now flags as recommended + default-enabled (see `recommendedModels` in `registry.go`).

### MiniMax
| Model | Context | Max output | Notes |
|-------|---------|-----------|-------|
| **MiniMax-M3** ⭐ | **1,000,000** (advertised; 512K guaranteed min) | 128K | MSA sparse attention, multimodal (text/image/video), released 2026-06-01. models.dev lists 512K → **overridden to 1M** in `contextLimitOverrides`. |
| MiniMax-M2.7 | 204,800 | — | Prior gen. |

Pricing (M3): ~$0.60/M input, $2.40/M output (models.dev); long-context tier above 512K.

### Moonshot (Kimi)
| Model | Context | Notes |
|-------|---------|-------|
| **kimi-k2.7-code** ⭐ | **262,144** (256K) | Newest coding model, released 2026-06-12, ~30% fewer reasoning tokens than K2.6. **Not** a 1M model. |
| kimi-k2.6 | 262,144 | Multimodal agentic, released 2026-04-20. |

### Zhipu / Z.ai (GLM)
| Model | Context | Max output | Notes |
|-------|---------|-----------|-------|
| **glm-5.2** ⭐ | **1,000,000** | 131,072 | Released 2026-06-13 to GLM Coding Plan users (Lite/Pro/Max/Team). Official Z.ai DevPack config: `contextWindow: 1000000, maxTokens: 131072`. Full 1M needs the **`glm-5.2[1m]`** variant. Standalone API + MIT open weights "next week". **Not on models.dev yet** → injected via `additionalModels` on `zhipuai`, `zhipuai-coding-plan`, `zai`, `zai-coding-plan`. |
| glm-5.1 | ~200,000 | 128K | models.dev (zhipuai 200K, alibaba 202,752), released 2026-04. |
| glm-5 | ~200,000 | — | Released 2026-02-11. |

> GLM-5.2's spec rests on the official Z.ai DevPack page (`docs.z.ai/devpack/latest-model`),
> not marketing — verified 2026-06-13 across the official dev-letter ("致开发者：GLM-5.2 全量开放",
> "支持真正可用的 1M 上下文") and 6+ outlets. When models.dev finally lists GLM-5.2, the
> `additionalModels` merge auto-defers to the official record (skips if already present).

### DeepSeek
| Model | Context | Notes |
|-------|---------|-------|
| **deepseek-v4-pro** ⭐ | **1,000,000** | 1.6T params MoE, CSA+HCA hybrid attention, released 2026-04-24. Genuine 1M. |
| deepseek-v4-flash | 1,000,000 | Cheaper sibling. |

### Alibaba (Qwen)
| Model | Context | Notes |
|-------|---------|-------|
| **qwen3.7-plus** ⭐ | **1,000,000** | Multimodal agent, released Jun 2026. |
| **qwen3.7-max** ⭐ | **1,000,000** | Flagship reasoning, released 2026-05-20 (3.6-max was 256K). ~$2.50/$7.50 per M. |
| qwen3.6-plus | 1,000,000 | Prior gen (still 1M). |

### Frontier (for reference; recommended bumped to current gen)
| Provider | Recommended (was → now) | Context |
|----------|------------------------|---------|
| OpenAI | gpt-4.1 → **gpt-5.5** | ~1,050,000 |
| Anthropic | claude-sonnet-4-20250514 → **claude-opus-4-8 + claude-sonnet-4-6** | 1,000,000 each |
| Google | gemini-2.5-pro → **gemini-3.1-pro-preview** | ~1,048,576 |

## How jcode manages the context window

### Resolution — single source of truth
All window sizing flows through **`model.ResolveContextLimit(reg, cfg, provider, model)`**
(`internal/model/context_limit.go`). Order, first positive hit wins:

1. **User override** — `config.ContextLimits["provider/model"]`, then `["model"]`
2. **models.dev registry** — `reg.GetModelContextLimit(...)` (+ hand overrides at init)
3. **Built-in fallback** — `knownModels` table in `chatmodel.go` (offline safety net)
4. **`config.DefaultContextLimit`**, else `DefaultContextLimitFallback` (200000)

This replaced 5 copy-pasted `registry → knownModels → 200000` blocks
(interactive / acp / web / runner), which was why unknown 1M models silently got
throttled to a 200K window.

### Thresholds derive from the resolved limit
The middleware stack scales off the resolved limit × a configurable fraction
(`config.CompactionThreshold()`, default **0.75**):

- **summarization** trigger: `limit × compactThreshold`
- **reduction** (lighter, earlier tool-output clearing): `limit × (compactThreshold − 0.15)`
- **compaction** strategy: `limit × compactThreshold`
- **reminders**: warn at 85% (`internal/prompts/reminders.go`)

Set `compaction.threshold` in config to change when compaction kicks in (was a dead
config field before — now wired).

### Correcting / teaching a window without code changes
```jsonc
// ~/.jcode/config.json
{
  "context_limits": {
    "minimax/MiniMax-M3": 1000000,   // override a specific provider/model
    "my-1m-model": 1000000           // or a bare model id
  },
  "default_context_limit": 200000,   // floor for models we can't identify
  "compaction": { "threshold": 0.8 } // compact at 80% instead of 75%
}
```
For a fully custom model also add it under `providers.<id>.custom_models` with its
`context` so it shows up in the picker.

### Built-in overrides that survive regeneration
`registry_generated.go` is rebuilt from models.dev via `go generate ./internal/model`
(`script/generate_models.go`) and is **DO NOT EDIT**. Hand-maintained corrections live
in `registry.go` and re-apply at `init()`:

- `additionalModels` — injects models that shipped before their models.dev record
  (currently GLM-5.2 on the four first-party Zhipu/Z.ai providers). Merge-only: skipped
  if the provider already defines that id, so the official record wins once it lands.
- `contextLimitOverrides` — fixes under-reported windows (currently MiniMax-M3 → 1M).
- `recommendedModels` — which models get the ⭐ recommended/default-enabled flag.

When refreshing the registry, re-check this doc's table and prune/extend those two
maps. Model IDs must match the registry exactly or the override is silently ignored
(guarded by `TestRecommendedFlagshipsAreLongContext`).

## Sources
- [MiniMax M3 (official)](https://www.minimax.io/models/text/m3), [MarkTechPost](https://www.marktechpost.com/2026/06/01/minimax-releases-minimax-m3-with-msa-architecture-supporting-1m-token-context-native-multimodality-and-agentic-coding/)
- [Kimi K2.7-Code (MarkTechPost)](https://www.marktechpost.com/2026/06/12/moonshot-ai-releases-kimi-k2-7-code-a-coding-model-reporting-21-8-on-kimi-code-bench-v2-over-k2-6/), [Kimi K2.6 (llm-stats)](https://llm-stats.com/models/kimi-k2.6)
- [DeepSeek-V4 (HF blog)](https://huggingface.co/blog/deepseekv4), [DeepSeek V4 Pro (Together)](https://www.together.ai/models/deepseek-v4-pro)
- [Qwen 3.7 Max (MarkTechPost)](https://www.marktechpost.com/2026/05/21/qwen-introduces-qwen3-7-max-a-reasoning-agent-model-with-a-1m-token-context-window/), [Qwen 3.6 Plus (llm-stats)](https://llm-stats.com/models/qwen3.6-plus)
- [GLM-5 (HF blog)](https://huggingface.co/blog/mlabonne/glm-5), [GLM-5 (llm-stats)](https://llm-stats.com/models/glm-5)
- GLM-5.2: [Z.ai DevPack config (official)](https://docs.z.ai/devpack/latest-model), [Z.ai announcement (X)](https://x.com/Zai_org/status/2065704919299235870), [系统极客](https://www.sysgeek.cn/glm-5-2-for-glm-coding-plan/), [新浪财经](https://finance.sina.com.cn/tech/roll/2026-06-13/doc-inicfzuq9552659.shtml)
- Registry data: [models.dev](https://models.dev/api.json)
