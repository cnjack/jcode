package providerauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	storeFileName = "provider-auth.json"
	lockFileName  = "provider-auth.lock"
	storeVersion  = 1
)

var errSecretChanged = errors.New("provider auth durable secret changed")

type storedAccount struct {
	ID              string    `json:"id"`
	Login           string    `json:"login"`
	Secret          string    `json:"secret"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
	RequiresReauth  bool      `json:"requires_reauth,omitempty"`
}

func (account storedAccount) public() Account {
	return Account{
		ID:              account.ID,
		Login:           account.Login,
		AuthenticatedAt: account.AuthenticatedAt,
		RequiresReauth:  account.RequiresReauth,
	}
}

type storedMethod struct {
	Accounts         map[string]storedAccount `json:"accounts"`
	DefaultAccountID string                   `json:"default_account_id,omitempty"`
	// Generation is a durable logout epoch. Device flows capture it at Start and
	// may commit an account only while it still matches, preventing a flow in
	// another process from restoring accounts after Logout has returned.
	Generation uint64 `json:"generation,omitempty"`
}

type storedState struct {
	Version int                      `json:"version"`
	Methods map[Method]*storedMethod `json:"methods"`
}

func newStoredState() storedState {
	return storedState{Version: storeVersion, Methods: make(map[Method]*storedMethod)}
}

func (state *storedState) method(method Method) *storedMethod {
	entry := state.Methods[method]
	if entry == nil {
		entry = &storedMethod{Accounts: make(map[string]storedAccount)}
		state.Methods[method] = entry
	}
	if entry.Accounts == nil {
		entry.Accounts = make(map[string]storedAccount)
	}
	return entry
}

type fileStore struct {
	dir        string
	path       string
	lockPath   string
	mutationMu sync.Mutex
}

func newFileStore(configDir string) (*fileStore, error) {
	if configDir == "" {
		return nil, errors.New("provider auth config directory is required")
	}
	abs, err := filepath.Abs(configDir)
	if err != nil {
		return nil, fmt.Errorf("resolve provider auth config directory: %w", err)
	}
	return &fileStore{
		dir:      filepath.Clean(abs),
		path:     filepath.Join(abs, storeFileName),
		lockPath: filepath.Join(abs, lockFileName),
	}, nil
}

func (store *fileStore) secureDirectory() error {
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		return fmt.Errorf("create provider auth directory: %w", err)
	}
	if err := os.Chmod(store.dir, 0o700); err != nil {
		return fmt.Errorf("secure provider auth directory: %w", err)
	}
	return nil
}

func (store *fileStore) secureExistingStore() error {
	info, err := os.Lstat(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect provider auth store: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("provider auth store must be a regular file")
	}
	if err := os.Chmod(store.path, 0o600); err != nil {
		return fmt.Errorf("secure existing provider auth store: %w", err)
	}
	return nil
}

func (store *fileStore) read() (storedState, error) {
	state := newStoredState()
	data, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("read provider auth store: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("decode provider auth store: %w", err)
	}
	if state.Version != storeVersion {
		return state, fmt.Errorf("unsupported provider auth store version %d", state.Version)
	}
	if state.Methods == nil {
		state.Methods = make(map[Method]*storedMethod)
	}
	return state, nil
}

func (store *fileStore) mutate(fn func(*storedState) error) error {
	store.mutationMu.Lock()
	defer store.mutationMu.Unlock()

	if err := store.secureDirectory(); err != nil {
		return err
	}
	lock, err := acquireFileLock(store.lockPath)
	if err != nil {
		return fmt.Errorf("lock provider auth store: %w", err)
	}
	defer func() { _ = lock.release() }()

	state, err := store.read()
	if err != nil {
		return err
	}
	if err := fn(&state); err != nil {
		return err
	}
	return store.write(state)
}

func (store *fileStore) compareSecret(method Method, accountID, expected string) error {
	store.mutationMu.Lock()
	defer store.mutationMu.Unlock()
	if err := store.secureDirectory(); err != nil {
		return err
	}
	lock, err := acquireFileLock(store.lockPath)
	if err != nil {
		return fmt.Errorf("lock provider auth store: %w", err)
	}
	defer func() { _ = lock.release() }()
	state, err := store.read()
	if err != nil {
		return err
	}
	account, ok := state.method(method).Accounts[accountID]
	if !ok {
		return fmt.Errorf("%w: %s/%s", ErrAccountNotFound, method, accountID)
	}
	if account.RequiresReauth {
		return fmt.Errorf("%w: %s/%s", ErrRequiresReauth, method, accountID)
	}
	if account.Secret != expected {
		return errSecretChanged
	}
	return nil
}

func (store *fileStore) generation(method Method) (uint64, error) {
	state, err := store.read()
	if err != nil {
		return 0, err
	}
	entry := state.Methods[method]
	if entry == nil {
		return 0, nil
	}
	return entry.Generation, nil
}

func (store *fileStore) write(state storedState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode provider auth store: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(store.dir, ".provider-auth.tmp-*")
	if err != nil {
		return fmt.Errorf("create provider auth temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("secure provider auth temporary file: %w", err)
	}
	if n, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write provider auth temporary file: %w", err)
	} else if n != len(data) {
		return fmt.Errorf("write provider auth temporary file: %w", io.ErrShortWrite)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync provider auth temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close provider auth temporary file: %w", err)
	}
	if err := replaceFile(tmpPath, store.path); err != nil {
		return fmt.Errorf("replace provider auth store: %w", err)
	}
	tmpPath = ""
	if err := os.Chmod(store.path, 0o600); err != nil {
		return fmt.Errorf("secure provider auth store: %w", err)
	}
	if err := syncDirectory(store.dir); err != nil {
		return fmt.Errorf("sync provider auth directory: %w", err)
	}
	return nil
}

func (store *fileStore) status(method Method) (Status, error) {
	state, err := store.read()
	if err != nil {
		return Status{}, err
	}
	entry := state.method(method)
	accounts := make([]Account, 0, len(entry.Accounts))
	for _, account := range entry.Accounts {
		accounts = append(accounts, account.public())
	}
	sort.Slice(accounts, func(i, j int) bool {
		iDefault := accounts[i].ID == entry.DefaultAccountID
		jDefault := accounts[j].ID == entry.DefaultAccountID
		if iDefault != jDefault {
			return iDefault
		}
		if accounts[i].RequiresReauth != accounts[j].RequiresReauth {
			return !accounts[i].RequiresReauth
		}
		if !accounts[i].AuthenticatedAt.Equal(accounts[j].AuthenticatedAt) {
			return accounts[i].AuthenticatedAt.After(accounts[j].AuthenticatedAt)
		}
		return accounts[i].ID < accounts[j].ID
	})
	return Status{
		Method:           method,
		Accounts:         accounts,
		DefaultAccountID: entry.DefaultAccountID,
		Authenticated:    len(accounts) > 0,
	}, nil
}

func (store *fileStore) resolve(binding Binding) (storedAccount, error) {
	state, err := store.read()
	if err != nil {
		return storedAccount{}, err
	}
	entry := state.method(binding.Method)
	id := binding.AccountID
	if id == "" {
		id = entry.DefaultAccountID
	}
	account, ok := entry.Accounts[id]
	if !ok || id == "" {
		return storedAccount{}, fmt.Errorf("%w: %s/%s", ErrAccountNotFound, binding.Method, id)
	}
	if account.RequiresReauth {
		return storedAccount{}, fmt.Errorf("%w: %s/%s", ErrRequiresReauth, binding.Method, id)
	}
	return account, nil
}

func fallbackDefault(accounts map[string]storedAccount) string {
	var best storedAccount
	for _, candidate := range accounts {
		if candidate.RequiresReauth {
			continue
		}
		if best.ID == "" || candidate.AuthenticatedAt.After(best.AuthenticatedAt) ||
			(candidate.AuthenticatedAt.Equal(best.AuthenticatedAt) && candidate.ID < best.ID) {
			best = candidate
		}
	}
	return best.ID
}
