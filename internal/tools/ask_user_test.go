package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// helper to build legacy deps with a pre-loaded response channel.
func newLegacyDeps(answer string) *AskUserDeps {
	respCh := make(chan AskUserResponse, 1)
	respCh <- AskUserResponse{Answer: answer}
	return &AskUserDeps{
		NotifyFn:   func(q string, opts []string) {},
		ResponseCh: respCh,
	}
}

// helper to build batch deps with a pre-loaded response channel.
func newBatchDeps(resp AskUserBatchResponse) *AskUserDeps {
	respCh := make(chan AskUserBatchResponse, 1)
	respCh <- resp
	return &AskUserDeps{
		NotifyFn:        func(q string, opts []string) {},
		ResponseCh:      make(chan AskUserResponse), // unused but present
		BatchNotifyFn:   func(qs []AskUserQuestion) {},
		BatchResponseCh: respCh,
	}
}

// A-01: Legacy format still works (question + string options)
func TestAskUser_LegacyCompat(t *testing.T) {
	deps := newLegacyDeps("Option B")
	tool := NewAskUserTool(deps)

	input := `{"question":"Pick one","options":[{"label":"Option A"},{"label":"Option B"}]}`
	result, err := tool.InvokableRun(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "User's answer: Option B" {
		t.Errorf("expected 'User's answer: Option B', got %q", result)
	}
}

// A-02: Single batch question returns plain text
func TestAskUser_BatchSingleQuestion(t *testing.T) {
	deps := newBatchDeps(AskUserBatchResponse{
		Answers: []AskUserAnswer{
			{QuestionHeader: "lang", Answer: "Go"},
		},
	})
	tool := NewAskUserTool(deps)

	input := `{"questions":[{"question":"What language?","header":"lang","options":[{"label":"Go","description":"Fast"},{"label":"Rust","description":"Safe"}]}]}`
	result, err := tool.InvokableRun(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "User's answer: Go" {
		t.Errorf("expected 'User's answer: Go', got %q", result)
	}
}

// A-03: Multiple batch questions returns JSON
func TestAskUser_BatchMultipleQuestions(t *testing.T) {
	deps := newBatchDeps(AskUserBatchResponse{
		Answers: []AskUserAnswer{
			{QuestionHeader: "lang", Answer: "Go"},
			{QuestionHeader: "framework", Answer: "Eino"},
		},
	})
	tool := NewAskUserTool(deps)

	input := `{"questions":[{"question":"Language?","header":"lang"},{"question":"Framework?","header":"framework"}]}`
	result, err := tool.InvokableRun(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp AskUserBatchResponse
	if jsonErr := json.Unmarshal([]byte(result), &resp); jsonErr != nil {
		t.Fatalf("expected JSON response, got parse error: %v\nraw: %s", jsonErr, result)
	}
	if len(resp.Answers) != 2 {
		t.Fatalf("expected 2 answers, got %d", len(resp.Answers))
	}
	if resp.Answers[0].Answer != "Go" {
		t.Errorf("answer[0] expected 'Go', got %q", resp.Answers[0].Answer)
	}
	if resp.Answers[1].Answer != "Eino" {
		t.Errorf("answer[1] expected 'Eino', got %q", resp.Answers[1].Answer)
	}
}

// A-06: >4 questions returns error
func TestAskUser_BatchTooManyQuestions(t *testing.T) {
	deps := newBatchDeps(AskUserBatchResponse{})
	tool := NewAskUserTool(deps)

	questions := make([]AskUserQuestion, 5)
	for i := range questions {
		questions[i] = AskUserQuestion{Question: "Q?"}
	}
	data, _ := json.Marshal(AskUserInput{Questions: questions})

	result, err := tool.InvokableRun(context.Background(), string(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Too many questions") {
		t.Errorf("expected 'Too many questions' error, got %q", result)
	}
}

// A-07: >4 options per question returns error
func TestAskUser_BatchTooManyOptions(t *testing.T) {
	deps := newBatchDeps(AskUserBatchResponse{})
	tool := NewAskUserTool(deps)

	opts := make([]AskUserOption, 5)
	for i := range opts {
		opts[i] = AskUserOption{Label: "opt"}
	}
	input := AskUserInput{
		Questions: []AskUserQuestion{
			{Question: "Pick?", Options: opts},
		},
	}
	data, _ := json.Marshal(input)

	result, err := tool.InvokableRun(context.Background(), string(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "too many options") {
		t.Errorf("expected 'too many options' error, got %q", result)
	}
}

// A-08: Header >16 chars gets truncated (not errored)
func TestAskUser_BatchHeaderTruncation(t *testing.T) {
	longHeader := "this-is-a-very-long-header-value"
	var capturedQuestions []AskUserQuestion

	respCh := make(chan AskUserBatchResponse, 1)
	respCh <- AskUserBatchResponse{
		Answers: []AskUserAnswer{
			{QuestionHeader: longHeader[:maxHeaderLen], Answer: "yes"},
		},
	}
	deps := &AskUserDeps{
		NotifyFn:   func(q string, opts []string) {},
		ResponseCh: make(chan AskUserResponse),
		BatchNotifyFn: func(qs []AskUserQuestion) {
			capturedQuestions = qs
		},
		BatchResponseCh: respCh,
	}
	tool := NewAskUserTool(deps)

	input := AskUserInput{
		Questions: []AskUserQuestion{
			{Question: "Continue?", Header: longHeader},
		},
	}
	data, _ := json.Marshal(input)

	result, err := tool.InvokableRun(context.Background(), string(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the header was truncated before being sent to the notify function
	if len(capturedQuestions) != 1 {
		t.Fatalf("expected 1 captured question, got %d", len(capturedQuestions))
	}
	if len(capturedQuestions[0].Header) > maxHeaderLen {
		t.Errorf("header not truncated: %q (len %d)", capturedQuestions[0].Header, len(capturedQuestions[0].Header))
	}
	if capturedQuestions[0].Header != longHeader[:maxHeaderLen] {
		t.Errorf("header mismatch: got %q, want %q", capturedQuestions[0].Header, longHeader[:maxHeaderLen])
	}

	if !strings.Contains(result, "yes") {
		t.Errorf("expected answer containing 'yes', got %q", result)
	}
}

// Batch fallback to legacy when no batch channels configured
func TestAskUser_BatchFallbackToLegacy(t *testing.T) {
	deps := newLegacyDeps("fallback answer")
	tool := NewAskUserTool(deps)

	input := `{"questions":[{"question":"Fallback?","header":"fb"}]}`
	result, err := tool.InvokableRun(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "User's answer: fallback answer" {
		t.Errorf("expected fallback answer, got %q", result)
	}
}

// Empty question returns error
func TestAskUser_EmptyQuestion(t *testing.T) {
	deps := newLegacyDeps("")
	tool := NewAskUserTool(deps)

	result, err := tool.InvokableRun(context.Background(), `{"question":""}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Question is required." {
		t.Errorf("expected 'Question is required.', got %q", result)
	}
}

// Batch question with too few options (1 option) returns error
func TestAskUser_BatchTooFewOptions(t *testing.T) {
	deps := newBatchDeps(AskUserBatchResponse{})
	tool := NewAskUserTool(deps)

	input := AskUserInput{
		Questions: []AskUserQuestion{
			{Question: "Pick?", Options: []AskUserOption{{Label: "only one"}}},
		},
	}
	data, _ := json.Marshal(input)

	result, err := tool.InvokableRun(context.Background(), string(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "at least 2 options") {
		t.Errorf("expected 'at least 2 options' error, got %q", result)
	}
}
