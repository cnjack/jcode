package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
)

// AskUserResponse is the user's answer to an ask_user question.
type AskUserResponse struct {
	Answer string
}

// AskUserDeps holds the dependencies for the ask_user tool.
type AskUserDeps struct {
	NotifyFn   func(question string, options []string) // sends question to TUI
	ResponseCh <-chan AskUserResponse
}

type askUserOption struct {
	Label string `json:"label"`
}

type askUserInput struct {
	Question string          `json:"question"`
	Options  []askUserOption `json:"options"`
}

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

Plan mode note: In plan mode, use this tool to clarify requirements or choose between approaches BEFORE finalizing your plan. Do NOT use this tool to ask "Is my plan ready?" or "Should I proceed?" — just present your final plan as your response.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"question": {
				Type:     schema.String,
				Desc:     "The question to ask the user",
				Required: true,
			},
			"options": {
				Type:     schema.Array,
				Desc:     `Optional list of choices. Each item: {"label": "<option text>"}. The user can always type a custom answer.`,
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
	var input askUserInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return fmt.Sprintf("Failed to parse ask_user input: %v", err), nil
	}
	if input.Question == "" {
		return "Question is required.", nil
	}

	var labels []string
	for _, opt := range input.Options {
		if opt.Label != "" {
			labels = append(labels, opt.Label)
		}
	}

	config.Logger().Printf("[ask_user] question: %q, options: %d", input.Question, len(labels))

	// Notify TUI to show the question dialog
	if t.deps.NotifyFn != nil {
		t.deps.NotifyFn(input.Question, labels)
	}

	// Block waiting for user response, respecting context cancellation.
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
