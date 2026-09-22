//go:build !windows

package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDraftCommandCancellationStopsChildProcesses(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "child-survived")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := draftCommand(ctx, dir, "sh", os.Environ(), "", "-c", `(sleep 0.5; touch "$1") & wait`, "sh", marker)
	if err == nil {
		t.Fatal("cancelled command succeeded")
	}
	time.Sleep(700 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("cancelled delivery left a running child")
	}
}
