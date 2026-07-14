package model

import (
	"context"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/config"
)

// stubModel is a minimal ToolCallingChatModel for testing.
type stubModel struct{ id string }

func (s *stubModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	return &schema.Message{Content: s.id}, nil
}
func (s *stubModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}
func (s *stubModel) WithTools(_ []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return s, nil
}

func TestParseProviderModel(t *testing.T) {
	tests := []struct {
		input    string
		provider string
		model    string
		wantErr  bool
	}{
		{"openai/gpt-4o", "openai", "gpt-4o", false},
		{"anthropic/claude-3-opus-20240229", "anthropic", "claude-3-opus-20240229", false},
		{"deepseek/deepseek-chat", "deepseek", "deepseek-chat", false},
		{"provider/model/extra", "provider", "model/extra", false}, // SplitN(s,"/",2) keeps rest
		{"", "", "", true},
		{"noslash", "", "", true},
		{"/model", "", "", true},
		{"provider/", "", "", true},
		{"/", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p, m, err := ParseProviderModel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p != tt.provider {
				t.Errorf("provider: got %q, want %q", p, tt.provider)
			}
			if m != tt.model {
				t.Errorf("model: got %q, want %q", m, tt.model)
			}
		})
	}
}

func TestGetModel_EmptyReturnsDefault(t *testing.T) {
	fallback := &stubModel{id: "fallback"}
	f := NewModelFactory(&config.Config{
		Providers: map[string]*config.ProviderConfig{},
	}, fallback)

	m, err := f.GetModel(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != fallback {
		t.Error("expected fallback model for empty string")
	}
}

func TestGetModel_UnknownProvider(t *testing.T) {
	fallback := &stubModel{id: "fallback"}
	f := NewModelFactory(&config.Config{
		Providers: map[string]*config.ProviderConfig{
			"openai": {APIKey: "sk-test", Models: []string{"gpt-4o"}},
		},
	}, fallback)

	_, err := f.GetModel(context.Background(), "nonexistent/gpt-4o")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestGetModel_ModelNotInProvider(t *testing.T) {
	fallback := &stubModel{id: "fallback"}
	f := NewModelFactory(&config.Config{
		Providers: map[string]*config.ProviderConfig{
			"openai": {APIKey: "sk-test"},
		},
	}, fallback)

	// Model not in registry logs a warning but still attempts creation.
	// Without a real API, GetModel will fail at the HTTP level, not model validation.
	// Pre-populate cache to simulate success.
	cached := &stubModel{id: "gpt-nonexistent"}
	f.mu.Lock()
	f.cache["openai/gpt-nonexistent"] = cached
	f.mu.Unlock()

	m, err := f.GetModel(context.Background(), "openai/gpt-nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != cached {
		t.Error("expected cached model")
	}
}

func TestGetModel_EmptyModelsList(t *testing.T) {
	f := NewModelFactory(&config.Config{
		Providers: map[string]*config.ProviderConfig{
			"custom": {APIKey: "sk-test", BaseURL: "http://localhost:8080"},
		},
	}, &stubModel{id: "fallback"})

	// With no models list, any model should be accepted.
	// Pre-populate cache to avoid real API calls.
	cached := &stubModel{id: "any-model"}
	f.mu.Lock()
	f.cache["custom/any-model"] = cached
	f.mu.Unlock()

	m, err := f.GetModel(context.Background(), "custom/any-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != cached {
		t.Error("expected cached model")
	}
}

func TestGetModel_Caching(t *testing.T) {
	f := NewModelFactory(&config.Config{
		Providers: map[string]*config.ProviderConfig{
			"test": {APIKey: "sk-test", Models: []string{"model-a"}},
		},
	}, &stubModel{id: "fallback"})

	// Pre-populate cache to avoid real API calls
	cached := &stubModel{id: "cached"}
	f.mu.Lock()
	f.cache["test/model-a"] = cached
	f.mu.Unlock()

	m, err := f.GetModel(context.Background(), "test/model-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != cached {
		t.Error("expected cached model to be returned")
	}
}

func TestGetModel_SmallAlias(t *testing.T) {
	fallback := &stubModel{id: "fallback"}
	f := NewModelFactory(&config.Config{
		Model:      "test/model-main",
		SmallModel: "test/model-small",
		Providers: map[string]*config.ProviderConfig{
			"test": {APIKey: "sk-test"},
		},
	}, fallback)

	// Pre-populate cache for the resolved small model to avoid real API calls.
	cached := &stubModel{id: "small"}
	f.mu.Lock()
	f.cache["test/model-small"] = cached
	f.mu.Unlock()

	m, err := f.GetModel(context.Background(), SmallModelAlias)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != cached {
		t.Error("expected 'small' alias to resolve to the configured small model")
	}
}

func TestGetModel_SmallAliasUnsetFallsBack(t *testing.T) {
	fallback := &stubModel{id: "fallback"}
	f := NewModelFactory(&config.Config{
		Model:     "test/model-main",
		Providers: map[string]*config.ProviderConfig{},
	}, fallback)

	m, err := f.GetModel(context.Background(), SmallModelAlias)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != fallback {
		t.Error("expected 'small' alias without small_model to degrade to the session fallback")
	}
}

func TestGetModel_SmallAliasMalformedFallsBack(t *testing.T) {
	// A slash-less small_model (user typo) must degrade to the session model,
	// not hard-error every delegated task.
	fallback := &stubModel{id: "fallback"}
	f := NewModelFactory(&config.Config{
		Model:      "test/model-main",
		SmallModel: "gpt-4o-mini", // missing provider prefix
		Providers:  map[string]*config.ProviderConfig{},
	}, fallback)

	m, err := f.GetModel(context.Background(), SmallModelAlias)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != fallback {
		t.Error("malformed small_model must degrade the alias to the fallback")
	}
}

func TestBareModelID(t *testing.T) {
	if got := BareModelID("openai/gpt-4o"); got != "gpt-4o" {
		t.Errorf("got %q", got)
	}
	if got := BareModelID("provider/model/extra"); got != "model/extra" {
		t.Errorf("got %q", got)
	}
	if got := BareModelID("bare-id"); got != "bare-id" {
		t.Errorf("got %q", got)
	}
	if got := BareModelID(""); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestResolveRef(t *testing.T) {
	f := NewModelFactory(&config.Config{SmallModel: "test/model-small"}, nil)
	if got := f.ResolveRef(SmallModelAlias); got != "test/model-small" {
		t.Errorf("ResolveRef(small): got %q, want test/model-small", got)
	}
	if got := f.ResolveRef("openai/gpt-4o"); got != "openai/gpt-4o" {
		t.Errorf("ResolveRef(passthrough): got %q", got)
	}
	if got := f.ResolveRef(""); got != "" {
		t.Errorf("ResolveRef(empty): got %q, want empty", got)
	}

	unset := NewModelFactory(&config.Config{}, nil)
	if got := unset.ResolveRef(SmallModelAlias); got != "" {
		t.Errorf("ResolveRef(small, unset): got %q, want empty", got)
	}
}

func TestFallback(t *testing.T) {
	fallback := &stubModel{id: "fallback"}
	f := NewModelFactory(&config.Config{}, fallback)

	if f.Fallback() != fallback {
		t.Error("Fallback() should return the fallback model")
	}
}
