//go:build !windows

package session

import (
	"os"

	"golang.org/x/sys/unix"
)

type securityFileLock struct{ file *os.File }

func acquireSecurityFileLock(path string) (*securityFileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &securityFileLock{file: file}, nil
}

func (lock *securityFileLock) release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	_ = unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	return lock.file.Close()
}
