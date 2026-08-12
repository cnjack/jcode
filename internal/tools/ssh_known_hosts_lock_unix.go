//go:build !windows

package tools

import (
	"os"

	"golang.org/x/sys/unix"
)

type knownHostsFileLock struct{ file *os.File }

func acquireKnownHostsFileLock(path string) (*knownHostsFileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &knownHostsFileLock{file: file}, nil
}

func (lock *knownHostsFileLock) release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	_ = unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	return lock.file.Close()
}
