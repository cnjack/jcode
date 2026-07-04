//go:build !ble || (darwin && !cgo)

package ble

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/cnjack/jcode/internal/channel"
	"github.com/cnjack/jcode/internal/config"
)

// This is the DEFAULT build's BLE notifier. It links NOTHING that touches
// CoreBluetooth — instead it spawns the separate `jcode-ble` helper binary
// (built with `-tags ble`) and relays events to it over stdio. That is what
// lets BLE be a pure runtime config toggle: the main binary never instantiates
// a CBCentralManager, so it never triggers the macOS Bluetooth permission
// prompt at startup, no matter the config. The helper — spawned only when BLE
// is enabled — is the only process that touches Bluetooth, so the prompt (if
// any) appears exactly when the user turns BLE on.
//
// If the jcode-ble helper isn't present next to the main binary, BLE is simply
// unavailable (no-op). Build it once with `make build-ble`.

// ReceivedCommand is a parsed command received from the BLE device.
type ReceivedCommand struct {
	Cmd string `json:"cmd"`
	Val string `json:"val"`
}

// wireEvent is the JSON line sent to the helper's stdin per NotifyEvent.
type wireEvent struct {
	Type int    `json:"type"`
	Tool string `json:"tool,omitempty"`
	Err  string `json:"err,omitempty"`
}

// Notifier relays notification events to the jcode-ble helper process.
type Notifier struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	inbound chan ReceivedCommand
	started bool
	dead    bool
}

// New spawns the jcode-ble helper (if present) and returns a notifier. It is
// only constructed when BLE is enabled in config, so the helper — and any
// Bluetooth prompt — only appears then.
func New() *Notifier {
	n := &Notifier{inbound: make(chan ReceivedCommand, 16)}
	n.start()
	return n
}

func helperPath() string {
	// Explicit override wins (e.g. set by the desktop shell).
	if p := os.Getenv("JCODE_BLE_HELPER"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	exeSuffix := ""
	if runtime.GOOS == "windows" {
		exeSuffix = ".exe"
	}
	// Exact name first (production bundle co-locates jcode + jcode-ble).
	if p := filepath.Join(dir, "jcode-ble"+exeSuffix); statExecutable(p) {
		return p
	}
	// Tauri dev mode keeps the target-triple suffix (jcode-ble-<triple>); match it.
	if matches, _ := filepath.Glob(filepath.Join(dir, "jcode-ble-*"+exeSuffix)); len(matches) > 0 {
		for _, m := range matches {
			if statExecutable(m) {
				return m
			}
		}
	}
	return ""
}

func statExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func (n *Notifier) start() {
	hp := helperPath()
	if hp == "" {
		config.Logger().Printf("[ble] enabled but the jcode-ble helper is not installed (build it with `make build-ble`); BLE disabled")
		n.dead = true
		return
	}
	cmd := exec.Command(hp)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		n.dead = true
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		n.dead = true
		return
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		config.Logger().Printf("[ble] failed to start helper: %v", err)
		n.dead = true
		return
	}
	n.cmd = cmd
	n.stdin = stdin
	n.started = true
	config.Logger().Printf("[ble] helper started: %s", hp)
	go n.readLoop(stdout)
}

func (n *Notifier) readLoop(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), 1<<16)
	for sc.Scan() {
		var rc ReceivedCommand
		if json.Unmarshal(sc.Bytes(), &rc) == nil && rc.Cmd != "" {
			select {
			case n.inbound <- rc:
			default:
			}
		}
	}
	n.mu.Lock()
	n.dead = true
	n.mu.Unlock()
}

func (n *Notifier) Name() string { return "ble" }

func (n *Notifier) Available() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.started && !n.dead
}

func (n *Notifier) Notify(event channel.NotifyEvent) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.started || n.dead || n.stdin == nil {
		return
	}
	we := wireEvent{Type: int(event.Type), Tool: event.Tool}
	if event.Err != nil {
		we.Err = event.Err.Error()
	}
	b, _ := json.Marshal(we)
	if _, err := n.stdin.Write(append(b, '\n')); err != nil {
		n.dead = true
	}
}

func (n *Notifier) Receive() <-chan ReceivedCommand { return n.inbound }

func (n *Notifier) Close() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.dead && n.cmd == nil {
		return
	}
	if n.stdin != nil {
		_ = n.stdin.Close()
	}
	if n.cmd != nil && n.cmd.Process != nil {
		_ = n.cmd.Process.Kill()
	}
	n.dead = true
}
