---
title: Channels
parent: Overview
nav_order: 14
---

# Channels

Channels let jcode send push notifications to your messaging apps. When the agent needs your attention — approval required, task completed, or task failed — you get a message on your phone instead of having to watch the terminal.

Currently supported channels:

| Channel | Protocol | Status |
|---|---|---|
| **WeChat** | [iLink Bot API](https://ilinkai.weixin.qq.com) | Supported |
| **JCode Buddy** | BLE (Nordic UART Service) | Supported |

## How It Works

1. **Connect** — Scan a QR code to link your WeChat account
2. **Work normally** — Use jcode in the TUI or web interface
3. **Get notified** — When the agent waits for approval or finishes a task, you receive a WeChat message
4. **Send messages** — You can also send prompts to jcode directly from WeChat

Channels are a notification sidecar — they don't replace the TUI or web interface. The agent still runs locally; channels just let you step away without missing anything.

## WeChat Setup

### TUI (Terminal)

1. Open jcode and type `/channel`
2. Select **WeChat** → **Login**
3. Scan the QR code with your WeChat app
4. The channel auto-enables after a successful scan

```
You › /channel

  ┌─────────────────────────────┐
  │  📱 Channels                │
  │                             │
  │  WeChat     Not connected   │
  │  ─────────────────────────  │
  │  [L] Login   [D] Disable    │
  └─────────────────────────────┘
```

### Web Interface

1. Open **Settings** → **Channels** tab
2. Click **Connect** and scan the QR code with WeChat
3. After scanning, the channel is ready to use

Once connected, a **WeChat toggle** appears in the input toolbar (next to the Auto toggle). Use it to quickly enable or disable notifications without opening Settings.

To disconnect entirely, go back to **Settings** → **Channels** → **Disconnect**.

### Auto-Enable on Startup

If you've previously logged in, jcode remembers your credentials. On the next launch:

- **TUI mode** — The channel auto-enables automatically. No configuration needed.
- **Web mode** — Add `"channel": { "web_enabled": true }` to your config to auto-enable on startup. Without this, you can still enable manually via the toolbar toggle.

## Notifications

When the channel is enabled, you receive WeChat notifications for:

| Event | Notification |
|---|---|
| **Approval needed** | Tool name + arguments (sent after 10 seconds of no response) |
| **Task completed** | Summary of the agent's output |
| **Task failed** | Error message |
| **Session started** | Time-aware welcome message (TUI only) |
| **Session ended** | Time-aware goodbye message |

### Message Format

```
⏳ Approval Needed
————————————————
Tool: execute
Args: npm test
————————————————
Please return to terminal
```

```
✅ Task Completed
————————————————
All 42 tests passing. Updated the
README with the new API documentation.
```

### Time-Aware Messages

Welcome and goodbye messages adapt to the time of day:

| Time | Greeting |
|---|---|
| Before 6am | 🌙 Burning the midnight oil? |
| 6am–12pm | 🌅/☀️ Good morning! |
| 12pm–6pm | ☀️ Good afternoon! |
| 6pm–10pm | 🌆 Good evening! |
| After 10pm | 🌙 Working late? |

Weekend messages include a casual "Enjoy your weekend!" touch.

## Sending Messages from WeChat

You can send prompts to jcode directly from WeChat. Your message is submitted to the agent just as if you typed it in the TUI or web interface.

- In **web mode**, inbound WeChat messages appear in the web UI with a green **WeChat** label, so you can tell them apart from locally typed prompts.
- If the agent is currently busy, you'll receive an immediate reply letting you know your message has been queued.

When the channel is disabled, inbound messages are silently ignored.

## JCode Buddy (BLE Device)

JCode Buddy is a physical desktop companion — an ESP32-based gadget with a pixel-art cat that reacts to your coding activity in real time. It connects to your computer over Bluetooth Low Energy (BLE) and displays live status updates from jcode.

### What You See

The buddy shows a pixel cat with different animations depending on what jcode is doing:

| jcode Status | Buddy Animation |
|---|---|
| **Idle** | Cat sits calmly, tail wagging |
| **Thinking / Working** | Cat walks with purpose |
| **Approval needed** | Cat lunges forward, alert |
| **Task complete** | Cat bounces happily with hearts 🎉 |
| **Disconnected** | Cat sleeps (Zzz…) |

Recent status messages scroll beside the cat so you can see what's happening at a glance.

### Setup

1. **Enable BLE** in your jcode config:
   ```json
   {
     "channel": {
       "ble_enabled": true
     }
   }
   ```

2. **Power on** your JCode Buddy device

3. **That's it** — jcode automatically discovers nearby devices named `JCODE-*` and connects over BLE. No pairing code needed.

The connection is lazy: jcode only starts scanning when you send your first message. Once connected, it stays connected until the device is turned off or goes out of range.

### Voice Input

If your buddy device has a microphone, you can press the record button on the device to speak a prompt. The audio is transcribed in real-time and sent to jcode as if you typed it — perfect for quick commands without touching the keyboard.

### Configuration

| Setting | Default | Description |
|---|---|---|
| `channel.ble_enabled` | `false` | Enable BLE device auto-discovery and notifications |

BLE is off by default to avoid unnecessary Bluetooth scanning. Turn it on only if you have a JCode Buddy device.

## Configuration

Add to `~/.jcode/config.json`:

```json
{
  "channel": {
    "web_enabled": true,
    "ble_enabled": false
  }
}
```

| Setting | Effect |
|---|---|
| `channel.web_enabled` | Auto-enable WeChat on `jcode web` startup (if already logged in) |
| `channel.ble_enabled` | Enable BLE auto-discovery and status notifications for JCode Buddy |

This setting only controls auto-enable. You can always connect and toggle the channel manually through the UI, even without this config.

In TUI mode, no configuration is needed — channels are always available via `/channel`.

### Credential Storage

WeChat credentials are stored at `~/.jcode/channel/wechat.json` and created automatically after the first successful login. Delete this file to force a re-login.
