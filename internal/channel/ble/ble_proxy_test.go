package ble

import (
	"testing"

	"github.com/cnjack/jcode/internal/channel"
)

func TestProxyStateMachine(t *testing.T) {
	var p Proxy

	// Disabled: inert.
	if p.Active() {
		t.Fatal("new proxy should be inactive")
	}
	if p.Available() {
		t.Fatal("inactive proxy should not be available")
	}
	p.Notify(channel.NotifyEvent{Type: channel.EventIdle}) // must not panic

	// Enable: becomes active (the helper is absent in tests, so it won't actually
	// connect, but the proxy holds an inner notifier).
	p.Enable()
	if !p.Active() {
		t.Fatal("proxy should be active after Enable")
	}
	// Idempotent enable.
	p.Enable()
	if !p.Active() {
		t.Fatal("double Enable should keep it active")
	}

	// Disable: back to inert.
	p.Disable()
	if p.Active() {
		t.Fatal("proxy should be inactive after Disable")
	}
	// Idempotent disable + notify after disable is a no-op.
	p.Disable()
	p.Notify(channel.NotifyEvent{Type: channel.EventWorking})

	if p.Name() != "ble" {
		t.Errorf("Name = %q, want ble", p.Name())
	}
}
