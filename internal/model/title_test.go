package model

import (
	"context"
	"strings"
	"testing"
)

func TestGenerateSessionTitle(t *testing.T) {
	cm := &stubModel{id: "修复登录超时问题"}
	got := GenerateSessionTitle(context.Background(), cm, "登录接口在高并发下超时,帮我查一下")
	if got != "修复登录超时问题" {
		t.Errorf("got %q", got)
	}
}

func TestGenerateSessionTitle_EmptyInputs(t *testing.T) {
	if got := GenerateSessionTitle(context.Background(), &stubModel{id: "x"}, "   "); got != "" {
		t.Errorf("blank message should yield empty title, got %q", got)
	}
	if got := GenerateSessionTitle(context.Background(), nil, "hello"); got != "" {
		t.Errorf("nil model should yield empty title, got %q", got)
	}
}

func TestSanitizeTitle(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Fix login timeout", "Fix login timeout"},
		{"\"Quoted title\"", "Quoted title"},
		{"「中文书名号」", "中文书名号"},
		{"# Heading style", "Heading style"},
		{"\n\nfirst real line\nsecond line", "first real line"},
		{"", ""},
		{"   ", ""},
	}
	for _, tt := range tests {
		if got := sanitizeTitle(tt.in); got != tt.want {
			t.Errorf("sanitizeTitle(%q): got %q, want %q", tt.in, got, tt.want)
		}
	}

	long := strings.Repeat("很", 100)
	got := sanitizeTitle(long)
	if runes := []rune(got); len(runes) != titleMaxRunes+1 { // +1 for the ellipsis
		t.Errorf("long title not capped: %d runes", len(runes))
	}
}
