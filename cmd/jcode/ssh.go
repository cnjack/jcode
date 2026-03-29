package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/cnjack/jcode/internal/prompts"
	"github.com/cnjack/jcode/internal/tools"
	"github.com/cnjack/jcode/internal/tui"
)

// handleSSHConnect connects to a remote machine via SSH and reconfigures the env.
func handleSSHConnect(
	ctx context.Context,
	env *tools.Env,
	addr, path string,
	p *tea.Program,
	systemPrompt *string,
	ag **adk.ChatModelAgent,
	chatModel einomodel.ToolCallingChatModel,
	createAgent func() (*adk.ChatModelAgent, error),
	skillDescriptions string,
) {
	user := "root"
	host := addr
	if parts := strings.SplitN(addr, "@", 2); len(parts) == 2 {
		user = parts[0]
		host = parts[1]
	}

	executor, err := tools.NewSSHExecutor(host, user, tools.BuildSSHAuthMethods())
	if err != nil {
		p.Send(tui.SSHStatusMsg{Success: false, Err: err})
		return
	}

	// Temporarily set the executor so handleSSHListDir can use it during
	// interactive path selection.
	env.SetSSH(executor, "/root")

	if path == "?" {
		remotePwd := "/root"
		if stdout, _, execErr := executor.Exec(ctx, "pwd", "", 5*1e9); execErr == nil {
			if trimmed := strings.TrimSpace(stdout); trimmed != "" {
				remotePwd = trimmed
			}
		}
		handleSSHListDir(ctx, env, remotePwd, p)
		return // Do not initialize agent yet
	}

	remotePwd := "/root"
	if path != "" {
		remotePwd = path
	} else {
		if stdout, _, execErr := executor.Exec(ctx, "pwd", "", 5*1e9); execErr == nil {
			if trimmed := strings.TrimSpace(stdout); trimmed != "" {
				remotePwd = trimmed
			}
		}
	}

	env.SetSSH(executor, remotePwd)
	envLabel := fmt.Sprintf("%s@%s (pwd: %s)", user, host, remotePwd)
	*systemPrompt = prompts.GetSystemPrompt(executor.Platform(), remotePwd, envLabel, nil, skillDescriptions)

	newAg, agErr := createAgent()
	if agErr == nil {
		*ag = newAg
	}

	p.Send(tui.SSHStatusMsg{
		Success: true,
		Label:   fmt.Sprintf("%s@%s (pwd: %s)", user, host, remotePwd),
	})
}

// handleSSHListDir runs `ls` on the remote host and sends the results to the
// TUI directory picker.
func handleSSHListDir(ctx context.Context, env *tools.Env, path string, p *tea.Program) {
	cmd := fmt.Sprintf("ls -F -1 %s", tools.ShellQuote(path))
	stdout, stderr, err := env.Exec.Exec(ctx, cmd, "", 10*1e9)
	if err != nil {
		p.Send(tui.SSHDirResultsMsg{Err: fmt.Errorf("ls failed: %v\nstderr: %s", err, truncate(stderr, 100))})
		return
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	var dirs []string
	if path != "/" {
		dirs = append(dirs, "..")
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, "/") {
			dirs = append(dirs, line[:len(line)-1])
		}
	}

	p.Send(tui.SSHDirResultsMsg{Path: path, Items: dirs})
}

func truncate(s string, l int) string {
	if len(s) > l {
		return s[:l] + "..."
	}
	return s
}
