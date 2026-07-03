package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/automation"
	"github.com/cnjack/jcode/internal/browser"
	appconfig "github.com/cnjack/jcode/internal/config"
	"golang.org/x/crypto/ssh"
)

// MaxSubagentDepth is the maximum allowed nesting depth for subagents.
const MaxSubagentDepth = 3

// Env holds the execution context (local or remote) and is shared by all tools.
type Env struct {
	Exec        Executor
	pwd         string
	platform    string
	TodoStore   *TodoStore
	GoalStore   *GoalStore
	FileTracker *FileTracker
	OnEnvChange func(envLabel string, isLocal bool, err error)
	Depth       int // subagent nesting depth, 0 for top-level

	// AutomationStore is the process-wide automation store shared with the web
	// server and its scheduler. The automation_create tool MUST write through it
	// (not a throwaway automation.NewStore()) so a created automation is visible
	// to the server's in-memory cache, its REST API, and the scheduler. nil falls
	// back to opening a fresh store (CLI/ACP contexts with no live server).
	AutomationStore *automation.Store

	// Browser is the process-wide browser-use manager shared with the web server
	// (its extension bridge and /api/browser routes) so the agent's browser_*
	// tools and the settings UI operate the same Chrome. nil disables the tools.
	Browser *browser.Manager

	// browserSession is the lazily-opened per-task browser session (one per Env),
	// closed when the task ends. Guarded by browserMu.
	browserMu      sync.Mutex
	browserSession *browser.Session

	// origExec and origPwd remember the initial executor state so that
	// ResetToLocal can restore the correct local executor after SSH.
	origExec Executor
	origPwd  string
}

// NewEnv creates a local Env.
func NewEnv(pwd, platform string) *Env {
	exec := NewLocalExecutor(platform)
	return &Env{
		Exec:      exec,
		pwd:       pwd,
		platform:  platform,
		TodoStore: NewTodoStore(),
		GoalStore: NewGoalStore(),
		origExec:  exec,
		origPwd:   pwd,
	}
}

// SetRemote switches this Env to use a remote executor (SSH or Docker).
func (e *Env) SetRemote(executor RemoteExecutor, remotePwd string) {
	e.Exec = executor
	e.pwd = remotePwd
	e.platform = executor.Platform()
}

// SetSSH switches this Env to use a remote SSH executor. Thin wrapper kept for
// existing callers; SetRemote is the general form.
func (e *Env) SetSSH(executor *SSHExecutor, remotePwd string) {
	e.SetRemote(executor, remotePwd)
}

// CloseRemote closes the executor if it is remote (SSH/Docker), releasing the
// SSH connection or the Docker container hold (ref-count). No-op when local.
func (e *Env) CloseRemote() error {
	if re, ok := e.Exec.(RemoteExecutor); ok {
		return re.Close()
	}
	return nil
}

// ResetToLocal restores this Env to use the original local executor.
func (e *Env) ResetToLocal(pwd, platform string) {
	if e.origExec != nil {
		e.Exec = e.origExec
		e.pwd = e.origPwd
		e.platform = e.origExec.Platform()
	} else {
		e.Exec = NewLocalExecutor(platform)
		e.pwd = pwd
		e.platform = platform
	}
}

// Pwd returns the current working directory.
func (e *Env) Pwd() string { return e.pwd }

// ResolvePath resolves a file path relative to the working directory.
// Absolute paths are cleaned and returned as-is.
// Relative paths are joined with Pwd and cleaned.
// Logs a warning if the resolved relative path escapes the working directory.
func (e *Env) ResolvePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	cleaned := filepath.Clean(filepath.Join(e.pwd, path))
	pwd := filepath.Clean(e.pwd)
	if cleaned != pwd && !strings.HasPrefix(cleaned, pwd+string(filepath.Separator)) {
		appconfig.Logger().Printf("warning: resolved path %s escapes working directory %s", cleaned, pwd)
	}
	return cleaned
}

// CloneForSubagent creates a copy of this Env with the same executor and pwd
// but an isolated TodoStore, suitable for use by a subagent. GoalStore is
// intentionally left nil: the session goal belongs to the top-level agent and
// subagents are not registered with the goal tools.
func (e *Env) CloneForSubagent() *Env {
	return &Env{
		Exec:        e.Exec,
		pwd:         e.pwd,
		platform:    e.platform,
		TodoStore:   NewTodoStore(),
		FileTracker: e.FileTracker,
		Depth:       e.Depth + 1,
		Browser:     e.Browser,
	}
}

// BrowserSession returns this task's browser session, opening one on first use.
// It requires a configured, enabled Browser manager.
func (e *Env) BrowserSession(ctx context.Context) (*browser.Session, error) {
	if e.Browser == nil {
		return nil, fmt.Errorf("browser use is not available in this context")
	}
	e.browserMu.Lock()
	defer e.browserMu.Unlock()
	if e.browserSession != nil {
		return e.browserSession, nil
	}
	sess, err := e.Browser.OpenSession(ctx)
	if err != nil {
		return nil, err
	}
	e.browserSession = sess
	return sess, nil
}

// CurrentBrowserOrigin returns the origin (scheme://host) of this task's active
// browser tab, or "" when no session is open yet. The approval layer uses it to
// scope per-site permissions for browser actions whose args carry no URL (e.g.
// clicks and fills), which otherwise could never match a site rule.
func (e *Env) CurrentBrowserOrigin() string {
	e.browserMu.Lock()
	sess := e.browserSession
	e.browserMu.Unlock()
	if sess == nil {
		return ""
	}
	return sess.CurrentOrigin()
}

// CloseBrowser closes this task's browser session if one was opened.
func (e *Env) CloseBrowser() {
	e.browserMu.Lock()
	sess := e.browserSession
	e.browserSession = nil
	e.browserMu.Unlock()
	if sess != nil {
		_ = sess.Close()
	}
}

// CanNest returns whether this Env can spawn further subagents.
func (e *Env) CanNest() bool {
	return e.Depth < MaxSubagentDepth
}

// IsRemote returns true if operating over a remote executor (SSH or Docker),
// i.e. anything that is not the local executor.
func (e *Env) IsRemote() bool {
	_, ok := e.Exec.(*LocalExecutor)
	return !ok
}

// RemoteExecutor is an Executor backed by a remote target (SSH host or Docker
// container) that owns a connection/hold needing release and can produce a
// stable, scheme-qualified session key.
type RemoteExecutor interface {
	Executor
	Close() error
	ProjectLabel(pwd string) string
}

// Executor abstracts file and command operations so tools can work
// transparently on both local and remote (SSH) machines.
type Executor interface {
	// ReadFile returns the contents of the file at path.
	ReadFile(ctx context.Context, path string) ([]byte, error)

	// WriteFile writes data to the file at path, creating parent dirs as needed.
	WriteFile(ctx context.Context, path string, data []byte, perm os.FileMode) error

	// MkdirAll creates a directory tree.
	MkdirAll(ctx context.Context, path string, perm os.FileMode) error

	// Stat returns basic info about a file (exists, is dir, etc.).
	Stat(ctx context.Context, path string) (*FileInfo, error)

	// Exec runs a command and returns stdout, stderr, and any error.
	Exec(ctx context.Context, command string, workDir string, timeout time.Duration) (stdout, stderr string, err error)

	// Platform returns the OS/arch string of the target machine.
	Platform() string

	// Label returns a human-readable description (e.g. "local" or "user@host:/path").
	Label() string
}

// FileInfo is a minimal stat result.
type FileInfo struct {
	Exists bool
	IsDir  bool
}

// ---------------------------------------------------------------------------
// LocalExecutor — runs everything on the local machine
// ---------------------------------------------------------------------------

type LocalExecutor struct {
	platform string
}

func NewLocalExecutor(platform string) *LocalExecutor {
	return &LocalExecutor{platform: platform}
}

func (l *LocalExecutor) ReadFile(_ context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (l *LocalExecutor) WriteFile(_ context.Context, path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}

func (l *LocalExecutor) MkdirAll(_ context.Context, path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (l *LocalExecutor) Stat(_ context.Context, path string) (*FileInfo, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return &FileInfo{Exists: false}, nil
	}
	if err != nil {
		return nil, err
	}
	return &FileInfo{Exists: true, IsDir: info.IsDir()}, nil
}

func (l *LocalExecutor) Exec(ctx context.Context, command, workDir string, timeout time.Duration) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	if workDir != "" {
		cmd.Dir = workDir
	}

	// Prevent interactive programs (pagers, editors, prompts) from blocking
	// forever by closing stdin. This causes programs like `less` (invoked by
	// git diff, git log, etc.) to exit immediately instead of waiting for
	// keyboard input. Same approach as opencode's stdin:"ignore".
	cmd.Stdin = nil

	// Set environment variables to further disable interactive behaviors:
	// - GIT_TERMINAL_PROMPT=0: prevent git from prompting for credentials
	// - GIT_PAGER=cat: disable git's pager (less/more) entirely
	// - PAGER=cat: disable generic pager for other commands (man, etc.)
	// - GIT_EDITOR=true: prevent git from opening an editor (rebase -i, commit without -m)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		"PAGER=cat",
		"GIT_EDITOR=true",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return stdout.String(), stderr.String(), fmt.Errorf("command timed out")
	}
	return stdout.String(), stderr.String(), err
}

func (l *LocalExecutor) Platform() string { return l.platform }
func (l *LocalExecutor) Label() string    { return "local" }

// ---------------------------------------------------------------------------
// SSHExecutor — runs everything on a remote machine over SSH
// ---------------------------------------------------------------------------

type SSHExecutor struct {
	client   *ssh.Client
	host     string
	user     string
	platform string
}

// NewSSHExecutor connects to a remote host and returns an executor.
// It tries the SSH agent first, then common key paths.
func NewSSHExecutor(addr, user string, authMethods []ssh.AuthMethod) (*SSHExecutor, error) {
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	// Ensure addr includes port
	if !strings.Contains(addr, ":") {
		addr += ":22"
	}

	appconfig.Logger().Printf("[ssh] dial tcp %s@%s", user, addr)
	start := time.Now()
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		appconfig.Logger().Printf("[ssh] dial failed after %v: %v", time.Since(start), err)
		return nil, fmt.Errorf("ssh dial %s@%s: %w", user, addr, err)
	}
	appconfig.Logger().Printf("[ssh] dial success %s@%s in %v", user, addr, time.Since(start))

	// Detect remote platform
	platform := "linux/amd64"
	if out, _, err := sshExecSimple(client, "uname -sm"); err == nil {
		parts := strings.Fields(strings.TrimSpace(out))
		if len(parts) == 2 {
			os := strings.ToLower(parts[0])
			arch := strings.ToLower(parts[1])
			switch arch {
			case "x86_64":
				arch = "amd64"
			case "aarch64":
				arch = "arm64"
			}
			platform = os + "/" + arch
		}
	}

	return &SSHExecutor{
		client:   client,
		host:     addr,
		user:     user,
		platform: platform,
	}, nil
}

func (s *SSHExecutor) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *SSHExecutor) ReadFile(ctx context.Context, path string) ([]byte, error) {
	out, serr, err := s.run(ctx, fmt.Sprintf("cat %s", ShellQuote(path)), "", 30*time.Second)
	if err != nil {
		detail := strings.TrimSpace(serr)
		if detail != "" {
			return nil, fmt.Errorf("%s", detail)
		}
		return nil, err
	}
	return []byte(out), nil
}

func (s *SSHExecutor) WriteFile(ctx context.Context, path string, data []byte, perm os.FileMode) error {
	// Create parent dirs, then write via stdin
	mkdirCmd := fmt.Sprintf("mkdir -p %s", ShellQuote(filepath.Dir(path)))
	if _, _, err := s.run(ctx, mkdirCmd, "", 10*time.Second); err != nil {
		return fmt.Errorf("mkdir failed: %w", err)
	}

	// Use cat with heredoc-style write. Encode data as base64 to avoid shell escaping issues.
	encoded := base64Encode(data)
	writeCmd := fmt.Sprintf("echo %s | base64 -d > %s && chmod %o %s",
		ShellQuote(encoded), ShellQuote(path), perm, ShellQuote(path))
	if _, serr, err := s.run(ctx, writeCmd, "", 30*time.Second); err != nil {
		return fmt.Errorf("write failed: %s %w", serr, err)
	}
	return nil
}

func (s *SSHExecutor) MkdirAll(ctx context.Context, path string, _ os.FileMode) error {
	_, serr, err := s.run(ctx, fmt.Sprintf("mkdir -p %s", ShellQuote(path)), "", 10*time.Second)
	if err != nil {
		return fmt.Errorf("mkdir -p failed: %s %w", serr, err)
	}
	return nil
}

func (s *SSHExecutor) Stat(ctx context.Context, path string) (*FileInfo, error) {
	// Use test command for existence and type checks
	out, _, err := s.run(ctx, fmt.Sprintf(
		`if [ -e %s ]; then if [ -d %s ]; then echo "dir"; else echo "file"; fi; else echo "none"; fi`,
		ShellQuote(path), ShellQuote(path),
	), "", 5*time.Second)
	if err != nil {
		return nil, err
	}
	result := strings.TrimSpace(out)
	switch result {
	case "dir":
		return &FileInfo{Exists: true, IsDir: true}, nil
	case "file":
		return &FileInfo{Exists: true, IsDir: false}, nil
	default:
		return &FileInfo{Exists: false}, nil
	}
}

func (s *SSHExecutor) Exec(ctx context.Context, command, workDir string, timeout time.Duration) (string, string, error) {
	// Prepend environment variables to disable pagers/editors/prompts on remote.
	envPrefix := "export GIT_TERMINAL_PROMPT=0 GIT_PAGER=cat PAGER=cat GIT_EDITOR=true; "
	fullCmd := envPrefix + command
	if workDir != "" {
		fullCmd = fmt.Sprintf("cd %s && %s", ShellQuote(workDir), envPrefix+command)
	}
	return s.run(ctx, fullCmd, "", timeout)
}

func (s *SSHExecutor) Platform() string { return s.platform }

// User returns the SSH username.
func (s *SSHExecutor) User() string { return s.user }

// Host returns the dialed host (includes the port, e.g. "1.2.3.4:22").
func (s *SSHExecutor) Host() string { return s.host }

func (s *SSHExecutor) Label() string {
	return fmt.Sprintf("%s@%s", s.user, s.host)
}

// ProjectLabel returns a stable, host-qualified session key of the form
// ssh://user@host:port/remote/path.
func (s *SSHExecutor) ProjectLabel(pwd string) string {
	return fmt.Sprintf("ssh://%s@%s%s", s.user, s.host, normalizeAbs(pwd))
}

// run executes a command over SSH, respecting both the context and timeout.
func (s *SSHExecutor) run(ctx context.Context, command, _ string, timeout time.Duration) (string, string, error) {
	session, err := s.client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("ssh session: %w", err)
	}
	defer func() { _ = session.Close() }()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// Run with timeout via goroutine
	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case err := <-done:
		return stdout.String(), stderr.String(), err
	case <-time.After(timeout):
		_ = session.Signal(ssh.SIGTERM)
		return stdout.String(), stderr.String(), fmt.Errorf("command timed out after %v", timeout)
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		return stdout.String(), stderr.String(), fmt.Errorf("command cancelled: %w", ctx.Err())
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sshExecSimple(client *ssh.Client, command string) (string, string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", err
	}
	defer func() { _ = session.Close() }()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	err = session.Run(command)
	return stdout.String(), stderr.String(), err
}

func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func base64Encode(data []byte) string {
	var buf bytes.Buffer
	encoder := base64.NewEncoder(base64.StdEncoding, &buf)
	_, _ = encoder.Write(data)
	_ = encoder.Close()
	return buf.String()
}
