package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestExpandToolSearchResultLoadsWholeEffectiveGroup(t *testing.T) {
	groups := testToolDisclosureGroups(
		[]string{"browser_snapshot", "browser_open", "browser_read", "browser_act"},
		"browser.workflow",
	)

	got, ok := expandToolSearchResult(`{"matches":["browser_act"]}`, groups)
	if !ok {
		t.Fatal("expandToolSearchResult() did not expand a partial group match")
	}
	want := `{"matches":["browser_act","browser_open","browser_read","browser_snapshot"]}`
	if got != want {
		t.Fatalf("expanded result = %s, want %s", got, want)
	}
}

func TestExpandToolSearchResultUsesStableDedupAcrossGroups(t *testing.T) {
	browser := testToolDisclosureGroups(
		[]string{"browser_snapshot", "browser_open", "browser_read", "browser_act"},
		"browser.workflow",
	)
	computer := testToolDisclosureGroups(
		[]string{"computer_snapshot", "computer_open", "computer_apps", "computer_act"},
		"computer.workflow",
	)
	for name, group := range computer.byTool {
		browser.byTool[name] = group
	}
	for group, members := range computer.members {
		browser.members[group] = members
	}

	input := `{"matches":["browser_snapshot","unknown","browser_open","browser_snapshot","computer_open"]}`
	got, ok := expandToolSearchResult(input, browser)
	if !ok {
		t.Fatal("expandToolSearchResult() did not expand grouped matches")
	}
	want := `{"matches":["browser_snapshot","unknown","browser_open","computer_open","browser_act","browser_read","computer_act","computer_apps","computer_snapshot"]}`
	if got != want {
		t.Fatalf("expanded result = %s, want %s", got, want)
	}
}

func TestToolSearchDisclosureLeavesUnknownUngroupedAndInvalidResultsUntouched(t *testing.T) {
	groups := testToolDisclosureGroups([]string{"browser_open", "browser_snapshot"}, "browser.workflow")
	for _, input := range []string{
		` {"matches":["unknown","goal_get"]} `,
		`{"matches":`,
		`{"matches":["browser_open"],"future_field":true}`,
		`{"matches":["browser_open"]} {"matches":[]}`,
	} {
		if got, ok := expandToolSearchResult(input, groups); ok || got != "" {
			t.Fatalf("expandToolSearchResult(%q) = (%q, %v), want fail-closed no change", input, got, ok)
		}
	}
}

func TestToolSearchDisclosureEndpointErrorsFailClosed(t *testing.T) {
	groups := testToolDisclosureGroups([]string{"browser_open", "browser_snapshot"}, "browser.workflow")
	middleware := newToolSearchDisclosureMiddleware(groups).(*toolSearchDisclosureMiddleware)
	wantErr := errors.New("search endpoint failed")
	wrapped, err := middleware.WrapInvokableToolCall(
		context.Background(),
		func(context.Context, string, ...tool.Option) (string, error) {
			return `{"matches":["browser_open"]}`, wantErr
		},
		&adk.ToolContext{Name: ToolSearchReservedName},
	)
	if err != nil {
		t.Fatalf("WrapInvokableToolCall() error = %v", err)
	}
	got, err := wrapped(context.Background(), `{}`)
	if !errors.Is(err, wantErr) || got != `{"matches":["browser_open"]}` {
		t.Fatalf("wrapped endpoint = (%q, %v), want original result and error", got, err)
	}
}

func TestToolSearchDisclosureFeedsHistoryVisibilityAndObservation(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "read"}
	browserOpen := &agentToolSearchTestTool{name: "browser_open"}
	browserSnapshot := &agentToolSearchTestTool{name: "browser_snapshot"}
	browserAct := &agentToolSearchTestTool{name: "browser_act"}
	searchArgs := `{"query":"select:browser_open"}`
	openArgs := `{"value":"https://example.test"}`
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		toolCallMessage("search-browser", ToolSearchReservedName, searchArgs),
		toolCallMessage("open-browser", "browser_open", openArgs),
		schema.AssistantMessage("done", nil),
	}}
	approvals := &agentToolSearchApprovalRecorder{}
	plan := ToolPlan{
		Direct: []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{
			groupedAgentToolSearchDescriptor(browserOpen, "browser.workflow"),
			groupedAgentToolSearchDescriptor(browserSnapshot, "browser.workflow"),
			groupedAgentToolSearchDescriptor(browserAct, "browser.workflow"),
		},
	}
	ag, err := NewAgentWithToolPlan(
		context.Background(), model, plan, "test", approvals.approve, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewAgentWithToolPlan() error = %v", err)
	}

	var mu sync.Mutex
	var observations []ToolObservation
	ctx := WithToolObservationSink(context.Background(), func(observation ToolObservation) {
		mu.Lock()
		observations = append(observations, observation)
		mu.Unlock()
	})
	runObservedAgent(ctx, t, ag)

	assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{
		{"read", ToolSearchReservedName},
		{"browser_act", "browser_open", "browser_snapshot", "read", ToolSearchReservedName},
		{"browser_act", "browser_open", "browser_snapshot", "read", ToolSearchReservedName},
	})
	searchResults := model.resultsFor(ToolSearchReservedName)
	if len(searchResults) == 0 {
		t.Fatal("model history omitted tool_search result")
	}
	wantResult := `{"matches":["browser_open","browser_act","browser_snapshot"]}`
	if searchResults[0].content != wantResult {
		t.Fatalf("tool_search history result = %s, want %s", searchResults[0].content, wantResult)
	}

	mu.Lock()
	gotObservations := append([]ToolObservation(nil), observations...)
	mu.Unlock()
	var searchObservation *ToolObservation
	for i := range gotObservations {
		if gotObservations[i].Kind == ToolObservationSearch {
			searchObservation = &gotObservations[i]
			break
		}
	}
	if searchObservation == nil ||
		strings.Join(searchObservation.ValidatedSelectNames, ",") != "browser_open" ||
		strings.Join(searchObservation.MatchNames, ",") != "browser_act,browser_open,browser_snapshot" ||
		strings.Join(searchObservation.NewMatchNames, ",") != "browser_act,browser_open,browser_snapshot" {
		t.Fatalf("search observation did not see expanded matches: %#v", searchObservation)
	}

	wantApprovals := []agentToolSearchApprovalCall{
		{name: ToolSearchReservedName, args: searchArgs},
		{name: "browser_open", args: openArgs},
	}
	if got := approvals.snapshot(); !reflect.DeepEqual(got, wantApprovals) {
		t.Fatalf("approval calls = %#v, want %#v", got, wantApprovals)
	}
}

func TestGroupedToolSearchSameBatchRemainsBypassUntilNextGeneration(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "read"}
	browserOpen := &agentToolSearchTestTool{name: "browser_open"}
	browserAct := &agentToolSearchTestTool{name: "browser_act"}
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{
			{ID: "search-same-batch", Function: schema.FunctionCall{
				Name: ToolSearchReservedName, Arguments: `{"query":"select:browser_open"}`,
			}},
			{ID: "act-same-batch", Function: schema.FunctionCall{
				Name: "browser_act", Arguments: `{"value":"must-be-bypass"}`,
			}},
		}),
		schema.AssistantMessage("done", nil),
	}}
	plan := ToolPlan{
		Direct: []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{
			groupedAgentToolSearchDescriptor(browserOpen, "browser.workflow"),
			groupedAgentToolSearchDescriptor(browserAct, "browser.workflow"),
		},
	}
	ag, err := NewAgentWithToolPlan(context.Background(), model, plan, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAgentWithToolPlan() error = %v", err)
	}

	var mu sync.Mutex
	var observations []ToolObservation
	ctx := WithToolObservationSink(context.Background(), func(observation ToolObservation) {
		mu.Lock()
		observations = append(observations, observation)
		mu.Unlock()
	})
	runObservedAgent(ctx, t, ag)

	assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{
		{"read", ToolSearchReservedName},
		{"browser_act", "browser_open", "read", ToolSearchReservedName},
	})
	mu.Lock()
	got := append([]ToolObservation(nil), observations...)
	mu.Unlock()
	var bypass *ToolObservation
	for i := range got {
		if got[i].Kind == ToolObservationBypass && got[i].ToolCallID == "act-same-batch" {
			bypass = &got[i]
			break
		}
	}
	if bypass == nil || bypass.ToolName != "browser_act" ||
		bypass.Reason != "not_visible_in_last_model_request" || bypass.ModelRequestSeq != 1 {
		t.Fatalf("same-batch browser_act bypass observation = %#v", bypass)
	}
}

func TestGroupedDisclosureDoesNotAuthorizeMutatingPeer(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "read"}
	browserOpen := &agentToolSearchTestTool{name: "browser_open"}
	browserAct := &agentToolSearchTestTool{name: "browser_act"}
	searchArgs := `{"query":"select:browser_open"}`
	actArgs := `{"value":"click-dangerous-control"}`
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		toolCallMessage("search-browser", ToolSearchReservedName, searchArgs),
		toolCallMessage("act-browser", "browser_act", actArgs),
		schema.AssistantMessage("stopped", nil),
	}}
	approvals := &agentToolSearchApprovalRecorder{denyName: "browser_act"}
	plan := ToolPlan{
		Direct: []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{
			groupedAgentToolSearchDescriptor(browserOpen, "browser.workflow"),
			groupedAgentToolSearchDescriptor(browserAct, "browser.workflow"),
		},
	}
	ag, err := NewAgentWithToolPlan(
		context.Background(), model, plan, "test", approvals.approve, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewAgentWithToolPlan() error = %v", err)
	}
	runAgentToolSearchTest(t, ag)

	assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{
		{"read", ToolSearchReservedName},
		{"browser_act", "browser_open", "read", ToolSearchReservedName},
		{"browser_act", "browser_open", "read", ToolSearchReservedName},
	})
	if got := browserAct.arguments(); len(got) != 0 {
		t.Fatalf("approval-denied grouped browser_act executed with args %v", got)
	}
	wantApprovals := []agentToolSearchApprovalCall{
		{name: ToolSearchReservedName, args: searchArgs},
		{name: "browser_act", args: actArgs},
	}
	if got := approvals.snapshot(); !reflect.DeepEqual(got, wantApprovals) {
		t.Fatalf("approval calls = %#v, want %#v", got, wantApprovals)
	}
}

func TestToolSearchDisclosureExcludesGatedGroupPeers(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "read"}
	browserOpen := &agentToolSearchTestTool{name: "browser_open"}
	browserSnapshot := &agentToolSearchTestTool{name: "browser_snapshot"}
	revokedAct := &agentToolSearchTestTool{name: "browser_act"}
	descriptors := []ToolDescriptor{
		agentToolSearchDescriptor(direct, ToolExposureDirect),
		groupedCapabilityDescriptor(browserOpen, "browser.workflow", "browser"),
		groupedCapabilityDescriptor(browserSnapshot, "browser.workflow", "browser"),
		groupedCapabilityDescriptor(revokedAct, "browser.workflow", "browser_interact"),
	}
	plan, err := NewToolPlanBuilder(descriptors).Build(context.Background(), ToolPlanContext{
		Capabilities: map[string]bool{"browser": true},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		toolCallMessage("search-browser", ToolSearchReservedName, `{"query":"select:browser_open"}`),
		schema.AssistantMessage("done", nil),
	}}
	ag, err := NewAgentWithToolPlan(context.Background(), model, plan, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAgentWithToolPlan() error = %v", err)
	}
	runAgentToolSearchTest(t, ag)

	assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{
		{"read", ToolSearchReservedName},
		{"browser_open", "browser_snapshot", "read", ToolSearchReservedName},
	})
	if containsAgentToolSearchName(model.visibleTools()[1], "browser_act") {
		t.Fatal("capability-revoked browser_act was disclosed through its former group")
	}
	if got := revokedAct.arguments(); len(got) != 0 {
		t.Fatalf("capability-revoked browser_act executed: %v", got)
	}
}

func testToolDisclosureGroups(names []string, group string) toolDisclosureGroups {
	descriptors := make([]ToolDescriptor, len(names))
	for i, name := range names {
		descriptors[i] = ToolDescriptor{
			Name: name, Exposure: ToolExposureDeferred, DisclosureGroup: group,
		}
	}
	return disclosureGroupsFromDescriptors(descriptors)
}

func groupedAgentToolSearchDescriptor(
	t *agentToolSearchTestTool,
	group string,
) ToolDescriptor {
	descriptor := agentToolSearchDescriptor(t, ToolExposureDeferred)
	descriptor.DisclosureGroup = group
	return descriptor
}

func groupedCapabilityDescriptor(
	t *agentToolSearchTestTool,
	group, capability string,
) ToolDescriptor {
	descriptor := groupedAgentToolSearchDescriptor(t, group)
	descriptor.RequiredCapabilities = []string{capability}
	return descriptor
}
