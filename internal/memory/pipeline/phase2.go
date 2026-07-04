package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/memory"
	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/tools"
)

const (
	maxDiffChars       = 40000
	maxAgentIterations = 60
)

type decision struct {
	Op     string `json:"op"`
	Target string `json:"target"`
	Reason string `json:"reason"`
}

type decisionList struct {
	Decisions []decision `json:"decisions"`
}

// runPhase2 consolidates the scope workspace. Steps (design §5.3):
// rank & expire → sync workspace → git diff → no-diff fast exit →
// restricted consolidation agent → commit new baseline.
func runPhase2(ctx context.Context, cfg *config.Config, projectDir string, log func(string, ...any)) error {
	if !gitAvailable() {
		return fmt.Errorf("memory: git not found in PATH; consolidation requires git")
	}
	scope := memory.ProjectRoot(projectDir)
	if err := memory.EnsureScope(scope); err != nil {
		return err
	}
	if _, err := ensureBaseline(scope); err != nil {
		return err
	}

	// Step: expiry + top-N ranking over extracted summaries (usage feedback
	// closes the loop here). Losers are deleted from disk so the deletion
	// shows up in the diff and the agent prunes MEMORY.md accordingly.
	st := memory.LoadState(scope)
	expireAndRank(scope, st, cfg, log)

	// Inbox inventory BEFORE the agent runs: these are the files the agent is
	// asked to digest; the pipeline deletes them after a successful run (the
	// agent has no delete capability by design).
	notes := memory.RecentNotes(scope, 0)

	dirty, diffText, err := workspaceDirty(scope, maxDiffChars)
	if err != nil {
		return err
	}
	if !dirty {
		log("memory: phase 2: no workspace changes — no-op fast path (zero tokens)")
		return memory.UpdateState(scope, func(st *memory.State) error {
			st.LastConsolidation = &memory.ConsolidationRecord{
				At: time.Now().Format(time.RFC3339), NoopFastPath: true,
			}
			return nil
		})
	}

	decisions, err := runConsolidationAgent(ctx, cfg, scope, diffText, notes, log)
	if err != nil {
		// Leave the workspace dirty: next run resumes from the same diff.
		return fmt.Errorf("memory: consolidation agent: %w", err)
	}

	// Post-conditions the agent must have met; refuse to commit garbage.
	if !fileNonEmpty(filepath.Join(scope, memory.IndexFile)) ||
		!fileNonEmpty(filepath.Join(scope, memory.SummaryFile)) {
		return fmt.Errorf("memory: consolidation finished without producing %s/%s", memory.IndexFile, memory.SummaryFile)
	}

	// Digest the inbox: consumed notes are deleted by the pipeline.
	for _, n := range notes {
		_ = os.Remove(n.Path)
	}

	sha, err := commitBaseline(scope, "memory: consolidation "+time.Now().Format("2006-01-02 15:04"))
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, d := range decisions {
		counts[strings.ToUpper(d.Op)]++
	}
	log("memory: phase 2 done: %v (commit %s)", counts, sha)
	return memory.UpdateState(scope, func(st *memory.State) error {
		st.LastConsolidation = &memory.ConsolidationRecord{
			At: time.Now().Format(time.RFC3339), Decisions: counts, Commit: sha,
		}
		return nil
	})
}

// expireAndRank deletes summaries past the unused window and keeps only the
// top-N by usage; deletions surface in the git diff.
//
// The usage signal lives in st.Files (written by RecordUsage on every read of
// a memory file) — ExtractRecord's own counters are never populated, so we
// join through st.Files[SummaryFile] here. That closes the usage-feedback
// loop the design calls for: a summary the agent keeps re-reading ranks high
// and resists expiry; one nobody reads falls to its extraction time.
func expireAndRank(scope string, st *memory.State, cfg *config.Config, log func(string, ...any)) {
	type ranked struct {
		uuid  string
		rec   *memory.ExtractRecord
		count int
		last  string // effective last-activity time (usage or, fallback, extraction)
	}
	usageFor := func(rec *memory.ExtractRecord) (int, string) {
		if u := st.Files[rec.SummaryFile]; u != nil {
			last := u.LastUsage
			if last == "" {
				last = rec.At
			}
			return u.UsageCount, last
		}
		return 0, rec.At
	}

	var withFile []ranked
	maxUnused := time.Duration(config.MemoryMaxUnusedDays(cfg)) * 24 * time.Hour
	now := time.Now()
	for uuid, rec := range st.Extracted {
		if rec.SummaryFile == "" {
			continue
		}
		count, last := usageFor(rec)
		if ts, err := time.Parse(time.RFC3339, last); err == nil && now.Sub(ts) > maxUnused {
			removeSummary(scope, uuid, rec, "expired", log)
			continue
		}
		withFile = append(withFile, ranked{uuid, rec, count, last})
	}
	sort.Slice(withFile, func(i, j int) bool {
		a, b := withFile[i], withFile[j]
		if a.count != b.count {
			return a.count > b.count
		}
		return a.last > b.last
	})
	topN := config.MemoryPhase2TopN(cfg)
	for i := topN; i < len(withFile); i++ {
		removeSummary(scope, withFile[i].uuid, withFile[i].rec, "ranked out", log)
	}
}

func removeSummary(scope, uuid string, rec *memory.ExtractRecord, why string, log func(string, ...any)) {
	p := filepath.Join(scope, rec.SummaryFile)
	if err := os.Remove(p); err == nil || os.IsNotExist(err) {
		log("memory: forgetting %s (%s)", rec.SummaryFile, why)
		_ = memory.UpdateState(scope, func(st *memory.State) error {
			delete(st.Extracted, uuid)
			delete(st.Files, rec.SummaryFile)
			return nil
		})
	}
}

// runConsolidationAgent spawns the restricted subagent: cwd locked to the
// memory scope, tools limited to read/grep/write/edit behind a path guard,
// no shell, no network, no MCP, no nested agents, usage accounting off.
func runConsolidationAgent(ctx context.Context, cfg *config.Config, scope, diffText string, notes []memory.NoteFile, log func(string, ...any)) ([]decision, error) {
	providerModel := pipelineModel(cfg)
	factory := internalmodel.NewModelFactory(cfg, nil)
	cm, err := factory.GetModel(ctx, providerModel)
	if err != nil {
		return nil, fmt.Errorf("model %q unavailable: %w", providerModel, err)
	}

	env := tools.NewEnv(scope, "local")
	toolset := []einotool.BaseTool{
		env.NewReadTool(), env.NewGrepTool(), env.NewWriteTool(), env.NewEditTool(),
	}
	ag, err := agent.NewAgent(ctx, cm, toolset, consolidationSystemPrompt,
		nil, // no approval gate: the path guard is the containment
		[]adk.ChatModelAgentMiddleware{memory.NewPathGuardMiddleware(scope)},
		nil,
	)
	if err != nil {
		return nil, err
	}

	mode := "INCREMENTAL"
	if !fileNonEmpty(filepath.Join(scope, memory.IndexFile)) {
		mode = "INIT"
	}
	var inv strings.Builder
	fmt.Fprintf(&inv, "MODE: %s\nWORKSPACE: %s\nTODAY: %s\n\n", mode, scope, time.Now().Format("2006-01-02"))
	if len(notes) > 0 {
		inv.WriteString("## Inbox notes to digest (will be deleted after this run)\n")
		for _, n := range notes {
			fmt.Fprintf(&inv, "- notes/%s [kind=%s source=%s]\n", n.Name, n.Kind, n.Source)
		}
		inv.WriteString("\n")
	}
	inv.WriteString(diffText)

	runCtx := memory.WithoutUsageAccounting(ctx)
	tk := &internalmodel.TokenUsage{}
	runCtx = internalmodel.WithTokenTracker(runCtx, tk)

	final, err := driveAgent(runCtx, ag, inv.String())

	// Book the spend regardless of outcome.
	_, _, tok := tk.Get()
	today := time.Now().Format("2006-01-02")
	_ = memory.UpdateState(scope, func(st *memory.State) error {
		if st.Budget == nil {
			st.Budget = map[string]int64{}
		}
		st.Budget[today] += tok
		return nil
	})
	if err != nil {
		return nil, err
	}

	var dl decisionList
	if m := firstJSONObject(final); m != "" {
		if err := json.Unmarshal([]byte(m), &dl); err != nil {
			log("memory: could not parse consolidation decisions: %v", err)
		}
	}
	if len(dl.Decisions) == 0 {
		log("memory: consolidation agent returned no decision protocol (continuing; artifacts are validated separately)")
	}
	return dl.Decisions, nil
}

// driveAgent runs one adk agent turn to completion and returns the final
// assistant text (same iteration pattern as the subagent tool).
func driveAgent(ctx context.Context, ag *adk.ChatModelAgent, prompt string) (string, error) {
	iter := ag.Run(ctx, &adk.AgentInput{
		Messages:        []adk.Message{schema.UserMessage(prompt)},
		EnableStreaming: false,
	})
	var finalText strings.Builder
	steps := 0
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			return finalText.String(), ev.Err
		}
		steps++
		if steps > maxAgentIterations*2 {
			return finalText.String(), fmt.Errorf("consolidation agent exceeded step limit")
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mo := ev.Output.MessageOutput
		if mo.Role != schema.Assistant {
			continue
		}
		if mo.IsStreaming {
			var sb strings.Builder
			for {
				chunk, err := mo.MessageStream.Recv()
				if err != nil {
					break
				}
				if chunk != nil {
					sb.WriteString(chunk.Content)
				}
			}
			if sb.Len() > 0 {
				// keep only the last assistant message (the decision JSON)
				finalText.Reset()
				finalText.WriteString(sb.String())
			}
			continue
		}
		if mo.Message != nil && mo.Message.Content != "" {
			// keep only the last assistant message (the decision JSON)
			finalText.Reset()
			finalText.WriteString(mo.Message.Content)
		}
	}
	return finalText.String(), nil
}

func fileNonEmpty(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Size() > 0
}
