//go:build !windows

package tasks

import (
	"syscall"
)

// processAlive reports whether pid is a live process on this machine.
// Used to detect tasks orphaned by a crashed jcode process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// Signal 0 performs the permission/existence check without sending a
	// signal. ESRCH means the process is gone; EPERM means it exists but is
	// owned by someone else — still alive for our purposes.
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
