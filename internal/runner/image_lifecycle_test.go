package runner

import (
	"testing"

	"github.com/cnjack/jcode/internal/handler"
)

func TestImageLifecycleBindsHostOperationAfterQueuedToolCall(t *testing.T) {
	call := handler.ToolCallEvent{Name: "generate_image", ToolCallID: "model-call"}
	decorateToolCallEvent(&call)
	if call.OperationID != "" || call.Surface != handler.ToolSurfaceStandalone ||
		call.Phase != handler.ToolPhaseQueued {
		t.Fatalf("queued call = %#v", call)
	}

	result := handler.ToolResultEvent{
		Name: "generate_image", ToolCallID: "model-call",
		Output: `{"message":"failed","operation_id":"host-operation","outcome":"failed","error_code":"authentication_failed","provider":"provider-old","model":"model-old"}`,
	}
	decorateToolResultEvent(&result)
	if result.OperationID != "host-operation" || result.Provider != "provider-old" ||
		result.Model != "model-old" || result.ErrorCode != "authentication_failed" {
		t.Fatalf("terminal result = %#v", result)
	}
}
