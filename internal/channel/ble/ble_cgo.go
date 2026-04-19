//go:build !darwin || cgo

// Package ble provides a channel.Notifier that sends short status messages
// to a JCODE-* BLE IoT device using the Nordic UART Service (NUS).
//
// The notifier auto-discovers nearby JCODE-* devices on first use and
// reconnects on failure. All sends are best-effort and non-blocking.
package ble

import (
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
)

// command is the NDJSON protocol message sent to the device.
type command struct {
	Cmd string `json:"cmd"`
	Val string `json:"val,omitempty"`
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
}

// New creates a BLE notifier. It does NOT block — device discovery happens
// lazily on the first Notify call (in a background goroutine).
func New() *Notifier {
	return &Notifier{
		adapter: bluetooth.DefaultAdapter,
	}
}

func (n *Notifier) Name() string { return "ble" }

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
	if n.ready {
		n.ready = false
		go n.device.Disconnect()
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
			adapter.StopScan()
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
		n.adapter.StopScan()
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
		device.Disconnect()
		n.mu.Lock()
		n.connecting = false
		n.mu.Unlock()
		return
	}

	chars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{nusRXCharUUID})
	if err != nil || len(chars) == 0 {
		logger.Printf("[ble] RX characteristic not found")
		device.Disconnect()
		n.mu.Lock()
		n.connecting = false
		n.mu.Unlock()
		return
	}

	n.mu.Lock()
	n.device = device
	n.rxChar = chars[0]
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
