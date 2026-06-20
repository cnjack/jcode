package command

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/cnjack/jcode/internal/prompts"
	"github.com/cnjack/jcode/internal/remote"
	"github.com/cnjack/jcode/internal/tools"
	"github.com/cnjack/jcode/internal/tui"
)

// HandleSSHConnect connects to a remote machine via SSH and reconfigures the
// env. The pure connection + directory-listing logic lives in internal/remote
// so the web server can reuse it; this function keeps only the TUI glue
// (p.Send of status/dir messages).
func HandleSSHConnect(
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

	executor, err := remote.Connect(remote.SSHOptions{Host: host, User: user})
	if err != nil {
		p.Send(tui.SSHStatusMsg{Success: false, Err: err})
		return
	}

	// Temporarily set the executor so HandleSSHListDir can use it during
	// interactive path selection.
	env.SetSSH(executor, "/root")

	if path == "?" {
		remotePwd := remote.DiscoverPwd(ctx, executor, "/root")
		HandleSSHListDir(ctx, env, remotePwd, p)
		return // Do not initialize agent yet
	}

	remotePwd := "/root"
	if path != "" {
		remotePwd = path
	} else {
		remotePwd = remote.DiscoverPwd(ctx, executor, "/root")
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
		Label:   envLabel,
	})
}

// HandleSSHListDir runs `ls` on the remote host and sends the results to the
// TUI directory picker.
func HandleSSHListDir(ctx context.Context, env *tools.Env, path string, p *tea.Program) {
	dirs, err := remote.ListDirs(ctx, env.Exec, path)
	if err != nil {
		p.Send(tui.SSHDirResultsMsg{Err: err})
		return
	}
	p.Send(tui.SSHDirResultsMsg{Path: path, Items: dirs})
}
