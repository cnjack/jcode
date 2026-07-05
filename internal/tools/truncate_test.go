package tools

import (
	"fmt"
	"strings"
	"testing"
)

// numberedLines returns n lines of the form "L00000\n".."L<n-1>\n" (7 bytes each).
func numberedLines(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "L%05d\n", i)
	}
	return b.String()
}

func TestTruncateHeadTail(t *testing.T) {
	tests := []struct {
		name             string
		in               string
		headMax, tailMax int
		wantTrunc        bool
	}{
		{"empty", "", 100, 100, false},
		{"short input untouched", "hello\nworld\n", 100, 100, false},
		{"exactly at limit untouched", strings.Repeat("a", 200), 100, 100, false},
		{"one byte over limit", strings.Repeat("a", 201), 100, 100, true},
		{"long multi-line", numberedLines(10000), 1000, 1500, true},
		{"single huge line without newlines", strings.Repeat("x", 5000), 1000, 1000, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, droppedBytes, droppedLines := truncateHeadTail(tc.in, tc.headMax, tc.tailMax)
			if !tc.wantTrunc {
				if out != tc.in || droppedBytes != 0 || droppedLines != 0 {
					t.Fatalf("want input untouched, got len(out)=%d droppedBytes=%d droppedLines=%d",
						len(out), droppedBytes, droppedLines)
				}
				return
			}
			if droppedBytes <= 0 {
				t.Fatalf("droppedBytes = %d, want > 0", droppedBytes)
			}
			marker := fmt.Sprintf("\n[... output truncated: %d bytes (~%d lines) dropped ...]\n",
				droppedBytes, droppedLines)
			i := strings.Index(out, marker)
			if i < 0 {
				t.Fatalf("output missing marker %q:\n%.200q", marker, out)
			}
			// droppedBytes accounting: out = head + marker + tail.
			if want := len(tc.in) - (len(out) - len(marker)); droppedBytes != want {
				t.Fatalf("droppedBytes = %d, want %d (in=%d out=%d marker=%d)",
					droppedBytes, want, len(tc.in), len(out), len(marker))
			}
			// The head must be a prefix and the tail a suffix of the input.
			head, tail := out[:i], out[i+len(marker):]
			if !strings.HasPrefix(tc.in, head) {
				t.Fatalf("head is not a prefix of the input: %.80q", head)
			}
			if !strings.HasSuffix(tc.in, tail) {
				t.Fatalf("tail is not a suffix of the input: %.80q", tail)
			}
			if head == "" || tail == "" {
				t.Fatalf("head/tail must both be non-empty, got head=%d tail=%d bytes", len(head), len(tail))
			}
		})
	}
}

// TestTruncateHeadTail_NewlineAlignment verifies that head and tail are aligned
// to line boundaries: no line of the input is cut in the middle.
func TestTruncateHeadTail_NewlineAlignment(t *testing.T) {
	in := numberedLines(10000)
	out, _, _ := truncateHeadTail(in, 1000, 1500)

	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, "[... output truncated:") {
			continue
		}
		if len(line) != 6 || line[0] != 'L' {
			t.Fatalf("line %q was cut mid-line", line)
		}
	}
	if !strings.HasPrefix(out, "L00000\n") {
		t.Fatalf("first line missing from head: %.20q", out)
	}
	if !strings.HasSuffix(out, "L09999\n") {
		t.Fatalf("last line missing from tail: %.20q", out[len(out)-20:])
	}
}
