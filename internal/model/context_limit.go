package model

import "github.com/cnjack/jcode/internal/config"

// DefaultContextLimitFallback is the conservative context window assumed when a
// model's true limit cannot be determined from the registry, built-in tables, or
// user config. Kept deliberately small so unknown models compact early rather than
// overflowing the provider's real window. Override per-model via config.ContextLimits
// or globally via config.DefaultContextLimit.
const DefaultContextLimitFallback = 200000

// DefaultOutputReserveTokens is the headroom carved out of the raw context
// window before compaction/summarization threshold math. Without it, an
// aggressive threshold (e.g. 0.95) leaves no room for the model's next output
// or the summarizer's own response, so the real window can overflow before
// compaction fires. Mirrors Claude Code's min(maxOutputTokens, 20000) formula
// in its no-per-model-data degenerate form.
const DefaultOutputReserveTokens = 20000

// EffectiveContextLimit returns the usable window for compaction trigger math:
// the raw limit minus an output reserve of min(DefaultOutputReserveTokens,
// limit/4). The quarter clamp protects small windows (a 16k model reserves 4k,
// not 20k) and guarantees the result stays positive. Non-positive limits are
// passed through unchanged (they mean "unknown/disabled" to callers).
func EffectiveContextLimit(limit int) int {
	if limit <= 0 {
		return limit
	}
	reserve := DefaultOutputReserveTokens
	if quarter := limit / 4; quarter < reserve {
		reserve = quarter
	}
	return limit - reserve
}

// ResolveContextLimit determines the effective context window (in tokens) for a
// provider/model pair. This is the single source of truth for window-size
// management — all middleware thresholds (compaction, summarization, reduction,
// reminders) derive from it.
//
// Resolution order (first positive hit wins):
//  1. explicit user override: cfg.ContextLimits["provider/model"], then cfg.ContextLimits["model"]
//  2. models.dev registry metadata (reg.GetModelContextLimit)
//  3. built-in knownModels fallback table (GetModelContextLimit)
//  4. cfg.DefaultContextLimit, else DefaultContextLimitFallback
//
// reg and cfg may be nil; the resolver degrades gracefully.
func ResolveContextLimit(reg *ModelRegistry, cfg *config.Config, providerID, modelID string) int {
	if cfg != nil && len(cfg.ContextLimits) > 0 {
		if v, ok := cfg.ContextLimits[providerID+"/"+modelID]; ok && v > 0 {
			return v
		}
		if v, ok := cfg.ContextLimits[modelID]; ok && v > 0 {
			return v
		}
	}
	if reg != nil {
		if v := reg.GetModelContextLimit(providerID, modelID); v > 0 {
			return v
		}
	}
	if v := GetModelContextLimit(modelID); v > 0 {
		return v
	}
	if cfg != nil && cfg.DefaultContextLimit > 0 {
		return cfg.DefaultContextLimit
	}
	return DefaultContextLimitFallback
}
