---
title: Model Providers & Models
parent: Overview
nav_order: 3
---

# Model Providers & Models

jcode works with OpenAI-compatible APIs and with managed account transports for ChatGPT/Codex, xAI/Grok, and GitHub Copilot. Configure multiple providers and switch between models mid-session.

## Supported Providers

Any provider that implements the OpenAI chat completion API is supported. Common options include:

| Provider | Base URL | Notes |
|---|---|---|
| OpenAI | `https://api.openai.com/v1` | Default if no base URL specified |
| ChatGPT / Codex | Managed by jcode | Device-code sign-in; uses the ChatGPT Codex Responses transport |
| xAI / Grok | `https://api.x.ai/v1` | API key or Grok device-code sign-in |
| GitHub Copilot | Managed by jcode | GitHub.com device-code sign-in; account-scoped catalog with model-specific Responses or Chat Completions transport |
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

## Provider Authentication

In Web or Desktop, open **Settings → Providers**, add or edit a supported
Provider, then choose its Authentication method:

| Provider | Authentication choices |
|---|---|
| OpenAI | API key, or **Sign in with ChatGPT** |
| xAI | API key, or **Sign in with Grok** |
| GitHub Copilot | **Sign in with GitHub** |
| Custom OpenAI-compatible endpoint | API key |

For managed sign-in, jcode shows a short-lived device code, opens the provider's
verification page, and polls until authorization finishes. You can keep
multiple accounts, choose a default, pin a Provider to a specific account,
remove one account, or sign out all accounts for that authentication method.
Providers that need reauthentication fail closed and show a reauthenticate
action instead of sending a stale token.

Managed account bindings are non-secret and look like this in
`~/.jcode/config.json`:

```json
{
  "providers": {
    "openai": {
      "auth": { "method": "codex_oauth" }
    },
    "xai": {
      "auth": { "method": "xai_oauth", "account_id": "optional-account-id" }
    },
    "github-copilot": {
      "auth": { "method": "github_copilot" }
    }
  }
}
```

An omitted `account_id` follows the current default account. Durable refresh or
GitHub credentials are stored separately in `~/.jcode/provider-auth.json` with
owner-only permissions. Access tokens are resolved immediately before each
request and are never returned to the UI. Managed transports also pin their
runtime URL, protocol, and protected headers, so custom base URLs and headers
cannot redirect or replace their authorization.

Managed Providers load the model catalog for the selected account instead of
assuming that every subscription exposes the same models. In Settings, use the
refresh action to reload that catalog, then enable the models you want in the
chat picker. After the first successful load, Settings keeps an account-scoped
local catalog cache: reopening the page shows it immediately while a background
request fetches newer results. A transient refresh failure leaves the cached
list visible, and changing the bound/default account uses a separate cache.
Enabled live models are retained locally for restart continuity. GitHub Copilot
may expose OpenAI, Google, Microsoft, and other vendor models in one account;
jcode preserves the wire protocol advertised for each enabled model.

{: .note }
**Sign in with ChatGPT** is the ChatGPT/Codex subscription transport, not a
general OpenAI API OAuth flow. API-key billing and ChatGPT subscription access
remain separate. GitHub Enterprise Server is not supported by the initial
GitHub Copilot integration.

{: .note }
Custom image endpoints still use their Provider's API key and cannot be attached
to an arbitrary managed-login Provider. Grok login is the explicit exception:
the official xAI profile exposes `grok-imagine-image` and
`grok-imagine-image-quality` as Image Model choices, pins the xAI Images API,
and resolves the selected account token only when a generation request is
dispatched. Video models are recognized but are not yet exposed because jcode
does not yet implement the asynchronous video workflow.

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

jcode supports three model roles:

| Role | Config Key | Purpose |
|---|---|---|
| **Primary** | `model` | Main model for agent interactions, compaction, and memory distillation |
| **Small** | `small_model` | Optional lightweight model for cheap side work |
| **Image** | `image_model` | Optional image-generation model used by the billable `generate_image` tool |

```json
{
  "model": "openai/gpt-4o",
  "small_model": "openai/gpt-4o-mini",
  "image_model": "xai/grok-imagine-image-quality"
}
```

In the web UI and desktop app, set the small and image models from **Settings →
Providers → Model roles** — changes apply immediately, no restart needed.

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

In the web UI, providers and models are managed from a card-based **Settings** view: each provider is a card showing its brand, authentication status, name, endpoint, and a catalog of its models (built-in and account-scoped models toggle show/hide; custom models are editable or removable). Refresh reloads the selected managed account's live catalog. Editing a provider keeps API-key and managed-account authentication in the same form. A custom model's editor exposes its ID, display name, context window, image-input toggle, and a reasoning-effort tier editor — when a custom model is flagged as reasoning, the standard `minimal` / `low` / `medium` / `high` effort levels are offered, or you can define your own tiers. Models advertising effort levels then expose the per-model reasoning-effort control in the chat input.

## Verify Model Connectivity

```bash
jcode doctor
```

This sends a test message to your configured model and reports any connection issues.
