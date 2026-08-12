package tools

import (
	"errors"
	"fmt"
	"os"
)

// ---------------------------------------------------------------------------
// Fatal errors (#16) — unrecoverable infrastructure failures.
//
// A Fatal error marks an infrastructure failure that this run must not ask the
// model to retry. Examples include a removed container, exhausted SSH reconnect
// attempts, or an SSH command whose outcome became unknown after dispatch. A
// transient SSH loss is repaired inside the executor and is not Fatal when a
// safe operation can be replayed. Error-folding middleware
// (internal/agent/middleware.go approvalMiddleware and the subagent
// safeToolMiddleware in subagent.go) checks IsFatal BEFORE folding and
// propagates the error to abort the run instead of letting the model burn
// its iteration budget retrying.
//
// Deliberately NOT fatal: context cancellation (the runner already handles
// clean cancel paths), MCP transport failures (the model can fall back to
// built-in tools), argument parsing errors, missing files, and panics (their
// source is unknown; aborting the run would be an over-reaction).
// ---------------------------------------------------------------------------

// fatalError wraps an error to mark it unrecoverable. The error text is
// unchanged so log grepping and prefix matching keep working.
type fatalError struct{ err error }

func (e *fatalError) Error() string { return e.err.Error() }
func (e *fatalError) Unwrap() error { return e.err }

// Fatal marks err as a permanent, run-aborting failure. A nil err returns nil.
func Fatal(err error) error {
	if err == nil {
		return nil
	}
	return &fatalError{err: err}
}

// IsFatal reports whether err (or any error in its %w chain) was marked with
// Fatal.
func IsFatal(err error) bool {
	var fe *fatalError
	return errors.As(err, &fe)
}

// ---------------------------------------------------------------------------
// ToolError (#28) — model-facing tool failures with a stable Code and a
// curated next-step Hint.
//
// The hint is baked into Error() itself ("<original error>. Hint: <hint>") so
// every surface that stringifies the error benefits automatically: the
// middleware folding above, SubagentTask.Error, runner OnAgentDone display,
// and direct InvokableRun callers. The original error text always comes
// first, keeping logs grep-able and preserving any substring matching by
// upstream consumers. Code stays a struct field only (for telemetry/error
// aggregation); it is intentionally not rendered into the text.
// ---------------------------------------------------------------------------

// ToolError carries a stable machine-readable Code, a model-facing Hint, and
// the underlying error.
type ToolError struct {
	Code string // stable identifier, e.g. "file_not_found"
	Hint string // short imperative next step for the model
	Err  error  // underlying error; its text leads in Error()
}

func (e *ToolError) Error() string {
	msg := ""
	if e.Err != nil {
		msg = e.Err.Error()
	}
	if e.Hint == "" {
		return msg
	}
	return fmt.Sprintf("%v. Hint: %s", e.Err, e.Hint)
}

func (e *ToolError) Unwrap() error { return e.Err }

// toolErrf builds a ToolError whose underlying error is
// fmt.Errorf(format, args...), so %w-wrapped causes stay reachable through
// errors.Is/As.
func toolErrf(code, hint, format string, args ...any) *ToolError {
	return &ToolError{Code: code, Hint: hint, Err: fmt.Errorf(format, args...)}
}

// Curated hints for high-frequency tool errors. Model-facing, short
// imperative next steps.
const (
	hintInvalidJSON = "Arguments must be valid JSON matching the tool schema; " +
		"re-emit the tool call with corrected JSON (check for unescaped quotes or truncation)."
	hintFileNotFound = "Verify the path (it is resolved against the workspace root) " +
		"or locate the file with the grep tool before reading."
	hintWriteFailed      = "Ensure the parent directory exists and the location is writable."
	hintPermissionDenied = "Permission denied; choose a readable file or ask the user."
	hintReadFailed       = "Check that the path points to a regular readable file."
)

// missingParamHint returns the hint for a missing required parameter.
func missingParamHint(param string) string {
	return "Provide the " + param + " parameter and retry."
}

// readFailHint picks the hint for a failed stat/read based on the cause.
func readFailHint(err error) string {
	if errors.Is(err, os.ErrPermission) {
		return hintPermissionDenied
	}
	return hintReadFailed
}
