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

jcode supports different models for different purposes:

| Role | Config Key | Purpose |
|---|---|---|
| **Primary** | `model` | Main model for agent interactions |
| **Small** | `small_model` | Lightweight model for summaries and context compaction |
| **Fallback** | `fallback_model` | Used when the primary model fails |

```json
{
  "model": "openai/gpt-4o",
  "small_model": "openai/gpt-4o-mini",
  "fallback_model": "anthropic/claude-3-5-sonnet"
}
```

## Add a Provider at Runtime

jcode includes a setup wizard. Run it from the TUI with `/setting` → "Add Model", or press Ctrl+L and select "Add new provider".

## Verify Model Connectivity

```bash
jcode doctor
```

This sends a test message to your configured model and reports any connection issues.
