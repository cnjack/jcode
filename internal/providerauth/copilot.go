package providerauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	copilotGitHubClientID = "Iv1.b507a08c87ecfe98"
	copilotEditorVersion  = "vscode/1.110.1"
	copilotPluginVersion  = "copilot-chat/0.38.2"
	copilotUserAgent      = "GitHubCopilotChat/0.38.2"
	copilotAPIVersion     = "2025-10-01"
	copilotIntegrationID  = "vscode-chat"
)

func (manager *Manager) startCopilot(ctx context.Context) (*pendingFlow, error) {
	status, value, err := manager.postForm(
		ctx,
		manager.endpoints.CopilotDeviceStart,
		url.Values{
			"client_id": {copilotGitHubClientID},
			"scope":     {"read:user"},
		},
		copilotGitHubHeaders(),
	)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, upstreamError("start GitHub device authorization", status, value)
	}
	deviceCode := stringField(value, "device_code")
	userCode := stringField(value, "user_code")
	verificationURI := stringField(value, "verification_uri")
	verificationComplete := stringField(value, "verification_uri_complete")
	if deviceCode == "" || userCode == "" || verificationURI == "" {
		return nil, errors.New("GitHub device authorization response is missing required fields")
	}
	if err := manager.validateVerificationURIs(
		verificationURI, verificationComplete, "github.com",
	); err != nil {
		return nil, err
	}
	expiresIn := clampSeconds(intField(value, "expires_in", 900), 24*60*60)
	interval := clampSeconds(intField(value, "interval", 5), 60) + 3
	now := manager.now()
	return &pendingFlow{
		public: Flow{
			Method:                  MethodGitHubCopilot,
			State:                   FlowStatePending,
			UserCode:                userCode,
			VerificationURI:         verificationURI,
			VerificationURIComplete: verificationComplete,
			ExpiresAt:               now.Add(time.Duration(expiresIn) * time.Second),
			IntervalSeconds:         int(interval),
		},
		deviceCode: deviceCode,
		nextPollAt: now,
		interval:   time.Duration(interval) * time.Second,
	}, nil
}

func (manager *Manager) pollCopilot(ctx context.Context, pending *pendingFlow) (Flow, error) {
	status, value, err := manager.postForm(
		ctx,
		manager.endpoints.CopilotOAuthToken,
		url.Values{
			"client_id":   {copilotGitHubClientID},
			"device_code": {pending.deviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		},
		copilotGitHubHeaders(),
	)
	if err != nil {
		return Flow{}, err
	}
	switch oauthError(value) {
	case "authorization_pending":
		return pending.public, nil
	case "slow_down":
		pending.interval = min(pending.interval+5*time.Second, 63*time.Second)
		pending.public.IntervalSeconds = int(pending.interval / time.Second)
		pending.nextPollAt = manager.now().Add(pending.interval)
		return pending.public, nil
	case "access_denied":
		return terminalFlow(pending.public, FlowStateDenied, ErrAccessDenied), nil
	case "expired_token":
		return terminalFlow(pending.public, FlowStateExpired, ErrFlowExpired), nil
	case "":
	default:
		return Flow{}, upstreamError("poll GitHub device authorization", status, value)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return Flow{}, upstreamError("poll GitHub device authorization", status, value)
	}
	githubToken := stringField(value, "access_token")
	if githubToken == "" {
		return Flow{}, errors.New("GitHub OAuth response is missing access_token")
	}
	accountID, login, err := manager.fetchGitHubUser(ctx, githubToken)
	if err != nil {
		return Flow{}, err
	}
	copilotToken, expiresAt, err := manager.exchangeCopilotToken(ctx, githubToken)
	if err != nil {
		return Flow{}, err
	}
	account := storedAccount{
		ID: accountID, Login: login, Secret: githubToken, AuthenticatedAt: manager.now().UTC(),
	}
	if err := manager.commitFlowAccount(MethodGitHubCopilot, pending, account); err != nil {
		return Flow{}, err
	}
	manager.cache(MethodGitHubCopilot, accountID, copilotToken, expiresAt)
	flow := pending.public
	flow.State = FlowStateAuthorized
	public := account.public()
	flow.Account = &public
	return flow, nil
}

func copilotGitHubHeaders() map[string]string {
	return map[string]string{
		"Accept":                "application/json",
		"User-Agent":            copilotUserAgent,
		"Editor-Version":        copilotEditorVersion,
		"Editor-Plugin-Version": copilotPluginVersion,
	}
}

func (manager *Manager) fetchGitHubUser(
	ctx context.Context,
	githubToken string,
) (string, string, error) {
	headers := copilotGitHubHeaders()
	headers["Authorization"] = "Bearer " + githubToken
	status, value, err := manager.getJSON(ctx, manager.endpoints.CopilotUser, headers)
	if err != nil {
		return "", "", err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return "", "", errors.New("GitHub OAuth token was rejected")
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return "", "", upstreamError("fetch GitHub account", status, value)
	}
	id := numericStringField(value, "id")
	login := stringField(value, "login")
	if id == "" || login == "" {
		return "", "", errors.New("GitHub account response is missing id or login")
	}
	return id, login, nil
}

func numericStringField(value map[string]any, name string) string {
	switch raw := value[name].(type) {
	case string:
		return raw
	case json.Number:
		return raw.String()
	case float64:
		return strconv.FormatInt(int64(raw), 10)
	default:
		return ""
	}
}

func (manager *Manager) exchangeCopilotToken(
	ctx context.Context,
	githubToken string,
) (string, time.Time, error) {
	headers := copilotGitHubHeaders()
	headers["Authorization"] = "token " + githubToken
	status, value, err := manager.getJSON(ctx, manager.endpoints.CopilotToken, headers)
	if err != nil {
		return "", time.Time{}, err
	}
	if status == http.StatusUnauthorized {
		return "", time.Time{}, errors.New("GitHub OAuth token was rejected")
	}
	if status == http.StatusForbidden {
		return "", time.Time{}, ErrNoCopilotSubscription
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return "", time.Time{}, upstreamError("exchange GitHub Copilot token", status, value)
	}
	token := stringField(value, "token")
	if token == "" {
		return "", time.Time{}, errors.New("GitHub Copilot token response is missing token")
	}
	expiresAt := time.Unix(intField(value, "expires_at", manager.now().Add(time.Hour).Unix()), 0)
	if !expiresAt.After(manager.now().Add(refreshBuffer)) {
		return "", time.Time{}, errors.New("GitHub Copilot token response contains an expired token")
	}
	return token, expiresAt, nil
}

func (manager *Manager) refreshCopilot(
	ctx context.Context,
	account storedAccount,
) (string, error) {
	token, expiresAt, err := manager.exchangeCopilotToken(ctx, account.Secret)
	if err != nil {
		if errors.Is(err, ErrNoCopilotSubscription) {
			return "", err
		}
		if strings.Contains(err.Error(), "OAuth token was rejected") {
			if markErr := manager.markRequiresReauth(
				MethodGitHubCopilot, account.ID, account.Secret,
			); markErr != nil {
				return "", markErr
			}
			return "", fmt.Errorf("%w: %s", ErrRequiresReauth, account.ID)
		}
		return "", err
	}
	if err := manager.store.compareSecret(MethodGitHubCopilot, account.ID, account.Secret); err != nil {
		return "", err
	}
	manager.cache(MethodGitHubCopilot, account.ID, token, expiresAt)
	return token, nil
}

func (manager *Manager) copilotEndpoint(ctx context.Context, account storedAccount) (string, error) {
	key := accountKey(MethodGitHubCopilot, account.ID)
	manager.mu.RLock()
	endpoint := manager.copilotEndpoints[key]
	manager.mu.RUnlock()
	if endpoint != "" {
		return endpoint, nil
	}
	lock := manager.endpointLock(MethodGitHubCopilot, account.ID)
	lock.Lock()
	defer lock.Unlock()
	manager.mu.RLock()
	endpoint = manager.copilotEndpoints[key]
	manager.mu.RUnlock()
	if endpoint != "" {
		return endpoint, nil
	}
	headers := copilotGitHubHeaders()
	headers["Authorization"] = "token " + account.Secret
	status, value, err := manager.getJSON(ctx, manager.endpoints.CopilotUsage, headers)
	if err != nil {
		endpoint = manager.endpoints.CopilotRuntime
		manager.mu.Lock()
		manager.copilotEndpoints[key] = endpoint
		manager.mu.Unlock()
		return endpoint, nil
	}
	if status == http.StatusUnauthorized {
		if markErr := manager.markRequiresReauth(
			MethodGitHubCopilot, account.ID, account.Secret,
		); markErr != nil {
			return "", markErr
		}
		return "", fmt.Errorf("%w: %s", ErrRequiresReauth, account.ID)
	}
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		candidate := nestedString(value, "endpoints", "api")
		if candidate != "" && manager.validCopilotRuntime(candidate) {
			endpoint = candidate
		}
	}
	if endpoint == "" {
		endpoint = manager.endpoints.CopilotRuntime
	}
	manager.mu.Lock()
	manager.copilotEndpoints[key] = endpoint
	manager.mu.Unlock()
	return endpoint, nil
}

func (manager *Manager) validCopilotRuntime(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.User != nil {
		return false
	}
	if manager.allowUnsafe {
		return parsed.Scheme == "http" || parsed.Scheme == "https"
	}
	host := parsed.Hostname()
	return parsed.Scheme == "https" && parsed.Port() == "" &&
		(host == "api.githubcopilot.com" || strings.HasSuffix(host, ".githubcopilot.com"))
}
