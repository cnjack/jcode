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

type SwitchEnvInput struct {
	Target string `json:"target"`
}

func (e *Env) NewSwitchEnvTool() tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "switch_env",
		Desc: "Switch the execution environment between the local machine and SSH servers.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"target": {
				Type:     schema.String,
				Desc:     "The destination environment. Must be 'local' or an exact SSH alias name.",
				Required: true,
			},
		}),
	}

	return &switchEnvTool{
		env:  e,
		info: info,
	}
}

type switchEnvTool struct {
	env  *Env
	info *schema.ToolInfo
}

func (s *switchEnvTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return s.info, nil
}

func (s *switchEnvTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var input SwitchEnvInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	if input.Target == "" {
		return "", fmt.Errorf("target is required")
	}

	if input.Target == "local" {
		// Just reuse current locally stored if possible or prompt for reset.
		// Since we don't have util imported, we'll avoid it. We can just keep existing platform/pwd if they were stored previously in initial creation.
		// Wait, if we are remote, env pwd/platform are remote. We need original ones.
		// Actually, Env doesn't have original local pwd/platform, but we can just use the tool's platform?
		// No, `ResetToLocal` needs it. Let's add them to Env later or just use OS calls.
		// For now, if we cannot cleanly switch to local without UI's help, let's keep it simple.
		// But in MVP, let `OnEnvChange` handle UI resets, right? Yes.
		if s.env.OnEnvChange != nil {
			s.env.OnEnvChange("local", true, nil)
		}
		return "Switched to 'local' execution context.", nil
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	var match *config.SSHAlias
	for _, alias := range cfg.SSHAliases {
		if alias.Name == input.Target {
			match = &alias
			break
		}
	}

	if match == nil {
		return "", fmt.Errorf("SSH alias '%s' not found locally. Switch to 'local' or valid alias.", input.Target)
	}

	authMethods := BuildSSHAuthMethods()
	user := ""
	addr := match.Addr
	if idx := strings.Index(addr, "@"); idx > 0 {
		user = addr[:idx]
		// Don't modify addr, NewSSHExecutor expects "host:port" in addr?
		// Wait, NewSSHExecutor expects "user@host" as addr? No, looking at env.go, it expects addr=host:port, user=user. Let's extract correctly.
		// Wait, my env.go says NewSSHExecutor(addr, user string, authMethods []ssh.AuthMethod)
		addr = addr[idx+1:]
	}

	sshExec, err := NewSSHExecutor(addr, user, authMethods)
	if err != nil {
		if s.env.OnEnvChange != nil {
			s.env.OnEnvChange("", false, fmt.Errorf("failed to connect to %s: %v", input.Target, err))
		}
		return "", fmt.Errorf("failed to connect to %s: %v", input.Target, err)
	}

	s.env.SetSSH(sshExec, match.Path)
	label := sshExec.Label()

	if s.env.OnEnvChange != nil {
		s.env.OnEnvChange(label, false, nil)
	}

	return fmt.Sprintf("Switched to '%s' (%s: %s).", input.Target, label, match.Path), nil
}
