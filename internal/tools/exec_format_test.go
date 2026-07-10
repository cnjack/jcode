package tools

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildExecResult_SuccessStdoutOnly(t *testing.T) {
	res := BuildExecResult("hello\n", "", nil, 1200*time.Millisecond, "echo hello")
	if !strings.Contains(res.ModelOutput, "STDOUT:\nhello") {
		t.Fatalf("model missing stdout: %q", res.ModelOutput)
	}
	if !strings.Contains(res.ModelOutput, "[Completed in 1.2s]") {
		t.Fatalf("model missing duration footer: %q", res.ModelOutput)
	}
	if strings.Contains(res.DisplayBody, "STDOUT:") || strings.Contains(res.DisplayBody, "[Completed") {
		t.Fatalf("display body must be clean: %q", res.DisplayBody)
	}
	if res.DisplayBody != "hello" {
		t.Fatalf("display body = %q, want hello", res.DisplayBody)
	}
	if strings.TrimSpace(res.Streams.Stdout) != "hello" {
		t.Fatalf("streams.stdout = %q", res.Streams.Stdout)
	}
	if res.Streams.Stderr != "" {
		t.Fatalf("stderr should be empty, got %q", res.Streams.Stderr)
	}
	if res.Meta.ExitCode != 0 {
		t.Fatalf("exit = %d", res.Meta.ExitCode)
	}
	if res.Meta.DurationMs < 1000 || res.Meta.DurationMs > 1300 {
		t.Fatalf("duration_ms = %d", res.Meta.DurationMs)
	}
	if res.Presentation.Kind != "list" {
		t.Fatalf("echo should present as list (safe), got kind=%q", res.Presentation.Kind)
	}
	if !res.Presentation.Collapsible {
		t.Fatal("echo (safe) should be collapsible")
	}
}

func TestBuildExecResult_FailedWithStderr(t *testing.T) {
	runErr := runExit(t, 1)
	res := BuildExecResult("out\n", "boom\n", runErr, 500*time.Millisecond, "false")
	if !strings.Contains(res.ModelOutput, "STDERR:\nboom") {
		t.Fatalf("model missing stderr: %q", res.ModelOutput)
	}
	if !strings.Contains(res.ModelOutput, "[Exit code: 1]") {
		t.Fatalf("model missing exit: %q", res.ModelOutput)
	}
	if res.Meta.ExitCode != 1 {
		t.Fatalf("exit = %d want 1", res.Meta.ExitCode)
	}
	if strings.TrimSpace(res.Streams.Stderr) != "boom" {
		t.Fatalf("streams.stderr = %q", res.Streams.Stderr)
	}
	if strings.Contains(res.DisplayBody, "STDERR:") || strings.Contains(res.DisplayBody, "[Exit") {
		t.Fatalf("display dirty: %q", res.DisplayBody)
	}
	if !strings.Contains(res.DisplayBody, "out") || !strings.Contains(res.DisplayBody, "boom") {
		t.Fatalf("display should join streams: %q", res.DisplayBody)
	}
	if res.Presentation.Collapsible {
		t.Fatalf("mutating false should not be collapsible")
	}
}

func TestBuildExecResult_ReadCommandCollapsible(t *testing.T) {
	res := BuildExecResult("file contents", "", nil, time.Millisecond, "cat foo.txt")
	if !res.Presentation.Collapsible {
		t.Fatal("cat should be collapsible")
	}
	if res.Presentation.Kind != "read" {
		t.Fatalf("kind = %q want read", res.Presentation.Kind)
	}
}

func TestParseExecModelOutput_RoundTrip(t *testing.T) {
	runErr := runExit(t, 2)
	built := BuildExecResult("line1\nline2\n", "errline\n", runErr, 2500*time.Millisecond, "make test")
	parsed, ok := ParseExecModelOutput(built.ModelOutput)
	if !ok {
		t.Fatal("parse should succeed")
	}
	if !strings.Contains(parsed.Streams.Stdout, "line1") || !strings.Contains(parsed.Streams.Stdout, "line2") {
		t.Fatalf("parsed stdout = %q", parsed.Streams.Stdout)
	}
	if !strings.Contains(parsed.Streams.Stderr, "errline") {
		t.Fatalf("parsed stderr = %q", parsed.Streams.Stderr)
	}
	if parsed.Meta.ExitCode != 2 {
		t.Fatalf("exit = %d", parsed.Meta.ExitCode)
	}
	if parsed.Meta.DurationMs < 2000 || parsed.Meta.DurationMs > 3000 {
		t.Fatalf("duration = %d", parsed.Meta.DurationMs)
	}
	if strings.Contains(parsed.DisplayBody, "STDOUT:") {
		t.Fatalf("display should be clean: %q", parsed.DisplayBody)
	}
}

func TestParseExecModelOutput_RejectsNonExec(t *testing.T) {
	if _, ok := ParseExecModelOutput("just some tool text"); ok {
		t.Fatal("should reject plain text")
	}
}

func TestPresentationKindForTool(t *testing.T) {
	k, c := PresentationKindForTool("read", `{"file_path":"a.go"}`)
	if k != "read" || !c {
		t.Fatalf("read: kind=%s collapsible=%v", k, c)
	}
	k, c = PresentationKindForTool("edit", `{"file_path":"a.go"}`)
	if k != "edit" || c {
		t.Fatalf("edit: kind=%s collapsible=%v", k, c)
	}
	k, c = PresentationKindForTool("execute", `{"command":"ls -la"}`)
	if k != "list" || !c {
		t.Fatalf("ls: kind=%s collapsible=%v", k, c)
	}
	k, c = PresentationKindForTool("execute", `{"command":"rm -rf /tmp/x"}`)
	if k != "shell" || c {
		t.Fatalf("rm: kind=%s collapsible=%v", k, c)
	}
}

func TestBuildExecResult_ExitCodeFromExecError(t *testing.T) {
	res := BuildExecResult("", "nope", runExit(t, 7), time.Millisecond, "false")
	if res.Meta.ExitCode != 7 {
		t.Fatalf("exit = %d want 7 (model=%q)", res.Meta.ExitCode, res.ModelOutput)
	}
}

func runExit(t *testing.T, code int) error {
	t.Helper()
	env := NewEnv(t.TempDir(), "test")
	_, _, err := env.Exec.Exec(context.Background(),
		"sh -c 'exit "+strconv.Itoa(code)+"'", env.pwd, 5*time.Second)
	if err == nil {
		t.Fatal("expected non-nil exit error")
	}
	return err
}
