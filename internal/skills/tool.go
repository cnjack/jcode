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
	return &schema.ToolInfo{
		Name: "load_skill",
		Desc: "Load the full instructions for one available skill by its exact name. " +
			"Use this when a task matches a specialized skill domain and you need its detailed workflow.",
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
