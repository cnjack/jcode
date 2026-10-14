package model

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestParseProviderUsage(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		write     int
		read      int
		wantFound bool
	}{
		{"openrouter cache_write_tokens", `{"usage":{"prompt_tokens":100,"prompt_tokens_details":{"cached_tokens":40,"cache_write_tokens":60}}}`, 60, 0, true},
		{"litellm cache_creation_tokens", `{"usage":{"prompt_tokens_details":{"cache_creation_tokens":25}}}`, 25, 0, true},
		{"anthropic style top level", `{"usage":{"cache_creation_input_tokens":30,"cache_read_input_tokens":70}}`, 30, 70, true},
		{"details win over top level", `{"usage":{"cache_creation_input_tokens":30,"prompt_tokens_details":{"cache_write_tokens":31}}}`, 31, 0, true},
		{"deepseek hit tokens", `{"usage":{"prompt_cache_hit_tokens":12,"prompt_cache_miss_tokens":8}}`, 0, 12, true},
		{"moonshot top-level cached", `{"usage":{"prompt_tokens":10,"cached_tokens":9}}`, 0, 9, true},
		{"standard openai only", `{"usage":{"prompt_tokens":10,"prompt_tokens_details":{"cached_tokens":5}}}`, 0, 0, true},
		{"no usage", `{"choices":[{"delta":{"content":"cache"}}]}`, 0, 0, false},
		{"usage without cache keys", `{"usage":{"prompt_tokens":10}}`, 0, 0, false},
		{"null usage", `{"usage":null,"x":"cache"}`, 0, 0, false},
		{"malformed", `{"usage":{"cache_creation_input_tokens":"abc"}}`, 0, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, r, ok := parseProviderUsage([]byte(tc.payload))
			if ok != tc.wantFound || w != tc.write || r != tc.read {
				t.Fatalf("parseProviderUsage = (%d, %d, %v), want (%d, %d, %v)", w, r, ok, tc.write, tc.read, tc.wantFound)
			}
		})
	}
}

func TestUsageCaptureApply(t *testing.T) {
	var nilCapture *usageCapture
	p := AddParams{Prompt: 10}
	nilCapture.apply(&p) // must not panic

	c := &usageCapture{}
	c.apply(&p)
	if p.CacheWrite != 0 || p.Cached != 0 {
		t.Fatalf("unseen capture must not change params: %+v", p)
	}

	c.set(7, 3)
	p = AddParams{Prompt: 10}
	c.apply(&p)
	if p.CacheWrite != 7 || p.Cached != 3 || !p.CacheDetailsPresent {
		t.Fatalf("expected captured write/read to fill in, got %+v", p)
	}

	// Standard prompt_tokens_details wins over the captured cache-read.
	p = AddParams{Prompt: 10, Cached: 0, CacheDetailsPresent: true}
	c.apply(&p)
	if p.Cached != 0 || p.CacheWrite != 7 {
		t.Fatalf("standard cache details must win for cache-read, got %+v", p)
	}
}

// chunkedReader returns at most n bytes per Read so SSE lines are split across
// reads, exercising the sniffer's partial-line handling.
type chunkedReader struct {
	r io.Reader
	n int
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if len(p) > c.n {
		p = p[:c.n]
	}
	return c.r.Read(p)
}

func TestUsageSnifferSSESplitAcrossReads(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"prompt_tokens_details\":{\"cached_tokens\":10,\"cache_write_tokens\":90}}}\n\n" +
		"data: [DONE]\n\n"
	c := &usageCapture{}
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}},
		Body:   io.NopCloser(&chunkedReader{r: strings.NewReader(body), n: 7}),
	}
	s := newUsageSniffer(resp, c)
	if _, err := io.ReadAll(s); err != nil {
		t.Fatal(err)
	}
	p := AddParams{}
	c.apply(&p)
	if p.CacheWrite != 90 {
		t.Fatalf("CacheWrite = %d, want 90", p.CacheWrite)
	}
}

func TestUsageSnifferJSONParsedOnClose(t *testing.T) {
	body := "{\n  \"usage\": {\n    \"prompt_tokens\": 5,\n    \"cache_creation_input_tokens\": 4\n  }\n}"
	c := &usageCapture{}
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(body)),
	}
	s := newUsageSniffer(resp, c)
	buf := make([]byte, len(body)) // read everything but never observe EOF
	if _, err := io.ReadFull(s, buf); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	p := AddParams{}
	c.apply(&p)
	if p.CacheWrite != 4 {
		t.Fatalf("CacheWrite = %d, want 4", p.CacheWrite)
	}
}

func newUsageTestModel(t *testing.T, handler http.HandlerFunc) *chatModel {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	m, err := NewChatModel(context.Background(), &ChatModelConfig{Model: "test-model", APIKey: "k", BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	return m.(*chatModel)
}

func TestChatModelRecordsCacheWrite(t *testing.T) {
	t.Run("stream", func(t *testing.T) {
		m := newUsageTestModel(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"id\":\"1\",\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":5,\"total_tokens\":105,\"prompt_tokens_details\":{\"cached_tokens\":20,\"cache_write_tokens\":80}}}\n\n")
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		})
		tracker := &TokenUsage{}
		ctx := WithTokenTracker(context.Background(), tracker)
		sr, err := m.Stream(ctx, []*schema.Message{schema.UserMessage("hi")})
		if err != nil {
			t.Fatal(err)
		}
		for {
			if _, err := sr.Recv(); err != nil {
				break
			}
		}
		got := tracker.GetFull()
		if got.CacheWriteTokens != 80 || got.CachedTokens != 20 || got.PromptTokens != 100 {
			t.Fatalf("unexpected usage: %+v", got)
		}
	})

	t.Run("generate", func(t *testing.T) {
		m := newUsageTestModel(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"id":"1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":50,"completion_tokens":2,"total_tokens":52,"cache_creation_input_tokens":40,"cache_read_input_tokens":10}}`)
		})
		tracker := &TokenUsage{}
		ctx := WithTokenTracker(context.Background(), tracker)
		if _, err := m.Generate(ctx, []*schema.Message{schema.UserMessage("hi")}); err != nil {
			t.Fatal(err)
		}
		got := tracker.GetFull()
		if got.CacheWriteTokens != 40 || got.CachedTokens != 10 {
			t.Fatalf("unexpected usage: %+v", got)
		}
		if !tracker.CacheObserved() {
			t.Fatal("expected cache to be observed via captured cache-read")
		}
	})
}

func TestResponsesUsageCacheWrite(t *testing.T) {
	tests := []struct {
		name string
		wire responsesUsageWire
		want int
	}{
		{"details cache_write_tokens", responsesUsageWire{InputTokens: 10, InputTokensDetails: &responsesTokenDetails{CacheWriteTokens: 6}}, 6},
		{"details cache_creation_tokens", responsesUsageWire{InputTokens: 10, InputTokensDetails: &responsesTokenDetails{CacheCreationTokens: 5}}, 5},
		{"top-level fallback", responsesUsageWire{InputTokens: 10, CacheCreationInputTokens: 4, InputTokensDetails: &responsesTokenDetails{CachedTokens: 1}}, 4},
		{"none", responsesUsageWire{InputTokens: 10}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := tc.wire.normalized()
			if u.CacheWriteTokens != tc.want {
				t.Fatalf("CacheWriteTokens = %d, want %d", u.CacheWriteTokens, tc.want)
			}
			meta := responseMetaFor(u, "completed", false)
			if meta.Usage.PromptTokenDetails.CacheWriteTokens != tc.want {
				t.Fatalf("ResponseMeta CacheWriteTokens = %d, want %d", meta.Usage.PromptTokenDetails.CacheWriteTokens, tc.want)
			}
		})
	}
}
