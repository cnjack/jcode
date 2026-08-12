package session

import "testing"

func TestRecorderSetProviderModelKeepsPairTogether(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := NewRecorder("/workspace", "old-provider", "old-model")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { recorder.Close() })

	recorder.SetProviderModel("xai", "grok-4.5")
	recorder.RecordUser("first turn")
	entries, err := LoadSession(recorder.UUID())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 || entries[0].Provider != "xai" || entries[0].Model != "grok-4.5" {
		t.Fatalf("session_start pair = %s/%s, want xai/grok-4.5", entries[0].Provider, entries[0].Model)
	}

	recorder.SetProviderModel("github-copilot", "gpt-4.1")
	meta, err := FindSessionMeta(recorder.UUID())
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil || meta.Provider != "github-copilot" || meta.Model != "gpt-4.1" {
		t.Fatalf("current index pair = %+v, want github-copilot/gpt-4.1", meta)
	}
}
