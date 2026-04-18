package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"tinygo.org/x/bluetooth"
)

var adapter = bluetooth.DefaultAdapter

// Nordic UART Service UUIDs
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

func main() {
	fmt.Println("=== Go BLE Client for JCODE Device ===")
	fmt.Println("Enabling BLE adapter...")

	must("enable BLE adapter", adapter.Enable())

	var foundDevice bluetooth.ScanResult
	found := make(chan struct{})

	fmt.Println("Scanning for JCODE-* devices...")
	err := adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
		name := result.LocalName()
		if strings.HasPrefix(name, "JCODE-") {
			fmt.Printf("  Found: %s (RSSI: %d)\n", name, result.RSSI)
			foundDevice = result
			adapter.StopScan()
			close(found)
		}
	})
	if err != nil {
		// Scan returns after StopScan, check if we found something
		select {
		case <-found:
			// ok
		default:
			fatal("scan", err)
		}
	}

	select {
	case <-found:
	case <-time.After(15 * time.Second):
		fmt.Println("Timeout: no JCODE device found.")
		os.Exit(1)
	}

	fmt.Printf("\nConnecting to %s ...\n", foundDevice.LocalName())
	device, err := adapter.Connect(foundDevice.Address, bluetooth.ConnectionParams{})
	must("connect", err)
	fmt.Println("Connected!")

	fmt.Println("Discovering NUS service...")
	services, err := device.DiscoverServices([]bluetooth.UUID{nusServiceUUID})
	must("discover services", err)
	if len(services) == 0 {
		fmt.Println("NUS service not found on device!")
		os.Exit(1)
	}

	fmt.Println("Discovering characteristics...")
	chars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{nusRXCharUUID, nusTXCharUUID})
	must("discover characteristics", err)

	var rxChar bluetooth.DeviceCharacteristic
	var txChar bluetooth.DeviceCharacteristic
	rxFound, txFound := false, false

	for _, c := range chars {
		if c.UUID() == nusRXCharUUID {
			rxChar = c
			rxFound = true
			fmt.Println("  Found RX characteristic (write)")
		}
		if c.UUID() == nusTXCharUUID {
			txChar = c
			txFound = true
			fmt.Println("  Found TX characteristic (notify)")
		}
	}

	if !rxFound {
		fmt.Println("RX characteristic not found!")
		os.Exit(1)
	}

	// Enable notifications on TX if found
	if txFound {
		err = txChar.EnableNotifications(func(buf []byte) {
			fmt.Printf("[Device TX] %s\n", string(buf))
		})
		if err != nil {
			fmt.Printf("Warning: could not enable TX notifications: %v\n", err)
		} else {
			fmt.Println("  TX notifications enabled")
		}
	}

	fmt.Println()
	fmt.Println("=== Ready! Enter commands below ===")
	fmt.Println("Commands:")
	fmt.Println("  heart <message>         - Send a text message to display")
	fmt.Println("  idle [message]          - Set pet to idle state")
	fmt.Println("  working [message]       - Set pet to working state")
	fmt.Println("  attention [message]     - Set pet to attention state")
	fmt.Println("  complete [message]      - Set pet to complete state")
	fmt.Println("  raw <json>              - Send raw JSON line")
	fmt.Println("  quit                    - Disconnect and exit")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var jsonCmd string

		switch {
		case line == "quit" || line == "exit":
			fmt.Println("Disconnecting...")
			device.Disconnect()
			fmt.Println("Bye!")
			return

		case strings.HasPrefix(line, "heart "):
			msg := strings.TrimPrefix(line, "heart ")
			jsonCmd = fmt.Sprintf(`{"cmd":"heart","val":"%s"}`, escapeJSON(msg))

		case matchCmd(line, "idle", "working", "attention", "complete"):
			parts := strings.SplitN(line, " ", 2)
			cmd := parts[0]
			msg := ""
			if len(parts) > 1 {
				msg = parts[1]
			}
			jsonCmd = fmt.Sprintf(`{"cmd":"%s","val":"%s"}`, cmd, escapeJSON(msg))

		case strings.HasPrefix(line, "raw "):
			jsonCmd = strings.TrimPrefix(line, "raw ")

		default:
			fmt.Println("Unknown command. Type 'heart <msg>', a state name, or 'quit'.")
			continue
		}

		// Append newline (NDJSON protocol)
		data := []byte(jsonCmd + "\n")
		fmt.Printf("  Sending: %s", string(data))

		_, err := rxChar.WriteWithoutResponse(data)
		if err != nil {
			fmt.Printf("  Write error: %v\n", err)
		} else {
			fmt.Println("  Sent OK!")
		}
	}
}

func matchCmd(line string, cmds ...string) bool {
	for _, c := range cmds {
		if line == c || strings.HasPrefix(line, c+" ") {
			return true
		}
	}
	return false
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func must(action string, err error) {
	if err != nil {
		fatal(action, err)
	}
}

func fatal(action string, err error) {
	fmt.Printf("Fatal: failed to %s: %v\n", action, err)
	os.Exit(1)
}
