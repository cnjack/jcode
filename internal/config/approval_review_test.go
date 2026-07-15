package config

import (
	"sync"
	"testing"
)

func TestApprovalReviewSettingsSnapshot(t *testing.T) {
	cfg := &Config{}
	if got := cfg.ApprovalReviewSettings(); got != (ApprovalReviewConfig{}) {
		t.Errorf("unset block: got %+v, want zero value", got)
	}
	if got := (*Config)(nil).ApprovalReviewSettings(); got != (ApprovalReviewConfig{}) {
		t.Errorf("nil config: got %+v, want zero value", got)
	}

	cfg.SetApprovalReview(&ApprovalReviewConfig{Model: "small", TimeoutSeconds: 30})
	snap := cfg.ApprovalReviewSettings()
	if snap.Model != "small" || snap.TimeoutSeconds != 30 {
		t.Fatalf("after publish: got %+v", snap)
	}

	// The snapshot is a copy: a later publish must not reach through it.
	cfg.SetApprovalReview(&ApprovalReviewConfig{Model: "other"})
	if snap.Model != "small" {
		t.Errorf("snapshot mutated by later publish: %+v", snap)
	}
	if got := cfg.ApprovalReviewSettings().Model; got != "other" {
		t.Errorf("republish not visible: model=%q, want %q", got, "other")
	}
}

// TestApprovalReviewConcurrentPublish pins that the web settings handler can
// publish a new block while a task goroutine reads it to build its reviewer.
// The two hold no lock in common, so this only passes under -race because
// ApprovalReviewSettings/SetApprovalReview share approvalReviewMu.
func TestApprovalReviewConcurrentPublish(t *testing.T) {
	cfg := &Config{ApprovalReview: &ApprovalReviewConfig{Model: "initial"}}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); cfg.SetApprovalReview(&ApprovalReviewConfig{Model: "published"}) }()
		go func() {
			defer wg.Done()
			if got := cfg.ApprovalReviewSettings().Model; got != "initial" && got != "published" {
				t.Errorf("torn read: model=%q", got)
			}
		}()
	}
	wg.Wait()
}
