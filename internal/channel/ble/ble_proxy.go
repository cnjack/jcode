package ble

import (
	"sync"

	"github.com/cnjack/jcode/internal/channel"
)

// Proxy is a channel.Notifier that forwards to a live BLE notifier which can be
// swapped in/out at runtime. It is added once to each task's notifier chain, so
// enabling/disabling BLE takes effect immediately across all active tasks
// without an app restart. When no inner notifier is set, it is a no-op.
//
// Notifier's concrete type differs per build (real BLE vs. helper-spawner), but
// both expose the same New()/*Notifier surface, so this file is build-tag free.
type Proxy struct {
	mu    sync.Mutex
	inner *Notifier
}

// Name implements channel.Notifier.
func (p *Proxy) Name() string { return "ble" }

// Available reports whether a live notifier is present and ready.
func (p *Proxy) Available() bool {
	p.mu.Lock()
	n := p.inner
	p.mu.Unlock()
	return n != nil && n.Available()
}

// Notify forwards to the current inner notifier (no-op when disabled).
func (p *Proxy) Notify(event channel.NotifyEvent) {
	p.mu.Lock()
	n := p.inner
	p.mu.Unlock()
	if n != nil {
		n.Notify(event)
	}
}

// Close tears down the inner notifier.
func (p *Proxy) Close() { p.Disable() }

// Active reports whether BLE is currently live.
func (p *Proxy) Active() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inner != nil
}

// Enable spawns a fresh BLE notifier (the helper) if not already running and
// pushes an initial idle event so the device connects (and macOS prompts, if it
// is going to) right away.
func (p *Proxy) Enable() {
	p.mu.Lock()
	if p.inner != nil {
		p.mu.Unlock()
		return
	}
	n := New()
	p.inner = n
	p.mu.Unlock()
	n.Notify(channel.NotifyEvent{Type: channel.EventIdle})
}

// Disable stops and forgets the inner notifier.
func (p *Proxy) Disable() {
	p.mu.Lock()
	n := p.inner
	p.inner = nil
	p.mu.Unlock()
	if n != nil {
		n.Close()
	}
}
