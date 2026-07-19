package agent

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// TestToolSearchLifecycleNewGenerationStartsWithoutImplicitActivation locks the
// generation boundary. Selection is reconstructed from the messages supplied
// to a run; it is not mutable state that leaks from an old agent instance into
// a rebuilt one (mode/capability/MCP reload all rebuild an agent).
func TestToolSearchLifecycleNewGenerationStartsWithoutImplicitActivation(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "read"}
	browserAct := &agentToolSearchTestTool{name: "browser_act"}
	browserSnapshot := &agentToolSearchTestTool{name: "browser_snapshot"}
	plan := ToolPlan{
		Direct: []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{
			groupedAgentToolSearchDescriptor(browserAct, "browser.workflow"),
			groupedAgentToolSearchDescriptor(browserSnapshot, "browser.workflow"),
		},
	}

	firstModel := &agentToolSearchScriptModel{responses: []*schema.Message{
		toolCallMessage("search-browser", ToolSearchReservedName, `{"query":"select:browser_act"}`),
		schema.AssistantMessage("selected", nil),
	}}
	first, err := NewAgentWithToolPlan(context.Background(), firstModel, plan, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("first NewAgentWithToolPlan() error = %v", err)
	}
	if err := runToolSearchLifecycle(first, []adk.Message{schema.UserMessage("select browser")}); err != nil {
		t.Fatalf("first generation run error = %v", err)
	}
	assertAgentToolSearchVisible(t, firstModel.visibleTools(), [][]string{
		{"read", ToolSearchReservedName},
		{"browser_act", "browser_snapshot", "read", ToolSearchReservedName},
	})

	secondModel := &agentToolSearchScriptModel{responses: []*schema.Message{
		schema.AssistantMessage("fresh generation", nil),
	}}
	second, err := NewAgentWithToolPlan(context.Background(), secondModel, plan, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("second NewAgentWithToolPlan() error = %v", err)
	}
	if err := runToolSearchLifecycle(second, []adk.Message{schema.UserMessage("new generation")}); err != nil {
		t.Fatalf("second generation run error = %v", err)
	}
	assertAgentToolSearchVisible(t, secondModel.visibleTools(), [][]string{
		{"read", ToolSearchReservedName},
	})
}

// TestToolSearchLifecycleResumeRestoresRecordedSelection documents the durable
// part of disclosure: an un-compacted transcript contains a successful search
// result, so a freshly built agent can deterministically reconstruct the same
// model-visible schema. There is no process-local activation set to serialize.
func TestToolSearchLifecycleResumeRestoresRecordedSelection(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "read"}
	deferred := &agentToolSearchTestTool{name: "computer_read"}
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		schema.AssistantMessage("resumed", nil),
	}}
	plan := ToolPlan{
		Direct:   []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{agentToolSearchDescriptor(deferred, ToolExposureDeferred)},
	}
	ag, err := NewAgentWithToolPlan(context.Background(), model, plan, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAgentWithToolPlan() error = %v", err)
	}

	history := recordedToolSearchHistory("computer_read")
	if err := runToolSearchLifecycle(ag, history); err != nil {
		t.Fatalf("resumed run error = %v", err)
	}
	assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{
		{"computer_read", "read", ToolSearchReservedName},
	})
}

func TestToolSearchLifecycleResumePreservesExpandedAndLegacyGroupHistory(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "read"}
	plan := ToolPlan{
		Direct: []ToolDescriptor{agentToolSearchDescriptor(direct, ToolExposureDirect)},
		Deferred: []ToolDescriptor{
			groupedAgentToolSearchDescriptor(
				&agentToolSearchTestTool{name: "browser_open"}, "browser.workflow",
			),
			groupedAgentToolSearchDescriptor(
				&agentToolSearchTestTool{name: "browser_snapshot"}, "browser.workflow",
			),
			groupedAgentToolSearchDescriptor(
				&agentToolSearchTestTool{name: "browser_act"}, "browser.workflow",
			),
			groupedAgentToolSearchDescriptor(
				&agentToolSearchTestTool{name: "browser_read"}, "browser.workflow",
			),
		},
	}

	tests := []struct {
		name    string
		matches string
		visible []string
	}{
		{
			name:    "persisted expanded result restores all valid peers",
			matches: `{"matches":["browser_open","browser_act","browser_read","browser_snapshot"]}`,
			visible: []string{
				"browser_act", "browser_open", "browser_read", "browser_snapshot",
				"read", ToolSearchReservedName,
			},
		},
		{
			name:    "legacy unexpanded result restores only original match",
			matches: `{"matches":["browser_open"]}`,
			visible: []string{"browser_open", "read", ToolSearchReservedName},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &agentToolSearchScriptModel{responses: []*schema.Message{
				schema.AssistantMessage("resumed", nil),
			}}
			ag, err := NewAgentWithToolPlan(
				context.Background(), model, plan, "test", nil, nil, nil,
			)
			if err != nil {
				t.Fatalf("NewAgentWithToolPlan() error = %v", err)
			}
			if err := runToolSearchLifecycle(
				ag, recordedToolSearchHistoryWithResult("browser_open", tt.matches),
			); err != nil {
				t.Fatalf("resumed run error = %v", err)
			}
			assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{tt.visible})
		})
	}
}

// TestToolSearchLifecycleCompactedHistoryRequiresFreshSelection is the safe
// compaction contract. A natural-language summary is not trusted to activate
// executable endpoints. If the exact tool_search result was compacted away, a
// new run starts with the deferred schema hidden and can disclose it again via
// a fresh search. Within the run, middleware ordering preserves the selection
// even if a later compaction rewrite removes the old result (covered by
// TestToolSearchRunsBeforeHistoryDestroyingHandler).
func TestToolSearchLifecycleCompactedHistoryRequiresFreshSelection(t *testing.T) {
	direct := &agentToolSearchTestTool{name: "read"}
	deferred := &agentToolSearchTestTool{name: "computer_read"}
	model := &agentToolSearchScriptModel{responses: []*schema.Message{
		toolCallMessage("search-again", ToolSearchReservedName, `{"query":"select:computer_read"}`),
		toolCallMessage("read-after-search", "computer_read", `{"value":"safe"}`),
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

	compacted := []adk.Message{
		schema.SystemMessage("[Context Summary] computer_read had been selected earlier"),
		schema.UserMessage("continue after compaction"),
	}
	if err := runToolSearchLifecycle(ag, compacted); err != nil {
		t.Fatalf("compacted run error = %v", err)
	}
	assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{
		{"read", ToolSearchReservedName},
		{"computer_read", "read", ToolSearchReservedName},
		{"computer_read", "read", ToolSearchReservedName},
	})
	if got := deferred.arguments(); !reflect.DeepEqual(got, []string{`{"value":"safe"}`}) {
		t.Fatalf("computer_read arguments = %v, want one post-search call", got)
	}
}

// TestToolSearchLifecycleRevokedEndpointsCannotBeCalledByGuessing covers the
// execution boundary, not just schema hiding. A stale transcript may name a
// formerly selected tool, and the model may guess that name, but transport,
// mode, and capability gates put it in Hidden. Hidden endpoints never enter
// runCtx.Tools and therefore cannot execute.
func TestToolSearchLifecycleRevokedEndpointsCannotBeCalledByGuessing(t *testing.T) {
	tests := []struct {
		name       string
		targetName string
		descriptor ToolDescriptor
		planCtx    ToolPlanContext
	}{
		{
			name:       "browser disabled",
			targetName: "browser_act",
			descriptor: ToolDescriptor{
				Name: "browser_act", Exposure: ToolExposureDeferred,
				RequiredCapabilities: []string{"browser"},
			},
			planCtx: ToolPlanContext{
				Transport: ToolTransportWeb, Mode: ToolModeNormal,
				Capabilities: map[string]bool{"browser": false},
			},
		},
		{
			name:       "computer disabled",
			targetName: "computer_act",
			descriptor: ToolDescriptor{
				Name: "computer_act", Exposure: ToolExposureDeferred,
				RequiredCapabilities: []string{"computer"},
			},
			planCtx: ToolPlanContext{
				Transport: ToolTransportACP, Mode: ToolModeNormal,
				Capabilities: map[string]bool{"computer": false},
			},
		},
		{
			name:       "plan mode",
			targetName: "computer_read",
			descriptor: ToolDescriptor{
				Name: "computer_read", Exposure: ToolExposureDeferred,
				Modes: []string{ToolModeNormal},
			},
			planCtx: ToolPlanContext{Transport: ToolTransportTUI, Mode: ToolModePlan},
		},
		{
			name:       "transport boundary",
			targetName: "browser_open",
			descriptor: ToolDescriptor{
				Name: "browser_open", Exposure: ToolExposureDeferred,
				Transports: []string{ToolTransportTUI, ToolTransportWeb},
			},
			planCtx: ToolPlanContext{Transport: ToolTransportACP, Mode: ToolModeNormal},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			direct := &agentToolSearchTestTool{name: "read"}
			revoked := &agentToolSearchTestTool{name: tt.targetName}
			authorizedDeferred := &agentToolSearchTestTool{name: "goal_get"}
			tt.descriptor.Tool = revoked
			descriptors := []ToolDescriptor{
				agentToolSearchDescriptor(direct, ToolExposureDirect),
				tt.descriptor,
				agentToolSearchDescriptor(authorizedDeferred, ToolExposureDeferred),
			}
			plan, err := NewToolPlanBuilder(descriptors).Build(context.Background(), tt.planCtx)
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			capture := &agentToolSearchRuntimeCapture{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}
			model := &agentToolSearchScriptModel{responses: []*schema.Message{
				toolCallMessage("guessed-revoked", tt.targetName, `{"value":"must-not-run"}`),
			}}
			ag, err := NewAgentWithToolPlan(
				context.Background(), model, plan, "test", nil, nil,
				[]adk.ChatModelAgentMiddleware{capture},
			)
			if err != nil {
				t.Fatalf("NewAgentWithToolPlan() error = %v", err)
			}

			err = runToolSearchLifecycle(ag, recordedToolSearchHistory(tt.targetName))
			if err == nil {
				t.Fatal("guessed revoked tool unexpectedly completed without an execution error")
			}
			if !strings.Contains(err.Error(), tt.targetName) {
				t.Fatalf("execution error %q does not identify guessed tool %q", err, tt.targetName)
			}
			if got := revoked.arguments(); len(got) != 0 {
				t.Fatalf("revoked endpoint executed with args %v", got)
			}
			if containsAgentToolSearchName(capture.toolNames(), tt.targetName) {
				t.Fatalf("runtime registry contains revoked endpoint %q: %v", tt.targetName, capture.toolNames())
			}
			if visible := model.visibleTools(); len(visible) != 1 ||
				containsAgentToolSearchName(visible[0], tt.targetName) {
				t.Fatalf("first model-visible tools contain revoked endpoint %q: %v", tt.targetName, visible)
			}
		})
	}
}

// TestToolSearchDisabledUsesCurrentStaticCatalog distinguishes a disclosure
// policy switch from a permission/capability revocation. Disabled ToolSearch is
// intentionally the compatibility eager/static path: current tools are direct,
// while tools omitted from the rebuilt catalog remain absent and uncallable.
func TestToolSearchDisabledUsesCurrentStaticCatalog(t *testing.T) {
	t.Run("current capability remains eager", func(t *testing.T) {
		current := &agentToolSearchTestTool{name: "browser_open"}
		model := &agentToolSearchScriptModel{responses: []*schema.Message{
			schema.AssistantMessage("static", nil),
		}}
		ag, err := NewAgent(context.Background(), model, []tool.BaseTool{current}, "test", nil, nil, nil)
		if err != nil {
			t.Fatalf("NewAgent() error = %v", err)
		}
		if err := runToolSearchLifecycle(ag, []adk.Message{schema.UserMessage("static fallback")}); err != nil {
			t.Fatalf("run error = %v", err)
		}
		assertAgentToolSearchVisible(t, model.visibleTools(), [][]string{{"browser_open"}})
	})

	t.Run("removed capability is absent", func(t *testing.T) {
		revoked := &agentToolSearchTestTool{name: "browser_open"}
		direct := &agentToolSearchTestTool{name: "read"}
		capture := &agentToolSearchRuntimeCapture{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}
		model := &agentToolSearchScriptModel{responses: []*schema.Message{
			toolCallMessage("guess-static-revoked", "browser_open", `{"value":"must-not-run"}`),
		}}
		ag, err := NewAgent(
			context.Background(), model, []tool.BaseTool{direct}, "test", nil, nil,
			[]adk.ChatModelAgentMiddleware{capture},
		)
		if err != nil {
			t.Fatalf("NewAgent() error = %v", err)
		}
		err = runToolSearchLifecycle(ag, recordedToolSearchHistory("browser_open"))
		if err == nil {
			t.Fatal("guessed endpoint removed from static catalog unexpectedly completed")
		}
		if got := revoked.arguments(); len(got) != 0 {
			t.Fatalf("revoked endpoint executed with args %v", got)
		}
		if got := capture.toolNames(); !reflect.DeepEqual(got, []string{"read"}) {
			t.Fatalf("static runtime registry = %v, want [read]", got)
		}
	})
}

func recordedToolSearchHistory(selected string) []adk.Message {
	return recordedToolSearchHistoryWithResult(
		selected, `{"matches":["`+selected+`"]}`,
	)
}

func recordedToolSearchHistoryWithResult(selected, result string) []adk.Message {
	const callID = "recorded-search"
	return []adk.Message{
		schema.UserMessage("load a tool"),
		toolCallMessage(callID, ToolSearchReservedName, `{"query":"select:`+selected+`"}`),
		schema.ToolMessage(
			result,
			callID,
			schema.WithToolName(ToolSearchReservedName),
		),
		schema.UserMessage("continue"),
	}
}

func runToolSearchLifecycle(ag *adk.ChatModelAgent, messages []adk.Message) error {
	iterator := ag.Run(context.Background(), &adk.AgentInput{Messages: messages})
	for {
		event, ok := iterator.Next()
		if !ok {
			return nil
		}
		if event.Err != nil {
			return event.Err
		}
		if event.Output == nil || event.Output.MessageOutput == nil ||
			!event.Output.MessageOutput.IsStreaming {
			continue
		}
		stream := event.Output.MessageOutput.MessageStream
		for {
			_, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}
	}
}
