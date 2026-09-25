package computer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cnjack/jcode/internal/config"
)

// Relocating a nested helper bundle out of its host app.
//
// macOS attributes Screen Recording (kTCCServiceScreenCapture) to the
// OUTERMOST app bundle containing the requesting executable, while
// Accessibility stays with the inner bundle. Verified against tccd logging on
// macOS 26: a helper .app under Contents/Resources, Contents/Helpers or
// Contents/Library/LoginItems — main executable or not — has its Screen
// Recording checks keyed on the host app. So the desktop build, which ships
// jcode-computerd.app inside jcode.app, split the one "jcode Computer Use"
// identity in two: Accessibility on "jcode Computer Use", Screen Recording on
// "jcode", which the onboarding window never asks the user to enable.
//
// Launching the helper from a copy that sits outside every other bundle keeps
// both grants on "jcode Computer Use". Under Developer ID the TCC requirement
// is identifier + team, so a grant survives the path change and the re-copy
// that follows each app update.

// helperInstallDir is where a nested helper bundle is mirrored to.
func helperInstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home for the computer-use helper: %w", err)
	}
	return filepath.Join(home, "Library", "Application Support", "jcode"), nil
}

// launchableHelperBin returns the daemon path to spawn. A failed relocation
// falls back to the in-place helper: Accessibility still works there, only
// Screen Recording attribution is wrong, and a working daemon beats none.
func launchableHelperBin(bin string) string {
	dir, err := helperInstallDir()
	if err != nil {
		config.Logger().Printf("[computer] running nested helper in place: %v", err)
		return bin
	}
	moved, err := relocateNestedHelper(bin, dir)
	if err != nil {
		config.Logger().Printf("[computer] running nested helper in place: %v", err)
		return bin
	}
	return moved
}

// relocateNestedHelper mirrors a helper bundle that lives inside another .app
// into installDir and returns the equivalent executable path in the copy.
// Everything else — bare binaries, a bundle beside the CLI binary, dev builds —
// is returned unchanged.
func relocateNestedHelper(bin, installDir string) (string, error) {
	resolved, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return "", fmt.Errorf("resolve helper path: %w", err)
	}
	src := appBundleOfExecutable(resolved)
	if src == "" || !isInsideAppBundle(src) {
		return bin, nil
	}
	dst := filepath.Join(filepath.Clean(installDir), filepath.Base(src))
	if isInsideAppBundle(dst) || dst == src {
		return "", fmt.Errorf("helper install dir %s is not outside an app bundle", installDir)
	}
	rel, err := filepath.Rel(src, resolved)
	if err != nil {
		return "", fmt.Errorf("locate helper executable in bundle: %w", err)
	}
	if err := syncHelperBundle(src, dst); err != nil {
		return "", err
	}
	return filepath.Join(dst, rel), nil
}

// appBundleOfExecutable returns <root>.app for <root>.app/Contents/MacOS/<exe>,
// or "" when bin is not a bundle executable.
func appBundleOfExecutable(bin string) string {
	macOS := filepath.Dir(filepath.Clean(bin))
	contents := filepath.Dir(macOS)
	root := filepath.Dir(contents)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" ||
		!strings.EqualFold(filepath.Ext(root), ".app") {
		return ""
	}
	return root
}

// isInsideAppBundle reports whether any ancestor directory of path is an .app.
func isInsideAppBundle(path string) bool {
	dir := filepath.Dir(filepath.Clean(path))
	for {
		if strings.EqualFold(filepath.Ext(dir), ".app") {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// syncHelperBundle makes dst an exact copy of src, copying only when the trees
// differ. Concurrent jcode processes serialize on a lock file beside dst; the
// new copy is staged and swapped in so a reader never sees a half-written
// bundle.
func syncHelperBundle(src, dst string) error {
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("prepare helper install dir: %w", err)
	}
	lock, err := acquireScreenshotFileLock(filepath.Join(parent, "."+filepath.Base(dst)+".lock"))
	if err != nil {
		return fmt.Errorf("lock helper install dir: %w", err)
	}
	defer func() { _ = lock.release() }()

	want, err := bundleDigest(src)
	if err != nil {
		return fmt.Errorf("fingerprint bundled helper: %w", err)
	}
	if have, err := bundleDigest(dst); err == nil && have == want {
		return nil
	}

	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dst)+".tmp-")
	if err != nil {
		return fmt.Errorf("stage helper copy: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	staged := filepath.Join(tmp, filepath.Base(dst))
	if err := copyTree(src, staged); err != nil {
		return fmt.Errorf("copy helper bundle: %w", err)
	}
	if got, err := bundleDigest(staged); err != nil || got != want {
		return fmt.Errorf("helper bundle changed while copying from %s", src)
	}
	// rename(2) cannot replace a non-empty directory, so park the previous copy
	// inside the staging dir (removed by the deferred cleanup) first.
	previous := filepath.Join(tmp, "previous")
	if err := os.Rename(dst, previous); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("replace installed helper: %w", err)
	}
	if err := os.Rename(staged, dst); err != nil {
		_ = os.Rename(previous, dst)
		return fmt.Errorf("install helper bundle: %w", err)
	}
	config.Logger().Printf("[computer] installed helper bundle %s from %s", dst, src)
	return nil
}

// copyTree copies a directory tree, preserving permission bits and symlinks.
// Extended attributes are not copied; the code signature does not use them.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch mode := info.Mode(); {
		case mode.IsDir():
			if err := os.Mkdir(target, 0o700); err != nil {
				return err
			}
			return os.Chmod(target, mode.Perm())
		case mode&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case mode.IsRegular():
			return copyFile(path, target, mode.Perm())
		default:
			return fmt.Errorf("unsupported file type in helper bundle: %s", path)
		}
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// Chmod after create so the umask cannot drop bits and make the copy's
	// fingerprint differ from the source on every launch.
	return os.Chmod(dst, perm)
}

// bundleDigest fingerprints a tree by relative path, type, permission bits,
// and content (or link target), without following symlinks.
func bundleDigest(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00", filepath.ToSlash(rel), mode.String())
		switch {
		case mode&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(h, "%s\x00", link)
		case mode.IsRegular():
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			n, err := io.Copy(h, f)
			_ = f.Close()
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(h, "\x00%d\x00", n)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
