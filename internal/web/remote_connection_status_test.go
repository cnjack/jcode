package web

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/tools"
)

type statusRemoteExecutor struct {
	mu      sync.Mutex
	handler tools.RemoteConnectionStatusHandler
}

func (*statusRemoteExecutor) ReadFile(context.Context, string) ([]byte, error) { return nil, nil }
func (*statusRemoteExecutor) WriteFile(context.Context, string, []byte, os.FileMode) error {
	return nil
}
func (*statusRemoteExecutor) MkdirAll(context.Context, string, os.FileMode) error { return nil }
func (*statusRemoteExecutor) Stat(context.Context, string) (*tools.FileInfo, error) {
	return &tools.FileInfo{Exists: true}, nil
}
func (*statusRemoteExecutor) Exec(context.Context, string, string, time.Duration) (string, string, error) {
	return "", "", nil
}
func (*statusRemoteExecutor) Platform() string               { return "linux/amd64" }
func (*statusRemoteExecutor) Label() string                  { return "status-remote" }
func (*statusRemoteExecutor) ProjectLabel(pwd string) string { return "ssh://test@example.test" + pwd }
func (*statusRemoteExecutor) Probe(context.Context) error    { return nil }
func (e *statusRemoteExecutor) SetRemoteConnectionStatusHandler(h tools.RemoteConnectionStatusHandler) {
	e.mu.Lock()
	e.handler = h
	e.mu.Unlock()
}
func (e *statusRemoteExecutor) Close() error {
	e.SetRemoteConnectionStatusHandler(nil)
	return nil
}
func (e *statusRemoteExecutor) emit(status tools.RemoteConnectionStatus) {
	e.mu.Lock()
	h := e.handler
	e.mu.Unlock()
	if h != nil {
		h(status)
	}
}

func TestRemoteConnectionStatusBridgeIsTaskScoped(t *testing.T) {
	s := &Server{Engine: &Engine{}, tasks: make(map[string]*Engine), wsBroker: NewWSBroker()}
	bg := context.Background()
	s.ctxPtr.Store(&bg)

	exec := &statusRemoteExecutor{}
	env := tools.NewEnv("/local", "darwin/arm64")
	env.SetRemote(exec, "/work")
	eng := &Engine{
		taskID: "remote-task", env: env,
		handler: handler.NewWebHandler(),
	}
	client := newWSClient(nil)
	client.subscribe([]string{"remote-task"})
	s.wsBroker.mu.Lock()
	s.wsBroker.clients[1] = client
	s.wsBroker.mu.Unlock()

	if err := s.registerEngine(eng); err != nil {
		t.Fatalf("register engine: %v", err)
	}
	t.Cleanup(func() { s.deleteEngine(eng.taskID) })

	exec.emit(tools.RemoteConnectionStatus{
		Kind: "ssh", Status: "reconnecting", Attempt: 2, MaxAttempts: 3,
		Host: "example.test:22", RetryInMS: 250,
	})

	select {
	case raw := <-client.sendCh:
		var event struct {
			Type   string                       `json:"type"`
			TaskID string                       `json:"task_id"`
			Data   tools.RemoteConnectionStatus `json:"data"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if event.Type != "remote_connection_status" || event.TaskID != eng.taskID {
			t.Fatalf("event = %+v, want task-scoped remote_connection_status", event)
		}
		if event.Data.Status != "reconnecting" || event.Data.Attempt != 2 || event.Data.MaxAttempts != 3 {
			t.Fatalf("status data = %+v", event.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for remote connection status")
	}
}
