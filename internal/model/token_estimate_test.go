package model

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

// EstimateTokens must be deterministic, slightly more conservative than the
// old len/4 byte heuristic for dense ASCII (code/JSON/base64), and must stop
// underestimating CJK by ~2x (len/4 counts a 3-byte CJK rune as 0.75 tokens;
// real tokenizers sit around 1+ token per CJK char).
func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int64
	}{
		{"empty", "", 0},
		{"pure ascii 360 chars", strings.Repeat("a", 360), 100},
		{"pure cjk 100 runes", strings.Repeat("好", 100), 100},
		// 7 ASCII (70/36=1) + 2 CJK runes = 3.
		{"mixed", "hello, 世界", 3},
		// 3 ASCII (30/36=0) + 3 CJK = 3.
		{"mixed small ascii", "abc一二三", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EstimateTokens(tc.in); got != tc.want {
				t.Fatalf("EstimateTokens(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}

	// Dense ASCII (base64-ish) must estimate at or above the old len/4 value.
	dense := strings.Repeat("QUJDREVGR0hJSktMTU5PUA==", 20) // 480 ASCII bytes
	if got, old := EstimateTokens(dense), int64(len(dense)/4); got < old {
		t.Fatalf("EstimateTokens(dense ascii) = %d, want >= len/4 = %d", got, old)
	}
}

// asciiMsg builds a user message whose estimate is exactly nASCII*10/36 + 1
// (role "user" contributes 40/36 = 1).
func asciiMsg(nASCII int) *schema.Message {
	return &schema.Message{Role: schema.User, Content: strings.Repeat("a", nASCII)}
}

// The scale must walk toward the provider-reported total via EMA when the
// provider reports usage, so returned counts converge on reality regardless
// of which vendor's tokenizer is on the other side.
func TestCalibratedCounter_ScaleConverges(t *testing.T) {
	msgs := []*schema.Message{asciiMsg(36000)} // raw estimate E = 10001
	const E = int64(10001)

	tu := &TokenUsage{}
	tu.Add(AddParams{Total: int(2 * E)}) // provider says the window is really 2E

	c := NewCalibratedCounter(tu)
	ctx := context.Background()

	v1, err := c.Count(ctx, msgs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v1 != E {
		t.Fatalf("first count = %d, want raw estimate %d (scale starts at 1.0)", v1, E)
	}
	v2, _ := c.Count(ctx, msgs, nil)
	v3, _ := c.Count(ctx, msgs, nil)
	if v1 >= v2 || v2 >= v3 {
		t.Fatalf("counts not monotonically converging up: %d, %d, %d", v1, v2, v3)
	}
	if v3 > 2*E {
		t.Fatalf("count %d overshot provider total %d", v3, 2*E)
	}
	if v3 > 3*E {
		t.Fatalf("count %d exceeded clamp upper bound 3*raw = %d", v3, 3*E)
	}
}

func TestCalibratedCounter_ClampAndFloor(t *testing.T) {
	ctx := context.Background()

	t.Run("upper clamp at 3.0", func(t *testing.T) {
		msgs := []*schema.Message{asciiMsg(36000)} // E = 10001
		tu := &TokenUsage{}
		tu.Add(AddParams{Total: 1000100}) // absurd: 100x the estimate
		c := NewCalibratedCounter(tu)
		var last int64
		for i := 0; i < 6; i++ {
			last, _ = c.Count(ctx, msgs, nil)
		}
		if c.scale > 3.0 {
			t.Fatalf("scale = %v, want clamped <= 3.0", c.scale)
		}
		if want := int64(float64(10001) * 3.0); last != want {
			t.Fatalf("count = %d, want %d (raw * clamped scale)", last, want)
		}
	})

	t.Run("lower clamp at 0.5", func(t *testing.T) {
		msgs := []*schema.Message{asciiMsg(360000)} // E = 100001
		tu := &TokenUsage{}
		tu.Add(AddParams{Total: 5000}) // provider claims 5% of the estimate
		c := NewCalibratedCounter(tu)
		for i := 0; i < 8; i++ {
			_, _ = c.Count(ctx, msgs, nil)
		}
		if c.scale != 0.5 {
			t.Fatalf("scale = %v, want clamped at 0.5", c.scale)
		}
	})

	t.Run("no calibration below 5000 provider tokens", func(t *testing.T) {
		msgs := []*schema.Message{asciiMsg(36000)}
		tu := &TokenUsage{}
		tu.Add(AddParams{Total: 4000}) // too small: noise
		c := NewCalibratedCounter(tu)
		_, _ = c.Count(ctx, msgs, nil)
		_, _ = c.Count(ctx, msgs, nil)
		if c.scale != 1.0 {
			t.Fatalf("scale = %v, want 1.0 (no calibration under noise floor)", c.scale)
		}
	})

	t.Run("no calibration when provider reports nothing", func(t *testing.T) {
		msgs := []*schema.Message{asciiMsg(36000)}
		c := NewCalibratedCounter(&TokenUsage{})
		_, _ = c.Count(ctx, msgs, nil)
		_, _ = c.Count(ctx, msgs, nil)
		if c.scale != 1.0 {
			t.Fatalf("scale = %v, want 1.0 (GetLastTotal is 0)", c.scale)
		}
	})

	t.Run("nil tracker degrades to static estimate", func(t *testing.T) {
		msgs := []*schema.Message{asciiMsg(36000)}
		c := NewCalibratedCounter(nil)
		got, err := c.Count(ctx, msgs, nil)
		if err != nil || got != 10001 {
			t.Fatalf("Count = %d, %v; want 10001, nil", got, err)
		}
	})
}

// eino re-counts the message SUBSET after a clear pass with the same counter;
// that subset count must not be mistaken for a full-window measurement and
// pollute the calibration scale.
func TestCalibratedCounter_SubsetCountNoRecalibrate(t *testing.T) {
	full := []*schema.Message{asciiMsg(18000), asciiMsg(18000)} // raw = 2*5001 = 10002
	tu := &TokenUsage{}
	tu.Add(AddParams{Total: 20000})
	c := NewCalibratedCounter(tu)
	ctx := context.Background()

	_, _ = c.Count(ctx, full, nil) // establishes lastFullEstimate
	_, _ = c.Count(ctx, full, nil) // calibrates
	scaleBefore := c.scale
	lastFullBefore := c.lastFullEstimate

	_, _ = c.Count(ctx, full[:1], nil) // subset re-count (post-clear)
	if c.scale != scaleBefore {
		t.Fatalf("subset count changed scale: %v -> %v", scaleBefore, c.scale)
	}
	if c.lastFullEstimate != lastFullBefore {
		t.Fatalf("subset count changed lastFullEstimate: %d -> %d", lastFullBefore, c.lastFullEstimate)
	}
}

// Tool call arguments ride in the request too — they must be counted.
func TestCalibratedCounter_WalksToolCalls(t *testing.T) {
	ctx := context.Background()
	base := &schema.Message{Role: schema.Assistant, Content: "ok"}
	withCalls := &schema.Message{
		Role:    schema.Assistant,
		Content: "ok",
		ToolCalls: []schema.ToolCall{
			{ID: "c1", Function: schema.FunctionCall{Name: "read", Arguments: strings.Repeat("x", 3600)}},
		},
	}

	plain, _ := NewCalibratedCounter(nil).Count(ctx, []*schema.Message{base}, nil)
	loaded, _ := NewCalibratedCounter(nil).Count(ctx, []*schema.Message{withCalls}, nil)
	if loaded <= plain {
		t.Fatalf("tool call arguments not counted: with=%d, without=%d", loaded, plain)
	}
}
