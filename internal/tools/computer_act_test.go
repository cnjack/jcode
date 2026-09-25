package tools

import "testing"

func TestParseComputerActStepsPropagatesTopLevelApp(t *testing.T) {
	steps, err := parseComputerActSteps(`{"app":"com.apple.calculator","steps":[{"action":"type","text":"1+1"},{"action":"press","key":"return","app":"com.apple.Notes"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(steps))
	}
	if steps[0].App != "com.apple.calculator" {
		t.Errorf("step 0 should inherit the top-level app, got %q", steps[0].App)
	}
	if steps[1].App != "com.apple.Notes" {
		t.Errorf("a step's own app must win over the top-level one, got %q", steps[1].App)
	}

	single, err := parseComputerActSteps(`{"action":"click","uid":"e3","app":"com.apple.calculator"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(single) != 1 || single[0].App != "com.apple.calculator" || single[0].UID != "e3" {
		t.Errorf("single-action form lost fields: %+v", single)
	}

	if _, err := parseComputerActSteps(`{"action":"click","steps":[{"action":"click"}]}`); err == nil {
		t.Error("both forms at once must be rejected")
	}
	if _, err := parseComputerActSteps(`{}`); err == nil {
		t.Error("an empty request must be rejected")
	}
}
