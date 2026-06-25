package model

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// ValidateProvider tests connectivity to a provider by making a lightweight
// GET /models request. Custom headers (if any) are applied last so they can
// override the default Authorization for gateways. Returns nil on success, or
// a descriptive error.
func ValidateProvider(ctx context.Context, apiKey, baseURL string, headers map[string]string) error {
	if baseURL == "" {
		return fmt.Errorf("base URL is empty")
	}

	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid API key (401 Unauthorized)")
	}
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("access denied (403 Forbidden) — check API key permissions")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned %d %s", resp.StatusCode, resp.Status)
	}

	return nil
}
