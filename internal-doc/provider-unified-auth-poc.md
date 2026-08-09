# Unified provider authentication POC

Status: accepted for implementation
Date: 2026-08-09

## Goal

Prove that JCode can bind a Provider to a managed account instead of copying a
short-lived access token into `config.json`. The POC covers the three managed
login methods already implemented by cc-switch:

| JCode login | Device authorization | Runtime API |
| --- | --- | --- |
| ChatGPT / Codex | `auth.openai.com/api/accounts/deviceauth/*`, then OAuth code exchange | OpenAI Responses at `chatgpt.com/backend-api/codex/responses` |
| Grok / xAI | OIDC discovery plus OAuth 2.0 Device Authorization Grant | OpenAI Responses at `api.x.ai/v1/responses` |
| GitHub Copilot | GitHub device flow, then GitHub-to-Copilot token exchange | Account catalog selects `/v1/responses` for OpenAI models and `/chat/completions` for other vendors |

API-key authentication remains supported and is the default for existing
configuration. Managed login is opt-in and backward compatible.

## Evidence copied from cc-switch

The implementation contract is derived from the local checkout at
`/Users/jack/workpath/opensource/cc-switch`, not from a generic OAuth
assumption:

- Provider configuration stores only an account binding; token lookup happens
  immediately before each upstream request.
- Access tokens are memory-only. Refresh tokens (or the long-lived GitHub
  token for Copilot) are durable, owner-only secrets.
- Account lists and the default account are shared across Providers. A Provider
  may bind a specific account or follow the current default.
- Refresh is serialized per account and checked again after taking the lock.
- An invalid refresh marks the account as requiring reauthentication. It never
  falls back to an API key or another Provider silently.
- Managed authentication pins the upstream origin, wire protocol and protected
  headers. User-supplied headers cannot replace them.

## POC boundaries

The automated POC is executable through unit and HTTP-handler tests without a
developer account and never sends a live billable model request.

The test transport substitutes local servers for each fixed remote endpoint
and proves:

1. start returns a public verification URL, user code, bounded expiry and a
   random opaque flow ID; the upstream device token is never returned;
2. poll maps pending, slow-down, denied, expired and success responses into one
   public state machine;
3. successful login persists the durable credential before publishing an
   in-memory access token;
4. concurrent inference requests perform at most one refresh per account;
5. refresh-token rotation uses compare-and-swap semantics and invalid refresh
   persists `requires_reauth`;
6. a Provider binding contains only `method` and optional `account_id`;
7. model requests resolve a fresh credential and inject protected headers at
   dispatch time;
8. ChatGPT/xAI chat requests select Responses. Copilot resolves the live model
   vendor per account, using Responses for OpenAI models and Chat Completions
   for other advertised vendors;
9. public flow responses, Provider bindings, durable-state fixtures and runtime
   credential serialization expose no access token, refresh token, GitHub
   token, authorization code or device token;
10. the durable store is `0700`/`0600`, written through fsync plus atomic
    rename, and mutations are guarded across processes.
11. redirects cannot forward a managed model body, bearer token, or protected
    header to a second origin;
12. cancel and logout are linearized with authorization commit, including
    flows owned by a second manager process, and flow capacity is reserved
    before any authorization-server request;
13. Copilot user, tool-continuation, and subagent requests receive the correct
    initiator/interaction headers while one session keeps a stable opaque
    interaction ID.
14. managed model catalogs are fetched with the selected account credential,
    bounded to 1 MiB, filtered by provider visibility, and never expose an
    upstream response body on error. Explicitly enabled live chat models retain
    their wire protocol across restart;
15. xAI image and video entries are separated from chat. The official managed
    xAI image profile pins `api.x.ai/v1/images/generations`, resolves its token
    only at dispatch, and reuses the billable-operation approval, journal,
    quota, Artifact, and safe-download pipeline.

## Accepted result

The POC is accepted when the focused auth, catalog, model transport and provider
API tests pass without network access. A manual account-scoped smoke test also
confirmed that xAI and GitHub Copilot return their live model catalogs, and that
the connected xAI account can read the official image-generation catalog. No
billable inference or image-generation request is part of that smoke test.
