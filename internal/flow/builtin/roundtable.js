export const meta = {
  name: "roundtable",
  description: "Convene several independent expert agents on a question, then synthesize their views into one answer",
  whenToUse: "Use when a question benefits from multiple independent perspectives before committing to an answer (design decisions, trade-off analysis, risky choices)",
  phases: [
    { title: "Panel", detail: "Each expert answers the question independently" },
    { title: "Critique", detail: "Each expert reviews the others' answers" },
    { title: "Synthesize", detail: "Merge into one balanced recommendation" },
  ],
};

// args: { question: string, roles?: string[] }
const question = (args && args.question) ? String(args.question) : "";
if (!question) {
  return "roundtable requires args.question — the topic to deliberate on.";
}
const roles = (args && Array.isArray(args.roles) && args.roles.length)
  ? args.roles
  : ["a pragmatic engineer", "a skeptical reviewer", "a long-term maintainer"];

phase("Panel", roles.length + " independent takes");
const takes = await parallel(
  roles.map((role, i) => () =>
    agent(
      "You are " + role + ". Answer this question with your honest, independent view. " +
      "Be concrete and take a clear position.\n\nQuestion: " + question,
      { label: "panel:" + (i + 1), phase: "Panel", agentType: "explore" }
    ).then((view) => ({ role: role, view: view }))
  )
);

const panel = takes.filter(Boolean);
log("Collected " + panel.length + " independent views");

phase("Critique", "Cross-review");
const critiques = await parallel(
  panel.map((p, i) => () =>
    agent(
      "You are " + p.role + ". Here are the other panelists' views on the question:\n\n" +
      panel.filter((_, j) => j !== i).map((o) => "### " + o.role + "\n" + o.view).join("\n\n") +
      "\n\nWhat do they get right, and where do you still disagree? Be specific.",
      { label: "critique:" + (i + 1), phase: "Critique", agentType: "explore" }
    )
  )
);

phase("Synthesize", "Merging into one recommendation");
const answer = await agent(
  "Synthesize a single, balanced recommendation for this question from the panel below. " +
  "Note where the panel agreed, surface the strongest dissent, and end with a clear recommendation.\n\n" +
  "Question: " + question + "\n\n" +
  panel.map((p, i) => "## " + p.role + "\nView: " + p.view + "\nRebuttal: " + (critiques[i] || "")).join("\n\n"),
  { label: "synthesize", phase: "Synthesize", agentType: "explore" }
);

return answer;
