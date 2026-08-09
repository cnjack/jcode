package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/providerauth"
)

const providerAuthAPIKeyMethod = "api_key"

func providerAuthMethodsForID(s *Server, providerID string) []string {
	if s != nil && s.registry != nil {
		if provider := s.registry.GetProvider(providerID); provider != nil {
			if len(provider.AuthMethods) > 0 {
				return append([]string(nil), provider.AuthMethods...)
			}
		}
	}
	// Existing registry and custom providers predate AuthMethods and remain
	// ordinary API-key providers unless explicitly declared otherwise.
	return []string{providerAuthAPIKeyMethod}
}

func containsProviderAuthMethod(methods []string, method string) bool {
	for _, candidate := range methods {
		if candidate == method {
			return true
		}
	}
	return false
}

func (s *Server) validateProviderBinding(
	ctx context.Context,
	providerID string,
	binding *config.ProviderAuthBinding,
) (*config.ProviderAuthBinding, error) {
	methods := providerAuthMethodsForID(s, providerID)
	if binding == nil {
		if containsProviderAuthMethod(methods, providerAuthAPIKeyMethod) {
			return nil, nil
		}
		return nil, newConfigMutationHTTPError(
			http.StatusBadRequest,
			"this provider requires account login",
		)
	}
	normalized := &config.ProviderAuthBinding{
		Method:    strings.TrimSpace(binding.Method),
		AccountID: strings.TrimSpace(binding.AccountID),
	}
	method, err := parseProviderAuthMethod(normalized.Method)
	if err != nil || !containsProviderAuthMethod(methods, string(method)) {
		return nil, newConfigMutationHTTPError(
			http.StatusBadRequest,
			"authentication method is not supported by this provider",
		)
	}
	service, err := s.providerAuthService()
	if err != nil {
		return nil, newConfigMutationHTTPError(http.StatusInternalServerError, err.Error())
	}
	if err := service.ValidateBinding(ctx, providerauth.Binding{
		Method: method, AccountID: normalized.AccountID,
	}); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, providerauth.ErrAccountNotFound) ||
			errors.Is(err, providerauth.ErrRequiresReauth) {
			status = http.StatusConflict
		}
		return nil, newConfigMutationHTTPError(status, err.Error())
	}
	return normalized, nil
}

func (s *Server) providerAuthStatus(
	ctx context.Context,
	binding *config.ProviderAuthBinding,
) *providerauth.Status {
	if binding == nil {
		return nil
	}
	method, err := parseProviderAuthMethod(binding.Method)
	if err != nil {
		return nil
	}
	service, err := s.providerAuthService()
	if err != nil {
		config.Logger().Printf("[provider-auth] status unavailable for %s: %v", method, err)
		return nil
	}
	status, err := service.Status(ctx, method)
	if err != nil {
		config.Logger().Printf("[provider-auth] status failed for %s: %v", method, err)
		return nil
	}
	return &status
}

func (s *Server) providerAuthService() (ProviderAuthService, error) {
	s.providerAuthMu.Lock()
	defer s.providerAuthMu.Unlock()
	if s.providerAuth != nil || s.providerAuthErr != nil {
		return s.providerAuth, s.providerAuthErr
	}
	s.providerAuth, s.providerAuthErr = providerauth.Default(config.ConfigDir())
	return s.providerAuth, s.providerAuthErr
}

func parseProviderAuthMethod(raw string) (providerauth.Method, error) {
	method := providerauth.Method(strings.TrimSpace(raw))
	switch method {
	case providerauth.MethodCodexOAuth, providerauth.MethodXAIOAuth,
		providerauth.MethodGitHubCopilot:
		return method, nil
	default:
		return "", providerauth.ErrUnsupportedMethod
	}
}

func (s *Server) handleProviderAuthStatus(w http.ResponseWriter, r *http.Request) {
	method, err := parseProviderAuthMethod(r.PathValue("method"))
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	service, err := s.providerAuthService()
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	status, err := service.Status(r.Context(), method)
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleProviderAuthStart(w http.ResponseWriter, r *http.Request) {
	method, err := parseProviderAuthMethod(r.PathValue("method"))
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	// Consume an optional empty object so clients may consistently POST JSON.
	// No credential or endpoint input is accepted: driver policy is server-side.
	if r.Body != nil {
		var request map[string]json.RawMessage
		err = json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&request)
		if err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if len(request) != 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "managed login does not accept endpoint or token input"})
			return
		}
	}
	service, err := s.providerAuthService()
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	flow, err := service.Start(r.Context(), method)
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, flow)
}

func (s *Server) handleProviderAuthPoll(w http.ResponseWriter, r *http.Request) {
	method, err := parseProviderAuthMethod(r.PathValue("method"))
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	flowID := strings.TrimSpace(r.PathValue("flow_id"))
	if flowID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "flow_id is required"})
		return
	}
	service, err := s.providerAuthService()
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	flow, err := service.Poll(r.Context(), method, flowID)
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	if flow.Method != method {
		writeProviderAuthError(w, providerauth.ErrFlowNotFound)
		return
	}
	writeJSON(w, http.StatusOK, flow)
}

func (s *Server) handleProviderAuthCancel(w http.ResponseWriter, r *http.Request) {
	method, err := parseProviderAuthMethod(r.PathValue("method"))
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	service, err := s.providerAuthService()
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	if err := service.Cancel(method, strings.TrimSpace(r.PathValue("flow_id"))); err != nil {
		writeProviderAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleProviderAuthSetDefault(w http.ResponseWriter, r *http.Request) {
	method, err := parseProviderAuthMethod(r.PathValue("method"))
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	var request struct {
		AccountID string `json:"account_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&request); err != nil || strings.TrimSpace(request.AccountID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id is required"})
		return
	}
	service, err := s.providerAuthService()
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	if err := service.SetDefault(r.Context(), method, request.AccountID); err != nil {
		writeProviderAuthError(w, err)
		return
	}
	status, err := service.Status(r.Context(), method)
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleProviderAuthRemove(w http.ResponseWriter, r *http.Request) {
	method, err := parseProviderAuthMethod(r.PathValue("method"))
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	service, err := s.providerAuthService()
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	if err := service.Remove(r.Context(), method, strings.TrimSpace(r.PathValue("account_id"))); err != nil {
		writeProviderAuthError(w, err)
		return
	}
	status, err := service.Status(r.Context(), method)
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleProviderAuthLogout(w http.ResponseWriter, r *http.Request) {
	method, err := parseProviderAuthMethod(r.PathValue("method"))
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	service, err := s.providerAuthService()
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	if err := service.Logout(r.Context(), method); err != nil {
		writeProviderAuthError(w, err)
		return
	}
	status, err := service.Status(r.Context(), method)
	if err != nil {
		writeProviderAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func writeProviderAuthError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, providerauth.ErrUnsupportedMethod),
		errors.Is(err, providerauth.ErrUnsupportedGHES):
		status = http.StatusBadRequest
	case errors.Is(err, providerauth.ErrFlowNotFound),
		errors.Is(err, providerauth.ErrAccountNotFound):
		status = http.StatusNotFound
	case errors.Is(err, providerauth.ErrFlowExpired),
		errors.Is(err, providerauth.ErrAccessDenied),
		errors.Is(err, providerauth.ErrRequiresReauth):
		status = http.StatusConflict
	case errors.Is(err, providerauth.ErrAuthorizationPending):
		status = http.StatusTooEarly
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
