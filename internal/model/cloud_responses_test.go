package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/config"
)

func TestCloudCodexProtocolUsesRunCredentialAndResponses(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/llm/v1/responses" {
			t.Errorf("wrong protocol path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer scoped-run-token" {
			t.Error("run credential missing")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "fixture-codex" || body["store"] != false || body["stream"] != true {
			t.Errorf("Codex request fields %#v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"fixture result\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r-test\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"fixture result\"}]}]}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	m, err := NewChatModelFromProvider(context.Background(), "openai-codex", "fixture-codex", server.URL+"/llm/v1", &config.ProviderConfig{Protocol: "codex_responses", APIKey: "scoped-run-token", ReasoningEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.Generate(context.Background(), []*schema.Message{schema.UserMessage("test")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "fixture result" || calls != 1 {
		t.Fatalf("result=%q calls=%d", result.Content, calls)
	}
}
