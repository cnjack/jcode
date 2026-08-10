package model

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/model/responsemeta"
)

type capturedResponsesRequest struct {
	Path      string
	Header    http.Header
	Body      []byte
	JSON      map[string]json.RawMessage
	CallIndex int
}

type responsesRoundTripFunc func(*http.Request) (*http.Response, error)

func (f responsesRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestResponsesCodexGenerateUsesStrictContractAndOpaqueContinuity(t *testing.T) {
	captured := make(chan capturedResponsesRequest, 1)
	client := &http.Client{Transport: responsesRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]json.RawMessage
		_ = json.Unmarshal(body, &decoded)
		captured <- capturedResponsesRequest{Path: r.URL.Path, Header: r.Header.Clone(), Body: body, JSON: decoded}
		stream := "event: response.reasoning_summary_text.delta\n" +
			"data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"brief reason\"}\n\n" +
			"event: response.output_text.delta\n" +
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n" +
			"event: response.output_item.done\n" +
			"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs-new\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"must-not-persist\"}],\"encrypted_content\":\"cipher-new\"}}\n\n" +
			"event: response.output_item.done\n" +
			"data: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\\\"x\\\"}\"}}\n\n" +
			"event: response.completed\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"status\":\"completed\",\"output\":[{\"type\":\"reasoning\",\"id\":\"rs-new\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"must-not-persist\"}],\"encrypted_content\":\"cipher-new\"},{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]},{\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\\\"x\\\"}\"}],\"usage\":{\"input_tokens\":10,\"output_tokens\":5,\"total_tokens\":15,\"input_tokens_details\":{\"cached_tokens\":3},\"output_tokens_details\":{\"reasoning_tokens\":2}}}}\n\n" +
			"data: [DONE]\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(stream)),
			Request:    r,
		}, nil
	})}

	var credentialCalls atomic.Int32
	chatModel, err := NewResponsesModel(context.Background(), &ResponsesModelConfig{
		Model:   "gpt-test",
		BaseURL: "https://example.test/backend-api/codex",
		Headers: map[string]string{"Authorization": "Bearer stale", "X-Account": "stale"},
		Credential: func(context.Context) (string, map[string]string, error) {
			credentialCalls.Add(1)
			return "fresh-token", map[string]string{"X-Account": "fresh"}, nil
		},
		Codex:      true,
		HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}

	oldOpaque := json.RawMessage(`{"type":"reasoning","id":"rs-old","summary":[{"type":"summary_text","text":"clear-old"}],"encrypted_content":"cipher-old"}`)
	message, err := chatModel.Generate(context.Background(), []*schema.Message{
		schema.SystemMessage("follow the project rules"),
		schema.UserMessage("first"),
		{
			Role: schema.Assistant, Content: "prior",
			Extra: map[string]any{responsemeta.OpaqueItemsExtraKey: []json.RawMessage{oldOpaque}},
		},
		schema.UserMessage("continue"),
	}, einomodel.WithMaxTokens(99), einomodel.WithTemperature(0.4))
	if err != nil {
		t.Fatal(err)
	}
	if credentialCalls.Load() != 1 {
		t.Fatalf("credential calls = %d, want 1", credentialCalls.Load())
	}
	if message.Content != "hello" || message.ReasoningContent != "brief reason" {
		t.Fatalf("message = %#v", message)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "call-1" ||
		message.ToolCalls[0].Function.Arguments != `{"q":"x"}` {
		t.Fatalf("tool calls = %#v", message.ToolCalls)
	}
	opaque := responsemeta.FromExtra(message.Extra)
	if len(opaque) != 1 || !strings.Contains(string(opaque[0]), "cipher-new") ||
		strings.Contains(string(opaque[0]), "must-not-persist") {
		t.Fatalf("opaque items = %s", opaque)
	}
	if message.ResponseMeta == nil || message.ResponseMeta.Usage == nil ||
		message.ResponseMeta.Usage.TotalTokens != 15 {
		t.Fatalf("response meta = %#v", message.ResponseMeta)
	}

	request := <-captured
	if request.Path != "/backend-api/codex/responses" {
		t.Fatalf("path = %q", request.Path)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer fresh-token" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := request.Header.Get("X-Account"); got != "fresh" {
		t.Fatalf("X-Account = %q", got)
	}
	assertJSONField(t, request.JSON, "stream", "true")
	assertJSONField(t, request.JSON, "store", "false")
	assertJSONField(t, request.JSON, "instructions", `"follow the project rules"`)
	assertJSONField(t, request.JSON, "tools", "[]")
	assertJSONField(t, request.JSON, "parallel_tool_calls", "false")
	assertJSONField(t, request.JSON, "include", `["reasoning.encrypted_content"]`)
	for _, forbidden := range []string{"max_output_tokens", "temperature", "top_p"} {
		if _, ok := request.JSON[forbidden]; ok {
			t.Errorf("Codex request contains forbidden field %q", forbidden)
		}
	}
	requestText := string(request.Body)
	if !strings.Contains(requestText, "cipher-old") || strings.Contains(requestText, "clear-old") {
		t.Fatalf("request did not replay only canonical opaque reasoning: %s", requestText)
	}
}

func TestResponsesStandardGenerateRefreshesCredentialPerDispatch(t *testing.T) {
	captured := make(chan capturedResponsesRequest, 2)
	client := &http.Client{Transport: responsesRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]json.RawMessage
		_ = json.Unmarshal(body, &decoded)
		captured <- capturedResponsesRequest{Header: r.Header.Clone(), Body: body, JSON: decoded}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"id":"resp","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
			)),
			Request: r,
		}, nil
	})}
	var calls atomic.Int32
	chatModel, err := NewResponsesModel(context.Background(), &ResponsesModelConfig{
		Model: "grok-test", BaseURL: "https://example.test/v1", Vision: true, HTTPClient: client,
		Credential: func(context.Context) (string, map[string]string, error) {
			call := calls.Add(1)
			return "token-" + string('0'+call), nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		message, generateErr := chatModel.Generate(
			context.Background(), []*schema.Message{schema.UserMessage("hi")},
			einomodel.WithMaxTokens(123), einomodel.WithTemperature(0.2), einomodel.WithTopP(0.9),
		)
		if generateErr != nil || message.Content != "ok" {
			t.Fatalf("Generate message=%#v err=%v", message, generateErr)
		}
	}
	for index := 1; index <= 2; index++ {
		request := <-captured
		wantAuth := "Bearer token-" + string(rune('0'+index))
		if request.Header.Get("Authorization") != wantAuth {
			t.Fatalf("dispatch %d Authorization = %q, want %q", index, request.Header.Get("Authorization"), wantAuth)
		}
		assertJSONField(t, request.JSON, "stream", "false")
		assertJSONField(t, request.JSON, "max_output_tokens", "123")
		if _, ok := request.JSON["store"]; ok {
			t.Fatal("standard Responses request unexpectedly contains store")
		}
		if _, ok := request.JSON["include"]; ok {
			t.Fatal("standard Responses request unexpectedly contains include")
		}
	}
}

func TestResponsesDispatchRejectsEmptyDynamicToken(t *testing.T) {
	var dispatched atomic.Bool
	client := &http.Client{Transport: responsesRoundTripFunc(func(*http.Request) (*http.Response, error) {
		dispatched.Store(true)
		return nil, errors.New("must not dispatch")
	})}
	chatModel, err := NewResponsesModel(context.Background(), &ResponsesModelConfig{
		Model: "grok-test", BaseURL: "https://example.test/v1", HTTPClient: client,
		Credential: func(context.Context) (string, map[string]string, error) {
			return "  ", map[string]string{"Authorization": "Bearer stale"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = chatModel.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")})
	if err == nil || !strings.Contains(err.Error(), "empty token") {
		t.Fatalf("error = %v, want empty-token failure", err)
	}
	if dispatched.Load() {
		t.Fatal("request was dispatched without a dynamic token")
	}
}

func TestResponsesDispatchDoesNotFollow307Or308Redirects(t *testing.T) {
	for _, statusCode := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			targetRequests := make(chan capturedResponsesRequest, 1)
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				targetRequests <- capturedResponsesRequest{Header: r.Header.Clone(), Body: body}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w,
					`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"redirected"}]}]}`,
				)
			}))
			defer target.Close()

			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", target.URL+"/capture")
				w.WriteHeader(statusCode)
			}))
			defer redirect.Close()

			var injectedPolicyCalls atomic.Int32
			injected := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
				injectedPolicyCalls.Add(1)
				return nil
			}}
			chatModel, err := NewResponsesModel(context.Background(), &ResponsesModelConfig{
				Model: "grok-test", BaseURL: redirect.URL, HTTPClient: injected,
				Headers: map[string]string{
					"Authorization": "Bearer stale-token",
					"X-Protected":   "stale-protected",
				},
				Credential: func(context.Context) (string, map[string]string, error) {
					return "dynamic-token", map[string]string{"X-Protected": "dynamic-protected"}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			_, err = chatModel.Generate(context.Background(), []*schema.Message{
				schema.UserMessage("do-not-forward-body"),
			})
			var apiErr *ResponsesAPIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != statusCode {
				t.Fatalf("Generate error = %v, want ResponsesAPIError status %d", err, statusCode)
			}
			if injectedPolicyCalls.Load() != 0 {
				t.Fatalf("injected redirect policy was used %d time(s)", injectedPolicyCalls.Load())
			}
			select {
			case leaked := <-targetRequests:
				t.Fatalf("redirect target received body=%q Authorization=%q X-Protected=%q",
					leaked.Body, leaked.Header.Get("Authorization"), leaked.Header.Get("X-Protected"))
			default:
			}
			// NewResponsesModel must not mutate a caller-owned client while replacing
			// the redirect policy on its private shallow clone.
			if err := injected.CheckRedirect(nil, nil); err != nil || injectedPolicyCalls.Load() != 1 {
				t.Fatalf("injected client redirect policy was mutated: calls=%d err=%v",
					injectedPolicyCalls.Load(), err)
			}
		})
	}
}

func TestResponsesConversationItemsDropOrphanOpaqueReasoning(t *testing.T) {
	opaque := json.RawMessage(`{"type":"reasoning","id":"rs-orphan","summary":[],"encrypted_content":"cipher-orphan"}`)
	extra := responsemeta.Extra([]json.RawMessage{opaque})

	orphan, err := responsesConversationItems(&schema.Message{
		Role: schema.Assistant, Extra: extra,
	}, false, NewModelImageBudget())
	if err != nil {
		t.Fatal(err)
	}
	if len(orphan) != 0 {
		t.Fatalf("orphan assistant items = %s, want none", orphan)
	}

	withContent, err := responsesConversationItems(&schema.Message{
		Role: schema.Assistant, Content: "visible", Extra: extra,
	}, false, NewModelImageBudget())
	if err != nil {
		t.Fatal(err)
	}
	if len(withContent) != 2 || !strings.Contains(string(withContent[0]), "cipher-orphan") ||
		!strings.Contains(string(withContent[1]), "visible") {
		t.Fatalf("assistant content items = %s", withContent)
	}

	withToolCall, err := responsesConversationItems(&schema.Message{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{{
			ID: "call-1", Function: schema.FunctionCall{Name: "lookup", Arguments: `{}`},
		}},
		Extra: extra,
	}, false, NewModelImageBudget())
	if err != nil {
		t.Fatal(err)
	}
	if len(withToolCall) != 2 || !strings.Contains(string(withToolCall[0]), "cipher-orphan") ||
		!strings.Contains(string(withToolCall[1]), `"type":"function_call"`) {
		t.Fatalf("assistant tool-call items = %s", withToolCall)
	}
}

func TestDecodeResponsesSSEDeduplicatesDoneAndCompletedItems(t *testing.T) {
	stream := "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"reasoning\",\"id\":\"rs\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"why\"}],\"encrypted_content\":\"cipher\"}}\n\n" +
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"answer\"}]}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"reasoning\",\"id\":\"rs\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"why\"}],\"encrypted_content\":\"cipher\"},{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"answer\"}]}]}}\n\n"
	message := &schema.Message{Role: schema.Assistant}
	_, err := decodeResponsesSSE(strings.NewReader(stream), func(chunk *schema.Message) error {
		mergeResponsesMessage(message, chunk)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "answer" || message.ReasoningContent != "why" {
		t.Fatalf("deduplicated message = %#v", message)
	}
	if items := responsemeta.FromExtra(message.Extra); len(items) != 1 {
		t.Fatalf("opaque items = %s", items)
	}
}

func TestDecodeResponsesSSEPreservesOutputIndexesForParallelToolCalls(t *testing.T) {
	stream := "data: {\"type\":\"response.output_item.done\",\"output_index\":2,\"item\":{\"type\":\"function_call\",\"call_id\":\"call-a\",\"name\":\"first\",\"arguments\":\"{}\"}}\n\n" +
		"data: {\"type\":\"response.output_item.done\",\"output_index\":5,\"item\":{\"type\":\"function_call\",\"call_id\":\"call-b\",\"name\":\"second\",\"arguments\":\"{}\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"
	var calls []schema.ToolCall
	_, err := decodeResponsesSSE(strings.NewReader(stream), func(message *schema.Message) error {
		calls = append(calls, message.ToolCalls...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertToolCallIndexes(t, calls, []int{2, 5})
}

func TestDecodeResponsesSSEAssignsOutputIndexesToCompletedFallbackToolCalls(t *testing.T) {
	stream := "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[" +
		"{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"working\"}]}," +
		"{\"type\":\"function_call\",\"call_id\":\"call-a\",\"name\":\"first\",\"arguments\":\"{}\"}," +
		"{\"type\":\"function_call\",\"call_id\":\"call-b\",\"name\":\"second\",\"arguments\":\"{}\"}]}}\n\n"
	var calls []schema.ToolCall
	_, err := decodeResponsesSSE(strings.NewReader(stream), func(message *schema.Message) error {
		calls = append(calls, message.ToolCalls...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertToolCallIndexes(t, calls, []int{1, 2})
}

func TestDecodeResponsesSSERejectsTruncatedStream(t *testing.T) {
	stream := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"
	_, err := decodeResponsesSSE(strings.NewReader(stream), func(*schema.Message) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "terminal event") {
		t.Fatalf("error = %v, want missing terminal event", err)
	}
}

func TestDecodeResponsesSSEAcceptsCRLF(t *testing.T) {
	stream := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\r\n\r\n" +
		"data: [DONE]\r\n\r\n"
	var content string
	_, err := decodeResponsesSSE(strings.NewReader(stream), func(message *schema.Message) error {
		content += message.Content
		return nil
	})
	if err != nil || content != "ok" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func assertJSONField(t *testing.T, object map[string]json.RawMessage, key, want string) {
	t.Helper()
	got, ok := object[key]
	if !ok {
		t.Fatalf("missing JSON field %q in %#v", key, object)
	}
	if string(got) != want {
		t.Fatalf("JSON field %q = %s, want %s", key, got, want)
	}
}

func assertToolCallIndexes(t *testing.T, calls []schema.ToolCall, want []int) {
	t.Helper()
	if len(calls) != len(want) {
		t.Fatalf("tool calls = %#v, want %d calls", calls, len(want))
	}
	for index, call := range calls {
		if call.Index == nil || *call.Index != want[index] {
			t.Fatalf("tool call %d index = %v, want %d", index, call.Index, want[index])
		}
	}
}
