// Package imagegen provides provider-neutral image generation primitives.
package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // Register JPEG decoding for provider-response validation.
	_ "image/png"  // Register PNG decoding for provider-response validation.
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout       = 3 * time.Minute
	defaultMaxImageBytes = int64(25 << 20)
	maxResponseBytes     = int64(2 << 20)
	maxErrorBytes        = int64(16 << 10)
	maxPromptBytes       = 64 << 10
)

// Protocol identifies the upstream image-generation wire protocol.
type Protocol string

const (
	// ProtocolOpenAIImages is the POST /images/generations JSON protocol used
	// by OpenAI and compatible providers such as BigModel.
	ProtocolOpenAIImages Protocol = "openai_images"
	// ProtocolTokenPlanMultimodal is the synchronous multimodal-generation
	// protocol exposed by Alibaba Token Plan. It is deliberately distinct from
	// both OpenAI Images and the general DashScope asynchronous task API.
	ProtocolTokenPlanMultimodal Protocol = "token_plan_multimodal"
)

// ClientConfig describes one concrete image-generation endpoint. Secrets are
// deliberately kept out of result and error types so callers can safely
// serialize those values into session records.
type ClientConfig struct {
	Protocol Protocol
	BaseURL  string
	APIKey   string
	Headers  map[string]string
	Model    string
	// AssetHosts explicitly allows temporary image URL hosts in addition to the
	// provider API host. Values are exact hosts or "*.example.com" wildcards.
	AssetHosts []string

	HTTPClient   *http.Client
	MaxImageSize int64

	// AllowInsecureHTTP exists for local tests and development endpoints. Real
	// provider configuration must leave it false.
	AllowInsecureHTTP bool
}

// Request is the provider-neutral subset supported by the POC protocol.
// Empty optional values are omitted so older OpenAI-compatible gateways do not
// reject newer fields they do not understand.
type Request struct {
	Prompt         string
	Size           string
	Quality        string
	Background     string
	OutputFormat   string
	ResponseFormat string
	Count          int
}

// Image is validated binary output. SourceURL is retained only as provenance;
// callers should persist Data in managed local storage instead of depending on
// the provider's temporary URL.
type Image struct {
	Data          []byte
	MIMEType      string
	Width         int
	Height        int
	SourceURL     string
	RevisedPrompt string
}

// Result contains all images returned by one billable upstream invocation.
type Result struct {
	Images []Image
}

// Generator is the seam used by the generate_image tool and provider adapters.
type Generator interface {
	Generate(context.Context, Request) (Result, error)
}

// Client implements the OpenAI-compatible image generation protocol.
type Client struct {
	endpoint      *url.URL
	apiKey        string
	headers       map[string]string
	model         string
	httpClient    *http.Client
	maxImageBytes int64
	allowHTTP     bool
	assetHosts    []string
}

// NewGenerator constructs the exact protocol adapter selected by provider
// capability resolution. Callers must not infer a protocol from a provider or
// model name.
func NewGenerator(cfg ClientConfig) (Generator, error) {
	switch cfg.Protocol {
	case ProtocolOpenAIImages:
		return NewClient(cfg)
	case ProtocolTokenPlanMultimodal:
		return NewTokenPlanMultimodalClient(cfg)
	default:
		return nil, fmt.Errorf("unsupported image protocol %q", cfg.Protocol)
	}
}

// NewClient validates config eagerly so a misconfigured image provider can be
// omitted from the agent tool list instead of failing only after a tool call.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.Protocol != ProtocolOpenAIImages {
		return nil, fmt.Errorf("unsupported image protocol %q", cfg.Protocol)
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("image model is required")
	}
	endpoint, err := imagesEndpoint(cfg.BaseURL, cfg.AllowInsecureHTTP)
	if err != nil {
		return nil, err
	}
	httpClient := cloneHTTPClient(cfg.HTTPClient)
	assetHosts, err := validateAssetHosts(cfg.AssetHosts)
	if err != nil {
		return nil, err
	}
	maxImageBytes := cfg.MaxImageSize
	if maxImageBytes <= 0 {
		maxImageBytes = defaultMaxImageBytes
	}
	headers := make(map[string]string, len(cfg.Headers))
	for name, value := range cfg.Headers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if forbiddenProviderHeader(name) {
			return nil, fmt.Errorf("image provider header %q is not allowed", name)
		}
		headers[name] = value
	}
	return &Client{
		endpoint: endpoint, apiKey: cfg.APIKey, headers: headers,
		model: strings.TrimSpace(cfg.Model), httpClient: httpClient,
		maxImageBytes: maxImageBytes, allowHTTP: cfg.AllowInsecureHTTP,
		assetHosts: assetHosts,
	}, nil
}

type openAIRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	Background     string `json:"background,omitempty"`
	OutputFormat   string `json:"output_format,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	N              int    `json:"n,omitempty"`
}

type openAIResponse struct {
	Data []struct {
		URL           string `json:"url"`
		B64JSON       string `json:"b64_json"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
}

// Generate performs exactly one upstream image-generation request.
func (c *Client) Generate(ctx context.Context, input Request) (Result, error) {
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return Result{}, fmt.Errorf("image prompt is required")
	}
	if len(prompt) > maxPromptBytes {
		return Result{}, fmt.Errorf("image prompt exceeds %d bytes", maxPromptBytes)
	}
	if input.Count != 0 && input.Count != 1 {
		return Result{}, fmt.Errorf("P0 image generation supports exactly one image")
	}
	body, err := json.Marshal(openAIRequest{
		Model: c.model, Prompt: prompt, Size: strings.TrimSpace(input.Size),
		Quality: strings.TrimSpace(input.Quality), Background: strings.TrimSpace(input.Background),
		OutputFormat: strings.TrimSpace(input.OutputFormat), ResponseFormat: strings.TrimSpace(input.ResponseFormat),
		N: 1,
	})
	if err != nil {
		return Result{}, fmt.Errorf("encode image request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("create image request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for name, value := range c.headers {
		if strings.EqualFold(name, "Host") || strings.EqualFold(name, "Content-Length") {
			continue
		}
		req.Header.Set(name, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if IsContextError(err) || IsContextError(ctx.Err()) {
			return Result{}, ctx.Err()
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
	var decoded openAIResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return Result{}, fmt.Errorf("decode image provider response: %w", err)
	}
	if len(decoded.Data) == 0 {
		return Result{}, fmt.Errorf("image provider returned no images")
	}
	if len(decoded.Data) != 1 {
		return Result{}, fmt.Errorf("image provider returned %d images; P0 requires exactly one", len(decoded.Data))
	}

	result := Result{Images: make([]Image, 0, len(decoded.Data))}
	for index, item := range decoded.Data {
		generated, err := c.resolveImage(ctx, item.URL, item.B64JSON)
		if err != nil {
			return Result{}, fmt.Errorf("validate generated image %d: %w", index+1, err)
		}
		generated.RevisedPrompt = item.RevisedPrompt
		result.Images = append(result.Images, generated)
	}
	return result, nil
}

func (c *Client) resolveImage(ctx context.Context, rawURL, encoded string) (Image, error) {
	var data []byte
	var err error
	switch {
	case encoded != "":
		data, err = decodeBase64Limited(encoded, c.maxImageBytes)
	case rawURL != "":
		data, err = c.download(ctx, rawURL)
	default:
		return Image{}, fmt.Errorf("response item contains neither url nor b64_json")
	}
	if err != nil {
		return Image{}, err
	}
	mimeType, width, height, err := inspectImage(data)
	if err != nil {
		return Image{}, err
	}
	return Image{Data: data, MIMEType: mimeType, Width: width, Height: height, SourceURL: rawURL}, nil
}

func (c *Client) download(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid generated image URL")
	}
	if u.Scheme != "https" && (!c.allowHTTP || u.Scheme != "http") {
		return nil, fmt.Errorf("generated image URL must use HTTPS")
	}
	if u.User != nil || !c.assetHostAllowed(u.Hostname()) {
		return nil, fmt.Errorf("generated image URL host is not allowed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create image download: %w", err)
	}
	req.Header.Set("Accept", "image/*")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if IsContextError(err) || IsContextError(ctx.Err()) {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("generated image download failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("download generated image: HTTP %d", resp.StatusCode)
	}
	return readLimited(resp.Body, c.maxImageBytes, "generated image")
}

func imagesEndpoint(baseURL string, allowHTTP bool) (*url.URL, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("image provider base URL is required")
	}
	u, err := url.Parse(baseURL)
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
	path := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(path, "/images/generations") {
		path += "/images/generations"
	}
	u.Path = path
	return u, nil
}

func providerHTTPError(resp *http.Response) error {
	// Drain a bounded amount for connection reuse, but never surface upstream
	// text: gateways may echo credentials, signed URLs, or the prompt.
	_, _ = readLimited(resp.Body, maxErrorBytes, "image provider error")
	return fmt.Errorf("image provider returned HTTP %d", resp.StatusCode)
}

func cloneHTTPClient(input *http.Client) *http.Client {
	if input != nil {
		copy := *input
		copy.Jar = nil
		copy.CheckRedirect = rejectRedirect
		if copy.Timeout <= 0 {
			copy.Timeout = defaultTimeout
		}
		return &copy
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.DialContext = safeDialContext
	return &http.Client{
		Transport: base, Timeout: defaultTimeout, CheckRedirect: rejectRedirect,
	}
}

func rejectRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("image provider network address is invalid")
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("image provider host resolution failed")
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("image provider did not resolve to a permitted public address")
	}
	// Reject the entire DNS answer when any A/AAAA record is not public. This
	// avoids selecting a public sibling from a mixed public/private response,
	// while dialing the already-validated IP below pins this connection against
	// a second DNS lookup. net/http still derives TLS ServerName from the
	// original request host rather than this pinned dial address.
	for _, candidate := range addresses {
		if !publicIP(candidate.IP) {
			return nil, fmt.Errorf("image provider did not resolve to a permitted public address")
		}
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	for _, candidate := range addresses {
		conn, dialErr := dialer.DialContext(
			ctx, network, net.JoinHostPort(candidate.IP.String(), port),
		)
		if dialErr == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("image provider did not resolve to a permitted public address")
}

func publicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range nonPublicDestinationPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// netip.Addr.IsGlobalUnicast intentionally includes several IANA
// special-purpose ranges (for example 100.64.0.0/10 and documentation
// networks). Provider requests may carry credentials, so image generation is
// stricter: only ordinarily routable public destinations are accepted.
var nonPublicDestinationPrefixes = []netip.Prefix{
	// IPv4 special-purpose and non-forwardable ranges.
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),

	// IPv6 local, translation, documentation, transition, and special-service
	// ranges. IPv4-mapped addresses are unwrapped before this check.
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func forbiddenProviderHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "cookie", "cookie2", "host",
		"content-length", "transfer-encoding", "connection":
		return true
	default:
		return false
	}
}

func validateAssetHosts(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
		if value == "" {
			continue
		}
		bare := strings.TrimPrefix(value, "*.")
		if strings.ContainsAny(bare, "/:@") || net.ParseIP(bare) != nil ||
			strings.HasPrefix(bare, ".") || !strings.Contains(bare, ".") || !asciiHost(bare) {
			return nil, fmt.Errorf("invalid generated image asset host %q", value)
		}
		result = append(result, value)
	}
	return result, nil
}

func (c *Client) assetHostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if !asciiHost(host) {
		return false
	}
	if host == strings.ToLower(strings.TrimSuffix(c.endpoint.Hostname(), ".")) {
		return true
	}
	for _, allowed := range c.assetHosts {
		if strings.HasPrefix(allowed, "*.") {
			suffix := strings.TrimPrefix(allowed, "*")
			prefix := strings.TrimSuffix(host, suffix)
			if prefix != host && prefix != "" && !strings.Contains(prefix, ".") {
				return true
			}
			continue
		}
		if host == allowed {
			return true
		}
	}
	return false
}

func asciiHost(host string) bool {
	if host == "" {
		return false
	}
	for _, char := range host {
		if char > 127 {
			return false
		}
	}
	return true
}

func readLimited(r io.Reader, limit int64, label string) ([]byte, error) {
	limited := io.LimitReader(r, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", label, limit)
	}
	return data, nil
}

func decodeBase64Limited(encoded string, limit int64) ([]byte, error) {
	if int64(base64.StdEncoding.DecodedLen(len(encoded))) > limit {
		return nil, fmt.Errorf("generated image exceeds %d bytes", limit)
	}
	decoded, err := io.ReadAll(io.LimitReader(base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded)), limit+1))
	if err != nil {
		return nil, fmt.Errorf("decode generated image: %w", err)
	}
	if int64(len(decoded)) > limit {
		return nil, fmt.Errorf("generated image exceeds %d bytes", limit)
	}
	return decoded, nil
}

func inspectImage(data []byte) (string, int, int, error) {
	if len(data) == 0 {
		return "", 0, 0, fmt.Errorf("generated image is empty")
	}
	if width, height, animated, ok := inspectGeneratedWebP(data); ok {
		if animated {
			return "", 0, 0, fmt.Errorf("animated generated images are not supported")
		}
		return "image/webp", width, height, nil
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, fmt.Errorf("unsupported or invalid generated image: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 {
		return "", 0, 0, fmt.Errorf("generated image has invalid dimensions")
	}
	var mimeType string
	switch format {
	case "png":
		mimeType = "image/png"
	case "jpeg":
		mimeType = "image/jpeg"
	default:
		return "", 0, 0, fmt.Errorf("unsupported generated image format %q", format)
	}
	return mimeType, config.Width, config.Height, nil
}

func inspectGeneratedWebP(data []byte) (int, int, bool, bool) {
	if len(data) < 20 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, false, false
	}
	if int64(binary.LittleEndian.Uint32(data[4:8]))+8 != int64(len(data)) {
		return 0, 0, false, false
	}
	var canvasWidth, canvasHeight, imageWidth, imageHeight int
	animated := false
	for offset := 12; offset+8 <= len(data); {
		kind := string(data[offset : offset+4])
		length := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start := offset + 8
		end := start + length
		if length < 0 || end < start || end > len(data) {
			return 0, 0, false, false
		}
		chunk := data[start:end]
		switch kind {
		case "VP8X":
			if len(chunk) < 10 {
				return 0, 0, false, false
			}
			canvasWidth = 1 + int(chunk[4]) + int(chunk[5])<<8 + int(chunk[6])<<16
			canvasHeight = 1 + int(chunk[7]) + int(chunk[8])<<8 + int(chunk[9])<<16
			animated = chunk[0]&0x02 != 0
		case "VP8 ":
			if len(chunk) < 10 || !bytes.Equal(chunk[3:6], []byte{0x9d, 0x01, 0x2a}) {
				return 0, 0, false, false
			}
			imageWidth = int(binary.LittleEndian.Uint16(chunk[6:8]) & 0x3fff)
			imageHeight = int(binary.LittleEndian.Uint16(chunk[8:10]) & 0x3fff)
		case "VP8L":
			if len(chunk) < 5 || chunk[0] != 0x2f {
				return 0, 0, false, false
			}
			imageWidth = 1 + int(chunk[1]) + (int(chunk[2]&0x3f) << 8)
			imageHeight = 1 + int(chunk[2]>>6) + (int(chunk[3]) << 2) + (int(chunk[4]&0x0f) << 10)
		}
		offset = end + length%2
		if offset == len(data) {
			break
		}
		if offset+8 > len(data) {
			return 0, 0, false, false
		}
	}
	if animated && canvasWidth > 0 && canvasHeight > 0 {
		return canvasWidth, canvasHeight, true, true
	}
	if imageWidth <= 0 || imageHeight <= 0 {
		return 0, 0, false, false
	}
	if canvasWidth > 0 || canvasHeight > 0 {
		if canvasWidth <= 0 || canvasHeight <= 0 || imageWidth > canvasWidth || imageHeight > canvasHeight {
			return 0, 0, false, false
		}
		return canvasWidth, canvasHeight, false, true
	}
	return imageWidth, imageHeight, false, true
}

// IsContextError lets the tool distinguish cancellation/deadline failures from
// provider errors without parsing strings.
func IsContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
