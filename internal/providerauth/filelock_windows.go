//go:build windows

package providerauth

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

type fileLock struct{ file *os.File }

func acquireFileLock(path string) (*fileLock, error) {
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
	return &fileLock{file: file}, nil
}

func (lock *fileLock) release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	overlapped := new(windows.Overlapped)
	_ = windows.UnlockFileEx(windows.Handle(lock.file.Fd()), 0, 1, 0, overlapped)
	return lock.file.Close()
}

func replaceFile(source, destination string) error {
	from, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// Windows has no portable directory fsync equivalent; MoveFileEx with
// MOVEFILE_WRITE_THROUGH supplies the durability barrier for replacement.
func syncDirectory(string) error { return nil }
