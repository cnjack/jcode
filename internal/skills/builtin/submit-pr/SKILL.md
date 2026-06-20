---
name: submit-pr
description: Commit the current changes, push the branch, and open a GitHub pull request
slash: /submit-pr
---

# Submit PR Skill

You commit the working changes, push the branch, and open a GitHub pull request.
This is the way the user submits work — there is no manual git UI, so do it
carefully and report the PR URL at the end.

## Steps

1. **Check state.** Run `git status --short` and `git diff --stat`. If there is
   nothing to submit (no staged, unstaged, or unpushed commits), tell the user
   and stop.
2. **Pick a branch.** Run `git rev-parse --abbrev-ref HEAD`. If you are on the
   default branch (`main`/`master`), create a feature branch first:
   `git checkout -b <concise-kebab-branch>` named after the change.
3. **Review before committing.** Read `git status` and the diff so you do not
   commit secrets, build artifacts, or unrelated files. Stage intentionally
   (`git add <paths>`), not blindly with `git add -A` unless you have confirmed
   the whole tree is intended.
4. **Commit** with a clear Conventional Commit message (`feat:`/`fix:`/`docs:`…)
   summarizing the work.
5. **Push:** `git push -u origin <branch>`.
6. **Open the PR:** `gh pr create --title "<title>" --body "<body>"`. The body
   should summarize what changed and why, and list the testing/verification done.
7. **Report the PR URL** returned by `gh`.

## Rules

- Never force-push, and never push directly to the default branch.
- If the diff is large or ambiguous, summarize it and confirm intent before committing.
- If `gh` is not authenticated, instruct the user to run `gh auth login`.
- Keep the commit message and PR title concise and conventional.
- If a PR already exists for the branch, update it (push) rather than creating a duplicate.
