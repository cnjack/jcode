package model

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// ---------------------------------------------------------------------------
// ClassifyError
// ---------------------------------------------------------------------------

func TestClassifyError_Nil(t *testing.T) {
	if cat := ClassifyError(nil); cat != ErrCategoryFatal {
		t.Errorf("nil error: got %v, want Fatal", cat)
	}
}

func TestClassifyError_RateLimit429(t *testing.T) {
	err := &openai.APIError{HTTPStatusCode: 429, Message: "rate limit"}
	if cat := ClassifyError(err); cat != ErrCategoryRateLimit {
		t.Errorf("429: got %v, want RateLimit", cat)
	}
}

func TestClassifyError_Overloaded529(t *testing.T) {
	err := &openai.APIError{HTTPStatusCode: 529, Message: "overloaded"}
	if cat := ClassifyError(err); cat != ErrCategoryRateLimit {
		t.Errorf("529: got %v, want RateLimit", cat)
	}
}

func TestClassifyError_Transient5xx(t *testing.T) {
	for _, status := range []int{500, 502, 503} {
		err := &openai.APIError{HTTPStatusCode: status, Message: "server error"}
		if cat := ClassifyError(err); cat != ErrCategoryTransient {
			t.Errorf("status %d: got %v, want Transient", status, cat)
		}
	}
}

func TestClassifyError_Auth(t *testing.T) {
	for _, status := range []int{401, 403} {
		err := &openai.APIError{HTTPStatusCode: status, Message: "unauthorized"}
		if cat := ClassifyError(err); cat != ErrCategoryAuth {
			t.Errorf("status %d: got %v, want Auth", status, cat)
		}
	}
}

func TestClassifyError_ContextOverflow400(t *testing.T) {
	err := &openai.APIError{
		HTTPStatusCode: 400,
		Message:        "This model's maximum context length is 128000 tokens",
	}
	if cat := ClassifyError(err); cat != ErrCategoryContextOverflow {
		t.Errorf("overflow via 400: got %v, want ContextOverflow", cat)
	}
}

func TestClassifyError_ContextOverflow413(t *testing.T) {
	err := &openai.APIError{HTTPStatusCode: 413, Message: "payload too large"}
	if cat := ClassifyError(err); cat != ErrCategoryContextOverflow {
		t.Errorf("413: got %v, want ContextOverflow", cat)
	}
}

func TestClassifyError_ContextOverflowByMessage(t *testing.T) {
	tests := []string{
		"prompt is too long: 137500 tokens > 135000 maximum",
		"context_length_exceeded",
		"input length and `max_tokens` exceed context limit: 188059 + 20000 > 200000",
		"request too large for the model",
	}
	for _, msg := range tests {
		err := errors.New(msg)
		if cat := ClassifyError(err); cat != ErrCategoryContextOverflow {
			t.Errorf("msg %q: got %v, want ContextOverflow", msg, cat)
		}
	}
}

func TestClassifyError_RateLimitByMessage(t *testing.T) {
	tests := []string{
		"Too many requests",
		"rate limit reached",
		"Resource exhausted",
		"quota exceeded for project",
		"API is overloaded",
	}
	for _, msg := range tests {
		err := errors.New(msg)
		if cat := ClassifyError(err); cat != ErrCategoryRateLimit {
			t.Errorf("msg %q: got %v, want RateLimit", msg, cat)
		}
	}
}

func TestClassifyError_TransientByMessage(t *testing.T) {
	tests := []string{
		"request timeout after 30s",
		"context deadline exceeded",
		"connection refused",
		"connection reset by peer",
	}
	for _, msg := range tests {
		err := errors.New(msg)
		if cat := ClassifyError(err); cat != ErrCategoryTransient {
			t.Errorf("msg %q: got %v, want Transient", msg, cat)
		}
	}
}

func TestClassifyError_NetError(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	if cat := ClassifyError(err); cat != ErrCategoryTransient {
		t.Errorf("net.Error: got %v, want Transient", cat)
	}
}

func TestClassifyError_Fatal400(t *testing.T) {
	err := &openai.APIError{HTTPStatusCode: 400, Message: "invalid model name"}
	if cat := ClassifyError(err); cat != ErrCategoryFatal {
		t.Errorf("400 non-overflow: got %v, want Fatal", cat)
	}
}

// ---------------------------------------------------------------------------
// ParseRetryAfter
// ---------------------------------------------------------------------------

func TestParseRetryAfterFromMessage(t *testing.T) {
	tests := []struct {
		msg      string
		wantMin  time.Duration
		wantMax  time.Duration
		wantZero bool
	}{
		{"Please retry after 30s", 29 * time.Second, 31 * time.Second, false},
		{"try again in 2 seconds", 1500 * time.Millisecond, 2500 * time.Millisecond, false},
		{"retry after 1.5 seconds", 1400 * time.Millisecond, 1600 * time.Millisecond, false},
		{"Retry-After: 60", 59 * time.Second, 61 * time.Second, false},
		{"retry-after-ms: 5000", 4900 * time.Millisecond, 5100 * time.Millisecond, false},
		{"some random error", 0, 0, true},
	}
	for _, tt := range tests {
		d := parseRetryAfterFromMessage(tt.msg)
		if tt.wantZero {
			if d != 0 {
				t.Errorf("msg %q: got %v, want 0", tt.msg, d)
			}
			continue
		}
		if d < tt.wantMin || d > tt.wantMax {
			t.Errorf("msg %q: got %v, want [%v, %v]", tt.msg, d, tt.wantMin, tt.wantMax)
		}
	}
}

// ---------------------------------------------------------------------------
// IsRetryable
// ---------------------------------------------------------------------------

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{&openai.APIError{HTTPStatusCode: 429, Message: "rate limit"}, true},
		{&openai.APIError{HTTPStatusCode: 529, Message: "overloaded"}, true},
		{&openai.APIError{HTTPStatusCode: 500, Message: "internal error"}, true},
		{errors.New("connection reset by peer"), true},
		{&openai.APIError{HTTPStatusCode: 401, Message: "unauthorized"}, false},
		{&openai.APIError{HTTPStatusCode: 400, Message: "bad request"}, false},
		{errors.New("context_length_exceeded"), false}, // needs compaction, not retry
	}
	for _, tt := range tests {
		got := IsRetryable(context.Background(), tt.err)
		if got != tt.want {
			t.Errorf("IsRetryable(%v): got %v, want %v", tt.err, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Exponential Backoff
// ---------------------------------------------------------------------------

func TestExponentialBackoff_Progression(t *testing.T) {
	// Verify delays increase roughly exponentially.
	prev := time.Duration(0)
	for attempt := 1; attempt <= 7; attempt++ {
		d := exponentialBackoff(attempt)
		if d <= prev && attempt <= 6 { // with jitter, should still increase
			// Allow some slack: re-test with multiple samples
			allSmaller := true
			for i := 0; i < 10; i++ {
				if exponentialBackoff(attempt) > prev {
					allSmaller = false
					break
				}
			}
			if allSmaller {
				t.Errorf("attempt %d: delay %v not > prev %v", attempt, d, prev)
			}
		}
		prev = d
	}
}

func TestExponentialBackoff_Cap(t *testing.T) {
	// Very high attempt should still be capped.
	for i := 0; i < 10; i++ {
		d := exponentialBackoff(100)
		if d > maxDelay+time.Duration(float64(maxDelay)*jitterFraction)+time.Millisecond {
			t.Errorf("attempt 100: delay %v exceeds max+jitter", d)
		}
	}
}

// ---------------------------------------------------------------------------
// ParseContextOverflow
// ---------------------------------------------------------------------------

func TestParseContextOverflow(t *testing.T) {
	tests := []struct {
		msg        string
		wantActual int
		wantLimit  int
		wantNonNil bool
	}{
		{"prompt is too long: 137500 tokens > 135000", 137500, 135000, true},
		{"input length and `max_tokens` exceed context limit: 188059 + 20000 > 200000", 188059, 200000, true},
		{"bad request: invalid model", 0, 0, false},
	}
	for _, tt := range tests {
		info := ParseContextOverflow(errors.New(tt.msg))
		if tt.wantNonNil {
			if info == nil {
				t.Errorf("msg %q: got nil, want non-nil", tt.msg)
				continue
			}
			if info.ActualTokens != tt.wantActual {
				t.Errorf("msg %q: actual=%d, want %d", tt.msg, info.ActualTokens, tt.wantActual)
			}
			if info.LimitTokens != tt.wantLimit {
				t.Errorf("msg %q: limit=%d, want %d", tt.msg, info.LimitTokens, tt.wantLimit)
			}
		} else if info != nil {
			t.Errorf("msg %q: got %+v, want nil", tt.msg, info)
		}
	}
}

// ---------------------------------------------------------------------------
// FormatAPIError
// ---------------------------------------------------------------------------

func TestFormatAPIError_Categories(t *testing.T) {
	tests := []struct {
		err      error
		contains string
	}{
		{&openai.APIError{HTTPStatusCode: 429, Message: "rate limit; retry after 5s"}, "Rate limited"},
		{&openai.APIError{HTTPStatusCode: 500, Message: "internal"}, "Temporary API error"},
		{errors.New("prompt is too long: 100000 tokens > 90000"), "Context overflow"},
		{&openai.APIError{HTTPStatusCode: 401, Message: "invalid key"}, "Authentication error"},
		{errors.New("bad request"), "API error"},
	}
	for _, tt := range tests {
		msg := FormatAPIError(tt.err, 1, 3)
		if !contains(msg, tt.contains) {
			t.Errorf("FormatAPIError(%v): %q does not contain %q", tt.err, msg, tt.contains)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// SmartBackoff with context
// ---------------------------------------------------------------------------

func TestSmartBackoff_WithRetryError(t *testing.T) {
	// Inject a 429 error with "retry after 10s" into context.
	err := &openai.APIError{HTTPStatusCode: 429, Message: "retry after 10s"}
	ctx := WithRetryError(context.Background(), err)
	d := SmartBackoff(ctx, 1)
	if d < 9*time.Second || d > 11*time.Second {
		t.Errorf("SmartBackoff with retry-after 10s: got %v", d)
	}
}

func TestSmartBackoff_WithoutRetryError(t *testing.T) {
	d := SmartBackoff(context.Background(), 1)
	// Should be baseDelay (500ms) + up to 25% jitter
	if d < 400*time.Millisecond || d > 700*time.Millisecond {
		t.Errorf("SmartBackoff attempt 1 without error: got %v, want ~500ms", d)
	}
}

func TestSmartBackoffWithMaxRetriesPublishesConfiguredBound(t *testing.T) {
	var got RetryBackoffEvent
	observer := NewRetryObserver(func(event RetryBackoffEvent) { got = event })
	ctx := WithRetryObserver(context.Background(), observer)
	err := &openai.APIError{HTTPStatusCode: 429, Message: "rate limited"}
	if !IsRetryable(ctx, err) {
		t.Fatal("429 error was not classified as retryable")
	}

	_ = SmartBackoffWithMaxRetries(3)(ctx, 1)

	if got.Attempt != 1 || got.MaxAttempts != 3 || got.Delay <= 0 {
		t.Fatalf("retry event = %+v, want attempt 1 with max 3 and positive delay", got)
	}
}

// ---------------------------------------------------------------------------
// Category String
// ---------------------------------------------------------------------------

func TestAPIErrorCategory_String(t *testing.T) {
	tests := []struct {
		cat  APIErrorCategory
		want string
	}{
		{ErrCategoryTransient, "transient"},
		{ErrCategoryRateLimit, "rate_limit"},
		{ErrCategoryContextOverflow, "context_overflow"},
		{ErrCategoryAuth, "auth"},
		{ErrCategoryFatal, "fatal"},
		{APIErrorCategory(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.cat.String(); got != tt.want {
			t.Errorf("String(%d): got %q, want %q", tt.cat, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Wrapped errors
// ---------------------------------------------------------------------------

func TestClassifyError_WrappedAPIError(t *testing.T) {
	inner := &openai.APIError{HTTPStatusCode: 429, Message: "rate limited"}
	wrapped := fmt.Errorf("openai call failed: %w", inner)
	if cat := ClassifyError(wrapped); cat != ErrCategoryRateLimit {
		t.Errorf("wrapped 429: got %v, want RateLimit", cat)
	}
}
