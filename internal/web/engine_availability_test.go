package web

import (
	"errors"
	"testing"

	"github.com/cloudwego/eino/adk"
)

func TestEnsureAgentAvailableRetriesAfterProviderRecovery(t *testing.T) {
	wantErr := errors.New("provider needs reauthentication")
	recovered := &adk.ChatModelAgent{}
	attempts := 0
	eng := newEngine(&EngineConfig{
		ProviderName: "xai",
		ModelName:    "grok-4.6",
		CreateAgent: func(provider, model string) (*adk.ChatModelAgent, error) {
			attempts++
			if provider != "xai" || model != "grok-4.6" {
				t.Fatalf("create agent called with %s/%s", provider, model)
			}
			if attempts == 1 {
				return nil, wantErr
			}
			return recovered, nil
		},
	})

	if err := eng.ensureAgentAvailable(); !errors.Is(err, wantErr) {
		t.Fatalf("first recovery error = %v, want %v", err, wantErr)
	}
	if err := eng.ensureAgentAvailable(); err != nil {
		t.Fatalf("second recovery: %v", err)
	}
	if eng.agent != recovered {
		t.Fatalf("recovered agent = %p, want %p", eng.agent, recovered)
	}
	if err := eng.ensureAgentAvailable(); err != nil {
		t.Fatalf("available agent check: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("create agent attempts = %d, want 2", attempts)
	}
}
