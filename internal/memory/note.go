package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// MaxNoteBytes caps a single note (memory tool official guidance: bound file
// sizes at the implementation layer).
const MaxNoteBytes = 64 * 1024

// Note is one L1 inbox entry. Notes only ever land in the notes/ inbox —
// the curated files (MEMORY.md, memory_summary.md) are maintained solely by
// the phase-2 consolidation agent, keeping cheap-and-fast decoupled from
// expensive-and-curated.
type Note struct {
	Scope     string // "project" (default) | "global"
	Kind      string // preference | fact | pitfall | workflow
	Source    string // "user" (explicit "remember X") | "agent"
	Text      string
	SessionID string
	Cwd       string
}

var validKinds = map[string]bool{"preference": true, "fact": true, "pitfall": true, "workflow": true}

// WriteNote validates, redacts and persists a note into the scope's inbox.
// Returns the absolute path of the created file.
func WriteNote(n Note) (string, error) {
	text := strings.TrimSpace(n.Text)
	if text == "" {
		return "", fmt.Errorf("note text is empty")
	}
	if len(text) > MaxNoteBytes {
		return "", fmt.Errorf("note is too large (%d bytes, max %d) — split it into smaller facts", len(text), MaxNoteBytes)
	}
	if n.Scope != "global" {
		n.Scope = "project"
	}
	if !validKinds[n.Kind] {
		n.Kind = "fact"
	}
	if n.Source != "user" {
		n.Source = "agent"
	}

	scopeRoot := ScopeRootFor(n.Scope, n.Cwd)
	operation, err := acquireScopeOperation(scopeRoot)
	if err != nil {
		return "", err
	}
	defer operation.release()
	if err := EnsureScope(scopeRoot); err != nil {
		return "", err
	}

	text = Redact(text)
	now := time.Now()
	slug := noteSlug(text)
	notesDir := filepath.Join(scopeRoot, NotesDir)

	// Claim a unique filename atomically with O_CREATE|O_EXCL so concurrent
	// writers in the same second (eino runs a turn's tool calls in parallel)
	// each get a distinct file instead of silently overwriting one another.
	var path string
	var handle *os.File
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("%s-%s.md", now.Format("20060102-150405"), slug)
		if i > 0 {
			name = fmt.Sprintf("%s-%s-%d.md", now.Format("20060102-150405"), slug, i)
		}
		path = filepath.Join(notesDir, name)
		if err := withinRoot(Root(), path); err != nil {
			return "", err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			handle = f
			break
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	if handle == nil {
		return "", fmt.Errorf("could not allocate a unique note filename in %s", notesDir)
	}
	// Closed explicitly after a successful write (below) to surface flush
	// errors; this defensive close covers the error-return paths and is a
	// no-op once the file is already closed.
	defer func() { _ = handle.Close() }()

	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "kind: %s\n", n.Kind)
	fmt.Fprintf(&b, "source: %s\n", n.Source)
	if n.SessionID != "" {
		fmt.Fprintf(&b, "session: %s\n", n.SessionID)
	}
	if n.Cwd != "" {
		fmt.Fprintf(&b, "cwd: %s\n", Redact(n.Cwd))
	}
	fmt.Fprintf(&b, "time: %s\n", now.Format(time.RFC3339))
	b.WriteString("---\n\n")
	b.WriteString(text)
	b.WriteString("\n")

	if _, err := handle.WriteString(b.String()); err != nil {
		return "", err
	}
	if err := handle.Close(); err != nil {
		return "", err
	}
	return path, nil
}

// noteSlug builds a filename-safe, human-readable slug. It keeps ASCII
// alphanumerics and letters from other scripts (CJK etc.) so that non-Latin
// notes get a distinctive slug instead of all collapsing to "note" — the
// filename also carries a per-second uniqueness suffix, but a meaningful slug
// makes the inbox browsable and reduces same-name churn. Falls back to a hash
// fragment when nothing usable remains.
func noteSlug(text string) string {
	var b strings.Builder
	runes := 0
	prevDash := false
	for _, r := range text {
		if runes >= 24 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
			runes++
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
			prevDash = false
			runes++
		case unicode.IsLetter(r) && !isPathUnsafeRune(r):
			// non-ASCII letters (CJK, Cyrillic, ...): keep as-is.
			b.WriteRune(r)
			prevDash = false
			runes++
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		sum := sha256.Sum256([]byte(text))
		return "note-" + hex.EncodeToString(sum[:])[:8]
	}
	return s
}

// isPathUnsafeRune rejects runes that are letters by Unicode but unsafe or
// confusing in a filename (path separators, wildcards, control chars).
func isPathUnsafeRune(r rune) bool {
	return r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// RecentNotes returns up to limit inbox notes for a scope, newest first.
func RecentNotes(scopeRoot string, limit int) []NoteFile {
	entries, err := os.ReadDir(filepath.Join(scopeRoot, NotesDir))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	// Filenames start with a sortable timestamp; lexical desc = newest first.
	sortDesc(names)
	if limit > 0 && len(names) > limit {
		names = names[:limit]
	}
	var out []NoteFile
	for _, name := range names {
		p := filepath.Join(scopeRoot, NotesDir, name)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		nf := parseNoteFile(name, string(data))
		nf.Path = p
		out = append(out, nf)
	}
	return out
}

// NoteFile is a parsed inbox note (for injection and /memory display).
type NoteFile struct {
	Path   string
	Name   string
	Kind   string
	Source string
	Time   string
	Text   string
}

func parseNoteFile(name, content string) NoteFile {
	nf := NoteFile{Name: name, Kind: "fact", Source: "agent"}
	body := content
	if strings.HasPrefix(content, "---\n") {
		if end := strings.Index(content[4:], "\n---"); end >= 0 {
			front := content[4 : 4+end]
			body = strings.TrimPrefix(content[4+end+4:], "\n")
			for _, line := range strings.Split(front, "\n") {
				k, v, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				v = strings.TrimSpace(v)
				switch strings.TrimSpace(k) {
				case "kind":
					nf.Kind = v
				case "source":
					nf.Source = v
				case "time":
					nf.Time = v
				}
			}
		}
	}
	nf.Text = strings.TrimSpace(body)
	return nf
}

func sortDesc(names []string) {
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] > names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
}
