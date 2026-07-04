---
title: JCode Buddy
parent: Overview
nav_order: 16
---

# JCode Buddy

JCode Buddy (Liuguang) is a desktop companion device that sits under your monitor — a 3.5-inch screen driven by an ESP32 development board, home to a pixel cat. It connects to your computer via Bluetooth and displays jcode's working status in real time.

## Why You Need It

You're coding. You kick off a build or have jcode run a long task, then switch to your browser. When you look back, jcode is already waiting for your approval — or it has failed.

With Buddy, you don't need to stare at the terminal. The cat tells you what's happening:

- 🐱 **Leisurely wagging tail** — jcode is idle, standing by
- 🚶 **Walking off** — jcode is working
- ⚠️ **Pouncing and staring at you** — action needs your approval
- 🎉 **Jumping + hearts** — task completed!
- 😴 **Dozing off** — Bluetooth disconnected

Status messages also appear alongside the cat on screen, so you can see progress at a glance.

## Quick Start

### 1. Enable BLE

Edit `~/.jcode/config.json` and add:

```json
{
  "channel": {
    "ble_enabled": true
  }
}
```

Or type `/setting` in the TUI, find the Channel settings, and enable BLE.

### 2. Power On

Power up your Buddy device (USB-C to a power source or your computer).

### 3. Auto-Connect

No pairing needed. jcode automatically scans for nearby Bluetooth devices named `JCODE-*` and connects. Once connected, the cat wakes up from sleep, and the device plays a notification sound.

The first connection happens when you send your first message (jcode uses lazy connection to avoid unnecessary Bluetooth scanning). Once established, the connection stays active until the device powers off or goes out of Bluetooth range.

## Voice Input

If your Buddy device has a microphone, you can speak directly to it to send commands to jcode:

1. Press the **record button** on the device
2. Speak your command (e.g., "Check if the tests passed")
3. Your speech is transcribed to text in real time and sent to jcode
4. Press **send** to confirm, or **cancel** to discard

This is especially handy when you don't want to take your hands off the keyboard. Voice transcription uses Alibaba Cloud DashScope real-time ASR service, which requires the device to be connected to Wi-Fi.

## Status Animations

| Status | Animation | Trigger |
|---|---|---|
| **Sleep** | Eyes closed + floating Zzz | Default state when Bluetooth is not connected |
| **Idle** | Wagging tail + occasional blink | After jcode starts or between tasks |
| **Working** | Walking animation | jcode is thinking and executing |
| **Attention** | Pounce + paws appear | jcode is waiting for your approval |
| **Complete** | Jumping + hearts + sound effect | Task completed successfully |

## Device Features

Beyond the jcode-connected pixel cat, Buddy is also a fully functional little device on its own:

- **Clock** — Large font digital clock + date display + second-hand arc progress ring
- **Wi-Fi** — First-time setup via hotspot (`LiuGuang-Setup`), auto-connects thereafter
- **Touchscreen** — Press BOOT button to switch between clock and cat pages

## Configuration Reference

| Setting | Default | Description |
|---|---|---|
| `channel.ble_enabled` | `false` | Enable Bluetooth device auto-discovery and status notifications |

BLE is off by default to avoid Bluetooth scanning when not needed. Only enable it if you have a JCode Buddy device.

## Troubleshooting

### Device Not Found / Can't Connect

- Make sure the device is powered on (screen should be displaying)
- Make sure `ble_enabled` is set to `true`
- Make sure your computer's Bluetooth is turned on
- macOS users: jcode uses CoreBluetooth, which requires CGo compilation. If you run into issues, try recompiling jcode

### No Animation After Connecting

- Wait a few seconds — Bluetooth connections sometimes need a moment to stabilize
- Try sending a message in jcode to trigger a status push

### Voice Input Not Working

- Make sure the device is connected to Wi-Fi
- Make sure the ASR API Key (DashScope) is configured
- Check device logs to confirm the WebSocket connection status
