---
title: File Tools
parent: Tools
nav_order: 1
---

# File Tools

## read

Read file contents with line numbers, or list a directory's contents.

**Approval:** Auto-approved within project path; requires approval for external paths.

```
  ⚙ Tool  read   path=src/main.go

  ╭─────────────────────────────────────────────────────╮
  │   1 │ package main                                  │
  │   2 │                                               │
  │   3 │ import "fmt"                                  │
  │   4 │                                               │
  │   5 │ func main() {                                 │
  │   6 │     fmt.Println("Hello, World!")              │
  │   7 │ }                                             │
  ╰─────────────────────────────────────────────────────╯
```

Key features:
- Supports `offset` and `limit` for reading specific line ranges
- Automatically detects and rejects binary files
- Shows directory listing when the path is a directory
- Max file size: 10 MB

## edit

Make precise string replacements in files.

**Approval:** Always requires approval.

```
  ⚙ Tool  edit   path=server.go

  ╭─────────────────────────────────────────────────────╮
  │  - go handle(conn)                                  │
  │  + wg.Add(1)                                        │
  │  + go func() { defer wg.Done(); handle(conn) }()   │
  ╰─────────────────────────────────────────────────────╯
     ✓ Edit applied
```

Key features:
- **Single edit**: Replace an exact string with a new string
- **Create mode**: Create a new file (no old_string)
- **Multi-edit**: Apply multiple replacements at once
- **Replace all**: Replace every occurrence in the file
- **Line scoping**: Narrow the search with `start_line` / `end_line`
- Automatic backup before modifying
- Conflict detection if the file changed since last read
- Helpful error messages with suggestions when old text isn't found

## write

Write full content to a file, creating or overwriting it.

**Approval:** Always requires approval.

Key features:
- Creates parent directories automatically
- Shows a diff when overwriting existing files
- Automatic backup before overwriting
- Conflict detection
- Max content size: 10 MB
