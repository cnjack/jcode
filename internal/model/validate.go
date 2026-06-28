package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ValidateResult is the structured outcome of a connectivity test against a
// provider's /models endpoint. It carries everything the UI needs to render a
// status banner: success with latency + available-model count, or a classified
// failure (auth vs. network vs. server).
type ValidateResult struct {
	OK         bool   `json:"ok"`
	LatencyMS  int    `json:"latency_ms"`
	ModelCount int    `json:"model_count"`
	ErrorType  string `json:"error_type,omitempty"` // "" | "auth" | "network" | "server"
	Error      string `json:"error,omitempty"`
}

// ValidateProvider tests connectivity to a provider by making a lightweight
// GET /models request. Custom headers (if any) are applied last so they can
// override the default Authorization for gateways. Returns nil on success, or
// a descriptive error.
//
// This thin wrapper preserves the original signature for any caller that only
// cares about success/failure. Callers needing latency, model count, or error
// classification should use ValidateProviderDetailed instead.
func ValidateProvider(ctx context.Context, apiKey, baseURL string, headers map[string]string) error {
	res := ValidateProviderDetailed(ctx, apiKey, baseURL, headers)
	if res.OK {
		return nil
	}
	if res.Error != "" {
		return fmt.Errorf("%s", res.Error)
	}
	return fmt.Errorf("validation failed")
}

// ValidateProviderDetailed performs the same connectivity test as
// ValidateProvider but returns the full structured result, including the
// measured latency, the number of models advertised at /models, and a
// classified error type on failure.
func ValidateProviderDetailed(ctx context.Context, apiKey, baseURL string, headers map[string]string) ValidateResult {
	if baseURL == "" {
		return ValidateResult{ErrorType: "server", Error: "base URL is empty"}
	}

	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return ValidateResult{ErrorType: "server", Error: "failed to create request: " + err.Error()}
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		// A transport-level failure (DNS, connection refused, timeout, TLS) is
		// classified as "network" — distinct from an HTTP error from the server.
		return ValidateResult{LatencyMS: latency, ErrorType: "network", Error: "connection failed: " + err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return ValidateResult{LatencyMS: latency, ErrorType: "auth", Error: "invalid API key (401 Unauthorized)"}
	}
	if resp.StatusCode == http.StatusForbidden {
		return ValidateResult{LatencyMS: latency, ErrorType: "auth", Error: "access denied (403 Forbidden) — check API key permissions"}
	}
	if resp.StatusCode >= 400 {
		return ValidateResult{
			LatencyMS: latency,
			ErrorType: "server",
			Error:     fmt.Sprintf("server returned %d %s", resp.StatusCode, resp.Status),
		}
	}

	// Success — best-effort parse of the model count from the /models payload.
	// The OpenAI-compatible shape is {"data":[{"id":"..."}, ...]}; if the body
	// doesn't decode or lacks "data", we still report success with count 0.
	count := countModels(resp.Body)
	return ValidateResult{OK: true, LatencyMS: latency, ModelCount: count}
}

// modelsList is the subset of the OpenAI-compatible /models response we need
// to count advertised models.
type modelsList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// readModelsBody fully drains a response body up to a 1MiB safety cap, returning
// the accumulated bytes. Used by both ValidateProviderDetailed and
// ListProviderModelsLive so the /models payload is only fetched once per call.
func readModelsBody(r interface{ Read([]byte) (int, error) }) []byte {
	var buf [1 << 16]byte // 64KiB per read is ample for a model-id listing
	var full []byte
	for {
		n, err := r.Read(buf[:])
		if n > 0 {
			full = append(full, buf[:n]...)
		}
		if err != nil {
			break
		}
		if len(full) > 1<<20 { // 1MiB safety cap
			break
		}
	}
	return full
}

// countModels decodes the OpenAI-compatible /models body and returns the number
// of advertised models. It never returns an error: an undecodable or unexpected
// body simply yields 0, since a successful HTTP status is enough to confirm
// connectivity.
func countModels(r interface{ Read([]byte) (int, error) }) int {
	var ml modelsList
	if err := json.Unmarshal(readModelsBody(r), &ml); err != nil {
		return 0
	}
	return len(ml.Data)
}

// ListProviderModelsLive queries a provider's live /models endpoint and returns
// the advertised model ids. It is best-effort: any failure (network, auth,
// non-JSON body) yields an empty slice, since the catalog UI degrades to "no
// catalog, add manually" rather than erroring.
func ListProviderModelsLive(ctx context.Context, apiKey, baseURL string, headers map[string]string) []string {
	if baseURL == "" {
		return nil
	}
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil
	}
	var ml modelsList
	if err := json.NewDecoder(resp.Body).Decode(&ml); err != nil {
		return nil
	}
	ids := make([]string, 0, len(ml.Data))
	for _, m := range ml.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids
}
