package imagegen_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/imagegen"
)

// TestLiveBigModelOpenAIImages is an opt-in POC smoke test. It intentionally
// reads the user's existing provider configuration so credentials never appear
// in command arguments, test fixtures, logs, or the repository.
func TestLiveBigModelOpenAIImages(t *testing.T) {
	if os.Getenv("JCODE_LIVE_BIGMODEL_IMAGE") != "1" {
		t.Skip("set JCODE_LIVE_BIGMODEL_IMAGE=1 for a billable provider smoke test")
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	provider := cfg.GetProviders()["zhipuai-coding-plan"]
	if provider == nil || provider.APIKey == "" || provider.BaseURL == "" {
		t.Skip("zhipuai-coding-plan credentials are not configured")
	}
	client, err := imagegen.NewClient(imagegen.ClientConfig{
		Protocol: imagegen.ProtocolOpenAIImages,
		BaseURL:  provider.BaseURL,
		APIKey:   provider.APIKey,
		Headers:  provider.Headers,
		Model:    "cogview-3-flash",
		AssetHosts: []string{
			"*.bigmodel.cn", "*.chatglm.cn",
		},
	})
	if err != nil {
		t.Fatalf("create image client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	result, err := client.Generate(ctx, imagegen.Request{
		Prompt: "A single blue ceramic cup on a plain warm-gray studio background, product photo, no text",
		Size:   "1024x1024",
		Count:  1,
	})
	if err != nil {
		t.Fatalf("generate image: %v", err)
	}
	if len(result.Images) != 1 {
		t.Fatalf("image count = %d, want 1", len(result.Images))
	}
	got := result.Images[0]
	if got.Width <= 0 || got.Height <= 0 || got.MIMEType == "" || len(got.Data) == 0 {
		t.Fatalf("invalid image metadata: mime=%q size=%dx%d bytes=%d", got.MIMEType, got.Width, got.Height, len(got.Data))
	}
	t.Logf("live image validated: mime=%s size=%dx%d bytes=%d", got.MIMEType, got.Width, got.Height, len(got.Data))
}
