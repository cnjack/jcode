package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"

	appconfig "github.com/cnjack/jcode/internal/config"
)

// ACPExecutor implements Executor by delegating file and command operations
// to the ACP client via fs/* and terminal/* protocol methods.
type ACPExecutor struct {
	conn      *acp.AgentSideConnection
	sessionID acp.SessionId
	platform  string
}

// NewACPExecutor creates an executor that uses the ACP client's filesystem
// and terminal capabilities.
func NewACPExecutor(conn *acp.AgentSideConnection, sessionID acp.SessionId, platform string) *ACPExecutor {
	appconfig.Logger().Printf("[acp-exec] created ACPExecutor for session %s", sessionID)
	return &ACPExecutor{
		conn:      conn,
		sessionID: sessionID,
		platform:  platform,
	}
}

func (a *ACPExecutor) ReadFile(ctx context.Context, path string) ([]byte, error) {
	appconfig.Logger().Printf("[acp-exec] ReadFile: %s", path)
	resp, err := a.conn.ReadTextFile(ctx, acp.ReadTextFileRequest{
		SessionId: a.sessionID,
		Path:      path,
	})
	if err != nil {
		appconfig.Logger().Printf("[acp-exec] ReadFile error: %s: %v", path, err)
		return nil, fmt.Errorf("acp read %s: %w", path, err)
	}
	appconfig.Logger().Printf("[acp-exec] ReadFile ok: %s (%d bytes)", path, len(resp.Content))
	return []byte(resp.Content), nil
}

func (a *ACPExecutor) WriteFile(ctx context.Context, path string, data []byte, _ os.FileMode) error {
	appconfig.Logger().Printf("[acp-exec] WriteFile: %s (%d bytes)", path, len(data))
	_, err := a.conn.WriteTextFile(ctx, acp.WriteTextFileRequest{
		SessionId: a.sessionID,
		Path:      path,
		Content:   string(data),
	})
	if err != nil {
		appconfig.Logger().Printf("[acp-exec] WriteFile error: %s: %v", path, err)
		return fmt.Errorf("acp write %s: %w", path, err)
	}
	appconfig.Logger().Printf("[acp-exec] WriteFile ok: %s", path)
	return nil
}

func (a *ACPExecutor) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	// ACP protocol has no mkdir method. Use terminal to create directories.
	appconfig.Logger().Printf("[acp-exec] MkdirAll (via terminal): %s", path)
	cmd := fmt.Sprintf("mkdir -p %s", ShellQuote(path))
	_, _, err := a.Exec(ctx, cmd, "", 10*time.Second)
	if err != nil {
		appconfig.Logger().Printf("[acp-exec] MkdirAll error: %s: %v", path, err)
		return fmt.Errorf("acp mkdir %s: %w", path, err)
	}
	return nil
}

func (a *ACPExecutor) Stat(ctx context.Context, path string) (*FileInfo, error) {
	// ACP protocol has no stat method. Use terminal to check file existence.
	appconfig.Logger().Printf("[acp-exec] Stat (via terminal): %s", path)
	cmd := fmt.Sprintf(
		`if [ -e %s ]; then if [ -d %s ]; then echo "dir"; else echo "file"; fi; else echo "none"; fi`,
		ShellQuote(path), ShellQuote(path),
	)
	stdout, _, err := a.Exec(ctx, cmd, "", 5*time.Second)
	if err != nil {
		appconfig.Logger().Printf("[acp-exec] Stat error: %s: %v", path, err)
		return nil, err
	}
	result := strings.TrimSpace(stdout)
	switch result {
	case "dir":
		return &FileInfo{Exists: true, IsDir: true}, nil
	case "file":
		return &FileInfo{Exists: true, IsDir: false}, nil
	default:
		return &FileInfo{Exists: false}, nil
	}
}

func (a *ACPExecutor) Exec(ctx context.Context, command, workDir string, timeout time.Duration) (string, string, error) {
	appconfig.Logger().Printf("[acp-exec] Exec: cmd=%q workDir=%q timeout=%v", command, workDir, timeout)

	// Build the terminal create request. We pass the full command via "bash -c"
	// so that shell features (pipes, redirects, &&) work as expected.
	req := acp.CreateTerminalRequest{
		SessionId: a.sessionID,
		Command:   "bash",
		Args:      []string{"-c", command},
	}
	if workDir != "" {
		req.Cwd = &workDir
	}

	createResp, err := a.conn.CreateTerminal(ctx, req)
	if err != nil {
		appconfig.Logger().Printf("[acp-exec] Exec CreateTerminal error: %v", err)
		return "", "", fmt.Errorf("acp terminal create: %w", err)
	}
	termID := createResp.TerminalId
	appconfig.Logger().Printf("[acp-exec] Exec terminal created: %s", termID)

	// Ensure the terminal is always released.
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, releaseErr := a.conn.ReleaseTerminal(releaseCtx, acp.ReleaseTerminalRequest{
			SessionId:  a.sessionID,
			TerminalId: termID,
		}); releaseErr != nil {
			appconfig.Logger().Printf("[acp-exec] Exec ReleaseTerminal error: %v", releaseErr)
		}
	}()

	// Wait for the command to finish, respecting timeout.
	waitCtx, waitCancel := context.WithTimeout(ctx, timeout)
	defer waitCancel()

	exitResp, err := a.conn.WaitForTerminalExit(waitCtx, acp.WaitForTerminalExitRequest{
		SessionId:  a.sessionID,
		TerminalId: termID,
	})

	if err != nil {
		// Timeout or context cancelled — kill the terminal and retrieve partial output.
		if waitCtx.Err() != nil {
			appconfig.Logger().Printf("[acp-exec] Exec timeout/cancel, killing terminal %s", termID)
			killCtx, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer killCancel()
			_, _ = a.conn.KillTerminal(killCtx, acp.KillTerminalRequest{
				SessionId:  a.sessionID,
				TerminalId: termID,
			})

			outCtx, outCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer outCancel()
			outResp, outErr := a.conn.TerminalOutput(outCtx, acp.TerminalOutputRequest{
				SessionId:  a.sessionID,
				TerminalId: termID,
			})
			if outErr == nil {
				if errors.Is(waitCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
					appconfig.Logger().Printf("[acp-exec] Exec timed out, partial output (%d bytes)", len(outResp.Output))
					return outResp.Output, "", fmt.Errorf("command timed out after %v", timeout)
				}
				appconfig.Logger().Printf("[acp-exec] Exec cancelled, partial output (%d bytes)", len(outResp.Output))
				return outResp.Output, "", fmt.Errorf("command cancelled: %w", waitCtx.Err())
			}
		}
		appconfig.Logger().Printf("[acp-exec] Exec WaitForTerminalExit error: %v", err)
		return "", "", fmt.Errorf("acp terminal wait: %w", err)
	}

	// Retrieve the terminal output.
	outResp, err := a.conn.TerminalOutput(ctx, acp.TerminalOutputRequest{
		SessionId:  a.sessionID,
		TerminalId: termID,
	})
	if err != nil {
		appconfig.Logger().Printf("[acp-exec] Exec TerminalOutput error: %v", err)
		return "", "", fmt.Errorf("acp terminal output: %w", err)
	}

	output := outResp.Output
	appconfig.Logger().Printf("[acp-exec] Exec done: exitCode=%v output=%d bytes", exitResp.ExitCode, len(output))

	// ACP terminal output is merged stdout+stderr. We return it as stdout.
	// Map non-zero exit code to an error.
	if exitResp.ExitCode != nil && *exitResp.ExitCode != 0 {
		return output, "", fmt.Errorf("exit status %d", *exitResp.ExitCode)
	}
	return output, "", nil
}

func (a *ACPExecutor) Platform() string { return a.platform }
func (a *ACPExecutor) Label() string    { return "acp-client" }
