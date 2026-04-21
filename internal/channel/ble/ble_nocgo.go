//go:build darwin && !cgo

package ble

import "github.com/cnjack/jcode/internal/channel"

// Notifier is a no-op stub for macOS without CGo (CoreBluetooth requires CGo).
type Notifier struct{}

// ReceivedCommand is a parsed command received from the BLE device.
type ReceivedCommand struct {
	Cmd string
	Val string
}

func New() *Notifier                                { return &Notifier{} }
func (n *Notifier) Name() string                    { return "ble" }
func (n *Notifier) Available() bool                 { return false }
func (n *Notifier) Notify(_ channel.NotifyEvent)    {}
func (n *Notifier) Close()                          {}
func (n *Notifier) Receive() <-chan ReceivedCommand { return nil }
