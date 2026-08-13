package model

import (
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestTokenUsage_AddAndGetFull(t *testing.T) {
	u := &TokenUsage{}
	u.Add(AddParams{Prompt: 1000, Completion: 200, Total: 1200, Cached: 800, Reasoning: 50})
	u.Add(AddParams{Prompt: 500, Completion: 100, Total: 600, Cached: 500, Reasoning: 10})

	got := u.GetFull()
	if got.PromptTokens != 1500 {
		t.Errorf("PromptTokens = %d, want 1500", got.PromptTokens)
	}
	if got.CompletionTokens != 300 {
		t.Errorf("CompletionTokens = %d, want 300", got.CompletionTokens)
	}
	if got.TotalTokens != 1800 {
		t.Errorf("TotalTokens = %d, want 1800", got.TotalTokens)
	}
	if got.CachedTokens != 1300 {
		t.Errorf("CachedTokens = %d, want 1300", got.CachedTokens)
	}
	if got.ReasoningTokens != 60 {
		t.Errorf("ReasoningTokens = %d, want 60", got.ReasoningTokens)
	}
	if got.CallCount != 2 {
		t.Errorf("CallCount = %d, want 2", got.CallCount)
	}
}

func TestTokenUsage_CacheHitRate(t *testing.T) {
	tests := []struct {
		name   string
		params []AddParams
		want   float64
		obs    bool
	}{
		{"no calls", nil, 0, false},
		{"half cached", []AddParams{{Prompt: 1000, Cached: 500}}, 0.5, true},
		{
			"token weighted across calls",
			[]AddParams{{Prompt: 1000, Cached: 900}, {Prompt: 1000, Cached: 100}},
			0.5, true,
		},
		{"no cache reported", []AddParams{{Prompt: 1000, Cached: 0}}, 0, false},
		{"clamp over one", []AddParams{{Prompt: 100, Cached: 250}}, 1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := &TokenUsage{}
			for _, p := range tc.params {
				u.Add(p)
			}
			if got := u.CacheHitRate(); got != tc.want {
				t.Errorf("CacheHitRate() = %v, want %v", got, tc.want)
			}
			if got := u.CacheObserved(); got != tc.obs {
				t.Errorf("CacheObserved() = %v, want %v", got, tc.obs)
			}
		})
	}
}

func TestTokenUsageDetail_Minus(t *testing.T) {
	cur := TokenUsageDetail{PromptTokens: 1500, CompletionTokens: 300, TotalTokens: 1800, CachedTokens: 1300, ReasoningTokens: 60, CallCount: 3}
	prev := TokenUsageDetail{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200, CachedTokens: 800, ReasoningTokens: 50, CallCount: 2}
	d := cur.Minus(prev)
	if d.PromptTokens != 500 || d.CompletionTokens != 100 || d.TotalTokens != 600 || d.CachedTokens != 500 || d.ReasoningTokens != 10 || d.CallCount != 1 {
		t.Errorf("Minus() = %+v, want deltas {500,100,600,500,10,1}", d)
	}
}

func TestTokenUsage_ResetContext_PreservesLedger(t *testing.T) {
	u := &TokenUsage{}
	u.Add(AddParams{Prompt: 1000, Completion: 200, Total: 1200, Cached: 800, CacheDetailsPresent: true})
	u.Add(AddParams{Prompt: 500, Completion: 100, Total: 600})
	u.AddByModel("m", 1500, 300, 1800)

	u.ResetContext()

	// Cumulative ledger must survive a context reset (the compaction case).
	if got := u.GetFull(); got.PromptTokens != 1500 || got.CompletionTokens != 300 || got.TotalTokens != 1800 || got.CallCount != 2 {
		t.Errorf("ResetContext wiped the cumulative ledger: %+v", got)
	}
	if !u.CacheObserved() {
		t.Errorf("ResetContext should not clear the cache-support flag")
	}
	if u.GetByModel() == nil {
		t.Errorf("ResetContext should not clear the per-model breakdown")
	}
	// Only the current-occupancy snapshot is cleared.
	if got := u.GetLastTotal(); got != 0 {
		t.Errorf("GetLastTotal after ResetContext = %d, want 0", got)
	}
	if got := u.GetLastDetail(); got.PromptTokens != 0 || got.CompletionTokens != 0 {
		t.Errorf("GetLastDetail after ResetContext = %+v, want zero", got)
	}
}

func TestTokenUsage_TurnUsage(t *testing.T) {
	u := &TokenUsage{}
	u.Add(AddParams{Prompt: 1000, Completion: 200, Cached: 100})

	u.BeginTurn() // baseline at turn start

	u.Add(AddParams{Prompt: 2000, Completion: 300, Cached: 500})
	u.Add(AddParams{Prompt: 2000, Completion: 100, Cached: 500})

	p, c, cached := u.TurnUsage()
	if p != 4000 || c != 400 || cached != 1000 {
		t.Errorf("TurnUsage() = (%d,%d,%d), want (4000,400,1000)", p, c, cached)
	}

	// A mid-turn Reset zeroes cumulative AND baseline together; the delta must
	// clamp to >=0, never go negative.
	u.Reset()
	u.Add(AddParams{Prompt: 50, Completion: 10})
	if p, c, _ := u.TurnUsage(); p < 0 || c < 0 {
		t.Errorf("TurnUsage() after Reset = (%d,%d), must not be negative", p, c)
	}
}

func TestTokenUsage_CacheObserved_DetailsButZeroHit(t *testing.T) {
	u := &TokenUsage{}
	// Provider reported a details object but served 0 cached tokens (cold cache).
	u.Add(AddParams{Prompt: 1000, Completion: 100, Cached: 0, CacheDetailsPresent: true})
	if !u.CacheObserved() {
		t.Errorf("CacheObserved() = false, want true when details present even at 0 hits")
	}
	if got := u.CacheHitRate(); got != 0 {
		t.Errorf("CacheHitRate() = %v, want 0", got)
	}
}

func TestTokenUsage_Reset(t *testing.T) {
	u := &TokenUsage{}
	u.Add(AddParams{Prompt: 100, Completion: 20, Total: 120, Cached: 80, Reasoning: 5})
	u.AddByModel("m", 100, 20, 120)
	if u.GetLastModel() != "m" {
		t.Errorf("GetLastModel() = %q, want m", u.GetLastModel())
	}
	u.Reset()
	if got := u.GetFull(); got.PromptTokens != 0 || got.CachedTokens != 0 || got.CallCount != 0 {
		t.Errorf("after Reset GetFull() = %+v, want zero", got)
	}
	if u.GetByModel() != nil {
		t.Errorf("after Reset GetByModel() should be nil")
	}
	if u.CacheObserved() {
		t.Errorf("after Reset CacheObserved() should be false")
	}
	if u.GetLastModel() != "" {
		t.Errorf("after Reset GetLastModel() = %q, want empty", u.GetLastModel())
	}
}

func TestExtractUsage_ReadsAPITotalAndDetails(t *testing.T) {
	got := extractUsage(openai.Usage{
		PromptTokens:     19,
		CompletionTokens: 10,
		TotalTokens:      29,
		PromptTokensDetails: &openai.PromptTokensDetails{
			CachedTokens: 4,
		},
		CompletionTokensDetails: &openai.CompletionTokensDetails{
			ReasoningTokens: 3,
		},
	})
	if got.Prompt != 19 || got.Completion != 10 || got.Total != 29 || got.Cached != 4 || got.Reasoning != 3 {
		t.Errorf("extractUsage() = %+v, want prompt/completion/total from API and cached/reasoning from details", got)
	}
	if !got.CacheDetailsPresent {
		t.Error("CacheDetailsPresent = false, want true when prompt_tokens_details is present")
	}
}

func TestExtractUsage_DerivesTotalWhenAPIOmitsIt(t *testing.T) {
	got := extractUsage(openai.Usage{PromptTokens: 19, CompletionTokens: 10})
	if got.Total != 29 {
		t.Errorf("extractUsage() Total = %d, want 19+10 when API total_tokens is 0", got.Total)
	}
}
