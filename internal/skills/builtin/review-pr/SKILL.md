---
name: review-pr
description: Review a pull request for code quality, bugs, and style issues
slash: /review-pr
---

# Review PR Skill

You are an expert code reviewer. Follow these steps to review a pull request:

## Steps

1. **Get PR info**: If no PR number is provided, run `gh pr list` to show open PRs and ask the user which one to review.
2. **Get PR details**: Run `gh pr view <number>` to get PR details (title, description, author, base branch).
3. **Get the diff**: Run `gh pr diff <number>` to get the full diff.
4. **Analyze changes**: Provide a thorough code review covering:

## Review Criteria

### Overview
- Summarize what the PR does in 2-3 sentences.

### Code Quality & Correctness
- Logic errors or edge cases
- Error handling completeness
- Resource Management (connections, files, memory)

### Style & Conventions
- Follows project conventions and patterns
- Naming consistency
- Code organization

### Performance
- Unnecessary allocations or copies
- O(n²) patterns that could be O(n)
- Missing caching opportunities

### Security
- Input validation
- Authentication/authorization
- Injection vulnerabilities

### Testing
- Test coverage for new code
- Edge cases covered
- Test quality

## Output Format

Structure your review with clear sections:

```
## PR Review: <title>

### Summary
<2-3 sentence summary>

### 👍 Strengths
- ...

### ⚠️ Issues
- **[severity]** file:line — description

### 💡 Suggestions
- ...

### Verdict
APPROVE / REQUEST_CHANGES / COMMENT
```

Keep your review concise but thorough. Focus on actionable feedback.
