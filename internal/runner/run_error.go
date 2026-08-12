package runner

import (
	"errors"
	"fmt"
	"strings"

	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/tools"
)

// runDisplayError replaces infrastructure/framework prose with a sentence for
// the user while retaining the complete typed error chain for policy and
// diagnostics. It is deliberately separate from model.FriendlyError: a remote
// executor failure is not a failed request to the model.
type runDisplayError struct {
	err     error
	message string
}

func (e *runDisplayError) Error() string { return e.message }
func (e *runDisplayError) Unwrap() error { return e.err }

func wrapRunError(err error, provider, modelName string) error {
	if err == nil {
		return nil
	}
	var displayed *runDisplayError
	if errors.As(err, &displayed) {
		return err
	}
	var remoteErr *tools.RemoteTransportError
	if errors.As(err, &remoteErr) {
		return &runDisplayError{err: err, message: remoteTransportMessage(remoteErr)}
	}
	return internalmodel.WrapFriendly(err, provider, modelName)
}

// FormatRunError returns the already-classified run error text when runner.Run
// handled it, and applies the model formatter only to a raw error from a legacy
// caller. ACP uses this instead of unconditionally labelling every turn error
// as an API/model failure.
func FormatRunError(err error, provider, modelName string) string {
	if err == nil {
		return ""
	}
	var displayed *runDisplayError
	var modelDisplay *internalmodel.FriendlyError
	if errors.As(err, &displayed) || errors.As(err, &modelDisplay) {
		return err.Error()
	}
	var remoteErr *tools.RemoteTransportError
	if errors.As(err, &remoteErr) {
		return remoteTransportMessage(remoteErr)
	}
	return internalmodel.FriendlyAPIError(err, provider, modelName)
}

func remoteTransportMessage(err *tools.RemoteTransportError) string {
	kind := "Remote"
	if err != nil && strings.TrimSpace(err.Kind) != "" {
		kind = strings.ToUpper(strings.TrimSpace(err.Kind))
	}
	if err != nil && err.Phase == tools.RemoteTransportOutcomeUnknown {
		message := fmt.Sprintf(
			"%s connection was lost after the remote operation started. JCode did not replay the operation because it may already have completed.",
			kind,
		)
		if err.ReconnectErr != nil {
			return message + " The connection could not be restored; reconnect and inspect the remote workspace before continuing."
		}
		return message + " The connection was restored; inspect the remote workspace before continuing."
	}
	return fmt.Sprintf(
		"%s connection was lost before the remote operation started and could not be restored. The operation was not run; reconnect and try again.",
		kind,
	)
}
