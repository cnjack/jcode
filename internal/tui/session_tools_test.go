package tui

import "testing"

func TestSessionToolsCommandIsNotAdvertised(t *testing.T) {
	for _, command := range (Model{}).getAllCommands() {
		if command.cmd == "/tools" {
			t.Fatal("removed session tool override command is still advertised")
		}
	}
	if matches := filterCommands((Model{}).getAllCommands(), "/tools"); len(matches) != 0 {
		t.Fatalf("removed /tools command matched suggestions: %#v", matches)
	}
}
