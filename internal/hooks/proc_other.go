//go:build windows

package hooks

import "os/exec"

// setupProcessGroup is a no-op on platforms without POSIX process groups. The
// default CommandContext cancel plus cmd.WaitDelay still bound cmd.Wait so the
// agent is never blocked; a spawned child may briefly outlive the timeout.
func setupProcessGroup(cmd *exec.Cmd) {}
