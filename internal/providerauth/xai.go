package providerauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	xaiIssuer           = "https://auth.x.ai"
	xaiAuthHost         = "auth.x.ai"
	xaiVerificationHost = "accounts.x.ai"
	xaiClientID         = "b1a00492-073a-47ea-816f-4c329264a828"
	xaiScope            = "openid profile email offline_access grok-cli:access api:access"
	xaiUserAgent        = "jcode-xai-oauth"
)

type xaiOAuthEndpoints struct {
	device string
	token  string
}

func (manager *Manager) startXAI(ctx context.Context) (*pendingFlow, error) {
	endpoints, err := manager.discoverXAI(ctx)
	if err != nil {
		return nil, err
	}
	status, value, err := manager.postForm(
		ctx,
		endpoints.device,
		url.Values{"client_id": {xaiClientID}, "scope": {xaiScope}},
		map[string]string{"User-Agent": xaiUserAgent},
	)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, upstreamError("start xAI device authorization", status, value)
	}
	deviceCode := stringField(value, "device_code")
	userCode := stringField(value, "user_code")
	verificationURI := stringField(value, "verification_uri")
	verificationComplete := stringField(value, "verification_uri_complete")
	if deviceCode == "" || userCode == "" || verificationURI == "" {
		return nil, errors.New("xAI device authorization response is missing required fields")
	}
	if err := manager.validateVerificationURIs(
		verificationURI, verificationComplete, xaiVerificationHost,
	); err != nil {
		return nil, err
	}
	expiresIn := clampSeconds(intField(value, "expires_in", 900), 24*60*60)
	interval := clampSeconds(intField(value, "interval", 5), 60) + 3
	now := manager.now()
	return &pendingFlow{
		public: Flow{
			Method:                  MethodXAIOAuth,
			State:                   FlowStatePending,
			UserCode:                userCode,
			VerificationURI:         verificationURI,
			VerificationURIComplete: verificationComplete,
			ExpiresAt:               now.Add(time.Duration(expiresIn) * time.Second),
			IntervalSeconds:         int(interval),
		},
		deviceCode:    deviceCode,
		tokenEndpoint: endpoints.token,
		nextPollAt:    now,
		interval:      time.Duration(interval) * time.Second,
	}, nil
}

func (manager *Manager) discoverXAI(ctx context.Context) (xaiOAuthEndpoints, error) {
	manager.mu.RLock()
	cached := manager.xaiEndpoints
	manager.mu.RUnlock()
	if cached != nil {
		return *cached, nil
	}
	status, value, err := manager.getJSON(
		ctx, manager.endpoints.XAIDiscovery, map[string]string{"User-Agent": xaiUserAgent},
	)
	if err != nil {
		return xaiOAuthEndpoints{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return xaiOAuthEndpoints{}, upstreamError("discover xAI OAuth endpoints", status, value)
	}
	if strings.TrimSuffix(stringField(value, "issuer"), "/") != xaiIssuer {
		return xaiOAuthEndpoints{}, errors.New("xAI discovery issuer does not match https://auth.x.ai")
	}
	endpoints := xaiOAuthEndpoints{
		device: stringField(value, "device_authorization_endpoint"),
		token:  stringField(value, "token_endpoint"),
	}
	if endpoints.device == "" || endpoints.token == "" {
		return xaiOAuthEndpoints{}, errors.New("xAI discovery response is missing required endpoints")
	}
	if !manager.allowUnsafe {
		if err := validateManagedAuthEndpoint(endpoints.device, xaiAuthHost); err != nil {
			return xaiOAuthEndpoints{}, err
		}
		if err := validateManagedAuthEndpoint(endpoints.token, xaiAuthHost); err != nil {
			return xaiOAuthEndpoints{}, err
		}
	}
	manager.mu.Lock()
	if manager.xaiEndpoints == nil {
		copy := endpoints
		manager.xaiEndpoints = &copy
	} else {
		endpoints = *manager.xaiEndpoints
	}
	manager.mu.Unlock()
	return endpoints, nil
}

func validateManagedAuthEndpoint(endpoint, host string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != host ||
		parsed.Port() != "" || parsed.User != nil {
		return errors.New("managed OAuth discovery returned an untrusted endpoint")
	}
	return nil
}

func (manager *Manager) pollXAI(ctx context.Context, pending *pendingFlow) (Flow, error) {
	status, value, err := manager.postForm(
		ctx,
		pending.tokenEndpoint,
		url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {xaiClientID},
			"device_code": {pending.deviceCode},
		},
		map[string]string{"User-Agent": xaiUserAgent},
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
		return Flow{}, upstreamError("poll xAI device authorization", status, value)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return Flow{}, upstreamError("poll xAI device authorization", status, value)
	}
	access := stringField(value, "access_token")
	refresh := stringField(value, "refresh_token")
	if access == "" || refresh == "" {
		return Flow{}, errors.New("xAI token response is missing a required token")
	}
	accountID, login := xaiIdentity(value)
	if accountID == "" {
		return Flow{}, errors.New("xAI token does not contain a stable sub claim")
	}
	account := storedAccount{
		ID: accountID, Login: login, Secret: refresh, AuthenticatedAt: manager.now().UTC(),
	}
	if account.Login == "" {
		account.Login = "xAI (" + shortID(accountID) + ")"
	}
	if err := manager.commitFlowAccount(MethodXAIOAuth, pending, account); err != nil {
		return Flow{}, err
	}
	manager.cache(
		MethodXAIOAuth,
		accountID,
		access,
		tokenExpiry(manager.now, intField(value, "expires_in", 3600)),
	)
	flow := pending.public
	flow.State = FlowStateAuthorized
	public := account.public()
	flow.Account = &public
	return flow, nil
}

func xaiIdentity(tokens map[string]any) (string, string) {
	for _, token := range []string{stringField(tokens, "id_token"), stringField(tokens, "access_token")} {
		payload := jwtPayload(token)
		if payload == nil {
			continue
		}
		accountID := stringField(payload, "sub")
		if accountID == "" {
			continue
		}
		for _, field := range []string{"email", "preferred_username", "name"} {
			if login := stringField(payload, field); login != "" {
				return accountID, login
			}
		}
		return accountID, ""
	}
	return "", ""
}

func (manager *Manager) refreshXAI(ctx context.Context, account storedAccount) (string, error) {
	endpoints, err := manager.discoverXAI(ctx)
	if err != nil {
		return "", err
	}
	status, value, err := manager.postForm(
		ctx,
		endpoints.token,
		url.Values{
			"grant_type":    {"refresh_token"},
			"client_id":     {xaiClientID},
			"refresh_token": {account.Secret},
			"scope":         {xaiScope},
		},
		map[string]string{"User-Agent": xaiUserAgent},
	)
	if err != nil {
		return "", err
	}
	code := oauthError(value)
	if status == http.StatusUnauthorized || status == http.StatusForbidden ||
		(status == http.StatusBadRequest && len(value) == 0) ||
		code == "invalid_grant" || code == "invalid_token" {
		if err := manager.markRequiresReauth(MethodXAIOAuth, account.ID, account.Secret); err != nil {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", ErrRequiresReauth, account.ID)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices || code != "" {
		return "", upstreamError("refresh xAI access token", status, value)
	}
	access := stringField(value, "access_token")
	if access == "" {
		return "", errors.New("xAI refresh response is missing access_token")
	}
	if err := manager.replaceSecret(
		MethodXAIOAuth, account.ID, account.Secret, stringField(value, "refresh_token"),
	); err != nil {
		return "", err
	}
	manager.cache(
		MethodXAIOAuth,
		account.ID,
		access,
		tokenExpiry(manager.now, intField(value, "expires_in", 3600)),
	)
	return access, nil
}
