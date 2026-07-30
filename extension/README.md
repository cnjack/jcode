# jcode Browser Bridge (Chrome extension)

Lets jcode see and operate **your** Chrome — with your logins and sessions —
via the Chrome DevTools Protocol. This is the `extension` backend of jcode's
browser-use feature (the other backend is a managed Chrome jcode launches
itself; that one needs no extension). See
[`site/docs/overview/browser-use.md`](../site/docs/overview/browser-use.md).

The Chrome Web Store build has id `olkapiiikpfhaccmjphakolinkcggcbd`. The
unpacked development build has a separate fixed id
(`ekcnniaefmnhnemnpphikhgfoofnojnd`, pinned by the `key` field in
`manifest.json`) so native messaging works across reloads.

## Install

[Install jcode Browser Bridge from the Chrome Web Store](https://chromewebstore.google.com/detail/jcode-browser-bridge/olkapiiikpfhaccmjphakolinkcggcbd).

Then start jcode with browser use enabled (Settings → Browser → on) and continue
with **Auto-connect** below.

### Unpacked development build

1. Start jcode web/desktop.
2. Open `chrome://extensions` (or `edge://extensions`), enable **Developer mode**.
3. Click **Load unpacked** and select this `extension/` folder.

## Connect — Auto-connect

Make sure jcode is running with browser use enabled (Settings → Browser → on).
Click the extension's toolbar icon → **Auto-connect to jcode**.

It uses Chrome Native Messaging to find the running jcode app (even on a dynamic
desktop-app port), fetch the server URL + a token, and connect. No code, no URL,
and it self-heals when the app restarts on a new port.

- Requires the native-host manifest, which jcode **installs automatically** when
  it starts with browser use enabled (macOS/Linux: a file under the browser's
  `NativeMessagingHosts` dir; Windows: a registry key under HKCU). If
  Auto-connect reports the host is unavailable, start/restart jcode once with
  browser use enabled, then try again.

Auto-connect exchanges for a long-lived token in `chrome.storage.local`;
afterwards the extension reconnects silently — you connect once. Use
**Disconnect** in the popup to stop and forget the token.

## How it works

- The service worker (`background.js`) holds a websocket to
  `/api/browser/ext/ws` on the jcode server.
- jcode sends CDP commands over that socket; the worker relays them to the
  target tab with `chrome.debugger.sendCommand` and streams events back.
- jcode-controlled tabs are placed in a **"jcode 🔎"** tab group so you can see
  which tabs are under agent control. Detaching the debugger (or the Chrome
  "started debugging" bar → Cancel) hands control back — jcode stops.

## Permissions

- `debugger` — the CDP control channel (Chrome shows a banner while attached).
- `tabs`, `tabGroups` — create/switch/group tabs.
- `storage` — persist the server URL and pairing token.
- `host_permissions` limited to `127.0.0.1` / `localhost` — it only ever talks
  to your local jcode.

## Security

The bridge only connects to a loopback jcode server and authenticates with a
short-lived pairing code. Nothing is sent to any third party. Use the popup's
**Disconnect** to revoke the token and detach all tabs.
