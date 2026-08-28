package command

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

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
			recordTitleUsage(cfg, id, project, tracker)
		}()
	})
}

// recordTitleUsage attributes a small-model title call's spend to the session
// the suggestion belonged to, not whatever the recorder points at now.
func recordTitleUsage(cfg *config.Config, sessionID, project string, tracker *internalmodel.TokenUsage) {
	if d := tracker.GetFull(); d.TotalTokens > 0 {
		usage.RecordEvent(usage.Event{
			Session:    sessionID,
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
}

// Errors returned by suggestSessionTitle so transports can give the user an
// actionable reason instead of a generic failure.
var (
	// ErrNoSmallModel means no small_model is configured; suggestions are
	// unavailable but manual renames still work.
	ErrNoSmallModel = errors.New("no small model configured (set small_model to enable title suggestions)")
	// ErrEmptyConversation means the session has no user/assistant text yet.
	ErrEmptyConversation = errors.New("no conversation yet to suggest a title from")
)

// titleTurnsFromHistory filters an agent history into the shared title-suggestion
// contract: user and assistant text only. System prompts, tool calls and tool
// results — including MCP servers', teammates' and Guardian's traffic — never
// reach the title model, so nothing privileged can leak into a title.
func titleTurnsFromHistory(history []adk.Message) []internalmodel.TitleMsg {
	out := make([]internalmodel.TitleMsg, 0, len(history))
	for _, m := range history {
		if m == nil || m.Content == "" {
			continue
		}
		switch m.Role {
		case schema.User:
			out = append(out, internalmodel.TitleMsg{Role: "user", Content: m.Content})
		case schema.Assistant:
			out = append(out, internalmodel.TitleMsg{Role: "assistant", Content: m.Content})
		}
	}
	return out
}

// suggestSessionTitle generates an editable /rename suggestion for the current
// conversation via the small model. It never writes the title — persistence
// happens only after the user confirms or edits (setSessionTitle), so a failed
// or cancelled suggestion always leaves the existing title untouched. Safe to
// call while a turn is running: it snapshots the history under historyMu and
// applies its own hard timeout.
func (s *interactiveState) suggestSessionTitle(ctx context.Context) (string, error) {
	turns := titleTurnsFromHistory(s.snapshotHistory())
	if len(internalmodel.TitleTurns(turns)) == 0 {
		return "", ErrEmptyConversation
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}
	if cfg.SmallModel == "" {
		return "", ErrNoSmallModel
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	factory := internalmodel.NewModelFactory(cfg, nil)
	cm, err := factory.GetModel(ctx, internalmodel.SmallModelAlias)
	if err != nil || cm == nil {
		return "", fmt.Errorf("small model init failed: %w", err)
	}
	tracker := &internalmodel.TokenUsage{}
	ctx = internalmodel.WithTokenTracker(ctx, tracker)
	title := internalmodel.GenerateSessionTitleFromConversation(ctx, cm, turns)
	if title == "" {
		return "", errors.New("title generation failed (timeout or empty response)")
	}
	if s.rec != nil {
		recordTitleUsage(cfg, s.rec.UUID(), s.rec.Project(), tracker)
	}
	return title, nil
}

// setSessionTitle persists an explicit, user-confirmed session title (TUI
// /rename). The title is sanitized with the shared cross-transport rule before
// it is stored. Empty results are rejected so a rename can never blank a title.
func (s *interactiveState) setSessionTitle(title string) (string, error) {
	cleaned := internalmodel.SanitizeTitle(title)
	if cleaned == "" {
		return "", errors.New("title is empty after cleanup")
	}
	if s.rec == nil {
		return "", errors.New("session recording is unavailable")
	}
	id := s.rec.UUID()
	if err := s.rec.SetUserTitle(id, cleaned); err != nil {
		return "", err
	}
	return cleaned, nil
}
