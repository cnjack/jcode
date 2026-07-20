package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultCloudURL is the public jcloud orchestrator address.
const DefaultCloudURL = "https://cloud.j-code.net"

// Polling defaults per RFC 8628 when the server omits them.
const (
	defaultPollInterval = 5 * time.Second
	defaultPollExpiry   = 10 * time.Minute
)

// Sentinel errors for terminal device-token polling outcomes.
var (
	// ErrAuthorizationDenied is returned when the user denies the user_code on
	// the verification page.
	ErrAuthorizationDenied = errors.New("authorization denied by user")
	// ErrDeviceCodeExpired is returned when the device_code expires before the
	// user authorizes, or when the overall expiry deadline passes while polling.
	ErrDeviceCodeExpired = errors.New("device code expired")
)

// APIError is the orchestrator's error envelope {error:{code,message}}.
type APIError struct {
	StatusCode int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s (HTTP %d)", e.Code, e.Message, e.StatusCode)
	}
	return fmt.Sprintf("%s (HTTP %d)", e.Message, e.StatusCode)
}

// ValidateCloudURL normalizes a --cloud flag value and enforces the scheme
// policy from docs/17 §3.3: https everywhere, except that
// localhost / 127.0.0.1 / [::1] (any port) may use http for development.
// The returned URL has no trailing slash.
func ValidateCloudURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("cloud URL must not be empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid cloud URL %q: %w", raw, err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid cloud URL %q: missing scheme or host (want e.g. %s)", raw, DefaultCloudURL)
	}
	switch u.Scheme {
	case "https":
	case "http":
		switch u.Hostname() {
		case "localhost", "127.0.0.1", "::1":
		default:
			return "", fmt.Errorf("cloud URL %q: plain http is only allowed for localhost/127.0.0.1/[::1] (use https)", raw)
		}
	default:
		return "", fmt.Errorf("invalid cloud URL %q: unsupported scheme %q (use https)", raw, u.Scheme)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// Client talks to the jcloud orchestrator's device-auth and internal device
// endpoints.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient returns a Client for baseURL (already validated). A nil
// HTTPClient means a default one with a 30s timeout.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// DeviceCodeResponse is the answer of POST /auth/device/code.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"` // seconds
	Interval        int    `json:"interval"`   // seconds
}

// DeviceTokenResponse is the success answer of POST /auth/device/token.
type DeviceTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	DeviceID    string `json:"device_id"`
}

// RegisterDeviceRequest is the body of POST /internal/v1/device/register.
type RegisterDeviceRequest struct {
	Name         string `json:"name"`
	Hostname     string `json:"hostname"`
	JcodeVersion string `json:"jcode_version"`
	PubKey       string `json:"pubkey"` // X25519 public key, base64
}

// post issues one JSON POST request and decodes the response envelope. token,
// when non-empty, is sent as a Bearer credential. out may be nil for responses
// whose body is irrelevant. Non-2xx responses become *APIError.
func (c *Client) post(ctx context.Context, path, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request POST %s: %w", path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("POST %s: read response: %w", path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
		var envelope struct {
			Error *APIError `json:"error"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.Error != nil {
			apiErr = envelope.Error
			apiErr.StatusCode = resp.StatusCode
		} else if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
			apiErr.Message = trimmed
		}
		return apiErr
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("POST %s: decode response: %w", path, err)
		}
	}
	return nil
}

// RequestDeviceCode starts the device authorization flow (RFC 8628 §3.1):
// POST {url}/auth/device/code.
func (c *Client) RequestDeviceCode(ctx context.Context, clientName string) (*DeviceCodeResponse, error) {
	var out DeviceCodeResponse
	if err := c.post(ctx, "/auth/device/code", "", map[string]string{"client_name": clientName}, &out); err != nil {
		return nil, err
	}
	if out.DeviceCode == "" || out.UserCode == "" || out.VerificationURI == "" {
		return nil, fmt.Errorf("incomplete device code response from %s", c.BaseURL)
	}
	return &out, nil
}

// PollDeviceToken makes a single token poll attempt: POST /auth/device/token.
// Pending authorization surfaces as an *APIError with Code
// "authorization_pending" (or "slow_down"); terminal failures use
// "access_denied" / "expired_token".
func (c *Client) PollDeviceToken(ctx context.Context, deviceCode string) (*DeviceTokenResponse, error) {
	var out DeviceTokenResponse
	if err := c.post(ctx, "/auth/device/token", "", map[string]string{"device_code": deviceCode}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PollForToken polls POST /auth/device/token every interval until the user
// authorizes, the deadline (now+expiresIn) passes, or ctx is cancelled. Zero
// interval/expiresIn fall back to RFC 8628 defaults.
func (c *Client) PollForToken(ctx context.Context, deviceCode string, interval, expiresIn time.Duration) (*DeviceTokenResponse, error) {
	if interval <= 0 {
		interval = defaultPollInterval
	}
	if expiresIn <= 0 {
		expiresIn = defaultPollExpiry
	}
	deadline := time.Now().Add(expiresIn)

	for {
		tok, err := c.PollDeviceToken(ctx, deviceCode)
		if err == nil {
			return tok, nil
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			return nil, err
		}
		switch apiErr.Code {
		case "authorization_pending":
			// Keep polling at the current interval.
		case "slow_down":
			interval += 5 * time.Second
		case "access_denied":
			return nil, ErrAuthorizationDenied
		case "expired_token":
			return nil, ErrDeviceCodeExpired
		default:
			return nil, err
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, ErrDeviceCodeExpired
		}
		wait := interval
		if wait > remaining {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		if time.Now().After(deadline) {
			return nil, ErrDeviceCodeExpired
		}
	}
}

// RegisterDevice registers (or refreshes) this device with the orchestrator:
// POST /internal/v1/device/register (Bearer device token).
func (c *Client) RegisterDevice(ctx context.Context, token string, req RegisterDeviceRequest) error {
	return c.post(ctx, "/internal/v1/device/register", token, req, nil)
}

// RevokeDevice asks the orchestrator to revoke this device's token:
// POST /internal/v1/device/revoke. A 404 (endpoint not deployed yet on the
// server side) is treated as success — local cleanup proceeds regardless.
func (c *Client) RevokeDevice(ctx context.Context, token string) error {
	err := c.post(ctx, "/internal/v1/device/revoke", token, map[string]string{}, nil)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return nil
	}
	return err
}
