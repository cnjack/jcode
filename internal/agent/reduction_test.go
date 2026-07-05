package agent

import (
	"math"
	"testing"

	internalmodel "github.com/cnjack/jcode/internal/model"
)

// ReductionThreshold is the single source for deriving the reduction (tool
// output clearing) trigger fraction from the compaction threshold. It must
// reproduce the formula previously copy-pasted across the three surfaces:
// compactThreshold-0.15, falling back to compactThreshold*0.8 when the result
// would drop below 0.1.
func TestReductionThreshold(t *testing.T) {
	cases := []struct {
		name             string
		compactThreshold float64
		want             float64
	}{
		{"typical", 0.8, 0.65},
		{"boundary no fallback", 0.25, 0.10},
		{"fallback", 0.2, 0.16},
		{"low threshold fallback", 0.1, 0.08},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReductionThreshold(tc.compactThreshold)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("ReductionThreshold(%v) = %v, want %v", tc.compactThreshold, got, tc.want)
			}
		})
	}
}

// BuildReductionConfig is the single source for the reduction.Config the three
// surfaces (TUI/ACP/web) previously each hand-rolled (and let drift). It must
// fill every field consistently, and its clear trigger must be based on the
// EFFECTIVE context limit (output headroom reserved) — the same base the
// summarization trigger uses — not the raw window.
func TestBuildReductionConfig(t *testing.T) {
	cfg := BuildReductionConfig("/x", 200000, 0.8, nil)

	// 200000 raw → 180000 effective (20000 output reserve); trigger sits at the
	// reduction threshold fraction of the effective window.
	wantClear := int64(float64(180000) * ReductionThreshold(0.8))
	if cfg.MaxTokensForClear != wantClear {
		t.Errorf("MaxTokensForClear = %d, want %d", cfg.MaxTokensForClear, wantClear)
	}
	if cfg.MaxLengthForTrunc != 50000 {
		t.Errorf("MaxLengthForTrunc = %d, want 50000", cfg.MaxLengthForTrunc)
	}
	if cfg.ReadFileToolName != "read" {
		t.Errorf("ReadFileToolName = %q, want \"read\"", cfg.ReadFileToolName)
	}
	if cfg.RootDir != "/x" {
		t.Errorf("RootDir = %q, want \"/x\"", cfg.RootDir)
	}
	// The trunc exclusion list previously only existed on the TUI surface —
	// the builder must set it for everyone.
	wantExclude := map[string]bool{"ask_user": true, "load_skill": true}
	if len(cfg.TruncExcludeTools) != len(wantExclude) {
		t.Fatalf("TruncExcludeTools = %v, want exactly %v", cfg.TruncExcludeTools, wantExclude)
	}
	for _, name := range cfg.TruncExcludeTools {
		if !wantExclude[name] {
			t.Errorf("unexpected TruncExcludeTools entry %q", name)
		}
	}
	rc, ok := cfg.ToolConfig["read"]
	if !ok || rc == nil || !rc.SkipClear {
		t.Errorf("ToolConfig[\"read\"].SkipClear not set: %+v", cfg.ToolConfig)
	}
	backend, ok := cfg.Backend.(*LocalReductionBackend)
	if !ok {
		t.Fatalf("Backend is %T, want *LocalReductionBackend", cfg.Backend)
	}
	if backend.RootDir != "/x" {
		t.Errorf("Backend.RootDir = %q, want \"/x\"", backend.RootDir)
	}
	if cfg.TokenCounter != nil {
		t.Errorf("TokenCounter should stay nil when no counter is injected (eino default applies)")
	}
}

// A non-nil counter must be passed through verbatim so the surfaces can inject
// the calibrated estimator.
func TestBuildReductionConfig_CounterPassthrough(t *testing.T) {
	counter := internalmodel.NewCalibratedCounter(&internalmodel.TokenUsage{})
	cfg := BuildReductionConfig("/x", 200000, 0.8, counter.Count)
	if cfg.TokenCounter == nil {
		t.Fatal("TokenCounter = nil, want the injected counter")
	}
}
