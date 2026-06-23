//go:build !windows

package automation

import (
	"os"

	"golang.org/x/sys/unix"
)

// fileLock is an advisory OS file lock (flock). The kernel releases it
// automatically when the holding process exits — including on crash/SIGKILL —
// so a dead owner never deadlocks the lock.
type fileLock struct{ f *os.File }

// acquireLock blocks until the exclusive lock at path is held.
func acquireLock(path string) (*fileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &fileLock{f: f}, nil
}

// tryAcquireLock attempts a non-blocking exclusive lock. ok=false means another
// process holds it.
func tryAcquireLock(path string) (lock *fileLock, ok bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if err == unix.EWOULDBLOCK {
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
	_ = unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	return l.f.Close()
}
