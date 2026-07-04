// Package feature holds compile-time feature flags gated by build tags, so a
// build can omit whole capabilities instead of only hiding them at runtime.
//
// BLE (the Bluetooth status channel) is desktop-only: the desktop app bundles
// the jcode-ble helper and builds with `-tags desktop`. Plain `jcode web` (the
// browser server) is built without it, so BLE is compiled off — its notifier is
// never spawned, its API endpoints report unavailable, and the settings UI hides
// the toggle (the server reports the capability as false).
package feature
