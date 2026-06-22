package agent

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
)

// WarningLevel indicates the severity of a budget warning.
type WarningLevel int

const (
	WarningNone     WarningLevel = iota
	WarningApproach              // approaching budget limit
	WarningExceeded              // budget limit exceeded
)

// BudgetStatus is a snapshot of current token/cost usage.
type BudgetStatus struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	EstimatedCost    float64
	RemainingBudget  float64
	WarningLevel     WarningLevel
}

// BudgetManager tracks token consumption and cost against configurable limits.
type BudgetManager struct {
	mu                sync.RWMutex
	promptTokens      int64
	completionTokens  int64
	totalCost         float64
	maxTokensPerTurn  int64
	maxCostPerSession float64
	warningThreshold  float64 // fraction (0-1) at which to warn
	pricing           internalmodel.ModelPricing
}

// NewBudgetManager creates a BudgetManager from config and model pricing.
// A nil BudgetConfig results in a manager with no limits.
func NewBudgetManager(cfg *config.BudgetConfig, pricing internalmodel.ModelPricing) *BudgetManager {
	bm := &BudgetManager{
		pricing:          pricing,
		warningThreshold: 0.8, // default 80%
	}
	if cfg != nil {
		bm.maxTokensPerTurn = cfg.MaxTokensPerTurn
		bm.maxCostPerSession = cfg.MaxCostPerSession
		if cfg.WarningThreshold > 0 {
			bm.warningThreshold = cfg.WarningThreshold
		}
	}
	return bm
}

// Track records a model call's token usage and returns the updated status.
func (b *BudgetManager) Track(promptTokens, completionTokens int64) BudgetStatus {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.promptTokens += promptTokens
	b.completionTokens += completionTokens
	b.totalCost += b.costLocked(promptTokens, completionTokens, 0)

	return b.statusLocked()
}

// costLocked computes the USD cost of the given token counts, charging the
// cached (cache-read) subset of the prompt at the discounted CacheRead rate when
// the registry has it (else at the full input rate). Caller must hold the lock.
func (b *BudgetManager) costLocked(promptTokens, completionTokens, cachedTokens int64) float64 {
	if cachedTokens < 0 {
		cachedTokens = 0
	}
	if cachedTokens > promptTokens {
		cachedTokens = promptTokens
	}
	cacheRate := b.pricing.CacheReadPer1M
	if cacheRate <= 0 {
		cacheRate = b.pricing.InputPer1M // no discount data: bill cached at full price
	}
	inputCost := float64(promptTokens-cachedTokens)*b.pricing.InputPer1M/1_000_000 +
		float64(cachedTokens)*cacheRate/1_000_000
	outputCost := float64(completionTokens) * b.pricing.OutputPer1M / 1_000_000
	return inputCost + outputCost
}

// Check returns the current budget status and whether the budget has been exceeded.
func (b *BudgetManager) Check() (BudgetStatus, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	s := b.statusLocked()
	return s, s.WarningLevel >= WarningExceeded
}

// Status returns the current budget status without modifying state.
func (b *BudgetManager) Status() BudgetStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.statusLocked()
}

// statusLocked computes the status snapshot. Caller must hold at least an RLock.
func (b *BudgetManager) statusLocked() BudgetStatus {
	total := b.promptTokens + b.completionTokens
	remaining := float64(0)
	if b.maxCostPerSession > 0 {
		remaining = b.maxCostPerSession - b.totalCost
	}

	wl := WarningNone
	if b.maxCostPerSession > 0 {
		ratio := b.totalCost / b.maxCostPerSession
		if ratio >= 1.0 {
			wl = WarningExceeded
		} else if ratio >= b.warningThreshold {
			wl = WarningApproach
		}
	}
	if wl == WarningNone && b.maxTokensPerTurn > 0 && total >= b.maxTokensPerTurn {
		wl = WarningExceeded
	}

	return BudgetStatus{
		PromptTokens:     b.promptTokens,
		CompletionTokens: b.completionTokens,
		TotalTokens:      total,
		EstimatedCost:    b.totalCost,
		RemainingBudget:  remaining,
		WarningLevel:     wl,
	}
}

// budgetMiddleware is a ChatModelAgentMiddleware that tracks token usage
// after each model invocation and emits warnings when approaching limits.
type budgetMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	manager    *BudgetManager
	tokenUsage *internalmodel.TokenUsage
	onWarn     func(status BudgetStatus)
}

// NewBudgetMiddleware creates a ChatModelAgentMiddleware that tracks budget.
// tokenUsage is the per-agent tracker to read from.
// onWarn is called when the budget warning level changes to WarningApproach
// or WarningExceeded. It may be nil.
func NewBudgetMiddleware(manager *BudgetManager, tokenUsage *internalmodel.TokenUsage, onWarn func(BudgetStatus)) adk.ChatModelAgentMiddleware {
	return &budgetMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		manager:                      manager,
		tokenUsage:                   tokenUsage,
		onWarn:                       onWarn,
	}
}

// AfterModelRewriteState is called after each model invocation. We sync the
// BudgetManager with the per-agent tokenUsage and check budget limits.
func (m *budgetMiddleware) AfterModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	mc *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	var promptTokens, completionTokens int64
	var sessionPrompt, sessionCompletion, sessionCached int64
	if m.tokenUsage != nil {
		// Per-turn delta for the per-agent-turn TOKEN cap (max_tokens_per_turn):
		// runner.BeginTurn sets the baseline at turn start. Reading cumulative
		// Get() here made the "per turn" cap behave as a session total.
		promptTokens, completionTokens, _ = m.tokenUsage.TurnUsage()
		// Session-cumulative for the COST cap (max_cost_per_session): cost must
		// accumulate across turns, not reset each turn.
		full := m.tokenUsage.GetFull()
		sessionPrompt = int64(full.PromptTokens)
		sessionCompletion = int64(full.CompletionTokens)
		sessionCached = int64(full.CachedTokens)
	}

	// promptTokens/completionTokens drive the per-turn token cap; totalCost is the
	// session-cumulative cost (cached subset billed at the cache-read rate).
	m.manager.mu.Lock()
	m.manager.promptTokens = promptTokens
	m.manager.completionTokens = completionTokens
	m.manager.totalCost = m.manager.costLocked(sessionPrompt, sessionCompletion, sessionCached)
	m.manager.mu.Unlock()

	status := m.manager.Status()

	if status.WarningLevel > WarningNone && m.onWarn != nil {
		m.onWarn(status)
	}

	if status.WarningLevel >= WarningApproach {
		config.Logger().Printf("[budget] warning level=%d, cost=%.4f, tokens=%d",
			status.WarningLevel, status.EstimatedCost, status.TotalTokens)
	}

	// Inject a system message warning the agent when approaching limits.
	if status.WarningLevel == WarningApproach {
		state.Messages = append(state.Messages, &schema.Message{
			Role:    schema.System,
			Content: "[Budget Warning] You are approaching the session cost/token limit. Please wrap up your current task efficiently.",
		})
	} else if status.WarningLevel >= WarningExceeded {
		state.Messages = append(state.Messages, &schema.Message{
			Role:    schema.System,
			Content: "[Budget Exceeded] The session budget has been exceeded. Finish immediately and summarize remaining work.",
		})
	}

	return ctx, state, nil
}
