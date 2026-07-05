package model

import (
	"context"
	"sync"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
)

// Token estimation for the reduction middleware's TokenCounter.
//
// jcode is multi-provider (Anthropic, Qwen, GLM, Kimi, MiniMax, ...), so no
// single embedded BPE tokenizer (cl100k/o200k) would be correct — and shipping
// one costs either network access or 2-4MB of embedded data for a value that
// is still approximate. Instead we use a zero-dependency two-level scheme:
//
//  1. EstimateTokens: a static heuristic that fixes the two known failure
//     modes of eino's len/4 default — CJK underestimation (a 3-byte CJK rune
//     is ~1+ token, not 0.75) and dense ASCII (code/JSON) underestimation.
//  2. CalibratedCounter: wraps the heuristic with an EMA scale calibrated
//     against the provider's own reported usage (TokenUsage.GetLastTotal),
//     adapting to whatever tokenizer is actually on the other side.

// EstimateTokens estimates the token count of s: ASCII characters count as
// 1/3.6 token each (slightly more conservative than the classic len/4 for
// code/JSON-dense text), and every non-ASCII rune (CJK etc.) counts as a full
// token. Implemented in integer arithmetic (n*10/36 == n/3.6) so results are
// deterministic across platforms.
func EstimateTokens(s string) int64 {
	if s == "" {
		return 0
	}
	var ascii, other int64
	for _, r := range s {
		if r < utf8.RuneSelf {
			ascii++
		} else {
			other++
		}
	}
	return ascii*10/36 + other
}

// estimateMessageTokens walks the same message parts as eino's default token
// counter (role, reasoning, content, tool call names+arguments, multimodal
// text parts), summing EstimateTokens over each. Tool schema definitions are
// deliberately not counted: they are a per-session constant that the
// calibration scale absorbs.
func estimateMessageTokens(msg *schema.Message) int64 {
	if msg == nil {
		return 0
	}
	t := EstimateTokens(string(msg.Role)) +
		EstimateTokens(msg.ReasoningContent) +
		EstimateTokens(msg.Content)
	if msg.Role == schema.Assistant {
		for _, tc := range msg.ToolCalls {
			t += EstimateTokens(tc.Function.Name) + EstimateTokens(tc.Function.Arguments)
		}
	}
	for _, mc := range msg.UserInputMultiContent {
		if mc.Type == schema.ChatMessagePartTypeText {
			t += EstimateTokens(mc.Text)
		}
	}
	for _, mc := range msg.AssistantGenMultiContent {
		if mc.Type == schema.ChatMessagePartTypeText {
			t += EstimateTokens(mc.Text)
		}
	}
	return t
}

const (
	// calibrationAlpha is the EMA weight given to the newest provider/estimate
	// ratio observation.
	calibrationAlpha = 0.3
	// calibrationMinScale / calibrationMaxScale clamp the scale so one absurd
	// provider report can never swing the counter into uselessness.
	calibrationMinScale = 0.5
	calibrationMaxScale = 3.0
	// calibrationMinTokens is the noise floor: provider totals below this are
	// too small for the ratio to be meaningful.
	calibrationMinTokens = 5000
)

// CalibratedCounter is a reduction TokenCounter that self-calibrates the
// static EstimateTokens heuristic against the provider's reported usage.
//
// Calibration only happens on full-window counts (raw >= the previous full
// estimate): eino re-invokes the same counter on message SUBSETS after a clear
// pass to measure how much was reclaimed, and those subset counts must not
// pollute the scale. If the provider never reports usage (GetLastTotal stays
// 0), the counter degrades gracefully to the pure static heuristic.
//
// One instance per agent (it follows the per-agent TokenUsage), so web tasks
// never cross-contaminate each other's scales.
type CalibratedCounter struct {
	tu *TokenUsage

	mu               sync.Mutex
	scale            float64
	lastFullEstimate int64
}

// NewCalibratedCounter returns a counter calibrated against tu. tu may be nil,
// in which case the counter is purely static.
func NewCalibratedCounter(tu *TokenUsage) *CalibratedCounter {
	return &CalibratedCounter{tu: tu, scale: 1.0}
}

// Count implements the reduction.Config TokenCounter signature.
func (c *CalibratedCounter) Count(_ context.Context, msgs []*schema.Message, _ []*schema.ToolInfo) (int64, error) {
	var raw int64
	for _, m := range msgs {
		raw += estimateMessageTokens(m)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if raw >= c.lastFullEstimate {
		// Full-window measurement: a calibration opportunity. The provider's
		// last reported total corresponds to the window measured by the
		// PREVIOUS full estimate, so pair those two.
		if c.tu != nil && c.lastFullEstimate > 0 {
			if last := c.tu.GetLastTotal(); last >= calibrationMinTokens {
				ratio := float64(last) / float64(c.lastFullEstimate)
				c.scale = (1-calibrationAlpha)*c.scale + calibrationAlpha*ratio
				if c.scale < calibrationMinScale {
					c.scale = calibrationMinScale
				}
				if c.scale > calibrationMaxScale {
					c.scale = calibrationMaxScale
				}
			}
		}
		c.lastFullEstimate = raw
	}
	return int64(float64(raw) * c.scale), nil
}
