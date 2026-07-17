//go:build !windows

package computer

import (
	"os"

	"golang.org/x/sys/unix"
)

// screenshotFileLock is the same crash-released advisory lock pattern used by
// internal/automation and internal/memory. Every jcode process locks the same
// sibling file before it mutates or opens the shared public screenshot store.
type screenshotFileLock struct{ f *os.File }

func acquireScreenshotFileLock(path string) (*screenshotFileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &screenshotFileLock{f: f}, nil
}

func (l *screenshotFileLock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	return l.f.Close()
}
