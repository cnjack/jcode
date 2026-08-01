//go:build desktop

package feature

// BLE is compiled ON for desktop builds (`-tags desktop`).
const (
	BLE     = true
	Desktop = true
)
