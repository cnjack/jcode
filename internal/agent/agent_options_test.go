package agent

import "testing"

func TestResolveAgentOptionsMaxIterations(t *testing.T) {
	tests := []struct {
		name    string
		options []AgentOption
		want    int
	}{
		{name: "default", want: defaultMaxIterations},
		{name: "explicit limit", options: []AgentOption{WithMaxIterations(40)}, want: 40},
		{name: "non-positive limit keeps default", options: []AgentOption{WithMaxIterations(0)}, want: defaultMaxIterations},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveAgentOptions(tt.options...).maxIterations; got != tt.want {
				t.Fatalf("max iterations = %d, want %d", got, tt.want)
			}
		})
	}
}
