// Package memory implements jcode's cross-session learned memory: a
// file-based store under ~/.jcode/memory with a per-project root, an online
// note inbox (L1), a summary/index read path injected into the system prompt,
// and usage accounting that feeds the offline distillation pipeline (L2).
// See internal-doc/agent-memory-design.md.
package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/cnjack/jcode/internal/config"
)

// Layout, relative to a scope root (global/ or projects/<slug>/):
//
//	memory_summary.md   consolidated summary, injected into the system prompt
//	MEMORY.md           grep-able index (maintained by the consolidation agent)
//	notes/              L1 inbox: one small fact per file, <ts>-<slug>.md
//	session_summaries/  phase-1 products (M2)
//	skills/             distilled reusable workflows (M3, SKILL.md format)
//	state.json          usage accounting / pipeline coordination
const (
	SummaryFile  = "memory_summary.md"
	IndexFile    = "MEMORY.md"
	NotesDir     = "notes"
	SummariesDir = "session_summaries"
	StateFile    = "state.json"
)

// Root returns the memory root directory (~/.jcode/memory). It follows
// config.ConfigDir() so isolated-HOME test environments are respected.
func Root() string {
	return filepath.Join(config.ConfigDir(), "memory")
}

// GlobalRoot returns the scope root for cross-project memory.
func GlobalRoot() string {
	return filepath.Join(Root(), "global")
}

// ProjectRoot returns the scope root for a project working directory.
func ProjectRoot(projectDir string) string {
	return filepath.Join(Root(), "projects", ProjectSlug(projectDir))
}

// ProjectSlug derives the stable per-project directory name:
// <sanitized-basename>-<hash8-of-canonical-path>. The hash keeps same-named
// projects apart; the basename keeps the directory human-readable.
func ProjectSlug(projectDir string) string {
	canon := canonicalPath(projectDir)
	base := sanitizeSlug(filepath.Base(canon))
	sum := sha256.Sum256([]byte(canon))
	return base + "-" + hex.EncodeToString(sum[:])[:8]
}

func canonicalPath(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return abs
}

var slugUnsafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeSlug(s string) string {
	s = slugUnsafe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return "project"
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

// ScopeRootFor maps a memory_note scope value to its directory.
func ScopeRootFor(scope, projectDir string) string {
	if scope == "global" {
		return GlobalRoot()
	}
	return ProjectRoot(projectDir)
}

// withinRoot verifies that target stays inside root after cleaning. It
// rejects `..` traversal (including URL-encoded variants that could survive
// naive cleaning) and resolves symlinked parents so a link inside the memory
// tree cannot redirect writes elsewhere. This is the implementation-level
// guard the design mandates — never rely on prompt discipline for it.
func withinRoot(root, target string) error {
	lower := strings.ToLower(target)
	if strings.Contains(lower, "%2e") || strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return fmt.Errorf("memory path contains encoded traversal sequence")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	abs = filepath.Clean(abs)
	// Resolve the deepest existing ancestor so symlinks cannot escape.
	if resolved := resolveExistingPrefix(abs); resolved != "" {
		abs = resolved
	}
	if resolvedRoot := resolveExistingPrefix(absRoot); resolvedRoot != "" {
		absRoot = resolvedRoot
	}
	rel, err := filepath.Rel(absRoot, abs)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("memory path %q escapes memory root", target)
	}
	return nil
}

// resolveExistingPrefix resolves symlinks on the longest existing prefix of
// path and rejoins the non-existing remainder.
func resolveExistingPrefix(path string) string {
	remainder := ""
	cur := path
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(resolved, remainder)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path
		}
		remainder = filepath.Join(filepath.Base(cur), remainder)
		cur = parent
	}
}

// EnsureScope creates the standard layout for a scope root.
func EnsureScope(scopeRoot string) error {
	for _, d := range []string{scopeRoot, filepath.Join(scopeRoot, NotesDir), filepath.Join(scopeRoot, SummariesDir)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// atomicWrite writes data to path via a temp file + rename, matching the
// convention used by internal/session. The temp file name is unique per
// writer (pid + counter) so concurrent writers to the same target never
// clobber each other's temp file.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), atomic.AddUint64(&tmpCounter, 1))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

var tmpCounter uint64

// TruncateRunes truncates s to at most maxChars bytes without splitting a
// UTF-8 rune, then appends suffix. Byte-count budgeting (not rune count) is
// intentional — token/size limits are byte-based — but the cut lands on a
// rune boundary so multibyte text (e.g. Chinese) is never corrupted.
func TruncateRunes(s string, maxChars int, suffix string) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	cut := maxChars
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + suffix
}
