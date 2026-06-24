//go:build windows

package automation

import (
	"os"

	"golang.org/x/sys/windows"
)

// fileLock is an advisory OS file lock via LockFileEx. Windows releases the lock
// when the file handle closes, which the OS does on process exit, so a crashed
// owner does not deadlock the lock.
type fileLock struct{ f *os.File }

func lockFile(f *os.File, flags uint32) error {
	ol := new(windows.Overlapped)
	// Lock the first byte; that is sufficient for advisory whole-file locking
	// when every participant locks the same byte range.
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, ol)
}

func acquireLock(path string) (*fileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f, windows.LOCKFILE_EXCLUSIVE_LOCK); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &fileLock{f: f}, nil
}

func tryAcquireLock(path string) (lock *fileLock, ok bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := lockFile(f, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY); err != nil {
		_ = f.Close()
		if err == windows.ERROR_LOCK_VIOLATION {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &fileLock{f: f}, true, nil
}

func (l *fileLock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	ol := new(windows.Overlapped)
	_ = windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, ol)
	return l.f.Close()
}
