# Custom agents implementation plan

Status: implementation contract

## Product contract

JCode custom agents follow Codex's useful separation between a discoverable role and its execution configuration, adapted to JCode's JSON configuration and existing subagent/team tools.

A role has:

- a stable lowercase name used as `agent_type`;
- a required description advertised in tool schemas so the parent can choose it;
- a base profile (`explore`, `general`, or `coordinator`) that caps available tools;
- optional instructions appended to the base profile prompt;
- an optional default model reference (`provider/model` or `small`).

Definitions can be declared in `config.json` under `agents`, or discovered from `~/.jcode/agents/*.json` and `<project>/.jcode/agents/*.json`. Precedence is builtin < user < project < inline config. A malformed role is ignored with a visible diagnostic in `~/.jcode/debug.log`; it never broadens permissions.

Example:

```json
{
  "name": "reviewer",
  "description": "Review a patch for correctness, security, and missing tests",
  "profile": "explore",
  "instructions": "Lead with findings ordered by severity.",
  "model": "small"
}
```

## Security rules

- A custom role cannot define tools directly. Its `profile` selects an existing audited tool set.
- Unknown or malformed profiles are rejected; no fallback to a broader profile.
- Custom role spawns always pass the parent approval boundary; the resolved base profile then caps child tools. Renaming a role therefore cannot bypass delegated-write approval (an `explore` custom role may prompt more conservatively than the builtin).
- Role files are read from fixed user/project directories; symlinks and files larger than 64 KiB are rejected.
- Explicit spawn `model` overrides the role default. A role default never overrides a caller's explicit selection.

## Implementation and tests

1. Add a deterministic loader/validator in `internal/config/agent_roles.go` with unit tests for precedence, malformed input, symlinks, size limits, and duplicate names.
2. Resolve custom roles in direct subagents, teams, and workflow agents. Tool schemas advertise the available role names and descriptions.
3. Preserve built-in behavior when no custom roles exist.
4. Test tool-set capping, prompt composition, model precedence, unknown-role errors, and each transport's tool construction.
5. Adversarial review permission escalation, path traversal/symlink reads, unbounded prompt size, and config races before delivery.
