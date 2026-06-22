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
		Desc: "Switch the execution environment between the local machine, SSH servers, and Docker containers.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"target": {
				Type:     schema.String,
				Desc:     "The destination environment. Must be 'local', an exact SSH alias name, or an exact Docker alias name.",
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

	// Remember the outgoing executor so we can release its hold (SSH connection
	// or Docker container ref-count) once the switch succeeds.
	prev := s.env.Exec

	if input.Target == "local" {
		s.env.ResetToLocal("", "")
		closeIfRemote(prev)
		if s.env.OnEnvChange != nil {
			s.env.OnEnvChange("local", true, nil)
		}
		return "Switched to 'local' execution context.", nil
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	// Docker alias?
	for i := range cfg.DockerAliases {
		if cfg.DockerAliases[i].Name != input.Target {
			continue
		}
		da := cfg.DockerAliases[i]
		dexec, derr := AcquireDockerContainer(ctx, da.Container)
		if derr != nil {
			if s.env.OnEnvChange != nil {
				s.env.OnEnvChange("", false, fmt.Errorf("failed to connect to docker '%s': %v", input.Target, derr))
			}
			return "", fmt.Errorf("failed to connect to docker '%s': %v", input.Target, derr)
		}
		path := da.Path
		if path == "" {
			path = "/"
		}
		s.env.SetRemote(dexec, path)
		closeIfRemote(prev)
		label := dexec.Label()
		if s.env.OnEnvChange != nil {
			s.env.OnEnvChange(label, false, nil)
		}
		return fmt.Sprintf("Switched to '%s' (%s: %s).", input.Target, label, path), nil
	}

	// SSH alias?
	var match *config.SSHAlias
	for i := range cfg.SSHAliases {
		if cfg.SSHAliases[i].Name == input.Target {
			match = &cfg.SSHAliases[i]
			break
		}
	}
	if match == nil {
		return "", fmt.Errorf("environment '%s' not found locally. Switch to 'local' or a valid SSH/Docker alias", input.Target)
	}

	authMethods := BuildSSHAuthMethods()
	user := ""
	addr := match.Addr
	if idx := strings.Index(addr, "@"); idx > 0 {
		user = addr[:idx]
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
	closeIfRemote(prev)
	label := sshExec.Label()

	if s.env.OnEnvChange != nil {
		s.env.OnEnvChange(label, false, nil)
	}

	return fmt.Sprintf("Switched to '%s' (%s: %s).", input.Target, label, match.Path), nil
}

// closeIfRemote releases a remote executor's underlying hold (SSH connection or
// Docker container ref-count). No-op for the local executor or nil.
func closeIfRemote(e Executor) {
	if re, ok := e.(RemoteExecutor); ok {
		_ = re.Close()
	}
}
