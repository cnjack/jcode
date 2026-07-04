// Package pipeline implements the offline memory distillation pipeline
// (design §5): phase 1 extracts durable facts per ended session with a cheap
// model; phase 2 consolidates them into curated artifacts with a restricted
// subagent, git-diff driven with a zero-token no-op fast path.
//
// It lives in a subpackage because internal/agent and internal/tools import
// internal/memory (usage middleware, note tool); the pipeline needs both.
package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/memory"
)

// Options controls one pipeline run.
type Options struct {
	// IncludeRecent skips the "session idle for 2h / ended" gate — needed by
	// `memory sync` right after a session and by the e2e suite.
	IncludeRecent bool
	// IgnoreCooldown forces a run even within the cooldown window (manual sync).
	IgnoreCooldown bool
	// Log receives progress lines; nil means silent.
	Log func(format string, args ...any)
}

// Run executes phase 1 + phase 2 for a project. Concurrency-safe across
// processes: a non-blocking flock guards the whole run, so concurrent
// sessions simply skip.
func Run(ctx context.Context, cfg *config.Config, projectDir string, opts Options) error {
	log := opts.Log
	if log == nil {
		log = func(string, ...any) {}
	}
	if !config.MemoryGenerate(cfg) {
		return fmt.Errorf("memory pipeline disabled by config")
	}
	scope := memory.ProjectRoot(projectDir)
	if err := memory.EnsureScope(scope); err != nil {
		return err
	}

	release, ok, err := memory.TryLockPipeline(scope)
	if err != nil {
		return err
	}
	if !ok {
		log("memory: pipeline already running elsewhere, skipping")
		return nil
	}
	defer release()

	// Cooldown gate (skipped for manual sync).
	st := memory.LoadState(scope)
	if !opts.IgnoreCooldown && st.LastPipelineAt != "" {
		if ts, err := time.Parse(time.RFC3339, st.LastPipelineAt); err == nil {
			cool := time.Duration(config.MemoryCooldownHours(cfg)) * time.Hour
			if time.Since(ts) < cool {
				log("memory: within cooldown (%s), skipping", cool)
				return nil
			}
		}
	}

	// Once we commit to a run, stamp LastPipelineAt no matter the outcome:
	// a failed run must still start the cooldown clock, otherwise a failing
	// consolidation would rerun on every session start (retry storm) and
	// bypass both the cooldown and — since phase 2's spend is unbounded — the
	// daily budget. Backoff = the normal cooldown window.
	defer func() {
		_ = memory.UpdateState(scope, func(st *memory.State) error {
			st.LastPipelineAt = time.Now().Format(time.RFC3339)
			return nil
		})
	}()

	// Daily budget gate covers the WHOLE pipeline (phase 1 + phase 2).
	today := time.Now().Format("2006-01-02")
	if spent := st.Budget[today]; spent >= int64(config.MemoryDailyTokenBudget(cfg)) {
		log("memory: daily token budget exhausted (%d), skipping run", spent)
		return nil
	}

	n, err := runPhase1(ctx, cfg, projectDir, opts.IncludeRecent, log)
	if err != nil {
		return err
	}
	log("memory: phase 1 wrote %d session summaries", n)

	// Re-check budget before the (most expensive) consolidation agent: phase 1
	// may have consumed the remaining allowance.
	if spent := memory.LoadState(scope).Budget[today]; spent >= int64(config.MemoryDailyTokenBudget(cfg)) {
		log("memory: budget exhausted after phase 1 (%d), skipping phase 2", spent)
		return nil
	}

	return runPhase2(ctx, cfg, projectDir, log)
}

// MaybeStartBackground fires a pipeline run in a goroutine if the gates pass
// (design §5.1): enabled, not a subagent context, cooldown handled inside
// Run. Errors are logged, never surfaced to the session.
func MaybeStartBackground(cfg *config.Config, projectDir string) {
	if !config.MemoryGenerate(cfg) {
		return
	}
	go func() {
		defer func() { _ = recover() }() // memory must never take a session down
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		err := Run(ctx, cfg, projectDir, Options{Log: func(f string, a ...any) {
			config.Logger().Printf("[memory] "+f, a...)
		}})
		if err != nil {
			config.Logger().Printf("[memory] background pipeline: %v", err)
		}
	}()
}
