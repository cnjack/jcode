package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

type countingExecuteExecutor struct {
	Executor
	calls  int
	stdout string
	stderr string
	err    error
}

func (e *countingExecuteExecutor) Exec(
	context.Context,
	string,
	string,
	time.Duration,
) (string, string, error) {
	e.calls++
	stdout := e.stdout
	if stdout == "" && e.err == nil {
		stdout = "ok"
	}
	return stdout, e.stderr, e.err
}

func TestExecutePreservesFatalRemoteTransportError(t *testing.T) {
	env := NewEnv(t.TempDir(), runtime.GOOS+"/"+runtime.GOARCH)
	transportErr := &RemoteTransportError{
		Kind:      "ssh",
		Code:      "ssh_connection_failed",
		Phase:     RemoteTransportOutcomeUnknown,
		Retryable: true,
		Err:       errors.New("use of closed network connection"),
	}
	executor := &countingExecuteExecutor{
		Executor: env.Exec,
		stdout:   "possibly applied\n",
		stderr:   "connection lost\n",
		err:      Fatal(transportErr),
	}
	env.Exec = executor

	modelOutput, err := env.NewExecuteTool(nil).InvokableRun(
		context.Background(), `{"command":"touch state"}`,
	)
	if !IsFatal(err) {
		t.Fatalf("execute error = %v, want Fatal", err)
	}
	var got *RemoteTransportError
	if !errors.As(err, &got) || got != transportErr {
		t.Fatalf("execute error = %v, want original RemoteTransportError", err)
	}
	if !strings.Contains(modelOutput, "possibly applied") {
		t.Fatalf("model output lost diagnostic stdout: %q", modelOutput)
	}
}

func TestExecuteOrdinaryExitRemainsModelResult(t *testing.T) {
	env := NewEnv(t.TempDir(), runtime.GOOS+"/"+runtime.GOARCH)
	executor := &countingExecuteExecutor{
		Executor: env.Exec,
		stderr:   "ordinary failure\n",
		err:      fmt.Errorf("exit status 7"),
	}
	env.Exec = executor

	modelOutput, err := env.NewExecuteTool(nil).InvokableRun(
		context.Background(), `{"command":"false"}`,
	)
	if err != nil {
		t.Fatalf("ordinary command error propagated: %v", err)
	}
	if !strings.Contains(modelOutput, "ordinary failure") {
		t.Fatalf("model output lost ordinary stderr: %q", modelOutput)
	}
}

// execToolRun runs the execute tool against a real LocalExecutor.
func execToolRun(t *testing.T, args string) string {
	t.Helper()
	env := NewEnv(t.TempDir(), runtime.GOOS+"/"+runtime.GOARCH)
	et := env.NewExecuteTool(nil)
	out, err := et.InvokableRun(context.Background(), args)
	if err != nil {
		t.Fatalf("InvokableRun(%s): %v", args, err)
	}
	return out
}

// #28: Malformed JSON arguments carry a re-emit hint.
func TestExecute_InvalidArgs_Hint(t *testing.T) {
	env := NewEnv(t.TempDir(), runtime.GOOS+"/"+runtime.GOARCH)
	et := env.NewExecuteTool(nil)

	_, err := et.InvokableRun(context.Background(), `{"command":`)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "valid JSON") {
		t.Fatalf("expected JSON hint, got: %s", err.Error())
	}
}

// #28: Missing command parameter carries a provide-and-retry hint.
func TestExecute_MissingCommand_Hint(t *testing.T) {
	env := NewEnv(t.TempDir(), runtime.GOOS+"/"+runtime.GOARCH)
	et := env.NewExecuteTool(nil)

	_, err := et.InvokableRun(context.Background(), `{}`)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("original error text must be preserved, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "Provide the command parameter") {
		t.Fatalf("expected missing-param hint, got: %s", err.Error())
	}
}

func TestPlanExecuteRejectsBackgroundAndNonReadOnlyCommands(t *testing.T) {
	env := NewEnv(t.TempDir(), runtime.GOOS+"/"+runtime.GOARCH)
	executor := &countingExecuteExecutor{Executor: env.Exec}
	env.Exec = executor
	planExecute := env.NewPlanExecuteTool()
	if _, err := planExecute.InvokableRun(context.Background(), `{"command":"pwd"}`); err != nil {
		t.Fatalf("Plan execute rejected allowlisted command: %v", err)
	}
	if executor.calls != 1 {
		t.Fatalf("allowlisted command reached executor %d times, want 1", executor.calls)
	}

	for _, args := range []string{
		`{"command":"ls","background":true}`,
		`{"command":"touch changed"}`,
		`{"command":"git diff --output=/tmp/leak"}`,
		`{"command":"git diff --ext-diff"}`,
		`{"command":"ls; touch changed"}`,
	} {
		if _, err := planExecute.InvokableRun(context.Background(), args); err == nil {
			t.Errorf("Plan execute accepted %s", args)
		}
	}
	if executor.calls != 1 {
		t.Fatalf("rejected Plan commands reached executor; total calls=%d, want 1", executor.calls)
	}
}

func TestPlanExecuteSchemaOmitsBackground(t *testing.T) {
	env := NewEnv(t.TempDir(), runtime.GOOS+"/"+runtime.GOARCH)
	planInfo, err := env.NewPlanExecuteTool().Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	planSchema, err := planInfo.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if planSchema.Properties.Value("background") != nil {
		t.Fatal("Plan execute schema advertises background execution")
	}
	if planSchema.Properties.Value("command") == nil {
		t.Fatal("Plan execute schema is missing command")
	}

	normalInfo, err := env.NewExecuteTool(nil).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	normalSchema, err := normalInfo.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if normalSchema.Properties.Value("background") == nil {
		t.Fatal("normal execute schema unexpectedly lost background")
	}
}

func TestExecute_TruncatesLargeStdout(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep the spill file out of the real ~/.jcode

	res := execToolRun(t, `{"command":"seq 1 100000"}`)
	if len(res) >= 50000 {
		t.Fatalf("result length = %d, want < 50000 (below the eino reduction threshold)", len(res))
	}
	if !strings.Contains(res, "STDOUT:\n1\n2\n") {
		t.Fatalf("head of stdout missing: %.120q", res)
	}
	if !strings.Contains(res, "\n100000\n") {
		t.Fatalf("tail of stdout missing: ...%q", res[len(res)-200:])
	}
	if !strings.Contains(res, "output truncated") {
		t.Fatalf("truncation marker missing: ...%q", res[len(res)-200:])
	}
}

func TestExecute_StderrSurvivesHugeStdout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	res := execToolRun(t, `{"command":"seq 1 100000; echo BOOM_ERR >&2; exit 1"}`)
	if !strings.Contains(res, "BOOM_ERR") {
		t.Fatalf("stderr was squeezed out by the huge stdout: ...%q", res[len(res)-300:])
	}
	if !strings.Contains(res, "[Exit code: 1]") {
		t.Fatalf("exit-code footer missing: ...%q", res[len(res)-300:])
	}
}

func TestExecute_SmallOutputUntouched(t *testing.T) {
	res := execToolRun(t, `{"command":"echo hi"}`)
	if !strings.Contains(res, "hi") {
		t.Fatalf("output missing: %q", res)
	}
	if strings.Contains(res, "truncated") || strings.Contains(res, "Full output") {
		t.Fatalf("small output must not carry truncation markers: %q", res)
	}
}

func TestExecute_SpillFileWritten(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	res := execToolRun(t, `{"command":"seq 1 100000"}`)
	m := regexp.MustCompile(`\[Full output: (.+)\]`).FindStringSubmatch(res)
	if m == nil {
		t.Fatalf("no [Full output: <path>] marker in result: ...%q", res[len(res)-300:])
	}
	data, err := os.ReadFile(m[1])
	if err != nil {
		t.Fatalf("read spill file %q: %v", m[1], err)
	}
	full := string(data)
	// The spill must be the complete output, including the middle dropped inline.
	for _, want := range []string{"STDOUT:\n1\n", "\n50000\n", "\n100000\n"} {
		if !strings.Contains(full, want) {
			t.Fatalf("spill file missing %q — not the full output (len=%d)", want, len(full))
		}
	}
	if strings.Contains(full, "output truncated") {
		t.Fatalf("spill file itself must not be truncated")
	}
}
