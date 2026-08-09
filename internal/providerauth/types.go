// Package providerauth manages OAuth-backed provider accounts without exposing
// durable secrets to provider configuration or UI/API callers.
package providerauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// Method identifies a managed provider login mechanism.
type Method string

const (
	MethodCodexOAuth    Method = "codex_oauth"
	MethodXAIOAuth      Method = "xai_oauth"
	MethodGitHubCopilot Method = "github_copilot"
)

// Protocol is the wire protocol required by a managed provider runtime.
type Protocol string

const (
	ProtocolResponses       Protocol = "responses"
	ProtocolChatCompletions Protocol = "chat_completions"
)

// FlowState is the public state of a device authorization flow.
type FlowState string

const (
	FlowStatePending    FlowState = "pending"
	FlowStateAuthorized FlowState = "authorized"
	FlowStateDenied     FlowState = "denied"
	FlowStateExpired    FlowState = "expired"
)

// Binding is the non-secret value stored on a Provider configuration.
type Binding struct {
	Method    Method `json:"method"`
	AccountID string `json:"account_id,omitempty"`
}

// Account is the non-secret account projection exposed to clients.
type Account struct {
	ID              string    `json:"id"`
	Login           string    `json:"login"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
	RequiresReauth  bool      `json:"requires_reauth"`
}

// Flow is the public device-flow projection. Upstream device tokens and PKCE
// material are deliberately absent and remain in process memory only.
type Flow struct {
	ID                      string    `json:"flow_id"`
	Method                  Method    `json:"method"`
	State                   FlowState `json:"state"`
	UserCode                string    `json:"user_code"`
	VerificationURI         string    `json:"verification_uri"`
	VerificationURIComplete string    `json:"verification_uri_complete,omitempty"`
	ExpiresAt               time.Time `json:"expires_at"`
	IntervalSeconds         int       `json:"interval_seconds,omitempty"`
	Account                 *Account  `json:"account,omitempty"`
	Error                   string    `json:"error,omitempty"`
}

// Status describes all locally stored accounts for one login method.
type Status struct {
	Method           Method    `json:"method"`
	Accounts         []Account `json:"accounts"`
	DefaultAccountID string    `json:"default_account_id,omitempty"`
	Authenticated    bool      `json:"authenticated"`
}

// Credential is resolved immediately before an upstream request. Token is
// intentionally excluded from JSON serialization to prevent accidental API or
// log exposure.
type Credential struct {
	Token     string            `json:"-"`
	AccountID string            `json:"account_id"`
	BaseURL   string            `json:"base_url"`
	Protocol  Protocol          `json:"protocol"`
	Headers   map[string]string `json:"headers,omitempty"`
}

var (
	ErrUnsupportedMethod     = errors.New("unsupported provider auth method")
	ErrUnsupportedGHES       = errors.New("GitHub Enterprise Server is not supported")
	ErrFlowNotFound          = errors.New("provider auth flow not found")
	ErrFlowExpired           = errors.New("provider auth flow expired")
	ErrAuthorizationPending  = errors.New("provider authorization pending")
	ErrAccessDenied          = errors.New("provider authorization denied")
	ErrAccountNotFound       = errors.New("provider auth account not found")
	ErrRequiresReauth        = errors.New("provider auth account requires reauthentication")
	ErrNoCopilotSubscription = errors.New("GitHub account has no Copilot subscription")
	ErrInvalidBinding        = errors.New("invalid provider auth binding")
)

// Endpoints contains managed upstream endpoints. The zero value selects the
// production endpoints. Non-zero overrides are intended only for hermetic
// tests and require AllowInsecureTestEndpoints when they are not trusted HTTPS
// origins.
type Endpoints struct {
	CodexDeviceStart   string
	CodexDevicePoll    string
	CodexToken         string
	CodexVerification  string
	CodexRuntime       string
	XAIDiscovery       string
	XAIRuntime         string
	CopilotDeviceStart string
	CopilotOAuthToken  string
	CopilotUser        string
	CopilotToken       string
	CopilotUsage       string
	CopilotRuntime     string
}

// Options supplies dependencies for Manager. ConfigDir is required. HTTPClient,
// Now and Rand are injectable to keep tests deterministic and offline.
type Options struct {
	ConfigDir                  string
	HTTPClient                 *http.Client
	Now                        func() time.Time
	Rand                       io.Reader
	Endpoints                  Endpoints
	AllowInsecureTestEndpoints bool
}

// Service is the consuming-package-friendly contract implemented by Manager.
type Service interface {
	Start(context.Context, Method) (Flow, error)
	Poll(context.Context, Method, string) (Flow, error)
	Cancel(Method, string) error
	Status(context.Context, Method) (Status, error)
	SetDefault(context.Context, Method, string) error
	Remove(context.Context, Method, string) error
	Logout(context.Context, Method) error
	ValidateBinding(context.Context, Binding) error
	Credential(context.Context, Binding) (Credential, error)
}
