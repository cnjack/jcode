package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/usage"
)

func TestUsageStatsEndpoint(t *testing.T) {
	today := usage.Today()
	// seedIndex (from tasks_test.go) points HOME at a temp dir AND writes a
	// session index, so countSessions sees exactly one session.
	seedIndex(t, map[string][]session.SessionMeta{
		"/p": {{UUID: "u1", Project: "/p", StartTime: today + "T10:00:00Z"}},
	})

	store := usage.NewStore(filepath.Join(t.TempDir(), "events.jsonl"))
	mustRecord(t, store, usage.Event{Date: today, Model: "glm-5.2", Project: "/p", Prompt: 1000, Cached: 800, Completion: 200, Total: 1200, Calls: 2})
	mustRecord(t, store, usage.Event{Date: today, Model: "glm-5.2", Project: "/p", Prompt: 500, Cached: 500, Completion: 50, Total: 550, Calls: 1})

	s := &Server{usageStore: store}
	rec := httptest.NewRecorder()
	s.handleUsageStats(rec, httptest.NewRequest(http.MethodGet, "/api/usage/stats?days=7", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}

	var resp struct {
		RangeDays int `json:"range_days"`
		Totals    struct {
			TotalTokens int64 `json:"total_tokens"`
			Turns       int64 `json:"turns"`
			Sessions    int64 `json:"sessions"`
		} `json:"totals"`
		ActiveDays     int              `json:"active_days"`
		CurrentStreak  int              `json:"current_streak"`
		MostUsedModel  string           `json:"most_used_model"`
		CacheHitRate   float64          `json:"cache_hit_rate"`
		CacheSupported bool             `json:"cache_supported"`
		Heatmap        []map[string]any `json:"heatmap"`
		ByModel        []map[string]any `json:"by_model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v body=%q", err, rec.Body.String())
	}

	if resp.RangeDays != 7 {
		t.Errorf("range_days = %d, want 7", resp.RangeDays)
	}
	if resp.Totals.TotalTokens != 1750 {
		t.Errorf("total_tokens = %d, want 1750", resp.Totals.TotalTokens)
	}
	if resp.Totals.Turns != 2 {
		t.Errorf("turns = %d, want 2", resp.Totals.Turns)
	}
	if resp.Totals.Sessions != 1 {
		t.Errorf("sessions = %d, want 1", resp.Totals.Sessions)
	}
	if resp.ActiveDays != 1 {
		t.Errorf("active_days = %d, want 1", resp.ActiveDays)
	}
	if resp.CurrentStreak != 1 {
		t.Errorf("current_streak = %d, want 1", resp.CurrentStreak)
	}
	if resp.MostUsedModel != "glm-5.2" {
		t.Errorf("most_used_model = %q, want glm-5.2", resp.MostUsedModel)
	}
	// cached/prompt = 1300/1500 ≈ 0.8667
	if resp.CacheHitRate < 0.86 || resp.CacheHitRate > 0.87 {
		t.Errorf("cache_hit_rate = %v, want ~0.8667", resp.CacheHitRate)
	}
	if !resp.CacheSupported {
		t.Error("cache_supported = false, want true")
	}
	if len(resp.Heatmap) != 1 {
		t.Errorf("heatmap len = %d, want 1 active day", len(resp.Heatmap))
	}
	if len(resp.ByModel) != 1 || resp.ByModel[0]["name"] != "glm-5.2" {
		t.Errorf("by_model = %+v, want one glm-5.2 entry", resp.ByModel)
	}
}

func TestUsageStatsEmpty(t *testing.T) {
	seedIndex(t, map[string][]session.SessionMeta{})
	s := &Server{usageStore: usage.NewStore(filepath.Join(t.TempDir(), "events.jsonl"))}
	rec := httptest.NewRecorder()
	s.handleUsageStats(rec, httptest.NewRequest(http.MethodGet, "/api/usage/stats", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	var resp struct {
		RangeDays int `json:"range_days"`
		Totals    struct {
			TotalTokens int64 `json:"total_tokens"`
		} `json:"totals"`
		CacheSupported bool `json:"cache_supported"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if resp.RangeDays != 30 {
		t.Errorf("default range_days = %d, want 30", resp.RangeDays)
	}
	if resp.Totals.TotalTokens != 0 || resp.CacheSupported {
		t.Errorf("empty stats should be zero/unsupported, got %+v", resp)
	}
}

func mustRecord(t *testing.T, s *usage.Store, ev usage.Event) {
	t.Helper()
	if err := s.Record(ev); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func TestTaskStatsActive(t *testing.T) {
	seedIndex(t, map[string][]session.SessionMeta{})
	rec, err := session.NewRecorder(t.TempDir(), "p", "glm-5.2")
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	tu := &model.TokenUsage{}
	tu.Add(model.AddParams{Prompt: 1000, Completion: 200, Total: 1200, Cached: 800})

	s := &Server{
		Engine: &Engine{
			recorder:   rec,
			tokenUsage: tu,
			breakdownFn: func() usage.ContextBreakdown {
				return usage.ContextBreakdown{SystemPromptTokens: 100, SystemToolsTokens: 200, MCPToolsTokens: 50, SkillsTokens: 30}
			},
		},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+rec.UUID()+"/stats", nil)
	req.SetPathValue("id", rec.UUID())
	s.handleTaskStats(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rr.Code, rr.Body.String())
	}
	var resp struct {
		IsActive bool `json:"is_active"`
		Context  struct {
			SystemPromptTokens int `json:"system_prompt_tokens"`
			MessagesTokens     int `json:"messages_tokens"`
		} `json:"context"`
		CacheHitRate   float64 `json:"cache_hit_rate"`
		CacheSupported bool    `json:"cache_supported"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if !resp.IsActive {
		t.Error("is_active = false, want true for the current session")
	}
	// messages = lastPrompt(1000) - static(100+200+50+30=380) = 620
	if resp.Context.MessagesTokens != 620 {
		t.Errorf("messages_tokens = %d, want 620", resp.Context.MessagesTokens)
	}
	if resp.Context.SystemPromptTokens != 100 {
		t.Errorf("system_prompt_tokens = %d, want 100", resp.Context.SystemPromptTokens)
	}
	if resp.CacheHitRate != 0.8 || !resp.CacheSupported {
		t.Errorf("cache = (%v, %v), want (0.8, true)", resp.CacheHitRate, resp.CacheSupported)
	}
}

func TestTaskStatsHistorical(t *testing.T) {
	today := usage.Today()
	store := usage.NewStore(filepath.Join(t.TempDir(), "events.jsonl"))
	mustRecord(t, store, usage.Event{Date: today, Session: "sess-A", Model: "m", Prompt: 1000, Cached: 700, Completion: 100, Total: 1100, Calls: 1})
	mustRecord(t, store, usage.Event{Date: today, Session: "sess-A", Model: "m", Prompt: 500, Cached: 300, Completion: 50, Total: 550, Calls: 1})
	mustRecord(t, store, usage.Event{Date: today, Session: "sess-B", Model: "m", Prompt: 999, Cached: 0, Completion: 9, Total: 1008, Calls: 1})

	// No recorder → every query is treated as historical.
	s := &Server{Engine: &Engine{}, usageStore: store}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/sess-A/stats", nil)
	req.SetPathValue("id", "sess-A")
	s.handleTaskStats(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d", rr.Code)
	}
	var resp struct {
		IsActive bool `json:"is_active"`
		Tokens   struct {
			TotalTokens int64 `json:"total_tokens"`
			Turns       int64 `json:"turns"`
		} `json:"tokens"`
		CacheHitRate float64 `json:"cache_hit_rate"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if resp.IsActive {
		t.Error("is_active = true, want false")
	}
	if resp.Tokens.TotalTokens != 1650 {
		t.Errorf("total_tokens = %d, want 1650 (only sess-A)", resp.Tokens.TotalTokens)
	}
	if resp.Tokens.Turns != 2 {
		t.Errorf("turns = %d, want 2", resp.Tokens.Turns)
	}
	// cached/prompt = 1000/1500 ≈ 0.6667
	if resp.CacheHitRate < 0.66 || resp.CacheHitRate > 0.67 {
		t.Errorf("cache_hit_rate = %v, want ~0.6667", resp.CacheHitRate)
	}
}
