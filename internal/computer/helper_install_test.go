package computer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeHelperBundle lays out a minimal jcode-computerd.app at root.
func writeHelperBundle(t *testing.T, root string) string {
	t.Helper()
	files := map[string]struct {
		body string
		perm os.FileMode
	}{
		"Contents/Info.plist":                        {"<plist/>", 0o644},
		"Contents/MacOS/jcode-computerd":             {"daemon-v1", 0o755},
		"Contents/MacOS/jcode-computerd-capture":     {"capture-v1", 0o755},
		"Contents/MacOS/jcode-computerd-onboarding":  {"onboarding-v1", 0o755},
		"Contents/Resources/jcode-computer-use.icns": {"icon", 0o644},
		"Contents/_CodeSignature/CodeResources":      {"sig-v1", 0o644},
	}
	for rel, f := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(f.body), f.perm); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, f.perm); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink("MacOS/jcode-computerd", filepath.Join(root, "Contents", "daemon-link")); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(root, "Contents", "MacOS", "jcode-computerd")
}

func TestRelocateNestedHelperCopiesBundleOutOfHostApp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	src := filepath.Join(base, "jcode.app", "Contents", "Resources", "jcode-computerd.app")
	bin := writeHelperBundle(t, src)
	installDir := filepath.Join(base, "Application Support", "jcode")

	got, err := relocateNestedHelper(bin, installDir)
	if err != nil {
		t.Fatalf("relocateNestedHelper: %v", err)
	}
	dst := filepath.Join(installDir, "jcode-computerd.app")
	want := filepath.Join(dst, "Contents", "MacOS", "jcode-computerd")
	if got != want {
		t.Fatalf("relocated bin = %q, want %q", got, want)
	}
	if isInsideAppBundle(dst) {
		t.Fatalf("installed bundle %s is still inside an app bundle", dst)
	}
	srcDigest, err := bundleDigest(src)
	if err != nil {
		t.Fatal(err)
	}
	dstDigest, err := bundleDigest(dst)
	if err != nil {
		t.Fatal(err)
	}
	if srcDigest != dstDigest {
		t.Fatal("installed bundle differs from the bundled helper")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(got)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("installed daemon mode = %v, want 0755", info.Mode().Perm())
		}
		link, err := os.Readlink(filepath.Join(dst, "Contents", "daemon-link"))
		if err != nil || link != "MacOS/jcode-computerd" {
			t.Fatalf("symlink not preserved: %q, %v", link, err)
		}
	}
}

func TestRelocateNestedHelperSkipsUpToDateCopyAndRefreshesStaleOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	src := filepath.Join(base, "jcode.app", "Contents", "Resources", "jcode-computerd.app")
	bin := writeHelperBundle(t, src)
	installDir := filepath.Join(base, "install")
	dst := filepath.Join(installDir, "jcode-computerd.app")

	if _, err := relocateNestedHelper(bin, installDir); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dst, "Contents", "MacOS", "jcode-computerd")
	before, err := os.Stat(marker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := relocateNestedHelper(bin, installDir); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(marker)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("an up-to-date install was re-copied")
	}

	// An app update ships a new helper; a stray file in the old copy must not
	// survive the refresh.
	if err := os.WriteFile(filepath.Join(src, "Contents", "MacOS", "jcode-computerd"), []byte("daemon-v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "Contents", "stray"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := relocateNestedHelper(bin, installDir); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "daemon-v2" {
		t.Fatalf("installed daemon = %q, want refreshed copy", body)
	}
	if _, err := os.Stat(filepath.Join(dst, "Contents", "stray")); !os.IsNotExist(err) {
		t.Fatalf("stale file survived refresh: %v", err)
	}
	entries, err := os.ReadDir(installDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "jcode-computerd.app" && e.Name() != ".jcode-computerd.app.lock" {
			t.Fatalf("leftover staging entry %q in install dir", e.Name())
		}
	}
}

func TestRelocateNestedHelperLeavesStandaloneHelpersInPlace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	installDir := filepath.Join(base, "install")

	// CLI layout: jcode-computerd.app beside the jcode binary, not in an app.
	standalone := writeHelperBundle(t, filepath.Join(base, "bin", "jcode-computerd.app"))
	// install.sh layout: bare binaries.
	bare := filepath.Join(base, "bin", "jcode-computerd")
	if err := os.WriteFile(bare, []byte("bare"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, bin := range []string{standalone, bare} {
		got, err := relocateNestedHelper(bin, installDir)
		if err != nil {
			t.Fatalf("relocateNestedHelper(%s): %v", bin, err)
		}
		if got != bin {
			t.Fatalf("relocateNestedHelper(%s) = %q, want unchanged", bin, got)
		}
	}
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Fatalf("install dir created for a standalone helper: %v", err)
	}
}

func TestRelocateNestedHelperFollowsSymlinkedExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevated privileges on Windows")
	}
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	bin := writeHelperBundle(t, filepath.Join(base, "jcode.app", "Contents", "Resources", "jcode-computerd.app"))
	link := filepath.Join(base, "jcode-computerd")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(base, "install")
	got, err := relocateNestedHelper(link, installDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(installDir, "jcode-computerd.app", "Contents", "MacOS", "jcode-computerd"); got != want {
		t.Fatalf("relocated bin = %q, want %q", got, want)
	}
}

func TestRelocateNestedHelperRejectsInstallDirInsideAnApp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	bin := writeHelperBundle(t, filepath.Join(base, "jcode.app", "Contents", "Resources", "jcode-computerd.app"))
	if _, err := relocateNestedHelper(bin, filepath.Join(base, "Other.app", "Contents")); err == nil {
		t.Fatal("expected an install dir inside an app bundle to be rejected")
	}
}
