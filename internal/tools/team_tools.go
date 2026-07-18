package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	orderedmap "github.com/wk8/go-ordered-map/v2"

	"github.com/cnjack/jcode/internal/team"
)

// --- team_create ---

type teamCreateInput struct {
	TeamName    string `json:"team_name"`
	Description string `json:"description"`
}

type teamCreateTool struct {
	manager *team.Manager
	info    *schema.ToolInfo
}

func NewTeamCreateTool(manager *team.Manager) tool.InvokableTool {
	return &teamCreateTool{
		manager: manager,
		info: &schema.ToolInfo{
			Name: "team_create",
			Desc: "Create a new agent team. You become the team lead. " +
				"After creating a team, use team_spawn to add teammates.",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"team_name": {
					Type: schema.String, Desc: "Name for the team", Required: true,
				},
				"description": {
					Type: schema.String, Desc: "Brief description of the team's purpose", Required: false,
				},
			}),
		},
	}
}

func (t *teamCreateTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *teamCreateTool) InvokableRun(ctx context.Context, argsJSON string, _ ...tool.Option) (string, error) {
	var input teamCreateInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if input.TeamName == "" {
		return "", fmt.Errorf("team_name is required")
	}

	if err := t.manager.CreateTeam(input.TeamName, input.Description); err != nil {
		return "", err
	}
	return fmt.Sprintf("Team %q created. Lead agent: %s\nUse team_spawn to add teammates.",
		input.TeamName, t.manager.LeadAgentID()), nil
}

// --- team_spawn ---

type teamSpawnInput struct {
	Name      string `json:"name"`
	Prompt    string `json:"prompt"`
	AgentType string `json:"agent_type"`
	Model     string `json:"model"`
	Cwd       string `json:"cwd"`
	Mode      string `json:"mode"`
}

type teamSpawnTool struct {
	manager *team.Manager
	info    *schema.ToolInfo
}

func NewTeamSpawnTool(manager *team.Manager) tool.InvokableTool {
	return &teamSpawnTool{
		manager: manager,
		info: &schema.ToolInfo{
			Name: "team_spawn",
			Desc: "Start a new teammate agent that runs in parallel. " +
				"The teammate gets its own isolated environment and can communicate via messages. " +
				"Use team_send_message to coordinate with teammates. A general/coder teammate in auto mode " +
				"requests a one-time delegated-write grant at this parent call.",
			ParamsOneOf: teamSpawnParams(),
		},
	}
}

func teamSpawnParams() *schema.ParamsOneOf {
	properties := orderedmap.New[string, *jsonschema.Schema]()
	properties.Set("name", &jsonschema.Schema{
		Type: "string", Description: "Unique name for the teammate (for example 'researcher' or 'coder').",
	})
	properties.Set("prompt", &jsonschema.Schema{
		Type: "string", Description: "Detailed task instructions for the teammate.",
	})
	properties.Set("agent_type", &jsonschema.Schema{
		Type: "string",
		Description: "Agent type. 'explore' is always read-only; 'general' and 'coder' can receive write tools " +
			"according to mode. Defaults to 'general'.",
		Enum:    []any{team.AgentTypeExplore, team.AgentTypeGeneral, team.AgentTypeCoder},
		Default: team.AgentTypeGeneral,
	})
	properties.Set("model", &jsonschema.Schema{
		Type: "string", Description: "Override model in 'provider/model' format (optional).",
	})
	properties.Set("cwd", &jsonschema.Schema{
		Type: "string", Description: "Working directory (defaults to current).",
	})
	properties.Set("mode", &jsonschema.Schema{
		Type: "string",
		Description: "Permission mode. 'normal' (default) shares the leader's approval gate for each mutating child call; " +
			"'plan' is strictly read-only; 'auto' skips child approvals, so general/coder requests a one-time " +
			"delegated-write grant at this team_spawn call.",
		Enum:    []any{team.PermissionNormal, team.PermissionPlan, team.PermissionAuto},
		Default: team.PermissionNormal,
	})
	return schema.NewParamsOneOfByJSONSchema(&jsonschema.Schema{
		Type:                 "object",
		Properties:           properties,
		Required:             []string{"name", "prompt"},
		AdditionalProperties: jsonschema.FalseSchema,
	})
}

func (t *teamSpawnTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *teamSpawnTool) InvokableRun(ctx context.Context, argsJSON string, _ ...tool.Option) (string, error) {
	var input teamSpawnInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if input.Name == "" || input.Prompt == "" {
		return "", fmt.Errorf("name and prompt are required")
	}

	if !t.manager.HasTeam() {
		return "", fmt.Errorf("no active team. Use team_create first")
	}
	agentType, err := team.NormalizeAgentType(input.AgentType)
	if err != nil {
		return "", err
	}
	permission, err := team.NormalizePermission(input.Mode)
	if err != nil {
		return "", err
	}

	agentID, err := t.manager.SpawnTeammate(ctx, team.SpawnConfig{
		Name:       input.Name,
		Prompt:     input.Prompt,
		AgentType:  agentType,
		Model:      input.Model,
		Cwd:        input.Cwd,
		Permission: permission,
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Teammate %q spawned (ID: %s, type: %s, mode: %s). It is now running its task in parallel.",
		input.Name, agentID, agentType, permission), nil
}

// --- team_send_message ---

type teamSendMsgInput struct {
	To      string `json:"to"`
	Message string `json:"message"`
	Summary string `json:"summary"`
}

type teamSendMsgTool struct {
	manager *team.Manager
	info    *schema.ToolInfo
}

func NewTeamSendMessageTool(manager *team.Manager) tool.InvokableTool {
	return &teamSendMsgTool{
		manager: manager,
		info: &schema.ToolInfo{
			Name: "team_send_message",
			Desc: "Send a message to a teammate or broadcast to all. " +
				"Use '*' as recipient to broadcast. Messages are delivered to the teammate's mailbox.",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"to": {
					Type: schema.String, Desc: "Recipient: teammate name, or '*' for broadcast", Required: true,
				},
				"message": {
					Type: schema.String, Desc: "Message content", Required: true,
				},
				"summary": {
					Type: schema.String, Desc: "5-10 word summary for UI preview", Required: false,
				},
			}),
		},
	}
}

func (t *teamSendMsgTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *teamSendMsgTool) InvokableRun(ctx context.Context, argsJSON string, _ ...tool.Option) (string, error) {
	var input teamSendMsgInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if input.To == "" || input.Message == "" {
		return "", fmt.Errorf("to and message are required")
	}

	from := team.GetAgentName(ctx)

	if input.To == "*" {
		count, err := t.manager.BroadcastMessage(from, input.Message, input.Summary)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Message broadcast to %d teammate(s)", count), nil
	}

	if err := t.manager.SendMessage(input.To, from, input.Message, input.Summary); err != nil {
		return "", err
	}
	return fmt.Sprintf("Message sent to @%s", input.To), nil
}

// --- team_list ---

type teamListTool struct {
	manager *team.Manager
	info    *schema.ToolInfo
}

func NewTeamListTool(manager *team.Manager) tool.InvokableTool {
	return &teamListTool{
		manager: manager,
		info: &schema.ToolInfo{
			Name:        "team_list",
			Desc:        "List all teammates and their current status.",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
		},
	}
}

func (t *teamListTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *teamListTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	if !t.manager.HasTeam() {
		return "No active team.", nil
	}

	teammates := t.manager.ListTeammates()
	if len(teammates) == 0 {
		return fmt.Sprintf("Team %q active, no teammates spawned yet.", t.manager.TeamName()), nil
	}

	var result string
	result = fmt.Sprintf("Team: %s (%d teammates)\n\n", t.manager.TeamName(), len(teammates))
	for _, t := range teammates {
		progress := ""
		if t.Progress != nil {
			progress = fmt.Sprintf(" [%d tools, last: %s]", t.Progress.ToolCallCount, t.Progress.LastToolName)
		}
		result += fmt.Sprintf("  @%-15s  status=%-10s  type=%-10s%s\n",
			t.Identity.AgentName, t.Status, t.AgentType, progress)
	}
	return result, nil
}

// --- team_delete ---

type teamDeleteTool struct {
	manager *team.Manager
	info    *schema.ToolInfo
}

func NewTeamDeleteTool(manager *team.Manager) tool.InvokableTool {
	return &teamDeleteTool{
		manager: manager,
		info: &schema.ToolInfo{
			Name: "team_delete",
			Desc: "Dissolve the current team, shutting down all teammates.",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"force": {
					Type: schema.Boolean, Desc: "Force kill without graceful shutdown", Required: false,
				},
			}),
		},
	}
}

func (t *teamDeleteTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *teamDeleteTool) InvokableRun(ctx context.Context, argsJSON string, _ ...tool.Option) (string, error) {
	if !t.manager.HasTeam() {
		return "No active team to dissolve.", nil
	}
	teamName := t.manager.TeamName()
	if err := t.manager.DissolveTeam(ctx); err != nil {
		return "", err
	}
	return fmt.Sprintf("Team %q dissolved. All teammates shut down.", teamName), nil
}
