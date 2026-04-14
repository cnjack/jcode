---
title: SSH & Remote Work
parent: Overview
nav_order: 9
---

# SSH & Remote Work

jcode can work on any machine over SSH. All tools — read, edit, write, execute — run transparently on the remote host. No agents, no tunnels, no extra setup.

## Quick Start

Type `/ssh` in the TUI or use the SSH wizard:

```
You › /ssh deploy@10.0.1.5:/var/www/app

  ✓ SSH  Connected · linux/amd64

You › why is nginx restarting?

  ⚙ Tool  execute  [deploy@10.0.1.5]  docker logs app-nginx-1 --tail 20

  ╭─────────────────────────────────────────────────────╮
  │  nginx: [emerg] bind() to 0.0.0.0:80 failed        │
  │  (98: Address already in use)                       │
  ╰─────────────────────────────────────────────────────╯

  ◆ Port 80 is already taken. Let me find what's holding it.
```

## Configuring SSH Aliases

Save frequently used connections as named aliases in `~/.jcode/config.json`:

```json
{
  "ssh_aliases": [
    { "name": "prod", "addr": "deploy@10.0.1.5", "path": "/var/www/app" },
    { "name": "staging", "addr": "ci@10.0.1.8", "path": "/srv/staging" }
  ]
}
```

Then select from the picker:

```
  ┌─────── /ssh ────────────────────────────────────┐
  │  > 🔗 prod        deploy@10.0.1.5:/var/www/app  │
  │    🔗 staging      ci@10.0.1.8:/srv/staging      │
  │    ➕ Connect New SSH                             │
  └─────────────────────────────────────────────────┘
```

## Authentication

jcode tries the following SSH authentication methods in order:

1. **SSH Agent** — If `SSH_AUTH_SOCK` is set and your agent has keys
2. **Key files** — Tries common key files from `~/.ssh/`:
   - `id_rsa`
   - `id_ed25519`
   - `id_ecdsa`

{: .note }
Make sure your SSH keys are set up and the remote host is accessible. Test with `ssh user@host` before using jcode's SSH feature.

## Switching Environments

Once connected, you can switch between local and remote:

- **In the TUI**: Type `/ssh` and select a different host or "local"
- **The agent** can use the `switch_env` tool to switch environments (requires your approval)

## What Works Over SSH

Everything works the same as local:

- **File operations** — Read, edit, write files on the remote machine
- **Command execution** — Run commands on the remote host
- **Code search** — Grep and glob search remote files
- **Project context** — The agent detects the remote project type, git state, and directory structure

## Connection Details

- Connection timeout: 10 seconds
- File writes use base64 encoding to avoid shell escaping issues
- Platform auto-detected via `uname -sm`
- Default port: 22 (specify in addr as `user@host:port`)
