---
title: Release & CI
nav_order: 7
---

# Release & CI

jcode ships through two GitHub Actions workflows:

| Workflow | File | Trigger | Purpose |
| --- | --- | --- | --- |
| **CI** | [`.github/workflows/ci.yml`](https://github.com/cnjack/jcode/blob/main/.github/workflows/ci.yml) | every push to `main`, every pull request | Quality gate — Go `build`/`vet`/`test` + golangci-lint, web type-check + lint + build. |
| **Release** | [`.github/workflows/release.yml`](https://github.com/cnjack/jcode/blob/main/.github/workflows/release.yml) | push a `v*` tag (or manual `workflow_dispatch`) | Builds the CLI binaries **and** the desktop app bundles for every platform, then publishes them to a GitHub Release. |

## What gets released

A single `vX.Y.Z` tag produces, in one GitHub Release:

### CLI binaries

| OS | Architectures |
| --- | --- |
| Linux | `amd64`, `arm64` |
| macOS | `amd64` (Intel), `arm64` (Apple Silicon) |
| Windows | `amd64`, `arm64` |

Each binary ships with a `.sha256` checksum.

### Desktop app bundles

Built on native runners because Tauri emits OS-native installers. The Go binary is
compiled as the Tauri **sidecar** and embedded into the bundle (see
[Desktop App](desktop.html)).

| Platform | Runner | Target triple | Artifact |
| --- | --- | --- | --- |
| macOS Apple Silicon | `macos-latest` | `aarch64-apple-darwin` | `.dmg` |
| macOS Intel | `macos-15-intel` | `x86_64-apple-darwin` | `.dmg` |
| Windows x64 | `windows-latest` | `x86_64-pc-windows-msvc` | `.msi` + `.exe` (NSIS) |
| Linux x64 | `ubuntu-22.04` | `x86_64-unknown-linux-gnu` | `.deb` + `.AppImage` |

> Yes — the release covers **macOS (Intel + Apple Silicon), Windows, and Linux**.
> Each desktop bundle also ships with a `.sha256` checksum.

## Cutting a release

```bash
# Bump the version, then tag and push:
git tag v0.6.0
git push origin v0.6.0
```

Or trigger manually from the Actions tab → **Release** → *Run workflow*, supplying the
version (e.g. `v0.6.0`). The tag's version is written into the desktop bundle metadata
automatically, so `tauri.conf.json` does not need to be bumped by hand.

`alpha` / `beta` / `rc` tags are published as **pre-releases** automatically.

## macOS code signing & notarization

**The release builds without any signing secrets.** With nothing configured, the
macOS `.dmg` is produced **unsigned** — it works, but Gatekeeper warns on first launch
and users must right-click → **Open** (or run `xattr -dr com.apple.quarantine /Applications/jcode.app`).

To ship a **signed + notarized** app that launches cleanly, add the secrets below. A
dedicated `Set up macOS signing & notarization` step on the macOS runners resolves
them — importing the certificate into a temporary keychain and writing the API key to
disk — then `tauri build` signs with Developer ID and notarizes automatically. Add
every secret under **Repo → Settings → Secrets and variables → Actions → New repository secret**.

### Prerequisite

A paid **[Apple Developer Program](https://developer.apple.com/programs/)** membership
(USD $99/year). This is required to issue a *Developer ID Application* certificate and
to notarize — there is no free path to a Gatekeeper-clean macOS app.

### Part 1 — Signing certificate (always required)

Notarization can only run on an app that is **already signed** with a Developer ID
Application certificate, so these are needed no matter which notarization method you pick:

| Secret | What it is | How to get it |
| --- | --- | --- |
| `APPLE_CERTIFICATE` | Base64 of your **Developer ID Application** certificate exported as `.p12` | Steps below |
| `APPLE_CERTIFICATE_PASSWORD` | The password you set when exporting the `.p12` | You choose it at export time |
| `APPLE_SIGNING_IDENTITY` | _(optional)_ The certificate's full name, e.g. `Developer ID Application: Your Name (TEAMID)` — inferred from the cert if omitted | `security find-identity -v -p codesigning` |

1. Open **Keychain Access** → *Certificate Assistant* → *Request a Certificate From a Certificate Authority* and save the CSR to disk.
2. At [developer.apple.com/account/resources/certificates](https://developer.apple.com/account/resources/certificates) → **+** → choose **Developer ID Application** → upload the CSR → download the `.cer`, then double-click to install it into Keychain Access.
3. In Keychain Access, find the **Developer ID Application** entry, expand it to include its private key, right-click → **Export** → save as `certificate.p12` and set a password (this becomes `APPLE_CERTIFICATE_PASSWORD`).
4. Base64-encode the `.p12` for the secret value (single line — note the `-A`):
   ```bash
   openssl base64 -A -in certificate.p12 | pbcopy   # value for APPLE_CERTIFICATE
   ```

### Part 2 — Notarization (pick ONE method)

The workflow auto-selects whichever method's secrets you provide. **The App Store
Connect API key (Option A) is recommended for CI** — it uses a downloadable key file
rather than a personal Apple ID + password, and survives password/2FA changes.

#### Option A — App Store Connect API key (recommended)

| Secret | What it is | How to get it |
| --- | --- | --- |
| `APPLE_API_KEY_P8_BASE64` | Base64 of the **`.p8`** private-key file (this secret carries the actual key material) | Steps below |
| `APPLE_API_KEY_ID` | The **Key ID** string, e.g. `ABCD1234XY`, from the Key ID column | Steps below |
| `APPLE_API_ISSUER` | The **Issuer ID** (a UUID) shown above the keys table | Steps below |

1. In [App Store Connect](https://appstoreconnect.apple.com/access/integrations/api) → **Users and Access** → **Integrations** → **App Store Connect API** → **Team Keys** → **Generate API Key** (you must be Account Holder / Admin).
2. Name it and assign the **Developer** role (sufficient for notarization), then **Generate**.
3. On the new key's row click **Download API Key** — the **`.p8` file can be downloaded only once**. Save it.
4. Copy the **Issuer ID** (above the table → `APPLE_API_ISSUER`) and the **Key ID** (the row's `Key ID` column → `APPLE_API_KEY_ID`).
5. Base64-encode the `.p8` for the secret value:
   ```bash
   base64 -i AuthKey_ABCD1234XY.p8 | pbcopy     # macOS → value for APPLE_API_KEY_P8_BASE64
   # base64 -w0 AuthKey_ABCD1234XY.p8           # Linux equivalent
   ```

> **Naming gotcha:** Tauri's own `APPLE_API_KEY` env var holds the **Key ID** — not the
> file and not its contents. You don't set `APPLE_API_KEY` / `APPLE_API_KEY_PATH`
> directly; the workflow sets `APPLE_API_KEY` from your `APPLE_API_KEY_ID` secret,
> decodes `APPLE_API_KEY_P8_BASE64` to `$RUNNER_TEMP/AuthKey_<id>.p8`, and points
> `APPLE_API_KEY_PATH` at that file. You only manage the three secrets above.

#### Option B — Apple ID + app-specific password

| Secret | What it is | How to get it |
| --- | --- | --- |
| `APPLE_ID` | The Apple ID email used for notarization | Your developer-account email |
| `APPLE_PASSWORD` | An **app-specific password** (not your real Apple ID password) | [appleid.apple.com](https://appleid.apple.com) → Sign-In and Security → App-Specific Passwords → **+** |
| `APPLE_TEAM_ID` | Your 10-character Apple Team ID | [developer.apple.com/account](https://developer.apple.com/account) → Membership |

Once **Part 1** plus **one** option of **Part 2** are set, the next tagged release
produces a signed, notarized `.dmg` with no workflow changes needed. (Set only one
notarization method — if both are present the workflow uses Option A.)

## Windows signing (optional)

Windows `.msi` / `.exe` bundles are **unsigned** by default — they run, but SmartScreen
shows a publisher warning. To sign them you need a code-signing certificate (OV or EV)
and to wire it into the desktop job (e.g. via Tauri's `signCommand` or
`certificateThumbprint`). This is not configured yet; track it as future work if a
verified publisher is required.

## Linux

Linux `.deb` / `.AppImage` bundles are not code-signed (the platform has no equivalent
Gatekeeper gate). No secrets are required.

## Notes

- The desktop **auto-updater is not enabled**, so **no** `TAURI_SIGNING_PRIVATE_KEY` /
  `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` secrets are needed.
- If a single platform's desktop build fails, the matrix uses `fail-fast: false` so the
  others still publish; re-run the failed job afterward.

See also: [Desktop App](desktop.html).
