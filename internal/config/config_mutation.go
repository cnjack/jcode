package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// configWriteMu complements the OS advisory lock. Separate flock handles in
// one process have platform-specific ownership semantics; this mutex makes
// nested goroutine behavior deterministic while the file lock coordinates
// independent JCode processes.
var configWriteMu sync.Mutex

// ErrConfigConflict means SaveConfig received a snapshot loaded before another
// process committed a newer config. The caller must reload and reapply only its
// intended field mutation; blindly retrying the stale snapshot is unsafe.
var ErrConfigConflict = errors.New("config changed on disk")

// MutateConfig performs a serialized reload -> mutate -> durable atomic save.
// Callers must publish the returned snapshot only after this function succeeds;
// a stale in-memory Config must never be written back directly.
func MutateConfig(mutate func(*Config) error) (*Config, error) {
	return mutateConfig(false, mutate)
}

// MutateConfigOrCreate is the setup/add-provider variant of MutateConfig. It
// accepts a missing or valid-but-providerless config, but still fails closed on
// malformed/unreadable files rather than replacing them with a blank config.
func MutateConfigOrCreate(mutate func(*Config) error) (*Config, error) {
	return mutateConfig(true, mutate)
}

func mutateConfig(allowCreate bool, mutate func(*Config) error) (*Config, error) {
	if mutate == nil {
		return nil, fmt.Errorf("config mutation callback is required")
	}
	var updated *Config
	err := withConfigWriteLock(func() error {
		var err error
		updated, err = loadConfig(!allowCreate)
		if err != nil && allowCreate && errors.Is(err, os.ErrNotExist) {
			updated = &Config{MaxIterations: 1000}
			err = nil
		}
		if err != nil {
			return err
		}
		if err := mutate(updated); err != nil {
			return err
		}
		return saveConfig(updated, os.Rename)
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func withConfigWriteLock(run func() error) error {
	configWriteMu.Lock()
	defer configWriteMu.Unlock()

	cfgPath, err := configFilePath()
	if err != nil {
		return fmt.Errorf("config file path error: %w", err)
	}
	dir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("failed to secure config directory %s: %w", dir, err)
	}
	lock, err := acquireConfigFileLock(cfgPath + ".lock")
	if err != nil {
		return fmt.Errorf("failed to lock config file %s: %w", cfgPath, err)
	}
	defer func() {
		if releaseErr := lock.release(); releaseErr != nil {
			Logger().Printf("[config] failed to release config lock: %v", releaseErr)
		}
	}()
	return run()
}

func verifyConfigRevision(cfg *Config) error {
	// Empty revisions preserve compatibility for setup/import callers that build
	// a Config from scratch. Every Config returned by LoadConfig carries a
	// revision and therefore gets strict compare-and-swap behavior.
	if cfg == nil || cfg.diskRevision == "" {
		return nil
	}
	cfgPath, err := configFilePath()
	if err != nil {
		return fmt.Errorf("config file path error: %w", err)
	}
	current, err := os.ReadFile(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: config file was removed", ErrConfigConflict)
		}
		return fmt.Errorf("read config revision: %w", err)
	}
	if configContentRevision(current) != cfg.diskRevision {
		return fmt.Errorf("%w: reload before saving", ErrConfigConflict)
	}
	return nil
}
