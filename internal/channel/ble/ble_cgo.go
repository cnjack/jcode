//go:build !darwin || cgo

// Package ble provides a channel.Notifier that sends short status messages
// to a JCODE-* BLE IoT device using the Nordic UART Service (NUS).
//
// The notifier auto-discovers nearby JCODE-* devices on first use and
// reconnects on failure. All sends are best-effort and non-blocking.
package ble

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"

	"github.com/cnjack/jcode/internal/channel"
	"github.com/cnjack/jcode/internal/config"
)

// Nordic UART Service UUIDs.
var (
	nusServiceUUID = bluetooth.NewUUID([16]byte{
		0x6E, 0x40, 0x00, 0x01, 0xB5, 0xA3, 0xF3, 0x93,
		0xE0, 0xA9, 0xE5, 0x0E, 0x24, 0xDC, 0xCA, 0x9E,
	})
	nusRXCharUUID = bluetooth.NewUUID([16]byte{
		0x6E, 0x40, 0x00, 0x02, 0xB5, 0xA3, 0xF3, 0x93,
		0xE0, 0xA9, 0xE5, 0x0E, 0x24, 0xDC, 0xCA, 0x9E,
	})
	nusTXCharUUID = bluetooth.NewUUID([16]byte{
		0x6E, 0x40, 0x00, 0x03, 0xB5, 0xA3, 0xF3, 0x93,
		0xE0, 0xA9, 0xE5, 0x0E, 0x24, 0xDC, 0xCA, 0x9E,
	})
)

// command is the NDJSON protocol message sent to/received from the device.
type command struct {
	Cmd string `json:"cmd"`
	Val string `json:"val,omitempty"`
}

// ReceivedCommand is a parsed command received from the BLE device.
type ReceivedCommand struct {
	Cmd string // "input", "submit", "cancel"
	Val string // payload for "input" command
}

// Notifier implements channel.Notifier for BLE IoT devices.
type Notifier struct {
	mu      sync.Mutex
	adapter *bluetooth.Adapter
	device  bluetooth.Device
	rxChar  bluetooth.DeviceCharacteristic
	ready   bool
	closed  bool

	// connectOnce ensures we only attempt one background connect at a time.
	connecting bool

	// inbound receives parsed commands from the BLE device.
	inbound chan ReceivedCommand
}

// New creates a BLE notifier. It does NOT block — device discovery happens
// lazily on the first Notify call (in a background goroutine).
func New() *Notifier {
	return &Notifier{
		adapter: bluetooth.DefaultAdapter,
		inbound: make(chan ReceivedCommand, 16),
	}
}

func (n *Notifier) Name() string { return "ble" }

// Receive returns a channel that delivers commands received from the BLE device.
func (n *Notifier) Receive() <-chan ReceivedCommand {
	return n.inbound
}

func (n *Notifier) Available() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ready && !n.closed
}

// Notify sends a command to the BLE device based on the event type.
// If the device is not connected yet, a background connection is triggered.
func (n *Notifier) Notify(event channel.NotifyEvent) {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return
	}

	if !n.ready {
		if !n.connecting {
			n.connecting = true
			go n.connect()
		}
		n.mu.Unlock()
		return
	}

	rxChar := n.rxChar
	n.mu.Unlock()

	cmd, val := channel.FormatBLE(event)
	n.send(rxChar, cmd, val)
}

func (n *Notifier) Close() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return
	}
	n.closed = true
	close(n.inbound) // unblock the range loop in the forwarding goroutine
	if n.ready {
		n.ready = false
		go func() { _ = n.device.Disconnect() }()
	}
}

// connect performs BLE adapter enable, scan, connect, and characteristic discovery.
func (n *Notifier) connect() {
	logger := config.Logger()

	if err := n.adapter.Enable(); err != nil {
		logger.Printf("[ble] failed to enable adapter: %v", err)
		n.mu.Lock()
		n.connecting = false
		n.mu.Unlock()
		return
	}

	var found bluetooth.ScanResult
	foundCh := make(chan struct{})

	logger.Printf("[ble] scanning for JCODE-* devices...")
	err := n.adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
		name := result.LocalName()
		if strings.HasPrefix(name, "JCODE-") {
			logger.Printf("[ble] found device: %s (RSSI: %d)", name, result.RSSI)
			found = result
			_ = adapter.StopScan()
			close(foundCh)
		}
	})
	if err != nil {
		select {
		case <-foundCh:
		default:
			logger.Printf("[ble] scan error: %v", err)
			n.mu.Lock()
			n.connecting = false
			n.mu.Unlock()
			return
		}
	}

	// Wait for device with timeout
	select {
	case <-foundCh:
	case <-time.After(10 * time.Second):
		logger.Printf("[ble] no JCODE device found within timeout")
		_ = n.adapter.StopScan()
		n.mu.Lock()
		n.connecting = false
		n.mu.Unlock()
		return
	}

	logger.Printf("[ble] connecting to %s...", found.LocalName())
	device, err := n.adapter.Connect(found.Address, bluetooth.ConnectionParams{})
	if err != nil {
		logger.Printf("[ble] connect failed: %v", err)
		n.mu.Lock()
		n.connecting = false
		n.mu.Unlock()
		return
	}

	services, err := device.DiscoverServices([]bluetooth.UUID{nusServiceUUID})
	if err != nil || len(services) == 0 {
		logger.Printf("[ble] NUS service not found")
		_ = device.Disconnect()
		n.mu.Lock()
		n.connecting = false
		n.mu.Unlock()
		return
	}

	chars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{nusRXCharUUID, nusTXCharUUID})
	if err != nil || len(chars) == 0 {
		logger.Printf("[ble] NUS characteristics not found")
		_ = device.Disconnect()
		n.mu.Lock()
		n.connecting = false
		n.mu.Unlock()
		return
	}

	var rxChar bluetooth.DeviceCharacteristic
	var txCharFound bool
	for _, c := range chars {
		if c.UUID() == nusRXCharUUID {
			rxChar = c
		}
		if c.UUID() == nusTXCharUUID {
			txCharFound = true
			// Subscribe to TX notifications (data FROM the device).
			if err := c.EnableNotifications(n.handleTXNotification); err != nil {
				logger.Printf("[ble] failed to subscribe to TX notifications: %v", err)
			} else {
				logger.Printf("[ble] subscribed to TX notifications")
			}
		}
	}
	if !txCharFound {
		logger.Printf("[ble] TX characteristic not found, receive disabled")
	}

	n.mu.Lock()
	n.device = device
	n.rxChar = rxChar
	n.ready = true
	n.connecting = false
	n.mu.Unlock()

	logger.Printf("[ble] connected and ready")
}

func (n *Notifier) send(rxChar bluetooth.DeviceCharacteristic, cmd, val string) {
	msg := command{Cmd: cmd, Val: val}
	data, err := json.Marshal(msg)
	if err != nil {
		config.Logger().Printf("[ble] marshal error: %v", err)
		return
	}
	data = append(data, '\n')

	if _, err := rxChar.WriteWithoutResponse(data); err != nil {
		config.Logger().Printf("[ble] write error: %v", err)
		// Mark as disconnected so next Notify triggers reconnect
		n.mu.Lock()
		n.ready = false
		n.mu.Unlock()
	}
}

// handleTXNotification is called when the BLE device sends data via NUS TX.
// It parses NDJSON commands and delivers them to the inbound channel.
func (n *Notifier) handleTXNotification(data []byte) {
	logger := config.Logger()

	// Trim trailing newline if present.
	data = bytes.TrimRight(data, "\n\r")
	if len(data) == 0 {
		return
	}

	var cmd command
	if err := json.Unmarshal(data, &cmd); err != nil {
		logger.Printf("[ble] received invalid JSON: %s", string(data))
		return
	}

	logger.Printf("[ble] received cmd=%s val=%s", cmd.Cmd, cmd.Val)

	n.mu.Lock()
	closed := n.closed
	n.mu.Unlock()
	if closed {
		return
	}

	select {
	case n.inbound <- ReceivedCommand{Cmd: cmd.Cmd, Val: cmd.Val}:
	default:
		logger.Printf("[ble] inbound channel full, dropping cmd=%s", cmd.Cmd)
	}
}
