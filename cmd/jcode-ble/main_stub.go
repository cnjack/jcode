//go:build !ble

package main

import (
	"fmt"
	"os"
)

// Without the `ble` tag there is no CoreBluetooth support to run. Building this
// stub (instead of nothing) keeps `go build ./...` green; the real worker is
// produced by `make build-ble`.
func main() {
	fmt.Fprintln(os.Stderr, "jcode-ble must be built with -tags ble (run: make build-ble)")
	os.Exit(1)
}
