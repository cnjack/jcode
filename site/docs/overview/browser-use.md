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

The same six core tools work over either backend — the model doesn't know or
care which one is active. Developer mode adds the optional `browser_eval` tool.

| Backend | What it is | Best for |
| --- | --- | --- |
| **Managed** | jcode launches its own Chrome/Chromium with an isolated profile under `~/.jcode/browser/`. No extension needed, nothing touches your daily browser. | localhost verification, scraping public pages, anything that doesn't need your logins |
| **Extension** | The **jcode Browser Bridge** Chrome extension connects *your* Chrome to jcode over a local WebSocket. The agent works in your real browser — with your sessions and logins. | tasks on sites you're signed into, taking over a tab you already have open |

With `"backend": "auto"` (the default) jcode prefers the extension when it is
connected and falls back to the managed browser otherwise.

Tabs the agent controls in your Chrome are grouped under a **"jcode 🔎"** tab
group so you always see what's under agent control. Detach the debugger (or
click the popup's Disconnect) and control returns to you immediately.

## Set up the extension

> [!TIP]
> [Install **jcode Browser Bridge** from the Chrome Web Store](https://chromewebstore.google.com/detail/jcode-browser-bridge/olkapiiikpfhaccmjphakolinkcggcbd).

1. Start jcode, then enable browser use in **Settings → Browser** or run
   `/browser on` in the TUI.
2. Install **jcode Browser Bridge** from the Chrome Web Store. Pin it to the
   toolbar if you want the connection control to stay visible.
3. Click the extension icon, then click **Auto-connect to jcode**.
4. Confirm that **Settings → Browser → Extension** says **Connected**, or run
   `/browser` in the TUI.

The extension uses native messaging to find the running jcode app, including a
desktop app using a dynamic port. The first connection stores a local pairing
token; after that, the extension reconnects silently when jcode starts. Click
**Disconnect** in the extension popup to revoke the token and detach every
controlled tab.

### Development install

When testing an unpublished extension change, load the repository copy:

1. Open `chrome://extensions` (or `edge://extensions`) and enable
   **Developer mode**.
2. Click **Load unpacked** and select the repository's `extension/` folder.
3. Start or restart jcode with browser use enabled, then click the extension's
   **Auto-connect to jcode** button.

The extension only ever talks to your local jcode (`host_permissions` are
limited to `127.0.0.1` / `localhost`) and sends nothing to any third party.

### If it does not connect

- Make sure jcode is running and browser use is enabled.
- If the popup says the native host is unavailable, restart jcode once with
  browser use enabled. jcode installs the native-messaging registration at
  startup.
- In the extension's site-access settings, allow access to `127.0.0.1` and
  `localhost`, reload the extension, then try **Auto-connect to jcode** again.
- Run `/browser` or open **Settings → Browser** to check whether the extension
  is online.

## The tools

| Tool | What it does |
| --- | --- |
| `browser_open` | Navigate to a URL (returns the title plus a snapshot header, saving a round trip) |
| `browser_snapshot` | Text snapshot of the page — uid-tagged interactive elements; the primary way to "see" |
| `browser_screenshot` | PNG screenshot; renders inline in the web UI chat, injected as an image for vision models |
| `browser_act` | One interaction: click / fill / press / hover / scroll / select on a uid from the latest snapshot |
| `browser_read` | Read visible page text, bounded by a configurable character limit |
| `browser_tabs` | List / open / select / close tabs; `claim` takes over one of your tabs (extension backend) |
| `browser_eval` | Evaluate a JavaScript expression — requires developer mode |

## Approvals

Browser actions plug into the standard jcode approval flow, in three tiers:

- **Read-only — no approval.** Snapshots, screenshots, reading visible page
  text, and listing tabs.
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
| `enabled` | Enable browser tools. Off by default; turn it on in Settings or with `/browser on` |
| `backend` | `auto` (extension if connected, else managed), `managed`, or `extension` |
| `chrome_path` | Path to a Chrome/Chromium binary; empty = auto-discover |
| `headless` | Run the managed browser without a visible window |
| `approval` | Per-class defaults: `navigate` / `interact` → `ask` or `always_allow` |
| `site_permissions` | Per-origin overrides (`ask` / `allow`) |
| `dev_mode` | Unlocks `browser_eval` (high-risk; off by default) |
