//go:build windows

package session

import (
	"os"

	"golang.org/x/sys/windows"
)

type securityFileLock struct{ file *os.File }

func acquireSecurityFileLock(path string) (*securityFileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	if err := windows.LockFileEx(
		windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped,
	); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &securityFileLock{file: file}, nil
}

func (lock *securityFileLock) release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	overlapped := new(windows.Overlapped)
	_ = windows.UnlockFileEx(windows.Handle(lock.file.Fd()), 0, 1, 0, overlapped)
	return lock.file.Close()
}
