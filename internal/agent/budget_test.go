package agent

import (
	"testing"

	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
)

// TestBudgetCost_CacheReadDiscount verifies the cached subset of the prompt is
// billed at the discounted cache-read rate when the registry has it, and at the
// full input rate otherwise (S3).
func TestBudgetCost_CacheReadDiscount(t *testing.T) {
	bm := NewBudgetManager(nil, internalmodel.ModelPricing{InputPer1M: 10, OutputPer1M: 30, CacheReadPer1M: 1})
	// 1000 prompt (800 cached) + 100 completion.
	got := bm.costLocked(1000, 100, 800)
	want := float64(200)*10/1e6 + float64(800)*1/1e6 + float64(100)*30/1e6
	if got != want {
		t.Errorf("discounted cost = %v, want %v", got, want)
	}

	// No cache pricing → cached billed at full input rate (no discount applied).
	plain := NewBudgetManager(nil, internalmodel.ModelPricing{InputPer1M: 10, OutputPer1M: 30})
	got = plain.costLocked(1000, 100, 800)
	want = float64(1000)*10/1e6 + float64(100)*30/1e6
	if got != want {
		t.Errorf("no-discount cost = %v, want %v", got, want)
	}

	// cached clamped to prompt (never negative uncached portion).
	if c := bm.costLocked(100, 0, 999); c < 0 {
		t.Errorf("cached>prompt produced negative cost: %v", c)
	}
}

// TestBudget_MaxTokensPerTurn verifies the per-turn cap trips on the (per-turn)
// token total it is given, not before (C5: the middleware now feeds it the
// turn delta rather than the session cumulative).
func TestBudget_MaxTokensPerTurn(t *testing.T) {
	bm := NewBudgetManager(&config.BudgetConfig{MaxTokensPerTurn: 1000}, internalmodel.ModelPricing{})

	bm.mu.Lock()
	bm.promptTokens, bm.completionTokens = 600, 300 // 900 < 1000
	bm.mu.Unlock()
	if _, exceeded := bm.Check(); exceeded {
		t.Error("900 tokens should not exceed a 1000 per-turn cap")
	}

	bm.mu.Lock()
	bm.completionTokens = 500 // 1100 >= 1000
	bm.mu.Unlock()
	if _, exceeded := bm.Check(); !exceeded {
		t.Error("1100 tokens should exceed a 1000 per-turn cap")
	}
}
