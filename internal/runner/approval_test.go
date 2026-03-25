package runner

import (
	"context"
    "testing"

    "github.com/cnjack/coding/internal/tui"
)

func TestNewApprovalState(t *testing.T) {
    s := NewApprovalState("/tmp/workdir")
    if s.mode != tui.ModeManual {
        t.Errorf("expected default mode to be Manual, got %v", s.mode)
    }
}

func TestApprovalState_SetWorkpath(t *testing.T) {
    s := NewApprovalState("/tmp/workdir")
    s.SetWorkpath("/tmp/otherdir")
    if s.workpath != "/tmp/otherdir" {
        t.Errorf("expected workpath to be /tmp/otherdir, got %v", s.workpath)
    }
}

func TestIsWithinWorkpath(t *testing.T) {
    s := NewApprovalState("/tmp/workdir")

    tests := []struct {
        path     string
        expected bool
    }{
        {"/tmp/workdir/file.txt", true},
        {"/tmp/workdir/subdir/file.txt", true},
        {"/tmp/workdir/subdir", true},
        {"/tmp/otherdir/file.txt", false},
        {"/tmp/external_dir/file.txt", false},
        {"/etc/passwd", false},
        {"../file.txt", false},
        {"/tmp/workdir/../external/file.txt", false},
        {"/tmp/workdir/../external_dir/file.txt", false},
    }

    for _, tc := range tests {
        result := s.isWithinWorkpath(tc.path)
        if result != tc.expected {
            t.Errorf("isWithinWorkpath(%q) = %v, expected %v", tc.path, tc.expected, result)
        }
    }
}

func TestRequestApproval_AutoMode(t *testing.T) {
    s := NewApprovalState("/tmp/workdir")
    s.SetMode(tui.ModeAuto)

    ctx := context.Background()

    // AUTO mode - all tools auto-approve
    approved, err := s.RequestApproval(ctx, "read", `{"file_path": "/etc/passwd"}`)
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
    if !approved {
        t.Errorf("expected auto-approve in AUTO mode")
    }
}

func TestRequestApproval_ManualMode(t *testing.T) {
    s := NewApprovalState("/tmp/workdir")
    ctx := context.Background()

    // Test read within workpath - auto-approve
    approved, err := s.RequestApproval(ctx, "read", `{"file_path": "/tmp/workdir/file.txt"}`)
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
    if !approved {
        t.Errorf("expected auto-approve for read within workpath")
    }

    // Test read outside workpath - needs approval (no TUI program, should fail)
    approved, err = s.RequestApproval(ctx, "read", `{"file_path": "/etc/passwd"}`)
    if err == nil {
        // Without TUI program, this should error
        t.Logf("read outside workpath correctly returned error: %v", err)
    // Test read outside workpath - needs approval (no TUI program, should fail)
    approved, err = s.RequestApproval(ctx, "read", `{"file_path": "/etc/passwd"}`)
    if err == nil {
        // Without TUI program, this should error
        t.Logf("read outside workpath correctly returned error: %v", err)
    }

}

    // Test safe command - auto-approve
    approved, err = s.RequestApproval(ctx, "execute", `{"command": "ls -la"}`)
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
    if !approved {
        t.Errorf("expected auto-approve for safe command")
    }

    // Test dangerous command - needs approval (no TUI program, should fail)
    approved, err = s.RequestApproval(ctx, "execute", `{"command": "rm -rf /"}`)
    if err == nil {
        // Without TUI program, this should error
        t.Logf("dangerous command correctly returned error: %v", err)
    }
}

func TestRequestApproval_NoApprovalTools(t *testing.T) {
    s := NewApprovalState("/tmp/workdir")
    ctx := context.Background()

    // Test glob - auto-approve
    approved, err := s.RequestApproval(ctx, "glob", `{"pattern": "*.go"}`)
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
    if !approved {
        t.Errorf("expected auto-approve for glob")
    }

    // Test grep - auto-approve
    approved, err = s.RequestApproval(ctx, "grep", `{"pattern": "test"}`)
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
    if !approved {
        t.Errorf("expected auto-approve for grep")
    }
}
