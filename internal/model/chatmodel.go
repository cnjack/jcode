package model

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	openai "github.com/sashabaranov/go-openai"

	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/config"
)

// TokenUsage tracks token consumption across all API calls
type TokenUsage struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	CachedTokens     int64
	LastTotalTokens  int64
	// Per-call "last" values for tracing/observability.
	lastPrompt     int64
	lastCompletion int64
	lastCached     int64
	byModel        map[string]int64
	mu             sync.RWMutex
}

// TokenUsageDetail holds per-call token usage details for tracing/observability.
type TokenUsageDetail struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens"`
}

// TokenTracker is a global token usage tracker
var TokenTracker = &TokenUsage{}

// Add adds token usage to the tracker
func (t *TokenUsage) Add(prompt, completion, total, cached int) {
	atomic.AddInt64(&t.PromptTokens, int64(prompt))
	atomic.AddInt64(&t.CompletionTokens, int64(completion))
	atomic.AddInt64(&t.TotalTokens, int64(total))
	atomic.AddInt64(&t.CachedTokens, int64(cached))
	atomic.StoreInt64(&t.LastTotalTokens, int64(total))
	atomic.StoreInt64(&t.lastPrompt, int64(prompt))
	atomic.StoreInt64(&t.lastCompletion, int64(completion))
	atomic.StoreInt64(&t.lastCached, int64(cached))
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
	}
}

// Reset resets the token tracker
func (t *TokenUsage) Reset() {
	atomic.StoreInt64(&t.PromptTokens, 0)
	atomic.StoreInt64(&t.CompletionTokens, 0)
	atomic.StoreInt64(&t.TotalTokens, 0)
	atomic.StoreInt64(&t.CachedTokens, 0)
	t.mu.Lock()
	t.byModel = nil
	t.mu.Unlock()
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
	InputPer1M  float64 // cost per 1M input tokens
	OutputPer1M float64 // cost per 1M output tokens
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
}

type chatModel struct {
	client *openai.Client
	model  string
	tools  []openai.Tool
}

func NewChatModel(_ context.Context, cfg *ChatModelConfig) (einomodel.ToolCallingChatModel, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("APIKey is required")
	}
	config := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		config.BaseURL = cfg.BaseURL
	}
	return &chatModel{
		client: openai.NewClientWithConfig(config),
		model:  cfg.Model,
	}, nil
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
	return &chatModel{client: m.client, model: m.model, tools: oaiTools}, nil
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
	if resp.Usage.TotalTokens > 0 {
		cached := 0
		if resp.Usage.PromptTokensDetails != nil {
			cached = resp.Usage.PromptTokensDetails.CachedTokens
		}
		TokenTracker.Add(resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens, cached)
		TokenTracker.AddByModel(m.model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
		if local := TokenTrackerFromContext(ctx); local != nil {
			local.Add(resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens, cached)
			local.AddByModel(m.model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
		}
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
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				config.Logger().Printf("[chatmodel] Stream EOF after %d chunks, toolCallSeen=%v", chunkCount, toolCallSeen)
				break
			}
			if err != nil {
				config.Logger().Printf("[chatmodel] Stream recv error after %d chunks: %v", chunkCount, err)
				sw.Send(nil, err)
				break
			}
			chunkCount++
			// Track token usage from stream response
			if resp.Usage != nil && resp.Usage.TotalTokens > 0 {
				cached := 0
				if resp.Usage.PromptTokensDetails != nil {
					cached = resp.Usage.PromptTokensDetails.CachedTokens
				}
				TokenTracker.Add(resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens, cached)
				TokenTracker.AddByModel(m.model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
				if local := TokenTrackerFromContext(ctx); local != nil {
					local.Add(resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens, cached)
					local.AddByModel(m.model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
				}
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
	}()

	return sr, nil
}

func (m *chatModel) buildRequest(input []*schema.Message, stream bool, opts ...einomodel.Option) openai.ChatCompletionRequest {
	msgs := make([]openai.ChatCompletionMessage, 0, len(input))
	for _, msg := range input {
		msgs = append(msgs, toOpenAIMessage(msg))
	}
	req := openai.ChatCompletionRequest{
		Model:    m.model,
		Messages: msgs,
		Stream:   stream,
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

func toOpenAIMessage(msg *schema.Message) openai.ChatCompletionMessage {
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

// GetTokenUsage returns the current token usage statistics
func GetTokenUsage() (prompt, completion, total int64) {
	return TokenTracker.Get()
}

// ResetTokenUsage resets the token usage tracker
func ResetTokenUsage() {
	TokenTracker.Reset()
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
