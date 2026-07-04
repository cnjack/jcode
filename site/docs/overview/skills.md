---
title: Skills
parent: Overview
nav_order: 7
---

# Skills

Skills are domain-specific instruction packs that extend jcode's capabilities. Each skill provides specialized knowledge for a particular task domain.

## How Skills Work

Skills use a **two-layer** loading system:

1. **Layer 1 (always loaded)** — The skill's name, slash command, and short description are injected into the agent's context at startup. This costs very few tokens.
2. **Layer 2 (on demand)** — When the agent needs detailed instructions, it loads the full skill content via the `load_skill` tool.

## Built-in Skills

jcode includes four built-in skills:

### PR Review (`/review-pr`)

Reviews a GitHub pull request for code quality, bugs, style, performance, and security. Provides a structured report with:

- Summary and strengths
- Issues categorized by severity
- Suggestions for improvement
- Verdict: APPROVE / REQUEST CHANGES / COMMENT

### PR Comments (`/pr-comments`)

Fetches and displays all comments from a GitHub pull request, including both PR-level comments and inline code review comments with diff context.

### Security Review (`/security-review`)

Performs a security-focused code review of pending changes on the current branch. Identifies high-confidence vulnerabilities with real exploitation potential, including:

- SQL/command injection
- Authentication bypass
- Hardcoded credentials
- Remote code execution
- Data exposure

### Submit PR (`/submit-pr`)

Commits the current working changes, pushes the branch, and opens a GitHub pull request. It:

- Checks for changes to submit and stops if there are none
- Creates a feature branch first if you are on the default branch (`main`/`master`)
- Commits with a Conventional Commit message and pushes the branch
- Opens the PR with `gh pr create` and reports the PR URL

It never force-pushes or pushes directly to the default branch.

{: .note }
The PR-related skills require the `gh` CLI tool to be installed and authenticated. Run `gh auth login` if you haven't already.

## Using Skills

Type the skill's slash command in the TUI:

```
/review-pr
```

Or simply ask the agent to perform the task:
```
Review the current pull request
```

The agent will automatically load the relevant skill if available.

## Custom Skills

Create your own skills to teach jcode specialized behaviors.

### Skill File Format

Each skill is a directory containing a `SKILL.md` file:

```markdown
---
name: my-skill
description: What this skill does
slash: /my-command
---

# My Skill Title

Detailed instructions for the agent...
```

The frontmatter supports:
- `name` — Skill identifier (defaults to directory name)
- `description` — Short description shown in the skill list
- `slash` — Slash command trigger (e.g., `/my-command`)

### Where to Put Skills

Skills are loaded from multiple locations. Later sources override earlier ones:

| Priority | Location | Scope |
|---|---|---|
| 1 (lowest) | Built-in (embedded in binary) | Always available |
| 2 | `~/.agents/skills/{name}/SKILL.md` | Agent-level, shared across projects |
| 3 | `~/.jcode/skills/{name}/SKILL.md` | User-level, your personal skills |
| 4 (highest) | `.jcode/skills/{name}/SKILL.md` | Project-level, committed to the repo |

### Example: Creating a Custom Skill

1. Create the directory:
   ```bash
   mkdir -p ~/.jcode/skills/deploy-check
   ```

2. Create `SKILL.md`:
   ```markdown
   ---
   name: deploy-check
   description: Pre-deployment checklist for production releases
   slash: /deploy-check
   ---

   # Deploy Check

   Before deploying to production, verify:

   1. Run all tests: `make test`
   2. Check for TODO/FIXME comments in modified files
   3. Verify no debug logging left in code
   4. Check that version numbers are bumped
   5. Review the git diff for any unexpected changes
   ```

3. The skill is now available as `/deploy-check` in any jcode session.
