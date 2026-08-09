package toolpolicy

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestUsageLedgerAtomicallyLimitsConcurrentReservations(t *testing.T) {
	ledger := NewUsageLedger(1, 20, 0)
	var admitted atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(operationID string) {
			defer wg.Done()
			<-start
			reservation, err := ledger.Reserve("run-1", operationID)
			if err == nil {
				admitted.Add(1)
				reservation.Commit()
			}
		}(string(rune('a' + i)))
	}
	close(start)
	wg.Wait()
	if got := admitted.Load(); got != 1 {
		t.Fatalf("admitted reservations = %d, want 1", got)
	}
}

func TestUsageLedgerReleaseOnlyRollsBackBeforeCommit(t *testing.T) {
	ledger := NewUsageLedger(1, 1, 0)
	first, err := ledger.Reserve("run-1", "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	first.Release()
	second, err := ledger.Reserve("run-1", "operation-2")
	if err != nil {
		t.Fatalf("reserve after pre-dispatch release: %v", err)
	}
	second.Commit()
	second.Release()
	if _, err := ledger.Reserve("run-2", "operation-3"); err == nil {
		t.Fatal("committed provider call must continue consuming the session limit")
	}
}

func TestUsageLedgerReplaysDurableDispatchCount(t *testing.T) {
	ledger := NewUsageLedger(2, 3, 3)
	if _, err := ledger.Reserve("run-1", "operation-1"); err == nil {
		t.Fatal("durable dispatch count must consume the session limit")
	}
}

func TestUsageLedgerReserveRunLeavesSessionDecisionToDurableTransaction(t *testing.T) {
	ledger := NewUsageLedger(1, 1, 1)
	reservation, err := ledger.ReserveRun("run-1", "operation-1")
	if err != nil {
		t.Fatalf("per-run reservation used stale process session count: %v", err)
	}
	defer reservation.Release()
	if _, err := ledger.ReserveRun("run-1", "operation-2"); err == nil {
		t.Fatal("ReserveRun did not enforce the process-local per-turn limit")
	}
}
