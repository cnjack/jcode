# Model research — 2026-10-18

Per-provider survey behind `recommendedModels`, `contextLimitOverrides`,
`retiredModels`, the static provider tables in `internal/model/registry.go`, and
the offline `knownModels` fallback in `internal/model/chatmodel.go`.

**Source of truth:** the models.dev `api.json` snapshot fetched by
`go generate ./internal/model/...` on 2026-10-18 (232 providers, 25 of which are
wanted). Prices below are USD per 1M tokens (input / output). Context windows
are tokens. "Starred" = `Recommended` + `DefaultEnabled` in the registry.

## Curation rule

`recommendedModels` follows the newest *mainline* flagship per vendor — the
model the vendor positions as the current default for coding/agent work — not
the most expensive premium tier and not a preview/beta. Premium tiers
(`gpt-6-astra`, `claude-fable-5-1`, `mistral-large-4` beta,
`MiniMax-M3.1-Flash-Preview`) remain selectable but unstarred. This matches the
2026-06-13 curation and keeps the auto-picked default affordable. Aggregators
and resellers apply the same rule per vendor they serve.

## Headline providers

| Provider | Starred | Context | $/1M in / out | Notes |
| --- | --- | --- | --- | --- |
| openai | `gpt-6.1-sol` | 1,050,000 | 2.00 / 10.00 | 2026-09-29. "Near-Astra performance… at a lower cost". `gpt-6-astra` (2026-09-04, 10/50) is the most capable tier but stays unstarred; `gpt-6-luna` (0.10/0.50) is the volume tier; `gpt-6-sol` is the previous 6.0 generation. |
| anthropic | `claude-opus-5-5`, `claude-sonnet-5-5` | 1,000,000 | 4.00 / 20.00 and 2.00 / 10.00 | Opus 5.5 (2026-09-22) for long-running agentic coding, Sonnet 5.5 (2026-09-28) for everyday agent work. `claude-haiku-5-5` (2026-10-07, 0.10/0.50) is the light tier; `claude-fable-5-1` (10/50) is the premium reasoning tier, unstarred. |
| google | `gemini-4-argon` | 1,048,576 | 2.00 / 10.00 | 2026-10-13, "flagship reasoning model for complex software engineering… long-horizon autonomous agents". `gemini-3.8-flash` (0.75/3.75, 1M) is the flash tier. |
| deepseek | `deepseek-v4-pro`, `deepseek-flash` | 1,000,000 | 0.66 / 1.98 and 0.15 / 0.60 | V4 Pro (2026-08-12) is the thinking flagship; `deepseek-flash` is the 2026-10-17 V4.1 Flash update (multimodal, improved coding/agentic). `deepseek-v4-flash` was marked deprecated upstream on 2026-10-17 and is retired from the catalog (see below). |
| zhipuai / zhipuai-coding-plan / zai / zai-coding-plan | `glm-5.3` | 1,000,000 | 1.40 / 4.40 (plans: subscription) | 2026-08-14 flagship. `glm-5.3-flash` (0.15/0.50, multimodal) and `glm-5.3-flashx` (0.37/1.25, high-speed) are selectable. GLM-5.2 remains listed but unstarred. |
| alibaba-cn | `qwen3.8-max`, `qwen3.8-flash`, `deepseek-v4.1-flash`, `kimi-k3` | 1,000,000 (Kimi 1,048,576) | 1.78/5.33, 0.12/0.40, 0.30/1.19, 2.83/14.13 | Qwen3.8 Max is the 2.4T MoE flagship (2026-08-03); Flash is the 1M vision tier; the endpoint also resells DeepSeek V4.1 Flash and Kimi K3. `qwen3.8-omni-flash` (0.11/0.38) is selectable. |
| alibaba-coding-plan-cn | `qwen3.7-plus` | 1,000,000 | subscription | The coding-plan catalog still tops out at Qwen3.7 Plus (2026-06-02); do not invent Qwen3.8 availability there. |
| moonshotai | `kimi-k3` | 1,048,576 | 3.00 / 15.00 | 2026-07-16 multimodal flagship. `kimi-k2.7-code` (0.95/4.00, 256K) is the coding-focused option, unstarred. |
| minimax | `MiniMax-M3` | 1,000,000 (override) | 0.30 / 1.20 | Newest stable flagship. The current snapshot reports the advertised 1M window, but `contextLimitOverrides` pins it so a future upstream regression to the 512K guaranteed minimum cannot shrink it. `MiniMax-M3.1-Flash-Preview` (2026-09-27, coding-plan catalog only) is a preview and stays unstarred. |
| xai (static) | `grok-4.7` | 500,000 | 2.00 / 6.00 | 2026-09-21. Static provider because it also carries `xai_oauth`. Grok 4.6 (2026-08-12) and Grok 4.5 (2026-07-08) stay selectable, all with 500K windows. |

## Additional supported providers (starred 2026-10-18)

Aggregators and resellers now carry the same curation as the headline vendors:
the newest affordable mainline flagship(s) each catalog actually serves are
starred; premium, preview, and beta tiers stay selectable but unstarred.

| Provider | Starred | Context | $/1M in / out | Notes |
| --- | --- | --- | --- | --- |
| openrouter | `openai/gpt-6.1-sol`, `anthropic/claude-sonnet-5.5`, `google/gemini-4-argon`, `deepseek/deepseek-v4.1-flash-1017` | 1,048,576–1,050,000 | 2.00/10.00, 2.00/10.00, 2.00/10.00, 0.15/0.60 | Aggregator (390 models): one current affordable mainline per major vendor. Premium variants (`openai/gpt-6-sol-pro`, `z-ai/glm-5.3-prime`) stay selectable, unstarred. |
| togetherai | `meta-models/Muse-Spark-Open-816B-A42B`, `deepseek-ai/DeepSeek-V4.1-Flash` | 1,048,576 | 1.25/4.25 and 0.30/1.20 | Newest large open model it serves (2026-10-15) plus the current DeepSeek flash lane. |
| siliconflow | `zai-org/GLM-5.3`, `deepseek-ai/DeepSeek-V4-Pro-0813`, `Qwen/Qwen3.8-2.4T-A95B` | 1,000,000–1,049,000 | 1.40/4.40, 1.32/3.96, 2.00/6.00 | The newest mainline flagship per vendor it serves. |
| ollama-cloud | `deepseek-v4.1-flash:1017`, `glm-5.3` | 1,048,576 | 0.15/0.60 and 1.40/4.40 | Cheapest current flash lane plus the current GLM flagship; `muse-spark-open` and `mistral-large-4` stay selectable. |
| groq | `qwen/qwen3.8-27b` | 131,042 | 0.80 / 4.00 | The only 2026 mainline refresh in its catalog; `openai/gpt-oss-120b` remains selectable. |
| mistral | `mistral-medium-2604` | 262,144 | 1.50 / 7.50 | `mistral-large-4` (2026-10-06) is still beta, so the newest stable reasoning tier is the mainline pick; `mistral-large-2512` (262K, 0.50/1.50) stays selectable. |
| xiaomi / xiaomi-token-plan-cn | `mimo-v2.6-pro`, `mimo-v2.6-flash` | 1,048,576 | 0.435/0.87 and 0.14/0.28 (plans: subscription) | 2026-09-22 refresh: pro flagship plus the affordable flash lane. |
| alibaba-token-plan / -cn | `qwen3.8-max`, `qwen3.8-flash`, `deepseek-v4.1-flash`, `glm-5.3` | 1,000,000 | subscription | Subscription-metered mirror of the `alibaba-cn` picks. Kimi K3 is not offered on the token plans, so `kimi-k2.7-code` stays selectable only. |
| tencent-coding-plan | `hunyuan-2.0-thinking` | 131,072 | subscription | Hunyuan line unchanged since 2026-03; the reasoning flagship is the newest model in the plan. |
| tencent-tokenhub | `hy3` | 256,000 | 0 / 0 | `hy4-preview` (2026-08-28, 1,024,000, 0.83/2.50) is still a preview, so the stable Hy3 is the mainline pick. |
| tencent-tokenhub-ep (static) | `glm-5.1`, `kimi-k2.6`, `minimax-m2.7`, `deepseek-v4-pro` | hand-written | — | Enterprise endpoint table; stars unchanged, but its retired DeepSeek V4 Flash entry was deleted directly. |
| github-copilot (static) | `gpt-4.1` | 128,000 | — | Conservative baseline only; managed Copilot accounts refresh the live catalog. |
| kimi-for-coding (static) | `k3` | 1,048,576 | subscription | Unchanged: K3 is still the newest plan flagship. |

## Retired models that must survive `go generate`

- `retiredModels` drops the DeepSeek V4 Flash line, which upstream retired on
  2026-10-17 when V4.1 Flash shipped: the direct catalog
  (`deepseek-v4-flash`, `deepseek-v4-flash-vision-exp`) plus every resold
  snapshot — Alibaba (direct and token plans), OpenRouter, Together AI,
  SiliconFlow, and Ollama Cloud. The static Tencent TokenHub Enterprise table
  drops its entry directly. `knownModels` no longer resolves the old ID, so the
  offline fallback cannot resurrect it.

## Registry corrections that must survive `go generate`

- `contextLimitOverrides` — `MiniMax-M3` 1M (pins the advertised window against
  an upstream regression to the 512K guaranteed minimum); OpenRouter
  `google/gemini-3.1-pro-preview-customtools` → 1,048,576 and `qwen/qwen3-14b` →
  131,072 (transposed digits upstream).
- `knownModels` — offline fallback table, refreshed alongside the starred set so
  a missing registry still resolves 1M windows and pricing for the current
  flagships.
- `registry_generated.go` is gitignored; `go generate ./internal/model/...`
  regenerates it from models.dev. Never edit it by hand.
