//go:build windows

package tasks

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

type taskLock struct{ file *os.File }

// acquireTaskLock takes an exclusive advisory lock held for the duration of
// one registry mutation, serializing appends across jcode processes.
func acquireTaskLock(path string) (*taskLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), privateTaskDirMode); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, privateTaskFileMode)
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
	return &taskLock{file: file}, nil
}

func (l *taskLock) release() error {
	if l == nil || l.file == nil {
		return nil
	}
	overlapped := new(windows.Overlapped)
	_ = windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, overlapped)
	return l.file.Close()
}
