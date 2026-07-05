export const meta = {
  name: "repo-audit",
  description: "Audit a repository area across several dimensions and merge findings into a ranked report",
  whenToUse: "Use when the user asks for a structured repository or code audit of an area, module, or the whole repo",
  phases: [
    { title: "Scan", detail: "Find the files and entry points in the target area" },
    { title: "Analyze", detail: "Run one focused audit agent per dimension" },
    { title: "Summarize", detail: "Merge findings into a single ranked report" },
  ],
};

// args: { area?: string }  — the module/path/area to audit ("" = whole repo)
const area = (args && args.area) ? String(args.area) : "the repository";

phase("Scan", "Mapping " + area);
const map = await agent(
  "Map " + area + ": list the key files, entry points, and responsibilities. Be concise. " +
  "Return a short structured overview a reviewer can use to target deeper analysis.",
  { label: "scan:" + area, agentType: "explore" }
);

const DIMENSIONS = [
  { key: "correctness", ask: "correctness bugs, error handling gaps, and edge cases" },
  { key: "security", ask: "security issues: injection, authz, secrets, unsafe input handling" },
  { key: "performance", ask: "performance problems: N+1s, unbounded work, needless allocation" },
  { key: "maintainability", ask: "maintainability smells: duplication, dead code, unclear boundaries" },
];

phase("Analyze", "Auditing " + DIMENSIONS.length + " dimensions");
const findings = await parallel(
  DIMENSIONS.map((d) => () =>
    agent(
      "Audit " + area + " for " + d.ask + ".\n\nContext map:\n" + map +
      "\n\nReport concrete findings with file references, most severe first. If none, say so.",
      { label: "audit:" + d.key, phase: "Analyze", agentType: "explore" }
    ).then((text) => ({ dimension: d.key, findings: text }))
  )
);

log("Audited " + findings.filter(Boolean).length + "/" + DIMENSIONS.length + " dimensions");

phase("Summarize", "Merging findings");
const report = await agent(
  "Merge these audit findings into ONE ranked report. Deduplicate overlaps, rank by severity, " +
  "and end with the top 3 recommended fixes.\n\n" +
  findings.filter(Boolean).map((f) => "## " + f.dimension + "\n" + f.findings).join("\n\n"),
  { label: "summarize", phase: "Summarize", agentType: "explore" }
);

return report;
