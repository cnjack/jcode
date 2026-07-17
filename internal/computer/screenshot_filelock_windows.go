//go:build windows

package computer

import (
	"os"

	"golang.org/x/sys/windows"
)

// screenshotFileLock mirrors the LockFileEx implementation used by
// internal/automation. Locking one byte is sufficient because every jcode
// process coordinates on the same byte of the same stable sibling file.
type screenshotFileLock struct{ f *os.File }

func acquireScreenshotFileLock(path string) (*screenshotFileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(
		windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol,
	); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &screenshotFileLock{f: f}, nil
}

func (l *screenshotFileLock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	ol := new(windows.Overlapped)
	_ = windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, ol)
	return l.f.Close()
}
