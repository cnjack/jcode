package channel

import (
	"testing"
)

func TestState_String(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateNone, "none"},
		{StateDisabled, "disabled"},
		{StateEnabled, "enabled"},
		{State(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestState_Iota(t *testing.T) {
	if StateNone != 0 {
		t.Errorf("StateNone = %d, want 0", StateNone)
	}
	if StateDisabled != 1 {
		t.Errorf("StateDisabled = %d, want 1", StateDisabled)
	}
	if StateEnabled != 2 {
		t.Errorf("StateEnabled = %d, want 2", StateEnabled)
	}
}
