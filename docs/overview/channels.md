---
title: Channels
parent: Overview
nav_order: 13
---

# Channels

Channels let jcode send push notifications to your messaging apps. When the agent needs your attention — approval required, task completed, or task failed — you get a message on your phone instead of having to watch the terminal.

Currently supported channels:

| Channel | Protocol | Status |
|---|---|---|
| **WeChat** | [iLink Bot API](https://ilinkai.weixin.qq.com) | Supported |

## How It Works

1. **Connect** — Scan a QR code to link your WeChat account
2. **Work normally** — Use jcode in the TUI or web interface
3. **Get notified** — When the agent waits for approval or finishes a task, you receive a WeChat message
4. **Respond** — Return to the terminal or web UI to approve, reject, or continue

Channels are a notification sidecar — they don't replace the TUI or web interface. The agent still runs in your terminal; channels just let you step away without missing anything.

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

1. Open Settings (⌘/Ctrl+,)
2. Go to the **Channels** tab
3. Click **Connect** to see the QR code
4. Scan with WeChat
5. Use **Disable** / **Enable** / **Disconnect** to manage the connection

### Auto-Enable on Startup

If you've previously logged in, jcode remembers your credentials (stored at `~/.jcode/wechat.json`). On the next launch, the channel auto-enables without scanning again.

## Notifications

### When You Get Notified

| Event | Notification |
|---|---|
| **Approval needed** | Tool name + arguments, sent after 10 seconds of no response |
| **Task completed** | Summary of the agent's last output |
| **Task failed** | Error message |
| **Session started** | Welcome message (time-aware) |
| **Session ended** | Goodbye message (time-aware) |

### Message Format

Messages use a title + divider + body format for readability:

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

Welcome and goodbye messages adapt to the time of day and day of week:

| Time | Greeting |
|---|---|
| Before 6am | 🌙 Burning the midnight oil? |
| 6am–12pm | 🌅/☀️ Good morning! |
| 12pm–6pm | ☀️ Good afternoon! |
| 6pm–10pm | 🌆 Good evening! |
| After 10pm | 🌙 Working late? |

Weekend messages include a casual "Enjoy your weekend!" touch.

## Inbound Messages

You can also send messages *to* jcode from WeChat. Messages are queued as prompts and processed in order.

If the agent is currently running a task, you'll receive an immediate reply:

```
⏳ Task In Progress
————————————————
A task is currently running.
Your message has been queued and
will be processed after the current
task completes.
```

Once the current task finishes, your message is processed automatically.

## Configuration

### Web Mode

To enable channels in `jcode web`, add to `~/.jcode/config.json`:

```json
{
  "channel": {
    "web_enabled": true
  }
}
```

### TUI Mode

Channels are always available in TUI mode. No extra configuration is needed — just use `/channel` to connect.

### Credential Storage

WeChat credentials are stored at `~/.jcode/wechat.json`. This file is created automatically after the first successful login. Delete it to force a re-login.

## Architecture

```
┌───────────┐     ┌──────────────────┐     ┌───────────────┐
│  TUI/Web  │────▶│ NotifyingHandler │────▶│ WeChat Client │
│  Handler  │     │  (10s delay)     │     │ (iLink Bot)   │
└───────────┘     └──────────────────┘     └───────────────┘
                         │                        │
                    Wraps inner               Sends via
                    handler                   HTTP API
                         │                        │
                    ┌────▼────┐              ┌────▼────┐
                    │ Approval│              │ WeChat  │
                    │ + Done  │              │  User   │
                    │ events  │              │  📱     │
                    └─────────┘              └─────────┘
```

The `NotifyingHandler` wraps the TUI or Web handler. It delegates all events to the inner handler and additionally:

- **Approval requests**: Starts a 10-second timer. If the user doesn't respond within 10 seconds, fires a notification to the channel.
- **Agent done**: Sends the last ~600 characters of agent output as a summary.

This design means channels are transparent — they don't affect normal operation. The agent and tools work exactly the same whether channels are enabled or not.

## Adding New Channels

The channel system is designed for extensibility. To add a new channel:

1. Implement the `channel.Channel` interface in a new package under `internal/`:

```go
type Channel interface {
    ID() string
    State() State
    Login() (*LoginSession, error)
    Logout() error
    Enable() error
    Disable() error
    SendText(text string) error
}
```

2. Wire it up in `internal/command/interactive.go` and `internal/command/web.go`
3. Add a case in `handleChannelAction()` for the TUI
4. Add API routes in `internal/web/channel.go` for the web interface

The shared message functions in `internal/channel/messages.go` (welcome, goodbye, approval, done, busy) work with any channel — they return plain text strings.
