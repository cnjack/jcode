package web

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tools"
)

// drain non-blockingly collects every queued WSEvent on a client's send channel.
func drain(c *WSClient) []WSEvent {
	var out []WSEvent
	for {
		select {
		case data := <-c.sendCh:
			var ev WSEvent
			if json.Unmarshal(data, &ev) == nil {
				out = append(out, ev)
			}
		default:
			return out
		}
	}
}

func hasType(evs []WSEvent, typ string) bool {
	for _, e := range evs {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// TestWSClientWants locks the per-client subscription predicate that prevents a
// busy task from flooding a client viewing a different one.
func TestWSClientWants(t *testing.T) {
	c := newWSClient(nil)
	// Before subscribing, a client receives everything (legacy compatibility).
	if !c.wants("task-A") || !c.wants("") {
		t.Fatal("fresh client (subAll) should want all events")
	}
	c.subscribe([]string{"task-A"})
	if !c.wants("task-A") {
		t.Error("subscribed task should be wanted")
	}
	if c.wants("task-B") {
		t.Error("unsubscribed task must NOT be wanted after first subscribe")
	}
	if !c.wants("") {
		t.Error("global events (empty task) must always be wanted")
	}
	c.subscribe([]string{"task-B"})
	if !c.wants("task-A") || !c.wants("task-B") {
		t.Error("subscriptions are additive")
	}
	c.unsubscribe([]string{"task-A"})
	if c.wants("task-A") {
		t.Error("unsubscribe should drop the task")
	}
}

// TestBrokerDeliversByTaskID proves the broker fans an event only to clients
// subscribed to its task (plus all clients for global events).
func TestBrokerDeliversByTaskID(t *testing.T) {
	b := NewWSBroker()
	viewer := newWSClient(nil) // only watching task-A
	viewer.subscribe([]string{"task-A"})
	legacy := newWSClient(nil) // never subscribed → sees everything
	b.mu.Lock()
	b.clients[1] = viewer
	b.clients[2] = legacy
	b.mu.Unlock()

	b.Broadcast(WSEvent{TaskID: "task-A", Type: "a_event"})
	b.Broadcast(WSEvent{TaskID: "task-B", Type: "b_event"})
	b.Broadcast(WSEvent{Type: "global_event"})

	got := drain(viewer)
	if !hasType(got, "a_event") || !hasType(got, "global_event") {
		t.Errorf("viewer should get its task + global, got %+v", got)
	}
	if hasType(got, "b_event") {
		t.Errorf("viewer must NOT get another task's events, got %+v", got)
	}
	gotLegacy := drain(legacy)
	if !hasType(gotLegacy, "a_event") || !hasType(gotLegacy, "b_event") || !hasType(gotLegacy, "global_event") {
		t.Errorf("legacy (subAll) client should get every event, got %+v", gotLegacy)
	}
}

// TestEnginePumpStampsTaskID is the end-to-end pump test: an engine's handler
// event reaches a subscribed client tagged with that engine's task id.
func TestEnginePumpStampsTaskID(t *testing.T) {
	s := &Server{Engine: &Engine{}, tasks: make(map[string]*Engine), wsBroker: NewWSBroker()}
	bg := context.Background()
	s.ctxPtr.Store(&bg)
	h := handler.NewWebHandler()
	eng := &Engine{taskID: "task-1", handler: h}

	client := newWSClient(nil)
	client.subscribe([]string{"task-1"})
	s.wsBroker.mu.Lock()
	s.wsBroker.clients[1] = client
	s.wsBroker.mu.Unlock()

	_ = s.registerEngine(eng) // starts the per-engine pump
	h.OnAgentText("hello from task 1")

	deadline := time.After(2 * time.Second)
	for {
		select {
		case data := <-client.sendCh:
			var ev WSEvent
			_ = json.Unmarshal(data, &ev)
			if ev.Type == "agent_text" {
				if ev.TaskID != "task-1" {
					t.Fatalf("event task_id = %q, want task-1", ev.TaskID)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for the pumped agent_text event")
		}
	}
}

// stubFactoryServer builds a Server whose newEngine factory produces fully
// isolated (but agent-less) engines, so engine lifecycle/routing can be tested
// without a live model.
func stubFactoryServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s := &Server{Engine: &Engine{}, tasks: make(map[string]*Engine), wsBroker: NewWSBroker()}
	bg := context.Background()
	s.ctxPtr.Store(&bg)
	s.newEngine = func(taskID, pwd, modeStr string) (*EngineConfig, error) {
		rec, _ := session.NewRecorder(pwd, "prov", "model")
		if taskID != "" && rec != nil {
			rec.SetUUID(taskID)
		}
		return &EngineConfig{
			TaskID:     taskID,
			Pwd:        pwd,
			Mode:       modeStr,
			Env:        tools.NewEnv(pwd, "darwin/arm64"),
			Recorder:   rec,
			TokenUsage: &model.TokenUsage{},
			Handler:    handler.NewWebHandler(),
		}, nil
	}
	return s
}

// TestEngineIsolationAndRouting verifies concurrently-built engines are fully
// isolated (distinct env/recorder/token tracker) and individually routable.
func TestEngineIsolationAndRouting(t *testing.T) {
	s := stubFactoryServer(t)

	const n = 16
	engines := make([]*Engine, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			eng, err := s.buildLocalEngine("", fmt.Sprintf("/proj/%d", i), "build")
			if err != nil {
				t.Errorf("buildLocalEngine: %v", err)
				return
			}
			engines[i] = eng
		}(i)
	}
	wg.Wait()

	seenEnv := map[*tools.Env]bool{}
	seenTok := map[*model.TokenUsage]bool{}
	seenID := map[string]bool{}
	for i, eng := range engines {
		if eng == nil {
			t.Fatalf("engine %d not built", i)
		}
		if seenEnv[eng.env] {
			t.Errorf("engine %d shares an env with another task", i)
		}
		if seenTok[eng.tokenUsage] {
			t.Errorf("engine %d shares a token tracker with another task", i)
		}
		if seenID[eng.taskID] {
			t.Errorf("engine %d has a duplicate task id %q", i, eng.taskID)
		}
		seenEnv[eng.env] = true
		seenTok[eng.tokenUsage] = true
		seenID[eng.taskID] = true

		// Each engine must be routable by its id, and not collide with others.
		if got := s.resolveEngine(eng.taskID); got != eng {
			t.Errorf("resolveEngine(%q) returned the wrong engine", eng.taskID)
		}
	}
	if got := s.resolveEngine("does-not-exist"); got != nil {
		t.Errorf("resolveEngine(unknown) = %v, want nil", got)
	}
}

// TestPerTaskGateIndependence proves one running task does not block another:
// the busy flag is per-engine, not global.
func TestPerTaskGateIndependence(t *testing.T) {
	a := &Engine{taskID: "a"}
	b := &Engine{taskID: "b"}

	if !a.running.CompareAndSwap(false, true) {
		t.Fatal("task a should acquire its own gate")
	}
	// a is busy; b must still be able to start.
	if !b.running.CompareAndSwap(false, true) {
		t.Fatal("task b must NOT be blocked by task a running")
	}
	// a cannot double-start while busy.
	if a.running.CompareAndSwap(false, true) {
		t.Fatal("task a must not start twice concurrently")
	}
	a.running.Store(false)
	if !a.running.CompareAndSwap(false, true) {
		t.Fatal("task a should restart after finishing")
	}
}

// TestDeleteEngineTeardown verifies a non-active task can be torn down (removed
// from the map, pump stopped) without disturbing the others.
func TestDeleteEngineTeardown(t *testing.T) {
	s := stubFactoryServer(t)
	keep, _ := s.buildLocalEngine("", "/proj/keep", "build")
	drop, _ := s.buildLocalEngine("", "/proj/drop", "build")

	s.deleteEngine(drop.taskID)

	if got := s.resolveEngine(drop.taskID); got != nil {
		t.Error("deleted engine should no longer resolve")
	}
	if got := s.resolveEngine(keep.taskID); got != keep {
		t.Error("the other engine must remain routable")
	}
}
