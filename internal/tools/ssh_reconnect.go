package tools

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	appconfig "github.com/cnjack/jcode/internal/config"
	"golang.org/x/crypto/ssh"
)

func (t *sshTransport) isOpen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.closed
}

func (t *sshTransport) retain() (uint64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.refs <= 0 {
		return 0, false
	}
	t.refs++
	t.nextLeaseID++
	return t.nextLeaseID, true
}

func (t *sshTransport) setObserver(leaseID uint64, handler RemoteConnectionStatusHandler) {
	if leaseID == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	if handler == nil {
		delete(t.observers, leaseID)
		return
	}
	if t.observers == nil {
		t.observers = make(map[uint64]RemoteConnectionStatusHandler)
	}
	t.observers[leaseID] = handler
}

func (t *sshTransport) emit(status RemoteConnectionStatus) {
	t.mu.Lock()
	handlers := make([]RemoteConnectionStatusHandler, 0, len(t.observers))
	for _, handler := range t.observers {
		if handler != nil {
			handlers = append(handlers, handler)
		}
	}
	t.mu.Unlock()
	for _, handler := range handlers {
		handler(status)
	}
}

func (t *sshTransport) release(leaseID uint64) error {
	t.mu.Lock()
	delete(t.observers, leaseID)
	if t.refs > 0 {
		t.refs--
	}
	last := t.refs == 0
	err := t.closeErr
	t.mu.Unlock()
	if last {
		return t.shutdown()
	}
	return err
}

// shutdown is the terminal transport close. Transient connection loss uses
// invalidateClient and keeps the lease set alive for a generation swap.
func (t *sshTransport) shutdown() error {
	t.mu.Lock()
	if t.closed {
		err := t.closeErr
		t.mu.Unlock()
		return err
	}
	t.closed = true
	client := t.client
	t.client = nil
	if t.lifetimeCancel != nil {
		t.lifetimeCancel()
	}
	close(t.keepaliveStop)
	t.mu.Unlock()

	var err error
	if client != nil {
		err = client.Close()
	}
	t.mu.Lock()
	t.closeErr = err
	t.mu.Unlock()
	return err
}

func (t *sshTransport) clientSnapshot() (*ssh.Client, uint64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, 0, fmt.Errorf("SSH transport %s@%s is closed", t.user, t.host)
	}
	if t.client == nil {
		return nil, t.clientGeneration, fmt.Errorf("SSH transport %s@%s is disconnected", t.user, t.host)
	}
	return t.client, t.clientGeneration, nil
}

func (t *sshTransport) connectedClient(
	ctx context.Context,
	cause error,
) (*ssh.Client, uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if client, generation, err := t.clientSnapshot(); err == nil {
		return client, generation, nil
	}
	if err := t.ensureConnected(ctx, cause); err != nil {
		return nil, 0, err
	}
	return t.clientSnapshot()
}

func (t *sshTransport) invalidateClient(client *ssh.Client, generation uint64) {
	if client == nil {
		return
	}
	t.mu.Lock()
	if t.client != client || t.clientGeneration != generation {
		t.mu.Unlock()
		return
	}
	t.client = nil
	t.mu.Unlock()
	_ = client.Close()
}

// ensureConnected performs a per-transport singleflight redial. Every lease
// waits on the same reconnectDone channel and observes the same client generation.
// The redial owns a transport-lifetime context rather than the first caller's
// context; a caller only cancels its own wait. If no waiters remain, the shared
// redial is cancelled so Stop does not leave background retries running.
func (t *sshTransport) ensureConnected(ctx context.Context, cause error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			return fmt.Errorf("SSH transport %s@%s is closed", t.user, t.host)
		}
		if t.client != nil {
			t.mu.Unlock()
			return nil
		}
		if !t.reconnecting {
			t.startReconnectLocked(cause)
		}
		done := t.reconnectDone
		t.reconnectWaiters++
		t.mu.Unlock()

		select {
		case <-ctx.Done():
			t.finishReconnectWait(done, true)
			return ctx.Err()
		case <-done:
			t.finishReconnectWait(done, false)
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		t.mu.Lock()
		err := t.reconnectErr
		connected := t.client != nil
		closed := t.closed
		t.mu.Unlock()
		if connected {
			return nil
		}
		if closed {
			return fmt.Errorf("SSH transport %s@%s is closed", t.user, t.host)
		}
		// A prior caller may have cancelled the shared attempt when it became
		// the last waiter. A still-live waiter elects itself into a new attempt.
		if errors.Is(err, context.Canceled) {
			continue
		}
		if err == nil {
			err = fmt.Errorf("SSH reconnect ended without a connection")
		}
		return err
	}
}

func (t *sshTransport) startReconnectLocked(cause error) {
	base := t.lifetimeCtx
	if base == nil {
		base = context.Background()
	}
	reconnectCtx, cancel := context.WithTimeout(base, sshReconnectTotalTimeout)
	t.reconnecting = true
	t.reconnectDone = make(chan struct{})
	t.reconnectErr = nil
	t.reconnectCause = cause
	t.reconnectCancel = cancel
	t.reconnectWaiters = 0
	done := t.reconnectDone
	go t.reconnect(reconnectCtx, cause, done, cancel)
}

func (t *sshTransport) finishReconnectWait(done <-chan struct{}, cancelled bool) {
	t.mu.Lock()
	if t.reconnectDone != done {
		t.mu.Unlock()
		return
	}
	if t.reconnectWaiters > 0 {
		t.reconnectWaiters--
	}
	if cancelled && t.reconnecting && t.reconnectWaiters == 0 && t.reconnectCancel != nil {
		t.reconnectCancel()
	}
	t.mu.Unlock()
}

func (t *sshTransport) reconnect(
	ctx context.Context,
	cause error,
	done chan struct{},
	cancel context.CancelFunc,
) {
	defer cancel()
	err := t.reconnectAttempts(ctx, cause)

	t.mu.Lock()
	// Only the goroutine owning this done channel may publish its result.
	if t.reconnectDone == done {
		t.reconnectErr = err
		t.reconnectCause = nil
		t.reconnectCancel = nil
		t.reconnecting = false
		close(done)
	}
	t.mu.Unlock()
}

func (t *sshTransport) reconnectAttempts(ctx context.Context, cause error) error {
	if t.dial == nil {
		return fmt.Errorf("SSH reconnect is unavailable")
	}
	lastErr := cause
	for attempt := 1; attempt <= sshReconnectMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt > 1 {
			delay := t.reconnectBackoff(attempt - 1)
			t.emit(RemoteConnectionStatus{
				Kind:        "ssh",
				Status:      RemoteConnectionWaiting,
				Attempt:     attempt,
				MaxAttempts: sshReconnectMaxAttempts,
				Host:        t.host,
				Error:       errorString(lastErr),
				Code:        "ssh_connection_failed",
				Retryable:   true,
				RetryInMS:   delay.Milliseconds(),
			})
			if err := waitContext(ctx, delay); err != nil {
				return err
			}
		}

		t.emit(RemoteConnectionStatus{
			Kind:        "ssh",
			Status:      RemoteConnectionReconnecting,
			Attempt:     attempt,
			MaxAttempts: sshReconnectMaxAttempts,
			Host:        t.host,
			Error:       errorString(lastErr),
			Code:        "ssh_connection_failed",
			Retryable:   true,
		})
		client, err := t.dial(ctx)
		if err == nil {
			if ctx.Err() != nil {
				_ = client.Close()
				return ctx.Err()
			}
			if installErr := t.installClient(client); installErr != nil {
				_ = client.Close()
				return installErr
			}
			t.emit(RemoteConnectionStatus{
				Kind:        "ssh",
				Status:      RemoteConnectionReady,
				Attempt:     attempt,
				MaxAttempts: sshReconnectMaxAttempts,
				Host:        t.host,
			})
			appconfig.Logger().Printf("[ssh] reconnected %s@%s on attempt %d", t.user, t.host, attempt)
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		code, retryable := classifySSHReconnectError(err)
		if !retryable {
			t.emit(RemoteConnectionStatus{
				Kind:        "ssh",
				Status:      RemoteConnectionActionRequired,
				Attempt:     attempt,
				MaxAttempts: sshReconnectMaxAttempts,
				Host:        t.host,
				Error:       err.Error(),
				Code:        code,
			})
			return err
		}
	}

	t.emit(RemoteConnectionStatus{
		Kind:        "ssh",
		Status:      RemoteConnectionFailed,
		Attempt:     sshReconnectMaxAttempts,
		MaxAttempts: sshReconnectMaxAttempts,
		Host:        t.host,
		Error:       errorString(lastErr),
		Code:        "ssh_connection_failed",
		Retryable:   true,
	})
	return lastErr
}

func (t *sshTransport) installClient(client *ssh.Client) error {
	if client == nil {
		return fmt.Errorf("SSH reconnect returned an empty client")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return fmt.Errorf("SSH transport %s@%s is closed", t.user, t.host)
	}
	t.client = client
	t.clientGeneration++
	return nil
}

func (t *sshTransport) reconnectBackoff(attempt int) time.Duration {
	if t.backoff != nil {
		return t.backoff(attempt)
	}
	return sshReconnectBackoff(attempt)
}

func sshReconnectBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := sshReconnectInitialBackoff
	for i := 1; i < attempt && delay < sshReconnectMaxBackoff; i++ {
		delay *= 2
		if delay > sshReconnectMaxBackoff {
			delay = sshReconnectMaxBackoff
		}
	}
	// Equal jitter prevents several conversations sharing one restored network
	// from redialing their independent hosts at exactly the same instant.
	half := delay / 2
	return half + time.Duration(rand.Int64N(int64(delay-half)+1))
}

func waitContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func classifySSHReconnectError(err error) (string, bool) {
	var hostKeyErr *SSHHostKeyError
	if errors.As(err, &hostKeyErr) {
		return hostKeyErr.Code, false
	}
	lower := strings.ToLower(errorString(err))
	if strings.Contains(lower, "unable to authenticate") ||
		strings.Contains(lower, "no supported methods remain") ||
		strings.Contains(lower, "no ssh credentials") {
		return "ssh_auth_required", false
	}
	return "ssh_connection_failed", true
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func probeSSHClient(ctx context.Context, client *ssh.Client, timeout time.Duration) error {
	if client == nil {
		return fmt.Errorf("empty SSH client")
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Probe by opening a disposable channel instead of sending a want-reply
	// global keepalive. x/crypto/ssh's global-response drain loop spins forever
	// when that response channel is already closed, turning every probe of a
	// dead connection into a leaked 100% CPU goroutine.
	type probeResult struct {
		session *ssh.Session
		err     error
	}
	done := make(chan probeResult, 1)
	go func() {
		session, err := client.NewSession()
		done <- probeResult{session: session, err: err}
	}()
	select {
	case result := <-done:
		if result.session != nil {
			_ = result.session.Close()
		}
		return result.err
	case <-probeCtx.Done():
		return probeCtx.Err()
	}
}

func (t *sshTransport) probe(ctx context.Context) error {
	client, generation, err := t.connectedClient(ctx, fmt.Errorf("SSH probe found no live client"))
	if err != nil {
		return fmt.Errorf("ssh probe: %w", err)
	}
	probeErr := probeSSHClient(ctx, client, sshProbeTimeout)
	if probeErr == nil {
		return nil
	}
	t.invalidateClient(client, generation)
	if err := ctx.Err(); err != nil {
		return err
	}
	if reconnectErr := t.ensureConnected(ctx, probeErr); reconnectErr != nil {
		return fmt.Errorf("ssh probe: %w", reconnectErr)
	}
	return nil
}

func (t *sshTransport) keepaliveLoop() {
	delay := sshKeepaliveEvery
	for {
		if err := waitContext(t.lifetimeCtx, delay); err != nil {
			return
		}
		if err := t.probe(t.lifetimeCtx); err != nil {
			if t.lifetimeCtx != nil && t.lifetimeCtx.Err() != nil {
				return
			}
			appconfig.Logger().Printf("[ssh] keepalive could not recover %s@%s: %v", t.user, t.host, err)
			delay = sshKeepaliveRetryEvery
			continue
		}
		delay = sshKeepaliveEvery
	}
}
