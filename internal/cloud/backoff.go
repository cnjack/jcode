package cloud

import (
	"context"
	"sync"
	"time"
)

// Backoff is a thread-safe exponential backoff with a cap, used by every
// connector retry loop (register, poll, WS reconnect). It is injectable so
// tests can shrink the delays.
type Backoff struct {
	Min time.Duration
	Max time.Duration

	mu  sync.Mutex
	cur time.Duration
}

// NewBackoff returns a Backoff starting at min and capped at max.
func NewBackoff(min, max time.Duration) *Backoff {
	return &Backoff{Min: min, Max: max}
}

// Next returns the current delay and doubles it (capped at Max).
func (b *Backoff) Next() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cur <= 0 {
		b.cur = b.Min
	} else {
		b.cur *= 2
		if b.cur > b.Max {
			b.cur = b.Max
		}
	}
	return b.cur
}

// Reset clears the backoff after a success.
func (b *Backoff) Reset() {
	b.mu.Lock()
	b.cur = 0
	b.mu.Unlock()
}

// Wait sleeps for the next backoff delay or until ctx is cancelled (in which
// case it returns ctx.Err()).
func (b *Backoff) Wait(ctx context.Context) error {
	d := b.Next()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
