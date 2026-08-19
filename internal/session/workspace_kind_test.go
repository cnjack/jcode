package session

import "testing"

func TestRecorderPersistsScratchWorkspaceKind(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	recorder, err := NewRecorder(project, "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	recorder.SetWorkspaceKind(WorkspaceScratch)
	recorder.RecordUser("scratch task")
	id := recorder.UUID()
	recorder.Close()

	metas, err := ListSessions(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].WorkspaceKind != WorkspaceScratch {
		t.Fatalf("scratch session metadata not persisted: %+v", metas)
	}
	entries, err := LoadSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 || entries[0].WorkspaceKind != WorkspaceScratch {
		t.Fatalf("scratch session header not persisted: %+v", entries)
	}
	projects, err := ListProjectMeta()
	if err != nil {
		t.Fatal(err)
	}
	if projects[project].WorkspaceKind != WorkspaceScratch {
		t.Fatalf("scratch project metadata not persisted: %+v", projects[project])
	}
}

func TestNormalizeWorkspaceKindDefaultsLegacyToProject(t *testing.T) {
	if got := NormalizeWorkspaceKind(""); got != WorkspaceProject {
		t.Fatalf("legacy empty kind = %q, want project", got)
	}
	if got := NormalizeWorkspaceKind("unknown"); got != WorkspaceProject {
		t.Fatalf("unknown kind = %q, want project", got)
	}
}
