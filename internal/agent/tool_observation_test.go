package agent

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestToolObservationSeesEinoFinalSchemasAndSameBatchBypass(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "direct_tool"}
	deferred := &agentToolSearchTestTool{name: "deferred_alpha"}
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{
			{ID: "search-same-batch", Function: schema.FunctionCall{
				Name: ToolSearchReservedName, Arguments: `{"query":"select:deferred_alpha"}`,
			}},
			{ID: "target-same-batch", Function: schema.FunctionCall{
				Name: "deferred_alpha", Arguments: `{"value":"must-be-detected"}`,
			}},
		}),
		schema.AssistantMessage("done", nil),
	}}
	plan := ToolPlan{
		Direct:   []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{agentToolSearchDescriptor(deferred, ToolExposureDeferred)},
	}
	ag, err := NewAgentWithToolPlan(context.Background(), model, plan, "test", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var observations []ToolObservation
	ctx := WithToolObservationSink(context.Background(), func(observation ToolObservation) {
		mu.Lock()
		observations = append(observations, observation)
		mu.Unlock()
	})
	runObservedAgent(ctx, t, ag)

	mu.Lock()
	got := append([]ToolObservation(nil), observations...)
	mu.Unlock()
	var modelRequests, searches, bypasses []ToolObservation
	for _, observation := range got {
		switch observation.Kind {
		case ToolObservationModelRequest:
			modelRequests = append(modelRequests, observation)
		case ToolObservationSearch:
			searches = append(searches, observation)
		case ToolObservationBypass:
			bypasses = append(bypasses, observation)
		}
	}
	if len(modelRequests) != 2 {
		t.Fatalf("model request observations = %#v", modelRequests)
	}
	if containsAgentToolSearchName(modelRequests[0].VisibleNames, "deferred_alpha") {
		t.Fatalf("first request exposed deferred tool: %#v", modelRequests[0])
	}
	if !containsAgentToolSearchName(modelRequests[0].VisibleNames, ToolSearchReservedName) {
		t.Fatalf("first request omitted tool_search: %#v", modelRequests[0])
	}
	if !containsAgentToolSearchName(modelRequests[1].VisibleNames, "deferred_alpha") ||
		strings.Join(modelRequests[1].NewlyVisibleDeferred, ",") != "deferred_alpha" {
		t.Fatalf("second request did not record activation: %#v", modelRequests[1])
	}
	if len(searches) != 1 || searches[0].ToolCallID != "search-same-batch" || !searches[0].Success {
		t.Fatalf("search observations = %#v", searches)
	}
	if len(bypasses) != 1 || bypasses[0].ToolCallID != "target-same-batch" ||
		bypasses[0].Reason != "not_visible_in_last_model_request" {
		t.Fatalf("bypass observations = %#v", bypasses)
	}
}

func TestToolObservationTracksVisibilitySearchAndBypassWithoutPayloads(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "direct_tool"}
	deferred := &agentToolSearchTestTool{name: "deferred_write"}
	middlewareValue, err := newToolObservationMiddleware(context.Background(), []tool.BaseTool{deferred})
	if err != nil {
		t.Fatalf("newToolObservationMiddleware() error = %v", err)
	}
	middleware := middlewareValue.(*toolObservationMiddleware)

	var mu sync.Mutex
	var observations []ToolObservation
	ctx := WithToolObservationSink(context.Background(), func(observation ToolObservation) {
		mu.Lock()
		observations = append(observations, observation)
		mu.Unlock()
	})

	directInfo, _ := direct.Info(ctx)
	searchInfo := &schema.ToolInfo{Name: ToolSearchReservedName, Desc: "search"}
	if _, err := middleware.WrapModel(ctx, nil, &adk.ModelContext{
		Tools: []*schema.ToolInfo{directInfo, searchInfo}, //nolint:staticcheck // exercise the intentional final-attempt compatibility hook
	}); err != nil {
		t.Fatalf("WrapModel() error = %v", err)
	}

	deferredEndpoint, err := middleware.WrapInvokableToolCall(
		ctx,
		func(_ context.Context, _ string, _ ...tool.Option) (string, error) {
			return "endpoint-secret-output", nil
		},
		&adk.ToolContext{Name: "deferred_write", CallID: "deferred-before-search"},
	)
	if err != nil {
		t.Fatalf("WrapInvokableToolCall(deferred) error = %v", err)
	}
	if _, err := deferredEndpoint(ctx, `{"password":"argument-secret"}`); err != nil {
		t.Fatalf("deferred endpoint error = %v", err)
	}

	searchEndpoint, err := middleware.WrapInvokableToolCall(
		ctx,
		func(_ context.Context, _ string, _ ...tool.Option) (string, error) {
			return `{"matches":["deferred_write"]}`, nil
		},
		&adk.ToolContext{Name: ToolSearchReservedName, CallID: "search-1"},
	)
	if err != nil {
		t.Fatalf("WrapInvokableToolCall(search) error = %v", err)
	}
	const sensitiveQuery = "select:deferred_write,private_customer_name"
	if _, err := searchEndpoint(ctx, `{"query":"`+sensitiveQuery+`","max_results":9}`); err != nil {
		t.Fatalf("search endpoint error = %v", err)
	}

	deferredInfo, _ := deferred.Info(ctx)
	if _, err := middleware.WrapModel(ctx, nil, &adk.ModelContext{
		Tools: []*schema.ToolInfo{directInfo, searchInfo, deferredInfo}, //nolint:staticcheck // exercise the intentional final-attempt compatibility hook
	}); err != nil {
		t.Fatalf("second WrapModel() error = %v", err)
	}

	mu.Lock()
	got := append([]ToolObservation(nil), observations...)
	mu.Unlock()
	if len(got) != 4 {
		t.Fatalf("observation count = %d, want 4: %#v", len(got), got)
	}
	if got[0].Kind != ToolObservationModelRequest || got[0].VisibleCount != 2 || got[0].SchemaBytes == 0 || got[0].SchemaTokensEstimate == 0 {
		t.Fatalf("first model observation = %#v", got[0])
	}
	if got[1].Kind != ToolObservationBypass || got[1].ToolName != "deferred_write" || got[1].ModelRequestSeq != 1 {
		t.Fatalf("bypass observation = %#v", got[1])
	}
	search := got[2]
	if search.Kind != ToolObservationSearch || search.QueryMode != "select" || search.QueryBytes != len(sensitiveQuery) || search.MaxResults != 9 || !search.Success || search.Redundant {
		t.Fatalf("search observation = %#v", search)
	}
	if strings.Join(search.ValidatedSelectNames, ",") != "deferred_write" || search.UnknownSelectCount != 1 || strings.Join(search.NewMatchNames, ",") != "deferred_write" {
		t.Fatalf("search selection metadata = %#v", search)
	}
	if got[3].ModelRequestSeq != 2 || strings.Join(got[3].NewlyVisibleDeferred, ",") != "deferred_write" {
		t.Fatalf("second model observation = %#v", got[3])
	}

	persisted, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal observations: %v", err)
	}
	for _, secret := range []string{sensitiveQuery, "private_customer_name", "argument-secret", "endpoint-secret-output"} {
		if strings.Contains(string(persisted), secret) {
			t.Fatalf("metadata leaked %q: %s", secret, persisted)
		}
	}
}

func TestToolObservationMarksRepeatedAndRedundantSearch(t *testing.T) {
	deferred := &agentToolSearchTestTool{name: "deferred_alpha"}
	middlewareValue, err := newToolObservationMiddleware(context.Background(), []tool.BaseTool{deferred})
	if err != nil {
		t.Fatal(err)
	}
	middleware := middlewareValue.(*toolObservationMiddleware)

	var got []ToolObservation
	ctx := WithToolObservationSink(context.Background(), func(observation ToolObservation) {
		got = append(got, observation)
	})
	endpoint, err := middleware.WrapInvokableToolCall(
		ctx,
		func(_ context.Context, _ string, _ ...tool.Option) (string, error) {
			return `{"matches":["deferred_alpha"]}`, nil
		},
		&adk.ToolContext{Name: ToolSearchReservedName, CallID: "search-repeat"},
	)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := endpoint(ctx, `{"query":"+deferred alpha"}`); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 {
		t.Fatalf("observation count = %d, want 2", len(got))
	}
	if got[0].QueryMode != "keyword" || got[0].TermCount != 2 || got[0].RequiredTermCount != 1 || got[0].RepeatedQuery || got[0].Redundant {
		t.Fatalf("first search = %#v", got[0])
	}
	if !got[1].RepeatedQuery || !got[1].Redundant || len(got[1].NewMatchNames) != 0 {
		t.Fatalf("second search = %#v", got[1])
	}
}

func runObservedAgent(ctx context.Context, t *testing.T, ag *adk.ChatModelAgent) {
	t.Helper()
	iterator := ag.Run(ctx, &adk.AgentInput{
		Messages: []adk.Message{schema.UserMessage("observe tool disclosure")},
	})
	for {
		event, ok := iterator.Next()
		if !ok {
			return
		}
		if event.Err != nil {
			t.Fatalf("agent run error = %v", event.Err)
		}
		if event.Output == nil || event.Output.MessageOutput == nil || !event.Output.MessageOutput.IsStreaming {
			continue
		}
		stream := event.Output.MessageOutput.MessageStream
		for {
			_, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("agent output stream error = %v", err)
			}
		}
	}
}
