package providerauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const maxOAuthResponseBytes = 64 << 10

func (manager *Manager) postJSON(
	ctx context.Context,
	endpoint string,
	body any,
	headers map[string]string,
) (int, map[string]any, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, nil, fmt.Errorf("encode OAuth request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return 0, nil, fmt.Errorf("create OAuth request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return manager.doJSON(request)
}

func (manager *Manager) postForm(
	ctx context.Context,
	endpoint string,
	form url.Values,
	headers map[string]string,
) (int, map[string]any, error) {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()),
	)
	if err != nil {
		return 0, nil, fmt.Errorf("create OAuth request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return manager.doJSON(request)
}

func (manager *Manager) getJSON(
	ctx context.Context,
	endpoint string,
	headers map[string]string,
) (int, map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("create OAuth request: %w", err)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return manager.doJSON(request)
}

func (manager *Manager) doJSON(request *http.Request) (int, map[string]any, error) {
	response, err := manager.client.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("provider auth HTTP request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.ContentLength > maxOAuthResponseBytes {
		return response.StatusCode, nil, errorsResponseTooLarge()
	}
	reader := io.LimitReader(response.Body, maxOAuthResponseBytes+1)
	body, err := io.ReadAll(reader)
	if err != nil {
		return response.StatusCode, nil, fmt.Errorf("read provider auth response: %w", err)
	}
	if len(body) > maxOAuthResponseBytes {
		return response.StatusCode, nil, errorsResponseTooLarge()
	}
	value := make(map[string]any)
	if len(bytes.TrimSpace(body)) == 0 {
		return response.StatusCode, value, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return response.StatusCode, value, nil
		}
		return response.StatusCode, nil, errors.New("provider auth response is not valid JSON")
	}
	return response.StatusCode, value, nil
}

func errorsResponseTooLarge() error {
	return errors.New("provider auth response exceeds 64 KiB limit")
}

func stringField(value map[string]any, name string) string {
	text, _ := value[name].(string)
	return strings.TrimSpace(text)
}

func intField(value map[string]any, name string, fallback int64) int64 {
	switch raw := value[name].(type) {
	case float64:
		return int64(raw)
	case json.Number:
		parsed, err := raw.Int64()
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func oauthError(value map[string]any) string {
	raw := stringField(value, "error")
	var builder strings.Builder
	for _, character := range raw {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("_.-", character) {
			builder.WriteRune(character)
			if builder.Len() >= 64 {
				break
			}
		}
	}
	return builder.String()
}

func upstreamError(operation string, status int, value map[string]any) error {
	code := oauthError(value)
	if code != "" {
		return fmt.Errorf("%s failed: HTTP %d (%s)", operation, status, code)
	}
	return fmt.Errorf("%s failed: HTTP %d", operation, status)
}
