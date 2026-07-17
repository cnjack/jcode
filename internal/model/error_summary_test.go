package model

import (
	"errors"
	"strings"
	"testing"
)

func TestSummarizeRunError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		err             string
		wantSummary     string
		wantDetailEmpty bool
	}{
		{
			name: "nil",
			err:  "",
		},
		{
			name: "eino NodeRunError with 400 image rejection",
			err: "[NodeRunError] error, status code: 400, status: 400 Bad Request, message: messages.content.type 参数非法，取值范围 ['text']\n" +
				"node path: [node_1, ChatModel]",
			wantSummary: "API error 400: messages.content.type 参数非法，取值范围 ['text'] — this model may not support image input",
		},
		{
			name:        "401 auth error keeps status and message",
			err:         "[NodeRunError] error, status code: 401, status: 401 Unauthorized, message: invalid api key\nnode path: [ChatModel]",
			wantSummary: "API error 401: invalid api key",
		},
		{
			name:        "plain error passes through without detail dup",
			err:         "context deadline exceeded",
			wantSummary: "context deadline exceeded",
			// summary == raw ⇒ detail must be empty to avoid double display
			wantDetailEmpty: true,
		},
		{
			name:        "go-openai error without eino wrapper",
			err:         "error, status code: 429, status: 429 Too Many Requests, message: rate limit reached",
			wantSummary: "API error 429: rate limit reached",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.err != "" {
				err = errors.New(tc.err)
			}
			summary, detail := SummarizeRunError(err)
			if summary != tc.wantSummary {
				t.Errorf("summary = %q, want %q", summary, tc.wantSummary)
			}
			if tc.wantDetailEmpty && detail != "" {
				t.Errorf("detail = %q, want empty", detail)
			}
			if !tc.wantDetailEmpty && err != nil && detail == "" {
				t.Error("detail should carry the raw error")
			}
			if detail != "" && !strings.Contains(detail, strings.Split(tc.err, "\n")[0]) && tc.err != "" {
				t.Errorf("detail %q should contain raw error", detail)
			}
		})
	}
}

func TestSummarizeRunErrorTruncatesLongSummary(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 500)
	summary, _ := SummarizeRunError(errors.New("error, status code: 500, status: 500, message: " + long))
	if n := len([]rune(summary)); n > maxSummaryLen {
		t.Errorf("summary len = %d runes, want <= %d", n, maxSummaryLen)
	}
}
