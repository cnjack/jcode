package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/providerauth"
)

type fakeCredentialResolver struct {
	mu          sync.Mutex
	credentials []providerauth.Credential
	calls       int
}

func (resolver *fakeCredentialResolver) Credential(
	_ context.Context,
	_ providerauth.Binding,
) (providerauth.Credential, error) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	if len(resolver.credentials) == 0 {
		return providerauth.Credential{}, fmt.Errorf("no fake credential")
	}
	index := resolver.calls
	if index >= len(resolver.credentials) {
		index = len(resolver.credentials) - 1
	}
	resolver.calls++
	return resolver.credentials[index], nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestManagedHeaderDoerResolvesCredentialPerRequest(t *testing.T) {
	var gotAuthorization string
	var gotManagedHeader string
	var gotConfiguredHeader string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		gotAuthorization = request.Header.Get("Authorization")
		gotManagedHeader = request.Header.Get("x-managed-account")
		gotConfiguredHeader = request.Header.Get("x-configured-secret")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("{}")),
			Request:    request,
		}, nil
	})}
	doer := &headerDoer{
		base:    client,
		headers: map[string]string{"x-configured-secret": "configured"},
		credential: func(context.Context) (string, map[string]string, error) {
			return "fresh-request-token", map[string]string{
				"x-managed-account":   "account-1",
				"x-configured-secret": "protected",
			}, nil
		},
	}
	request, err := http.NewRequestWithContext(
		context.Background(), http.MethodPost, "https://example.test/v1/chat/completions", nil,
	)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer sentinel")
	response, err := doer.Do(request)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	_ = response.Body.Close()
	if gotAuthorization != "Bearer fresh-request-token" {
		t.Fatalf("Authorization = %q", gotAuthorization)
	}
	if gotManagedHeader != "account-1" {
		t.Fatalf("managed header = %q", gotManagedHeader)
	}
	if gotConfiguredHeader != "protected" {
		t.Fatalf("protected header = %q", gotConfiguredHeader)
	}
}

func TestManagedChatCompletionsIgnoreConfiguredSecrets(t *testing.T) {
	resolver := &fakeCredentialResolver{credentials: []providerauth.Credential{{
		Token: "token", BaseURL: "https://api.githubcopilot.com", Protocol: providerauth.ProtocolChatCompletions,
	}}}
	providerConfig := &config.ProviderConfig{
		Auth:    &config.ProviderAuthBinding{Method: string(providerauth.MethodGitHubCopilot)},
		Headers: map[string]string{"x-configured-secret": "must-not-leak"},
	}
	created, err := newManagedChatModel(
		context.Background(), "github-copilot", "gpt-4.1", providerConfig, true, resolver,
	)
	if err != nil {
		t.Fatalf("create managed chat model: %v", err)
	}
	managed, ok := created.(*chatModel)
	if !ok {
		t.Fatalf("model type = %T, want *chatModel", created)
	}
	if managed.client == nil {
		t.Fatal("managed chat model client is nil")
	}
	if resolver.calls != 1 {
		t.Fatalf("credential calls = %d, want one construction lookup", resolver.calls)
	}
}

func TestManagedResponsesSelectsPinnedTransport(t *testing.T) {
	resolver := &fakeCredentialResolver{credentials: []providerauth.Credential{{
		Token: "token", BaseURL: "https://api.x.ai/v1", Protocol: providerauth.ProtocolResponses,
	}}}
	providerConfig := &config.ProviderConfig{
		Auth:            &config.ProviderAuthBinding{Method: string(providerauth.MethodXAIOAuth)},
		ReasoningEffort: "high",
	}
	created, err := newManagedChatModel(
		context.Background(), "xai", "grok-4.5", providerConfig, true, resolver,
	)
	if err != nil {
		t.Fatalf("create managed responses model: %v", err)
	}
	responses, ok := created.(*responsesModel)
	if !ok {
		t.Fatalf("model type = %T, want *responsesModel", created)
	}
	if responses.endpoint != "https://api.x.ai/v1/responses" {
		t.Fatalf("endpoint = %q", responses.endpoint)
	}
	if responses.codex {
		t.Fatal("xAI responses model must not enable Codex request restrictions")
	}
}

func TestManagedRuntimeProfileChangeFailsClosed(t *testing.T) {
	initial := providerauth.Credential{
		Token: "one", BaseURL: "https://api.githubcopilot.com", Protocol: providerauth.ProtocolChatCompletions,
	}
	resolver := &fakeCredentialResolver{credentials: []providerauth.Credential{{
		Token: "two", BaseURL: "https://changed.example.test", Protocol: providerauth.ProtocolChatCompletions,
	}}}
	credential := managedCredential(
		resolver,
		providerauth.Binding{Method: providerauth.MethodGitHubCopilot},
		initial,
	)
	if _, _, err := credential(context.Background()); err == nil {
		t.Fatal("expected runtime profile drift to fail closed")
	}
}

func TestManagedProviderMethodMismatchFailsBeforeCredentialLookup(t *testing.T) {
	resolver := &fakeCredentialResolver{credentials: []providerauth.Credential{{
		Token: "must-not-be-used", BaseURL: "https://api.x.ai/v1", Protocol: providerauth.ProtocolResponses,
	}}}
	providerConfig := &config.ProviderConfig{
		Auth: &config.ProviderAuthBinding{Method: string(providerauth.MethodXAIOAuth)},
	}
	if _, err := newManagedChatModel(
		context.Background(), "openai", "gpt-5", providerConfig, true, resolver,
	); err == nil {
		t.Fatal("expected mismatched provider and auth method to fail")
	}
	if resolver.calls != 0 {
		t.Fatalf("credential calls = %d, want zero", resolver.calls)
	}
}

func TestManagedChatCompletionsDoNotFollowRedirects(t *testing.T) {
	var targetReached atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetReached.Store(true)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL+"/stolen", http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	created, err := NewChatModel(context.Background(), &ChatModelConfig{
		Model:   "gpt-4.1",
		BaseURL: source.URL,
		Credential: func(context.Context) (string, map[string]string, error) {
			return "managed-secret", map[string]string{"x-managed-account": "account-1"}, nil
		},
	})
	if err != nil {
		t.Fatalf("create managed chat model: %v", err)
	}
	if _, err := created.Generate(context.Background(), nil); err == nil {
		t.Fatal("expected redirect response to fail")
	}
	if targetReached.Load() {
		t.Fatal("managed request followed redirect to a second origin")
	}
}

func TestManagedCopilotClassifiesRequestsAndKeepsInteractionStable(t *testing.T) {
	type observedHeaders struct {
		initiator       string
		interactionType string
		interactionID   string
	}
	var observed []observedHeaders
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed = append(observed, observedHeaders{
			initiator:       request.Header.Get("x-initiator"),
			interactionType: request.Header.Get("x-interaction-type"),
			interactionID:   request.Header.Get("x-interaction-id"),
		})
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": "ok"},
			}},
		})
	}))
	defer server.Close()

	resolver := &fakeCredentialResolver{credentials: []providerauth.Credential{{
		Token: "token", BaseURL: server.URL, Protocol: providerauth.ProtocolChatCompletions,
		Headers: map[string]string{
			"x-initiator":        "user",
			"x-interaction-type": "conversation-agent",
		},
	}}}
	created, err := newManagedChatModel(
		context.Background(),
		"github-copilot",
		"gpt-4.1",
		&config.ProviderConfig{Auth: &config.ProviderAuthBinding{
			Method: string(providerauth.MethodGitHubCopilot),
		}},
		true,
		resolver,
	)
	if err != nil {
		t.Fatalf("create managed Copilot model: %v", err)
	}
	sessionContext := WithProviderSessionID(context.Background(), "session-123")
	if _, err := created.Generate(sessionContext, []*schema.Message{schema.UserMessage("start")}); err != nil {
		t.Fatalf("user request: %v", err)
	}
	if _, err := created.Generate(sessionContext, []*schema.Message{
		schema.UserMessage("start"),
		schema.ToolMessage("result", "call-1"),
	}); err != nil {
		t.Fatalf("tool continuation: %v", err)
	}
	if _, err := created.Generate(WithProviderSubagent(sessionContext), []*schema.Message{
		schema.UserMessage("delegated"),
	}); err != nil {
		t.Fatalf("subagent request: %v", err)
	}

	if len(observed) != 3 {
		t.Fatalf("requests = %d, want 3", len(observed))
	}
	if observed[0].initiator != "user" || observed[0].interactionType != "conversation-agent" {
		t.Fatalf("user headers = %+v", observed[0])
	}
	if observed[1].initiator != "agent" || observed[1].interactionType != "conversation-agent" {
		t.Fatalf("tool headers = %+v", observed[1])
	}
	if observed[2].initiator != "agent" || observed[2].interactionType != "conversation-subagent" {
		t.Fatalf("subagent headers = %+v", observed[2])
	}
	if observed[0].interactionID == "" || observed[0].interactionID != observed[1].interactionID ||
		observed[0].interactionID != observed[2].interactionID {
		t.Fatalf("interaction ids are not stable: %+v", observed)
	}
}
