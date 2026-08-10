package providerauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	codexClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexRedirectURI   = "https://auth.openai.com/deviceauth/callback"
	codexOriginator    = "codex_cli_rs"
	codexClientVersion = "0.144.1"
	codexUserAgent     = "jcode-codex-oauth"
)

func (manager *Manager) startCodex(ctx context.Context) (*pendingFlow, error) {
	if err := manager.validateVerificationURIs(
		manager.endpoints.CodexVerification, "", "auth.openai.com",
	); err != nil {
		return nil, err
	}
	status, value, err := manager.postJSON(
		ctx,
		manager.endpoints.CodexDeviceStart,
		map[string]string{"client_id": codexClientID},
		map[string]string{"User-Agent": codexUserAgent},
	)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, upstreamError("start ChatGPT device authorization", status, value)
	}
	deviceCode := stringField(value, "device_auth_id")
	userCode := stringField(value, "user_code")
	if deviceCode == "" || userCode == "" {
		return nil, errors.New("ChatGPT device authorization response is missing required fields")
	}
	expiresIn := clampSeconds(intField(value, "expires_in", 900), 24*60*60)
	interval := clampSeconds(intField(value, "interval", 5), 60) + 3
	now := manager.now()
	return &pendingFlow{
		public: Flow{
			Method:          MethodCodexOAuth,
			State:           FlowStatePending,
			UserCode:        userCode,
			VerificationURI: manager.endpoints.CodexVerification,
			ExpiresAt:       now.Add(time.Duration(expiresIn) * time.Second),
			IntervalSeconds: int(interval),
		},
		deviceCode: deviceCode,
		nextPollAt: now,
		interval:   time.Duration(interval) * time.Second,
	}, nil
}

func (manager *Manager) pollCodex(ctx context.Context, pending *pendingFlow) (Flow, error) {
	status, value, err := manager.postJSON(
		ctx,
		manager.endpoints.CodexDevicePoll,
		map[string]string{
			"device_auth_id": pending.deviceCode,
			"user_code":      pending.public.UserCode,
		},
		map[string]string{"User-Agent": codexUserAgent},
	)
	if err != nil {
		return Flow{}, err
	}
	switch status {
	case http.StatusForbidden, http.StatusNotFound:
		return pending.public, nil
	case http.StatusGone:
		return terminalFlow(pending.public, FlowStateExpired, ErrFlowExpired), nil
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return Flow{}, upstreamError("poll ChatGPT device authorization", status, value)
	}
	code := stringField(value, "authorization_code")
	verifier := stringField(value, "code_verifier")
	if code == "" || verifier == "" {
		return Flow{}, errors.New("ChatGPT device authorization poll response is missing required fields")
	}
	tokens, err := manager.exchangeCodexCode(ctx, code, verifier)
	if err != nil {
		return Flow{}, err
	}
	accountID, login := codexIdentity(tokens)
	if accountID == "" {
		return Flow{}, errors.New("ChatGPT token does not contain a stable account ID")
	}
	refresh := stringField(tokens, "refresh_token")
	access := stringField(tokens, "access_token")
	if refresh == "" || access == "" {
		return Flow{}, errors.New("ChatGPT token response is missing a required token")
	}
	account := storedAccount{
		ID: accountID, Login: login, Secret: refresh, AuthenticatedAt: manager.now().UTC(),
	}
	if account.Login == "" {
		account.Login = "ChatGPT (" + shortID(accountID) + ")"
	}
	if err := manager.commitFlowAccount(MethodCodexOAuth, pending, account); err != nil {
		return Flow{}, err
	}
	manager.cache(
		MethodCodexOAuth,
		accountID,
		access,
		tokenExpiry(manager.now, intField(tokens, "expires_in", 3600)),
	)
	flow := pending.public
	flow.State = FlowStateAuthorized
	public := account.public()
	flow.Account = &public
	return flow, nil
}

func (manager *Manager) exchangeCodexCode(
	ctx context.Context,
	code string,
	verifier string,
) (map[string]any, error) {
	status, value, err := manager.postForm(
		ctx,
		manager.endpoints.CodexToken,
		url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {codexRedirectURI},
			"client_id":     {codexClientID},
			"code_verifier": {verifier},
		},
		map[string]string{"User-Agent": codexUserAgent},
	)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, upstreamError("exchange ChatGPT authorization code", status, value)
	}
	return value, nil
}

func codexIdentity(tokens map[string]any) (string, string) {
	for _, token := range []string{stringField(tokens, "id_token"), stringField(tokens, "access_token")} {
		payload := jwtPayload(token)
		if payload == nil {
			continue
		}
		accountID := stringField(payload, "chatgpt_account_id")
		if accountID == "" {
			accountID = nestedString(payload, "https://api.openai.com/auth", "chatgpt_account_id")
		}
		if accountID == "" {
			if organizations, ok := payload["organizations"].([]any); ok && len(organizations) > 0 {
				organization, _ := organizations[0].(map[string]any)
				accountID = stringField(organization, "id")
			}
		}
		if accountID != "" {
			return accountID, stringField(payload, "email")
		}
	}
	return "", ""
}

func (manager *Manager) refreshCodex(
	ctx context.Context,
	account storedAccount,
) (string, error) {
	status, value, err := manager.postForm(
		ctx,
		manager.endpoints.CodexToken,
		url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {account.Secret},
			"client_id":     {codexClientID},
			"scope":         {"openid profile email"},
		},
		map[string]string{"User-Agent": codexUserAgent},
	)
	if err != nil {
		return "", err
	}
	code := oauthError(value)
	if status == http.StatusUnauthorized || status == http.StatusForbidden ||
		(status == http.StatusBadRequest && len(value) == 0) ||
		code == "invalid_grant" || code == "invalid_token" {
		if err := manager.markRequiresReauth(MethodCodexOAuth, account.ID, account.Secret); err != nil {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", ErrRequiresReauth, account.ID)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices || code != "" {
		return "", upstreamError("refresh ChatGPT access token", status, value)
	}
	access := stringField(value, "access_token")
	if access == "" {
		return "", errors.New("ChatGPT refresh response is missing access_token")
	}
	if err := manager.replaceSecret(
		MethodCodexOAuth, account.ID, account.Secret, stringField(value, "refresh_token"),
	); err != nil {
		return "", err
	}
	manager.cache(
		MethodCodexOAuth,
		account.ID,
		access,
		tokenExpiry(manager.now, intField(value, "expires_in", 3600)),
	)
	return access, nil
}

func clampSeconds(value, maximum int64) int64 {
	if value < 1 {
		return 1
	}
	if value > maximum {
		return maximum
	}
	return value
}

func terminalFlow(flow Flow, state FlowState, err error) Flow {
	flow.State = state
	flow.Error = err.Error()
	return flow
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}
