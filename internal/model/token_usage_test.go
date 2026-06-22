package model

import "testing"

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

func TestTokenUsage_Reset(t *testing.T) {
	u := &TokenUsage{}
	u.Add(AddParams{Prompt: 100, Completion: 20, Total: 120, Cached: 80, Reasoning: 5})
	u.AddByModel("m", 100, 20, 120)
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
}
