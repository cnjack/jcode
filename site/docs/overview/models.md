---
title: Model Providers & Models
parent: Overview
nav_order: 3
---

# Model Providers & Models

jcode works with any OpenAI-compatible API. Configure multiple providers and switch between models mid-session.

## Supported Providers

Any provider that implements the OpenAI chat completion API is supported. Common options include:

| Provider | Base URL | Notes |
|---|---|---|
| OpenAI | `https://api.openai.com/v1` | Default if no base URL specified |
| Anthropic | Via compatible proxy | Use a provider that exposes OpenAI-compatible API |
| Azure OpenAI | Your Azure endpoint | Set `base_url` to your Azure endpoint |
| Local models | `http://localhost:PORT` | Ollama, LM Studio, vLLM, etc. |
| Any compatible API | Custom `base_url` | Any server implementing the chat completion protocol |

## Configure a Provider

Add a provider to `~/.jcode/config.json`:

```json
{
  "providers": {
    "openai": {
      "api_key": "sk-...",
      "base_url": "https://api.openai.com/v1"
    },
    "local": {
      "api_key": "not-needed",
      "base_url": "http://localhost:11434/v1"
    }
  },
  "model": "openai/gpt-4o"
}
```

The `model` field uses the format `"provider/model"`. For example:
- `"openai/gpt-4o"` — GPT-4o via OpenAI
- `"anthropic/claude-3-5-sonnet"` — Claude via Anthropic
- `"local/llama3"` — A local model via Ollama

{: .note }
The model registry is auto-generated from [models.dev](https://models.dev) at build time. If your model isn't listed, you can still use it by specifying the provider and model name.

## Switch Models Mid-Session

Press **Ctrl+L** in the TUI or type `/model` to open the model picker. You can switch models without restarting your session.

```
  ┌──────── Model Picker ────────┐
  │  > openai / gpt-4o           │
  │    openai / gpt-4o-mini      │
  │    anthropic / claude-3.5    │
  └──────────────────────────────┘
```

## Special Model Roles

jcode supports two model roles:

| Role | Config Key | Purpose |
|---|---|---|
| **Primary** | `model` | Main model for agent interactions, compaction, and memory distillation |
| **Small** | `small_model` | Optional lightweight model for cheap side work |

```json
{
  "model": "openai/gpt-4o",
  "small_model": "openai/gpt-4o-mini"
}
```

In the web UI and desktop app, set the small model from **Settings → Providers →
Model roles** — changes apply immediately, no restart needed.

When `small_model` is set, it powers:

- **Subagent delegation** — the `subagent` tool accepts `"small"` as its
  `model` value, and the main model is nudged to use it for mechanical,
  low-stakes subtasks (targeted searches, file inventories, simple
  extraction). Complex reasoning and code-writing subagents stay on the
  parent model, and any subagent can still pin an explicit `"provider/model"`.
  The same `"small"` alias works in workflow specs, `team_spawn`, and the
  automation model override — anywhere a model ref is accepted.
- **Session titles** — instead of truncating your first message, jcode asks
  the small model for a concise title (async and best-effort; failures keep
  the truncated title).

Everything quality-critical — the main loop, context compaction, memory
distillation — deliberately stays on the primary model: a lossy summary or a
bad long-term memory costs more than the tokens it saves. When `small_model`
is unset nothing changes: subagents inherit the parent model and titles stay
truncated. `jcode doctor` probes small-model connectivity alongside the
primary model.

## Reasoning & Extended Thinking

For reasoning-capable models, jcode can control thinking depth via the OpenAI-compatible `reasoning_effort` parameter. Set it per provider in `~/.jcode/config.json`:

```json
{
  "providers": {
    "openai": {
      "api_key": "sk-...",
      "reasoning_effort": "medium"
    }
  }
}
```

Accepted values are `"low"`, `"medium"`, and `"high"`. An empty string (or omitting the key) sends no effort parameter. The value is forwarded on every request; reasoning models honor it and others ignore it.

For gateways that gate reasoning behind a chat-template flag (for example qwen3), set `thinking` to explicitly toggle extended reasoning. It is sent as the `chat_template_kwargs` `{"enable_thinking": <bool>}` extension:

```json
{
  "providers": {
    "my-gateway": {
      "api_key": "...",
      "base_url": "https://gateway.example.com/v1",
      "thinking": true
    }
  }
}
```

{: .note }
Reasoning effort can also be chosen **per model** from the chat model picker. That choice is stored in `~/.jcode/model_state.json` (`effort_overrides`) and takes precedence over the provider-level `reasoning_effort`. It is applied consistently across the TUI, web, and ACP.

## Add a Provider at Runtime

jcode includes a setup wizard. Run it from the TUI with `/setting` → "Add Model", or press Ctrl+L and select "Add new provider".

In the web UI, providers and models are managed from a card-based **Settings** view: each provider is a card showing its brand, name, base URL, and a catalog of its models (built-in registry models toggle show/hide; custom models are editable or removable). Editing a provider or authoring a custom model opens a dedicated dialog. A custom model's editor exposes its ID, display name, context window, image-input toggle, and a reasoning-effort tier editor — when a custom model is flagged as reasoning, the standard `minimal` / `low` / `medium` / `high` effort levels are offered, or you can define your own tiers. Models advertising effort levels then expose the per-model reasoning-effort control in the chat input.

## Verify Model Connectivity

```bash
jcode doctor
```

This sends a test message to your configured model and reports any connection issues.
