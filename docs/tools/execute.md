---
title: Execute
parent: Tools
nav_order: 3
---

# Execute

Run shell commands on your machine (or remote SSH host).

## Approval

| Command Type | Approval |
|---|---|
| Safe commands (`ls`, `pwd`, `env`, `cat`, `echo`, `which`, `git status`, `git log`) | Auto-approved |
| Background tasks | Auto-approved |
| All other commands | **Requires approval** |

## Basic Usage

```
  ⚙ Tool  execute   command="make test"

  ╭─────────────────────────────────────────────────────╮
  │  ok      github.com/example/app     0.234s          │
  │  ok      github.com/example/app/api 1.456s          │
  │  PASS                                               │
  ╰─────────────────────────────────────────────────────╯
     ✓ Done (2.1s)
```

## Background Mode

For long-running tasks like builds and test suites:

```
  ⚙ Tool  execute   command="make build"  background=true
     ✓ Background task started: bg_1
```

Check status with `/bg` or the `check_background` tool:

```
  ⚙ Tool  check_background

  ╭─────────────────────────────────────────────────────╮
  │  bg_1: make build — running (45s)                   │
  │  bg_2: npm test — done (12s)                        │
  ╰─────────────────────────────────────────────────────╯
```

## Timeout

- Default timeout: **2 minutes** (120 seconds)
- Maximum timeout: **10 minutes** (600 seconds)
- Background tasks have a 5-minute default timeout
- Commands that exceed 30 seconds of `sleep` are blocked for safety

## SSH Remote

When connected to an SSH host, `execute` runs commands on the remote machine:

```
  ⚙ Tool  execute  [deploy@10.0.1.5]  docker logs app-nginx-1 --tail 20
```
