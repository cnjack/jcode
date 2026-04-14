package model

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cnjack/jcode/internal/config"
	openai "github.com/sashabaranov/go-openai"
)

// ---------------------------------------------------------------------------
// Error Classification
// ---------------------------------------------------------------------------

// APIErrorCategory classifies LLM API errors into actionable categories.
type APIErrorCategory int

const (
	// ErrCategoryTransient — network blips, timeouts, 5xx; safe to retry.
	ErrCategoryTransient APIErrorCategory = iota
	// ErrCategoryRateLimit — 429 / "overloaded"; retry with back-off.
	ErrCategoryRateLimit
	// ErrCategoryContextOverflow — input too long; needs compaction, NOT retry.
	ErrCategoryContextOverflow
	// ErrCategoryAuth — 401/403; permanent until key is fixed.
	ErrCategoryAuth
	// ErrCategoryFatal — 400 bad request, unknown; do not retry.
	ErrCategoryFatal
)

func (c APIErrorCategory) String() string {
	switch c {
	case ErrCategoryTransient:
		return "transient"
	case ErrCategoryRateLimit:
		return "rate_limit"
	case ErrCategoryContextOverflow:
		return "context_overflow"
	case ErrCategoryAuth:
		return "auth"
	case ErrCategoryFatal:
		return "fatal"
	default:
		return "unknown"
	}
}

// contextOverflowPatterns matches error messages from various LLM providers
// that indicate the input exceeded the model's context window.
// Sourced from Claude-Code & OpenCode's 40+ patterns.
var contextOverflowPatterns = []*regexp.Regexp{
	// OpenAI-compatible
	regexp.MustCompile(`(?i)context.length.exceeded`),
	regexp.MustCompile(`(?i)maximum context length`),
	regexp.MustCompile(`(?i)max.tokens.exceed.*context`),
	regexp.MustCompile(`(?i)input.*too long`),
	regexp.MustCompile(`(?i)prompt is too long`),
	regexp.MustCompile(`(?i)token limit`),
	regexp.MustCompile(`(?i)request too large`),
	regexp.MustCompile(`(?i)exceeds? the model.s.*(?:token|context|input)`),
	// Anthropic
	regexp.MustCompile(`(?i)prompt is too long:\s*\d+\s*tokens?\s*>\s*\d+`),
	regexp.MustCompile(`(?i)input length and.*max_tokens.*exceed.*context limit`),
	// Google / Gemini
	regexp.MustCompile(`(?i)exceeds? the maximum number of tokens`),
	regexp.MustCompile(`(?i)exceeds? the maximum input token`),
	// Generic
	regexp.MustCompile(`(?i)content.*too.*large`),
	regexp.MustCompile(`(?i)payload.*too.*large`),
}

// rateLimitPatterns matches error messages indicating rate-limiting.
var rateLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)rate.limit`),
	regexp.MustCompile(`(?i)too many requests`),
	regexp.MustCompile(`(?i)overloaded`),
	regexp.MustCompile(`(?i)resource.exhausted`),
	regexp.MustCompile(`(?i)quota.exceeded`),
	regexp.MustCompile(`(?i)throttl`),
}

// ClassifyError determines the category of an API error.
func ClassifyError(err error) APIErrorCategory {
	if err == nil {
		return ErrCategoryFatal
	}

	msg := err.Error()

	// Check OpenAI SDK typed errors first.
	var apiErr *openai.APIError
	if asAPIErr(err, &apiErr) {
		return classifyByStatus(apiErr.HTTPStatusCode, msg)
	}

	// Network-level errors are always transient.
	var netErr net.Error
	if asNetErr(err, &netErr) {
		return ErrCategoryTransient
	}

	// Fall back to message text matching.
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded") {
		return ErrCategoryTransient
	}
	if strings.Contains(lower, "connection refused") || strings.Contains(lower, "connection reset") {
		return ErrCategoryTransient
	}

	for _, re := range contextOverflowPatterns {
		if re.MatchString(msg) {
			return ErrCategoryContextOverflow
		}
	}
	for _, re := range rateLimitPatterns {
		if re.MatchString(msg) {
			return ErrCategoryRateLimit
		}
	}

	return ErrCategoryFatal
}

func classifyByStatus(status int, msg string) APIErrorCategory {
	switch {
	case status == 429:
		return ErrCategoryRateLimit
	case status == 529:
		return ErrCategoryRateLimit // Anthropic "overloaded"
	case status == 401 || status == 403:
		return ErrCategoryAuth
	case status == 408 || status == 409:
		return ErrCategoryTransient
	case status >= 500 && status < 600:
		return ErrCategoryTransient
	case status == 413:
		return ErrCategoryContextOverflow
	case status == 400:
		// 400 may contain context overflow info.
		for _, re := range contextOverflowPatterns {
			if re.MatchString(msg) {
				return ErrCategoryContextOverflow
			}
		}
		return ErrCategoryFatal
	default:
		return ErrCategoryFatal
	}
}

// ---------------------------------------------------------------------------
// Retry-After Header Parsing
// ---------------------------------------------------------------------------

// ParseRetryAfter extracts a delay from an error message or OpenAI APIError.
// It looks for Retry-After header patterns in the error text.
// Returns 0 if no delay information is found.
func ParseRetryAfter(err error) time.Duration {
	var apiErr *openai.APIError
	if asAPIErr(err, &apiErr) {
		return parseRetryAfterFromAPIError(apiErr)
	}
	return 0
}

func parseRetryAfterFromAPIError(apiErr *openai.APIError) time.Duration {
	if apiErr == nil {
		return 0
	}
	// The go-openai library includes the header value in the error message
	// like "Rate limit reached... Please retry after Xs" or similar.
	msg := apiErr.Message
	return parseRetryAfterFromMessage(msg)
}

// parseRetryAfterFromMessage tries to extract a retry delay from error text.
// Supports patterns like:
//
//	"Please retry after 30s"
//	"try again in 30 seconds"
//	"retry after 2.5 seconds"
//	"Retry-After: 30"
var retryAfterSecsPattern = regexp.MustCompile(
	`(?i)(?:retry.after|try.again.in|please.wait)\s*[:=]?\s*(\d+(?:\.\d+)?)\s*(?:s|sec|seconds?)?\b`,
)

var retryAfterMsPattern = regexp.MustCompile(
	`(?i)retry.after.ms\s*[:=]?\s*(\d+)`,
)

func parseRetryAfterFromMessage(msg string) time.Duration {
	// Check millisecond header first (higher precision).
	if m := retryAfterMsPattern.FindStringSubmatch(msg); len(m) > 1 {
		if ms, err := strconv.ParseInt(m[1], 10, 64); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	// Check seconds-based pattern.
	if m := retryAfterSecsPattern.FindStringSubmatch(msg); len(m) > 1 {
		if secs, err := strconv.ParseFloat(m[1], 64); err == nil && secs > 0 {
			return time.Duration(secs * float64(time.Second))
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// Retry Decision & Backoff (for Eino ModelRetryConfig)
// ---------------------------------------------------------------------------

const (
	baseDelay      = 500 * time.Millisecond
	maxDelay       = 32 * time.Second
	maxRetryAfter  = 5 * time.Minute // cap server-suggested delay
	jitterFraction = 0.25            // 0-25% random jitter
)

// IsRetryable returns true if the error should be retried. It is designed to
// be used as ModelRetryConfig.IsRetryAble in the Eino framework.
//
// Context overflow errors are NOT retryable — they need compaction.
// Auth errors are NOT retryable — they need user action.
func IsRetryable(_ context.Context, err error) bool {
	cat := ClassifyError(err)
	switch cat {
	case ErrCategoryTransient, ErrCategoryRateLimit:
		return true
	default:
		return false
	}
}

// SmartBackoff returns a delay for the given retry attempt, respecting
// server-sent Retry-After hints when available. It is designed to be used
// as ModelRetryConfig.BackoffFunc in the Eino framework.
//
// Strategy (matching Claude-Code & OpenCode patterns):
//  1. If the error contains a Retry-After hint, use it (capped at 5 min).
//  2. Otherwise fall back to exponential backoff: 500ms × 2^(attempt-1),
//     capped at 32s, plus 0-25% random jitter.
func SmartBackoff(ctx context.Context, attempt int) time.Duration {
	// Try to extract Retry-After from the most recent error via context.
	// Eino does not pass the error to BackoffFunc, so we rely on the
	// context wrapper set by our middleware (see retryErrorKey below).
	if retryErr := retryErrorFromCtx(ctx); retryErr != nil {
		if serverDelay := ParseRetryAfter(retryErr); serverDelay > 0 {
			delay := serverDelay
			if delay > maxRetryAfter {
				delay = maxRetryAfter
			}
			config.Logger().Printf("[retry] attempt %d: using server Retry-After %v (capped from %v)", attempt, delay, serverDelay)
			return delay
		}
	}

	return exponentialBackoff(attempt)
}

// exponentialBackoff calculates delay: baseDelay × 2^(attempt-1) + jitter,
// capped at maxDelay.
func exponentialBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	exp := math.Min(float64(baseDelay)*math.Pow(2, float64(attempt-1)), float64(maxDelay))
	jitter := exp * jitterFraction * rand.Float64()
	delay := time.Duration(exp + jitter)
	config.Logger().Printf("[retry] attempt %d: exponential backoff %v", attempt, delay)
	return delay
}

// ---------------------------------------------------------------------------
// Context key for passing last error to BackoffFunc (works around Eino API)
// ---------------------------------------------------------------------------

type retryErrorKeyType struct{}

var retryErrorKey retryErrorKeyType

// WithRetryError stores an error in context for BackoffFunc to inspect.
func WithRetryError(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, retryErrorKey, err)
}

func retryErrorFromCtx(ctx context.Context) error {
	if v := ctx.Value(retryErrorKey); v != nil {
		if err, ok := v.(error); ok {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers — type assertions via interface matching (avoids import cycles)
// ---------------------------------------------------------------------------

func asAPIErr(err error, target **openai.APIError) bool {
	// Walk the error chain.
	for err != nil {
		if ae, ok := err.(*openai.APIError); ok {
			*target = ae
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func asNetErr(err error, target *net.Error) bool {
	for err != nil {
		if ne, ok := err.(net.Error); ok {
			*target = ne
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// ---------------------------------------------------------------------------
// Context Overflow Info Extraction
// ---------------------------------------------------------------------------

// ContextOverflowInfo holds parsed token counts from an overflow error.
type ContextOverflowInfo struct {
	ActualTokens int
	LimitTokens  int
	TokenGap     int // ActualTokens - LimitTokens
}

var overflowTokenPattern = regexp.MustCompile(
	`(\d+)\s*(?:\+\s*\d+\s*)?>(?:\s*=)?\s*(\d+)`,
)

var promptTooLongPattern = regexp.MustCompile(
	`(?i)(?:prompt is too long|too long)[^0-9]*(\d+)\s*tokens?\s*>\s*(\d+)`,
)

// ParseContextOverflow extracts token counts from a context overflow error.
// Returns nil if the error is not a context overflow or counts cannot be parsed.
func ParseContextOverflow(err error) *ContextOverflowInfo {
	if err == nil {
		return nil
	}
	msg := err.Error()

	// Try "prompt is too long: 137500 tokens > 135000"
	if m := promptTooLongPattern.FindStringSubmatch(msg); len(m) > 2 {
		actual, _ := strconv.Atoi(m[1])
		limit, _ := strconv.Atoi(m[2])
		if actual > 0 && limit > 0 {
			return &ContextOverflowInfo{
				ActualTokens: actual,
				LimitTokens:  limit,
				TokenGap:     actual - limit,
			}
		}
	}

	// Try "input length and max_tokens exceed context limit: 188059 + 20000 > 200000"
	if m := overflowTokenPattern.FindStringSubmatch(msg); len(m) > 2 {
		actual, _ := strconv.Atoi(m[1])
		limit, _ := strconv.Atoi(m[2])
		if actual > 0 && limit > 0 {
			return &ContextOverflowInfo{
				ActualTokens: actual,
				LimitTokens:  limit,
				TokenGap:     actual - limit,
			}
		}
	}

	return nil
}

// FormatAPIError produces a user-friendly error message with retry context.
func FormatAPIError(err error, attempt, maxRetries int) string {
	cat := ClassifyError(err)
	switch cat {
	case ErrCategoryRateLimit:
		retryAfter := ParseRetryAfter(err)
		if retryAfter > 0 {
			return fmt.Sprintf("Rate limited (attempt %d/%d). Retrying in %v...", attempt, maxRetries, retryAfter.Round(time.Second))
		}
		return fmt.Sprintf("Rate limited (attempt %d/%d). Retrying with backoff...", attempt, maxRetries)
	case ErrCategoryTransient:
		return fmt.Sprintf("Temporary API error (attempt %d/%d). Retrying...", attempt, maxRetries)
	case ErrCategoryContextOverflow:
		info := ParseContextOverflow(err)
		if info != nil {
			return fmt.Sprintf("Context overflow: %d tokens exceeds limit of %d (by %d tokens). Compaction needed.",
				info.ActualTokens, info.LimitTokens, info.TokenGap)
		}
		return "Context overflow: input too long for model. Compaction needed."
	case ErrCategoryAuth:
		return "Authentication error. Please check your API key and provider configuration."
	default:
		return fmt.Sprintf("API error: %v", err)
	}
}
