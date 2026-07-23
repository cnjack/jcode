package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/cnjack/jcode/internal/config"
)

const CloudProviderPrefix = "cloud:"

type AccountSyncKeyState struct {
	State     string          `json:"state"`
	KeyGen    int             `json:"key_gen,omitempty"`
	Status    string          `json:"status,omitempty"`
	Wrap      json.RawMessage `json:"wrap,omitempty"`
	UpdatedAt string          `json:"updated_at,omitempty"`
}

type AccountSyncKeyRequest struct {
	DeviceID           string          `json:"device_id"`
	DeviceName         string          `json:"device_name,omitempty"`
	Pubkey             string          `json:"pubkey,omitempty"`
	KeyGen             int             `json:"key_gen"`
	Status             string          `json:"status"`
	Wrap               json.RawMessage `json:"wrap,omitempty"`
	ApprovedByDeviceID string          `json:"approved_by_device_id,omitempty"`
	CreatedAt          string          `json:"created_at"`
	ResolvedAt         string          `json:"resolved_at,omitempty"`
}

type AccountProviderConfigRemote struct {
	ProviderID string          `json:"provider_id"`
	Version    int64           `json:"version"`
	Envelope   json.RawMessage `json:"envelope"`
	Deleted    bool            `json:"deleted"`
	UpdatedAt  string          `json:"updated_at"`
}

type CloudModel struct {
	ModelID         string `json:"model_id"`
	ProviderID      string `json:"provider_id"`
	Kind            string `json:"kind"`
	ProviderName    string `json:"provider_name"`
	ModelName       string `json:"model_name"`
	UpstreamModelID string `json:"upstream_model_id"`
	Scope           string `json:"scope"`
	ScopeID         string `json:"scope_id"`
	ScopeName       string `json:"scope_name,omitempty"`
	Capabilities    struct {
		Reasoning bool `json:"reasoning"`
		Tools     bool `json:"tools"`
		Image     bool `json:"image"`
	} `json:"capabilities"`
	ContextWindow int `json:"context_window"`
}

func (c *Client) GetAccountSyncKey(ctx context.Context, token string) (*AccountSyncKeyState, error) {
	var out AccountSyncKeyState
	if _, err := c.get(ctx, "/internal/v1/device/config-key", token, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) InitializeAccountSyncKey(ctx context.Context, token string, keyGen int, wrap *AccountSyncKeyWrap) error {
	return c.post(ctx, "/internal/v1/device/config-key/initialize", token, map[string]any{
		"key_gen": keyGen, "wrap": wrap,
	}, nil)
}

func (c *Client) RequestAccountSyncKey(ctx context.Context, token string) (*AccountSyncKeyRequest, error) {
	var out AccountSyncKeyRequest
	if err := c.post(ctx, "/internal/v1/device/config-key/request", token, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListAccountSyncKeyRequests(ctx context.Context, token, status string) ([]AccountSyncKeyRequest, error) {
	var out struct {
		Requests []AccountSyncKeyRequest `json:"requests"`
	}
	path := "/internal/v1/device/config-key/requests"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	if _, err := c.get(ctx, path, token, &out); err != nil {
		return nil, err
	}
	return out.Requests, nil
}

func (c *Client) RespondAccountSyncKeyRequest(ctx context.Context, token, deviceID string, approve bool, keyGen int, wrap *AccountSyncKeyWrap) error {
	return c.post(ctx,
		"/internal/v1/device/config-key/requests/"+url.PathEscape(deviceID)+"/respond",
		token, map[string]any{"approve": approve, "key_gen": keyGen, "wrap": wrap}, nil)
}

func (c *Client) RevokeAccountSyncKeyDevice(ctx context.Context, token, deviceID string) error {
	return c.writeJSON(ctx, "DELETE",
		"/internal/v1/device/config-key/devices/"+url.PathEscape(deviceID),
		token, nil, nil)
}

func (c *Client) ListAccountProviderConfigs(ctx context.Context, token string) ([]AccountProviderConfigRemote, error) {
	var out struct {
		Providers []AccountProviderConfigRemote `json:"providers"`
	}
	if _, err := c.get(ctx, "/internal/v1/device/provider-configs", token, &out); err != nil {
		return nil, err
	}
	return out.Providers, nil
}

func (c *Client) PutAccountProviderConfig(ctx context.Context, token, providerID string, baseVersion int64, envelope json.RawMessage, deleted bool) (*AccountProviderConfigRemote, error) {
	var out AccountProviderConfigRemote
	err := c.put(ctx, "/internal/v1/device/provider-configs/"+url.PathEscape(providerID), token, map[string]any{
		"base_version": baseVersion, "envelope": envelope, "deleted": deleted,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListCloudModels(ctx context.Context, token string) ([]CloudModel, error) {
	var out struct {
		Models []CloudModel `json:"models"`
	}
	if _, err := c.get(ctx, "/internal/v1/device/cloud-models", token, &out); err != nil {
		return nil, err
	}
	return out.Models, nil
}

func CloudProviderRef(providerID string) string {
	return CloudProviderPrefix + providerID
}

func ParseCloudProviderRef(ref string) (string, bool) {
	providerID, ok := strings.CutPrefix(ref, CloudProviderPrefix)
	return providerID, ok && providerID != ""
}

func cloudURLForConfig(cfg *config.Config, creds *Credentials) string {
	if cfg != nil {
		if value := cfg.CloudSettings().URL; value != "" {
			return strings.TrimRight(value, "/")
		}
	}
	if creds != nil && creds.CloudURL != "" {
		return strings.TrimRight(creds.CloudURL, "/")
	}
	return DefaultCloudURL
}

// ListConfiguredCloudModels loads the current Desktop identity and returns the
// Cloud provider catalog visible to its account. It is deliberately independent
// from local ProviderConfig: Cloud provider secrets stay server-side.
func ListConfiguredCloudModels(ctx context.Context, cfg *config.Config) ([]CloudModel, error) {
	creds, err := LoadCredentials()
	if err != nil {
		return nil, err
	}
	if creds == nil || creds.DeviceToken == "" {
		return nil, errors.New("desktop is not logged in to Cloud")
	}
	return NewClient(cloudURLForConfig(cfg, creds)).ListCloudModels(ctx, creds.DeviceToken)
}

// ResolveCloudModel validates a cloud:<provider-id>/<model-id> selection and
// returns its catalog row plus the OpenAI-compatible cloud_proxy base URL and
// current device token. Local providers never call this path.
func ResolveCloudModel(ctx context.Context, cfg *config.Config, providerRef, modelID string) (CloudModel, string, string, error) {
	var zero CloudModel
	providerID, ok := ParseCloudProviderRef(providerRef)
	if !ok {
		return zero, "", "", fmt.Errorf("invalid Cloud provider reference %q", providerRef)
	}
	creds, err := LoadCredentials()
	if err != nil {
		return zero, "", "", err
	}
	if creds == nil || creds.DeviceToken == "" {
		return zero, "", "", errors.New("desktop is not logged in to Cloud")
	}
	baseURL := cloudURLForConfig(cfg, creds)
	models, err := NewClient(baseURL).ListCloudModels(ctx, creds.DeviceToken)
	if err != nil {
		return zero, "", "", err
	}
	for _, candidate := range models {
		if candidate.ProviderID != providerID || candidate.ModelID != modelID {
			continue
		}
		proxyBase := baseURL + "/internal/v1/device/cloud-models/" +
			url.PathEscape(candidate.ModelID) + "/llm/v1"
		return candidate, proxyBase, creds.DeviceToken, nil
	}
	return zero, "", "", fmt.Errorf("cloud model %q is not available from provider %q", modelID, providerID)
}
