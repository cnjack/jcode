---
title: Cloud & configuration sync
nav_order: 6
---

# Cloud & configuration sync

jcode Cloud extends Desktop with browser and mobile access, explicit device
trust, encrypted configuration sync, and centrally managed Cloud Providers. It
does **not** replace local jcode: Desktop and CLI continue to work with local
Providers without a Cloud account.

## Two Provider lanes

The model selector can contain both Provider sources, but their credential and
request paths stay separate:

| Source | Credential owner | Request path | Works without Cloud |
| --- | --- | --- | --- |
| Local Provider | The current Desktop | Desktop → model API | Yes |
| Cloud Provider | Cloud Cluster or Project | Desktop → `cloud_proxy` → model API | No |

Local Provider configuration retains its existing `ProviderConfig` shape:
API key, base URL, custom headers, custom models and model preferences. Selecting
one never routes inference through Cloud.

Cloud Providers are created at Cluster or Project scope. Desktop downloads a
catalog containing the Provider ID, display name, canonical `kind`, scope and
model capabilities. The UI maps `kind` to the same Provider icon used for local
Providers. Unknown kinds receive a generic fallback icon. Provider secrets are
never included in this catalog.

A Cloud model is addressed internally as:

```text
cloud:<provider-id>/<model-id>
```

When selected, Desktop validates the catalog entry and calls its
OpenAI-compatible `cloud_proxy` URL using the current device token. Cloud resolves
the server-side credential and upstream model ID.

## Opt-in Desktop configuration sync

Configuration sync is disabled by default. Enable it in **Settings → Cloud**.
The first trusted Desktop creates a random Account Sync Key (ASK). ASK is stored
in the operating system keychain and is never uploaded in plaintext.

Provider envelopes include:

- API key and custom headers
- base URL and Provider metadata
- custom model definitions
- favorites, enabled/disabled models and reasoning-effort overrides

Each Provider is encrypted independently with authenticated encryption. Cloud
stores only ciphertext plus merge metadata (version, update timestamp and
tombstone state).

## Approving another Desktop

A Cloud login proves account access; it does not automatically grant access to
encrypted configuration.

1. The new Desktop registers its device identity public key and requests config
   access.
2. An already trusted Desktop shows the request in **Settings → Cloud**.
3. On approval, the trusted Desktop wraps ASK specifically to the new device
   public key.
4. The new Desktop unwraps ASK with its keychain-held private key and can then
   decrypt Provider envelopes.

Denying a request leaves the new Desktop logged in to Cloud but unable to read
the encrypted configuration.

## Merge and deletion behavior

Provider records use compare-and-swap versions. Deleting a synced local Provider
publishes an encrypted tombstone, so another Desktop does not resurrect it.
Concurrent edits that cannot be safely merged are rejected rather than silently
overwriting a local secret.

## Browser and mobile

Cloud browser and mobile clients can list connected Desktops, continue synced
conversations, send prompts, stop runs and respond to approvals. Commands are
bound to the registered device identity. They do not receive local Provider
credentials.

## Security boundaries

- Signing out of Cloud does not disable local Providers or local sessions.
- Cloud Provider secrets stay server-side and are used only by `cloud_proxy`.
- Synced local Provider secrets are encrypted on Desktop before upload.
- Device login approval and ASK/config approval are deliberately separate.
- Provider icons are selected from a canonical `kind`; image files and secret
  values are not synchronized as presentation data.

See [Desktop App](/docs/desktop) for the native sidecar architecture and
[Configuration](/docs/configuration) for local configuration fields.
