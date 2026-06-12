package command

import "testing"

func TestAvailableCommandList_IncludesGoal(t *testing.T) {
	cmds := availableCommandList(nil)
	if len(cmds) == 0 {
		t.Fatal("expected at least the goal command")
	}
	found := false
	for _, c := range cmds {
		if c.Name == "goal" {
			found = true
			if c.Input == nil || c.Input.Unstructured == nil {
				t.Error("goal command should advertise unstructured input")
			}
		}
	}
	if !found {
		t.Fatal("goal command not advertised in availableCommandList")
	}
}
