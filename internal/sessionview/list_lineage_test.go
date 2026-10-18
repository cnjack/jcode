package sessionview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/session"
)

func TestConfigDefaults(t *testing.T) {
	svc := New(Config{})
	if svc.cfg.MaxResults != 20 || svc.cfg.MaxEntryBytes != 4096 || svc.cfg.MaxOutputBytes != 16384 || svc.cfg.TimeBudget != 3*time.Second {
		t.Fatalf("defaults=%+v", svc.cfg)
	}
	svc = New(Config{MaxResults: 500, MaxEntryBytes: 10, MaxOutputBytes: 20, TimeBudget: time.Millisecond})
	if svc.cfg.MaxResults != 200 || svc.cfg.MaxEntryBytes != 10 || svc.cfg.MaxOutputBytes != 20 {
		t.Fatalf("configured=%+v", svc.cfg)
	}
}

func TestListIncludesSelfMarkedAndOrdering(t *testing.T) {
	setupHome(t)
	callerID := "caller-1"
	writeSessionEntries(t, callerID, []session.Entry{mkStart(callerID, projectA), mkUser("self")})
	writeSessionEntries(t, "older", []session.Entry{mkStart("older", projectA), mkUser("older")})
	writeSessionEntries(t, "newer", []session.Entry{mkStart("newer", projectA), mkUser("newer")})
	addIndexMeta(t, projectA,
		mkMeta(callerID, projectA, "2026-10-18T09:00:00+08:00"),
		mkMeta("older", projectA, "2026-10-17T09:00:00+08:00"),
		mkMeta("newer", projectA, "2026-10-18T12:00:00+08:00"),
	)
	svc := New(Config{})
	res, err := svc.List(context.Background(), Caller{SessionID: callerID, Project: projectA}, ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if res.Eligible != 3 || res.Shown != 3 || res.Capped {
		t.Fatalf("res=%+v", res)
	}
	got := []string{res.Sessions[0].ID, res.Sessions[1].ID, res.Sessions[2].ID}
	want := []string{"newer", callerID, "older"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order=%v want %v", got, want)
		}
	}
	if !res.Sessions[1].IsCaller {
		t.Fatalf("self must be marked: %+v", res.Sessions)
	}
	if res.Sessions[0].IsCaller || res.Sessions[2].IsCaller {
		t.Fatalf("only self may be marked")
	}
}

func TestListCapAndClamp(t *testing.T) {
	setupHome(t)
	const n = 8
	for i := 0; i < n; i++ {
		id := "session-" + string(rune('a'+i))
		writeSessionEntries(t, id, []session.Entry{mkStart(id, projectA)})
		addIndexMeta(t, projectA, mkMeta(id, projectA, "2026-10-18T09:00:0"+string(rune('0'+i))+"+08:00"))
	}
	svc := New(Config{})
	res, err := svc.List(context.Background(), Caller{SessionID: "session-a", Project: projectA}, ListOptions{Limit: 3})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if res.Shown != 3 || !res.Capped || res.Eligible != n {
		t.Fatalf("res=%+v", res)
	}
	res, err = svc.List(context.Background(), Caller{SessionID: "session-a", Project: projectA}, ListOptions{Limit: 999})
	if err != nil {
		t.Fatalf("List clamps limit: %v", err)
	}
	if res.Shown != n {
		t.Fatalf("shown=%d want %d", res.Shown, n)
	}
}

func TestListArchived(t *testing.T) {
	setupHome(t)
	archived := mkMeta("archived", projectA, "2026-10-18T09:00:00+08:00")
	archived.Archived = true
	writeSessionEntries(t, "archived", []session.Entry{mkStart("archived", projectA)})
	writeSessionEntries(t, "live", []session.Entry{mkStart("live", projectA)})
	addIndexMeta(t, projectA, archived, mkMeta("live", projectA, "2026-10-17T09:00:00+08:00"))
	svc := New(Config{})
	caller := Caller{SessionID: "live", Project: projectA}
	res, err := svc.List(context.Background(), caller, ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if res.Eligible != 1 || res.Sessions[0].ID != "live" {
		t.Fatalf("archived must be hidden: %+v", res)
	}
	res, err = svc.List(context.Background(), caller, ListOptions{IncludeArchived: true})
	if err != nil {
		t.Fatalf("List archived: %v", err)
	}
	if res.Eligible != 2 {
		t.Fatalf("archived must be included: %+v", res)
	}
}

func TestListRequiresCallerAndWorkspace(t *testing.T) {
	setupHome(t)
	svc := New(Config{})
	if _, err := svc.List(context.Background(), Caller{}, ListOptions{}); !errors.Is(err, ErrNoCaller) {
		t.Fatalf("want ErrNoCaller, got %v", err)
	}
	if _, err := svc.List(context.Background(), Caller{SessionID: "x"}, ListOptions{}); !errors.Is(err, ErrNoWorkspace) {
		t.Fatalf("want ErrNoWorkspace, got %v", err)
	}
}

func TestListSkipsInvalidIndexUUID(t *testing.T) {
	setupHome(t)
	writeSessionEntries(t, "good", []session.Entry{mkStart("good", projectA)})
	addIndexMeta(t, projectA,
		mkMeta("good", projectA, ""),
		mkMeta("../escape", projectA, ""),
		mkMeta("", projectA, ""),
	)
	svc := New(Config{})
	res, err := svc.List(context.Background(), Caller{SessionID: "good", Project: projectA}, ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if res.Eligible != 1 || res.Sessions[0].ID != "good" {
		t.Fatalf("invalid index ids must be skipped: %+v", res)
	}
}

func TestLineageChildren(t *testing.T) {
	setupHome(t)
	writeSessionEntries(t, "parent-1", []session.Entry{mkStart("parent-1", projectA)})
	addIndexMeta(t, projectA, mkMeta("parent-1", projectA, ""))
	sub := filepath.Join(sessionsDir(t), "parent-1", "subagents")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"agent-one.jsonl", "agent-two.jsonl", "notes.txt", "agent-three.json"} {
		if err := os.WriteFile(filepath.Join(sub, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(sub, "agent-dir.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	svc := New(Config{})
	res, err := svc.List(context.Background(), Caller{SessionID: "other", Project: projectA}, ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Sessions) != 1 || res.Sessions[0].SubagentTranscripts != 2 {
		t.Fatalf("transcripts=%+v", res.Sessions)
	}
}

func TestLineageInvalidIDDoesNotTouchFilesystem(t *testing.T) {
	setupHome(t)
	svc := New(Config{})
	if n := svc.countSubagentTranscripts("../../etc"); n != 0 {
		t.Fatalf("invalid id must not enumerate: %d", n)
	}
	if n := svc.countSubagentTranscripts(""); n != 0 {
		t.Fatalf("empty id must not enumerate: %d", n)
	}
}
