package providerauth

import (
	"context"
	"errors"
	"fmt"
)

// Credential resolves a fresh runtime token and the immutable managed runtime
// profile for a Provider binding.
func (manager *Manager) Credential(ctx context.Context, binding Binding) (Credential, error) {
	if err := validateMethod(binding.Method); err != nil {
		return Credential{}, err
	}
	account, err := manager.store.resolve(binding)
	if err != nil {
		return Credential{}, err
	}
	token, err := manager.tokenForAccount(ctx, binding.Method, account.ID)
	if err != nil {
		return Credential{}, err
	}
	credential := Credential{Token: token, AccountID: account.ID}
	switch binding.Method {
	case MethodCodexOAuth:
		credential.BaseURL = manager.endpoints.CodexRuntime
		credential.Protocol = ProtocolResponses
		credential.Headers = map[string]string{
			"chatgpt-account-id": account.ID,
			"originator":         codexOriginator,
			"version":            codexClientVersion,
		}
	case MethodXAIOAuth:
		credential.BaseURL = manager.endpoints.XAIRuntime
		credential.Protocol = ProtocolResponses
	case MethodGitHubCopilot:
		latest, resolveErr := manager.store.resolve(Binding{
			Method: MethodGitHubCopilot, AccountID: account.ID,
		})
		if resolveErr != nil {
			return Credential{}, resolveErr
		}
		credential.BaseURL, err = manager.copilotEndpoint(ctx, latest)
		if err != nil {
			return Credential{}, err
		}
		credential.Protocol = ProtocolChatCompletions
		requestID, randomErr := manager.randomUUID()
		if randomErr != nil {
			return Credential{}, randomErr
		}
		credential.Headers = copilotRuntimeHeaders(requestID)
	default:
		return Credential{}, fmt.Errorf("%w: %q", ErrUnsupportedMethod, binding.Method)
	}
	return credential, nil
}

func (manager *Manager) tokenForAccount(
	ctx context.Context,
	method Method,
	accountID string,
) (string, error) {
	if token, ok := manager.cached(method, accountID); ok {
		return token, nil
	}
	lock := manager.refreshLock(method, accountID)
	lock.Lock()
	defer lock.Unlock()
	if token, ok := manager.cached(method, accountID); ok {
		return token, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		account, err := manager.store.resolve(Binding{Method: method, AccountID: accountID})
		if err != nil {
			return "", err
		}
		var token string
		switch method {
		case MethodCodexOAuth:
			token, err = manager.refreshCodex(ctx, account)
		case MethodXAIOAuth:
			token, err = manager.refreshXAI(ctx, account)
		case MethodGitHubCopilot:
			token, err = manager.refreshCopilot(ctx, account)
		default:
			return "", fmt.Errorf("%w: %q", ErrUnsupportedMethod, method)
		}
		if errors.Is(err, errSecretChanged) {
			manager.invalidate(method, accountID)
			continue
		}
		return token, err
	}
	return "", errors.New("provider auth secret changed repeatedly during refresh")
}

func copilotRuntimeHeaders(requestID string) map[string]string {
	return map[string]string{
		"editor-version":                      copilotEditorVersion,
		"editor-plugin-version":               copilotPluginVersion,
		"copilot-integration-id":              copilotIntegrationID,
		"user-agent":                          copilotUserAgent,
		"x-github-api-version":                copilotAPIVersion,
		"openai-intent":                       "conversation-agent",
		"x-initiator":                         "user",
		"x-interaction-type":                  "conversation-agent",
		"x-vscode-user-agent-library-version": "electron-fetch",
		"x-request-id":                        requestID,
		"x-agent-task-id":                     requestID,
	}
}
