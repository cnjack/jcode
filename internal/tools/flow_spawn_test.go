package tools

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

func TestFlowToolMatrixUsesPlanExecuteOnlyForExplore(t *testing.T) {
	env := NewEnv(t.TempDir(), "darwin/arm64")
	tests := []struct {
		agentType       string
		wantNames       []string
		wantPlanExecute bool
	}{
		{
			agentType:       AgentTypeExplore,
			wantNames:       []string{"read", "grep", "execute"},
			wantPlanExecute: true,
		},
		{
			agentType: AgentTypeGeneral,
			wantNames: []string{"read", "grep", "execute", "edit", "write", "todowrite", "todoread"},
		},
		{
			agentType: AgentTypeCoordinator,
			wantNames: []string{"read", "grep", "execute", "edit", "write", "todowrite", "todoread"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.agentType, func(t *testing.T) {
			gotTools := flowTools(env, tt.agentType)
			if gotNames := testToolNames(t, gotTools); !reflect.DeepEqual(gotNames, tt.wantNames) {
				t.Fatalf("tool names = %v, want %v", gotNames, tt.wantNames)
			}
			execute := findTestExecuteTool(t, gotTools)
			if execute.planOnly != tt.wantPlanExecute {
				t.Fatalf("execute.planOnly = %v, want %v", execute.planOnly, tt.wantPlanExecute)
			}
		})
	}
}

func TestFlowAgentHandlersFoldNonFatalToolErrors(t *testing.T) {
	handlers := flowAgentHandlers()
	if len(handlers) != 1 {
		t.Fatalf("flow handlers = %d, want one safe middleware", len(handlers))
	}
	endpoint := func(context.Context, string, ...tool.Option) (string, error) {
		return "partial", errors.New("flow tool failed")
	}
	wrapper, err := handlers[0].WrapInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "read"})
	if err != nil {
		t.Fatalf("WrapInvokableToolCall() error = %v", err)
	}
	result, err := wrapper(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("flow safe middleware returned error = %v", err)
	}
	const want = "partial\n\nTool execution failed: flow tool failed"
	if result != want {
		t.Fatalf("folded result = %q, want %q", result, want)
	}
}
