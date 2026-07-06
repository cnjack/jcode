package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// LocalExecutor.WriteFile — atomic write semantics (#24)
// ---------------------------------------------------------------------------

// AW-01: Overwriting an existing file preserves its mode (e.g. exec bits).
func TestAtomicWrite_PreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	ex := NewLocalExecutor("linux/amd64")
	if err := ex.WriteFile(context.Background(), path, []byte("#!/bin/sh\necho new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Fatalf("expected mode 0755 preserved, got %o", perm)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "#!/bin/sh\necho new\n" {
		t.Fatalf("unexpected content: %q", data)
	}
}

// AW-02: A new file is created with the requested permission.
func TestAtomicWrite_NewFileUsesPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "new.txt")

	ex := NewLocalExecutor("linux/amd64")
	if err := ex.WriteFile(context.Background(), path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("expected mode 0644, got %o", perm)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "data" {
		t.Fatalf("unexpected content: %q", data)
	}
}

// AW-03: Writing through a symlink updates the target and keeps the link.
func TestAtomicWrite_ThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	ex := NewLocalExecutor("linux/amd64")
	if err := ex.WriteFile(context.Background(), link, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected link.txt to remain a symlink")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("expected symlink target updated, got: %q", data)
	}
}

// AW-04: No temp files are left behind after a successful write.
func TestAtomicWrite_NoTempLeftover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.txt")

	ex := NewLocalExecutor("linux/amd64")
	for i := 0; i < 3; i++ {
		if err := ex.WriteFile(context.Background(), path, []byte("pass"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Fatalf("expected only the target file, got %d entries", len(entries))
	}
}

// AW-05: Overwrite fully replaces old content (no remnants).
func TestAtomicWrite_OverwriteContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "over.txt")

	ex := NewLocalExecutor("linux/amd64")
	if err := ex.WriteFile(context.Background(), path, []byte(strings.Repeat("long-old-content\n", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ex.WriteFile(context.Background(), path, []byte("short\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "short\n" {
		t.Fatalf("expected full replacement, got %d bytes: %q", len(data), data[:min(len(data), 64)])
	}
}

// AW-06: the SSH write command writes a temp file and atomically renames it
// into place (chmod before mv so the final path never holds partial data).
func TestSSHWriteCmd_AtomicMv(t *testing.T) {
	cmd := sshAtomicWriteCmd("QUJD", "/remote/dir/file.txt", 0o644)

	wantTmp := ShellQuote("/remote/dir/file.txt.jcode-tmp")
	wantFinal := ShellQuote("/remote/dir/file.txt")
	if !strings.Contains(cmd, "base64 -d > "+wantTmp) {
		t.Fatalf("expected decode into temp file, got: %s", cmd)
	}
	if !strings.Contains(cmd, "chmod 644 "+wantTmp) {
		t.Fatalf("expected chmod on temp file, got: %s", cmd)
	}
	if !strings.Contains(cmd, "mv -f "+wantTmp+" "+wantFinal) {
		t.Fatalf("expected atomic mv to final path, got: %s", cmd)
	}
	if strings.Contains(cmd, "> "+wantFinal) {
		t.Fatalf("write must not redirect into the final path directly: %s", cmd)
	}
}

// ---------------------------------------------------------------------------
// ResolvePath — semantics fixation (#32)
// ---------------------------------------------------------------------------

// RP-01: table-driven fixation of ResolvePath's documented behavior: absolute
// paths are cleaned and returned; relative paths (including ones escaping the
// working directory) are joined with pwd and cleaned, never rejected.
func TestResolvePath(t *testing.T) {
	env := NewEnv("/work/dir", "linux/amd64")

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"absolute unchanged", "/etc/hosts", "/etc/hosts"},
		{"absolute cleaned", "/x/y/../z", "/x/z"},
		{"absolute trailing slash", "/x/y/", "/x/y"},
		{"relative joins pwd", "a/b", "/work/dir/a/b"},
		{"relative dot segments", "./a/../b", "/work/dir/b"},
		{"relative escapes pwd allowed", "../outside", "/work/outside"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := env.ResolvePath(tc.in); got != tc.want {
				t.Fatalf("ResolvePath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
