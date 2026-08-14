export const meta = {
  name: "code-review-benchmark-judge",
  description: "Semantically match completed review findings to an independent human gold set",
  whenToUse: "Use only after a reviewer run has completed; never expose gold comments to the reviewer",
  phases: [
    { title: "Judge", detail: "Compare every confirmed finding with every gold issue" },
  ],
};

const golden = args && Array.isArray(args.golden) ? args.golden : [];
const candidates = args && Array.isArray(args.candidates) ? args.candidates : [];
const model = args && args.model ? String(args.model) : "";

const pairs = [];
golden.forEach(function (gold) {
  candidates.forEach(function (candidate) {
    pairs.push({
      gold_id: String(gold.id),
      candidate_id: String(candidate.id),
      golden_comment: String(gold.comment),
      candidate: {
        category: candidate.category,
        severity: candidate.severity,
        path: candidate.path,
        line: candidate.line,
        title: candidate.title,
        rationale: candidate.rationale,
        evidence: candidate.evidence,
        verification_reason: candidate.verification_reason,
      },
    });
  });
});

if (pairs.length === 0) {
  return { schema_version: "jcode-review-judge/v1", evaluations: [], metrics: { total_tokens: 0 } };
}

const schema = {
  type: "object",
  required: ["evaluations"],
  properties: {
    evaluations: {
      type: "array",
      items: {
        type: "object",
        required: ["gold_id", "candidate_id", "match", "confidence", "reason"],
        properties: {
          gold_id: { type: "string" },
          candidate_id: { type: "string" },
          match: { type: "boolean" },
          confidence: { type: "number", minimum: 0, maximum: 1 },
          reason: { type: "string" },
        },
      },
    },
  },
};

phase("Judge", pairs.length + " semantic comparison(s)");
const judged = await agent(
  [
    "You are an independent evaluator of an AI code review tool. The reviewer has already finished; this evaluation cannot affect its output.",
    "For every supplied pair, decide whether the candidate identifies the SAME underlying issue as the human golden comment. Accept different wording and line placement when the causal bug is the same. Reject merely related topics, broader guesses, or different failure modes.",
    "Return exactly one evaluation for every (gold_id, candidate_id) pair, preserving both ids. Do not merge or omit pairs.",
    "Pairs: " + JSON.stringify(pairs),
  ].join("\n\n"),
  {
    label: "semantic-judge",
    phase: "Judge",
    agentType: "reasoner",
    model: model,
    maxIterations: 2,
    schema: schema,
  }
);

return {
  schema_version: "jcode-review-judge/v1",
  evaluations: judged && Array.isArray(judged.evaluations) ? judged.evaluations : [],
  metrics: { total_tokens: budget.spent() },
};
