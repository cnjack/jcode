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

	"github.com/cnjack/jcode/internal/modelcatalog"
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
	entries := modelcatalog.RawEntries(payload)
	models := make([]Model, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		model, ok := parseManagedModel(method, entry.Value, entry.FallbackID)
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

// parseManagedModel delegates dialect handling (ids, names, Copilot
// capabilities, xAI image pricing, …) to modelcatalog and layers the
// method-specific kind and wire protocol on top.
func parseManagedModel(method Method, value any, fallbackID string) (Model, bool) {
	entry, ok := modelcatalog.ParseEntry(value, fallbackID)
	if !ok {
		return Model{}, false
	}
	if method == MethodGitHubCopilot && entry.Hidden {
		return Model{}, false
	}
	name := entry.Name
	if name == "" {
		name = entry.ID
	}
	kind := kindForManagedModel(method, entry.ID)
	model := Model{
		ID:       entry.ID,
		Name:     name,
		Vendor:   entry.Vendor,
		Protocol: protocolForManagedModel(method, entry.Vendor),
		Kind:     kind,
		Context:  entry.Context,
	}
	if kind == ModelKindChat {
		model.Attachment = entry.Attachment != nil && *entry.Attachment
		if entry.Reasoning != nil && *entry.Reasoning {
			model.Reasoning = true
			model.EffortTiers = append([]string(nil), entry.EffortTiers...)
		}
	}
	return model, true
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
