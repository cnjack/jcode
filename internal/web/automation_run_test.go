package web

import "testing"

// runAutomation keys the engine on eng.taskID (the single registration done by
// buildLocalEngine) and reclaims it with deleteEngine(eng.taskID). This guards
// the Finding-1 contract: the engine must live under exactly ONE tasks-map key
// so a run can't leak an entry (the earlier code registered a second time under
// a different id, leaking one entry per run and exhausting the pool after
// maxLiveEngines runs).
func TestAutomationEngineRegisteredOnceAndReclaimed(t *testing.T) {
	s := stubFactoryServer(t)

	for i := 0; i < maxLiveEngines+8; i++ {
		eng, err := s.buildLocalEngine("", "/proj/auto", "full_access")
		if err != nil {
			t.Fatalf("run %d: buildLocalEngine: %v (engine pool leaked?)", i, err)
		}
		sid := eng.taskID // exactly what runAutomation uses as the session id

		s.tasksMu.RLock()
		n := len(s.tasks)
		_, ok := s.tasks[sid]
		s.tasksMu.RUnlock()
		if !ok {
			t.Fatalf("run %d: engine not registered under its taskID", i)
		}
		if n != 1 {
			t.Fatalf("run %d: want exactly 1 live engine, got %d (double registration leaks)", i, n)
		}

		s.deleteEngine(sid) // run completion
		s.tasksMu.RLock()
		n = len(s.tasks)
		s.tasksMu.RUnlock()
		if n != 0 {
			t.Fatalf("run %d: engine not reclaimed, %d still live", i, n)
		}
	}
}
