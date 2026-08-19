package workspace

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCreateScratchUsesDailyMonotonicSequence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, time.August, 19, 23, 59, 0, 0, time.Local)

	first, err := CreateScratch(now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateScratch(now)
	if err != nil {
		t.Fatal(err)
	}
	nextDay, err := CreateScratch(now.Add(2 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	if got, want := filepath.Base(first), "2026-08-19-001"; got != want {
		t.Fatalf("first workspace = %q, want %q", got, want)
	}
	if got, want := filepath.Base(second), "2026-08-19-002"; got != want {
		t.Fatalf("second workspace = %q, want %q", got, want)
	}
	if got, want := filepath.Base(nextDay), "2026-08-20-001"; got != want {
		t.Fatalf("next-day workspace = %q, want %q", got, want)
	}
}

func TestCreateScratchIsConcurrentSafe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.Local)

	const count = 16
	paths := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := CreateScratch(now)
			if err != nil {
				errs <- err
				return
			}
			paths <- path
		}()
	}
	wg.Wait()
	close(paths)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := make(map[string]bool, count)
	for path := range paths {
		if seen[path] {
			t.Fatalf("duplicate workspace allocated: %s", path)
		}
		seen[path] = true
	}
	if len(seen) != count {
		t.Fatalf("allocated %d workspaces, want %d", len(seen), count)
	}
}
