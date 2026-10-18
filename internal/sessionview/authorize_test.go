package sessionview

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cnjack/jcode/internal/session"
)

const (
	projectA = "/work/project-a"
	projectB = "/work/project-b"
)

type authCase struct {
	name       string
	caller     Caller
	id         string
	seed       func(t *testing.T)
	wantErr    error
	wantReason string
	wantID     string
	wantSelf   bool
}

func seedSelf(t *testing.T) {
	t.Helper()
	writeSessionEntries(t, "self-1", []session.Entry{mkStart("self-1", projectA), mkUser("hi")})
	addIndexMeta(t, projectA, mkMeta("self-1", projectA, ""))
}

func seedPeerA(t *testing.T) {
	t.Helper()
	writeSessionEntries(t, "peer-a", []session.Entry{mkStart("peer-a", projectA), mkUser("peer")})
	addIndexMeta(t, projectA, mkMeta("peer-a", projectA, ""))
}

func seedPeerB(t *testing.T) {
	t.Helper()
	writeSessionEntries(t, "peer-b", []session.Entry{mkStart("peer-b", projectB), mkUser("peer")})
	addIndexMeta(t, projectB, mkMeta("peer-b", projectB, ""))
}

func authorizationCases() []authCase {
	seedSelfAndPeer := func(t *testing.T) {
		t.Helper()
		seedSelf(t)
		seedPeerA(t)
		seedPeerB(t)
	}
	return []authCase{
		{
			name: "self via empty id", caller: Caller{SessionID: "self-1", Project: projectA},
			id: "", seed: seedSelf, wantID: "self-1", wantSelf: true,
		},
		{
			name: "self via explicit id", caller: Caller{SessionID: "self-1", Project: projectA},
			id: "self-1", seed: seedSelf, wantID: "self-1", wantSelf: true,
		},
		{
			name: "self allowed with empty caller project", caller: Caller{SessionID: "self-1"},
			id: "", seed: seedSelf, wantID: "self-1", wantSelf: true,
		},
		{
			name: "same project allowed", caller: Caller{SessionID: "self-1", Project: projectA},
			id: "peer-a", seed: seedSelfAndPeer, wantID: "peer-a",
		},
		{
			name: "cross project denied", caller: Caller{SessionID: "self-1", Project: projectA},
			id: "peer-b", seed: seedSelfAndPeer, wantErr: ErrSessionUnavailable, wantReason: "project_mismatch",
		},
		{
			name: "indexed file deleted", caller: Caller{SessionID: "self-1", Project: projectA},
			id: "gone-a",
			seed: func(t *testing.T) {
				addIndexMeta(t, projectA, mkMeta("gone-a", projectA, ""))
			},
			wantErr: ErrSessionUnavailable, wantReason: "missing_file",
		},
		{
			name: "file exists but not indexed", caller: Caller{SessionID: "self-1", Project: projectA},
			id: "unindexed",
			seed: func(t *testing.T) {
				writeSessionEntries(t, "unindexed", []session.Entry{mkStart("unindexed", projectA)})
			},
			wantErr: ErrSessionUnavailable, wantReason: "not_indexed",
		},
		{
			name: "index project differs from file project", caller: Caller{SessionID: "self-1", Project: projectA},
			id: "mismatch",
			seed: func(t *testing.T) {
				writeSessionEntries(t, "mismatch", []session.Entry{mkStart("mismatch", projectA)})
				addIndexMeta(t, projectB, mkMeta("mismatch", projectB, ""))
			},
			wantErr: ErrSessionUnavailable, wantReason: "index_mismatch",
		},
		{
			name:   "legacy file without session_start falls back to index project",
			caller: Caller{SessionID: "self-1", Project: projectA},
			id:     "legacy",
			seed: func(t *testing.T) {
				writeSessionEntries(t, "legacy", []session.Entry{
					{Type: session.EntryUser, Content: "legacy", Timestamp: "2026-10-18T09:00:01+08:00"},
				})
				addIndexMeta(t, projectA, mkMeta("legacy", projectA, ""))
			},
			wantID: "legacy",
		},
		{
			name: "corrupt first line", caller: Caller{SessionID: "self-1", Project: projectA},
			id: "corrupt-header",
			seed: func(t *testing.T) {
				writeRawSession(t, "corrupt-header", []byte("{not json\n{\"type\":\"user\"}\n"))
				addIndexMeta(t, projectA, mkMeta("corrupt-header", projectA, ""))
			},
			wantErr: ErrSessionUnavailable, wantReason: "first_line_corrupt",
		},
		{
			name: "session_start uuid mismatch", caller: Caller{SessionID: "self-1", Project: projectA},
			id: "uuid-mismatch",
			seed: func(t *testing.T) {
				writeSessionEntries(t, "uuid-mismatch", []session.Entry{mkStart("somebody-else", projectA)})
				addIndexMeta(t, projectA, mkMeta("uuid-mismatch", projectA, ""))
			},
			wantErr: ErrSessionUnavailable, wantReason: "start_uuid_mismatch",
		},
		{
			name: "invalid id with slash", caller: Caller{SessionID: "self-1", Project: projectA},
			id: "a/b", seed: func(t *testing.T) {}, wantErr: ErrSessionUnavailable, wantReason: "invalid_id",
		},
		{
			name: "invalid id dot dot", caller: Caller{SessionID: "self-1", Project: projectA},
			id: "..", seed: func(t *testing.T) {}, wantErr: ErrSessionUnavailable, wantReason: "invalid_id",
		},
		{
			name: "empty caller project denies peers", caller: Caller{SessionID: "self-1"},
			id: "peer-a", seed: seedSelfAndPeer, wantErr: ErrNoWorkspace, wantReason: "empty_project",
		},
		{
			name: "no caller identity", caller: Caller{},
			id: "self-1", seed: seedSelf, wantErr: ErrNoCaller, wantReason: "no_caller",
		},
		{
			name: "duplicate index entry is unavailable not io", caller: Caller{SessionID: "self-1", Project: projectA},
			id: "dup",
			seed: func(t *testing.T) {
				writeSessionEntries(t, "dup", []session.Entry{mkStart("dup", projectA)})
				addIndexMeta(t, projectA, mkMeta("dup", projectA, ""))
				addIndexMeta(t, projectB, mkMeta("dup", projectB, ""))
			},
			wantErr: ErrSessionUnavailable, wantReason: "index_error",
		},
		{
			name: "self with missing file", caller: Caller{SessionID: "self-1", Project: projectA},
			id: "",
			seed: func(t *testing.T) {
				addIndexMeta(t, projectA, mkMeta("self-1", projectA, ""))
			},
			wantErr: ErrSessionUnavailable, wantReason: "missing_file",
		},
	}
}

func TestResolveTargetAuthorizationMatrix(t *testing.T) {
	for _, tc := range authorizationCases() {
		t.Run(tc.name, func(t *testing.T) {
			setupHome(t)
			tc.seed(t)
			svc := New(Config{})
			target, err := svc.ResolveTarget(context.Background(), tc.caller, tc.id)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got target %+v", tc.wantErr, target)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error %v is not %v", err, tc.wantErr)
				}
				if got := Reason(err); got != tc.wantReason {
					t.Fatalf("Reason=%q want %q (err=%v)", got, tc.wantReason, err)
				}
				if target.File != nil {
					_ = target.File.Close()
					t.Fatalf("target file must be nil on denial")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v (reason=%s)", err, Reason(err))
			}
			defer func() { _ = target.File.Close() }()
			if target.Ref.ID != tc.wantID {
				t.Fatalf("Ref.ID=%q want %q", target.Ref.ID, tc.wantID)
			}
			if target.Ref.IsCaller != tc.wantSelf {
				t.Fatalf("Ref.IsCaller=%t want %t", target.Ref.IsCaller, tc.wantSelf)
			}
			// The returned handle must be positioned at the start of the file so
			// the caller streams from the beginning of the same inode that was
			// authorized (N5).
			offset, seekErr := target.File.Seek(0, io.SeekCurrent)
			if seekErr != nil {
				t.Fatalf("seek: %v", seekErr)
			}
			if offset != 0 {
				t.Fatalf("file offset=%d want 0", offset)
			}
			if target.Ref.Project == "" {
				t.Fatalf("Ref.Project must be populated")
			}
		})
	}
}

func TestUnavailableErrorsIndistinguishable(t *testing.T) {
	var texts []string
	for _, tc := range authorizationCases() {
		if tc.wantErr != ErrSessionUnavailable {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			setupHome(t)
			tc.seed(t)
			svc := New(Config{})
			target, err := svc.ResolveTarget(context.Background(), tc.caller, tc.id)
			if target.File != nil {
				_ = target.File.Close()
			}
			if !errors.Is(err, ErrSessionUnavailable) {
				t.Fatalf("expected ErrSessionUnavailable, got %v", err)
			}
			if Reason(err) == "" {
				t.Fatalf("log-only reason must be attached")
			}
			if err.Error() != ErrSessionUnavailable.Error() {
				t.Fatalf("error text %q leaks detail; want %q", err.Error(), ErrSessionUnavailable.Error())
			}
			texts = append(texts, err.Error())
		})
	}
	for _, text := range texts[1:] {
		if text != texts[0] {
			t.Fatalf("denial texts differ: %q vs %q", texts[0], text)
		}
	}
}

func TestResolveTargetCorruptIndexIsIO(t *testing.T) {
	setupHome(t)
	writeSessionEntries(t, "peer-a", []session.Entry{mkStart("peer-a", projectA)})
	if err := os.WriteFile(filepath.Join(sessionsDir(t), "session.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := New(Config{})
	target, err := svc.ResolveTarget(context.Background(), Caller{SessionID: "self-1", Project: projectA}, "peer-a")
	if target.File != nil {
		_ = target.File.Close()
	}
	if !errors.Is(err, ErrIO) {
		t.Fatalf("corrupt index must be ErrIO, got %v (reason=%s)", err, Reason(err))
	}
}

func TestResolveTargetPopulatesMetadata(t *testing.T) {
	setupHome(t)
	writeSessionEntries(t, "peer-a", []session.Entry{mkStart("peer-a", projectA), mkUser("hello")})
	meta := mkMeta("peer-a", projectA, "2026-10-18T10:00:00+08:00")
	meta.Title = "Fix the flaky test"
	meta.Archived = true
	meta.AutomationID = "auto-1"
	addIndexMeta(t, projectA, meta)
	svc := New(Config{})
	target, err := svc.ResolveTarget(context.Background(), Caller{SessionID: "self-1", Project: projectA}, "peer-a")
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	defer func() { _ = target.File.Close() }()
	ref := target.Ref
	if ref.Title != "Fix the flaky test" || !ref.Archived || !ref.Automation {
		t.Fatalf("ref=%+v", ref)
	}
	if ref.Provider != "test-provider" || ref.Model != "test-model" {
		t.Fatalf("provider/model ref=%+v", ref)
	}
	if ref.UpdatedAt != "2026-10-18T10:00:00+08:00" {
		t.Fatalf("UpdatedAt=%q", ref.UpdatedAt)
	}
	if target.Start.UUID != "peer-a" || target.Start.Project != projectA {
		t.Fatalf("start=%+v", target.Start)
	}
}

func TestResolveTargetRefusesUnvalidatedIndexPath(t *testing.T) {
	setupHome(t)
	// A canary just above the sessions directory that an unvalidated
	// "../../canary" id would open.
	canary := filepath.Join(filepath.Dir(filepath.Dir(sessionsDir(t))), "canary.json")
	if err := os.WriteFile(canary, []byte(`{"type":"user","content":"CANARY"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	addIndexMeta(t, projectA, mkMeta("../../canary", projectA, ""))
	svc := New(Config{})
	target, err := svc.ResolveTarget(context.Background(), Caller{SessionID: "self-1", Project: projectA}, "../../canary")
	if target.File != nil {
		_ = target.File.Close()
	}
	if !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("traversal id must be unavailable, got %v", err)
	}
	if Reason(err) != "invalid_id" {
		t.Fatalf("Reason=%q want invalid_id", Reason(err))
	}
}
