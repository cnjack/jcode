//go:build ble

// Command jcode-ble is the out-of-process BLE worker. The main jcode binary
// never links CoreBluetooth (so it never triggers the macOS Bluetooth prompt);
// when BLE is enabled in config it spawns this helper, which is the only process
// that touches Bluetooth. Communication is line-delimited JSON over stdio:
//
//	stdin : {"type":<EventType int>,"tool":"...","err":"..."}  (NotifyEvent)
//	stdout: {"cmd":"...","val":"..."}                          (inbound device cmd)
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"

	"github.com/cnjack/jcode/internal/channel"
	"github.com/cnjack/jcode/internal/channel/ble"
)

type wireEvent struct {
	Type int    `json:"type"`
	Tool string `json:"tool,omitempty"`
	Err  string `json:"err,omitempty"`
}

func main() {
	n := ble.New() // real BLE (tinygo / CoreBluetooth)
	defer n.Close()

	// Forward inbound BLE device commands to stdout.
	go func() {
		for rc := range n.Receive() {
			b, _ := json.Marshal(map[string]string{"cmd": rc.Cmd, "val": rc.Val})
			_, _ = os.Stdout.Write(append(b, '\n'))
		}
	}()

	// Relay NotifyEvents from stdin into BLE sends.
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 4096), 1<<16)
	for sc.Scan() {
		var we wireEvent
		if json.Unmarshal(sc.Bytes(), &we) != nil {
			continue
		}
		ev := channel.NotifyEvent{Type: channel.EventType(we.Type), Tool: we.Tool}
		if we.Err != "" {
			ev.Err = errors.New(we.Err)
		}
		n.Notify(ev)
	}
}
