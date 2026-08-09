package providerauth

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	maxPendingFlows = 128
	refreshBuffer   = time.Minute
)

var productionEndpoints = Endpoints{
	CodexDeviceStart:   "https://auth.openai.com/api/accounts/deviceauth/usercode",
	CodexDevicePoll:    "https://auth.openai.com/api/accounts/deviceauth/token",
	CodexToken:         "https://auth.openai.com/oauth/token",
	CodexVerification:  "https://auth.openai.com/codex/device",
	CodexRuntime:       "https://chatgpt.com/backend-api/codex",
	XAIDiscovery:       "https://auth.x.ai/.well-known/openid-configuration",
	XAIRuntime:         "https://api.x.ai/v1",
	CopilotDeviceStart: "https://github.com/login/device/code",
	CopilotOAuthToken:  "https://github.com/login/oauth/access_token",
	CopilotUser:        "https://api.github.com/user",
	CopilotToken:       "https://api.github.com/copilot_internal/v2/token",
	CopilotUsage:       "https://api.github.com/copilot_internal/user",
	CopilotRuntime:     "https://api.githubcopilot.com",
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

func (token cachedToken) usable(now time.Time) bool {
	return token.token != "" && token.expiresAt.After(now.Add(refreshBuffer))
}

type pendingFlow struct {
	mu            sync.Mutex
	commitMu      sync.RWMutex
	cancelled     bool
	public        Flow
	deviceCode    string
	tokenEndpoint string
	nextPollAt    time.Time
	interval      time.Duration
	generation    uint64
}

// Manager coordinates device flows, durable accounts, token refresh and
// runtime credential resolution for all managed login methods.
type Manager struct {
	store       *fileStore
	client      *http.Client
	now         func() time.Time
	rand        io.Reader
	endpoints   Endpoints
	allowUnsafe bool

	mu               sync.RWMutex
	flows            map[string]*pendingFlow
	flowReservations int
	pendingFlowLimit int
	accessTokens     map[string]cachedToken
	copilotEndpoints map[string]string
	xaiEndpoints     *xaiOAuthEndpoints

	flowLifecycleMu sync.RWMutex
	randomMu        sync.Mutex
	refreshLocksMu  sync.Mutex
	refreshLocks    map[string]*sync.Mutex
	endpointLocks   map[string]*sync.Mutex
}

var defaultManagers struct {
	sync.Mutex
	byDir map[string]*Manager
}

// Default returns a process-wide Manager keyed by absolute config directory.
func Default(configDir string) (*Manager, error) {
	abs, err := filepath.Abs(configDir)
	if err != nil {
		return nil, fmt.Errorf("resolve provider auth config directory: %w", err)
	}
	key := filepath.Clean(abs)
	defaultManagers.Lock()
	defer defaultManagers.Unlock()
	if manager := defaultManagers.byDir[key]; manager != nil {
		return manager, nil
	}
	manager, err := NewManager(Options{ConfigDir: key})
	if err != nil {
		return nil, err
	}
	if defaultManagers.byDir == nil {
		defaultManagers.byDir = make(map[string]*Manager)
	}
	defaultManagers.byDir[key] = manager
	return manager, nil
}

// NewManager creates an isolated manager with injectable dependencies.
func NewManager(options Options) (*Manager, error) {
	store, err := newFileStore(options.ConfigDir)
	if err != nil {
		return nil, err
	}
	if err := store.secureDirectory(); err != nil {
		return nil, err
	}
	if err := store.secureExistingStore(); err != nil {
		return nil, err
	}
	if _, err := store.read(); err != nil {
		return nil, err
	}
	if options.Endpoints != (Endpoints{}) && !options.AllowInsecureTestEndpoints {
		return nil, errors.New("provider auth endpoint overrides require test mode")
	}
	endpoints := mergeEndpoints(productionEndpoints, options.Endpoints)
	client := cloneHTTPClient(options.HTTPClient)
	now := options.Now
	if now == nil {
		now = time.Now
	}
	random := options.Rand
	if random == nil {
		random = cryptorand.Reader
	}
	return &Manager{
		store:            store,
		client:           client,
		now:              now,
		rand:             random,
		endpoints:        endpoints,
		allowUnsafe:      options.AllowInsecureTestEndpoints,
		flows:            make(map[string]*pendingFlow),
		pendingFlowLimit: maxPendingFlows,
		accessTokens:     make(map[string]cachedToken),
		copilotEndpoints: make(map[string]string),
		refreshLocks:     make(map[string]*sync.Mutex),
		endpointLocks:    make(map[string]*sync.Mutex),
	}, nil
}

func cloneHTTPClient(source *http.Client) *http.Client {
	if source == nil {
		source = &http.Client{Timeout: 20 * time.Second}
	}
	clone := *source
	if clone.Timeout == 0 {
		clone.Timeout = 20 * time.Second
	}
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

func mergeEndpoints(base, overrides Endpoints) Endpoints {
	result := base
	fields := []*string{
		&result.CodexDeviceStart, &result.CodexDevicePoll, &result.CodexToken,
		&result.CodexVerification, &result.CodexRuntime, &result.XAIDiscovery,
		&result.XAIRuntime, &result.CopilotDeviceStart, &result.CopilotOAuthToken,
		&result.CopilotUser, &result.CopilotToken, &result.CopilotUsage,
		&result.CopilotRuntime,
	}
	values := []string{
		overrides.CodexDeviceStart, overrides.CodexDevicePoll, overrides.CodexToken,
		overrides.CodexVerification, overrides.CodexRuntime, overrides.XAIDiscovery,
		overrides.XAIRuntime, overrides.CopilotDeviceStart, overrides.CopilotOAuthToken,
		overrides.CopilotUser, overrides.CopilotToken, overrides.CopilotUsage,
		overrides.CopilotRuntime,
	}
	for index, value := range values {
		if value != "" {
			*fields[index] = value
		}
	}
	return result
}

func validateMethod(method Method) error {
	switch method {
	case MethodCodexOAuth, MethodXAIOAuth, MethodGitHubCopilot:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedMethod, method)
	}
}

func (manager *Manager) validateVerificationURIs(primary, complete, expectedHost string) error {
	if err := manager.validateVerificationURI(primary, expectedHost); err != nil {
		return err
	}
	if complete != "" {
		return manager.validateVerificationURI(complete, expectedHost)
	}
	return nil
}

func (manager *Manager) validateVerificationURI(raw, expectedHost string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return errors.New("provider authorization returned an untrusted verification URI")
	}
	if manager.allowUnsafe {
		if parsed.Scheme == "http" || parsed.Scheme == "https" {
			return nil
		}
		return errors.New("provider authorization returned an untrusted verification URI")
	}
	if parsed.Scheme != "https" || parsed.Port() != "" ||
		!strings.EqualFold(parsed.Hostname(), expectedHost) {
		return errors.New("provider authorization returned an untrusted verification URI")
	}
	return nil
}

// Start starts a provider device authorization flow.
func (manager *Manager) Start(ctx context.Context, method Method) (Flow, error) {
	if err := validateMethod(method); err != nil {
		return Flow{}, err
	}
	if !manager.reserveFlowSlot() {
		return Flow{}, errors.New("too many pending provider auth flows")
	}
	reserved := true
	defer func() {
		if reserved {
			manager.releaseFlowSlot()
		}
	}()

	manager.flowLifecycleMu.RLock()
	generation, err := manager.store.generation(method)
	manager.flowLifecycleMu.RUnlock()
	if err != nil {
		return Flow{}, err
	}

	var pending *pendingFlow
	switch method {
	case MethodCodexOAuth:
		pending, err = manager.startCodex(ctx)
	case MethodXAIOAuth:
		pending, err = manager.startXAI(ctx)
	case MethodGitHubCopilot:
		pending, err = manager.startCopilot(ctx)
	}
	if err != nil {
		return Flow{}, err
	}
	pending.generation = generation
	id, err := manager.randomID()
	if err != nil {
		return Flow{}, err
	}
	pending.public.ID = id

	// Logout holds the write side across its durable epoch bump and local-flow
	// cancellation. Rechecking under the read side prevents a Start that was in
	// flight during Logout from installing an already-invalid pending flow.
	manager.flowLifecycleMu.RLock()
	defer manager.flowLifecycleMu.RUnlock()
	currentGeneration, err := manager.store.generation(method)
	if err != nil {
		return Flow{}, err
	}
	if currentGeneration != generation {
		return Flow{}, ErrFlowNotFound
	}
	manager.mu.Lock()
	if _, exists := manager.flows[id]; exists {
		manager.mu.Unlock()
		return Flow{}, errors.New("provider auth flow ID collision")
	}
	manager.flowReservations--
	manager.flows[id] = pending
	reserved = false
	manager.mu.Unlock()
	return pending.public, nil
}

// Poll advances a device authorization flow by one upstream poll.
func (manager *Manager) Poll(ctx context.Context, method Method, flowID string) (Flow, error) {
	if err := validateMethod(method); err != nil {
		return Flow{}, err
	}
	manager.mu.RLock()
	pending := manager.flows[flowID]
	if pending != nil && pending.public.Method != method {
		pending = nil
	}
	manager.mu.RUnlock()
	if pending == nil {
		return Flow{}, ErrFlowNotFound
	}
	pending.mu.Lock()
	defer pending.mu.Unlock()
	if !manager.flowStillCurrent(method, flowID, pending) {
		return Flow{}, ErrFlowNotFound
	}
	now := manager.now()
	if !now.Before(pending.public.ExpiresAt) {
		flow := pending.public
		flow.State = FlowStateExpired
		flow.Error = ErrFlowExpired.Error()
		manager.deleteFlow(flowID, pending)
		return flow, nil
	}
	if now.Before(pending.nextPollAt) {
		return pending.public, nil
	}
	pending.nextPollAt = now.Add(pending.interval)

	var result Flow
	var err error
	switch method {
	case MethodCodexOAuth:
		result, err = manager.pollCodex(ctx, pending)
	case MethodXAIOAuth:
		result, err = manager.pollXAI(ctx, pending)
	case MethodGitHubCopilot:
		result, err = manager.pollCopilot(ctx, pending)
	default:
		err = fmt.Errorf("%w: %q", ErrUnsupportedMethod, pending.public.Method)
	}
	if err != nil {
		if errors.Is(err, ErrFlowNotFound) {
			manager.deleteFlow(flowID, pending)
		}
		return Flow{}, err
	}
	if result.State != FlowStatePending {
		manager.deleteFlow(flowID, pending)
	}
	return result, nil
}

// Cancel destroys an in-memory device authorization flow.
func (manager *Manager) Cancel(method Method, flowID string) error {
	if err := validateMethod(method); err != nil {
		return err
	}
	manager.mu.RLock()
	pending := manager.flows[flowID]
	if pending != nil && pending.public.Method != method {
		pending = nil
	}
	manager.mu.RUnlock()
	if pending == nil {
		return ErrFlowNotFound
	}
	return manager.cancelPending(method, flowID, pending)
}

func (manager *Manager) cancelPending(method Method, flowID string, pending *pendingFlow) error {
	pending.commitMu.Lock()
	defer pending.commitMu.Unlock()
	manager.mu.Lock()
	if manager.flows[flowID] != pending || pending.public.Method != method {
		manager.mu.Unlock()
		return ErrFlowNotFound
	}
	pending.cancelled = true
	delete(manager.flows, flowID)
	manager.mu.Unlock()
	return nil
}

// Status returns non-secret account information for one login method.
func (manager *Manager) Status(_ context.Context, method Method) (Status, error) {
	if err := validateMethod(method); err != nil {
		return Status{}, err
	}
	return manager.store.status(method)
}

// SetDefault selects the default usable account for a login method.
func (manager *Manager) SetDefault(_ context.Context, method Method, accountID string) error {
	if err := validateMethod(method); err != nil {
		return err
	}
	if strings.TrimSpace(accountID) == "" {
		return fmt.Errorf("%w: account_id is required", ErrInvalidBinding)
	}
	return manager.store.mutate(func(state *storedState) error {
		entry := state.method(method)
		account, ok := entry.Accounts[accountID]
		if !ok {
			return fmt.Errorf("%w: %s/%s", ErrAccountNotFound, method, accountID)
		}
		if account.RequiresReauth {
			return fmt.Errorf("%w: %s/%s", ErrRequiresReauth, method, accountID)
		}
		entry.DefaultAccountID = accountID
		return nil
	})
}

// Remove removes one durable account without changing Provider bindings.
func (manager *Manager) Remove(_ context.Context, method Method, accountID string) error {
	if err := validateMethod(method); err != nil {
		return err
	}
	err := manager.store.mutate(func(state *storedState) error {
		entry := state.method(method)
		if _, ok := entry.Accounts[accountID]; !ok {
			return fmt.Errorf("%w: %s/%s", ErrAccountNotFound, method, accountID)
		}
		delete(entry.Accounts, accountID)
		if entry.DefaultAccountID == accountID {
			entry.DefaultAccountID = fallbackDefault(entry.Accounts)
		}
		return nil
	})
	if err == nil {
		manager.invalidate(method, accountID)
	}
	return err
}

// Logout removes all accounts and pending flows for a login method.
func (manager *Manager) Logout(_ context.Context, method Method) error {
	if err := validateMethod(method); err != nil {
		return err
	}
	manager.flowLifecycleMu.Lock()
	defer manager.flowLifecycleMu.Unlock()
	if err := manager.store.mutate(func(state *storedState) error {
		entry := state.method(method)
		if entry.Generation == ^uint64(0) {
			return errors.New("provider auth logout generation exhausted")
		}
		entry.Generation++
		entry.Accounts = make(map[string]storedAccount)
		entry.DefaultAccountID = ""
		return nil
	}); err != nil {
		return err
	}
	manager.cancelFlowsForMethod(method)
	manager.mu.Lock()
	prefix := string(method) + "\x00"
	for key := range manager.accessTokens {
		if strings.HasPrefix(key, prefix) {
			delete(manager.accessTokens, key)
		}
	}
	for key := range manager.copilotEndpoints {
		if strings.HasPrefix(key, prefix) {
			delete(manager.copilotEndpoints, key)
		}
	}
	manager.mu.Unlock()
	return nil
}

// ValidateBinding verifies that a binding resolves to a usable local account.
func (manager *Manager) ValidateBinding(_ context.Context, binding Binding) error {
	if err := validateMethod(binding.Method); err != nil {
		return err
	}
	_, err := manager.store.resolve(binding)
	return err
}

func (manager *Manager) upsertAccount(method Method, account storedAccount) error {
	return manager.store.mutate(func(state *storedState) error {
		entry := state.method(method)
		entry.Accounts[account.ID] = account
		if current, ok := entry.Accounts[entry.DefaultAccountID]; entry.DefaultAccountID == "" ||
			!ok || current.RequiresReauth {
			entry.DefaultAccountID = account.ID
		}
		return nil
	})
}

// commitFlowAccount makes cancellation and durable flow completion linearizable:
// Cancel can win while an upstream poll is in flight, while a commit that has
// already acquired the read side completes before Cancel returns.
func (manager *Manager) commitFlowAccount(
	method Method,
	pending *pendingFlow,
	account storedAccount,
) error {
	pending.commitMu.RLock()
	defer pending.commitMu.RUnlock()
	if pending.cancelled || !manager.flowStillCurrent(method, pending.public.ID, pending) {
		return ErrFlowNotFound
	}
	return manager.store.mutate(func(state *storedState) error {
		entry := state.method(method)
		if entry.Generation != pending.generation {
			return ErrFlowNotFound
		}
		entry.Accounts[account.ID] = account
		if current, ok := entry.Accounts[entry.DefaultAccountID]; entry.DefaultAccountID == "" ||
			!ok || current.RequiresReauth {
			entry.DefaultAccountID = account.ID
		}
		return nil
	})
}

func (manager *Manager) replaceSecret(
	method Method,
	accountID string,
	expected string,
	replacement string,
) error {
	if replacement == "" || replacement == expected {
		return manager.store.compareSecret(method, accountID, expected)
	}
	return manager.store.mutate(func(state *storedState) error {
		entry := state.method(method)
		account, ok := entry.Accounts[accountID]
		if !ok {
			return fmt.Errorf("%w: %s/%s", ErrAccountNotFound, method, accountID)
		}
		if account.Secret != expected {
			return errSecretChanged
		}
		account.Secret = replacement
		account.RequiresReauth = false
		entry.Accounts[accountID] = account
		return nil
	})
}

func (manager *Manager) markRequiresReauth(method Method, accountID, expected string) error {
	err := manager.store.mutate(func(state *storedState) error {
		entry := state.method(method)
		account, ok := entry.Accounts[accountID]
		if !ok {
			return fmt.Errorf("%w: %s/%s", ErrAccountNotFound, method, accountID)
		}
		if account.Secret != expected {
			return errSecretChanged
		}
		account.RequiresReauth = true
		entry.Accounts[accountID] = account
		if entry.DefaultAccountID == accountID {
			entry.DefaultAccountID = fallbackDefault(entry.Accounts)
		}
		return nil
	})
	if err == nil {
		manager.invalidate(method, accountID)
	}
	return err
}

func (manager *Manager) invalidate(method Method, accountID string) {
	key := accountKey(method, accountID)
	manager.mu.Lock()
	delete(manager.accessTokens, key)
	delete(manager.copilotEndpoints, key)
	manager.mu.Unlock()
}

func (manager *Manager) randomID() (string, error) {
	bytes := make([]byte, 32)
	manager.randomMu.Lock()
	_, err := io.ReadFull(manager.rand, bytes)
	manager.randomMu.Unlock()
	if err != nil {
		return "", fmt.Errorf("generate provider auth flow ID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func (manager *Manager) randomUUID() (string, error) {
	manager.randomMu.Lock()
	id, err := uuid.NewRandomFromReader(manager.rand)
	manager.randomMu.Unlock()
	if err != nil {
		return "", fmt.Errorf("generate provider request ID: %w", err)
	}
	return id.String(), nil
}

func (manager *Manager) reserveFlowSlot() bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.cleanupFlowsLocked()
	limit := manager.pendingFlowLimit
	if limit <= 0 {
		limit = maxPendingFlows
	}
	if len(manager.flows)+manager.flowReservations >= limit {
		return false
	}
	manager.flowReservations++
	return true
}

func (manager *Manager) releaseFlowSlot() {
	manager.mu.Lock()
	if manager.flowReservations > 0 {
		manager.flowReservations--
	}
	manager.mu.Unlock()
}

func (manager *Manager) cleanupFlowsLocked() {
	now := manager.now()
	for id, flow := range manager.flows {
		if !now.Before(flow.public.ExpiresAt) {
			delete(manager.flows, id)
		}
	}
}

func (manager *Manager) flowStillCurrent(method Method, id string, expected *pendingFlow) bool {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.flows[id] == expected && expected.public.Method == method
}

func (manager *Manager) cancelFlowsForMethod(method Method) {
	type flowEntry struct {
		id      string
		pending *pendingFlow
	}
	manager.mu.RLock()
	flows := make([]flowEntry, 0)
	for id, pending := range manager.flows {
		if pending.public.Method == method {
			flows = append(flows, flowEntry{id: id, pending: pending})
		}
	}
	manager.mu.RUnlock()
	for _, flow := range flows {
		_ = manager.cancelPending(method, flow.id, flow.pending)
	}
}

func (manager *Manager) deleteFlow(id string, expected *pendingFlow) {
	manager.mu.Lock()
	if manager.flows[id] == expected {
		delete(manager.flows, id)
	}
	manager.mu.Unlock()
}

func (manager *Manager) refreshLock(method Method, accountID string) *sync.Mutex {
	key := accountKey(method, accountID)
	manager.refreshLocksMu.Lock()
	defer manager.refreshLocksMu.Unlock()
	lock := manager.refreshLocks[key]
	if lock == nil {
		lock = new(sync.Mutex)
		manager.refreshLocks[key] = lock
	}
	return lock
}

func (manager *Manager) endpointLock(method Method, accountID string) *sync.Mutex {
	key := accountKey(method, accountID)
	manager.refreshLocksMu.Lock()
	defer manager.refreshLocksMu.Unlock()
	lock := manager.endpointLocks[key]
	if lock == nil {
		lock = new(sync.Mutex)
		manager.endpointLocks[key] = lock
	}
	return lock
}

func (manager *Manager) cached(method Method, accountID string) (string, bool) {
	manager.mu.RLock()
	token := manager.accessTokens[accountKey(method, accountID)]
	manager.mu.RUnlock()
	if token.usable(manager.now()) {
		return token.token, true
	}
	return "", false
}

func (manager *Manager) cache(method Method, accountID, token string, expiresAt time.Time) {
	manager.mu.Lock()
	manager.accessTokens[accountKey(method, accountID)] = cachedToken{
		token: token, expiresAt: expiresAt,
	}
	manager.mu.Unlock()
}

func accountKey(method Method, accountID string) string {
	return string(method) + "\x00" + accountID
}
