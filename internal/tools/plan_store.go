package tools

import (
	"sync"
)

// PlanStatus represents the lifecycle state of a plan.
type PlanStatus string

const (
	PlanDraft     PlanStatus = "draft"
	PlanSubmitted PlanStatus = "submitted"
	PlanApproved  PlanStatus = "approved"
	PlanRejected  PlanStatus = "rejected"
)

// PlanStore is a concurrency-safe in-memory store for the active plan.
type PlanStore struct {
	mu       sync.RWMutex
	title    string
	content  string
	status   PlanStatus
	feedback string // rejection feedback
}

// NewPlanStore creates an empty PlanStore.
func NewPlanStore() *PlanStore {
	return &PlanStore{status: PlanDraft}
}

// SetDraft stores a plan as draft.
func (s *PlanStore) SetDraft(title, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.title = title
	s.content = content
	s.status = PlanDraft
	s.feedback = ""
}

// Submit marks the plan as submitted for user review.
func (s *PlanStore) Submit(title, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.title = title
	s.content = content
	s.status = PlanSubmitted
	s.feedback = ""
}

// Approve marks the plan as approved.
func (s *PlanStore) Approve() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = PlanApproved
}

// Reject marks the plan as rejected with optional feedback.
func (s *PlanStore) Reject(feedback string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = PlanRejected
	s.feedback = feedback
}

// Status returns the current plan status.
func (s *PlanStore) Status() PlanStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// Content returns the plan content.
func (s *PlanStore) Content() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.content
}

// Title returns the plan title.
func (s *PlanStore) Title() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.title
}

// Feedback returns the rejection feedback.
func (s *PlanStore) Feedback() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.feedback
}

// Clear resets the plan store to empty draft state.
func (s *PlanStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.title = ""
	s.content = ""
	s.status = PlanDraft
	s.feedback = ""
}

// HasApprovedPlan returns true if a plan is currently approved.
func (s *PlanStore) HasApprovedPlan() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status == PlanApproved && s.content != ""
}
