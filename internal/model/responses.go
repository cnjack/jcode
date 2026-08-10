package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/config"
)

const maxResponsesRequestBytes = 48 << 20

// ResponsesCredentialFunc resolves fresh authorization immediately before an
// HTTP dispatch. Returned headers are applied after configured headers; a
// non-empty token is applied last as Authorization: Bearer <token>.
type ResponsesCredentialFunc func(context.Context) (token string, headers map[string]string, err error)

// ResponsesModelConfig configures the raw OpenAI Responses API transport used
// by xAI OAuth and ChatGPT Codex OAuth providers.
type ResponsesModelConfig struct {
	Model           string
	BaseURL         string
	Headers         map[string]string
	ReasoningEffort string
	Vision          bool
	Credential      ResponsesCredentialFunc
	Codex           bool
	Copilot         bool
	HTTPClient      *http.Client
}

type responsesModel struct {
	model           string
	endpoint        string
	headers         map[string]string
	reasoningEffort string
	vision          bool
	credential      ResponsesCredentialFunc
	codex           bool
	copilot         bool
	client          *http.Client
	tools           []*schema.ToolInfo
}

type responsesReasoningRequest struct {
	Effort string `json:"effort,omitempty"`
}

type responsesRequest struct {
	Model             string                     `json:"model"`
	Instructions      string                     `json:"instructions"`
	Input             []json.RawMessage          `json:"input"`
	Tools             []responsesTool            `json:"tools"`
	ToolChoice        string                     `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool                      `json:"parallel_tool_calls,omitempty"`
	Reasoning         *responsesReasoningRequest `json:"reasoning,omitempty"`
	Stream            bool                       `json:"stream"`
	Store             *bool                      `json:"store,omitempty"`
	Include           []string                   `json:"include,omitempty"`
	MaxOutputTokens   *int                       `json:"max_output_tokens,omitempty"`
	Temperature       *float32                   `json:"temperature,omitempty"`
	TopP              *float32                   `json:"top_p,omitempty"`
}

// NewResponsesModel constructs an immutable Eino ToolCallingChatModel. Provider
// selection remains in NewChatModelFromProvider so this transport can be wired
// in without coupling it to config or authentication storage.
func NewResponsesModel(_ context.Context, cfg *ResponsesModelConfig) (einomodel.ToolCallingChatModel, error) {
	if cfg == nil {
		return nil, fmt.Errorf("responses model config is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("responses model is required")
	}
	endpoint, err := responsesEndpoint(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	if cfg.Credential == nil {
		return nil, fmt.Errorf("responses credential resolver is required")
	}
	client := cloneResponsesHTTPClient(cfg.HTTPClient)
	return &responsesModel{
		model:           cfg.Model,
		endpoint:        endpoint,
		headers:         cloneStringMap(cfg.Headers),
		reasoningEffort: cfg.ReasoningEffort,
		vision:          cfg.Vision,
		credential:      cfg.Credential,
		codex:           cfg.Codex,
		copilot:         cfg.Copilot,
		client:          client,
	}, nil
}

func cloneResponsesHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	cloned := *client
	// Responses requests carry replayable POST bodies and short-lived bearer
	// credentials. Never allow net/http to forward either to a redirect target,
	// even when an injected client normally follows 307/308 responses.
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &cloned
}

func responsesEndpoint(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("responses base URL is required")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid responses base URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("responses base URL must use http or https")
	}
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/responses") {
		u.Path += "/responses"
	}
	return u.String(), nil
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func (m *responsesModel) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	if _, err := responsesTools(tools); err != nil {
		return nil, err
	}
	derived := *m
	derived.tools = append([]*schema.ToolInfo(nil), tools...)
	return &derived, nil
}

func (m *responsesModel) buildRequest(
	input []*schema.Message,
	stream bool,
	opts ...einomodel.Option,
) (responsesRequest, error) {
	convertedInput, err := responsesInput(input, m.vision)
	if err != nil {
		return responsesRequest{}, err
	}
	common := einomodel.GetCommonOptions(nil, opts...)
	modelName := m.model
	if common.Model != nil && *common.Model != "" {
		modelName = *common.Model
	}
	boundTools := m.tools
	if len(common.Tools) > 0 {
		boundTools = common.Tools
	}
	tools, err := responsesTools(boundTools)
	if err != nil {
		return responsesRequest{}, err
	}
	req := responsesRequest{
		Model:        modelName,
		Instructions: responsesInstructions(input),
		Input:        convertedInput,
		Tools:        tools,
		Stream:       stream,
	}
	if m.reasoningEffort != "" {
		req.Reasoning = &responsesReasoningRequest{Effort: m.reasoningEffort}
	}
	if len(tools) > 0 {
		req.ToolChoice = "auto"
		parallel := true
		req.ParallelToolCalls = &parallel
	}
	if common.ToolChoice != nil {
		switch *common.ToolChoice {
		case schema.ToolChoiceForbidden:
			req.ToolChoice = "none"
		case schema.ToolChoiceForced:
			req.ToolChoice = "required"
		default:
			req.ToolChoice = "auto"
		}
	}
	if m.codex {
		store := false
		parallel := false
		req.Store = &store
		req.Include = []string{"reasoning.encrypted_content"}
		req.Stream = true
		if req.Tools == nil {
			req.Tools = []responsesTool{}
		}
		req.ToolChoice = "auto"
		req.ParallelToolCalls = &parallel
		// ChatGPT's Codex backend follows codex-rs and rejects the standard
		// max_output_tokens/temperature/top_p fields.
		return req, nil
	}
	req.MaxOutputTokens = common.MaxTokens
	req.Temperature = common.Temperature
	req.TopP = common.TopP
	return req, nil
}

func (m *responsesModel) Generate(
	ctx context.Context,
	input []*schema.Message,
	opts ...einomodel.Option,
) (*schema.Message, error) {
	if m.copilot {
		ctx = withCopilotModelRequest(ctx, input)
	}
	if m.codex {
		stream, err := m.Stream(ctx, input, opts...)
		if err != nil {
			return nil, err
		}
		defer stream.Close()
		return collectResponsesStream(stream)
	}
	req, err := m.buildRequest(input, false, opts...)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := m.dispatch(ctx, req, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	message, usage, err := decodeResponsesJSON(resp.Body)
	config.Logger().Printf("[responses] Generate finished in %v, err=%v", time.Since(start), err)
	if err != nil {
		return nil, err
	}
	if usage.hasUsage() {
		m.recordUsage(ctx, usage)
	}
	return message, nil
}

func (m *responsesModel) Stream(
	ctx context.Context,
	input []*schema.Message,
	opts ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	if m.copilot {
		ctx = withCopilotModelRequest(ctx, input)
	}
	req, err := m.buildRequest(input, true, opts...)
	if err != nil {
		return nil, err
	}
	resp, err := m.dispatch(ctx, req, true)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.Message](16)
	go func() {
		defer sw.Close()
		defer func() { _ = resp.Body.Close() }()
		usage, parseErr := decodeResponsesSSE(resp.Body, func(message *schema.Message) error {
			sw.Send(message, nil)
			return nil
		})
		if parseErr != nil {
			sw.Send(nil, parseErr)
			return
		}
		if usage.hasUsage() {
			m.recordUsage(ctx, usage)
		}
	}()
	return sr, nil
}

func (m *responsesModel) dispatch(
	ctx context.Context,
	payload responsesRequest,
	stream bool,
) (*http.Response, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode responses request: %w", err)
	}
	if len(raw) > maxResponsesRequestBytes {
		return nil, fmt.Errorf("responses request exceeds %d-byte limit", maxResponsesRequestBytes)
	}
	token, credentialHeaders, err := m.credential(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve responses credential: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("resolve responses credential: empty token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("create responses request: %w", err)
	}
	if err := applyResponsesHeaders(req.Header, m.headers); err != nil {
		return nil, err
	}
	// Transport-owned fields and dynamic credential headers are protected from
	// stale provider configuration by applying them last.
	req.Header.Set("Content-Type", "application/json")
	if stream || m.codex {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if err := applyResponsesHeaders(req.Header, credentialHeaders); err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("responses request: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer func() { _ = resp.Body.Close() }()
		return nil, decodeResponsesHTTPError(resp)
	}
	return resp, nil
}

func applyResponsesHeaders(dst http.Header, values map[string]string) error {
	for key, value := range values {
		if !validResponsesHeaderName(key) || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("invalid responses header")
		}
		dst.Set(key, value)
	}
	return nil
}

func validResponsesHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)):
		default:
			return false
		}
	}
	return true
}

func (m *responsesModel) recordUsage(ctx context.Context, usage responsesUsage) {
	params := AddParams{
		Prompt:              usage.InputTokens,
		Completion:          usage.OutputTokens,
		Total:               usage.TotalTokens,
		Cached:              usage.InputDetails.CachedTokens,
		Reasoning:           usage.OutputDetails.ReasoningTokens,
		CacheDetailsPresent: usage.InputDetails.Present,
	}
	if params.Total == 0 {
		params.Total = params.Prompt + params.Completion
	}
	TokenTracker.Add(params)
	TokenTracker.AddByModel(m.model, params.Prompt, params.Completion, params.Total)
	if local := TokenTrackerFromContext(ctx); local != nil {
		local.Add(params)
		local.AddByModel(m.model, params.Prompt, params.Completion, params.Total)
	}
	if notify := UsageNotifierFromContext(ctx); notify != nil {
		notify()
	}
}

func collectResponsesStream(stream *schema.StreamReader[*schema.Message]) (*schema.Message, error) {
	result := &schema.Message{Role: schema.Assistant}
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		mergeResponsesMessage(result, chunk)
	}
	if result.Content == "" && result.ReasoningContent == "" && len(result.ToolCalls) == 0 && len(result.Extra) == 0 {
		return nil, fmt.Errorf("empty response from Responses API")
	}
	return result, nil
}
