package handler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/tools"
)

func TestExtractToolDisplayInfo_CollapsibleKinds(t *testing.T) {
	read := extractToolDisplayInfo("read", `{"file_path":"internal/foo.go"}`)
	if !read.Collapsible || read.Kind != "read" {
		t.Fatalf("read: collapsible=%v kind=%q", read.Collapsible, read.Kind)
	}
	edit := extractToolDisplayInfo("edit", `{"file_path":"a.go"}`)
	if edit.Collapsible || edit.Kind != "edit" {
		t.Fatalf("edit: collapsible=%v kind=%q", edit.Collapsible, edit.Kind)
	}
	ls := extractToolDisplayInfo("execute", `{"command":"ls -la"}`)
	if !ls.Collapsible || ls.Kind != "list" {
		t.Fatalf("ls: collapsible=%v kind=%q", ls.Collapsible, ls.Kind)
	}
}

func TestOnToolResult_ExecuteEmitsStreamsAndMeta(t *testing.T) {
	h := NewWebHandler()

	failed := tools.BuildExecResult("out\n", "err\n", errLike(t), 800*time.Millisecond, "false")
	h.OnToolResult("execute", failed.ModelOutput, "call_1", nil)

	select {
	case ev := <-h.Events():
		if ev.Event != "tool_result" {
			t.Fatalf("event = %q", ev.Event)
		}
		data, ok := ev.Data.(WebToolResultData)
		if !ok {
			t.Fatalf("data type %T", ev.Data)
		}
		if data.Output != failed.ModelOutput {
			t.Fatal("legacy output must be preserved for model/history consumers")
		}
		if data.Streams == nil || !strings.Contains(data.Streams.Stderr, "err") {
			t.Fatalf("streams = %+v", data.Streams)
		}
		if data.Streams.Stdout == "" || !strings.Contains(data.Streams.Stdout, "out") {
			t.Fatalf("streams.stdout = %q", data.Streams.Stdout)
		}
		if data.Meta == nil || data.Meta.ExitCode != 3 {
			t.Fatalf("meta = %+v", data.Meta)
		}
		if strings.Contains(data.DisplayOutput, "STDERR:") || strings.Contains(data.DisplayOutput, "[Exit") {
			t.Fatalf("display_output dirty: %q", data.DisplayOutput)
		}
		if data.Presentation == nil {
			t.Fatal("presentation missing")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for tool_result event")
	}
}

func errLike(t *testing.T) error {
	t.Helper()
	env := tools.NewEnv(t.TempDir(), "test")
	_, _, err := env.Exec.Exec(context.Background(), "sh -c 'exit 3'", env.Pwd(), 5*time.Second)
	if err == nil {
		t.Fatal("expected exit error")
	}
	return err
}
