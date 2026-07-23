package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

const providerSyncSchemaVersion = 1

var (
	ErrConfigSyncDisabled        = errors.New("desktop configuration sync is disabled")
	ErrConfigSyncApprovalPending = errors.New("this Desktop is waiting for configuration sync approval")
	ErrConfigSyncDenied          = errors.New("configuration sync approval was denied")
	ErrProviderSyncConflict      = errors.New("local and Cloud provider configurations conflict")
)

type syncedProvider struct {
	SchemaVersion int                      `json:"schema_version"`
	ProviderID    string                   `json:"provider_id"`
	Kind          string                   `json:"kind"`
	Config        *config.ProviderConfig   `json:"config,omitempty"`
	ModelState    syncedProviderModelState `json:"model_state,omitempty"`
	Deleted       bool                     `json:"deleted"`
	UpdatedAt     time.Time                `json:"updated_at"`
}

type syncedProviderModelState struct {
	Favorite        []string          `json:"favorite,omitempty"`
	EnabledModels   []string          `json:"enabled_models,omitempty"`
	DisabledModels  []string          `json:"disabled_models,omitempty"`
	EffortOverrides map[string]string `json:"effort_overrides,omitempty"`
}

type providerSyncEntry struct {
	Version   int64     `json:"version"`
	LocalHash string    `json:"local_hash,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	Deleted   bool      `json:"deleted,omitempty"`
}

type providerSyncState struct {
	SchemaVersion int                          `json:"schema_version"`
	Providers     map[string]providerSyncEntry `json:"providers"`
	LastSyncAt    time.Time                    `json:"last_sync_at,omitempty"`
}

func defaultProviderSyncStatePath() string {
	return filepath.Join(config.ConfigDir(), "provider-sync.json")
}

func loadProviderSyncState(path string) (*providerSyncState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &providerSyncState{
			SchemaVersion: providerSyncSchemaVersion,
			Providers:     map[string]providerSyncEntry{},
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read provider sync state: %w", err)
	}
	var state providerSyncState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse provider sync state: %w", err)
	}
	if state.SchemaVersion != providerSyncSchemaVersion {
		return nil, fmt.Errorf("unsupported provider sync state schema %d", state.SchemaVersion)
	}
	if state.Providers == nil {
		state.Providers = map[string]providerSyncEntry{}
	}
	return &state, nil
}

func saveProviderSyncState(path string, state *providerSyncState) error {
	if state == nil {
		return errors.New("provider sync state is nil")
	}
	state.SchemaVersion = providerSyncSchemaVersion
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode provider sync state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create provider sync directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".provider-sync.tmp-*")
	if err != nil {
		return fmt.Errorf("create provider sync temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace provider sync state: %w", err)
	}
	return nil
}

func cloneProviderConfig(in *config.ProviderConfig) *config.ProviderConfig {
	if in == nil {
		return nil
	}
	data, _ := json.Marshal(in)
	var out config.ProviderConfig
	_ = json.Unmarshal(data, &out)
	return &out
}

func providerModelStateSnapshot(state *config.ModelState, providerID string) syncedProviderModelState {
	var out syncedProviderModelState
	if state == nil {
		return out
	}
	for _, ref := range state.Favorite {
		if ref.Provider == providerID {
			out.Favorite = append(out.Favorite, ref.Model)
		}
	}
	for _, ref := range state.EnabledModels {
		if ref.Provider == providerID {
			out.EnabledModels = append(out.EnabledModels, ref.Model)
		}
	}
	for _, ref := range state.DisabledModels {
		if ref.Provider == providerID {
			out.DisabledModels = append(out.DisabledModels, ref.Model)
		}
	}
	prefix := providerID + "/"
	for key, value := range state.EffortOverrides {
		if strings.HasPrefix(key, prefix) {
			if out.EffortOverrides == nil {
				out.EffortOverrides = map[string]string{}
			}
			out.EffortOverrides[strings.TrimPrefix(key, prefix)] = value
		}
	}
	sort.Strings(out.Favorite)
	sort.Strings(out.EnabledModels)
	sort.Strings(out.DisabledModels)
	return out
}

func providerSnapshotHash(pc *config.ProviderConfig, state *config.ModelState, providerID string) string {
	if pc == nil {
		return ""
	}
	return providerPayloadHash(pc, providerModelStateSnapshot(state, providerID))
}

func providerPayloadHash(pc *config.ProviderConfig, modelState syncedProviderModelState) string {
	if pc == nil {
		return ""
	}
	data, _ := json.Marshal(struct {
		Config     *config.ProviderConfig   `json:"config"`
		ModelState syncedProviderModelState `json:"model_state"`
	}{Config: pc, ModelState: modelState})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func initialProviderSyncConflict(
	localPC *config.ProviderConfig,
	localState *config.ModelState,
	providerID string,
	remote *syncedProvider,
) bool {
	if localPC == nil || remote == nil {
		return false
	}
	if remote.Deleted || remote.Config == nil {
		return true
	}
	return providerSnapshotHash(localPC, localState, providerID) !=
		providerPayloadHash(remote.Config, remote.ModelState)
}

func removeProviderRefs(refs []config.ModelRef, providerID string) []config.ModelRef {
	out := refs[:0]
	for _, ref := range refs {
		if ref.Provider != providerID {
			out = append(out, ref)
		}
	}
	return out
}

func applyProviderModelState(state *config.ModelState, providerID string, remote syncedProviderModelState) {
	state.Favorite = removeProviderRefs(state.Favorite, providerID)
	state.EnabledModels = removeProviderRefs(state.EnabledModels, providerID)
	state.DisabledModels = removeProviderRefs(state.DisabledModels, providerID)
	if state.EffortOverrides == nil {
		state.EffortOverrides = map[string]string{}
	}
	prefix := providerID + "/"
	for key := range state.EffortOverrides {
		if strings.HasPrefix(key, prefix) {
			delete(state.EffortOverrides, key)
		}
	}
	for _, modelID := range remote.Favorite {
		state.Favorite = append(state.Favorite, config.ModelRef{Provider: providerID, Model: modelID})
	}
	for _, modelID := range remote.EnabledModels {
		state.EnabledModels = append(state.EnabledModels, config.ModelRef{Provider: providerID, Model: modelID})
	}
	for _, modelID := range remote.DisabledModels {
		state.DisabledModels = append(state.DisabledModels, config.ModelRef{Provider: providerID, Model: modelID})
	}
	for modelID, effort := range remote.EffortOverrides {
		state.EffortOverrides[prefix+modelID] = effort
	}
}

func (c *Connector) providerSyncStatePath() string {
	if c.cfg.ProviderSyncStatePath != "" {
		return c.cfg.ProviderSyncStatePath
	}
	return defaultProviderSyncStatePath()
}

// ensureAccountSyncCipher enrolls this Desktop into the account ASK mesh.
// The first Desktop initializes the key; later Desktops request approval and
// remain unable to read provider ciphertext until an approved Desktop wraps
// ASK to their registered identity key.
func (c *Connector) ensureAccountSyncCipher(ctx context.Context) (*EnvelopeCipher, error) {
	if c.cfg.AppConfig == nil || !config.CloudConfigSync(c.cfg.AppConfig) {
		return nil, ErrConfigSyncDisabled
	}
	creds := c.cfg.Credentials
	if creds == nil || creds.DeviceToken == "" || creds.PublicKey == "" || creds.PrivateKey == "" {
		return nil, errors.New("desktop Cloud identity is incomplete")
	}
	remote, err := c.client.GetAccountSyncKey(ctx, c.token)
	if err != nil {
		return nil, err
	}
	switch remote.State {
	case "uninitialized":
		var ask []byte
		if creds.ASK != "" && creds.ASKKeyGen == 1 {
			ask, err = base64.StdEncoding.DecodeString(creds.ASK)
		} else {
			ask, err = GenerateAccountSyncKey()
		}
		if err != nil {
			return nil, err
		}
		wrap, err := WrapAccountSyncKey(creds.PublicKey, ask, 1)
		if err != nil {
			return nil, err
		}
		if err := c.client.InitializeAccountSyncKey(ctx, c.token, 1, wrap); err != nil {
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Code != "sync_key_exists" {
				return nil, err
			}
			// Another Desktop won initialization. Never upload or use our
			// losing key; fetch the winner's state on the next reconciliation.
			creds.ASK, creds.ASKKeyGen = "", 0
			if saveErr := SaveCredentials(creds); saveErr != nil {
				return nil, saveErr
			}
			return nil, ErrConfigSyncApprovalPending
		}
		if err := saveAccountSyncKey(creds, ask, 1); err != nil {
			return nil, err
		}
		return NewEnvelopeCipher(ask, 1)
	case "request_required":
		if _, err := c.client.RequestAccountSyncKey(ctx, c.token); err != nil {
			return nil, err
		}
		return nil, ErrConfigSyncApprovalPending
	case "waiting":
		return nil, ErrConfigSyncApprovalPending
	case "denied":
		return nil, ErrConfigSyncDenied
	case "ready":
		if creds.ASK != "" && creds.ASKKeyGen == remote.KeyGen {
			return accountSyncCipherFromCredentials(creds)
		}
		var wrap AccountSyncKeyWrap
		if err := json.Unmarshal(remote.Wrap, &wrap); err != nil {
			return nil, fmt.Errorf("parse account sync key wrap: %w", err)
		}
		ask, keyGen, err := UnwrapAccountSyncKey(creds.PrivateKey, wrap)
		if err != nil {
			return nil, err
		}
		if keyGen != remote.KeyGen {
			return nil, fmt.Errorf("account sync key generation mismatch: wrap=%d server=%d", keyGen, remote.KeyGen)
		}
		if err := saveAccountSyncKey(creds, ask, keyGen); err != nil {
			return nil, err
		}
		return NewEnvelopeCipher(ask, keyGen)
	default:
		return nil, fmt.Errorf("unknown account sync key state %q", remote.State)
	}
}

func openSyncedProvider(cipher *EnvelopeCipher, remote AccountProviderConfigRemote) (*syncedProvider, error) {
	plain, err := cipher.Open(remote.Envelope)
	if err != nil {
		return nil, fmt.Errorf("decrypt provider %s: %w", remote.ProviderID, err)
	}
	var payload syncedProvider
	dec := json.NewDecoder(strings.NewReader(string(plain)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		return nil, fmt.Errorf("parse provider %s: %w", remote.ProviderID, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse provider %s: trailing JSON", remote.ProviderID)
	}
	if payload.SchemaVersion != providerSyncSchemaVersion ||
		payload.ProviderID != remote.ProviderID || payload.UpdatedAt.IsZero() ||
		payload.Deleted != remote.Deleted || (!payload.Deleted && payload.Config == nil) {
		return nil, fmt.Errorf("provider %s has an invalid encrypted payload", remote.ProviderID)
	}
	return &payload, nil
}

func sealSyncedProvider(cipher *EnvelopeCipher, payload syncedProvider) (json.RawMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return cipher.Seal(data)
}

// ReconcileProviderConfigs performs per-provider timestamp merge + CAS. Local
// providers remain ordinary direct ProviderConfig entries; the Cloud stores
// only ASK-encrypted replicas and never participates in their runtime calls.
func (c *Connector) ReconcileProviderConfigs(ctx context.Context) error {
	c.providerSyncMu.Lock()
	defer c.providerSyncMu.Unlock()
	return c.reconcileProviderConfigs(ctx)
}

func (c *Connector) reconcileProviderConfigs(ctx context.Context) error {
	cipher, err := c.ensureAccountSyncCipher(ctx)
	if err != nil {
		return err
	}
	state, err := loadProviderSyncState(c.providerSyncStatePath())
	if err != nil {
		return err
	}
	remoteRows, err := c.client.ListAccountProviderConfigs(ctx, c.token)
	if err != nil {
		return err
	}
	type remoteProvider struct {
		row     AccountProviderConfigRemote
		payload *syncedProvider
	}
	remote := make(map[string]remoteProvider, len(remoteRows))
	for _, row := range remoteRows {
		payload, err := openSyncedProvider(cipher, row)
		if err != nil {
			return err
		}
		remote[row.ProviderID] = remoteProvider{row: row, payload: payload}
	}

	freshCfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("load local provider config: %w", err)
	}
	local := make(map[string]*config.ProviderConfig)
	for id, pc := range freshCfg.GetProviders() {
		local[id] = cloneProviderConfig(pc)
	}
	modelState, err := config.LoadModelState()
	if err != nil {
		return fmt.Errorf("load local model state: %w", err)
	}
	// Normalize the legacy `models` provider map before applying tombstones or
	// imports; otherwise deleting the last entry from a new `providers` map
	// would make GetProviders fall back to the untouched legacy map.
	if len(freshCfg.Providers) == 0 && len(local) > 0 {
		freshCfg.Providers = make(map[string]*config.ProviderConfig, len(local))
		for id, pc := range local {
			freshCfg.Providers[id] = cloneProviderConfig(pc)
		}
		freshCfg.Models = nil //nolint:staticcheck // clear migrated legacy provider storage
	}
	ids := make(map[string]bool)
	for id := range local {
		ids[id] = true
	}
	for id := range remote {
		ids[id] = true
	}
	for id := range state.Providers {
		ids[id] = true
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)

	configChanged := false
	modelStateChanged := false
	now := time.Now().UTC()
	for _, id := range ordered {
		localPC, localExists := local[id]
		localHash := providerSnapshotHash(localPC, modelState, id)
		entry, tracked := state.Providers[id]
		rr, remoteExists := remote[id]

		if !tracked {
			if remoteExists {
				// A Desktop that already has an untracked provider with the same
				// ID must never silently lose its local key to an older Cloud
				// replica (or tombstone). Surface a conflict and leave both
				// sides unchanged so the user can resolve the identity clash.
				if initialProviderSyncConflict(localPC, modelState, id, rr.payload) {
					return fmt.Errorf("%w: provider %q", ErrProviderSyncConflict, id)
				}
				if rr.payload.Deleted {
					delete(freshCfg.Providers, id)
					delete(local, id)
					applyProviderModelState(modelState, id, syncedProviderModelState{})
					modelStateChanged = true
					localHash = ""
				} else {
					if freshCfg.Providers == nil {
						freshCfg.Providers = map[string]*config.ProviderConfig{}
					}
					freshCfg.Providers[id] = cloneProviderConfig(rr.payload.Config)
					local[id] = cloneProviderConfig(rr.payload.Config)
					applyProviderModelState(modelState, id, rr.payload.ModelState)
					modelStateChanged = true
					localHash = providerSnapshotHash(rr.payload.Config, modelState, id)
				}
				configChanged = true
				state.Providers[id] = providerSyncEntry{
					Version: rr.row.Version, LocalHash: localHash,
					UpdatedAt: rr.payload.UpdatedAt, Deleted: rr.payload.Deleted,
				}
				continue
			}
			if !localExists {
				continue
			}
			entry = providerSyncEntry{UpdatedAt: now}
		}

		localChanged := (localExists && localHash != entry.LocalHash) || (!localExists && !entry.Deleted)
		if localChanged {
			entry.UpdatedAt = now
			entry.Deleted = !localExists
		}
		remoteChanged := remoteExists && rr.row.Version != entry.Version
		if remoteChanged && (!localChanged || rr.payload.UpdatedAt.After(entry.UpdatedAt)) {
			if rr.payload.Deleted {
				if freshCfg.Providers != nil {
					delete(freshCfg.Providers, id)
				}
				if strings.HasPrefix(freshCfg.Model, id+"/") {
					freshCfg.Model = ""
				}
				applyProviderModelState(modelState, id, syncedProviderModelState{})
				modelStateChanged = true
				localHash = ""
			} else {
				if freshCfg.Providers == nil {
					freshCfg.Providers = map[string]*config.ProviderConfig{}
				}
				freshCfg.Providers[id] = cloneProviderConfig(rr.payload.Config)
				applyProviderModelState(modelState, id, rr.payload.ModelState)
				modelStateChanged = true
				localHash = providerSnapshotHash(rr.payload.Config, modelState, id)
			}
			configChanged = true
			state.Providers[id] = providerSyncEntry{
				Version: rr.row.Version, LocalHash: localHash,
				UpdatedAt: rr.payload.UpdatedAt, Deleted: rr.payload.Deleted,
			}
			continue
		}
		if !localChanged {
			if remoteExists {
				entry.Version = rr.row.Version
			}
			state.Providers[id] = entry
			continue
		}

		payload := syncedProvider{
			SchemaVersion: providerSyncSchemaVersion,
			ProviderID:    id, Kind: id, Config: cloneProviderConfig(localPC),
			ModelState: providerModelStateSnapshot(modelState, id),
			Deleted:    !localExists, UpdatedAt: entry.UpdatedAt,
		}
		envelope, err := sealSyncedProvider(cipher, payload)
		if err != nil {
			return err
		}
		baseVersion := int64(0)
		if remoteExists {
			baseVersion = rr.row.Version
		}
		saved, err := c.client.PutAccountProviderConfig(
			ctx, c.token, id, baseVersion, envelope, payload.Deleted,
		)
		if err != nil {
			return err
		}
		entry.Version = saved.Version
		entry.LocalHash = localHash
		entry.Deleted = payload.Deleted
		state.Providers[id] = entry
	}

	if configChanged {
		if err := config.SaveConfig(freshCfg); err != nil {
			return fmt.Errorf("apply synced providers: %w", err)
		}
		// Publish onto the connector's shared config pointer so portable
		// preference validation and subsequent snapshots see imported providers.
		c.cfg.AppConfig.Providers = freshCfg.Providers
		c.cfg.AppConfig.Models = freshCfg.Models //nolint:staticcheck // preserve legacy migration state
		c.cfg.AppConfig.Model = freshCfg.Model
	}
	if modelStateChanged {
		if err := config.SaveModelState(modelState); err != nil {
			return fmt.Errorf("apply synced model state: %w", err)
		}
	}
	state.LastSyncAt = time.Now().UTC()
	return saveProviderSyncState(c.providerSyncStatePath(), state)
}

func (c *Connector) providerConfigLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.ReconcileProviderConfigs(ctx); err != nil &&
				!errors.Is(err, ErrConfigSyncDisabled) &&
				!errors.Is(err, ErrConfigSyncApprovalPending) {
				c.logf("provider config sync: %v", err)
			}
		}
	}
}

// AccountSyncKeyStatus returns the server-side enrollment state for this
// Desktop without exposing ASK.
func (c *Connector) AccountSyncKeyStatus(ctx context.Context) (*AccountSyncKeyState, error) {
	if c.cfg.AppConfig == nil || !config.CloudConfigSync(c.cfg.AppConfig) {
		return &AccountSyncKeyState{State: "disabled"}, nil
	}
	return c.client.GetAccountSyncKey(ctx, c.token)
}

func (c *Connector) PendingAccountSyncDevices(ctx context.Context) ([]AccountSyncKeyRequest, error) {
	c.providerSyncMu.Lock()
	defer c.providerSyncMu.Unlock()
	if _, err := c.ensureAccountSyncCipher(ctx); err != nil {
		return nil, err
	}
	return c.client.ListAccountSyncKeyRequests(ctx, c.token, "pending")
}

func (c *Connector) ApprovedAccountSyncDevices(ctx context.Context) ([]AccountSyncKeyRequest, error) {
	c.providerSyncMu.Lock()
	defer c.providerSyncMu.Unlock()
	if _, err := c.ensureAccountSyncCipher(ctx); err != nil {
		return nil, err
	}
	return c.client.ListAccountSyncKeyRequests(ctx, c.token, "approved")
}

func (c *Connector) ApproveAccountSyncDevice(ctx context.Context, deviceID string) error {
	c.providerSyncMu.Lock()
	defer c.providerSyncMu.Unlock()
	cipher, err := c.ensureAccountSyncCipher(ctx)
	if err != nil {
		return err
	}
	requests, err := c.client.ListAccountSyncKeyRequests(ctx, c.token, "pending")
	if err != nil {
		return err
	}
	for _, request := range requests {
		if request.DeviceID != deviceID {
			continue
		}
		wrap, err := WrapAccountSyncKey(request.Pubkey, cipher.CEK(), cipher.KeyGen())
		if err != nil {
			return err
		}
		return c.client.RespondAccountSyncKeyRequest(
			ctx, c.token, deviceID, true, cipher.KeyGen(), wrap,
		)
	}
	return fmt.Errorf("pending Desktop %q not found", deviceID)
}

func (c *Connector) DenyAccountSyncDevice(ctx context.Context, deviceID string) error {
	c.providerSyncMu.Lock()
	defer c.providerSyncMu.Unlock()
	cipher, err := c.ensureAccountSyncCipher(ctx)
	if err != nil {
		return err
	}
	return c.client.RespondAccountSyncKeyRequest(
		ctx, c.token, deviceID, false, cipher.KeyGen(), nil,
	)
}

func (c *Connector) RevokeAccountSyncDevice(ctx context.Context, deviceID string) error {
	c.providerSyncMu.Lock()
	defer c.providerSyncMu.Unlock()
	if _, err := c.ensureAccountSyncCipher(ctx); err != nil {
		return err
	}
	return c.client.RevokeAccountSyncKeyDevice(ctx, c.token, deviceID)
}
