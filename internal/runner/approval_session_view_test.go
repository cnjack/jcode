package runner

import "testing"

// TestSessionViewToolsAreReadOnlyApproved pins the approval tier of the
// cross-session view tools: they only read history, so they must never prompt
// in MANUAL mode (design §5.9 / §6.2).
func TestSessionViewToolsAreReadOnlyApproved(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)

	tools := []string{"session_list", "session_search", "session_read"}
	args := []string{`{}`, `{"query":"flaky test"}`, `{"session_id":"3f2a1c9e-6b7d-4e8f-9a0b-1c2d3e4f5a6b","start":98}`}
	for i, name := range tools {
		if got := s.decide(name, args[i]); got != decisionAutoApprove {
			t.Errorf("%s: got %v want auto-approve", name, got)
		}
	}
}

// TestSessionViewToolsStayAutoApprovedOutsideWorkdir makes sure the workpath
// escape heuristic cannot turn a history read into a prompt: these tools take
// no path argument at all.
func TestSessionViewToolsStayAutoApprovedOutsideWorkdir(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)
	for _, name := range []string{"session_list", "session_search", "session_read"} {
		if got := s.decide(name, `{"session_id":"../etc/passwd"}`); got != decisionAutoApprove {
			t.Errorf("%s: got %v want auto-approve", name, got)
		}
	}
}
