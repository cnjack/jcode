package toolpolicy

import (
	"context"
	"fmt"
	"sync"
)

type runIDContextKey struct{}

func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, runIDContextKey{}, runID)
}

func RunIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(runIDContextKey{}).(string)
	return value
}

// UsageLedger atomically reserves provider calls before dispatch. The durable
// generation journal remains the source of truth; initialDispatched is rebuilt
// from it whenever an agent/tool is reconstructed.
type UsageLedger struct {
	mu            sync.Mutex
	maxPerRun     int
	maxPerSession int
	sessionCount  int
	runCounts     map[string]int
	reservations  map[string]*UsageReservation
}

type UsageReservation struct {
	ledger      *UsageLedger
	operationID string
	runID       string
	done        bool
}

func NewUsageLedger(maxPerRun, maxPerSession, initialDispatched int) *UsageLedger {
	return &UsageLedger{
		maxPerRun: maxPerRun, maxPerSession: maxPerSession,
		sessionCount: initialDispatched, runCounts: make(map[string]int),
		reservations: make(map[string]*UsageReservation),
	}
}

// SetLimits updates policy limits without replacing the session-scoped ledger.
// Existing dispatched and reserved calls remain consumed.
func (l *UsageLedger) SetLimits(maxPerRun, maxPerSession int) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.maxPerRun = maxPerRun
	l.maxPerSession = maxPerSession
	l.mu.Unlock()
}

// ResetSession switches a transport-owned ledger to another idle session.
// Callers must only do this between runs (for example TUI session resume).
func (l *UsageLedger) ResetSession(initialDispatched int) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.sessionCount = initialDispatched
	l.runCounts = make(map[string]int)
	l.reservations = make(map[string]*UsageReservation)
	l.mu.Unlock()
}

func (l *UsageLedger) Reserve(runID, operationID string) (*UsageReservation, error) {
	return l.reserve(runID, operationID, true)
}

// ReserveRun protects the process-local per-turn limit while leaving the hard
// per-session decision to Recorder's cross-process dispatch transaction. Two
// processes necessarily have different in-memory ledgers, so checking a
// cached sessionCount here can never be the security boundary.
func (l *UsageLedger) ReserveRun(runID, operationID string) (*UsageReservation, error) {
	return l.reserve(runID, operationID, false)
}

func (l *UsageLedger) reserve(
	runID, operationID string,
	enforceSessionLimit bool,
) (*UsageReservation, error) {
	if l == nil {
		return nil, fmt.Errorf("provider usage ledger is unavailable")
	}
	if runID == "" {
		runID = "direct"
	}
	if operationID == "" {
		return nil, fmt.Errorf("provider operation ID is required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.reservations[operationID]; exists {
		return nil, fmt.Errorf("provider operation %q is already reserved", operationID)
	}
	if l.maxPerRun > 0 && l.runCounts[runID] >= l.maxPerRun {
		return nil, fmt.Errorf("provider call limit reached for this turn")
	}
	if enforceSessionLimit && l.maxPerSession > 0 && l.sessionCount >= l.maxPerSession {
		return nil, fmt.Errorf("provider call limit reached for this session")
	}
	reservation := &UsageReservation{ledger: l, operationID: operationID, runID: runID}
	l.reservations[operationID] = reservation
	l.runCounts[runID]++
	l.sessionCount++
	return reservation, nil
}

// Commit makes the reservation sticky. It is called only after the synchronous
// dispatch_attempted journal append succeeds.
func (r *UsageReservation) Commit() {
	if r == nil || r.ledger == nil {
		return
	}
	r.ledger.mu.Lock()
	r.done = true
	r.ledger.mu.Unlock()
}

// Release rolls back a pre-dispatch reservation. A committed reservation can
// never be released, even if the provider call later fails.
func (r *UsageReservation) Release() {
	if r == nil || r.ledger == nil {
		return
	}
	l := r.ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	if r.done {
		return
	}
	current, exists := l.reservations[r.operationID]
	if !exists || current != r {
		return
	}
	delete(l.reservations, r.operationID)
	if l.runCounts[r.runID] > 0 {
		l.runCounts[r.runID]--
	}
	if l.sessionCount > 0 {
		l.sessionCount--
	}
	r.done = true
}
