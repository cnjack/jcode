package command

import (
	"context"
	"time"

	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/usage"
)

// attachTitleRefiner upgrades session titles from first-message truncation to
// LLM generation when a small model is configured. The refiner is async and
// best-effort: any failure leaves the truncated title in place.
//
// All the work — config read, factory, model — happens at fire time, not
// attach time: attaching is free (no registry copy for sessions that never
// fire, e.g. resumed ones), and a small_model added or removed in the config
// file mid-run is honored by the next new session without a restart.
func attachTitleRefiner(ctx context.Context, rec *session.Recorder) {
	if rec == nil {
		return
	}
	rec.SetTitleRefiner(func(firstUserMsg string) {
		// Bind the session identity NOW: the goroutine may outlive a /resume
		// that re-points this recorder at another session (SetTitleFor drops
		// the title when the ids no longer match).
		id, project := rec.UUID(), rec.Project()
		go func() {
			// Never let title polish break or outlive-block a session; detach
			// from the turn's cancellation but keep a hard timeout.
			defer func() { _ = recover() }()
			cfg, err := config.LoadConfig()
			if err != nil || cfg.SmallModel == "" {
				return
			}
			tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
			defer cancel()
			factory := internalmodel.NewModelFactory(cfg, nil)
			cm, err := factory.GetModel(tctx, internalmodel.SmallModelAlias)
			if err != nil || cm == nil {
				config.Logger().Printf("[title] small model init failed: %v", err)
				return
			}
			tracker := &internalmodel.TokenUsage{}
			tctx = internalmodel.WithTokenTracker(tctx, tracker)
			title := internalmodel.GenerateSessionTitle(tctx, cm, firstUserMsg)
			if title == "" {
				return
			}
			rec.SetTitleFor(id, title)
			// Attribute the spend to the small model and to the session the
			// message belonged to, not whatever the recorder points at now.
			if d := tracker.GetFull(); d.TotalTokens > 0 {
				usage.RecordEvent(usage.Event{
					Session:    id,
					Project:    project,
					Model:      internalmodel.BareModelID(cfg.SmallModel),
					Prompt:     d.PromptTokens,
					Completion: d.CompletionTokens,
					Cached:     d.CachedTokens,
					Reasoning:  d.ReasoningTokens,
					CacheWrite: d.CacheWriteTokens,
					Total:      d.TotalTokens,
					Calls:      d.CallCount,
					CacheSeen:  tracker.CacheObserved(),
				})
			}
		}()
	})
}
