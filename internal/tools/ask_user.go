package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
)

// AskUserResponse is the user's answer to an ask_user question.
type AskUserResponse struct {
	Answer string
}

// AskUserOption is an enhanced option with a label and optional description.
type AskUserOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// AskUserQuestion is a single question in a batch request.
type AskUserQuestion struct {
	Question    string          `json:"question"`
	Header      string          `json:"header,omitempty"`
	Options     []AskUserOption `json:"options,omitempty"`
	MultiSelect bool            `json:"multi_select,omitempty"`
}

// AskUserAnswer is a single answer in a batch response.
type AskUserAnswer struct {
	QuestionHeader string   `json:"question_header"`
	Answer         string   `json:"answer"`
	Selected       []string `json:"selected,omitempty"`
}

// AskUserBatchResponse is the structured response for batch questions.
type AskUserBatchResponse struct {
	Answers []AskUserAnswer `json:"answers"`
}

// AskUserDeps holds the dependencies for the ask_user tool.
type AskUserDeps struct {
	NotifyFn        func(question string, options []string) // sends question to TUI
	ResponseCh      <-chan AskUserResponse                  // receives user answer
	BatchNotifyFn   func(questions []AskUserQuestion)       // sends batch questions to TUI
	BatchResponseCh <-chan AskUserBatchResponse             // receives batch answers
}

type askUserOption struct {
	Label string `json:"label"`
}

// AskUserInput supports both batch and legacy input formats.
type AskUserInput struct {
	Questions []AskUserQuestion `json:"questions,omitempty"`
	Question  string            `json:"question,omitempty"`
	Options   []askUserOption   `json:"options,omitempty"`
}

const (
	maxQuestions   = 4
	maxOptionsPerQ = 4
	minOptionsPerQ = 2
	maxHeaderLen   = 16
)

// NewAskUserTool creates the ask_user tool that allows the agent to ask the user
// a question during execution, optionally with selectable choices.
func NewAskUserTool(deps *AskUserDeps) tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "ask_user",
		Desc: `Ask the user a question during execution. This allows you to:
1. Gather user preferences or requirements
2. Clarify ambiguous instructions
3. Get decisions on implementation choices as you work
4. Offer choices to the user about what direction to take

Usage notes:
- The user will always be able to type a custom answer in addition to selecting an option
- If you recommend a specific option, make that the first option in the list and add "(Recommended)" at the end of the label
- Options are optional. If you just need a free-form answer, omit the options field
- Use the "questions" array to ask multiple questions in a single batch (up to 4)
- Each question can have its own options, header, and multi_select setting
- For a single question, use either the "questions" array or the legacy "question" field

Plan mode note: In plan mode, use this tool to clarify requirements or choose between approaches BEFORE finalizing your plan. Do NOT use this tool to ask "Is my plan ready?" or "Should I proceed?" — just present your final plan as your response.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"questions": {
				Type: schema.Array,
				Desc: `Batch questions. Array of 1-4 question objects, each with:
- "question" (string, required): the question text
- "header" (string, optional): short header ≤16 chars
- "options" (array, optional): 2-4 option objects with "label" and optional "description"
- "multi_select" (bool, optional): allow selecting multiple options`,
				Required: false,
			},
			"question": {
				Type:     schema.String,
				Desc:     "The question to ask the user (legacy compatibility)",
				Required: false,
			},
			"options": {
				Type:     schema.Array,
				Desc:     `Optional list of choices (legacy compatibility). Each item: {"label": "<option text>"}. The user can always type a custom answer.`,
				Required: false,
			},
		}),
	}
	return &askUserTool{deps: deps, info: info}
}

type askUserTool struct {
	deps *AskUserDeps
	info *schema.ToolInfo
}

func (t *askUserTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *askUserTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input AskUserInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return fmt.Sprintf("Failed to parse ask_user input: %v", err), nil
	}

	// Route: batch questions or single question
	if len(input.Questions) > 0 {
		return t.runBatch(ctx, input.Questions)
	}
	return t.runSingle(ctx, input.Question, input.Options)
}

// runSingle handles the legacy single-question path.
func (t *askUserTool) runSingle(ctx context.Context, question string, options []askUserOption) (string, error) {
	if question == "" {
		return "Question is required.", nil
	}

	var labels []string
	for _, opt := range options {
		if opt.Label != "" {
			labels = append(labels, opt.Label)
		}
	}

	config.Logger().Printf("[ask_user] question: %q, options: %d", question, len(labels))

	if t.deps.NotifyFn != nil {
		t.deps.NotifyFn(question, labels)
	}

	var resp AskUserResponse
	select {
	case resp = <-t.deps.ResponseCh:
	case <-ctx.Done():
		return "ask_user cancelled: context expired", nil
	}

	config.Logger().Printf("[ask_user] answer: %q", resp.Answer)

	if resp.Answer == "" {
		return "The user did not provide an answer.", nil
	}
	return fmt.Sprintf("User's answer: %s", resp.Answer), nil
}

// runBatch handles the batch questions path.
func (t *askUserTool) runBatch(ctx context.Context, questions []AskUserQuestion) (string, error) {
	// Validate question count
	if len(questions) == 0 {
		return "At least 1 question is required.", nil
	}
	if len(questions) > maxQuestions {
		return fmt.Sprintf("Too many questions: got %d, maximum is %d.", len(questions), maxQuestions), nil
	}

	// Validate and sanitize each question
	for i := range questions {
		q := &questions[i]
		if strings.TrimSpace(q.Question) == "" {
			return fmt.Sprintf("Question %d has empty text.", i+1), nil
		}
		// Truncate header to maxHeaderLen
		if len(q.Header) > maxHeaderLen {
			q.Header = q.Header[:maxHeaderLen]
		}
		// Validate options count
		if len(q.Options) > maxOptionsPerQ {
			return fmt.Sprintf("Question %d has too many options: got %d, maximum is %d.", i+1, len(q.Options), maxOptionsPerQ), nil
		}
		if len(q.Options) > 0 && len(q.Options) < minOptionsPerQ {
			return fmt.Sprintf("Question %d needs at least %d options when options are provided, got %d.", i+1, minOptionsPerQ, len(q.Options)), nil
		}
	}

	config.Logger().Printf("[ask_user] batch: %d questions", len(questions))

	// Use batch channels if available, otherwise fall back to single-question mode
	if t.deps.BatchNotifyFn != nil && t.deps.BatchResponseCh != nil {
		return t.executeBatch(ctx, questions)
	}
	return t.executeBatchViaLegacy(ctx, questions)
}

// executeBatch sends batch questions through batch channels.
func (t *askUserTool) executeBatch(ctx context.Context, questions []AskUserQuestion) (string, error) {
	t.deps.BatchNotifyFn(questions)

	var resp AskUserBatchResponse
	select {
	case resp = <-t.deps.BatchResponseCh:
	case <-ctx.Done():
		return "ask_user cancelled: context expired", nil
	}

	return formatBatchResponse(questions, resp)
}

// executeBatchViaLegacy falls back to legacy channels for batch requests (first question only).
func (t *askUserTool) executeBatchViaLegacy(ctx context.Context, questions []AskUserQuestion) (string, error) {
	firstQ := questions[0]
	var labels []string
	for _, opt := range firstQ.Options {
		if opt.Label != "" {
			labels = append(labels, opt.Label)
		}
	}

	config.Logger().Printf("[ask_user] batch→legacy fallback, asking first question only: %q", firstQ.Question)

	if t.deps.NotifyFn != nil {
		t.deps.NotifyFn(firstQ.Question, labels)
	}

	var resp AskUserResponse
	select {
	case resp = <-t.deps.ResponseCh:
	case <-ctx.Done():
		return "ask_user cancelled: context expired", nil
	}

	if resp.Answer == "" {
		return "The user did not provide an answer.", nil
	}

	// Build a batch response from legacy answer
	batchResp := AskUserBatchResponse{
		Answers: []AskUserAnswer{
			{
				QuestionHeader: firstQ.Header,
				Answer:         resp.Answer,
			},
		},
	}
	return formatBatchResponse(questions[:1], batchResp)
}

// formatBatchResponse formats the batch response. Single question returns plain text;
// multiple questions return a JSON object.
func formatBatchResponse(questions []AskUserQuestion, resp AskUserBatchResponse) (string, error) {
	if len(resp.Answers) == 0 {
		return "The user did not provide any answers.", nil
	}

	// Single question → plain text answer
	if len(questions) == 1 {
		ans := resp.Answers[0]
		if ans.Answer == "" && len(ans.Selected) == 0 {
			return "The user did not provide an answer.", nil
		}
		if ans.Answer != "" {
			return fmt.Sprintf("User's answer: %s", ans.Answer), nil
		}
		return fmt.Sprintf("User's answer: %s", strings.Join(ans.Selected, ", ")), nil
	}

	// Multiple questions → JSON
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Sprintf("Failed to format response: %v", err), nil
	}
	return string(data), nil
}
