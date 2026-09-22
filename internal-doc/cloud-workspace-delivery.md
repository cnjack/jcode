# Cloud remote workspace inspection and delivery

Companion contract: Cloud `docs/17-jcode-device-relay.md`, section 7.
Deploy the Cloud routes before distributing this connector.

The encrypted `workspace_actions` capability advertises `changes` and
`draft_pr`. The connector accepts these session-bound commands:

- `workspace.changes` → `GET /api/sessions/{id}/changes`.
- `workspace.draft_preview` → `GET /api/sessions/{id}/draft-pr/preview`.
- `workspace.draft_pr` → `POST /api/sessions/{id}/draft-pr`.

Every encrypted request includes `session_id`; it must match the routed ID.
The session must explicitly opt into Cloud sync. No handler falls back to the
foreground workspace. Responses contain paths and patches, so command ACKs
fail closed when encryption fails rather than uploading a plaintext fallback.

Changes are the current local Git workspace relative to HEAD, including staged,
unstaged and untracked files. The response is bounded to 100 files / 512 KiB;
incomplete previews carry no publishable revision. It is not an authorship claim.
The initial implementation supports local Git repository-root sessions. SSH,
Docker, missing workspaces and nested project directories fail with an explicit
recovery instruction rather than reading a different local directory.

Draft preview resolves the GitHub default branch with the device's authenticated
GitHub CLI, fetching the base commit if missing. It includes committed and
uncommitted differences against that base. Delivery binds `session_id`,
`repository_url`, `base_sha`, `revision`, selected `paths`, `title` and `body`.
The user reviews the complete diff and explicitly selects files before publishing.

Only selected patches are applied to a temporary Git index based on the reviewed
base. `commit-tree` and a deterministic branch preserve the current checkout,
original index and unrelated work. Push never forces and its URL must match the
reviewed GitHub repository. A failed push/PR creation retains the prepared branch
and returns a recoverable error. An unchanged retry reuses that branch and an
existing PR; success requires a validated provider-returned PR URL.

Tests use isolated HOME directories, real temporary Git repositories and bare
push targets, and an injected provider boundary. They cover inactive-session
isolation, sync opt-out, secret redaction, output bounds, unchanged checkout/index,
selected-only committed/uncommitted delivery, stale review and partial-push retry.
The Cloud browser acceptance additionally exercises actual encryption/pairing
and real device readback. OAuth/provider calls and public deployment acceptance
are tracked separately in Cloud's implementation log.
