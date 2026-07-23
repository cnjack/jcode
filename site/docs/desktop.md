---
title: Desktop App
nav_order: 7
---

# Desktop App

jcode ships a native desktop application built with [Tauri](https://tauri.app). It
wraps the exact same web UI you get from `jcode web` in a real OS window and adds
native system integration — notifications, a menu-bar tray, single-instance
focus, window-state memory, and a native folder picker.

It is **not** a second implementation: the Go backend (with the web UI already
embedded) runs as a bundled *sidecar* process, and Tauri renders it. Everything
you can do in the browser UI works identically here.

Cloud is an optional extension. Local Providers still call model APIs directly
and Desktop remains fully usable while signed out. When enabled, Cloud adds
browser/mobile session access, trusted-device approval, Cloud-managed Providers,
and end-to-end encrypted configuration sync between Desktops. See
[Cloud & configuration sync](/docs/cloud).

## Architecture

```
┌─────────────────────────── jcode.app (Tauri) ───────────────────────────┐
│                                                                          │
│   Rust shell (src-tauri)                                                 │
│   ├─ picks a free loopback port                                          │
│   ├─ spawns the jcode sidecar:  jcode web --port N --host 127.0.0.1      │
│   ├─ health-polls the port, then navigates the window to it             │
│   ├─ system tray · single-instance · window-state · close-to-tray       │
│   └─ kills the sidecar on exit                                           │
│                                                                          │
│   WebView  ──HTTP/WS──▶  http://127.0.0.1:N   (the jcode Go server)      │
│      ▲                       ├─ embedded Vue web UI                       │
│      │                       ├─ REST  /api/*                             │
│   native APIs                └─ WebSocket  /ws                           │
│   (notification, opener,                                                 │
│    dialog) via Tauri IPC                                                 │
└──────────────────────────────────────────────────────────────────────────┘
```

Why a sidecar instead of rewriting the backend in Rust:

- **Zero divergence.** The desktop app and `jcode web` serve byte-for-byte the
  same frontend, REST API, and WebSocket stream. A fix in one is a fix in both.
- **Same-origin simplicity.** The Vue app's relative `/api` and `/ws` URLs just
  work because the WebView loads the server's own origin.
- **Native where it counts.** The frontend feature-detects Tauri and routes
  notifications, external links, and the folder picker through native plugins,
  falling back to web APIs in a plain browser.

The localhost origin is granted the Tauri JS APIs through a capability `remote`
entry (`http://localhost:*`, `http://127.0.0.1:*`) in
`desktop/src-tauri/capabilities/default.json`.

## Native capabilities

| Capability | What it does |
| --- | --- |
| **Notifications** | Native OS notifications on task-finished / approval-needed, fired only when the window isn't focused. Falls back to the web Notification API in the browser. |
| **System tray** | Menu-bar icon: left-click toggles the window; right-click → Show / Hide / Quit. |
| **Close-to-tray** | Closing the window hides it (the agent keeps running in the background). Quit from the tray or ⌘Q to exit for real. |
| **Global shortcut** | ⌘/⊞ + Shift + J toggles the window from anywhere. |
| **Single instance** | Launching jcode again focuses the existing window instead of starting a second copy. |
| **Window state** | Window size and position are remembered across launches. |
| **Native folder picker** | "Open folder" in the workspace switcher uses the OS dialog on desktop. |
| **External links** | Links open in the system browser via the opener plugin. |
| **Overlay title bar** | On macOS the traffic-light buttons sit in a slim draggable strip above the shell. |

## Security model

The desktop app keeps the jcode server running in the background (close-to-tray),
so the loopback server's exposure matters. The server drives an agent with shell
and file tools, so reaching its API equals running commands as you.

- **Loopback only.** The sidecar binds `127.0.0.1` — never LAN-reachable.
- **Random per-launch port.** Not a security control, but raises the bar.
- **Cross-origin gate.** The WebSocket handshake and the REST CORS layer reject
  requests whose `Origin` is a foreign website. Same-origin, loopback origins
  (covering the Vite dev proxy), and empty-Origin clients are allowed; a page on
  `https://evil.com` cannot open `ws://127.0.0.1:<port>` or drive the agent. See
  `isAllowedWebOrigin` in `internal/web/server.go` (unit-tested in `cors_test.go`).
- **Native APIs scoped to 127.0.0.1.** The Tauri capability grants notification /
  opener / dialog only to the loopback origin the app actually loads.

Hardening still worth doing (tracked as follow-ups): a per-launch **bearer token**
the sidecar hands the UI (to also stop *local* processes on a shared machine from
reaching the port), and a **Content-Security-Policy** response header from the Go
server as defense-in-depth around the markdown sink (already DOMPurify-sanitized).

## Building & running

Prerequisites: the [Rust toolchain](https://rustup.rs) plus the usual jcode web
toolchain (Go, Node, pnpm). On Linux you also need the Tauri system
dependencies (WebKitGTK etc. — see the [Tauri prerequisites](https://v2.tauri.app/start/prerequisites/)).

```bash
# Develop — opens the app with a hot window (rebuilds the sidecar first)
make desktop-dev

# Build a distributable bundle (.app/.dmg on macOS, .msi on Windows, …)
make desktop-build

# Regenerate app icons from the brand mark (web/public/icon.svg)
make desktop-icons
```

Under the hood `make desktop-sidecar` compiles the Go binary with the host
target-triple suffix Tauri expects
(`desktop/src-tauri/binaries/jcode-<target-triple>`, plus `.exe` on Windows) —
the web UI is embedded into that binary, so the bundle is self-contained. The
finished artifacts land in `desktop/src-tauri/target/release/bundle/`.

Always build through `make` — it rebuilds the sidecar first. Invoking
`pnpm tauri build` / `pnpm tauri dev` directly does **not** rebuild the sidecar
and will bundle whatever stale binary is in `binaries/`; run `make desktop-sidecar`
first if you must use the Tauri CLI directly. (In this headless build environment
the DMG step fails because it needs Finder/AppleScript; the `.app` itself builds
fine, and the DMG bundles normally on a desktop session.)

For CI/CD — how tagged releases build and publish these bundles for every platform,
and which macOS signing certificates/secrets are required — see [Release & CI](release.html).

## Layout

```
desktop/
├─ package.json              # drives the Tauri CLI
├─ splash/index.html         # shown while the sidecar boots
└─ src-tauri/
   ├─ tauri.conf.json        # window, bundle, externalBin (the sidecar)
   ├─ capabilities/default.json   # permissions + remote localhost grant
   ├─ binaries/              # jcode-<target-triple> sidecar (built by make)
   ├─ icons/                 # generated from web/public/icon.svg
   └─ src/
      ├─ main.rs             # plugins, tray, close-to-tray, sidecar cleanup
      ├─ sidecar.rs          # port pick · spawn · health-poll · navigate
      └─ tray.rs             # menu-bar icon + menu
```

The desktop bridge on the frontend side lives in
`web/src/lib/useDesktop.ts` — every export is feature-detected, so the
same web bundle runs unchanged in a browser and inside the desktop shell.

## Troubleshooting

- **Window never appears.** The shell waits for the sidecar to accept
  connections (up to ~60s) before showing the window; after that it shows the
  splash anyway. Check the sidecar's output — it is forwarded to the desktop log
  with a `[jcode]` prefix.
- **Native notifications don't fire.** Confirm OS notification permission was
  granted, and that the localhost origin is listed under `remote.urls` in the
  capability file (this is what enables Tauri JS APIs on the loopback origin).
- **Auto-update.** The updater plugin is intentionally not bundled yet: it
  panics at startup unless `plugins.updater` (endpoints + pubkey) is configured,
  which needs a signed release feed. To enable it, re-add `tauri-plugin-updater`
  (Cargo + `main.rs` + the `updater:default` capability), set
  `bundle.createUpdaterArtifacts: true`, and add the `plugins.updater` config.
