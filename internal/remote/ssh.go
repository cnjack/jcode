// Package remote provides UI-agnostic helpers for connecting to and inspecting
// remote execution targets (currently SSH). The connection + directory-listing
// logic here was extracted from the TUI command layer so that both the TUI and
// the web server can drive the same flow without depending on bubbletea.
package remote

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cnjack/jcode/internal/tools"
)

// SSHOptions describes how to reach and authenticate with a remote host.
//
// Host may be "host", "host:port" or "user@host"; User and Port fill in the
// pieces a form provides separately. When neither Password nor KeyPath is set,
// authentication falls back to the SSH agent + the default ~/.ssh keys (the same
// behavior as tools.BuildSSHAuthMethods, used by the TUI).
type SSHOptions struct {
	Host       string
	Port       int
	User       string
	Password   string // password auth
	KeyPath    string // explicit private key file (~ is expanded)
	Passphrase string // passphrase for an encrypted private key
}

// resolveTarget splits Host into a dial address ("host:port") and a username,
// honoring an embedded "user@" prefix and an explicit Port.
func resolveTarget(opts SSHOptions) (addr, user string) {
	user = strings.TrimSpace(opts.User)
	host := strings.TrimSpace(opts.Host)
	if at := strings.SplitN(host, "@", 2); len(at) == 2 {
		if user == "" {
			user = at[0]
		}
		host = at[1]
	}
	if user == "" {
		user = "root"
	}
	// Apply an explicit port only when the host doesn't already carry one.
	if opts.Port > 0 && !strings.Contains(host, ":") {
		host = fmt.Sprintf("%s:%d", host, opts.Port)
	}
	return host, user
}

// BuildAuthMethods assembles the SSH auth methods for the given options. It
// returns an error if an explicit key cannot be read or parsed, and falls back
// to the agent + default keys when no explicit credentials are supplied.
func BuildAuthMethods(opts SSHOptions) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if opts.Password != "" {
		methods = append(methods, ssh.Password(opts.Password))
	}

	if kp := strings.TrimSpace(opts.KeyPath); kp != "" {
		key, err := os.ReadFile(expandHome(kp))
		if err != nil {
			return nil, fmt.Errorf("read private key: %w", err)
		}
		var signer ssh.Signer
		if opts.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(opts.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key %s: %w", kp, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	// Fall back to agent + default keys when nothing explicit was provided.
	if len(methods) == 0 {
		methods = tools.BuildSSHAuthMethods()
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("no SSH credentials available: provide a password or key, or load keys into the SSH agent")
	}
	return methods, nil
}

// Connect dials the remote host described by opts and returns a live executor.
func Connect(opts SSHOptions) (*tools.SSHExecutor, error) {
	addr, user := resolveTarget(opts)
	methods, err := BuildAuthMethods(opts)
	if err != nil {
		return nil, err
	}
	return tools.NewSSHExecutor(addr, user, methods)
}

// DiscoverPwd returns the remote default working directory (best effort),
// falling back to the provided default when `pwd` cannot be determined.
func DiscoverPwd(ctx context.Context, exec tools.Executor, fallback string) string {
	if fallback == "" {
		fallback = "/root"
	}
	if stdout, _, err := exec.Exec(ctx, "pwd", "", 5*time.Second); err == nil {
		if trimmed := strings.TrimSpace(stdout); trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

// ListDirs lists the sub-directories of path on the remote target using the
// executor. ".." is prepended (unless already at the filesystem root) so callers
// can render an "up" entry in a directory picker.
func ListDirs(ctx context.Context, exec tools.Executor, path string) ([]string, error) {
	cmd := fmt.Sprintf("ls -F -1 %s", tools.ShellQuote(path))
	stdout, stderr, err := exec.Exec(ctx, cmd, "", 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("ls %s failed: %v: %s", path, err, truncate(stderr, 100))
	}

	var dirs []string
	if path != "/" {
		dirs = append(dirs, "..")
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, "/") {
			dirs = append(dirs, strings.TrimSuffix(line, "/"))
		}
	}
	return dirs, nil
}

// ProjectLabel returns a stable, host-qualified identifier for a remote working
// directory. It is used as the session "project" key so remote sessions are
// grouped separately from local paths of the same name. Form:
// ssh://user@host:port/remote/path
func ProjectLabel(exec *tools.SSHExecutor, remotePwd string) string {
	return fmt.Sprintf("ssh://%s@%s%s", exec.User(), exec.Host(), normalizeRemotePath(remotePwd))
}

// normalizeRemotePath ensures the path is absolute (leading slash) for use in a
// project label.
func normalizeRemotePath(p string) string {
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}

// expandHome expands a leading ~ or ~/ to the current user's home directory.
func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
