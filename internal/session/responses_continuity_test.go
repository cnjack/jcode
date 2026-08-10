package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/model/responsemeta"
)

func TestRecordAssistantMessagePersistsOnlyEncryptedReasoningItem(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	opaque := json.RawMessage(`{"type":"reasoning","id":"rs-1","summary":[{"type":"summary_text","text":"clear summary secret"}],"content":[{"type":"reasoning_text","text":"clear chain secret"}],"encrypted_content":"ciphertext-only"}`)
	recorder.RecordAssistantMessage(&schema.Message{
		Role:             schema.Assistant,
		Content:          "visible answer",
		ReasoningContent: "runtime reasoning secret",
		Extra: map[string]any{
			responsemeta.OpaqueItemsExtraKey: []json.RawMessage{opaque},
			"arbitrary_secret":               "must not persist",
		},
	})
	id := recorder.UUID()
	recorder.Close()

	raw, err := os.ReadFile(filepath.Join(home, ".jcode", "sessions", id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{
		"clear summary secret", "clear chain secret", "runtime reasoning secret",
		"arbitrary_secret", "must not persist",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("session persisted forbidden cleartext %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "ciphertext-only") {
		t.Fatalf("session did not persist encrypted continuation: %s", text)
	}

	entries, err := LoadSession(id)
	if err != nil {
		t.Fatal(err)
	}
	state := ReconstructState(entries)
	if len(state.History) != 1 {
		t.Fatalf("history length = %d, want 1", len(state.History))
	}
	message := state.History[0]
	if message.Content != "visible answer" || message.ReasoningContent != "" {
		t.Fatalf("replayed message = %#v", message)
	}
	items := responsemeta.FromExtra(message.Extra)
	if len(items) != 1 || !strings.Contains(string(items[0]), "ciphertext-only") ||
		strings.Contains(string(items[0]), "clear summary secret") {
		t.Fatalf("replayed opaque items = %s", items)
	}
}

func TestRecordAssistantMessageDropsOrphanOpaqueReasoningOnRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	recorder.RecordUser("before")
	opaque := json.RawMessage(`{"type":"reasoning","id":"rs-orphan","summary":[],"encrypted_content":"cipher-orphan"}`)
	recorder.RecordAssistantMessage(&schema.Message{
		Role:  schema.Assistant,
		Extra: responsemeta.Extra([]json.RawMessage{opaque}),
	})
	id := recorder.UUID()
	recorder.Close()

	raw, err := os.ReadFile(filepath.Join(home, ".jcode", "sessions", id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "cipher-orphan") {
		t.Fatalf("session persisted orphan continuation: %s", raw)
	}
	entries, err := LoadSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if history := ReconstructState(entries).History; len(history) != 1 || history[0].Role != schema.User {
		t.Fatalf("restarted history = %#v, want only the preceding user turn", history)
	}
}

func TestReconstructStateDropsLegacyOrphanAndKeepsFunctionCallContinuation(t *testing.T) {
	orphan := json.RawMessage(`{"type":"reasoning","id":"rs-orphan","summary":[],"encrypted_content":"cipher-orphan"}`)
	follower := json.RawMessage(`{"type":"reasoning","id":"rs-follower","summary":[],"encrypted_content":"cipher-follower"}`)
	state := ReconstructState([]Entry{
		{Type: EntryAssistant, OpaqueResponseItems: []json.RawMessage{orphan}},
		{Type: EntryUser, Content: "separator"},
		{Type: EntryAssistant, OpaqueResponseItems: []json.RawMessage{follower}},
		{Type: EntryToolCall, Name: "lookup", Args: `{}`, ToolCallID: "call-1"},
	})

	var toolTurn *schema.Message
	for _, message := range state.History {
		for _, item := range responsemeta.FromExtra(message.Extra) {
			if strings.Contains(string(item), "cipher-orphan") {
				t.Fatalf("restart retained orphan continuation: %#v", state.History)
			}
		}
		if message.Role == schema.Assistant && len(message.ToolCalls) == 1 {
			toolTurn = message
		}
	}
	if toolTurn == nil {
		t.Fatalf("restart lost function-call turn: %#v", state.History)
	}
	items := responsemeta.FromExtra(toolTurn.Extra)
	if len(items) != 1 || !strings.Contains(string(items[0]), "cipher-follower") {
		t.Fatalf("function-call continuation = %s", items)
	}
}

func TestReconstructStateBoundsOpaqueItemsAndKeepsLegacyAssistant(t *testing.T) {
	items := make([]json.RawMessage, 0, responsemeta.MaxOpaqueItems+4)
	for i := 0; i < responsemeta.MaxOpaqueItems+4; i++ {
		items = append(items, json.RawMessage(`{"type":"reasoning","encrypted_content":"cipher"}`))
	}
	state := ReconstructState([]Entry{
		{Type: EntryAssistant, Content: "legacy"},
		{Type: EntryAssistant, Content: "bounded", OpaqueResponseItems: items},
	})
	if len(state.History) != 2 || state.History[0].Content != "legacy" || state.History[0].Extra != nil {
		t.Fatalf("legacy replay changed: %#v", state.History)
	}
	if got := len(responsemeta.FromExtra(state.History[1].Extra)); got != responsemeta.MaxOpaqueItems {
		t.Fatalf("opaque items = %d, want %d", got, responsemeta.MaxOpaqueItems)
	}
}
