package computer

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func writeStoredShot(t *testing.T, dir, name string, size int, modTime time.Time) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	return path
}

func requirePathState(t *testing.T, path string, wantExists bool) {
	t.Helper()
	_, err := os.Lstat(path)
	if wantExists && err != nil {
		t.Fatalf("%s should exist: %v", path, err)
	}
	if !wantExists && !os.IsNotExist(err) {
		t.Fatalf("%s should have been removed, stat err=%v", path, err)
	}
}

func TestPruneScreenshotStoreTTLCountAndTotalBytes(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

	t.Run("ttl owns only regular png files", func(t *testing.T) {
		dir := t.TempDir()
		old := writeStoredShot(t, dir, uuid.NewString()+".png", 3, now.Add(-2*time.Hour))
		recent := writeStoredShot(t, dir, uuid.NewString()+".png", 3, now.Add(-10*time.Minute))
		foreign := writeStoredShot(t, dir, "old.png", 3, now.Add(-2*time.Hour))
		upper := writeStoredShot(t, dir, uuid.NewString()+".PNG", 3, now.Add(-2*time.Hour))
		note := writeStoredShot(t, dir, "keep.txt", 3, now.Add(-2*time.Hour))
		link := filepath.Join(dir, uuid.NewString()+".png")
		if err := os.Symlink(note, link); err != nil {
			t.Fatal(err)
		}

		err := pruneScreenshotStore(dir, "", now, screenshotStorePolicy{
			maxAge: time.Hour, maxFiles: 10, maxBytes: 100,
		})
		if err != nil {
			t.Fatalf("pruneScreenshotStore: %v", err)
		}
		requirePathState(t, old, false)
		requirePathState(t, recent, true)
		requirePathState(t, foreign, true)
		requirePathState(t, upper, true)
		requirePathState(t, note, true)
		requirePathState(t, link, true)
	})

	t.Run("count removes oldest first", func(t *testing.T) {
		dir := t.TempDir()
		paths := []string{
			writeStoredShot(t, dir, uuid.NewString()+".png", 1, now.Add(-4*time.Minute)),
			writeStoredShot(t, dir, uuid.NewString()+".png", 1, now.Add(-3*time.Minute)),
			writeStoredShot(t, dir, uuid.NewString()+".png", 1, now.Add(-2*time.Minute)),
			writeStoredShot(t, dir, uuid.NewString()+".png", 1, now.Add(-time.Minute)),
		}
		if err := pruneScreenshotStore(dir, "", now, screenshotStorePolicy{maxFiles: 2, maxBytes: 100}); err != nil {
			t.Fatalf("pruneScreenshotStore: %v", err)
		}
		for i, path := range paths {
			requirePathState(t, path, i >= 2)
		}
	})

	t.Run("total bytes removes enough oldest files", func(t *testing.T) {
		dir := t.TempDir()
		a := writeStoredShot(t, dir, uuid.NewString()+".png", 5, now.Add(-3*time.Minute))
		b := writeStoredShot(t, dir, uuid.NewString()+".png", 6, now.Add(-2*time.Minute))
		c := writeStoredShot(t, dir, uuid.NewString()+".png", 7, now.Add(-time.Minute))
		if err := pruneScreenshotStore(dir, "", now, screenshotStorePolicy{maxFiles: 10, maxBytes: 10}); err != nil {
			t.Fatalf("pruneScreenshotStore: %v", err)
		}
		requirePathState(t, a, false)
		requirePathState(t, b, false)
		requirePathState(t, c, true)
	})

	t.Run("current capture is protected", func(t *testing.T) {
		dir := t.TempDir()
		current := writeStoredShot(t, dir, uuid.NewString()+".png", 8, now.Add(-48*time.Hour))
		other := writeStoredShot(t, dir, uuid.NewString()+".png", 8, now.Add(-time.Minute))
		if err := pruneScreenshotStore(dir, current, now, screenshotStorePolicy{
			maxAge: time.Hour, maxFiles: 1, maxBytes: 8,
		}); err != nil {
			t.Fatalf("pruneScreenshotStore: %v", err)
		}
		requirePathState(t, current, true)
		requirePathState(t, other, false)
	})
}

func TestSaveScreenshotPrunesExpiredStoreEntries(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(Config{}, home)
	old := writeStoredShot(t, mgr.shotDir, uuid.NewString()+".png", 3, time.Now().Add(-screenshotStoreTTL-time.Hour))

	id, err := mgr.SaveScreenshot([]byte("current"))
	if err != nil {
		t.Fatalf("SaveScreenshot: %v", err)
	}
	f, err := mgr.OpenScreenshot(id)
	if err != nil {
		t.Fatalf("OpenScreenshot: %v", err)
	}
	_ = f.Close()
	current := filepath.Join(mgr.shotDir, id+".png")
	requirePathState(t, old, false)
	requirePathState(t, current, true)
}

func TestNewManagerSweepsScreenshotsLeftByPriorProcess(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".jcode", "computer", "shots")
	old := writeStoredShot(t, dir, uuid.NewString()+".png", 3, time.Now().Add(-screenshotStoreTTL-time.Hour))
	recent := writeStoredShot(t, dir, uuid.NewString()+".png", 3, time.Now().Add(-time.Minute))

	mgr := NewManager(Config{}, home)
	t.Cleanup(func() { _ = mgr.Close() })
	requirePathState(t, old, false)
	requirePathState(t, recent, true)
}

func TestOpenScreenshotFailsClosedForExpiredAndNonRegularEntries(t *testing.T) {
	mgr := NewManager(Config{}, t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })

	expiredID := uuid.NewString()
	expired := writeStoredShot(
		t,
		mgr.shotDir,
		expiredID+".png",
		3,
		time.Now().Add(-screenshotStoreTTL-time.Hour),
	)
	if _, err := mgr.OpenScreenshot(expiredID); err == nil {
		t.Fatal("OpenScreenshot accepted an expired cache entry")
	}
	requirePathState(t, expired, false)

	target := writeStoredShot(t, t.TempDir(), "target.png", 3, time.Now())
	symlinkID := uuid.NewString()
	symlink := filepath.Join(mgr.shotDir, symlinkID+".png")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.OpenScreenshot(symlinkID); err == nil {
		t.Fatal("OpenScreenshot accepted a symlink cache entry")
	}
	requirePathState(t, symlink, true)
	requirePathState(t, target, true)

	directoryID := uuid.NewString()
	directory := filepath.Join(mgr.shotDir, directoryID+".png")
	if err := os.MkdirAll(filepath.Join(directory, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.OpenScreenshot(directoryID); err == nil {
		t.Fatal("OpenScreenshot accepted a directory cache entry")
	}
	requirePathState(t, directory, true)
}

func TestScreenshotStoreRootSymlinkFailsClosed(t *testing.T) {
	home := t.TempDir()
	victim := t.TempDir()
	victimID := uuid.NewString()
	victimPNG := writeStoredShot(
		t, victim, victimID+".png", 8, time.Now().Add(-screenshotStoreTTL-time.Hour),
	)
	parent := filepath.Join(home, ".jcode", "computer")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(parent, "shots")); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(Config{}, home)
	t.Cleanup(func() { _ = mgr.Close() })
	requirePathState(t, victimPNG, true)
	if _, err := mgr.SaveScreenshot([]byte("new pixels")); err == nil {
		t.Fatal("SaveScreenshot followed a symlink cache root")
	}
	if _, err := mgr.OpenScreenshot(victimID); err == nil {
		t.Fatal("OpenScreenshot followed a symlink cache root")
	}
	requirePathState(t, victimPNG, true)
}

func TestOpenScreenshotHandleSurvivesNameRemoval(t *testing.T) {
	mgr := NewManager(Config{}, t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })
	want := []byte("immutable screenshot bytes")
	id, err := mgr.SaveScreenshot(want)
	if err != nil {
		t.Fatal(err)
	}
	f, err := mgr.OpenScreenshot(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := os.Remove(filepath.Join(mgr.shotDir, id+".png")); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("opened handle changed after unlink: got %q want %q", got, want)
	}
}

func TestScreenshotStoreFileLockSerializesManagers(t *testing.T) {
	home := t.TempDir()
	first := NewManager(Config{}, home)
	second := NewManager(Config{}, home)
	t.Cleanup(func() { _ = first.Close() })
	t.Cleanup(func() { _ = second.Close() })

	lock, err := acquireScreenshotFileLock(screenshotStoreLockPath(first.shotDir))
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := second.SaveScreenshot([]byte("pixels"))
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		_ = lock.release()
		t.Fatalf("SaveScreenshot bypassed the cross-process lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := lock.release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SaveScreenshot did not resume after the store lock was released")
	}
}
