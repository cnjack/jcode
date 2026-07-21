package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidateCloudURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string // normalized URL; empty means expect an error
		wantErr bool
	}{
		{name: "https default", raw: "https://cloud.j-code.net", want: "https://cloud.j-code.net"},
		{name: "https trailing slash trimmed", raw: "https://cloud.j-code.net/", want: "https://cloud.j-code.net"},
		{name: "https self-host with port", raw: "https://cloud.example.com:8443", want: "https://cloud.example.com:8443"},
		{name: "http remote rejected", raw: "http://cloud.example.com", wantErr: true},
		{name: "http localhost", raw: "http://localhost", want: "http://localhost"},
		{name: "http localhost with port", raw: "http://localhost:8080", want: "http://localhost:8080"},
		{name: "http 127.0.0.1 with port", raw: "http://127.0.0.1:3000", want: "http://127.0.0.1:3000"},
		{name: "http ipv6 loopback with port", raw: "http://[::1]:9000", want: "http://[::1]:9000"},
		{name: "http other loopback rejected", raw: "http://127.0.0.2:3000", wantErr: true},
		{name: "unsupported scheme", raw: "ftp://cloud.example.com", wantErr: true},
		{name: "missing scheme", raw: "cloud.example.com", wantErr: true},
		{name: "empty", raw: "", wantErr: true},
		{name: "whitespace trimmed", raw: "  https://cloud.j-code.net  ", want: "https://cloud.j-code.net"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateCloudURL(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateCloudURL(%q) = %q, want error", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateCloudURL(%q) error = %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ValidateCloudURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// deviceAuthServer builds an httptest server speaking the device-auth
// contract. tokenHandler decides what POST /auth/device/token returns.
func deviceAuthServer(t *testing.T, tokenHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/device/code", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ClientName string `json:"client_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode /auth/device/code body: %v", err)
		}
		if body.ClientName == "" {
			t.Errorf("client_name is empty")
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"device_code":      "dev-code-123",
			"user_code":        "ABCD-EFGH",
			"verification_uri": "https://cloud.example.com/auth/device",
			"expires_in":       600,
			"interval":         1,
		})
	})
	mux.Handle("POST /auth/device/token", tokenHandler)
	return httptest.NewServer(mux)
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

// writeError emits the orchestrator's {error:{code,message}} envelope with a
// 400 status, as the device-token endpoint does for pending/denied/expired.
func writeError(t *testing.T, w http.ResponseWriter, code, message string) {
	t.Helper()
	writeJSON(t, w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func TestRequestDeviceCode(t *testing.T) {
	srv := deviceAuthServer(t, http.NotFoundHandler().ServeHTTP)
	defer srv.Close()

	dc, err := NewClient(srv.URL).RequestDeviceCode(context.Background(), "jcode CLI test")
	if err != nil {
		t.Fatalf("RequestDeviceCode() error = %v", err)
	}
	if dc.DeviceCode != "dev-code-123" || dc.UserCode != "ABCD-EFGH" {
		t.Fatalf("RequestDeviceCode() = %+v", dc)
	}
	if dc.ExpiresIn != 600 || dc.Interval != 1 {
		t.Fatalf("RequestDeviceCode() expiry/interval = %d/%d, want 600/1", dc.ExpiresIn, dc.Interval)
	}
}

func TestPollForTokenPendingThenSuccess(t *testing.T) {
	var calls int32
	srv := deviceAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		var body struct {
			DeviceCode string `json:"device_code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode token body: %v", err)
		}
		if body.DeviceCode != "dev-code-123" {
			t.Errorf("device_code = %q, want dev-code-123", body.DeviceCode)
		}
		if n < 3 {
			writeError(t, w, "authorization_pending", "user has not authorized yet")
			return
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"access_token": "dev-token-abc",
			"token_type":   "device",
			"device_id":    "device-42",
		})
	})
	defer srv.Close()

	tok, err := NewClient(srv.URL).PollForToken(context.Background(), "dev-code-123", "", time.Millisecond, 10*time.Second)
	if err != nil {
		t.Fatalf("PollForToken() error = %v", err)
	}
	if tok.AccessToken != "dev-token-abc" || tok.DeviceID != "device-42" {
		t.Fatalf("PollForToken() = %+v", tok)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("token endpoint called %d times, want 3", got)
	}
}

func TestPollForTokenDenied(t *testing.T) {
	srv := deviceAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(t, w, "access_denied", "user denied the request")
	})
	defer srv.Close()

	_, err := NewClient(srv.URL).PollForToken(context.Background(), "dev-code-123", "", time.Millisecond, 10*time.Second)
	if !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("PollForToken() error = %v, want ErrAuthorizationDenied", err)
	}
}

func TestPollForTokenExpired(t *testing.T) {
	srv := deviceAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(t, w, "expired_token", "device code expired")
	})
	defer srv.Close()

	_, err := NewClient(srv.URL).PollForToken(context.Background(), "dev-code-123", "", time.Millisecond, 10*time.Second)
	if !errors.Is(err, ErrDeviceCodeExpired) {
		t.Fatalf("PollForToken() error = %v, want ErrDeviceCodeExpired", err)
	}
}

func TestPollForTokenOverallDeadline(t *testing.T) {
	srv := deviceAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(t, w, "authorization_pending", "still waiting")
	})
	defer srv.Close()

	start := time.Now()
	_, err := NewClient(srv.URL).PollForToken(context.Background(), "dev-code-123", "", 5*time.Millisecond, 20*time.Millisecond)
	if !errors.Is(err, ErrDeviceCodeExpired) {
		t.Fatalf("PollForToken() error = %v, want ErrDeviceCodeExpired", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("PollForToken() took %v, want bounded by expires_in", elapsed)
	}
}

func TestPollForTokenContextCancel(t *testing.T) {
	srv := deviceAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(t, w, "authorization_pending", "still waiting")
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient(srv.URL).PollForToken(ctx, "dev-code-123", "", time.Second, time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PollForToken() error = %v, want context.Canceled", err)
	}
}

func TestRegisterDevice(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/device/register" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		var req RegisterDeviceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode register body: %v", err)
		}
		data, _ := json.Marshal(req)
		gotBody = string(data)
		writeJSON(t, w, http.StatusOK, map[string]any{"server_time": "2026-07-20T00:00:00Z"})
	}))
	defer srv.Close()

	err := NewClient(srv.URL).RegisterDevice(context.Background(), "dev-token-abc", RegisterDeviceRequest{
		Name:         "jack-macbook",
		Hostname:     "jack-macbook.local",
		JcodeVersion: "1.2.3",
		PubKey:       "cHVia2V5",
	})
	if err != nil {
		t.Fatalf("RegisterDevice() error = %v", err)
	}
	if gotAuth != "Bearer dev-token-abc" {
		t.Fatalf("Authorization header = %q, want Bearer token", gotAuth)
	}
	for _, field := range []string{`"name":"jack-macbook"`, `"hostname":"jack-macbook.local"`, `"jcode_version":"1.2.3"`, `"pubkey":"cHVia2V5"`} {
		if !strings.Contains(gotBody, field) {
			t.Fatalf("register body %s missing %s", gotBody, field)
		}
	}
}

func TestRevokeDeviceIgnores404(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if err := NewClient(srv.URL).RevokeDevice(context.Background(), "tok"); err != nil {
		t.Fatalf("RevokeDevice() on 404 error = %v, want nil", err)
	}
}

func TestErrorEnvelopeParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(t, w, "some_code", "something went wrong")
	}))
	defer srv.Close()

	err := NewClient(srv.URL).RegisterDevice(context.Background(), "tok", RegisterDeviceRequest{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("RegisterDevice() error = %v, want *APIError", err)
	}
	if apiErr.Code != "some_code" || apiErr.Message != "something went wrong" || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("APIError = %+v", apiErr)
	}
}

// TestPollForTokenSendsFingerprint covers the M16 contract: the sha256
// fingerprint hash rides every token poll, and a deduped=true response
// decodes.
func TestPollForTokenSendsFingerprint(t *testing.T) {
	const fp = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	srv := deviceAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DeviceCode  string `json:"device_code"`
			Fingerprint string `json:"fingerprint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode token body: %v", err)
		}
		if body.Fingerprint != fp {
			t.Errorf("fingerprint = %q, want %q", body.Fingerprint, fp)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"access_token": "dev-token-abc",
			"token_type":   "device",
			"device_id":    "device-42",
			"deduped":      true,
		})
	})
	defer srv.Close()

	tok, err := NewClient(srv.URL).PollForToken(context.Background(), "dev-code-123", fp, time.Millisecond, 10*time.Second)
	if err != nil {
		t.Fatalf("PollForToken() error = %v", err)
	}
	if !tok.Deduped {
		t.Fatalf("PollForToken() Deduped = false, want true (%+v)", tok)
	}
}

// TestRegisterDeviceSendsFingerprint: the register body carries the hash for
// the server-side backfill (M16).
func TestRegisterDeviceSendsFingerprint(t *testing.T) {
	const fp = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		gotBody = string(data)
		writeJSON(t, w, http.StatusOK, map[string]any{"server_time": "2026-07-20T00:00:00Z"})
	}))
	defer srv.Close()

	err := NewClient(srv.URL).RegisterDevice(context.Background(), "tok", RegisterDeviceRequest{
		Name: "x", PubKey: "cHVia2V5", Fingerprint: fp,
	})
	if err != nil {
		t.Fatalf("RegisterDevice() error = %v", err)
	}
	if !strings.Contains(gotBody, `"fingerprint":"`+fp+`"`) {
		t.Fatalf("register body %s missing fingerprint", gotBody)
	}
}
