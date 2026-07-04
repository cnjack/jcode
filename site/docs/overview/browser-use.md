---
title: Browser Use
parent: Overview
nav_order: 10
---

# Browser Use

jcode can **see and operate a real browser**: open pages, read them as text
snapshots, click, type, scroll, manage tabs — with every risky action gated by
the same approval flow as file edits and shell commands.

The first-class use case is the **local dev loop**: after the agent changes
your frontend, it opens `localhost`, reads the page, clicks through the flow,
and verifies its own work. General web operation (reading a PR, filling a
form, checking docs) is the second use case.

Page perception is **text-first**: the agent works from a compact
accessibility-tree snapshot where every interactive element carries a stable
`uid` it can act on. Screenshots exist as a visual fallback (and render inline
in the web UI chat), but they are not the primary channel.

## Two backends, one tool set

The same seven tools work over either backend — the model doesn't know or care
which one is active.

| Backend | What it is | Best for |
| --- | --- | --- |
| **Managed** (default) | jcode launches its own Chrome/Chromium with an isolated profile under `~/.jcode/browser/`. No extension needed, nothing touches your daily browser. | localhost verification, scraping public pages, anything that doesn't need your logins |
| **Extension** | The **jcode Browser Bridge** Chrome extension connects *your* Chrome to jcode over a local WebSocket. The agent works in your real browser — with your sessions and logins. | tasks on sites you're signed into, taking over a tab you already have open |

With `"backend": "auto"` (the default) jcode prefers the extension when it is
connected and falls back to the managed browser otherwise.

Tabs the agent controls in your Chrome are grouped under a **"jcode 🔎"** tab
group so you always see what's under agent control. Detach the debugger (or
click the popup's Disconnect) and control returns to you immediately.

## Installing the extension

> [!IMPORTANT]
> The extension is **not yet on the Chrome Web Store** — the store listing is
> in review. For now it must be loaded manually in developer mode from the
> `extension/` folder of the repository. The extension has a fixed ID, so a
> manual install behaves identically to a store install (and will be replaced
> by it once published).

1. Get the `extension/` folder — clone the repo or download it from GitHub.
2. Open `chrome://extensions` (or `edge://extensions`) and enable
   **Developer mode** (toggle in the top-right corner).
3. Click **Load unpacked** and select the `extension/` folder.
4. Start jcode with browser use enabled (Settings → Browser → on, or
   `/browser on` in the TUI).
5. Click the extension's toolbar icon → **Auto-connect to jcode**. It finds
   the running jcode app via native messaging — even on a dynamic desktop-app
   port — and connects. You connect once; afterwards it reconnects silently.

The extension only ever talks to your local jcode (`host_permissions` are
limited to `127.0.0.1` / `localhost`) and sends nothing to any third party.

## The tools

| Tool | What it does |
| --- | --- |
| `browser_open` | Navigate to a URL (returns the title plus a snapshot header, saving a round trip) |
| `browser_snapshot` | Text snapshot of the page — uid-tagged interactive elements; the primary way to "see" |
| `browser_screenshot` | PNG screenshot; renders inline in the web UI chat, injected as an image for vision models |
| `browser_act` | One interaction: click / fill / press / hover / scroll / select on a uid from the latest snapshot |
| `browser_read` | Read page text, console output, or network activity |
| `browser_tabs` | List / open / select / close tabs; `claim` takes over one of your tabs (extension backend) |
| `browser_eval` | Evaluate a JavaScript expression — requires developer mode |

## Approvals

Browser actions plug into the standard jcode approval flow, in three tiers:

- **Read-only — no approval.** Snapshots, screenshots, reading text/console/
  network, listing tabs.
- **Interaction — ask once per site.** Navigation and page actions prompt the
  first time for each origin; you can answer *just this once* or *always allow
  this site*. Site permissions are stored in config and manageable from the
  web UI. Full-access mode auto-approves this tier.
- **High-risk — always ask.** `browser_eval` additionally requires
  **developer mode** to be enabled in the browser settings, and destructive or
  outward-facing actions (submitting forms with side effects, payments,
  sending messages) always prompt regardless of site permissions or mode.

In **Plan mode** the browser is read-only: the agent can look but not touch.

## Using it

- **TUI** — `/browser` shows status (backend, Chrome discovery, extension
  connection); `/browser on` / `/browser off` toggles the capability.
- **Web / Desktop** — Settings → **Browser**: enable, pick a backend, manage
  site permissions, and toggle developer mode. Screenshots taken by the agent
  appear inline in the chat.

## Configuration

```json
{
  "browser": {
    "enabled": true,
    "backend": "auto",
    "chrome_path": "",
    "headless": false,
    "viewport": "1280x720",
    "approval": { "navigate": "ask", "interact": "ask" },
    "site_permissions": [
      { "origin": "https://github.com", "navigate": "allow", "interact": "allow" }
    ],
    "dev_mode": false
  }
}
```

| Field | Meaning |
| --- | --- |
| `backend` | `auto` (extension if connected, else managed), `managed`, or `extension` |
| `chrome_path` | Path to a Chrome/Chromium binary; empty = auto-discover |
| `headless` | Run the managed browser without a visible window |
| `approval` | Per-class defaults: `navigate` / `interact` → `ask` or `always_allow` |
| `site_permissions` | Per-origin overrides (`ask` / `allow`) |
| `dev_mode` | Unlocks `browser_eval` (high-risk; off by default) |
