package command

import (
	"context"
	"strings"
	"sync"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/cnjack/jcode/internal/computer"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/tools"
)

func newACPTestComputerManager(t *testing.T) *computer.Manager {
	t.Helper()
	mgr := computer.NewManager(computer.Config{Enabled: true}, t.TempDir())
	mgr.SetFakeBackend(computer.NewFake())
	return mgr
}

func TestACPSharedComputerManagerConcurrentSessions(t *testing.T) {
	mgr := newACPTestComputerManager(t)
	a := &acpAgent{
		sessions:    make(map[acp.SessionId]*acpSession),
		computerMgr: mgr,
	}
	t.Cleanup(a.close)
	cfg := &config.Config{Computer: &config.ComputerConfig{Enabled: true, MaxActionsPerBatch: 7}}

	const callers = 16
	got := make(chan *computer.Manager, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			shared, err := a.sharedComputerManager(cfg)
			if err != nil {
				t.Errorf("sharedComputerManager: %v", err)
				return
			}
			got <- shared
		}()
	}
	wg.Wait()
	close(got)
	for shared := range got {
		if shared != mgr {
			t.Fatalf("ACP session received Manager %p, want process-wide %p", shared, mgr)
		}
	}
	if batch := mgr.MaxBatch(); batch != 7 {
		t.Fatalf("shared Manager did not receive current config: max batch=%d", batch)
	}
}

func TestACPSessionCloseReleasesOnlyTaskComputerSession(t *testing.T) {
	mgr := newACPTestComputerManager(t)
	t.Cleanup(func() { _ = mgr.Close() })

	firstEnv := tools.NewEnv(t.TempDir(), "darwin")
	firstEnv.Computer = mgr
	secondEnv := tools.NewEnv(t.TempDir(), "darwin")
	secondEnv.Computer = mgr

	first, err := firstEnv.ComputerSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := secondEnv.ComputerSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	(&acpSession{env: firstEnv}).Close()

	reopened, err := firstEnv.ComputerSession(context.Background())
	if err != nil {
		t.Fatalf("session Close shut down the shared Manager: %v", err)
	}
	if reopened == first {
		t.Fatal("session Close retained its task-scoped computer Session")
	}
	stillSecond, err := secondEnv.ComputerSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stillSecond != second {
		t.Fatal("closing one ACP session replaced another session's computer state")
	}
	firstEnv.CloseComputer()
	secondEnv.CloseComputer()
}

func TestACPAgentCloseStopsSharedComputerManagerAfterSessions(t *testing.T) {
	mgr := newACPTestComputerManager(t)
	env := tools.NewEnv(t.TempDir(), "darwin")
	env.Computer = mgr
	if _, err := env.ComputerSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	a := &acpAgent{
		sessions: map[acp.SessionId]*acpSession{
			"session-one": {env: env},
		},
		computerMgr: mgr,
	}

	a.close()

	if len(a.sessions) != 0 || !a.computerClosed || a.computerMgr != nil {
		t.Fatalf("ACP agent lifecycle not fully closed: sessions=%d closed=%v manager=%p",
			len(a.sessions), a.computerClosed, a.computerMgr)
	}
	if _, err := mgr.OpenSession(context.Background()); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("process-wide Manager survived ACP agent close: %v", err)
	}
	if _, err := env.ComputerSession(context.Background()); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("task computer Session was not released before Manager close: %v", err)
	}
	if _, err := a.sharedComputerManager(&config.Config{}); err == nil {
		t.Fatal("closed ACP agent recreated a computer Manager")
	}
}
