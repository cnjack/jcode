//go:build darwin

package computer

import "golang.org/x/sys/unix"

func macOSProductVersion() (string, error) {
	return unix.Sysctl("kern.osproductversion")
}
