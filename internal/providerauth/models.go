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
	"sort"
	"strings"
)

const maxModelCatalogResponseBytes = 1 << 20

// Models returns the live, account-scoped model catalog for a managed login.
// Credentials are resolved immediately before the request and never included
// in the returned projection or an error body.
func (manager *Manager) Models(ctx context.Context, binding Binding) ([]Model, error) {
	credential, err := manager.Credential(ctx, binding)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(credential.Token) == "" {
		return nil, errors.New("managed provider model catalog requires a token")
	}

	endpoint, headers, err := modelCatalogRequest(binding.Method, credential)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create managed provider model request: %w", err)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	status, payload, err := manager.doModelCatalogJSON(request)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("managed provider model catalog failed: HTTP %d", status)
	}

	models := parseManagedModels(binding.Method, payload)
	if len(models) == 0 {
		return nil, errors.New("managed provider returned an empty model catalog")
	}
	return models, nil
}

func modelCatalogRequest(method Method, credential Credential) (string, map[string]string, error) {
	base, err := url.Parse(strings.TrimRight(credential.BaseURL, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", nil, errors.New("managed provider returned an invalid model catalog endpoint")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/models"
	base.RawQuery = ""
	base.Fragment = ""

	headers := make(map[string]string, len(credential.Headers)+1)
	for name, value := range credential.Headers {
		headers[name] = value
	}
	switch method {
	case MethodCodexOAuth:
		query := base.Query()
		query.Set("client_version", codexClientVersion)
		base.RawQuery = query.Encode()
	case MethodXAIOAuth:
		// Standard Responses provider catalog; no extra headers required.
	case MethodGitHubCopilot:
		headers["Content-Type"] = "application/json"
	default:
		return "", nil, fmt.Errorf("%w: %q", ErrUnsupportedMethod, method)
	}
	return base.String(), headers, nil
}

func (manager *Manager) doModelCatalogJSON(request *http.Request) (int, any, error) {
	response, err := manager.client.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("managed provider model request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.ContentLength > maxModelCatalogResponseBytes {
		return response.StatusCode, nil, errors.New("managed provider model response exceeds 1 MiB limit")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxModelCatalogResponseBytes+1))
	if err != nil {
		return response.StatusCode, nil, fmt.Errorf("read managed provider model response: %w", err)
	}
	if len(body) > maxModelCatalogResponseBytes {
		return response.StatusCode, nil, errors.New("managed provider model response exceeds 1 MiB limit")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return response.StatusCode, nil, nil
	}
	var payload any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return response.StatusCode, nil, errors.New("managed provider model response is not valid JSON")
	}
	return response.StatusCode, payload, nil
}

func parseManagedModels(method Method, payload any) []Model {
	entries := modelEntries(payload)
	models := make([]Model, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		model, ok := parseManagedModel(method, entry.value, entry.fallbackID)
		if !ok {
			continue
		}
		if _, duplicate := seen[model.ID]; duplicate {
			continue
		}
		seen[model.ID] = struct{}{}
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

type modelEntry struct {
	value      any
	fallbackID string
}

func modelEntries(payload any) []modelEntry {
	if list, ok := payload.([]any); ok {
		return wrapModelEntries(list)
	}
	object, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range []string{"data", "items"} {
		if list, ok := object[key].([]any); ok {
			return wrapModelEntries(list)
		}
	}
	if list, ok := object["models"].([]any); ok {
		return wrapModelEntries(list)
	}
	if modelMap, ok := object["models"].(map[string]any); ok {
		entries := make([]modelEntry, 0, len(modelMap))
		for id, value := range modelMap {
			entries = append(entries, modelEntry{value: value, fallbackID: id})
		}
		return entries
	}
	return nil
}

func wrapModelEntries(input []any) []modelEntry {
	entries := make([]modelEntry, 0, len(input))
	for _, value := range input {
		entries = append(entries, modelEntry{value: value})
	}
	return entries
}

func parseManagedModel(method Method, value any, fallbackID string) (Model, bool) {
	if id, ok := value.(string); ok {
		id = strings.TrimSpace(id)
		if id == "" {
			return Model{}, false
		}
		return Model{ID: id, Name: id, Protocol: protocolForManagedModel(method, ""), Kind: kindForManagedModel(method, id)}, true
	}
	object, ok := value.(map[string]any)
	if !ok {
		if strings.TrimSpace(fallbackID) == "" {
			return Model{}, false
		}
		id := strings.TrimSpace(fallbackID)
		return Model{ID: id, Name: id, Protocol: protocolForManagedModel(method, ""), Kind: kindForManagedModel(method, id)}, true
	}
	if method == MethodGitHubCopilot {
		if enabled, exists := object["model_picker_enabled"].(bool); exists && !enabled {
			return Model{}, false
		}
	}
	id := firstModelString(object, "slug", "id", "model")
	if id == "" {
		id = strings.TrimSpace(fallbackID)
	}
	if id == "" {
		id = firstModelString(object, "name")
	}
	if id == "" {
		return Model{}, false
	}
	name := firstModelString(object, "name", "display_name", "displayName")
	if name == "" {
		name = id
	}
	vendor := firstModelString(object, "vendor", "owned_by", "ownedBy", "provider", "owner")
	return Model{
		ID:       id,
		Name:     name,
		Vendor:   vendor,
		Protocol: protocolForManagedModel(method, vendor),
		Kind:     kindForManagedModel(method, id),
	}, true
}

func firstModelString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func protocolForManagedModel(method Method, vendor string) Protocol {
	switch method {
	case MethodCodexOAuth, MethodXAIOAuth:
		return ProtocolResponses
	case MethodGitHubCopilot:
		if strings.EqualFold(strings.TrimSpace(vendor), "openai") {
			return ProtocolResponses
		}
		return ProtocolChatCompletions
	default:
		return ""
	}
}

func kindForManagedModel(method Method, modelID string) ModelKind {
	if method == MethodXAIOAuth {
		switch {
		case strings.HasPrefix(modelID, "grok-imagine-image"):
			return ModelKindImage
		case strings.HasPrefix(modelID, "grok-imagine-video"):
			return ModelKindVideo
		}
	}
	return ModelKindChat
}
