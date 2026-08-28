//go:build windows

package tasks

import (
	"golang.org/x/sys/windows"
)

// processAlive reports whether pid is a live process on this machine.
// Used to detect tasks orphaned by a crashed jcode process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		_ = windows.CloseHandle(handle)
		return true // exists but could not be queried
	}
	_ = windows.CloseHandle(handle)
	return code == 259 // STILL_ACTIVE
}
