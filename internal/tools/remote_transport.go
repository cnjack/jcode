package tools

import (
	"context"
	"fmt"
	"time"
)

// RemoteTransportPhase records how far a remote operation got before the
// transport failed. Callers may replay before-dispatch failures, but must treat
// outcome-unknown failures as potentially applied on the remote host.
type RemoteTransportPhase string

const (
	RemoteTransportBeforeDispatch RemoteTransportPhase = "before_dispatch"
	RemoteTransportOutcomeUnknown RemoteTransportPhase = "outcome_unknown"
)

// RemoteTransportError is a machine-readable remote connection failure. A
// failure after dispatch deliberately does not claim that the command failed:
// the remote process may have completed after the local SSH channel disappeared.
type RemoteTransportError struct {
	Kind         string
	Code         string
	Phase        RemoteTransportPhase
	Retryable    bool
	Err          error
	ReconnectErr error
}

func (e *RemoteTransportError) Error() string {
	if e == nil {
		return "remote transport error"
	}
	message := fmt.Sprintf("%s remote transport failed (%s)", e.Kind, e.Phase)
	if e.Phase == RemoteTransportOutcomeUnknown {
		message += "; remote command outcome is unknown and it was not replayed"
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	if e.ReconnectErr != nil {
		message += "; reconnect failed: " + e.ReconnectErr.Error()
	}
	return message
}

func (e *RemoteTransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

const (
	RemoteConnectionWaiting        = "waiting"
	RemoteConnectionReconnecting   = "reconnecting"
	RemoteConnectionReady          = "ready"
	RemoteConnectionFailed         = "failed"
	RemoteConnectionActionRequired = "action_required"
)

// RemoteConnectionStatus is emitted by a live remote executor while it repairs
// a transient connection. The owning Engine supplies task_id in its WebSocket
// envelope, keeping this transport package UI- and session-agnostic.
type RemoteConnectionStatus struct {
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	Attempt     int    `json:"attempt"`
	MaxAttempts int    `json:"max_attempts"`
	Host        string `json:"host,omitempty"`
	Error       string `json:"error,omitempty"`
	Code        string `json:"code,omitempty"`
	Retryable   bool   `json:"retryable,omitempty"`
	RetryInMS   int64  `json:"retry_in_ms,omitempty"`
}

type RemoteConnectionStatusHandler func(RemoteConnectionStatus)

// RemoteConnectionStatusSource is implemented by per-Engine remote leases.
// SetRemoteConnectionStatusHandler replaces the callback for only that lease.
type RemoteConnectionStatusSource interface {
	SetRemoteConnectionStatusHandler(RemoteConnectionStatusHandler)
}

// ReadOnlyExecutor lets callers explicitly opt a command into safe transport
// replay. Arbitrary Executor.Exec calls are never inferred to be read-only.
type ReadOnlyExecutor interface {
	ExecReadOnly(ctx context.Context, command, workDir string, timeout time.Duration) (stdout, stderr string, err error)
}

// ExecReadOnly uses replay-aware execution when the executor supports it and
// preserves the ordinary Executor contract for local/Docker implementations.
func ExecReadOnly(
	ctx context.Context,
	executor Executor,
	command, workDir string,
	timeout time.Duration,
) (stdout, stderr string, err error) {
	if readOnly, ok := executor.(ReadOnlyExecutor); ok {
		return readOnly.ExecReadOnly(ctx, command, workDir, timeout)
	}
	return executor.Exec(ctx, command, workDir, timeout)
}
