package tools

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestProbeSSHClientClosedConnectionDoesNotLeakBusyGoroutines(t *testing.T) {
	addr, hostKey, clientSigner, _ := startLeaseTestSSHServer(t)
	exec, err := NewSSHExecutorContext(
		context.Background(),
		addr,
		"test",
		[]ssh.AuthMethod{ssh.PublicKeys(clientSigner)},
		ssh.FixedHostKey(hostKey),
	)
	if err != nil {
		t.Fatalf("connect test SSH server: %v", err)
	}
	t.Cleanup(func() { _ = exec.Close() })

	client, _, _ := exec.transport.clientSnapshot()
	if client == nil {
		t.Fatal("missing SSH client")
	}
	_ = client.Close()
	time.Sleep(20 * time.Millisecond)
	baseline := runtime.NumGoroutine()
	for range 12 {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_ = probeSSHClient(ctx, client, 50*time.Millisecond)
		cancel()
	}
	time.Sleep(50 * time.Millisecond)
	if got := runtime.NumGoroutine(); got > baseline+3 {
		t.Fatalf("closed-client probes leaked goroutines: baseline=%d after=%d", baseline, got)
	}
}

func TestSSHReconnectSingleflight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	releaseDial := make(chan struct{})
	transport := &sshTransport{
		user:           "test",
		host:           "example.test:22",
		refs:           2,
		lifetimeCtx:    ctx,
		lifetimeCancel: cancel,
		keepaliveStop:  make(chan struct{}),
		observers:      make(map[uint64]RemoteConnectionStatusHandler),
		backoff:        func(int) time.Duration { return 0 },
	}
	transport.dial = func(context.Context) (*ssh.Client, error) {
		calls.Add(1)
		<-releaseDial
		return &ssh.Client{}, nil
	}

	results := make(chan error, 2)
	var started sync.WaitGroup
	started.Add(2)
	for range 2 {
		go func() {
			started.Done()
			results <- transport.ensureConnected(context.Background(), errors.New("lost"))
		}()
	}
	started.Wait()
	waitFor(t, time.Second, func() bool { return calls.Load() == 1 })
	close(releaseDial)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("ensureConnected: %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("dial calls = %d, want one shared attempt", got)
	}
}

func TestSSHReconnectCallerCancellationDoesNotCancelOtherWaiter(t *testing.T) {
	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())
	defer lifetimeCancel()
	firstStarted := make(chan struct{})
	allowSuccess := make(chan struct{})
	var calls atomic.Int32
	transport := &sshTransport{
		user:           "test",
		host:           "example.test:22",
		refs:           2,
		lifetimeCtx:    lifetimeCtx,
		lifetimeCancel: lifetimeCancel,
		keepaliveStop:  make(chan struct{}),
		observers:      make(map[uint64]RemoteConnectionStatusHandler),
		backoff:        func(int) time.Duration { return 0 },
	}
	transport.dial = func(ctx context.Context) (*ssh.Client, error) {
		if calls.Add(1) == 1 {
			close(firstStarted)
		}
		select {
		case <-allowSuccess:
			return &ssh.Client{}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	shortCtx, shortCancel := context.WithCancel(context.Background())
	shortResult := make(chan error, 1)
	go func() {
		shortResult <- transport.ensureConnected(shortCtx, errors.New("lost"))
	}()
	<-firstStarted
	longResult := make(chan error, 1)
	go func() {
		longResult <- transport.ensureConnected(context.Background(), errors.New("lost"))
	}()
	waitFor(t, time.Second, func() bool {
		transport.mu.Lock()
		defer transport.mu.Unlock()
		return transport.reconnectWaiters == 2
	})
	shortCancel()
	if err := <-shortResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter error = %v, want context.Canceled", err)
	}
	close(allowSuccess)
	if err := <-longResult; err != nil {
		t.Fatalf("live waiter inherited cancellation: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("dial calls = %d, want original singleflight to continue", got)
	}
}

func TestSSHReconnectPermanentErrorStopsImmediately(t *testing.T) {
	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())
	defer lifetimeCancel()
	var calls atomic.Int32
	var statuses []RemoteConnectionStatus
	transport := &sshTransport{
		user:           "test",
		host:           "example.test:22",
		refs:           1,
		lifetimeCtx:    lifetimeCtx,
		lifetimeCancel: lifetimeCancel,
		keepaliveStop:  make(chan struct{}),
		observers: map[uint64]RemoteConnectionStatusHandler{
			1: func(status RemoteConnectionStatus) { statuses = append(statuses, status) },
		},
		backoff: func(int) time.Duration { return 0 },
	}
	transport.dial = func(context.Context) (*ssh.Client, error) {
		calls.Add(1)
		return nil, &SSHHostKeyError{Code: SSHHostKeyChanged, Host: "example.test:22"}
	}

	err := transport.ensureConnected(context.Background(), errors.New("lost"))
	var hostKeyErr *SSHHostKeyError
	if !errors.As(err, &hostKeyErr) {
		t.Fatalf("error = %v, want SSHHostKeyError", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("dial calls = %d, want permanent failure to stop immediately", got)
	}
	if len(statuses) == 0 || statuses[len(statuses)-1].Status != RemoteConnectionActionRequired {
		t.Fatalf("statuses = %+v, want action_required terminal status", statuses)
	}
	if statuses[len(statuses)-1].Code != SSHHostKeyChanged {
		t.Fatalf("terminal code = %q, want %q", statuses[len(statuses)-1].Code, SSHHostKeyChanged)
	}
}

func TestSSHReconnectExhaustionEmitsFailed(t *testing.T) {
	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())
	defer lifetimeCancel()
	var calls atomic.Int32
	var statusesMu sync.Mutex
	var statuses []RemoteConnectionStatus
	transport := &sshTransport{
		user:           "test",
		host:           "example.test:22",
		refs:           1,
		lifetimeCtx:    lifetimeCtx,
		lifetimeCancel: lifetimeCancel,
		keepaliveStop:  make(chan struct{}),
		observers: map[uint64]RemoteConnectionStatusHandler{
			1: func(status RemoteConnectionStatus) {
				statusesMu.Lock()
				statuses = append(statuses, status)
				statusesMu.Unlock()
			},
		},
		backoff: func(int) time.Duration { return 0 },
	}
	transport.dial = func(context.Context) (*ssh.Client, error) {
		calls.Add(1)
		return nil, fmt.Errorf("network unreachable")
	}

	if err := transport.ensureConnected(context.Background(), errors.New("lost")); err == nil {
		t.Fatal("expected reconnect exhaustion")
	}
	if got := calls.Load(); got != sshReconnectMaxAttempts {
		t.Fatalf("dial calls = %d, want %d", got, sshReconnectMaxAttempts)
	}
	statusesMu.Lock()
	defer statusesMu.Unlock()
	if len(statuses) == 0 || statuses[len(statuses)-1].Status != RemoteConnectionFailed {
		t.Fatalf("statuses = %+v, want failed terminal status", statuses)
	}
}

func TestSSHLastLeaseCloseCancelsReconnect(t *testing.T) {
	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())
	dialStarted := make(chan struct{})
	transport := &sshTransport{
		user:           "test",
		host:           "example.test:22",
		refs:           1,
		nextLeaseID:    1,
		lifetimeCtx:    lifetimeCtx,
		lifetimeCancel: lifetimeCancel,
		keepaliveStop:  make(chan struct{}),
		observers:      make(map[uint64]RemoteConnectionStatusHandler),
		backoff:        func(int) time.Duration { return 0 },
	}
	transport.dial = func(ctx context.Context) (*ssh.Client, error) {
		select {
		case <-dialStarted:
		default:
			close(dialStarted)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	result := make(chan error, 1)
	go func() {
		result <- transport.ensureConnected(context.Background(), errors.New("lost"))
	}()
	<-dialStarted
	if err := transport.release(1); err != nil {
		t.Fatalf("release final lease: %v", err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("reconnect unexpectedly succeeded after final Close")
		}
	case <-time.After(time.Second):
		t.Fatal("final lease Close did not cancel reconnect promptly")
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
