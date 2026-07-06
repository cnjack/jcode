//go:build windows

package procutil

import "os/exec"

// SetupProcessGroup is a no-op on platforms without POSIX process groups. The
// default CommandContext cancel plus cmd.WaitDelay still bound cmd.Wait so the
// caller is never blocked; a spawned child may briefly outlive the timeout.
func SetupProcessGroup(cmd *exec.Cmd) {}
