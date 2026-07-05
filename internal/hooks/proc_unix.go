//go:build !windows

package hooks

import (
	"os/exec"
	"syscall"
)

// setupProcessGroup runs the hook shell in its own process group and makes
// timeout/cancel kill the whole group, so a grandchild the shell spawned (e.g. a
// `sleep`) is torn down instead of surviving as an orphan.
func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid signals the whole process group (leader pid == group id).
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
