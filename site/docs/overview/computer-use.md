---
title: Computer Use
parent: Overview
nav_order: 11
---

# Computer Use

jcode can see and operate native macOS applications such as Finder, Notes,
Xcode, and System Settings. It uses Accessibility for structured UI snapshots
and actions, plus Screen Recording for screenshots. Computer Use is available
on **macOS 14 or newer** and is off by default.

## Set it up

1. Install a release build, run `make install`, or build the desktop app. These
   paths place `jcode-computerd` and its capture worker beside `jcode`.
2. Open **Settings → Computer** and turn on Computer Use.
3. Grant both permissions shown in the readiness card:
   - **Accessibility** lets the helper inspect controls and perform approved
     clicks or keyboard actions.
   - **Screen Recording** lets the capture worker return screenshots when an
     accessibility tree is incomplete.
4. Return to jcode and choose **Check again**. The page updates automatically,
   but the button makes the permission check immediate.

The settings page never treats an unknown permission as ready. If macOS asks
you to restart an app after granting a permission, restart jcode and check the
card again.

## How the agent sees an app

The primary view is an Accessibility snapshot: a compact tree where controls
have short IDs the agent can target precisely. A screenshot is the visual
fallback for custom-drawn interfaces and is attached directly to the model's
tool result, so vision-capable models can inspect it.

Screenshot pixels are retained in the active model context only until the next
model call consumes them. Private UI copies expire after 24 hours and are also
bounded by file count and total size.

Computer Use has no backend selector or AppleScript fallback. Production always
uses the native macOS helper. Deterministic screens used by the test suite are
only present in binaries built explicitly with `-tags jcode_eval`.

## Safety and approvals

- Apps must be granted before a task controls them.
- Terminals and IDEs are click-only through Computer Use; shell commands go
  through the normal command tool and approval flow.
- Browsers are read-only through Computer Use; web interaction uses Browser Use,
  which can verify URLs and origins.
- Clipboard read/write and system key combinations are separate, off-by-default
  grants.
- Turning Computer Use off, reducing the batch limit, tightening an app tier,
  or revoking clipboard/system-key grants applies to already-open tasks.
  An app approval granted for one task lasts until that task ends; turn
  Computer Use off before revoking an in-flight task's app access.

## TUI

`/computer` shows native-helper and permission readiness. `/computer on` and
`/computer off` toggle the feature. On another operating system, the command
explains that macOS 14+ is required and does not expose Computer Use tools.

## Configuration

```json
{
  "computer": {
    "enabled": true,
    "max_actions_per_batch": 20,
    "clipboard_read": false,
    "clipboard_write": false,
    "system_key_combos": false,
    "approval": {
      "launch": "ask",
      "interact": "ask"
    },
    "app_permissions": []
  }
}
```

There is intentionally no `backend` field. Very old `auto` or `helper` values
are removed during migration. Old `fake`, `osa`, or unknown values fail closed:
Computer Use is disabled and saved grants are cleared so a test configuration
cannot silently begin controlling the real desktop.

## Troubleshooting

- **Helper not installed:** reinstall jcode using a release package or
  `make install`, then restart jcode.
- **Accessibility not granted:** open the Accessibility row's System Settings
  button and enable the listed jcode helper/app.
- **Screen Recording not granted:** use the Screen Recording row's button,
  enable the capture helper/app, and follow macOS's restart instruction if one
  appears.
- **The current task does not show the tools:** use **Check again** first. If a
  model-provider error prevented the live tool refresh, start a new task; the
  saved setting will already be in effect.
