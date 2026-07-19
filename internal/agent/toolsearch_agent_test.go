package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type agentToolSearchTestTool struct {
	name   string
	result string

	mu    sync.Mutex
	calls []string
}

func (t *agentToolSearchTestTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.name,
		Desc: "Test tool " + t.name,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"value": {Type: schema.String, Desc: "Test value", Required: true},
		}),
	}, nil
}

func (t *agentToolSearchTestTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, args)
	if t.result != "" {
		return t.result, nil
	}
	return `{"ok":true}`, nil
}

func (t *agentToolSearchTestTool) arguments() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.calls...)
}

type agentToolSearchScriptModel struct {
	mu               sync.Mutex
	responses        []*schema.Message
	visible          [][]string
	toolDescriptions []map[string]string
	toolResults      []agentToolSearchToolResult
	systemTexts      []string
}

type agentToolSearchToolResult struct {
	name    string
	callID  string
	content string
}

func (m *agentToolSearchScriptModel) Generate(
	_ context.Context,
	messages []*schema.Message,
	opts ...einomodel.Option,
) (*schema.Message, error) {
	options := einomodel.GetCommonOptions(nil, opts...)
	names := make([]string, 0, len(options.Tools))
	descriptions := make(map[string]string, len(options.Tools))
	for _, info := range options.Tools {
		if info != nil {
			names = append(names, info.Name)
			descriptions[info.Name] = info.Desc
		}
	}
	sort.Strings(names)
	var systemText strings.Builder
	var toolResults []agentToolSearchToolResult
	for _, message := range messages {
		if message != nil && message.Role == schema.System {
			if systemText.Len() > 0 {
				systemText.WriteString("\n")
			}
			systemText.WriteString(message.Content)
		}
		if message != nil && message.Role == schema.Tool {
			toolResults = append(toolResults, agentToolSearchToolResult{
				name: message.ToolName, callID: message.ToolCallID, content: message.Content,
			})
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	call := len(m.visible)
	m.visible = append(m.visible, names)
	m.toolDescriptions = append(m.toolDescriptions, descriptions)
	m.toolResults = append(m.toolResults, toolResults...)
	m.systemTexts = append(m.systemTexts, systemText.String())
	if call >= len(m.responses) {
		return nil, fmt.Errorf("unexpected model call %d", call+1)
	}
	return m.responses[call], nil
}

func (m *agentToolSearchScriptModel) Stream(
	context.Context,
	[]*schema.Message,
	...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream is not used by this test model")
}

func (m *agentToolSearchScriptModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func (m *agentToolSearchScriptModel) visibleTools() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]string, len(m.visible))
	for i := range m.visible {
		result[i] = append([]string(nil), m.visible[i]...)
	}
	return result
}

func (m *agentToolSearchScriptModel) firstToolDescription(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.toolDescriptions) == 0 {
		return ""
	}
	return m.toolDescriptions[0][name]
}

func (m *agentToolSearchScriptModel) firstSystemText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.systemTexts) == 0 {
		return ""
	}
	return m.systemTexts[0]
}

func (m *agentToolSearchScriptModel) systemTextAt(index int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index < 0 || index >= len(m.systemTexts) {
		return ""
	}
	return m.systemTexts[index]
}

func (m *agentToolSearchScriptModel) resultsFor(name string) []agentToolSearchToolResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []agentToolSearchToolResult
	for _, candidate := range m.toolResults {
		if candidate.name == name {
			result = append(result, candidate)
		}
	}
	return result
}

type agentToolSearchApprovalCall struct {
	name string
	args string
}

type agentToolSearchApprovalRecorder struct {
	mu       sync.Mutex
	calls    []agentToolSearchApprovalCall
	denyName string
}

func (r *agentToolSearchApprovalRecorder) approve(_ context.Context, name, args string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, agentToolSearchApprovalCall{name: name, args: args})
	return name != r.denyName, nil
}

func (r *agentToolSearchApprovalRecorder) snapshot() []agentToolSearchApprovalCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]agentToolSearchApprovalCall(nil), r.calls...)
}

func TestNewAgentLegacyDoesNotInjectToolSearch(t *testing.T) {
	legacy := &agentToolSearchTestTool{name: "legacy_tool"}
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		schema.AssistantMessage("done", nil),
	}}

	ag, err := NewAgent(context.Background(), model, []tool.BaseTool{legacy}, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	runAgentToolSearchTest(t, ag)

	assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{{"legacy_tool"}})
	if strings.Contains(model.firstSystemText(), deferredToolBatchInstruction) {
		t.Fatal("legacy NewAgent unexpectedly injected deferred-tool instructions")
	}
}

func TestToolSearchGuidanceIsConditionalAndOwnedByEinoSchema(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "direct_tool"}

	t.Run("deferred", func(t *testing.T) {
		model := &agentToolSearchScriptModel{responses: []*schema.Message{schema.AssistantMessage("done", nil)}}
		plan := ToolPlan{
			Direct: []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
			Deferred: []ToolDescriptor{agentToolSearchDescriptor(
				&agentToolSearchTestTool{name: "deferred_tool"}, ToolExposureDeferred,
			)},
		}
		ag, err := NewAgentWithToolPlan(context.Background(), model, plan, "base instruction", nil, nil, nil)
		if err != nil {
			t.Fatalf("NewAgentWithToolPlan() error = %v", err)
		}
		runAgentToolSearchTest(t, ag)

		if !strings.Contains(model.firstSystemText(), deferredToolBatchInstruction) {
			t.Fatalf("deferred plan did not inject batch sequencing rule: %q", model.firstSystemText())
		}
		for _, want := range []string{
			"may be deferred when its schema is not attached",
			"before substituting execute",
			"use select:<tool_name>",
			"separate tool-call batch",
			"Use a legitimate already-attached alternative when appropriate",
		} {
			if !strings.Contains(model.firstSystemText(), want) {
				t.Errorf("deferred routing guidance missing %q: %q", want, model.firstSystemText())
			}
		}
		desc := model.firstToolDescription(ToolSearchReservedName)
		for _, want := range []string{
			"Keyword search",
			"select:<tool_name>",
			"select:Read,Edit,Grep",
			"Required keyword",
			"immediately available",
			"Do NOT follow up",
		} {
			if !strings.Contains(desc, want) {
				t.Errorf("Eino tool_search schema missing %q: %q", want, desc)
			}
		}
	})

	t.Run("no deferred", func(t *testing.T) {
		model := &agentToolSearchScriptModel{responses: []*schema.Message{schema.AssistantMessage("done", nil)}}
		plan := ToolPlan{Direct: []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)}}
		ag, err := NewAgentWithToolPlan(context.Background(), model, plan, "base instruction", nil, nil, nil)
		if err != nil {
			t.Fatalf("NewAgentWithToolPlan() error = %v", err)
		}
		runAgentToolSearchTest(t, ag)
		if strings.Contains(model.firstSystemText(), deferredToolBatchInstruction) {
			t.Fatal("plan without deferred tools injected batch sequencing rule")
		}
		if desc := model.firstToolDescription(ToolSearchReservedName); desc != "" {
			t.Fatalf("plan without deferred tools exposed tool_search schema: %q", desc)
		}
	})
}

func TestNewAgentWithToolPlanActivatesDeferredTool(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "direct_tool"}
	const deferredResult = `{"endpoint":"deferred_alpha","preserved":true}`
	deferredA := &agentToolSearchTestTool{name: "deferred_alpha", result: deferredResult}
	deferredB := &agentToolSearchTestTool{name: "deferred_beta"}
	hidden := &agentToolSearchTestTool{name: "hidden_tool"}
	searchArgs := `{"query":"select:deferred_alpha","max_results":1}`
	deferredArgs := `{"value":"hello"}`
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		toolCallMessage("search-1", "tool_search", searchArgs),
		toolCallMessage("deferred-1", "deferred_alpha", deferredArgs),
		schema.AssistantMessage("done", nil),
	}}
	approvals := &agentToolSearchApprovalRecorder{}
	plan := ToolPlan{
		Direct:   []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{agentToolSearchDescriptor(deferredA, ToolExposureDeferred), agentToolSearchDescriptor(deferredB, ToolExposureDeferred)},
		Hidden:   []ToolDescriptor{agentToolSearchDescriptor(hidden, ToolExposureHidden)},
	}

	ag, err := NewAgentWithToolPlan(
		context.Background(), model, plan, "test", approvals.approve, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewAgentWithToolPlan() error = %v", err)
	}
	runAgentToolSearchTest(t, ag)

	assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{
		{"direct_tool", "tool_search"},
		{"deferred_alpha", "direct_tool", "tool_search"},
		{"deferred_alpha", "direct_tool", "tool_search"},
	})
	if got := deferredA.arguments(); !reflect.DeepEqual(got, []string{deferredArgs}) {
		t.Fatalf("deferred alpha arguments = %v, want %v", got, []string{deferredArgs})
	}
	if got := model.resultsFor("deferred_alpha"); !reflect.DeepEqual(got, []agentToolSearchToolResult{{
		name: "deferred_alpha", callID: "deferred-1", content: deferredResult,
	}}) {
		t.Fatalf("deferred alpha results reaching the model = %#v", got)
	}
	if got := deferredB.arguments(); len(got) != 0 {
		t.Fatalf("deferred beta calls = %v, want none", got)
	}
	if got := hidden.arguments(); len(got) != 0 {
		t.Fatalf("hidden tool calls = %v, want none", got)
	}

	approvalCalls := approvals.snapshot()
	wantApprovalCalls := []agentToolSearchApprovalCall{
		{name: "tool_search", args: searchArgs},
		{name: "deferred_alpha", args: deferredArgs},
	}
	if !reflect.DeepEqual(approvalCalls, wantApprovalCalls) {
		t.Fatalf("approval calls = %#v, want %#v", approvalCalls, wantApprovalCalls)
	}
}

func TestLoadedSkillRemindsBeforeDeferredSearch(t *testing.T) {
	loadSkill := &agentToolSearchTestTool{
		name:   loadSkillToolName,
		result: "<skill>For native UI use `computer_open`.</skill>",
	}
	computerOpen := &agentToolSearchTestTool{name: "computer_open"}
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		toolCallMessage("skill-1", loadSkillToolName, `{"value":"computer-use"}`),
		toolCallMessage("search-1", ToolSearchReservedName, `{"query":"select:computer_open"}`),
		toolCallMessage("open-1", "computer_open", `{"value":"com.apple.Notes"}`),
		schema.AssistantMessage("done", nil),
	}}
	plan := ToolPlan{
		Direct:   []ToolDescriptor{agentToolSearchDescriptor(loadSkill, ToolExposureDirect)},
		Deferred: []ToolDescriptor{agentToolSearchDescriptor(computerOpen, ToolExposureDeferred)},
	}
	ag, err := NewAgentWithToolPlan(context.Background(), model, plan, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAgentWithToolPlan() error = %v", err)
	}
	runAgentToolSearchTest(t, ag)

	assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{
		{loadSkillToolName, ToolSearchReservedName},
		{loadSkillToolName, ToolSearchReservedName},
		{"computer_open", loadSkillToolName, ToolSearchReservedName},
		{"computer_open", loadSkillToolName, ToolSearchReservedName},
	})
	if strings.Contains(model.systemTextAt(1), "one or more purpose-built tools whose schemas are still deferred") {
		t.Fatalf("skill routing note was promoted to system text: %q", model.systemTextAt(1))
	}
	results := model.resultsFor(loadSkillToolName)
	if len(results) == 0 {
		t.Fatal("model history omitted load_skill result")
	}
	for _, want := range []string{
		"<tool-routing-reminder>",
		"one or more purpose-built tools whose schemas are still deferred",
		"before substituting execute",
	} {
		if !strings.Contains(results[0].content, want) {
			t.Errorf("load_skill result missing %q: %q", want, results[0].content)
		}
	}
	if got := strings.Count(results[0].content, "<tool-routing-reminder>"); got != 1 {
		t.Fatalf("load_skill routing note count = %d, want 1: %q", got, results[0].content)
	}
	if got := loadSkill.arguments(); len(got) != 1 {
		t.Fatalf("load_skill calls = %v, want one", got)
	}
	if got := computerOpen.arguments(); len(got) != 1 {
		t.Fatalf("computer_open calls = %v, want one", got)
	}
}

func TestToolSearchActivationDoesNotAuthorizeDeferredTool(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "direct_tool"}
	deferred := &agentToolSearchTestTool{name: "deferred_write"}
	searchArgs := `{"query":"select:deferred_write"}`
	deferredArgs := `{"value":"mutate"}`
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		toolCallMessage("search-1", "tool_search", searchArgs),
		toolCallMessage("deferred-1", "deferred_write", deferredArgs),
		schema.AssistantMessage("stopped", nil),
	}}
	approvals := &agentToolSearchApprovalRecorder{denyName: "deferred_write"}
	plan := ToolPlan{
		Direct:   []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{agentToolSearchDescriptor(deferred, ToolExposureDeferred)},
	}

	ag, err := NewAgentWithToolPlan(
		context.Background(), model, plan, "test", approvals.approve, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewAgentWithToolPlan() error = %v", err)
	}
	runAgentToolSearchTest(t, ag)

	if got := deferred.arguments(); len(got) != 0 {
		t.Fatalf("denied deferred endpoint executed with args %v", got)
	}
	wantApprovals := []agentToolSearchApprovalCall{
		{name: "tool_search", args: searchArgs},
		{name: "deferred_write", args: deferredArgs},
	}
	if got := approvals.snapshot(); !reflect.DeepEqual(got, wantApprovals) {
		t.Fatalf("approval calls = %#v, want %#v", got, wantApprovals)
	}
}

func TestNewAgentWithToolPlanAccumulatesSelections(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "direct_tool"}
	deferredA := &agentToolSearchTestTool{name: "deferred_alpha"}
	deferredB := &agentToolSearchTestTool{name: "deferred_beta"}
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		toolCallMessage("search-a", "tool_search", `{"query":"select:deferred_alpha"}`),
		toolCallMessage("search-b", "tool_search", `{"query":"select:deferred_beta"}`),
		schema.AssistantMessage("done", nil),
	}}
	plan := ToolPlan{
		Direct: []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{
			agentToolSearchDescriptor(deferredA, ToolExposureDeferred),
			agentToolSearchDescriptor(deferredB, ToolExposureDeferred),
		},
	}

	ag, err := NewAgentWithToolPlan(context.Background(), model, plan, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAgentWithToolPlan() error = %v", err)
	}
	runAgentToolSearchTest(t, ag)

	assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{
		{"direct_tool", "tool_search"},
		{"deferred_alpha", "direct_tool", "tool_search"},
		{"deferred_alpha", "deferred_beta", "direct_tool", "tool_search"},
	})
}

func TestNewAgentWithToolPlanUnknownSearchDoesNotExposeDeferredTools(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "direct_tool"}
	deferred := &agentToolSearchTestTool{name: "deferred_alpha"}
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		toolCallMessage("search-miss", "tool_search", `{"query":"not-a-real-capability"}`),
		schema.AssistantMessage("done", nil),
	}}
	plan := ToolPlan{
		Direct:   []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{agentToolSearchDescriptor(deferred, ToolExposureDeferred)},
	}

	ag, err := NewAgentWithToolPlan(context.Background(), model, plan, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAgentWithToolPlan() error = %v", err)
	}
	runAgentToolSearchTest(t, ag)

	assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{
		{"direct_tool", "tool_search"},
		{"direct_tool", "tool_search"},
	})
}

func TestNewAgentWithToolPlanEmptyDeferredDoesNotInjectToolSearch(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "direct_tool"}
	hidden := &agentToolSearchTestTool{name: "hidden_tool"}
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		schema.AssistantMessage("done", nil),
	}}
	plan := ToolPlan{
		Direct: []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Hidden: []ToolDescriptor{agentToolSearchDescriptor(hidden, ToolExposureHidden)},
	}

	ag, err := NewAgentWithToolPlan(context.Background(), model, plan, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAgentWithToolPlan() error = %v", err)
	}
	runAgentToolSearchTest(t, ag)

	assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{{"direct_tool"}})
}

func TestNewAgentWithToolPlanDoesNotRegisterHiddenTools(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "direct_tool"}
	deferred := &agentToolSearchTestTool{name: "deferred_tool"}
	hidden := &agentToolSearchTestTool{name: "hidden_tool"}
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		schema.AssistantMessage("done", nil),
	}}
	capture := &agentToolSearchRuntimeCapture{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
	}
	plan := ToolPlan{
		Direct:   []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{agentToolSearchDescriptor(deferred, ToolExposureDeferred)},
		Hidden:   []ToolDescriptor{agentToolSearchDescriptor(hidden, ToolExposureHidden)},
	}

	ag, err := NewAgentWithToolPlan(
		context.Background(), model, plan, "test", nil, nil,
		[]adk.ChatModelAgentMiddleware{capture},
	)
	if err != nil {
		t.Fatalf("NewAgentWithToolPlan() error = %v", err)
	}
	runAgentToolSearchTest(t, ag)

	if got, want := capture.toolNames(), []string{"deferred_tool", "direct_tool", "tool_search"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime tools = %v, want %v", got, want)
	}
}

func TestNewAgentWithToolPlanRejectsInvalidManualPlans(t *testing.T) {
	validDirect := &agentToolSearchTestTool{name: "valid"}
	infoErr := errors.New("info unavailable")
	tests := []struct {
		name      string
		plan      ToolPlan
		wantError string
	}{
		{
			name: "nil tool",
			plan: ToolPlan{Direct: []ToolDescriptor{{
				Name: "nil_tool", Exposure: ToolExposureDirect,
			}}},
			wantError: "nil tool",
		},
		{
			name: "tool info error",
			plan: ToolPlan{Direct: []ToolDescriptor{{
				Tool: agentToolSearchInvalidInfoTool{err: infoErr}, Name: "broken", Exposure: ToolExposureDirect,
			}}},
			wantError: "info unavailable",
		},
		{
			name: "nil tool info",
			plan: ToolPlan{Direct: []ToolDescriptor{{
				Tool: agentToolSearchInvalidInfoTool{}, Name: "broken", Exposure: ToolExposureDirect,
			}}},
			wantError: "nil ToolInfo",
		},
		{
			name: "descriptor info mismatch",
			plan: ToolPlan{Direct: []ToolDescriptor{{
				Tool: validDirect, Name: "different", Exposure: ToolExposureDirect,
			}}},
			wantError: "does not match",
		},
		{
			name: "duplicate across partitions",
			plan: ToolPlan{
				Direct: []ToolDescriptor{agentToolSearchDescriptor(
					&agentToolSearchTestTool{name: "same"}, ToolExposureDirect,
				)},
				Deferred: []ToolDescriptor{agentToolSearchDescriptor(
					&agentToolSearchTestTool{name: "same"}, ToolExposureDeferred,
				)},
			},
			wantError: "duplicate tool name",
		},
		{
			name: "reserved name",
			plan: ToolPlan{Direct: []ToolDescriptor{agentToolSearchDescriptor(
				&agentToolSearchTestTool{name: "tool_search"}, ToolExposureDirect,
			)}},
			wantError: "reserved",
		},
		{
			name: "partition exposure mismatch",
			plan: ToolPlan{Direct: []ToolDescriptor{agentToolSearchDescriptor(
				validDirect, ToolExposureDeferred,
			)}},
			wantError: "is in direct but declares exposure",
		},
		{
			name: "direct model only fails closed",
			plan: ToolPlan{DirectModelOnly: []ToolDescriptor{agentToolSearchDescriptor(
				&agentToolSearchTestTool{name: "model_only"}, ToolExposureDirectModelOnly,
			)}},
			wantError: "direct_model_only tools are unsupported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &agentToolSearchScriptModel{}
			_, err := NewAgentWithToolPlan(context.Background(), model, tt.plan, "test", nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("NewAgentWithToolPlan() error = %v, want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestToolSearchRunsBeforeHistoryDestroyingHandler(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "direct_tool"}
	deferred := &agentToolSearchTestTool{name: "deferred_alpha"}
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		toolCallMessage("search-1", "tool_search", `{"query":"select:deferred_alpha"}`),
		toolCallMessage("deferred-1", "deferred_alpha", `{"value":"after-compaction"}`),
		schema.AssistantMessage("done", nil),
	}}
	plan := ToolPlan{
		Direct:   []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{agentToolSearchDescriptor(deferred, ToolExposureDeferred)},
	}
	destroyer := &agentToolSearchHistoryDestroyer{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
	}

	ag, err := NewAgentWithToolPlan(
		context.Background(), model, plan, "test", nil, nil,
		[]adk.ChatModelAgentMiddleware{destroyer},
	)
	if err != nil {
		t.Fatalf("NewAgentWithToolPlan() error = %v", err)
	}
	runAgentToolSearchTest(t, ag)

	visible := model.visibleTools()
	if len(visible) < 2 || !containsAgentToolSearchName(visible[1], "deferred_alpha") {
		t.Fatalf("second model call tools = %v, want deferred_alpha activated before history was destroyed", visible)
	}
	if destroyer.removedCount() == 0 {
		t.Fatal("history destroyer did not remove a tool_search result; ordering test did not exercise the hazard")
	}
}

type agentToolSearchInvalidInfoTool struct {
	info *schema.ToolInfo
	err  error
}

func (t agentToolSearchInvalidInfoTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, t.err
}

type agentToolSearchHistoryDestroyer struct {
	*adk.BaseChatModelAgentMiddleware

	mu      sync.Mutex
	removed int
}

type agentToolSearchRuntimeCapture struct {
	*adk.BaseChatModelAgentMiddleware

	mu    sync.Mutex
	names []string
}

func (m *agentToolSearchRuntimeCapture) BeforeAgent(
	ctx context.Context,
	runCtx *adk.ChatModelAgentContext,
) (context.Context, *adk.ChatModelAgentContext, error) {
	names := make([]string, 0, len(runCtx.Tools))
	for _, candidate := range runCtx.Tools {
		info, err := candidate.Info(ctx)
		if err != nil {
			return ctx, runCtx, err
		}
		names = append(names, info.Name)
	}
	sort.Strings(names)
	m.mu.Lock()
	m.names = names
	m.mu.Unlock()
	return ctx, runCtx, nil
}

func (m *agentToolSearchRuntimeCapture) toolNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.names...)
}

func (m *agentToolSearchHistoryDestroyer) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	_ *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	kept := make([]*schema.Message, 0, len(state.Messages))
	removed := 0
	for _, message := range state.Messages {
		if message != nil && message.Role == schema.Tool && message.ToolName == "tool_search" {
			removed++
			continue
		}
		kept = append(kept, message)
	}
	state.Messages = kept
	if removed > 0 {
		m.mu.Lock()
		m.removed += removed
		m.mu.Unlock()
	}
	return ctx, state, nil
}

func (m *agentToolSearchHistoryDestroyer) removedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.removed
}

func agentToolSearchDescriptor(t *agentToolSearchTestTool, exposure ToolExposure) ToolDescriptor {
	return ToolDescriptor{Tool: t, Name: t.name, Exposure: exposure}
}

func toolCallMessage(id, name, args string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID: id,
		Function: schema.FunctionCall{
			Name:      name,
			Arguments: args,
		},
	}})
}

func runAgentToolSearchTest(t *testing.T, ag *adk.ChatModelAgent) {
	t.Helper()
	iterator := ag.Run(context.Background(), &adk.AgentInput{
		Messages: []adk.Message{schema.UserMessage("run test")},
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

func assertAgentToolSearchVisible(t *testing.T, got, want [][]string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("visible tools = %v, want %v", got, want)
	}
}

func containsAgentToolSearchName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

var _ tool.InvokableTool = (*agentToolSearchTestTool)(nil)
var _ tool.BaseTool = agentToolSearchInvalidInfoTool{}
var _ einomodel.ToolCallingChatModel = (*agentToolSearchScriptModel)(nil)
