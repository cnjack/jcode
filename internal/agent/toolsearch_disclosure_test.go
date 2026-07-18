package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/hooks"
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

func TestRewriteToolSearchExactNameListIsStrictAndPreservesOtherArguments(t *testing.T) {
	deferred := map[string]bool{
		"computer_open":       true,
		"computer_snapshot":   true,
		"computer_screenshot": true,
		"computer_act":        true,
		"computer_apps":       true,
		"computer_read":       true,
		"mcp__fixture__one":   true,
		"mcp__fixture__two":   true,
		"mcp__fixture__tri":   true,
		"mcp__fixture__four":  true,
	}
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{
			name:  "two canonical names and max results",
			input: ` { "query" : "computer_open, computer_snapshot", "max_results" : 2 } `,
			want:  ` { "query" : "select:computer_open,computer_snapshot", "max_results" : 2 } `,
			ok:    true,
		},
		{
			name:  "five names retain order and null max results",
			input: `{"max_results":null,"query":"computer_read,computer_apps,computer_act,computer_snapshot,computer_open"}`,
			want:  `{"max_results":null,"query":"select:computer_read,computer_apps,computer_act,computer_snapshot,computer_open"}`,
			ok:    true,
		},
		{
			name:  "six-name Computer family is accepted",
			input: `{"query":"computer_open,computer_snapshot,computer_screenshot,computer_act,computer_read,computer_apps"}`,
			want:  `{"query":"select:computer_open,computer_snapshot,computer_screenshot,computer_act,computer_read,computer_apps"}`,
			ok:    true,
		},
		{
			name:  "eight exact names are the hard compatibility ceiling",
			input: `{"query":"computer_open,computer_snapshot,computer_act,computer_apps,computer_read,mcp__fixture__one,mcp__fixture__two,mcp__fixture__tri"}`,
			want:  `{"query":"select:computer_open,computer_snapshot,computer_act,computer_apps,computer_read,mcp__fixture__one,mcp__fixture__two,mcp__fixture__tri"}`,
			ok:    true,
		},
		{
			name:  "MCP names work without server expansion",
			input: `{"query":"mcp__fixture__one,computer_open"}`,
			want:  `{"query":"select:mcp__fixture__one,computer_open"}`,
			ok:    true,
		},
		{
			name:  "escaped comma is replaced at byte-accurate JSON offsets",
			input: `{"query":"computer_open\u002ccomputer_snapshot"}`,
			want:  `{"query":"select:computer_open,computer_snapshot"}`,
			ok:    true,
		},
		{
			name:  "zero max results remains byte-for-byte",
			input: `{"query":"computer_open,computer_snapshot","max_results":0}`,
			want:  `{"query":"select:computer_open,computer_snapshot","max_results":0}`,
			ok:    true,
		},
		{name: "existing select", input: `{"query":"select:computer_open,computer_snapshot"}`},
		{name: "one name", input: `{"query":"computer_open"}`},
		{name: "nine names", input: `{"query":"computer_open,computer_snapshot,computer_act,computer_apps,computer_read,mcp__fixture__one,mcp__fixture__two,mcp__fixture__tri,mcp__fixture__four"}`},
		{name: "empty item", input: `{"query":"computer_open,,computer_snapshot"}`},
		{name: "duplicate", input: `{"query":"computer_open, computer_open"}`},
		{name: "unknown name", input: `{"query":"computer_open,not_a_tool"}`},
		{name: "ordinary semantic query", input: `{"query":"open browser, take snapshot"}`},
		{name: "Chinese comma", input: `{"query":"computer_open，computer_snapshot"}`},
		{name: "unknown JSON field fails closed", input: `{"query":"computer_open,computer_snapshot","future":true}`},
		{name: "duplicate query field fails closed", input: `{"query":"computer_open,computer_snapshot","query":"computer_act,computer_apps"}`},
		{name: "escaped duplicate query field fails closed", input: `{"qu\u0065ry":"computer_open,computer_snapshot","query":"computer_act,computer_apps"}`},
		{name: "duplicate max results field fails closed", input: `{"query":"computer_open,computer_snapshot","max_results":2,"max_results":3}`},
		{name: "invalid max results fails closed", input: `{"query":"computer_open,computer_snapshot","max_results":"2"}`},
		{name: "floating max results fails closed", input: `{"query":"computer_open,computer_snapshot","max_results":2.5}`},
		{name: "missing query fails closed", input: `{"max_results":2}`},
		{name: "null query fails closed", input: `{"query":null}`},
		{name: "non-object", input: `[]`},
		{name: "malformed", input: `{"query":"computer_open,computer_snapshot"`},
		{name: "trailing JSON", input: `{"query":"computer_open,computer_snapshot"}{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := rewriteToolSearchExactNameList(tt.input, deferred)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("rewriteToolSearchExactNameList() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestToolSearchExactNameListWrapperChangesOnlyRealEndpointArguments(t *testing.T) {
	middleware := newToolSearchExactListMiddleware(map[string]bool{
		"computer_open": true, "computer_snapshot": true,
	}).(*toolSearchExactListMiddleware)
	original := ` { "query" : "computer_open, computer_snapshot", "max_results" : 3 } `
	wantEndpointArgs := ` { "query" : "select:computer_open,computer_snapshot", "max_results" : 3 } `
	var endpointArgs string
	wrapped, err := middleware.WrapInvokableToolCall(
		context.Background(),
		func(_ context.Context, arguments string, _ ...tool.Option) (string, error) {
			endpointArgs = arguments
			return `{"matches":["computer_open","computer_snapshot"]}`, nil
		},
		&adk.ToolContext{Name: ToolSearchReservedName},
	)
	if err != nil {
		t.Fatalf("WrapInvokableToolCall() error = %v", err)
	}
	result, err := wrapped(context.Background(), original)
	if err != nil {
		t.Fatalf("wrapped endpoint error = %v", err)
	}
	if endpointArgs != wantEndpointArgs {
		t.Fatalf("real endpoint arguments = %q, want %q", endpointArgs, wantEndpointArgs)
	}
	if result != `{"matches":["computer_open","computer_snapshot"]}` {
		t.Fatalf("real endpoint result = %q", result)
	}

	unknownFieldArgs := `{"query":"computer_open,computer_snapshot","future":true}`
	if _, err = wrapped(context.Background(), unknownFieldArgs); err != nil {
		t.Fatalf("unknown-field endpoint error = %v", err)
	}
	if endpointArgs != unknownFieldArgs {
		t.Fatalf("unknown-field arguments = %q, want unchanged %q", endpointArgs, unknownFieldArgs)
	}

	nonSearchArgs := ""
	nonSearch, err := middleware.WrapInvokableToolCall(
		context.Background(),
		func(_ context.Context, arguments string, _ ...tool.Option) (string, error) {
			nonSearchArgs = arguments
			return "ok", nil
		},
		&adk.ToolContext{Name: "computer_open"},
	)
	if err != nil {
		t.Fatalf("WrapInvokableToolCall(non-search) error = %v", err)
	}
	if _, err = nonSearch(context.Background(), original); err != nil {
		t.Fatalf("non-search endpoint error = %v", err)
	}
	if nonSearchArgs != original {
		t.Fatalf("non-search arguments = %q, want original %q", nonSearchArgs, original)
	}
}

func TestToolSearchExactNameListFeedsHistoryWhileObservationSeesOriginal(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "read"}
	computerNames := []string{
		"computer_open", "computer_snapshot", "computer_act", "computer_apps", "computer_read",
	}
	deferred := make([]ToolDescriptor, 0, len(computerNames))
	for _, name := range computerNames {
		deferred = append(deferred, agentToolSearchDescriptor(
			&agentToolSearchTestTool{name: name}, ToolExposureDeferred,
		))
	}
	searchArgs := `{"query":"computer_open, computer_snapshot, computer_act, computer_apps, computer_read","max_results":2}`
	openArgs := `{"value":"TextEdit"}`
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		toolCallMessage("search-computer", ToolSearchReservedName, searchArgs),
		toolCallMessage("open-computer", "computer_open", openArgs),
		schema.AssistantMessage("done", nil),
	}}
	approvals := &agentToolSearchApprovalRecorder{}
	plan := ToolPlan{
		Direct:   []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: deferred,
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
	hookDir := t.TempDir()
	dispatcher := hooks.NewDispatcher(hooks.Config{Hooks: map[string][]hooks.HookGroup{
		string(hooks.PreToolUse): {
			{
				Matcher: ToolSearchReservedName,
				Hooks: []hooks.HookSpec{
					{Type: "command", Command: "cat > toolsearch-pre-payload.json"},
				},
			},
		},
	}}, hooks.Options{CWD: hookDir, SessionID: "exact-list-order-test"})
	ctx = hooks.WithDispatcher(ctx, dispatcher)
	runObservedAgent(ctx, t, ag)

	wantVisible := append([]string(nil), computerNames...)
	wantVisible = append(wantVisible, "read", ToolSearchReservedName)
	sort.Strings(wantVisible)
	assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{
		{"read", ToolSearchReservedName}, wantVisible, wantVisible,
	})
	searchResults := model.resultsFor(ToolSearchReservedName)
	wantResult := `{"matches":["computer_open","computer_snapshot","computer_act","computer_apps","computer_read"]}`
	if len(searchResults) == 0 || searchResults[0].content != wantResult {
		t.Fatalf("tool_search history results = %#v, want %s", searchResults, wantResult)
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
	originalQuery := "computer_open, computer_snapshot, computer_act, computer_apps, computer_read"
	if searchObservation == nil || searchObservation.QueryMode != "keyword" ||
		searchObservation.QueryBytes != len(originalQuery) || searchObservation.MaxResults != 2 ||
		len(searchObservation.ValidatedSelectNames) != 0 ||
		strings.Join(searchObservation.MatchNames, ",") != "computer_act,computer_apps,computer_open,computer_read,computer_snapshot" {
		t.Fatalf("search observation did not retain original query metadata: %#v", searchObservation)
	}
	hookPayloadBytes, err := os.ReadFile(filepath.Join(hookDir, "toolsearch-pre-payload.json"))
	if err != nil {
		t.Fatalf("read real PreToolUse hook payload: %v", err)
	}
	var hookPayload hooks.Payload
	if err = json.Unmarshal(hookPayloadBytes, &hookPayload); err != nil {
		t.Fatalf("decode real PreToolUse hook payload: %v", err)
	}
	if hookPayload.HookEventName != string(hooks.PreToolUse) ||
		hookPayload.ToolName != ToolSearchReservedName ||
		string(hookPayload.ToolInput) != searchArgs {
		t.Fatalf("real PreToolUse hook did not receive original model arguments: %#v", hookPayload)
	}

	wantApprovals := []agentToolSearchApprovalCall{
		{name: ToolSearchReservedName, args: `{"query":"select:computer_open,computer_snapshot,computer_act,computer_apps,computer_read","max_results":2}`},
		{name: "computer_open", args: openArgs},
	}
	if got := approvals.snapshot(); !reflect.DeepEqual(got, wantApprovals) {
		t.Fatalf("approval calls = %#v, want repaired read-only search arguments %#v", got, wantApprovals)
	}
}

func TestToolSearchExactNameListThenDisclosureGroupLoadsEffectivePeers(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "read"}
	browserOpen := &agentToolSearchTestTool{name: "browser_open"}
	browserSnapshot := &agentToolSearchTestTool{name: "browser_snapshot"}
	browserAct := &agentToolSearchTestTool{name: "browser_act"}
	browserRead := &agentToolSearchTestTool{name: "browser_read"}
	searchArgs := `{"query":"browser_open, browser_snapshot","max_results":1}`
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		toolCallMessage("search-browser-list", ToolSearchReservedName, searchArgs),
		schema.AssistantMessage("whole group visible", nil),
	}}
	plan := ToolPlan{
		Direct: []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{
			groupedAgentToolSearchDescriptor(browserOpen, "browser.workflow"),
			groupedAgentToolSearchDescriptor(browserSnapshot, "browser.workflow"),
			groupedAgentToolSearchDescriptor(browserAct, "browser.workflow"),
			groupedAgentToolSearchDescriptor(browserRead, "browser.workflow"),
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
		{"browser_act", "browser_open", "browser_read", "browser_snapshot", "read", ToolSearchReservedName},
	})
	searchResults := model.resultsFor(ToolSearchReservedName)
	wantResult := `{"matches":["browser_open","browser_snapshot","browser_act","browser_read"]}`
	if len(searchResults) == 0 || searchResults[0].content != wantResult {
		t.Fatalf("expanded exact-list history results = %#v, want %s", searchResults, wantResult)
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
	originalQuery := "browser_open, browser_snapshot"
	if searchObservation == nil || searchObservation.QueryMode != "keyword" ||
		searchObservation.QueryBytes != len(originalQuery) || searchObservation.MaxResults != 1 ||
		len(searchObservation.ValidatedSelectNames) != 0 ||
		strings.Join(searchObservation.MatchNames, ",") != "browser_act,browser_open,browser_read,browser_snapshot" ||
		strings.Join(searchObservation.NewMatchNames, ",") != "browser_act,browser_open,browser_read,browser_snapshot" {
		t.Fatalf("grouped exact-list observation = %#v", searchObservation)
	}
}

func TestToolSearchExactNameListSameBatchObservationRemainsBypass(t *testing.T) {
	alpha := &agentToolSearchTestTool{name: "computer_open"}
	beta := &agentToolSearchTestTool{name: "computer_snapshot"}
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{
			{ID: "search-same-batch", Function: schema.FunctionCall{
				Name: ToolSearchReservedName, Arguments: `{"query":"computer_open,computer_snapshot"}`,
			}},
			{ID: "target-same-batch", Function: schema.FunctionCall{
				Name: "computer_snapshot", Arguments: `{"value":"same-batch-existing-behavior"}`,
			}},
		}),
		schema.AssistantMessage("done", nil),
	}}
	plan := ToolPlan{Deferred: []ToolDescriptor{
		agentToolSearchDescriptor(alpha, ToolExposureDeferred),
		agentToolSearchDescriptor(beta, ToolExposureDeferred),
	}}
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
		{ToolSearchReservedName},
		{"computer_open", "computer_snapshot", ToolSearchReservedName},
	})
	mu.Lock()
	got := append([]ToolObservation(nil), observations...)
	mu.Unlock()
	for _, observation := range got {
		if observation.Kind == ToolObservationBypass && observation.ToolCallID == "target-same-batch" &&
			observation.ToolName == "computer_snapshot" && observation.Reason == "not_visible_in_last_model_request" {
			return
		}
	}
	t.Fatalf("same-batch bypass observation missing: %#v", got)
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
