//go:build windows

package computer

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// screenshotStoreRoot uses os.Root on Windows so child operations remain bound
// to the directory handle after validation. The explicit reparse-point check is
// the Windows equivalent of the Unix O_NOFOLLOW root open.
type screenshotStoreRoot struct{ root *os.Root }

func openScreenshotStoreRoot(path string, create bool) (*screenshotStoreRoot, error) {
	info, err := lstatOrCreateScreenshotRoot(path, create)
	if err != nil {
		return nil, err
	}
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode screenshot store path: %w", err)
	}
	attrs, err := windows.GetFileAttributes(path16)
	if err != nil {
		return nil, fmt.Errorf("inspect screenshot store attributes: %w", err)
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, fmt.Errorf("screenshot store must not be a reparse point")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("open screenshot store: %w", err)
	}
	opened, err := root.Stat(".")
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("stat opened screenshot store: %w", err)
	}
	if !os.SameFile(info, opened) {
		_ = root.Close()
		return nil, fmt.Errorf("screenshot store changed while it was being opened")
	}
	return &screenshotStoreRoot{root: root}, nil
}

func (r *screenshotStoreRoot) close() error { return r.root.Close() }

func (r *screenshotStoreRoot) readDir() ([]os.DirEntry, error) {
	f, err := r.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

func (r *screenshotStoreRoot) createExclusive(name string, perm os.FileMode) (*os.File, error) {
	return r.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
}

func (r *screenshotStoreRoot) openRegular(name string) (*os.File, os.FileInfo, error) {
	before, err := r.root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, nil, errScreenshotEntryNotRegular
	}
	f, err := r.root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	after, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		_ = f.Close()
		return nil, nil, fmt.Errorf("screenshot cache entry changed while it was being opened")
	}
	return f, after, nil
}

func (r *screenshotStoreRoot) remove(name string) error { return r.root.Remove(name) }
