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
	"errors"
	"fmt"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/memory"
)

// ErrAlreadyRunning reports that another process or goroutine owns the
// project memory pipeline lock. Callers may use errors.Is to surface a stable
// "busy" state without probing the lock first (which would introduce TOCTOU).
var ErrAlreadyRunning = errors.New("memory pipeline already running")

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

type runLease struct {
	cfg        *config.Config
	projectDir string
	scope      string
	opts       Options
	log        func(format string, args ...any)
	release    func()
}

// acquireRun snapshots policy and takes the cross-process pipeline lock before
// any asynchronous work is launched. Keeping acquisition synchronous lets HTTP
// callers report an exact busy result instead of guessing from goroutine timing.
func acquireRun(cfg *config.Config, projectDir string, opts Options) (*runLease, error) {
	// A run can outlive the request/session that launched it. Detach the memory
	// block once so a Settings update cannot change policy mid-run or race the
	// background goroutine.
	cfg = cfg.MemoryPipelineSnapshot()
	log := opts.Log
	if log == nil {
		log = func(string, ...any) {}
	}
	if !config.MemoryGenerate(cfg) {
		return nil, fmt.Errorf("memory pipeline disabled by config")
	}
	// Distillation deliberately does not ride small_model (persistent memories
	// deserve extraction quality) — but users who set small_model to keep this
	// pipeline cheap used to get it implicitly, so leave a breadcrumb about
	// where the spend now goes and how to restore the old behavior.
	memoryCfg := cfg.MemorySettings()
	if cfg != nil && cfg.SmallModel != "" && memoryCfg.Model == "" {
		log("memory: distillation runs on the main model %q (small_model is not used); set memory.model to pin a cheaper model", cfg.Model)
	}
	scope := memory.ProjectRoot(projectDir)
	release, ok, err := memory.TryLockPipeline(scope)
	if err != nil {
		return nil, err
	}
	if !ok {
		log("memory: pipeline already running elsewhere, skipping")
		return nil, ErrAlreadyRunning
	}
	// The pipeline lock lives outside the deletable scope, so acquire it before
	// creating any scope directories. Otherwise a concurrent ClearScope can take
	// the lock after EnsureScope, delete the scope successfully, and return just
	// before this run takes the lock and recreates the supposedly-cleared memory.
	if err := memory.EnsureScope(scope); err != nil {
		release()
		return nil, err
	}
	return &runLease{
		cfg: cfg, projectDir: projectDir, scope: scope, opts: opts,
		log: log, release: release,
	}, nil
}

func (l *runLease) run(ctx context.Context) error {
	defer l.release()
	return runLocked(ctx, l.cfg, l.projectDir, l.scope, l.opts, l.log)
}

// Run executes phase 1 + phase 2 for a project. A non-blocking flock guards
// the whole run; lock contention is reported as ErrAlreadyRunning.
func Run(ctx context.Context, cfg *config.Config, projectDir string, opts Options) error {
	lease, err := acquireRun(cfg, projectDir, opts)
	if err != nil {
		return err
	}
	return lease.run(ctx)
}

// Start acquires the cross-process lock synchronously, then runs the pipeline
// asynchronously. A successful call owns the lock before returning; callers
// receive completion on the buffered channel and may safely report "started".
func Start(ctx context.Context, cfg *config.Config, projectDir string, opts Options) (<-chan error, error) {
	lease, err := acquireRun(cfg, projectDir, opts)
	if err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		defer close(done)
		defer func() {
			if recovered := recover(); recovered != nil {
				done <- fmt.Errorf("memory pipeline panic: %v", recovered)
			}
		}()
		done <- lease.run(ctx)
	}()
	return done, nil
}

func runLocked(
	ctx context.Context,
	cfg *config.Config,
	projectDir, scope string,
	opts Options,
	log func(format string, args ...any),
) error {
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
	snapshot := cfg.MemoryPipelineSnapshot()
	if !config.MemoryGenerate(snapshot) {
		return
	}
	go func() {
		defer func() { _ = recover() }() // memory must never take a session down
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		err := Run(ctx, snapshot, projectDir, Options{Log: func(f string, a ...any) {
			config.Logger().Printf("[memory] "+f, a...)
		}})
		if err != nil && !errors.Is(err, ErrAlreadyRunning) {
			config.Logger().Printf("[memory] background pipeline: %v", err)
		}
	}()
}
