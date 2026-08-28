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
		if got := SanitizeTitle(tt.in); got != tt.want {
			t.Errorf("SanitizeTitle(%q): got %q, want %q", tt.in, got, tt.want)
		}
	}

	long := strings.Repeat("很", 100)
	got := SanitizeTitle(long)
	if runes := []rune(got); len(runes) != titleMaxRunes+1 { // +1 for the ellipsis
		t.Errorf("long title not capped: %d runes", len(runes))
	}
}

func TestGenerateSessionTitleFromConversation(t *testing.T) {
	turns := []TitleMsg{
		{Role: "user", Content: "帮我修复登录超时问题"},
		{Role: "assistant", Content: "定位到连接池泄漏并修复"},
	}
	cm := &stubModel{id: "修复登录连接池超时"}
	got := GenerateSessionTitleFromConversation(context.Background(), cm, turns)
	if got != "修复登录连接池超时" {
		t.Errorf("got %q", got)
	}
}

func TestGenerateSessionTitleFromConversation_EmptyInputs(t *testing.T) {
	if got := GenerateSessionTitleFromConversation(context.Background(), &stubModel{id: "x"}, nil); got != "" {
		t.Errorf("nil turns should yield empty title, got %q", got)
	}
	// Only system/tool traffic — nothing title-worthy.
	toolOnly := []TitleMsg{{Role: "tool", Content: "secret tool output"}}
	if got := GenerateSessionTitleFromConversation(context.Background(), &stubModel{id: "x"}, toolOnly); got != "" {
		t.Errorf("tool-only turns should yield empty title, got %q", got)
	}
	if got := GenerateSessionTitleFromConversation(context.Background(), nil, []TitleMsg{{Role: "user", Content: "hi"}}); got != "" {
		t.Errorf("nil model should yield empty title, got %q", got)
	}
}

func TestTitleTurnsSelection(t *testing.T) {
	msgs := []TitleMsg{
		{Role: "system", Content: "internal system prompt"}, // dropped: role
		{Role: "tool", Content: "mcp tool result"},          // dropped: role
		{Role: "user", Content: "first user ask"},           // kept: first user
		{Role: "assistant", Content: "first answer"},        // kept: first assistant
		{Role: "user", Content: "  "},                       // dropped: empty
		{Role: "user", Content: "middle unrelated tangent"}, // dropped: neither first nor last
		{Role: "assistant", Content: "middle answer"},       // dropped
		{Role: "user", Content: "latest user ask"},          // kept: last user
		{Role: "assistant", Content: "latest answer"},       // kept: last assistant
	}
	got := TitleTurns(msgs)
	want := []TitleMsg{
		{Role: "user", Content: "first user ask"},
		{Role: "assistant", Content: "first answer"},
		{Role: "user", Content: "latest user ask"},
		{Role: "assistant", Content: "latest answer"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d turns, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("turn %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestTitleTurnsDedupAndCap(t *testing.T) {
	// Single-turn conversation: first == last, must appear once.
	one := []TitleMsg{{Role: "user", Content: "only message"}}
	if got := TitleTurns(one); len(got) != 1 || got[0].Content != "only message" {
		t.Fatalf("single turn not deduped: %+v", got)
	}

	// Long content is capped to titleTurnCap runes (+1 ellipsis).
	long := TitleTurns([]TitleMsg{{Role: "user", Content: strings.Repeat("字", titleTurnCap+50)}})
	if len(long) != 1 {
		t.Fatalf("long turn lost: %+v", long)
	}
	if runes := []rune(long[0].Content); len(runes) != titleTurnCap+1 {
		t.Errorf("turn not capped: %d runes", len(runes))
	}
}
