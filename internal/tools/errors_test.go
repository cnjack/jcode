package tools

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

// #16: Fatal/IsFatal sentinel behavior, including nil handling, %w-chain
// penetration, and text preservation.
func TestFatal_IsFatal(t *testing.T) {
	base := errors.New("container removed")

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"plain error is not fatal", base, false},
		{"Fatal-wrapped error is fatal", Fatal(base), true},
		{"fatal survives %w wrapping", fmt.Errorf("wrap: %w", Fatal(base)), true},
		{"wrapped non-fatal stays non-fatal", fmt.Errorf("wrap: %w", base), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsFatal(tt.err); got != tt.want {
				t.Fatalf("IsFatal(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}

	if Fatal(nil) != nil {
		t.Fatal("Fatal(nil) must return nil")
	}
	if got := Fatal(base).Error(); got != base.Error() {
		t.Fatalf("Fatal must not change the error text: got %q, want %q", got, base.Error())
	}
	if !errors.Is(Fatal(base), base) {
		t.Fatal("Fatal must preserve the errors.Is chain via Unwrap")
	}
}

// #28: ToolError bakes the hint into Error() (Err text first, so logs stay
// grep-able), preserves the Unwrap chain, and carries a readable Code.
func TestToolError_Format(t *testing.T) {
	t.Run("hint appended after original text", func(t *testing.T) {
		err := toolErrf("write_failed", "do X", "boom")
		if got, want := err.Error(), "boom. Hint: do X"; got != want {
			t.Fatalf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("errors.Is penetrates Unwrap chain", func(t *testing.T) {
		te := &ToolError{
			Code: "read_failed",
			Hint: "check permissions",
			Err:  fmt.Errorf("stat failed: %w", os.ErrPermission),
		}
		if !errors.Is(te, os.ErrPermission) {
			t.Fatal("errors.Is must reach os.ErrPermission through ToolError.Unwrap")
		}
	})

	t.Run("errors.As exposes the Code field", func(t *testing.T) {
		wrapped := fmt.Errorf("outer: %w", toolErrf("file_not_found", "use grep", "no such file"))
		var te *ToolError
		if !errors.As(wrapped, &te) {
			t.Fatal("errors.As must find *ToolError in the chain")
		}
		if te.Code != "file_not_found" {
			t.Fatalf("Code = %q, want %q", te.Code, "file_not_found")
		}
	})

	t.Run("empty hint keeps original text only", func(t *testing.T) {
		err := &ToolError{Code: "x", Err: errors.New("plain")}
		if got := err.Error(); got != "plain" {
			t.Fatalf("Error() = %q, want %q", got, "plain")
		}
	})
}
