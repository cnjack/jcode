---
title: Custom Agents
parent: Overview
nav_order: 20
---

# Custom Agents

Custom agents give a reusable name and instruction set to a specialized JCode
role. The same definition can be selected for a top-level chat or delegated
through subagent, workflow, and team tools.

## Define an agent

Create a file ending in `.agent.md`:

- User scope: `~/.jcode/agents/<file>.agent.md`
- Project scope: `<project>/.jcode/agents/<file>.agent.md`

```md
---
name: bug-fix-teammate
description: Investigate regressions and implement focused fixes
model: anthropic/claude-sonnet-4-5
---

Reproduce the failure before editing. Keep the patch focused and run the
smallest relevant test suite before returning.
```

`name`, `description`, and the Markdown instruction body are required. `model`
is optional; it accepts `provider/model` or `small`. The name comes from
frontmatter, not from the filename.

Only `*.agent.md` files are loaded. Legacy JSON definitions are not supported.

## Precedence

Within one scope, files are checked in filename order and the first valid
definition for a name wins. A project definition overrides a user definition
with the same name. The picker shows one effective entry per name.

## Select an agent

In Web/Desktop, use the **Agent** picker beside the model controls. The picker
appears only when at least one valid custom agent exists and always includes
**Default**.

From the terminal:

```bash
jcode --agent bug-fix-teammate
jcode --agent default
```

The selection is saved with the session and restored on resume. ACP does not
offer an agent picker: new ACP sessions use Default, while resumed sessions
continue their saved custom agent automatically.

## Models, tools, and permissions

An agent with `model` switches to that model when selected; without it, the
current model is inherited. Custom agents inherit the current mode, approval
policy, sandbox, MCP access, and tool set. Markdown instructions cannot grant
extra tools or bypass approval.
