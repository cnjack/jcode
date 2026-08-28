//go:build !windows

package tasks

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type taskLock struct{ file *os.File }

// acquireTaskLock takes an exclusive advisory lock held for the duration of
// one registry mutation, serializing appends across jcode processes. The OS
// releases the lock if a process dies mid-mutation.
func acquireTaskLock(path string) (*taskLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), privateTaskDirMode); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, privateTaskFileMode)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &taskLock{file: file}, nil
}

func (l *taskLock) release() error {
	if l == nil || l.file == nil {
		return nil
	}
	_ = unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	return l.file.Close()
}
