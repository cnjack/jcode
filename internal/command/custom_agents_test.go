package command

import (
	"testing"

	"github.com/cnjack/jcode/internal/config"
)

func TestResolveCustomAgentModel(t *testing.T) {
	tests := []struct {
		name         string
		roleModel    string
		smallModel   string
		wantProvider string
		wantModel    string
		wantErr      bool
	}{
		{
			name:         "inherits current",
			wantProvider: "current", wantModel: "main",
		},
		{
			name: "role override", roleModel: "other/special",
			wantProvider: "other", wantModel: "special",
		},
		{
			name: "small alias", roleModel: "small", smallModel: "fast/mini",
			wantProvider: "fast", wantModel: "mini",
		},
		{
			name: "unset small inherits", roleModel: "small",
			wantProvider: "current", wantModel: "main",
		},
		{
			name: "invalid reference", roleModel: "missing-provider", wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotProvider, gotModel, err := resolveCustomAgentModel(
				config.AgentRoleConfig{Model: tt.roleModel},
				&config.Config{SmallModel: tt.smallModel},
				"current", "main",
			)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v, wantErr=%v", err, tt.wantErr)
			}
			if err == nil && (gotProvider != tt.wantProvider || gotModel != tt.wantModel) {
				t.Fatalf("got %s/%s, want %s/%s",
					gotProvider, gotModel, tt.wantProvider, tt.wantModel)
			}
		})
	}
}
