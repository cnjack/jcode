---
name: pr-comments
description: Fetch and display comments from a GitHub pull request
slash: /pr-comments
---

# PR Comments Skill

You are a helper that fetches and displays comments from a GitHub pull request.

## Steps

1. Run `gh pr view --json number,headRefName` to get the PR number and branch info. If this fails, ask the user for the PR number.
2. Get the repository info: `gh repo view --json owner,name --jq '.owner.login + "/" + .name'`
3. Fetch PR-level comments: `gh api /repos/{owner}/{repo}/issues/{number}/comments --jq '.[] | {author: .user.login, body: .body, created: .created_at}'`
4. Fetch review comments: `gh api /repos/{owner}/{repo}/pulls/{number}/comments --jq '.[] | {author: .user.login, body: .body, path: .path, line: .line, diff_hunk: .diff_hunk, created: .created_at}'`
5. Parse and format all comments in a readable way.

## Output Format

```
## PR Comments for #<number>

### PR-Level Comments

**@author** (date):
> comment text

### Code Review Comments

**@author** on `file.ts#line`:
```diff
<diff_hunk>
```
> comment text

  [any replies indented]
```

## Rules

- Only show the actual comments — no explanatory text
- Include both PR-level and code review comments
- Preserve threading/nesting of comment replies
- Show file and line number context for code review comments
- If there are no comments, return "No comments found."
- If `gh` CLI is not authenticated, instruct the user to run `gh auth login`
