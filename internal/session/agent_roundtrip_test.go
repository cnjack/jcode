package session

import "testing"

func TestReconstructStateAgentLastChangeWins(t *testing.T) {
	st := ReconstructState([]Entry{
		{Type: EntrySessionStart, Agent: "reviewer"},
		{Type: EntryAgentChange, Agent: "builder"},
		{Type: EntryAgentChange, Agent: ""},
	})
	if st.Agent != "" {
		t.Fatalf("Agent=%q, want Default", st.Agent)
	}
}

func TestRecorderPersistsAgentInStartAndIndex(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	rec, err := NewRecorder(project, "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	rec.SetAgent("reviewer")
	rec.RecordUser("hello")
	id := rec.UUID()
	rec.Close()

	entries, err := LoadSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 || entries[0].Type != EntrySessionStart || entries[0].Agent != "reviewer" {
		t.Fatalf("session start=%+v", entries)
	}
	metas, err := ListSessions(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].Agent != "reviewer" {
		t.Fatalf("metas=%+v", metas)
	}
}

func TestRecorderPersistsAgentChange(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	rec, err := NewRecorder(project, "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	rec.RecordUser("hello")
	rec.SetAgent("builder")
	id := rec.UUID()
	rec.Close()

	entries, err := LoadSession(id)
	if err != nil {
		t.Fatal(err)
	}
	st := ReconstructState(entries)
	if st.Agent != "builder" {
		t.Fatalf("Agent=%q, want builder", st.Agent)
	}
	metas, err := ListSessions(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].Agent != "builder" {
		t.Fatalf("metas=%+v", metas)
	}
}
