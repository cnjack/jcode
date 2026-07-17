//go:build !windows

package computer

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// screenshotStoreRoot pins the verified cache directory by file descriptor.
// All child operations are fd-relative and use O_NOFOLLOW, so replacing the
// pathname after validation cannot redirect a save, open, or prune elsewhere.
type screenshotStoreRoot struct{ dir *os.File }

func openScreenshotStoreRoot(path string, create bool) (*screenshotStoreRoot, error) {
	info, err := lstatOrCreateScreenshotRoot(path, create)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open screenshot store without following links: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	opened, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stat opened screenshot store: %w", err)
	}
	if !os.SameFile(info, opened) {
		_ = f.Close()
		return nil, fmt.Errorf("screenshot store changed while it was being opened")
	}
	return &screenshotStoreRoot{dir: f}, nil
}

func (r *screenshotStoreRoot) close() error { return r.dir.Close() }

func (r *screenshotStoreRoot) readDir() ([]os.DirEntry, error) {
	return r.dir.ReadDir(-1)
}

func (r *screenshotStoreRoot) createExclusive(name string, perm os.FileMode) (*os.File, error) {
	fd, err := unix.Openat(
		int(r.dir.Fd()), name,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		uint32(perm.Perm()),
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func (r *screenshotStoreRoot) openRegular(name string) (*os.File, os.FileInfo, error) {
	fd, err := unix.Openat(int(r.dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, nil, errScreenshotEntryNotRegular
		}
		return nil, nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, errScreenshotEntryNotRegular
	}
	return f, info, nil
}

func (r *screenshotStoreRoot) remove(name string) error {
	return unix.Unlinkat(int(r.dir.Fd()), name, 0)
}
