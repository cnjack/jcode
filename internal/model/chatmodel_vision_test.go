package model

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/config"
)

func TestSupportsImageInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		m    *RegistryModel
		want bool
	}{
		{"nil model", nil, false},
		{"nil modalities", &RegistryModel{ID: "x"}, false},
		{"text only", &RegistryModel{ID: "x", Modalities: &ModelModalities{Input: []string{"text"}}}, false},
		{"text+image", &RegistryModel{ID: "x", Modalities: &ModelModalities{Input: []string{"text", "image"}}}, true},
		{"video no image", &RegistryModel{ID: "x", Modalities: &ModelModalities{Input: []string{"text", "video"}}}, false},
	}
	for _, tc := range cases {
		if got := tc.m.SupportsImageInput(); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLookupStaticModel(t *testing.T) {
	t.Parallel()
	// glm-5.2 is merged into zhipuai at init() with text-only modalities.
	m := lookupStaticModel("zhipuai", "glm-5.2")
	if m == nil {
		t.Fatal("expected glm-5.2 in static registry")
	}
	if m.Modalities == nil {
		t.Fatal("expected modalities on glm-5.2")
	}
	if m.SupportsImageInput() {
		t.Error("glm-5.2 should not support image input")
	}
	if got := lookupStaticModel("no-such-provider", "x"); got != nil {
		t.Errorf("unknown provider: got %v, want nil", got)
	}
	if got := lookupStaticModel("zhipuai", "no-such-model"); got != nil {
		t.Errorf("unknown model: got %v, want nil", got)
	}
}

// TestVisionDerivation verifies NewChatModelFromProvider derives the vision
// flag from registry modalities when the provider config has no explicit
// override: a text-only registry model must strip image parts from requests
// instead of forwarding them to a provider that 400s on image content.
func TestVisionDerivation(t *testing.T) {
	t.Parallel()
	boolPtr := func(b bool) *bool { return &b }
	cases := []struct {
		name      string
		provider  string
		modelName string
		pc        *config.ProviderConfig
		wantStrip bool
	}{
		{
			name:      "text-only registry model strips images",
			provider:  "zhipuai",
			modelName: "glm-5.2",
			pc:        &config.ProviderConfig{APIKey: "k"},
			wantStrip: true,
		},
		{
			name:      "unknown model keeps vision default (images forwarded)",
			provider:  "custom-provider",
			modelName: "whatever",
			pc:        &config.ProviderConfig{APIKey: "k"},
			wantStrip: false,
		},
		{
			name:      "explicit vision override wins over registry",
			provider:  "zhipuai",
			modelName: "glm-5.2",
			pc:        &config.ProviderConfig{APIKey: "k", Vision: boolPtr(true)},
			wantStrip: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cm, err := NewChatModelFromProvider(context.Background(), tc.provider, tc.modelName, "http://127.0.0.1:1/v1", tc.pc)
			if err != nil {
				t.Fatalf("build model: %v", err)
			}
			m, ok := cm.(*chatModel)
			if !ok {
				t.Fatalf("unexpected model type %T", cm)
			}
			imgData := "aGk="
			req := m.buildRequest([]*schema.Message{{
				Role: schema.User,
				UserInputMultiContent: []schema.MessageInputPart{
					{Type: schema.ChatMessagePartTypeText, Text: "hello"},
					{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{
						MessagePartCommon: schema.MessagePartCommon{MIMEType: "image/png", Base64Data: &imgData},
					}},
				},
			}}, false)
			if len(req.Messages) != 1 {
				t.Fatalf("expected 1 message, got %d", len(req.Messages))
			}
			msg := req.Messages[0]
			stripped := len(msg.MultiContent) == 0
			if stripped != tc.wantStrip {
				t.Errorf("stripped=%v, want %v (MultiContent=%d, Content=%q)",
					stripped, tc.wantStrip, len(msg.MultiContent), msg.Content)
			}
			if stripped && msg.Content != "hello" {
				t.Errorf("stripped message should keep text, got %q", msg.Content)
			}
		})
	}
}
