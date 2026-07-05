package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// defaultTimeout is applied to a hook that does not set its own. Generous
// compared to Claude Code's 30s because a Stop-gate hook may run a test suite.
const defaultTimeout = 60 * time.Second

// asyncHardCap bounds fire-and-forget (async) hooks regardless of their
// configured timeout, so a slow or hung async hook cannot leak a goroutine for
// the life of the (detached) process.
const asyncHardCap = 30 * time.Second

// runOutcome is one hook command's contribution to the folded Decision.
type runOutcome struct {
	permission        Permission
	block             bool
	reason            string
	updatedInput      json.RawMessage
	modifiedResult    *string
	additionalContext string
	systemMessage     string
}

// runHook executes a single hook command, feeding it the JSON payload over stdin
// and interpreting its exit code + stdout per the Claude Code protocol.
//
// Fail-safe rules (never let a broken hook wedge the agent):
//   - timeout            → treated as allow/no-op (logged).
//   - parent ctx cancel  → aborted, no-op (the whole operation is unwinding).
//   - failed to spawn    → no-op error surfaced as a system message.
//   - unexpected code    → non-blocking; stderr shown, no decision effect.
func runHook(ctx context.Context, spec HookSpec, input []byte, cwd string, env []string, event Event, logf func(string, ...any)) runOutcome {
	timeout := defaultTimeout
	if spec.Timeout > 0 {
		timeout = time.Duration(spec.Timeout) * time.Second
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, "sh", "-c", spec.Command)
	cmd.Dir = cwd
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// On timeout/cancel, CommandContext SIGKILLs `sh` but a grandchild (e.g. a
	// `sleep` it spawned) can keep the stdout pipe open, which makes cmd.Wait
	// block until that grandchild exits — defeating the timeout. WaitDelay bounds
	// that wait and force-closes the pipes so runHook returns promptly.
	cmd.WaitDelay = 500 * time.Millisecond

	err := cmd.Run()

	// Parent cancellation (user stop): abort silently — the operation is ending.
	if ctx.Err() != nil {
		return runOutcome{}
	}
	// Our own timeout: fail-safe to allow, do not block the agent.
	if tctx.Err() == context.DeadlineExceeded {
		if logf != nil {
			logf("hooks: %s hook timed out after %s (treated as allow): %s", event, timeout, spec.Command)
		}
		return runOutcome{}
	}

	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			// Could not start the process at all.
			if logf != nil {
				logf("hooks: %s hook failed to start: %v", event, err)
			}
			return runOutcome{systemMessage: "hook failed to run: " + err.Error()}
		}
	}

	return parseOutcome(event, exitCode, stdout.Bytes(), strings.TrimSpace(stderr.String()), logf)
}

// parseOutcome maps a hook's exit code + stdout/stderr into a runOutcome.
func parseOutcome(event Event, exitCode int, stdout []byte, stderr string, logf func(string, ...any)) runOutcome {
	var out runOutcome

	switch exitCode {
	case 0:
		// Success: structured stdout (if any) provides fine-grained control.
	case 2:
		// Block, but only for events that can actually be blocked.
		if event.Blockable() {
			if event == PreToolUse {
				out.permission = PermDeny
			} else {
				out.block = true
			}
			out.reason = stderr
		} else if logf != nil {
			logf("hooks: %s hook exited 2 but event is non-blockable (ignored)", event)
		}
		return out
	default:
		// Non-blocking error: surface stderr, do not affect the decision.
		if stderr != "" {
			out.systemMessage = stderr
		}
		if logf != nil {
			logf("hooks: %s hook exited %d (non-blocking): %s", event, exitCode, stderr)
		}
		return out
	}

	// exit 0 → parse structured stdout if present.
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return out
	}
	var env hookOutput
	if err := json.Unmarshal(trimmed, &env); err != nil {
		if logf != nil {
			logf("hooks: %s hook stdout is not valid JSON (ignored): %v", event, err)
		}
		return out
	}

	if env.SystemMessage != "" {
		out.systemMessage = env.SystemMessage
	}

	// Envelope-level block controls (Stop / UserPromptSubmit).
	if event.Blockable() && event != PreToolUse {
		if env.Continue != nil && !*env.Continue {
			out.block = true
			out.reason = env.Reason
		}
		if env.Decision == "block" {
			out.block = true
			if out.reason == "" {
				out.reason = env.Reason
			}
		}
	}

	if hso := env.HookSpecificOutput; hso != nil {
		if event == PreToolUse && hso.PermissionDecision != "" {
			out.permission = Permission(hso.PermissionDecision)
			out.reason = hso.PermissionDecisionReason
		}
		if len(hso.UpdatedInput) > 0 {
			out.updatedInput = hso.UpdatedInput
		}
		if hso.ModifiedResult != nil {
			out.modifiedResult = hso.ModifiedResult
		}
		if hso.AdditionalContext != "" {
			out.additionalContext = hso.AdditionalContext
		}
	}
	return out
}

// hookEnv builds the environment for a hook process: the parent env plus the
// JCODE_* convenience variables so scripts can avoid parsing stdin.
func hookEnv(base []string, p Payload) []string {
	if base == nil {
		base = os.Environ()
	}
	return append(base,
		"JCODE_SESSION_ID="+p.SessionID,
		"JCODE_TOOL_NAME="+p.ToolName,
		"JCODE_CWD="+p.CWD,
		"JCODE_TRANSCRIPT_PATH="+p.TranscriptPath,
		"JCODE_HOOK_EVENT="+p.HookEventName,
	)
}
