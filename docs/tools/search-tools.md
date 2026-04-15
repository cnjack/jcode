---
title: Search Tools
parent: Tools
nav_order: 2
---

# Search Tools

## grep

Search for patterns across your codebase using regular expressions.

**Approval:** Auto-approved.

```
  ⚙ Tool  grep   pattern="handleError"  path=src/  include=*.go

  ╭─────────────────────────────────────────────────────╮
  │  src/handler.go:42: func handleError(err error) {   │
  │  src/handler.go:58:     handleError(err)            │
  │  src/middleware.go:23: handleError(ctx.Err())       │
  ╰─────────────────────────────────────────────────────╯
```

Key features:
- Uses **ripgrep** when available for fast searching
- Supports regex patterns
- Filter by file type (`go`, `py`, `js`, etc.) or glob pattern
- Case-insensitive search option
- Context lines around matches
- Pagination with offset
- Respects `.gitignore`
- Skips dependency directories (`node_modules`, `vendor`, etc.)

## glob

Find files by name pattern.

**Approval:** Auto-approved.

```
  ⚙ Tool  glob   pattern=**/*.test.ts

  ╭─────────────────────────────────────────────────────╮
  │  src/auth/login.test.ts                             │
  │  src/api/users.test.ts                              │
  │  src/utils/helpers.test.ts                          │
  ╰─────────────────────────────────────────────────────╯
```

Key features:
- Supports glob patterns like `*.go`, `**/*.test.ts`
- Max depth control
- Limit results (default 100, max 500)
- Skips dependency directories
