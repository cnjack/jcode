package model

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/model/responsemeta"
)

const (
	maxResponsesJSONBytes      = 16 << 20
	maxResponsesErrorBytes     = 64 << 10
	maxResponsesSSEEventBytes  = 2 << 20
	maxResponsesTextBytes      = 32 << 20
	maxResponsesReasoningBytes = 8 << 20
	maxResponsesToolBytes      = 8 << 20
)

type responsesTokenDetails struct {
	CachedTokens    int  `json:"cached_tokens"`
	ReasoningTokens int  `json:"reasoning_tokens"`
	Present         bool `json:"-"`
}

type responsesUsage struct {
	InputTokens   int `json:"input_tokens"`
	OutputTokens  int `json:"output_tokens"`
	TotalTokens   int `json:"total_tokens"`
	InputDetails  responsesTokenDetails
	OutputDetails responsesTokenDetails
}

func (u responsesUsage) hasUsage() bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.TotalTokens > 0
}

type responsesUsageWire struct {
	InputTokens         int                    `json:"input_tokens"`
	OutputTokens        int                    `json:"output_tokens"`
	TotalTokens         int                    `json:"total_tokens"`
	InputTokensDetails  *responsesTokenDetails `json:"input_tokens_details"`
	OutputTokensDetails *responsesTokenDetails `json:"output_tokens_details"`
}

func (u responsesUsageWire) normalized() responsesUsage {
	result := responsesUsage{
		InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, TotalTokens: u.TotalTokens,
	}
	if u.InputTokensDetails != nil {
		result.InputDetails = *u.InputTokensDetails
		result.InputDetails.Present = true
	}
	if u.OutputTokensDetails != nil {
		result.OutputDetails = *u.OutputTokensDetails
		result.OutputDetails.Present = true
	}
	return result
}

type responsesErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

type responsesEnvelope struct {
	ID                string              `json:"id"`
	Status            string              `json:"status"`
	Output            []json.RawMessage   `json:"output"`
	OutputText        string              `json:"output_text"`
	Usage             *responsesUsageWire `json:"usage"`
	Error             *responsesErrorBody `json:"error"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
}

type responsesOutputItem struct {
	Type             string `json:"type"`
	ID               string `json:"id"`
	Role             string `json:"role"`
	CallID           string `json:"call_id"`
	Name             string `json:"name"`
	Arguments        string `json:"arguments"`
	EncryptedContent string `json:"encrypted_content"`
	Content          []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Refusal string `json:"refusal"`
	} `json:"content"`
	Summary []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"summary"`
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("response exceeds %d-byte limit", limit)
	}
	return raw, nil
}

func decodeResponsesJSON(reader io.Reader) (*schema.Message, responsesUsage, error) {
	raw, err := readBounded(reader, maxResponsesJSONBytes)
	if err != nil {
		return nil, responsesUsage{}, fmt.Errorf("read Responses API response: %w", err)
	}
	var response responsesEnvelope
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, responsesUsage{}, fmt.Errorf("decode Responses API response: %w", err)
	}
	if response.Error != nil || response.Status == "failed" {
		return nil, responsesUsage{}, responseEnvelopeError(response)
	}
	message, err := messageFromResponsesEnvelope(response)
	if err != nil {
		return nil, responsesUsage{}, err
	}
	var usage responsesUsage
	if response.Usage != nil {
		usage = response.Usage.normalized()
		message.ResponseMeta = responseMetaFor(usage, response.Status, len(message.ToolCalls) > 0)
	}
	return message, usage, nil
}

func messageFromResponsesEnvelope(response responsesEnvelope) (*schema.Message, error) {
	message := &schema.Message{Role: schema.Assistant}
	for outputIndex, raw := range response.Output {
		itemMessage, err := messageFromResponsesItem(raw, outputIndex)
		if err != nil {
			return nil, err
		}
		mergeResponsesMessage(message, itemMessage)
	}
	if message.Content == "" && response.OutputText != "" {
		message.Content = response.OutputText
	}
	if message.Content == "" && message.ReasoningContent == "" && len(message.ToolCalls) == 0 && len(message.Extra) == 0 {
		return nil, fmt.Errorf("empty response from Responses API")
	}
	return message, nil
}

func messageFromResponsesItem(raw json.RawMessage, outputIndex int) (*schema.Message, error) {
	if len(raw) > maxResponsesSSEEventBytes {
		return nil, fmt.Errorf("responses API output item exceeds %d-byte limit", maxResponsesSSEEventBytes)
	}
	var item responsesOutputItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, fmt.Errorf("decode Responses API output item: %w", err)
	}
	message := &schema.Message{Role: schema.Assistant}
	switch item.Type {
	case "message":
		for _, content := range item.Content {
			switch content.Type {
			case "output_text", "text":
				message.Content += content.Text
			case "refusal":
				message.Content += content.Refusal
			}
		}
	case "reasoning":
		for _, summary := range item.Summary {
			if summary.Type == "summary_text" || summary.Type == "text" {
				message.ReasoningContent += summary.Text
			}
		}
		if opaque, ok := responsemeta.CanonicalReasoningItem(raw); ok {
			message.Extra = responsemeta.Extra([]json.RawMessage{opaque})
		}
	case "function_call":
		if item.CallID == "" || item.Name == "" {
			return nil, fmt.Errorf("responses API returned a function call without call_id or name")
		}
		index := outputIndex
		message.ToolCalls = []schema.ToolCall{{
			Index: &index, ID: item.CallID, Type: "function",
			Function: schema.FunctionCall{Name: item.Name, Arguments: item.Arguments},
		}}
	}
	return message, nil
}

func mergeResponsesMessage(dst, src *schema.Message) {
	if dst == nil || src == nil {
		return
	}
	dst.Content += src.Content
	dst.ReasoningContent += src.ReasoningContent
	if len(src.ToolCalls) > 0 {
		for _, call := range src.ToolCalls {
			idx := len(dst.ToolCalls)
			call.Index = &idx
			dst.ToolCalls = append(dst.ToolCalls, call)
		}
	}
	if len(src.Extra) > 0 {
		if dst.Extra == nil {
			dst.Extra = make(map[string]any, len(src.Extra))
		}
		for key, value := range src.Extra {
			if key == responsemeta.OpaqueItemsExtraKey {
				combined := append(responsemeta.FromExtra(dst.Extra), responsemeta.FromExtra(src.Extra)...)
				if normalized := responsemeta.Normalize(combined); len(normalized) > 0 {
					dst.Extra[key] = normalized
				}
				continue
			}
			dst.Extra[key] = value
		}
	}
	if src.ResponseMeta != nil {
		dst.ResponseMeta = src.ResponseMeta
	}
}

func responseMetaFor(usage responsesUsage, status string, hasTools bool) *schema.ResponseMeta {
	finishReason := "stop"
	if hasTools {
		finishReason = "tool_calls"
	} else if status == "incomplete" {
		finishReason = "length"
	}
	return &schema.ResponseMeta{
		FinishReason: finishReason,
		Usage: &schema.TokenUsage{
			PromptTokens: usage.InputTokens,
			PromptTokenDetails: schema.PromptTokenDetails{
				CachedTokens: usage.InputDetails.CachedTokens,
			},
			CompletionTokens: usage.OutputTokens,
			TotalTokens:      usage.TotalTokens,
			CompletionTokensDetails: schema.CompletionTokensDetails{
				ReasoningTokens: usage.OutputDetails.ReasoningTokens,
			},
		},
	}
}

type responsesSSEEvent struct {
	Type        string              `json:"type"`
	Delta       string              `json:"delta"`
	Message     string              `json:"message"`
	Code        string              `json:"code"`
	OutputIndex int                 `json:"output_index"`
	Item        json.RawMessage     `json:"item"`
	Response    *responsesEnvelope  `json:"response"`
	Error       *responsesErrorBody `json:"error"`
}

type responsesSSEState struct {
	textBytes          int
	reasoningBytes     int
	toolBytes          int
	sawTextDelta       bool
	sawReasoningDelta  bool
	emittedTextItem    bool
	emittedReasoning   bool
	completed          bool
	emittedToolCalls   map[string]bool
	emittedOpaqueItems map[string]bool
	usage              responsesUsage
}

func decodeResponsesSSE(
	reader io.Reader,
	emit func(*schema.Message) error,
) (responsesUsage, error) {
	state := &responsesSSEState{
		emittedToolCalls: make(map[string]bool), emittedOpaqueItems: make(map[string]bool),
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxResponsesSSEEventBytes)
	var eventName string
	var data bytes.Buffer
	flush := func() error {
		if data.Len() == 0 {
			eventName = ""
			return nil
		}
		payload := append([]byte(nil), data.Bytes()...)
		data.Reset()
		name := eventName
		eventName = ""
		if bytes.Equal(bytes.TrimSpace(payload), []byte("[DONE]")) {
			state.completed = true
			return nil
		}
		return state.handleEvent(name, payload, emit)
	}
	for scanner.Scan() {
		line := bytes.TrimSuffix(scanner.Bytes(), []byte{'\r'})
		if len(line) == 0 {
			if err := flush(); err != nil {
				return state.usage, err
			}
			continue
		}
		if line[0] == ':' {
			continue
		}
		if bytes.HasPrefix(line, []byte("event:")) {
			eventName = strings.TrimSpace(string(line[len("event:"):]))
			continue
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			part := bytes.TrimPrefix(line[len("data:"):], []byte(" "))
			if data.Len() > maxResponsesSSEEventBytes-len(part) {
				return state.usage, fmt.Errorf("responses API SSE event exceeds %d-byte limit", maxResponsesSSEEventBytes)
			}
			data.Write(part)
		}
	}
	if err := scanner.Err(); err != nil {
		return state.usage, fmt.Errorf("read Responses API stream: %w", err)
	}
	if err := flush(); err != nil {
		return state.usage, err
	}
	if !state.completed {
		return state.usage, fmt.Errorf("responses API stream ended before a terminal event")
	}
	return state.usage, nil
}

func (s *responsesSSEState) handleEvent(
	eventName string,
	payload []byte,
	emit func(*schema.Message) error,
) error {
	var event responsesSSEEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("decode Responses API SSE event: %w", err)
	}
	if event.Type == "" {
		event.Type = eventName
	}
	switch event.Type {
	case "response.output_text.delta", "response.refusal.delta":
		if err := s.addBounded(&s.textBytes, len(event.Delta), maxResponsesTextBytes, "text"); err != nil {
			return err
		}
		s.sawTextDelta = true
		return emit(&schema.Message{Role: schema.Assistant, Content: event.Delta})
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if err := s.addBounded(&s.reasoningBytes, len(event.Delta), maxResponsesReasoningBytes, "reasoning"); err != nil {
			return err
		}
		s.sawReasoningDelta = true
		return emit(&schema.Message{Role: schema.Assistant, ReasoningContent: event.Delta})
	case "response.output_item.done":
		return s.emitItem(event.Item, event.OutputIndex, emit)
	case "response.completed", "response.incomplete":
		if event.Response == nil {
			return fmt.Errorf("responses API completion event is missing response")
		}
		s.completed = true
		if event.Response.Usage != nil {
			s.usage = event.Response.Usage.normalized()
		}
		if err := s.emitEnvelopeFallback(*event.Response, emit); err != nil {
			return err
		}
		if event.Response.Usage != nil {
			return emit(&schema.Message{
				Role: schema.Assistant,
				ResponseMeta: responseMetaFor(
					s.usage, event.Response.Status, len(s.emittedToolCalls) > 0,
				),
			})
		}
		return nil
	case "response.failed":
		if event.Response != nil {
			return responseEnvelopeError(*event.Response)
		}
		return apiError(0, event.Error, "")
	case "error":
		if event.Error == nil && (event.Message != "" || event.Code != "") {
			event.Error = &responsesErrorBody{Message: event.Message, Code: event.Code}
		}
		return apiError(0, event.Error, "")
	default:
		return nil
	}
}

func (s *responsesSSEState) emitEnvelopeFallback(
	response responsesEnvelope,
	emit func(*schema.Message) error,
) error {
	if response.Error != nil || response.Status == "failed" {
		return responseEnvelopeError(response)
	}
	for outputIndex, raw := range response.Output {
		var item responsesOutputItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return fmt.Errorf("decode Responses API completed output: %w", err)
		}
		switch item.Type {
		case "message":
			if !s.sawTextDelta && !s.emittedTextItem {
				if err := s.emitItem(raw, outputIndex, emit); err != nil {
					return err
				}
			}
		case "reasoning":
			if err := s.emitItem(raw, outputIndex, emit); err != nil {
				return err
			}
		case "function_call":
			if !s.emittedToolCalls[item.CallID] {
				if err := s.emitItem(raw, outputIndex, emit); err != nil {
					return err
				}
			}
		}
	}
	if !s.sawTextDelta && !s.emittedTextItem && response.OutputText != "" {
		if err := s.addBounded(&s.textBytes, len(response.OutputText), maxResponsesTextBytes, "text"); err != nil {
			return err
		}
		return emit(&schema.Message{Role: schema.Assistant, Content: response.OutputText})
	}
	return nil
}

func (s *responsesSSEState) emitItem(
	raw json.RawMessage,
	outputIndex int,
	emit func(*schema.Message) error,
) error {
	message, err := messageFromResponsesItem(raw, outputIndex)
	if err != nil {
		return err
	}
	var item responsesOutputItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return err
	}
	switch item.Type {
	case "message":
		if s.sawTextDelta || s.emittedTextItem {
			return nil
		}
		if err := s.addBounded(&s.textBytes, len(message.Content), maxResponsesTextBytes, "text"); err != nil {
			return err
		}
		if message.Content != "" {
			s.emittedTextItem = true
		}
	case "reasoning":
		if s.sawReasoningDelta || s.emittedReasoning {
			message.ReasoningContent = ""
		} else if err := s.addBounded(&s.reasoningBytes, len(message.ReasoningContent), maxResponsesReasoningBytes, "reasoning"); err != nil {
			return err
		}
		if message.ReasoningContent != "" {
			s.emittedReasoning = true
		}
		if item.EncryptedContent != "" {
			if s.emittedOpaqueItems[item.EncryptedContent] {
				message.Extra = nil
			} else {
				s.emittedOpaqueItems[item.EncryptedContent] = true
			}
		}
	case "function_call":
		if s.emittedToolCalls[item.CallID] {
			return nil
		}
		if err := s.addBounded(&s.toolBytes, len(item.Arguments)+len(item.Name)+len(item.CallID), maxResponsesToolBytes, "tool call"); err != nil {
			return err
		}
		s.emittedToolCalls[item.CallID] = true
	}
	if message.Content == "" && message.ReasoningContent == "" && len(message.ToolCalls) == 0 && len(message.Extra) == 0 {
		return nil
	}
	return emit(message)
}

func (s *responsesSSEState) addBounded(total *int, delta, limit int, label string) error {
	if delta < 0 || *total > limit-delta {
		return fmt.Errorf("responses API %s exceeds %d-byte limit", label, limit)
	}
	*total += delta
	return nil
}

// ResponsesAPIError is a bounded provider error safe to surface through the
// runner. It never contains request headers or request bodies.
type ResponsesAPIError struct {
	StatusCode int
	Code       string
	Type       string
	Message    string
	RequestID  string
}

func (e *ResponsesAPIError) Error() string {
	if e == nil {
		return "Responses API error"
	}
	parts := []string{"Responses API error"}
	if e.StatusCode != 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.StatusCode))
	}
	if e.Code != "" {
		parts = append(parts, "code="+e.Code)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	return strings.Join(parts, ": ")
}

func decodeResponsesHTTPError(response *http.Response) error {
	raw, readErr := readBounded(response.Body, maxResponsesErrorBytes)
	if readErr != nil {
		return &ResponsesAPIError{StatusCode: response.StatusCode, Message: "bounded error body could not be read"}
	}
	var envelope struct {
		Error *responsesErrorBody `json:"error"`
	}
	_ = json.Unmarshal(raw, &envelope)
	requestID := response.Header.Get("x-request-id")
	return apiError(response.StatusCode, envelope.Error, requestID)
}

func responseEnvelopeError(response responsesEnvelope) error {
	message := response.Error
	if message == nil && response.IncompleteDetails != nil {
		message = &responsesErrorBody{Message: response.IncompleteDetails.Reason}
	}
	return apiError(0, message, response.ID)
}

func apiError(status int, body *responsesErrorBody, requestID string) error {
	if body == nil {
		body = &responsesErrorBody{Message: "provider returned an unspecified error"}
	}
	return &ResponsesAPIError{
		StatusCode: status,
		Code:       boundedErrorText(body.Code, 128),
		Type:       boundedErrorText(body.Type, 128),
		Message:    boundedErrorText(body.Message, 1024),
		RequestID:  boundedErrorText(requestID, 256),
	}
}

func boundedErrorText(value string, maxBytes int) string {
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	return value + "…"
}
