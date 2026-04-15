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

	inputCost := float64(promptTokens) * b.pricing.InputPer1M / 1_000_000
	outputCost := float64(completionTokens) * b.pricing.OutputPer1M / 1_000_000
	b.totalCost += inputCost + outputCost

	return b.statusLocked()
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
	manager     *BudgetManager
	tokenUsage  *internalmodel.TokenUsage
	onWarn      func(status BudgetStatus)
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
	if m.tokenUsage != nil {
		promptTokens, completionTokens, _ = m.tokenUsage.Get()
	}

	// Sync budget manager with per-agent token tracker values.
	m.manager.mu.Lock()
	m.manager.promptTokens = promptTokens
	m.manager.completionTokens = completionTokens
	inputCost := float64(promptTokens) * m.manager.pricing.InputPer1M / 1_000_000
	outputCost := float64(completionTokens) * m.manager.pricing.OutputPer1M / 1_000_000
	m.manager.totalCost = inputCost + outputCost
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
