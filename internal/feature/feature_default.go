//go:build !desktop

package feature

// BLE is compiled OFF for non-desktop builds (plain `jcode web`, CLI). The
// browser web server has no need for a Bluetooth status channel.
const (
	BLE     = false
	Desktop = false
)
