# Custom agents

Status: implementation contract

## Definition

JCode custom agents are Markdown-defined roles used both for top-level sessions
and for delegated subagents, workflows, and team members.

JCode loads only files matching:

- `~/.jcode/agents/*.agent.md` (user scope)
- `<project>/.jcode/agents/*.agent.md` (project scope)

Example:

```md
---
name: bug-fix-teammate
description: Investigate regressions and implement focused fixes
model: anthropic/claude-sonnet-4-5
---

Reproduce the failure before editing. Keep the patch focused and run the
smallest relevant test suite before returning.
```

`name` and `description` are required frontmatter strings. The Markdown body is
the required, non-empty agent instruction. `model` is optional and accepts a
`provider/model` reference or the `small` alias.

Unknown frontmatter fields, malformed YAML, empty required values, symlinks,
files larger than 64 KiB, invalid names, and built-in role names are ignored
with a diagnostic in `~/.jcode/debug.log`.

## Discovery and precedence

Files are read in lexicographic filename order. Within one scope, the first
valid definition for a given frontmatter `name` wins. A valid project
definition overrides the effective user definition with the same name. The UI
shows only the final effective definition.

Malformed higher-precedence files do not hide a valid lower-precedence
definition.

## Runtime semantics

- The Markdown instruction is appended to JCode's normal system prompt.
- The role inherits the caller's mode, approval policy, sandbox, MCP access,
  and tool set. Agent Markdown cannot add tools or bypass approval.
- A role model is applied when the role is selected. With no role model, the
  current session model is inherited. For delegated agents, the role model
  takes precedence over a per-call model; otherwise the per-call/current model
  rules remain unchanged.
- Session metadata and JSONL events retain the effective top-level agent.
  Resume restores it; if the definition is no longer valid, JCode falls back
  to Default and surfaces the change.

## Selection and display

- CLI: `jcode --agent <name>` selects a custom agent. Omitting the flag or
  passing `--agent default` selects Default.
- Web/Desktop: the composer shows the Agent picker only when at least one
  custom agent is available. It contains Default plus all effective custom
  agents, displays the active name in the toolbar, and records switches as
  visible timeline notices.
- ACP intentionally exposes no top-level agent picker. New ACP sessions use
  Default. Loading or resuming a session that already selected a custom agent
  restores that role automatically. Custom agents remain available to ACP's
  delegation tools.

Inline JSON agent definitions and legacy `*.json` files are unsupported.
