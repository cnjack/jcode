package tui

import (
	"testing"

	"github.com/cnjack/jcode/internal/session"
)

func TestConvertSessionEntriesShowsInitialAndChangedAgent(t *testing.T) {
	got := ConvertSessionEntries([]session.Entry{
		{Type: session.EntrySessionStart, Agent: "reviewer"},
		{Type: session.EntryAgentChange, Agent: ""},
	})
	if len(got) != 2 {
		t.Fatalf("entries=%+v", got)
	}
	if got[0].Type != string(session.EntryAgentChange) || got[0].Agent != "reviewer" {
		t.Fatalf("initial agent entry=%+v", got[0])
	}
	if got[1].Type != string(session.EntryAgentChange) || got[1].Agent != "" {
		t.Fatalf("changed agent entry=%+v", got[1])
	}
}
