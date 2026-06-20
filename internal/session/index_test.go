package session

import "testing"

// TestRecorderIndexingRequiresContent locks the contract the web server's
// todo/goal OnUpdate guard relies on: a recorder that has written nothing is
// NOT listed (so ambient todo/goal updates, which the server now skips while
// HasRecording() is false, can never create a phantom empty session), and the
// session only appears once a real user message has been recorded.
func TestRecorderIndexingRequiresContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const project = "/proj/elves"

	rec, err := NewRecorder(project, "zhipu", "glm-5.2")
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	// A fresh recorder has no file and must not be indexed.
	if rec.HasRecording() {
		t.Fatal("fresh recorder should report HasRecording() == false")
	}
	if metas, _ := ListSessions(project); len(metas) != 0 {
		t.Fatalf("a recorder that wrote nothing must not be indexed; got %d sessions", len(metas))
	}

	// The first user message creates the file and indexes the session.
	rec.RecordUser("hello")
	if !rec.HasRecording() {
		t.Fatal("recorder should report HasRecording() == true after a user message")
	}
	metas, err := ListSessions(project)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(metas) != 1 || metas[0].UUID != rec.UUID() {
		t.Fatalf("expected the session indexed after a user message, got %+v", metas)
	}
	rec.Close()
}
