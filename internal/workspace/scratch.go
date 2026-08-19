// Package workspace owns JCode-managed local working directories.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

const privateWorkspaceDirMode os.FileMode = 0o700

// ScratchRoot returns the directory that contains JCode-managed, no-project
// workspaces. It follows config.ConfigDir so isolated HOME test environments
// never touch a user's real ~/.jcode directory.
func ScratchRoot() string {
	return filepath.Join(config.ConfigDir(), "workspace")
}

// CreateScratch creates one private workspace named YYYY-MM-DD-NNN. The
// sequence is scoped to the local calendar day and allocated with an exclusive
// mkdir, so concurrent creators can race safely without a shared counter file.
func CreateScratch(now time.Time) (string, error) {
	root := ScratchRoot()
	if err := os.MkdirAll(root, privateWorkspaceDirMode); err != nil {
		return "", fmt.Errorf("create scratch workspace root: %w", err)
	}
	if err := os.Chmod(root, privateWorkspaceDirMode); err != nil {
		return "", fmt.Errorf("secure scratch workspace root: %w", err)
	}

	prefix := now.Format("2006-01-02") + "-"
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("list scratch workspaces: %w", err)
	}
	maxSeq := 0
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		seq, parseErr := strconv.Atoi(strings.TrimPrefix(entry.Name(), prefix))
		if parseErr == nil && seq > maxSeq {
			maxSeq = seq
		}
	}

	for seq := maxSeq + 1; ; seq++ {
		path := filepath.Join(root, fmt.Sprintf("%s%03d", prefix, seq))
		err = os.Mkdir(path, privateWorkspaceDirMode)
		if err == nil {
			return path, nil
		}
		if os.IsExist(err) {
			continue
		}
		return "", fmt.Errorf("create scratch workspace: %w", err)
	}
}

// ValidateScratchPath verifies that path is an existing, real directory created
// directly beneath ScratchRoot with the managed date/sequence name. Persisted
// metadata marked scratch must pass this check before it can select the
// scratch-only engine factory.
func ValidateScratchPath(path string) error {
	root, err := filepath.Abs(ScratchRoot())
	if err != nil {
		return fmt.Errorf("resolve scratch workspace root: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve scratch workspace: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || filepath.Dir(rel) != "." {
		return fmt.Errorf("scratch workspace is outside the managed root")
	}
	name := filepath.Base(rel)
	if len(name) < len("2006-01-02-1") {
		return fmt.Errorf("scratch workspace has an invalid managed name")
	}
	if _, err := time.Parse("2006-01-02", name[:10]); err != nil || name[10] != '-' {
		return fmt.Errorf("scratch workspace has an invalid managed date")
	}
	seq, err := strconv.Atoi(name[11:])
	if err != nil || seq < 1 {
		return fmt.Errorf("scratch workspace has an invalid managed sequence")
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return fmt.Errorf("stat scratch workspace: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("scratch workspace is not a real directory")
	}
	return nil
}
