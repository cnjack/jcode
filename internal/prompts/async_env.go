package prompts

import (
	"context"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/config"
	utils "github.com/cnjack/jcode/internal/util"
)

// AsyncEnvLoader loads environment info with a timeout.
// It wraps the synchronous CollectEnvInfo in a goroutine so callers can
// proceed with other work (e.g. memory loading) concurrently.
type AsyncEnvLoader struct {
	timeout time.Duration
}

// NewAsyncEnvLoader creates a loader with the given timeout.
func NewAsyncEnvLoader(timeout time.Duration) *AsyncEnvLoader {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &AsyncEnvLoader{timeout: timeout}
}

// Load collects environment information with a timeout guard.
// CollectEnvInfo already runs each git sub-command with its own 2s timeout;
// this wrapper provides an overall deadline so the prompt builder never blocks
// for longer than the configured duration.
func (a *AsyncEnvLoader) Load(ctx context.Context, pwd string) *utils.EnvInfo {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	type result struct {
		info *utils.EnvInfo
	}

	ch := make(chan result, 1)
	go func() {
		ch <- result{info: utils.CollectEnvInfo(pwd)}
	}()

	select {
	case r := <-ch:
		return r.info
	case <-ctx.Done():
		config.Logger().Printf("[async_env] timeout loading env info for %s", pwd)
		return &utils.EnvInfo{}
	}
}

// LoadAsync starts environment info collection in the background and
// returns a function that blocks until the result is ready. This enables
// the caller to kick off env loading and memory loading in parallel.
func (a *AsyncEnvLoader) LoadAsync(ctx context.Context, pwd string) func() *utils.EnvInfo {
	var info *utils.EnvInfo
	var once sync.Once
	done := make(chan struct{})

	go func() {
		defer close(done)
		info = a.Load(ctx, pwd)
	}()

	return func() *utils.EnvInfo {
		once.Do(func() {
			<-done
		})
		if info == nil {
			return &utils.EnvInfo{}
		}
		return info
	}
}
