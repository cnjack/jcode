export const meta = {
  name: "pr-review",
  description: "Review the current git diff across correctness, security, and tests, then verify each finding before reporting",
  whenToUse: "Use to review the current branch's changes (uncommitted or vs a base) before opening a PR",
  phases: [
    { title: "Diff", detail: "Collect the changed files and the diff" },
    { title: "Review", detail: "One reviewer per dimension" },
    { title: "Verify", detail: "Adversarially verify each finding" },
    { title: "Report", detail: "Merge surviving findings into a report" },
  ],
};

// args: { base?: string }  — base ref to diff against (default: staged + unstaged)
// The base is interpolated into a command the review agent runs, so restrict it to
// a safe git-ref charset to avoid shell/argument injection; anything else is ignored.
const rawBase = (args && args.base) ? String(args.base) : "";
const base = /^[A-Za-z0-9._/-]+$/.test(rawBase) ? rawBase : "";
if (rawBase && !base) log("ignoring unsafe base ref: " + rawBase, "warn");
const diffCmd = base ? ("git diff " + base + "...HEAD") : "git diff HEAD";

phase("Diff", "Collecting changes");
const diff = await agent(
  "Run `" + diffCmd + "` and list the changed files with a one-line summary of what each change does. " +
  "If the diff is empty, say so clearly.",
  { label: "diff", phase: "Diff", agentType: "explore" }
);

const DIMENSIONS = [
  { key: "correctness", ask: "correctness bugs, broken edge cases, and regressions introduced by this diff" },
  { key: "security", ask: "security issues introduced by this diff: injection, authz, secrets, unsafe input" },
  { key: "tests", ask: "missing or inadequate test coverage for the behavior this diff changes" },
];

phase("Review", DIMENSIONS.length + " dimensions");
const reviews = await parallel(
  DIMENSIONS.map((d) => () =>
    agent(
      "Review the current branch (" + diffCmd + ") for " + d.ask + ".\n\nDiff summary:\n" + diff +
      "\n\nRead the changed files as needed. Report concrete findings with file references, most severe first. If none, say 'no findings'.",
      { label: "review:" + d.key, phase: "Review", agentType: "explore" }
    ).then((text) => ({ dimension: d.key, text: text }))
  )
);

phase("Verify", "Cross-checking findings");
const verified = await pipeline(
  reviews.filter(Boolean),
  (r) =>
    agent(
      "Adversarially verify these " + r.dimension + " findings against the actual code. For each, decide if it is a " +
      "real defect or a false positive, and drop the false positives. Return only the surviving, verified findings.\n\n" + r.text,
      { label: "verify:" + r.dimension, phase: "Verify", agentType: "explore" }
    ).then((text) => ({ dimension: r.dimension, text: text }))
);

phase("Report", "Merging");
const report = await agent(
  "Merge these verified review findings into one PR review report. Group by severity, deduplicate, and end with a " +
  "clear go / no-go recommendation.\n\n" +
  verified.filter(Boolean).map((v) => "## " + v.dimension + "\n" + v.text).join("\n\n"),
  { label: "report", phase: "Report", agentType: "explore" }
);

return report;
