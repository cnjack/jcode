package model

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	openai "github.com/sashabaranov/go-openai"

	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/config"
)

// TokenUsage tracks token consumption across all API calls.
//
// CachedTokens is the cache-READ portion of the prompt (tokens served from the
// provider's KV cache). CacheWriteTokens is the cache-CREATION portion; it is
// 0 today because the shared go-openai transport does not surface
// cache_creation_input_tokens, and is kept as a forward-compatible field.
// ReasoningTokens is the reasoning/thinking subset of the completion.
type TokenUsage struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	CachedTokens     int64
	ReasoningTokens  int64
	CacheWriteTokens int64
	CallCount        int64 // number of API calls recorded (averages denominator)
	LastTotalTokens  int64
	// Per-call "last" values for tracing/observability.
	lastPrompt     int64
	lastCompletion int64
	lastCached     int64
	lastReasoning  int64
	lastCacheWrite int64
	// cacheSeen is set (sticky) once the provider returns a prompt_tokens_details
	// object, so CacheObserved can report "caching supported" even on a 0-hit
	// turn. Cleared by Reset (a session boundary), never by ResetContext.
	cacheSeen int64
	// turnBase* snapshot the cumulative counters at the start of an agent turn
	// (BeginTurn) so per-turn budgets measure THIS turn's consumption, not the
	// whole session's. ResetContext deliberately leaves these untouched so a
	// mid-turn compaction does not corrupt the per-turn measurement.
	turnBasePrompt     int64
	turnBaseCompletion int64
	turnBaseCached     int64
	byModel            map[string]int64
	mu                 sync.RWMutex
}

// AddParams carries one API call's token usage. Using a struct keeps the
// growing set of token categories from turning Add into a long positional list.
type AddParams struct {
	Prompt     int
	Completion int
	Total      int
	Cached     int
	Reasoning  int
	CacheWrite int
	// CacheDetailsPresent is true when the provider returned a
	// prompt_tokens_details object at all (even with cached_tokens:0), letting
	// CacheObserved tell "supports caching, 0 hits" apart from "never reports
	// caching". See https://platform.openai.com/docs/guides/prompt-caching.
	CacheDetailsPresent bool
}

// TokenUsageDetail holds a token usage snapshot for tracing/observability and
// for JSON transport to the UI. Reasoning/cache-write/call-count carry
// omitempty so per-call telemetry stays compact while cumulative snapshots
// (GetFull) carry the full breakdown.
type TokenUsageDetail struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	CallCount        int `json:"call_count,omitempty"`
}

// Minus returns the per-field difference d-prev, used to derive the token delta
// of a single agent run from cumulative snapshots.
func (d TokenUsageDetail) Minus(prev TokenUsageDetail) TokenUsageDetail {
	return TokenUsageDetail{
		PromptTokens:     d.PromptTokens - prev.PromptTokens,
		CompletionTokens: d.CompletionTokens - prev.CompletionTokens,
		TotalTokens:      d.TotalTokens - prev.TotalTokens,
		CachedTokens:     d.CachedTokens - prev.CachedTokens,
		ReasoningTokens:  d.ReasoningTokens - prev.ReasoningTokens,
		CacheWriteTokens: d.CacheWriteTokens - prev.CacheWriteTokens,
		CallCount:        d.CallCount - prev.CallCount,
	}
}

// TokenTracker is a global token usage tracker
var TokenTracker = &TokenUsage{}

// Add records one API call's token usage.
func (t *TokenUsage) Add(p AddParams) {
	atomic.AddInt64(&t.PromptTokens, int64(p.Prompt))
	atomic.AddInt64(&t.CompletionTokens, int64(p.Completion))
	atomic.AddInt64(&t.TotalTokens, int64(p.Total))
	atomic.AddInt64(&t.CachedTokens, int64(p.Cached))
	atomic.AddInt64(&t.ReasoningTokens, int64(p.Reasoning))
	atomic.AddInt64(&t.CacheWriteTokens, int64(p.CacheWrite))
	atomic.AddInt64(&t.CallCount, 1)
	atomic.StoreInt64(&t.LastTotalTokens, int64(p.Total))
	atomic.StoreInt64(&t.lastPrompt, int64(p.Prompt))
	atomic.StoreInt64(&t.lastCompletion, int64(p.Completion))
	atomic.StoreInt64(&t.lastCached, int64(p.Cached))
	atomic.StoreInt64(&t.lastReasoning, int64(p.Reasoning))
	atomic.StoreInt64(&t.lastCacheWrite, int64(p.CacheWrite))
	if p.CacheDetailsPresent || p.Cached > 0 {
		atomic.StoreInt64(&t.cacheSeen, 1)
	}
}

// Get returns the current token usage
func (t *TokenUsage) Get() (prompt, completion, total int64) {
	return atomic.LoadInt64(&t.PromptTokens),
		atomic.LoadInt64(&t.CompletionTokens),
		atomic.LoadInt64(&t.TotalTokens)
}

// GetLastTotal returns the last API call's total tokens (current context usage)
func (t *TokenUsage) GetLastTotal() int64 {
	return atomic.LoadInt64(&t.LastTotalTokens)
}

// GetLastDetail returns the last API call's token usage detail.
func (t *TokenUsage) GetLastDetail() *TokenUsageDetail {
	return &TokenUsageDetail{
		PromptTokens:     int(atomic.LoadInt64(&t.lastPrompt)),
		CompletionTokens: int(atomic.LoadInt64(&t.lastCompletion)),
		TotalTokens:      int(atomic.LoadInt64(&t.LastTotalTokens)),
		CachedTokens:     int(atomic.LoadInt64(&t.lastCached)),
		ReasoningTokens:  int(atomic.LoadInt64(&t.lastReasoning)),
		CacheWriteTokens: int(atomic.LoadInt64(&t.lastCacheWrite)),
	}
}

// GetFull returns a cumulative snapshot of all tracked token usage.
func (t *TokenUsage) GetFull() TokenUsageDetail {
	return TokenUsageDetail{
		PromptTokens:     int(atomic.LoadInt64(&t.PromptTokens)),
		CompletionTokens: int(atomic.LoadInt64(&t.CompletionTokens)),
		TotalTokens:      int(atomic.LoadInt64(&t.TotalTokens)),
		CachedTokens:     int(atomic.LoadInt64(&t.CachedTokens)),
		ReasoningTokens:  int(atomic.LoadInt64(&t.ReasoningTokens)),
		CacheWriteTokens: int(atomic.LoadInt64(&t.CacheWriteTokens)),
		CallCount:        int(atomic.LoadInt64(&t.CallCount)),
	}
}

// CacheHitRate returns the cumulative KV cache hit rate, defined as
// cached / prompt — the fraction of prompt tokens served from the provider's
// cache. Returns 0 when no prompt tokens have been recorded. The result is
// clamped to [0,1] to stay robust against provider quirks.
func (t *TokenUsage) CacheHitRate() float64 {
	prompt := atomic.LoadInt64(&t.PromptTokens)
	if prompt <= 0 {
		return 0
	}
	r := float64(atomic.LoadInt64(&t.CachedTokens)) / float64(prompt)
	switch {
	case r < 0:
		return 0
	case r > 1:
		return 1
	default:
		return r
	}
}

// CacheObserved reports whether the provider has reported cache details (a
// prompt_tokens_details object) — used to distinguish "cache hit rate is 0%"
// from "this provider never reports caching". It is true on the first turn that
// carries cache details even when cached_tokens is 0, and stays true for the
// session (cleared only by Reset). The CachedTokens>0 fallback keeps it correct
// for older snapshots recorded before the presence flag existed.
func (t *TokenUsage) CacheObserved() bool {
	return atomic.LoadInt64(&t.cacheSeen) > 0 || atomic.LoadInt64(&t.CachedTokens) > 0
}

// Reset resets the token tracker
func (t *TokenUsage) Reset() {
	atomic.StoreInt64(&t.PromptTokens, 0)
	atomic.StoreInt64(&t.CompletionTokens, 0)
	atomic.StoreInt64(&t.TotalTokens, 0)
	atomic.StoreInt64(&t.CachedTokens, 0)
	atomic.StoreInt64(&t.ReasoningTokens, 0)
	atomic.StoreInt64(&t.CacheWriteTokens, 0)
	atomic.StoreInt64(&t.CallCount, 0)
	atomic.StoreInt64(&t.LastTotalTokens, 0)
	atomic.StoreInt64(&t.lastPrompt, 0)
	atomic.StoreInt64(&t.lastCompletion, 0)
	atomic.StoreInt64(&t.lastCached, 0)
	atomic.StoreInt64(&t.lastReasoning, 0)
	atomic.StoreInt64(&t.lastCacheWrite, 0)
	atomic.StoreInt64(&t.cacheSeen, 0)
	atomic.StoreInt64(&t.turnBasePrompt, 0)
	atomic.StoreInt64(&t.turnBaseCompletion, 0)
	atomic.StoreInt64(&t.turnBaseCached, 0)
	t.mu.Lock()
	t.byModel = nil
	t.mu.Unlock()
}

// ResetContext clears only the "current context occupancy" snapshot (the last
// API call's per-call values), leaving the cumulative consumption ledger, the
// cache-support flag, the per-model breakdown, and the per-turn baseline
// intact. Call this after a compaction/summarization shrinks the live context:
// the context indicator should reflect the smaller window, but the session's
// accumulated spend must NOT be lost — it feeds budgets, the usage log, and
// cross-session stats. (Full Reset is for a genuine session boundary.)
func (t *TokenUsage) ResetContext() {
	atomic.StoreInt64(&t.LastTotalTokens, 0)
	atomic.StoreInt64(&t.lastPrompt, 0)
	atomic.StoreInt64(&t.lastCompletion, 0)
	atomic.StoreInt64(&t.lastCached, 0)
	atomic.StoreInt64(&t.lastReasoning, 0)
	atomic.StoreInt64(&t.lastCacheWrite, 0)
}

// BeginTurn snapshots the cumulative counters as the baseline for the current
// agent turn so TurnUsage reports only this turn's delta. Called at the start of
// every runner turn.
func (t *TokenUsage) BeginTurn() {
	atomic.StoreInt64(&t.turnBasePrompt, atomic.LoadInt64(&t.PromptTokens))
	atomic.StoreInt64(&t.turnBaseCompletion, atomic.LoadInt64(&t.CompletionTokens))
	atomic.StoreInt64(&t.turnBaseCached, atomic.LoadInt64(&t.CachedTokens))
}

// TurnUsage returns this turn's consumption (cumulative minus the BeginTurn
// baseline). Each value is clamped at 0 so a mid-turn Reset (which zeroes the
// cumulative and the baseline together) can never yield a negative delta.
func (t *TokenUsage) TurnUsage() (prompt, completion, cached int64) {
	prompt = atomic.LoadInt64(&t.PromptTokens) - atomic.LoadInt64(&t.turnBasePrompt)
	completion = atomic.LoadInt64(&t.CompletionTokens) - atomic.LoadInt64(&t.turnBaseCompletion)
	cached = atomic.LoadInt64(&t.CachedTokens) - atomic.LoadInt64(&t.turnBaseCached)
	if prompt < 0 {
		prompt = 0
	}
	if completion < 0 {
		completion = 0
	}
	if cached < 0 {
		cached = 0
	}
	return
}

// AddByModel adds token usage attributed to a specific model name.
func (t *TokenUsage) AddByModel(model string, prompt, completion, total int) {
	if model == "" {
		return
	}
	t.mu.Lock()
	if t.byModel == nil {
		t.byModel = make(map[string]int64)
	}
	t.byModel[model] += int64(total)
	t.mu.Unlock()
}

// GetByModel returns a snapshot of per-model token totals.
func (t *TokenUsage) GetByModel() map[string]int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.byModel) == 0 {
		return nil
	}
	copy := make(map[string]int64, len(t.byModel))
	for k, v := range t.byModel {
		copy[k] = v
	}
	return copy
}

// ModelPricing contains cost information for a model.
type ModelPricing struct {
	InputPer1M     float64 // cost per 1M input tokens
	OutputPer1M    float64 // cost per 1M output tokens
	CacheReadPer1M float64 // cost per 1M cache-read (cached input) tokens; 0 ⇒ no discount data, fall back to InputPer1M
}

// ModelInfo contains information about a model
type ModelInfo struct {
	ID           string
	ContextLimit int // Maximum context window size, 0 if unknown
	Pricing      ModelPricing
}

type ChatModelConfig struct {
	Model   string
	APIKey  string
	BaseURL string
	// Headers are extra HTTP headers injected into every request to the
	// provider endpoint (custom gateways, auth proxies). Empty ⇒ none.
	Headers map[string]string
	// ReasoningEffort sets thinking depth via the "reasoning_effort" parameter:
	// "", "low", "medium", or "high". Empty ⇒ parameter omitted.
	ReasoningEffort string
	// Thinking, when non-nil, sends chat_template_kwargs {"enable_thinking": v}
	// to explicitly toggle extended reasoning on compatible gateways.
	Thinking *bool
	// Vision controls whether image parts are forwarded to the model. When
	// false, multimodal image content is stripped to text before sending.
	Vision bool
}

type chatModel struct {
	client          *openai.Client
	model           string
	tools           []openai.Tool
	reasoningEffort string
	thinking        *bool
	vision          bool
}

// headerDoer wraps an http.Client to inject a fixed set of headers into every
// outgoing request. It satisfies go-openai's HTTPDoer interface so a provider's
// configured Headers reach the API. Set unconditionally so callers may override
// transport headers (including Authorization) for custom gateways.
type headerDoer struct {
	base    *http.Client
	headers map[string]string
}

func (h *headerDoer) Do(req *http.Request) (*http.Response, error) {
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	return h.base.Do(req)
}

func NewChatModel(_ context.Context, cfg *ChatModelConfig) (einomodel.ToolCallingChatModel, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("APIKey is required")
	}
	config := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		config.BaseURL = cfg.BaseURL
	}
	if len(cfg.Headers) > 0 {
		config.HTTPClient = &headerDoer{base: &http.Client{}, headers: cfg.Headers}
	}
	return &chatModel{
		client:          openai.NewClientWithConfig(config),
		model:           cfg.Model,
		reasoningEffort: cfg.ReasoningEffort,
		thinking:        cfg.Thinking,
		vision:          cfg.Vision,
	}, nil
}

// NewChatModelFromProvider builds a ChatModel from a provider config, applying
// its advanced settings (custom headers, thinking depth, explicit thinking
// toggle, and the vision capability — which defaults to enabled). baseURL is the
// already-resolved endpoint (config override or registry default). This is the
// single place that maps ProviderConfig → ChatModelConfig so every entrypoint
// (web, TUI, ACP, subagents) honors the same settings.
func NewChatModelFromProvider(ctx context.Context, modelName, baseURL string, pc *config.ProviderConfig) (einomodel.ToolCallingChatModel, error) {
	vision := true
	if pc.Vision != nil {
		vision = *pc.Vision
	}
	return NewChatModel(ctx, &ChatModelConfig{
		Model:           modelName,
		APIKey:          pc.APIKey,
		BaseURL:         baseURL,
		Headers:         pc.Headers,
		ReasoningEffort: pc.ReasoningEffort,
		Thinking:        pc.Thinking,
		Vision:          vision,
	})
}

func (m *chatModel) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	config.Logger().Printf("[chatmodel] WithTools called with %d tools", len(tools))
	oaiTools := make([]openai.Tool, 0, len(tools))
	for _, ti := range tools {
		if ti == nil {
			continue
		}
		params, err := ti.ToJSONSchema()
		if err != nil {
			return nil, fmt.Errorf("failed to convert params for tool %s: %w", ti.Name, err)
		}
		oaiTools = append(oaiTools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        ti.Name,
				Description: ti.Desc,
				Parameters:  params,
			},
		})
	}
	config.Logger().Printf("[chatmodel] WithTools: bound %d tools", len(oaiTools))
	for _, t := range oaiTools {
		config.Logger().Printf("[chatmodel]   tool: %s", t.Function.Name)
	}
	return &chatModel{
		client:          m.client,
		model:           m.model,
		tools:           oaiTools,
		reasoningEffort: m.reasoningEffort,
		thinking:        m.thinking,
		vision:          m.vision,
	}, nil
}

// extractUsage maps a go-openai Usage onto AddParams. cache_creation tokens are
// not exposed by go-openai's schema, so CacheWrite is always 0 here; reasoning
// and cache-read are picked up from the *TokensDetails sub-objects when present.
func extractUsage(u openai.Usage) AddParams {
	p := AddParams{
		Prompt:     u.PromptTokens,
		Completion: u.CompletionTokens,
		Total:      u.TotalTokens,
	}
	if u.PromptTokensDetails != nil {
		p.Cached = u.PromptTokensDetails.CachedTokens
		p.CacheDetailsPresent = true
	}
	if u.CompletionTokensDetails != nil {
		p.Reasoning = u.CompletionTokensDetails.ReasoningTokens
	}
	// Some providers (e.g. some GLM/OpenAI-compatible gateways) omit total_tokens
	// and only return prompt/completion. Derive it so the context indicator works.
	if p.Total == 0 {
		p.Total = p.Prompt + p.Completion
	}
	return p
}

// hasUsage reports whether a Usage object carries any token counts, tolerating
// providers that populate prompt/completion but leave total_tokens at 0.
func hasUsage(u openai.Usage) bool {
	return u.PromptTokens > 0 || u.CompletionTokens > 0 || u.TotalTokens > 0
}

// recordUsage feeds one API call's usage into both the global tracker and the
// per-agent tracker on the context (when present), preserving the dual-tracker
// pattern.
func (m *chatModel) recordUsage(ctx context.Context, u openai.Usage) {
	p := extractUsage(u)
	TokenTracker.Add(p)
	TokenTracker.AddByModel(m.model, p.Prompt, p.Completion, p.Total)
	if local := TokenTrackerFromContext(ctx); local != nil {
		local.Add(p)
		local.AddByModel(m.model, p.Prompt, p.Completion, p.Total)
	}
	// Real-time UI refresh: fire after the trackers are updated so the callback
	// reads the just-recorded usage.
	if notify := UsageNotifierFromContext(ctx); notify != nil {
		notify()
	}
}

func (m *chatModel) Generate(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	req := m.buildRequest(input, false, opts...)
	config.Logger().Printf("[chatmodel] Generate start (model: %s)", m.model)
	start := time.Now()
	resp, err := m.client.CreateChatCompletion(ctx, req)
	config.Logger().Printf("[chatmodel] Generate finished in %v, err: %v", time.Since(start), err)
	if err != nil {
		return nil, err
	}
	// Track token usage
	config.Logger().Printf("[chatmodel] Generate usage: prompt=%d completion=%d total=%d", resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	if hasUsage(resp.Usage) {
		m.recordUsage(ctx, resp.Usage)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from model")
	}
	return toEinoMessage(resp.Choices[0].Message), nil
}

func (m *chatModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	req := m.buildRequest(input, true, opts...)
	// Enable stream options to get usage information
	req.StreamOptions = &openai.StreamOptions{
		IncludeUsage: true,
	}
	config.Logger().Printf("[chatmodel] Stream start (model: %s)", m.model)
	start := time.Now()
	stream, err := m.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		config.Logger().Printf("[chatmodel] Stream failed to start in %v, err: %v", time.Since(start), err)
		return nil, err
	}
	config.Logger().Printf("[chatmodel] Stream started successfully in %v", time.Since(start))

	sr, sw := schema.Pipe[*schema.Message](16)
	go func() {
		defer sw.Close()
		defer func() { _ = stream.Close() }()
		chunkCount := 0
		toolCallSeen := false
		usageSeen := false
		var lastUsage *openai.Usage
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				config.Logger().Printf("[chatmodel] Stream EOF after %d chunks, toolCallSeen=%v usageSeen=%v", chunkCount, toolCallSeen, usageSeen)
				break
			}
			if err != nil {
				config.Logger().Printf("[chatmodel] Stream recv error after %d chunks: %v", chunkCount, err)
				sw.Send(nil, err)
				break
			}
			chunkCount++
			// Capture token usage from the stream. Some providers only send
			// usage in a final chunk (requires stream_options.include_usage),
			// and some omit total_tokens — hasUsage tolerates both. We record
			// only the LAST usage once the stream ends, so providers that repeat
			// (cumulative) usage per chunk aren't counted multiple times.
			if resp.Usage != nil && hasUsage(*resp.Usage) {
				u := *resp.Usage
				lastUsage = &u
				usageSeen = true
			}
			if len(resp.Choices) == 0 {
				continue
			}
			delta := resp.Choices[0].Delta
			if len(delta.ToolCalls) > 0 && !toolCallSeen {
				toolCallSeen = true
				config.Logger().Printf("[chatmodel] Stream: first tool call detected at chunk %d: %s", chunkCount, delta.ToolCalls[0].Function.Name)
			}
			msg := &schema.Message{
				Role:             schema.Assistant,
				Content:          delta.Content,
				ReasoningContent: delta.ReasoningContent,
			}
			if len(delta.ToolCalls) > 0 {
				msg.ToolCalls = toEinoToolCalls(delta.ToolCalls)
			}
			sw.Send(msg, nil)
		}
		// Record once at stream end so the per-call notifier fires a single
		// token_update for this call.
		if lastUsage != nil {
			config.Logger().Printf("[chatmodel] Stream usage: prompt=%d completion=%d total=%d", lastUsage.PromptTokens, lastUsage.CompletionTokens, lastUsage.TotalTokens)
			m.recordUsage(ctx, *lastUsage)
		}
	}()

	return sr, nil
}

func (m *chatModel) buildRequest(input []*schema.Message, stream bool, opts ...einomodel.Option) openai.ChatCompletionRequest {
	msgs := make([]openai.ChatCompletionMessage, 0, len(input))
	for _, msg := range input {
		msgs = append(msgs, toOpenAIMessage(msg, m.vision))
	}
	req := openai.ChatCompletionRequest{
		Model:    m.model,
		Messages: msgs,
		Stream:   stream,
	}

	// Thinking depth: forward reasoning_effort when configured. Sent for all
	// models — reasoning models honor it; others ignore it (OpenAI-compatible).
	if m.reasoningEffort != "" {
		req.ReasoningEffort = m.reasoningEffort
	}
	// Explicit thinking toggle for gateways that gate reasoning behind
	// chat_template_kwargs {"enable_thinking": <bool>} (e.g. qwen3).
	if m.thinking != nil {
		req.ChatTemplateKwargs = map[string]any{"enable_thinking": *m.thinking}
	}

	// Apply call-time options (e.g. model.WithTools from Eino framework).
	commonOpts := einomodel.GetCommonOptions(nil, opts...)
	if len(commonOpts.Tools) > 0 {
		oaiTools := make([]openai.Tool, 0, len(commonOpts.Tools))
		for _, ti := range commonOpts.Tools {
			if ti == nil {
				continue
			}
			params, err := ti.ToJSONSchema()
			if err != nil {
				config.Logger().Printf("[chatmodel] buildRequest: skip tool %s: %v", ti.Name, err)
				continue
			}
			oaiTools = append(oaiTools, openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        ti.Name,
					Description: ti.Desc,
					Parameters:  params,
				},
			})
		}
		req.Tools = oaiTools
	} else if len(m.tools) > 0 {
		// Fallback to pre-bound tools (from WithTools method).
		req.Tools = m.tools
	}
	config.Logger().Printf("[chatmodel] buildRequest: model=%s, messages=%d, tools=%d, stream=%v", m.model, len(msgs), len(req.Tools), stream)
	return req
}

func toOpenAIMessage(msg *schema.Message, vision bool) openai.ChatCompletionMessage {
	m := openai.ChatCompletionMessage{
		Role:             string(msg.Role),
		Content:          msg.Content,
		Name:             msg.Name,
		ToolCallID:       msg.ToolCallID,
		ReasoningContent: msg.ReasoningContent,
	}
	if len(msg.ToolCalls) > 0 {
		m.ToolCalls = toOpenAIToolCalls(msg.ToolCalls)
	}
	// Convert multimodal content (text + images) to OpenAI MultiContent format.
	if len(msg.UserInputMultiContent) > 0 {
		// Vision disabled: collapse to text-only so a non-vision endpoint
		// doesn't 400 on image parts. Text segments are preserved.
		if !vision {
			var text string
			for _, p := range msg.UserInputMultiContent {
				if p.Type == schema.ChatMessagePartTypeText {
					text += p.Text
				}
			}
			m.Content = text
			return m
		}
		m.Content = ""
		parts := make([]openai.ChatMessagePart, 0, len(msg.UserInputMultiContent))
		for _, p := range msg.UserInputMultiContent {
			switch p.Type {
			case schema.ChatMessagePartTypeText:
				parts = append(parts, openai.ChatMessagePart{
					Type: openai.ChatMessagePartTypeText,
					Text: p.Text,
				})
			case schema.ChatMessagePartTypeImageURL:
				if p.Image != nil {
					var url string
					if p.Image.Base64Data != nil && *p.Image.Base64Data != "" {
						url = "data:" + p.Image.MIMEType + ";base64," + *p.Image.Base64Data
					} else if p.Image.URL != nil {
						url = *p.Image.URL
					}
					if url != "" {
						parts = append(parts, openai.ChatMessagePart{
							Type: openai.ChatMessagePartTypeImageURL,
							ImageURL: &openai.ChatMessageImageURL{
								URL: url,
							},
						})
					}
				}
			}
		}
		m.MultiContent = parts
	}
	return m
}

func toEinoMessage(msg openai.ChatCompletionMessage) *schema.Message {
	m := &schema.Message{
		Role:             schema.RoleType(msg.Role),
		Content:          msg.Content,
		Name:             msg.Name,
		ToolCallID:       msg.ToolCallID,
		ReasoningContent: msg.ReasoningContent,
	}
	if len(msg.ToolCalls) > 0 {
		m.ToolCalls = toEinoToolCalls(msg.ToolCalls)
	}
	return m
}

func toOpenAIToolCalls(tcs []schema.ToolCall) []openai.ToolCall {
	ret := make([]openai.ToolCall, len(tcs))
	for i, tc := range tcs {
		ret[i] = openai.ToolCall{
			Index: tc.Index,
			ID:    tc.ID,
			Type:  openai.ToolTypeFunction,
			Function: openai.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		}
	}
	return ret
}

func toEinoToolCalls(tcs []openai.ToolCall) []schema.ToolCall {
	ret := make([]schema.ToolCall, len(tcs))
	for i, tc := range tcs {
		ret[i] = schema.ToolCall{
			Index: tc.Index,
			ID:    tc.ID,
			Type:  string(tc.Type),
			Function: schema.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		}
	}
	return ret
}

type knownModel struct {
	ContextLimit int
	InputPer1M   float64
	OutputPer1M  float64
}

// knownModels maps model names to their context window sizes and pricing.
// This is used as a local fallback when the models.dev registry is unavailable.
var knownModels = map[string]knownModel{
	// OpenAI models
	"gpt-4o":              {128000, 2.50, 10.00},
	"gpt-4o-mini":         {128000, 0.15, 0.60},
	"gpt-4-turbo":         {128000, 10.00, 30.00},
	"gpt-4-turbo-preview": {128000, 10.00, 30.00},
	"gpt-4-0125-preview":  {128000, 10.00, 30.00},
	"gpt-4-1106-preview":  {128000, 10.00, 30.00},
	"gpt-4":               {8192, 30.00, 60.00},
	"gpt-4-32k":           {32768, 60.00, 120.00},
	"gpt-3.5-turbo":       {16385, 0.50, 1.50},
	"gpt-3.5-turbo-16k":   {16385, 0.50, 1.50},
	"o1":                  {200000, 15.00, 60.00},
	"o1-preview":          {128000, 15.00, 60.00},
	"o1-mini":             {128000, 3.00, 12.00},
	// Claude models (for Anthropic-compatible APIs)
	"claude-3-5-sonnet-latest":   {200000, 3.00, 15.00},
	"claude-3-5-sonnet-20241022": {200000, 3.00, 15.00},
	"claude-3-5-sonnet-20240620": {200000, 3.00, 15.00},
	"claude-3-5-haiku-latest":    {200000, 0.80, 4.00},
	"claude-3-5-haiku-20241022":  {200000, 0.80, 4.00},
	"claude-3-opus-20240229":     {200000, 15.00, 75.00},
	"claude-3-sonnet-20240229":   {200000, 3.00, 15.00},
	"claude-3-haiku-20240307":    {200000, 0.25, 1.25},
	"claude-sonnet-4-20250514":   {200000, 3.00, 15.00},
	"claude-opus-4-20250514":     {200000, 15.00, 75.00},
	// DeepSeek models
	"deepseek-chat":     {64000, 0.14, 0.28},
	"deepseek-coder":    {16000, 0.14, 0.28},
	"deepseek-reasoner": {64000, 0.55, 2.19},
	// Other common models
	"llama-3.1-405b":   {128000, 0, 0},
	"llama-3.1-70b":    {128000, 0, 0},
	"llama-3.1-8b":     {128000, 0, 0},
	"llama-3-70b":      {8192, 0, 0},
	"llama-3-8b":       {8192, 0, 0},
	"mixtral-8x7b":     {32768, 0, 0},
	"mixtral-8x22b":    {65536, 0, 0},
	"mistral-large":    {128000, 0, 0},
	"gemini-1.5-pro":   {1000000, 1.25, 5.00},
	"gemini-1.5-flash": {1000000, 0.075, 0.30},
	// 2026 long-context flagships (offline fallback when the models.dev registry
	// is unavailable). Context windows verified against vendor docs / models.dev;
	// see docs/model-research.md. Pricing left 0 where not confidently known.
	"MiniMax-M3":             {1000000, 0.60, 2.40},
	"minimax-m3":             {1000000, 0.60, 2.40},
	"deepseek-v4-pro":        {1000000, 0, 0},
	"deepseek-v4-flash":      {1000000, 0, 0},
	"qwen3.7-max":            {1000000, 2.50, 7.50},
	"qwen3.7-plus":           {1000000, 0, 0},
	"qwen3.6-plus":           {1000000, 0, 0},
	"kimi-k2.6":              {262144, 0, 0},
	"kimi-k2.7-code":         {262144, 0, 0},
	"glm-5":                  {200000, 0, 0},
	"glm-5.1":                {200000, 0, 0},
	"glm-5.2":                {1000000, 0, 0}, // user-reported 1M; official docs pending
	"gpt-5.5":                {1050000, 0, 0},
	"claude-opus-4-8":        {1000000, 0, 0},
	"claude-sonnet-4-6":      {1000000, 0, 0},
	"gemini-3.1-pro-preview": {1048576, 0, 0},
	"gemini-3.5-flash":       {1048576, 0, 0},
}

// knownModelContextLimits is kept for backward compatibility.
//
// Deprecated: use knownModels instead.
var knownModelContextLimits = func() map[string]int {
	m := make(map[string]int, len(knownModels))
	for k, v := range knownModels {
		m[k] = v.ContextLimit
	}
	return m
}()

// GetModelInfo retrieves model information. It first tries the /models API,
// then falls back to known model limits.
func (m *chatModel) GetModelInfo(ctx context.Context) ModelInfo {
	info := ModelInfo{ID: m.model}

	// Try to get model info from API (may not work for all providers)
	model, err := m.client.GetModel(ctx, m.model)
	if err == nil {
		info.ID = model.ID
	}

	// Look up known model info (context limit + pricing)
	if km, ok := knownModels[m.model]; ok {
		info.ContextLimit = km.ContextLimit
		info.Pricing = ModelPricing{InputPer1M: km.InputPer1M, OutputPer1M: km.OutputPer1M}
	} else {
		// Try partial match for model name patterns
		for pattern, km := range knownModels {
			if containsModelPattern(m.model, pattern) {
				info.ContextLimit = km.ContextLimit
				info.Pricing = ModelPricing{InputPer1M: km.InputPer1M, OutputPer1M: km.OutputPer1M}
				break
			}
		}
	}

	return info
}

// containsModelPattern checks if the model name matches a pattern (partial match)
func containsModelPattern(model, pattern string) bool {
	// Simple prefix/suffix matching
	return len(model) >= len(pattern) &&
		(model == pattern ||
			(len(model) > len(pattern) && (model[:len(pattern)] == pattern || model[len(model)-len(pattern):] == pattern)))
}

// GetModelContextLimit returns the known context limit for a given model name.
// Returns 0 if the model is not in the known list.
func GetModelContextLimit(modelName string) int {
	if limit, ok := knownModelContextLimits[modelName]; ok {
		return limit
	}
	// Try partial match
	for pattern, limit := range knownModelContextLimits {
		if containsModelPattern(modelName, pattern) {
			return limit
		}
	}
	return 0
}
