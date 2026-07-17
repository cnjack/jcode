package computer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Screenshots are sensitive, replaceable cache entries. A 24-hour TTL keeps
// image_ref useful across a long task while preventing indefinite retention;
// count and byte ceilings cover high-frequency captures and unusually large
// windows independently.
const (
	screenshotStoreTTL      = 24 * time.Hour
	screenshotStoreMaxFiles = 128
	screenshotStoreMaxBytes = int64(256 << 20)
	screenshotStoreLockFile = "shots.lock"
)

var errScreenshotEntryNotRegular = errors.New("screenshot cache entry is not a regular file")

type screenshotStorePolicy struct {
	maxAge   time.Duration
	maxFiles int
	maxBytes int64
}

var defaultScreenshotStorePolicy = screenshotStorePolicy{
	maxAge:   screenshotStoreTTL,
	maxFiles: screenshotStoreMaxFiles,
	maxBytes: screenshotStoreMaxBytes,
}

type storedScreenshot struct {
	name    string
	size    int64
	modTime time.Time
}

// lstatOrCreateScreenshotRoot rejects a symlink (or any non-directory) before
// platform code performs its O_NOFOLLOW/reparse-point-safe open. The opened
// directory is also compared with this FileInfo, closing the Lstat→open race.
func lstatOrCreateScreenshotRoot(path string, create bool) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) && create {
		if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
			return nil, fmt.Errorf("create screenshot store: %w", err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect screenshot store: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("screenshot store must be a real directory, got %s", info.Mode())
	}
	return info, nil
}

func canonicalScreenshotName(name string) bool {
	if filepath.Base(name) != name || !strings.HasSuffix(name, ".png") {
		return false
	}
	stem := strings.TrimSuffix(name, ".png")
	id, err := uuid.Parse(stem)
	return err == nil && id.String() == stem
}

func screenshotStoreLockPath(dir string) string {
	return filepath.Join(filepath.Dir(dir), screenshotStoreLockFile)
}

// withScreenshotStoreRoot is the one entry point for the shared public cache.
// The in-process Manager mutex sits outside this function; this advisory lock
// coordinates distinct jcode processes and is held across save+prune or
// validate+open. The lock is a sibling, never part of the pruned directory.
func withScreenshotStoreRoot(
	dir string,
	create bool,
	fn func(*screenshotStoreRoot) error,
) (err error) {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("prepare screenshot store parent: %w", err)
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect screenshot store parent: %w", err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return fmt.Errorf("screenshot store parent must be a real directory")
	}

	lock, err := acquireScreenshotFileLock(screenshotStoreLockPath(dir))
	if err != nil {
		return fmt.Errorf("lock screenshot store: %w", err)
	}
	defer func() {
		if releaseErr := lock.release(); err == nil && releaseErr != nil {
			err = fmt.Errorf("release screenshot store lock: %w", releaseErr)
		}
	}()

	root, err := openScreenshotStoreRoot(dir, create)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := root.close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close screenshot store: %w", closeErr)
		}
	}()
	return fn(root)
}

func writeScreenshotToStore(
	dir string,
	name string,
	png []byte,
	now time.Time,
	policy screenshotStorePolicy,
) error {
	if !canonicalScreenshotName(name) {
		return fmt.Errorf("invalid screenshot cache filename %q", name)
	}
	return withScreenshotStoreRoot(dir, true, func(root *screenshotStoreRoot) error {
		f, err := root.createExclusive(name, 0o600)
		if err != nil {
			return fmt.Errorf("create screenshot: %w", err)
		}
		_, writeErr := f.Write(png)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			_ = root.remove(name)
			if writeErr != nil {
				return fmt.Errorf("write screenshot: %w", writeErr)
			}
			return fmt.Errorf("close screenshot: %w", closeErr)
		}
		if err := pruneScreenshotStoreRoot(root, name, now, policy); err != nil {
			_ = root.remove(name)
			return err
		}
		return nil
	})
}

// pruneScreenshotStore removes expired owned PNGs first, then the oldest
// remaining owned PNGs until both count and total-size limits are satisfied.
// Only canonical lowercase UUID.png regular files are cache-owned; unrelated
// PNGs, directories, and links are never touched.
func pruneScreenshotStore(
	dir string,
	keepPath string,
	now time.Time,
	policy screenshotStorePolicy,
) error {
	keepName := ""
	if keepPath != "" {
		keepName = filepath.Base(keepPath)
		if !canonicalScreenshotName(keepName) {
			return fmt.Errorf("invalid kept screenshot cache filename %q", keepPath)
		}
	}
	err := withScreenshotStoreRoot(dir, false, func(root *screenshotStoreRoot) error {
		return pruneScreenshotStoreRoot(root, keepName, now, policy)
	})
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func pruneScreenshotStoreRoot(
	root *screenshotStoreRoot,
	keepName string,
	now time.Time,
	policy screenshotStorePolicy,
) error {
	entries, err := root.readDir()
	if err != nil {
		return fmt.Errorf("read screenshot directory: %w", err)
	}
	shots := make([]storedScreenshot, 0, len(entries))
	var total int64
	for _, entry := range entries {
		name := entry.Name()
		if !canonicalScreenshotName(name) {
			continue
		}
		f, info, err := root.openRegular(name)
		if err != nil {
			if os.IsNotExist(err) || errors.Is(err, errScreenshotEntryNotRegular) {
				continue
			}
			return fmt.Errorf("open screenshot %s: %w", name, err)
		}
		if closeErr := f.Close(); closeErr != nil {
			return fmt.Errorf("close screenshot %s: %w", name, closeErr)
		}
		if policy.maxAge > 0 && name != keepName && !now.Before(info.ModTime().Add(policy.maxAge)) {
			if err := root.remove(name); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove expired screenshot %s: %w", name, err)
			}
			continue
		}
		shots = append(shots, storedScreenshot{name: name, size: info.Size(), modTime: info.ModTime()})
		total += info.Size()
	}

	// Oldest first, with a name tie-breaker for deterministic cleanup when a
	// filesystem has coarse timestamp precision.
	sort.Slice(shots, func(i, j int) bool {
		if shots[i].modTime.Equal(shots[j].modTime) {
			return shots[i].name < shots[j].name
		}
		return shots[i].modTime.Before(shots[j].modTime)
	})
	count := len(shots)
	for _, shot := range shots {
		overFiles := policy.maxFiles > 0 && count > policy.maxFiles
		overBytes := policy.maxBytes > 0 && total > policy.maxBytes
		if !overFiles && !overBytes {
			break
		}
		if shot.name == keepName {
			continue
		}
		if err := root.remove(shot.name); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove screenshot %s: %w", shot.name, err)
		}
		count--
		total -= shot.size
	}

	if (policy.maxFiles > 0 && count > policy.maxFiles) ||
		(policy.maxBytes > 0 && total > policy.maxBytes) {
		return fmt.Errorf("screenshot store remains over policy limits while preserving current capture")
	}
	return nil
}

func openScreenshotFromStore(
	dir string,
	name string,
	now time.Time,
	policy screenshotStorePolicy,
) (*os.File, error) {
	if !canonicalScreenshotName(name) {
		return nil, fmt.Errorf("invalid screenshot cache filename")
	}
	var opened *os.File
	err := withScreenshotStoreRoot(dir, false, func(root *screenshotStoreRoot) error {
		f, info, err := root.openRegular(name)
		if err != nil {
			return err
		}
		if policy.maxAge > 0 && !now.Before(info.ModTime().Add(policy.maxAge)) {
			_ = f.Close()
			if removeErr := root.remove(name); removeErr != nil && !os.IsNotExist(removeErr) {
				return fmt.Errorf("remove expired screenshot: %w", removeErr)
			}
			return fmt.Errorf("screenshot has expired")
		}
		opened = f
		return nil
	})
	if err != nil {
		if opened != nil {
			_ = opened.Close()
		}
		return nil, err
	}
	return opened, nil
}
