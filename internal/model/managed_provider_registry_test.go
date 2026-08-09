package model

import "testing"

func TestManagedLoginProviderMetadata(t *testing.T) {
	registry := NewModelRegistry()
	tests := []struct {
		provider string
		methods  []string
		model    string
	}{
		{provider: "openai", methods: []string{"api_key", "codex_oauth"}},
		{provider: "xai", methods: []string{"api_key", "xai_oauth"}, model: "grok-4.5"},
		{provider: "github-copilot", methods: []string{"github_copilot"}, model: "gpt-4.1"},
	}
	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			provider := registry.GetProvider(test.provider)
			if provider == nil {
				t.Fatalf("provider %q missing", test.provider)
			}
			if len(provider.AuthMethods) != len(test.methods) {
				t.Fatalf("auth methods = %v, want %v", provider.AuthMethods, test.methods)
			}
			for index, method := range test.methods {
				if provider.AuthMethods[index] != method {
					t.Fatalf("auth methods = %v, want %v", provider.AuthMethods, test.methods)
				}
			}
			if test.model != "" {
				_, model, ok := registry.LookupModel(test.provider, test.model)
				if !ok || model == nil || !model.ToolCall || !model.DefaultEnabled {
					t.Fatalf("baseline model %q = %#v", test.model, model)
				}
			}
		})
	}
}

func TestRegistryCopiesAuthMethods(t *testing.T) {
	first := NewModelRegistry()
	provider := first.GetProvider("xai")
	provider.AuthMethods[0] = "mutated"

	second := NewModelRegistry()
	if got := second.GetProvider("xai").AuthMethods[0]; got != "api_key" {
		t.Fatalf("registry copy leaked auth-method mutation: %q", got)
	}
}
