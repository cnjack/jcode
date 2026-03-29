package skills

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type loadSkillInput struct {
	Name string `json:"name"`
}

// NewLoadSkillTool creates the "load_skill" tool that loads a skill's full
// content on demand (Layer 2 injection via tool_result).
func NewLoadSkillTool(loader *Loader) tool.InvokableTool {
	return &loadSkillTool{loader: loader}
}

type loadSkillTool struct {
	loader *Loader
}

func (t *loadSkillTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	skillList := t.loader.Descriptions()
	desc := "Load a skill's full instructions by name. Available skills:\n" + skillList +
		"\nUse this when you need detailed instructions for a specific task domain."
	return &schema.ToolInfo{
		Name: "load_skill",
		Desc: desc,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"name": {
				Type:     schema.String,
				Desc:     "Name of the skill to load",
				Required: true,
			},
		}),
	}, nil
}

func (t *loadSkillTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input loadSkillInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}
	if input.Name == "" {
		return "Error: skill name is required. Available skills:\n" + t.loader.Descriptions(), nil
	}
	return t.loader.GetContent(input.Name), nil
}
