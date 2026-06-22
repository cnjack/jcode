package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/usage"
)

// usageHeatmapDays is the fixed lookback for the activity heatmap and streak
// computation, independent of the (smaller) totals window.
const usageHeatmapDays = 365

// handleUsageStats returns aggregated global usage statistics. The ?days=N
// query (default 30, capped at the heatmap window) scopes the totals,
// per-model/project breakdowns and the daily trend; the heatmap and streaks
// always span the full lookback so they read consistently across range toggles.
func (s *Server) handleUsageStats(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= usageHeatmapDays {
			days = n
		}
	}

	today := usage.Today()
	now := time.Now()
	heatSince := now.AddDate(0, 0, -(usageHeatmapDays - 1)).Format("2006-01-02")
	windowSince := now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	store := s.usageStore
	if store == nil {
		store = usage.Default()
	}
	events, err := store.Load(heatSince)
	if err != nil {
		config.Logger().Printf("[usage] load failed: %v", err)
		events = nil
	}
	full := usage.Aggregate(events, today) // heatmap + streaks over the full window

	windowEvents := make([]usage.Event, 0, len(events))
	for _, ev := range events {
		if ev.Date >= windowSince {
			windowEvents = append(windowEvents, ev)
		}
	}
	win := usage.Aggregate(windowEvents, today) // totals scoped to the selected range

	resp := map[string]any{
		"range_days": days,
		"totals": map[string]any{
			"total_tokens":      win.Totals.Total,
			"prompt_tokens":     win.Totals.Prompt,
			"completion_tokens": win.Totals.Completion,
			"cached_tokens":     win.Totals.Cached,
			"reasoning_tokens":  win.Totals.Reasoning,
			"calls":             win.Totals.Calls,
			"turns":             win.Totals.Turns,
			"sessions":          countSessions(windowSince),
		},
		"active_days":     win.ActiveDays,
		"current_streak":  full.CurrentStreak,
		"longest_streak":  full.LongestStreak,
		"most_used_model": win.MostUsedModel,
		"cache_hit_rate":  win.CacheHitRate,
		"cache_supported": win.CacheSupported,
		"heatmap":         full.Trend(),
		"daily_trend":     win.Trend(),
		"by_model":        win.ByModel,
		"by_project":      win.ByProject,
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleTaskStats returns per-task statistics. For the ACTIVE task (the current
// recorder's session) it returns a live context-window breakdown plus the live
// cache hit rate. For any other (historical) task it returns a token rollup +
// aggregate hit rate derived from the event log, with is_active=false and no
// breakdown (tool-schema sizes weren't persisted, so a breakdown isn't
// meaningful after the fact).
func (s *Server) handleTaskStats(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	activeUUID := ""
	if s.recorder != nil {
		activeUUID = s.recorder.UUID()
	}
	s.mu.RUnlock()

	resp := map[string]any{"uuid": id}

	if id != "" && id == activeUUID {
		full := s.tokenUsage.GetFull()
		last := s.tokenUsage.GetLastDetail()

		var bd usage.ContextBreakdown
		if s.breakdownFn != nil {
			bd = s.breakdownFn()
		}
		bd.ContextLimit = s.currentModelContextLimit()
		// Messages occupy whatever the last prompt held beyond the static
		// assembly (system prompt + tools + MCP + skills).
		if msg := last.PromptTokens - bd.StaticTotal(); msg > 0 {
			bd.MessagesTokens = msg
		}

		resp["is_active"] = true
		resp["context"] = bd
		resp["cache_hit_rate"] = s.tokenUsage.CacheHitRate()
		resp["cache_supported"] = s.tokenUsage.CacheObserved()
		resp["tokens"] = map[string]any{
			"total_tokens":      full.TotalTokens,
			"prompt_tokens":     full.PromptTokens,
			"completion_tokens": full.CompletionTokens,
			"cached_tokens":     full.CachedTokens,
			"reasoning_tokens":  full.ReasoningTokens,
			"calls":             full.CallCount,
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Historical task: aggregate this session's events.
	store := s.usageStore
	if store == nil {
		store = usage.Default()
	}
	events, _ := store.Load("")
	sel := make([]usage.Event, 0)
	for _, ev := range events {
		if ev.Session == id {
			sel = append(sel, ev)
		}
	}
	agg := usage.Aggregate(sel, usage.Today())
	resp["is_active"] = false
	resp["cache_hit_rate"] = agg.CacheHitRate
	resp["cache_supported"] = agg.CacheSupported
	resp["tokens"] = map[string]any{
		"total_tokens":      agg.Totals.Total,
		"prompt_tokens":     agg.Totals.Prompt,
		"completion_tokens": agg.Totals.Completion,
		"cached_tokens":     agg.Totals.Cached,
		"reasoning_tokens":  agg.Totals.Reasoning,
		"calls":             agg.Totals.Calls,
		"turns":             agg.Totals.Turns,
	}
	writeJSON(w, http.StatusOK, resp)
}

// countSessions counts sessions across all projects whose start date is on or
// after sinceDate (YYYY-MM-DD). The session index is authoritative for the
// session count; the usage log owns token/day metrics.
func countSessions(sinceDate string) int {
	all, err := session.ListAllSessions()
	if err != nil {
		return 0
	}
	n := 0
	for _, metas := range all {
		for _, m := range metas {
			d := m.StartTime
			if len(d) >= 10 {
				d = d[:10]
			}
			if d >= sinceDate {
				n++
			}
		}
	}
	return n
}
