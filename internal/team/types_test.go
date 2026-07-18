package team

import "testing"

func TestNormalizeAgentType(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "missing defaults general", want: AgentTypeGeneral},
		{name: "whitespace defaults general", input: "  ", want: AgentTypeGeneral},
		{name: "explore", input: AgentTypeExplore, want: AgentTypeExplore},
		{name: "general", input: AgentTypeGeneral, want: AgentTypeGeneral},
		{name: "coder", input: AgentTypeCoder, want: AgentTypeCoder},
		{name: "trimmed", input: "  coder\t", want: AgentTypeCoder},
		{name: "unknown", input: "writer", wantErr: true},
		{name: "wrong case", input: "GENERAL", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeAgentType(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeAgentType(%q) error = %v, wantErr=%v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("NormalizeAgentType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizePermission(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "missing defaults normal", want: PermissionNormal},
		{name: "whitespace defaults normal", input: "  ", want: PermissionNormal},
		{name: "normal", input: PermissionNormal, want: PermissionNormal},
		{name: "plan", input: PermissionPlan, want: PermissionPlan},
		{name: "auto", input: PermissionAuto, want: PermissionAuto},
		{name: "trimmed", input: "  plan\t", want: PermissionPlan},
		{name: "unknown", input: "unsafe", wantErr: true},
		{name: "wrong case", input: "AUTO", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizePermission(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizePermission(%q) error = %v, wantErr=%v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("NormalizePermission(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
