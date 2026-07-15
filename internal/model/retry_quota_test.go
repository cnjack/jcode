package model

import (
	"errors"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func apiErr(status int, msg string) error {
	return &openai.APIError{HTTPStatusCode: status, Message: msg}
}

func TestClassifyQuotaVsRateLimit(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want APIErrorCategory
	}{
		// The error that started this: TokenHub, observed live.
		{"402 tokenhub free trial", apiErr(402,
			"The free trial quota for the service has been exhausted and postpaid billing is not enabled, "+
				"so the service cannot be accessed."), ErrCategoryQuota},
		{"402 bare", apiErr(402, "Payment Required"), ErrCategoryQuota},

		// A plain 429 is pace, not money: back off and it clears.
		{"429 plain", apiErr(429, "Rate limit reached for requests"), ErrCategoryRateLimit},
		{"429 too many requests", apiErr(429, "Too Many Requests"), ErrCategoryRateLimit},

		// …but a 429 *about money* must not be retried forever. Some gateways
		// return 429 when a prepaid balance hits zero, and no amount of backoff
		// refills a wallet.
		{"429 that is really a quota", apiErr(429,
			"You exceeded your current quota, please check your plan and billing details"), ErrCategoryQuota},
		{"429 insufficient balance", apiErr(429, "Insufficient balance"), ErrCategoryQuota},
		{"429 chinese arrears", apiErr(429, "账户余额不足，请充值"), ErrCategoryQuota},

		// 403 is where several providers file billing failures.
		{"403 billing not enabled", apiErr(403, "Billing not enabled for this project"), ErrCategoryQuota},
		{"403 plain", apiErr(403, "Forbidden"), ErrCategoryAuth},
		{"401", apiErr(401, "Invalid API key"), ErrCategoryAuth},

		// Unchanged behavior.
		{"500", apiErr(500, "Internal Server Error"), ErrCategoryTransient},
		{"413", apiErr(413, "Payload Too Large"), ErrCategoryContextOverflow},
		{"400 context", apiErr(400, "This model's maximum context length is 8192 tokens"), ErrCategoryContextOverflow},
		{"400 plain", apiErr(400, "Bad Request"), ErrCategoryFatal},

		// Text-only classification (no typed API error).
		{"text quota", errors.New("insufficient credit"), ErrCategoryQuota},
		{"text rate limit", errors.New("rate limit exceeded"), ErrCategoryRateLimit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyError(c.err); got != c.want {
				t.Errorf("ClassifyError = %v, want %v", got, c.want)
			}
		})
	}
}

// Retrying a spent balance just delays telling the user the one thing they can
// act on.
func TestQuotaIsNotRetryable(t *testing.T) {
	if IsRetryable(nil, apiErr(402, "Payment Required")) {
		t.Error("a 402 must not be retried — waiting does not refill a balance")
	}
	if !IsRetryable(nil, apiErr(429, "Rate limit reached")) {
		t.Error("a plain 429 must still be retried")
	}
	if IsRetryable(nil, apiErr(429, "You exceeded your current quota")) {
		t.Error("a 429 that is really a quota error must not be retried")
	}
}

func TestFriendlyAPIErrorIsActionable(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		provider string
		model    string
		wants    []string
		notWants []string
	}{
		{
			name: "429 names the cause and what to do", err: apiErr(429, "Rate limit reached"),
			provider: "openai", model: "gpt-5",
			wants:    []string{"Rate limited", "openai", "gpt-5", "again"},
			notWants: []string{"status code", "NodeRunError"},
		},
		{
			name: "402 says it ran nothing and where to pay", err: apiErr(402,
				"The free trial quota for the service has been exhausted and postpaid billing is not enabled"),
			provider: "tencent-tokenhub",
			wants: []string{"Out of quota", "tencent-tokenhub",
				"console.cloud.tencent.com/tokenhub", "/model"},
			// The whole point: it must never read as if the work happened.
			notWants: []string{"status code"},
		},
		{
			name: "402 on an unknown provider omits the URL rather than guessing",
			err:  apiErr(402, "Payment Required"), provider: "some-gateway",
			wants:    []string{"Out of quota", "some-gateway"},
			notWants: []string{"http"},
		},
		{
			name: "auth points at the config", err: apiErr(401, "Invalid API key"),
			provider: "anthropic",
			wants:    []string{"API key", "config.json"},
		},
		{
			name:  "context overflow points at /compact",
			err:   apiErr(400, "This model's maximum context length is 8192 tokens, however you requested 9000 tokens"),
			wants: []string{"/compact"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := FriendlyAPIError(c.err, c.provider, c.model)
			for _, w := range c.wants {
				if !strings.Contains(got, w) {
					t.Errorf("message missing %q:\n%s", w, got)
				}
			}
			for _, n := range c.notWants {
				if strings.Contains(strings.ToLower(got), strings.ToLower(n)) {
					t.Errorf("message leaked %q (that belongs in the log):\n%s", n, got)
				}
			}
		})
	}
}

// The raw shape the runner actually receives, from the live 402 that exposed all
// of this.
func TestWrapFriendlyOnRealNodeRunError(t *testing.T) {
	raw := errors.New("[NodeRunError] error, status code: 402, status: 402 Payment Required, " +
		"message: The free trial quota for the service has been exhausted and postpaid billing is not enabled, " +
		"so the service cannot be accessed.\nnode path: [node_1, ChatModel]")

	wrapped := WrapFriendly(raw, "tencent-tokenhub", "kimi-k2.7-code")

	var fe *FriendlyError
	if !errors.As(wrapped, &fe) {
		t.Fatalf("WrapFriendly did not produce a FriendlyError: %T", wrapped)
	}
	if fe.Category != ErrCategoryQuota {
		t.Errorf("category = %v, want quota", fe.Category)
	}
	// What a frontend prints.
	msg := wrapped.Error()
	if strings.Contains(msg, "NodeRunError") || strings.Contains(msg, "node path") {
		t.Errorf("the displayed message still carries framework plumbing:\n%s", msg)
	}
	if !strings.Contains(msg, "Out of quota") || !strings.Contains(msg, "kimi-k2.7-code") {
		t.Errorf("the displayed message is not the friendly one:\n%s", msg)
	}
	// The raw error must stay reachable for logs.
	if !strings.Contains(fe.Raw().Error(), "NodeRunError") {
		t.Error("the raw provider error was lost; logs need it")
	}
}

// Cancellation is not a failure and must keep its identity for errors.Is.
func TestWrapFriendlyPassesThroughCancellation(t *testing.T) {
	cancel := errors.New("context canceled")
	if got := WrapFriendly(cancel, "openai", "gpt-5"); got != cancel {
		t.Errorf("cancellation was wrapped: %v", got)
	}
	if WrapFriendly(nil, "", "") != nil {
		t.Error("nil was wrapped")
	}
}

func TestWrapFriendlyIsIdempotent(t *testing.T) {
	once := WrapFriendly(apiErr(429, "Rate limit reached"), "openai", "gpt-5")
	twice := WrapFriendly(once, "openai", "gpt-5")
	if once != twice {
		t.Error("double-wrapping produced a new error; the message would nest")
	}
}
