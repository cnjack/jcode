package cloud

import (
	"testing"
	"time"
)

func TestBackoffProgressionAndCap(t *testing.T) {
	b := NewBackoff(1*time.Second, 60*time.Second)
	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60, 60}
	for i, w := range want {
		if got := b.Next(); got != w*time.Second {
			t.Fatalf("Next()[%d] = %v, want %v", i, got, w*time.Second)
		}
	}
}

func TestBackoffReset(t *testing.T) {
	b := NewBackoff(1*time.Second, 60*time.Second)
	b.Next()
	b.Next()
	b.Reset()
	if got := b.Next(); got != 1*time.Second {
		t.Fatalf("Next() after Reset = %v, want 1s", got)
	}
}
