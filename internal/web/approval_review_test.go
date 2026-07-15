package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/review"
)

func getApprovalReview(t *testing.T, cfg *config.Config) map[string]any {
	t.Helper()
	s := &Server{cfg: cfg}
	rec := httptest.NewRecorder()
	s.handleGetApprovalReviewConfig(rec, httptest.NewRequest(http.MethodGet, "/api/approval-review-config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rec.Body.String())
	}
	return body
}

// TestGetApprovalReviewConfigDefaults pins that unset fields come back unset
// (so "empty = follow the built-in default" survives a save round-trip) while
// the resolved defaults ride along separately for the settings form to show.
func TestGetApprovalReviewConfigDefaults(t *testing.T) {
	body := getApprovalReview(t, &config.Config{})

	if got := body["timeout_seconds"]; got != float64(0) {
		t.Errorf("stored timeout_seconds=%v, want 0 (unset)", got)
	}
	if got := body["audit_path"]; got != "" {
		t.Errorf("stored audit_path=%v, want empty (unset)", got)
	}
	if got := body["model"]; got != "" {
		t.Errorf("stored model=%v, want empty (unset)", got)
	}

	defaults, ok := body["defaults"].(map[string]any)
	if !ok {
		t.Fatalf("defaults block missing or wrong shape: %#v", body["defaults"])
	}
	if got, want := defaults["timeout_seconds"], float64(review.DefaultTimeout.Seconds()); got != want {
		t.Errorf("defaults.timeout_seconds=%v, want %v", got, want)
	}
	if got, want := defaults["audit_path"], review.DefaultAuditPath(); got != want {
		t.Errorf("defaults.audit_path=%v, want %v", got, want)
	}
	if defaults["audit_path"] == "" {
		t.Error("defaults.audit_path is empty; the form would show no hint")
	}
}

// TestGetApprovalReviewConfigStored pins that configured values are returned
// as-is rather than being masked by the defaults block.
func TestGetApprovalReviewConfigStored(t *testing.T) {
	body := getApprovalReview(t, &config.Config{ApprovalReview: &config.ApprovalReviewConfig{
		Model:          "small",
		TimeoutSeconds: 5,
		AuditPath:      "/tmp/custom.jsonl",
		Investigate:    true,
	}})

	if got := body["model"]; got != "small" {
		t.Errorf("model=%v, want %q", got, "small")
	}
	if got := body["timeout_seconds"]; got != float64(5) {
		t.Errorf("timeout_seconds=%v, want 5", got)
	}
	if got := body["audit_path"]; got != "/tmp/custom.jsonl" {
		t.Errorf("audit_path=%v, want %q", got, "/tmp/custom.jsonl")
	}
	if got := body["investigate"]; got != true {
		t.Errorf("investigate=%v, want true", got)
	}
	// The defaults block is informational and must not track the stored values.
	defaults := body["defaults"].(map[string]any)
	if got := defaults["timeout_seconds"]; got == float64(5) {
		t.Error("defaults.timeout_seconds tracked the stored value; it must stay the built-in default")
	}
}
