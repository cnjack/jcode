package imagegen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const tokenPlanMultimodalPath = "/api/v1/services/aigc/multimodal-generation/generation"

// TokenPlanMultimodalClient implements Alibaba Token Plan's synchronous image
// endpoint. Token Plan uses the DashScope-style messages schema on a
// plan-specific host; it is not an OpenAI Images endpoint and must not send the
// X-DashScope-Async header used by the general asynchronous task API.
type TokenPlanMultimodalClient struct {
	endpoint   *url.URL
	apiKey     string
	headers    map[string]string
	model      string
	httpClient *http.Client
	resolver   *Client
}

// NewTokenPlanMultimodalClient validates the endpoint eagerly so an invalid
// route never enters the agent's tool catalog.
func NewTokenPlanMultimodalClient(cfg ClientConfig) (*TokenPlanMultimodalClient, error) {
	if cfg.Protocol != ProtocolTokenPlanMultimodal {
		return nil, fmt.Errorf("unsupported image protocol %q", cfg.Protocol)
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("image model is required")
	}
	endpoint, err := tokenPlanMultimodalEndpoint(cfg.BaseURL, cfg.AllowInsecureHTTP)
	if err != nil {
		return nil, err
	}
	assetHosts, err := validateAssetHosts(cfg.AssetHosts)
	if err != nil {
		return nil, err
	}
	headers := make(map[string]string, len(cfg.Headers))
	for name, value := range cfg.Headers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if forbiddenProviderHeader(name) || strings.EqualFold(name, "X-DashScope-Async") {
			return nil, fmt.Errorf("image provider header %q is not allowed", name)
		}
		headers[name] = value
	}
	httpClient := cloneHTTPClient(cfg.HTTPClient)
	maxImageBytes := cfg.MaxImageSize
	if maxImageBytes <= 0 {
		maxImageBytes = defaultMaxImageBytes
	}
	// Reuse the hardened signed-asset downloader and image validator. It never
	// forwards provider credentials to the returned OSS URL.
	resolver := &Client{
		endpoint: endpoint, httpClient: httpClient, maxImageBytes: maxImageBytes,
		allowHTTP: cfg.AllowInsecureHTTP, assetHosts: assetHosts,
	}
	return &TokenPlanMultimodalClient{
		endpoint: endpoint, apiKey: cfg.APIKey, headers: headers,
		model: strings.TrimSpace(cfg.Model), httpClient: httpClient, resolver: resolver,
	}, nil
}

type tokenPlanMultimodalRequest struct {
	Model string `json:"model"`
	Input struct {
		Messages []tokenPlanMessage `json:"messages"`
	} `json:"input"`
	Parameters struct {
		Size string `json:"size,omitempty"`
		N    int    `json:"n"`
	} `json:"parameters"`
}

type tokenPlanMessage struct {
	Role    string             `json:"role"`
	Content []tokenPlanContent `json:"content"`
}

type tokenPlanContent struct {
	Text string `json:"text,omitempty"`
}

type tokenPlanMultimodalResponse struct {
	Code   string `json:"code"`
	Output struct {
		Choices []struct {
			Message struct {
				Content []struct {
					Type  string `json:"type"`
					Image string `json:"image"`
				} `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	} `json:"output"`
}

// Generate performs exactly one synchronous, billable upstream request and
// requires exactly one image result. There is intentionally no application
// retry: retrying an ambiguous POST could create and charge for another image.
func (c *TokenPlanMultimodalClient) Generate(ctx context.Context, input Request) (Result, error) {
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return Result{}, fmt.Errorf("image prompt is required")
	}
	if len(prompt) > maxPromptBytes || utf8.RuneCountInString(prompt) > 5000 {
		return Result{}, fmt.Errorf("image prompt exceeds 5000 characters")
	}
	if input.Count != 0 && input.Count != 1 {
		return Result{}, fmt.Errorf("P0 image generation supports exactly one image")
	}
	if strings.TrimSpace(input.Quality) != "" || strings.TrimSpace(input.Background) != "" ||
		strings.TrimSpace(input.OutputFormat) != "" || strings.TrimSpace(input.ResponseFormat) != "" {
		return Result{}, fmt.Errorf("image request contains options unsupported by Token Plan")
	}
	size, err := normalizeTokenPlanSize(input.Size)
	if err != nil {
		return Result{}, err
	}

	body := tokenPlanMultimodalRequest{Model: c.model}
	body.Input.Messages = []tokenPlanMessage{{
		Role: "user", Content: []tokenPlanContent{{Text: prompt}},
	}}
	body.Parameters.Size = size
	body.Parameters.N = 1
	encoded, err := json.Marshal(body)
	if err != nil {
		return Result{}, fmt.Errorf("encode image request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return Result{}, fmt.Errorf("create image request: %w", err)
	}
	for name, value := range c.headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{}, ctxErr
		}
		if IsContextError(err) {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("image provider request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Result{}, providerHTTPError(resp)
	}
	responseBody, err := readLimited(resp.Body, maxResponseBytes, "image provider response")
	if err != nil {
		return Result{}, err
	}
	var decoded tokenPlanMultimodalResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return Result{}, fmt.Errorf("decode image provider response: %w", err)
	}
	if decoded.Code != "" {
		return Result{}, fmt.Errorf("image provider request failed")
	}
	urls := make([]string, 0, 1)
	for _, choice := range decoded.Output.Choices {
		for _, content := range choice.Message.Content {
			if content.Image == "" {
				continue
			}
			if content.Type != "" && content.Type != "image" {
				return Result{}, fmt.Errorf("image provider returned invalid image content")
			}
			urls = append(urls, content.Image)
		}
	}
	if len(urls) == 0 {
		return Result{}, fmt.Errorf("image provider returned no images")
	}
	if len(urls) != 1 {
		return Result{}, fmt.Errorf("image provider returned %d images; P0 requires exactly one", len(urls))
	}
	generated, err := c.resolver.resolveImage(ctx, urls[0], "")
	if err != nil {
		return Result{}, fmt.Errorf("validate generated image 1: %w", err)
	}
	return Result{Images: []Image{generated}}, nil
}

func tokenPlanMultimodalEndpoint(raw string, allowHTTP bool) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("image provider base URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid image provider base URL")
	}
	if u.Scheme != "https" && (!allowHTTP || u.Scheme != "http") {
		return nil, fmt.Errorf("image provider base URL must use HTTPS")
	}
	if u.User != nil {
		return nil, fmt.Errorf("image provider base URL must not contain user info")
	}
	if !asciiHost(u.Hostname()) {
		return nil, fmt.Errorf("image provider base URL host must use canonical ASCII")
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path != tokenPlanMultimodalPath {
		return nil, fmt.Errorf("token plan image endpoint must end with %s", tokenPlanMultimodalPath)
	}
	return u, nil
}

func normalizeTokenPlanSize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	switch strings.ToUpper(raw) {
	case "1K", "2K", "4K":
		return strings.ToUpper(raw), nil
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(raw, "X", "*"), "x", "*")
	widthText, heightText, ok := strings.Cut(normalized, "*")
	if !ok || strings.Contains(heightText, "*") {
		return "", fmt.Errorf("invalid Token Plan image size")
	}
	width, widthErr := strconv.Atoi(widthText)
	height, heightErr := strconv.Atoi(heightText)
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return "", fmt.Errorf("invalid Token Plan image size")
	}
	return strconv.Itoa(width) + "*" + strconv.Itoa(height), nil
}
