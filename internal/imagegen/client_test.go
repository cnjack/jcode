package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestOpenAIImagesBase64RoundTrip(t *testing.T) {
	pixels := pngBytes(t, 3, 2)
	httpClient := stubHTTPClient(t, func(r *http.Request) *http.Response {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "image-model" || body["prompt"] != "a quiet desk" {
			t.Fatalf("request = %#v", body)
		}
		return jsonResponse(http.StatusOK, map[string]any{"data": []map[string]string{{
			"b64_json": base64.StdEncoding.EncodeToString(pixels), "revised_prompt": "a revised quiet desk",
		}}})
	})

	client, err := NewClient(ClientConfig{
		Protocol: ProtocolOpenAIImages, BaseURL: "https://provider.example/v1", APIKey: "secret",
		Model: "image-model", HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Generate(context.Background(), Request{Prompt: "a quiet desk"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Images) != 1 {
		t.Fatalf("images = %d", len(result.Images))
	}
	got := result.Images[0]
	if got.MIMEType != "image/png" || got.Width != 3 || got.Height != 2 {
		t.Fatalf("image metadata = %s %dx%d", got.MIMEType, got.Width, got.Height)
	}
	if got.RevisedPrompt != "a revised quiet desk" {
		t.Fatalf("revised prompt = %q", got.RevisedPrompt)
	}
}

func TestOpenAIImagesURLRoundTripDoesNotForwardSecrets(t *testing.T) {
	pixels := pngBytes(t, 1, 1)
	var downloadAuth, downloadCustom string
	httpClient := stubHTTPClient(t, func(r *http.Request) *http.Response {
		switch r.URL.Path {
		case "/v1/images/generations":
			return jsonResponse(http.StatusOK, map[string]any{"data": []map[string]string{{
				"url": "https://cdn.example/signed/generated.png",
			}}})
		case "/signed/generated.png":
			downloadAuth = r.Header.Get("Authorization")
			downloadCustom = r.Header.Get("X-Provider-Secret")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/octet-stream"}},
				Body:       io.NopCloser(bytes.NewReader(pixels)),
			}
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
			return nil
		}
	})

	client, err := NewClient(ClientConfig{
		Protocol: ProtocolOpenAIImages, BaseURL: "https://provider.example/v1", APIKey: "secret",
		Headers: map[string]string{"X-Provider-Secret": "value"}, Model: "image-model",
		HTTPClient: httpClient, AssetHosts: []string{"cdn.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Generate(context.Background(), Request{Prompt: "pixel"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Images) != 1 || result.Images[0].MIMEType != "image/png" {
		t.Fatalf("result = %#v", result)
	}
	if downloadAuth != "" || downloadCustom != "" {
		t.Fatalf("provider secrets leaked to download: auth=%q custom=%q", downloadAuth, downloadCustom)
	}
}

func TestOpenAIImagesRejectsOversizedAndInvalidOutputs(t *testing.T) {
	tests := []struct {
		name     string
		response map[string]any
		limit    int64
		want     string
	}{
		{name: "oversized", response: map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString([]byte("too large"))}}}, limit: 2, want: "exceeds 2 bytes"},
		{name: "not image", response: map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString([]byte("hello"))}}}, limit: 100, want: "unsupported or invalid"},
		{name: "empty data", response: map[string]any{"data": []any{}}, limit: 100, want: "returned no images"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			httpClient := stubHTTPClient(t, func(_ *http.Request) *http.Response {
				return jsonResponse(http.StatusOK, tc.response)
			})
			client, err := NewClient(ClientConfig{Protocol: ProtocolOpenAIImages, BaseURL: "https://provider.example/v1", Model: "m", MaxImageSize: tc.limit, HTTPClient: httpClient})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Generate(context.Background(), Request{Prompt: "test"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestInspectImageAcceptsStaticWebPAndRejectsGIF(t *testing.T) {
	webp := minimalLosslessWebP(3, 2)
	mimeType, width, height, err := inspectImage(webp)
	if err != nil || mimeType != "image/webp" || width != 3 || height != 2 {
		t.Fatalf("WebP metadata = %q %dx%d, err=%v", mimeType, width, height, err)
	}

	gifImage := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black})
	var encoded bytes.Buffer
	if err := gif.Encode(&encoded, gifImage, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := inspectImage(encoded.Bytes()); err == nil ||
		!strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("GIF was not rejected: %v", err)
	}
}

func minimalLosslessWebP(width, height int) []byte {
	payload := []byte{
		0x2f,
		byte((width - 1) & 0xff),
		byte(((width-1)>>8)&0x3f) | byte(((height-1)&0x03)<<6),
		byte(((height - 1) >> 2) & 0xff),
		byte(((height - 1) >> 10) & 0x0f),
	}
	chunkSize := 8 + len(payload) + len(payload)%2
	data := make([]byte, 12+chunkSize)
	copy(data[:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	copy(data[8:12], "WEBP")
	copy(data[12:16], "VP8L")
	binary.LittleEndian.PutUint32(data[16:20], uint32(len(payload)))
	copy(data[20:], payload)
	return data
}

func TestOpenAIImagesRedactsUnstructuredProviderError(t *testing.T) {
	httpClient := stubHTTPClient(t, func(_ *http.Request) *http.Response {
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("secret-bearing upstream dump"))}
	})
	client, err := NewClient(ClientConfig{Protocol: ProtocolOpenAIImages, BaseURL: "https://provider.example/v1", Model: "m", HTTPClient: httpClient})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Generate(context.Background(), Request{Prompt: "test"})
	if err == nil || err.Error() != "image provider returned HTTP 401" {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenAIImagesRedactsStructuredProviderError(t *testing.T) {
	httpClient := stubHTTPClient(t, func(_ *http.Request) *http.Response {
		return jsonResponse(http.StatusUnauthorized, map[string]any{"error": map[string]any{
			"message": "credential-canary and private prompt",
		}})
	})
	client, err := NewClient(ClientConfig{
		Protocol: ProtocolOpenAIImages, BaseURL: "https://provider.example/v1",
		Model: "m", HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Generate(context.Background(), Request{Prompt: "test"})
	if err == nil || err.Error() != "image provider returned HTTP 401" {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenAIImagesRejectsUnlistedAssetHostAndMultipleImages(t *testing.T) {
	tests := []struct {
		name     string
		response map[string]any
		want     string
	}{
		{
			name: "unlisted asset host",
			response: map[string]any{"data": []map[string]string{{
				"url": "https://untrusted.example/generated.png?secret=canary",
			}}},
			want: "host is not allowed",
		},
		{
			name: "multiple images",
			response: map[string]any{"data": []map[string]string{
				{"b64_json": base64.StdEncoding.EncodeToString(pngBytes(t, 1, 1))},
				{"b64_json": base64.StdEncoding.EncodeToString(pngBytes(t, 1, 1))},
			}},
			want: "P0 requires exactly one",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewClient(ClientConfig{
				Protocol: ProtocolOpenAIImages, BaseURL: "https://provider.example/v1",
				Model: "m", HTTPClient: stubHTTPClient(t, func(_ *http.Request) *http.Response {
					return jsonResponse(http.StatusOK, tc.response)
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Generate(context.Background(), Request{Prompt: "test"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestNewClientRejectsDangerousHeadersAndAssetHosts(t *testing.T) {
	for _, cfg := range []ClientConfig{
		{Protocol: ProtocolOpenAIImages, BaseURL: "https://example.com/v1", Model: "m", Headers: map[string]string{"Authorization": "override"}},
		{Protocol: ProtocolOpenAIImages, BaseURL: "https://example.com/v1", Model: "m", AssetHosts: []string{"localhost"}},
		{Protocol: ProtocolOpenAIImages, BaseURL: "https://example.com/v1", Model: "m", AssetHosts: []string{"127.0.0.1"}},
	} {
		if _, err := NewClient(cfg); err == nil {
			t.Fatalf("NewClient(%+v) succeeded", cfg)
		}
	}
}

func TestAssetHostWildcardMatchesExactlyOneLabel(t *testing.T) {
	client, err := NewClient(ClientConfig{
		Protocol: ProtocolOpenAIImages, BaseURL: "https://provider.example/v1",
		Model: "m", AssetHosts: []string{"*.assets.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !client.assetHostAllowed("one.assets.example") {
		t.Fatal("single-label wildcard did not match")
	}
	for _, host := range []string{"assets.example", "a.b.assets.example", "evilassets.example"} {
		if client.assetHostAllowed(host) {
			t.Fatalf("wildcard unexpectedly allowed %q", host)
		}
	}
}

func TestPublicIPRejectsSpecialPurposeDestinations(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"0.1.2.3",
		"10.1.2.3",
		"100.100.100.200",
		"127.0.0.1",
		"169.254.169.254",
		"172.16.0.1",
		"192.0.0.9",
		"192.0.2.1",
		"192.168.1.1",
		"198.18.0.1",
		"198.51.100.1",
		"203.0.113.1",
		"224.0.0.1",
		"240.0.0.1",
		"::1",
		"64:ff9b::a00:1",
		"100::1",
		"2001:db8::1",
		"2002::1",
		"fc00::1",
		"fe80::1",
		"ff02::1",
		"::ffff:100.100.100.200",
	} {
		if publicIP(net.ParseIP(raw)) {
			t.Errorf("publicIP(%q) = true", raw)
		}
	}
}

func TestPublicIPAllowsOrdinaryPublicDestinations(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"1.1.1.1",
		"8.8.8.8",
		"2606:4700:4700::1111",
		"2001:4860:4860::8888",
	} {
		if !publicIP(net.ParseIP(raw)) {
			t.Errorf("publicIP(%q) = false", raw)
		}
	}
}

func TestNewClientRequiresExplicitProtocolAndHTTPS(t *testing.T) {
	for _, cfg := range []ClientConfig{
		{Protocol: "", BaseURL: "https://example.com/v1", Model: "m"},
		{Protocol: ProtocolOpenAIImages, BaseURL: "http://example.com/v1", Model: "m"},
		{Protocol: ProtocolOpenAIImages, BaseURL: "https://example.com/v1", Model: ""},
	} {
		if _, err := NewClient(cfg); err == nil {
			t.Fatalf("NewClient(%+v) succeeded", cfg)
		}
	}
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 220, G: 180, B: 80, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func stubHTTPClient(t *testing.T, fn func(*http.Request) *http.Response) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := fn(req)
		if resp != nil {
			resp.Request = req
		}
		return resp, nil
	})}
}

func jsonResponse(status int, value any) *http.Response {
	body, _ := json.Marshal(value)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}
