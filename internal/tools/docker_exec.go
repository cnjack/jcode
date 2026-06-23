package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	appconfig "github.com/cnjack/jcode/internal/config"
)

// ---------------------------------------------------------------------------
// Shared daemon client — one per process. FromEnv honors DOCKER_HOST / TLS, so
// a remote daemon (tcp:// or ssh://) works transparently. The client is never
// closed by an executor: many executors share it.
// ---------------------------------------------------------------------------

var (
	dockerOnce sync.Once
	dockerCli  *client.Client
	dockerErr  error
)

// DockerClient returns the process-wide Docker client, creating it on first use.
func DockerClient() (*client.Client, error) {
	dockerOnce.Do(func() {
		dockerCli, dockerErr = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	})
	if dockerErr != nil {
		return nil, dockerErr
	}
	return dockerCli, nil
}

// ---------------------------------------------------------------------------
// Lifecycle ref-counting — only for containers WE started (A1). A container the
// user already had running is never counted and never stopped by us. The last
// release of a container we started stops it.
// ---------------------------------------------------------------------------

var (
	dockerRefMu sync.Mutex
	dockerRefs  = map[string]int{}
)

func dockerAcquireRef(id string) {
	dockerRefMu.Lock()
	dockerRefs[id]++
	dockerRefMu.Unlock()
}

// dockerReleaseRef decrements the ref-count for a container we started and, when
// it reaches zero, stops the container. stop=false skips the stop (used when the
// container already exited on its own).
func dockerReleaseRef(id string, stop bool) {
	dockerRefMu.Lock()
	n := dockerRefs[id] - 1
	if n <= 0 {
		delete(dockerRefs, id)
	} else {
		dockerRefs[id] = n
	}
	dockerRefMu.Unlock()
	if n <= 0 && stop {
		if cli, err := DockerClient(); err == nil {
			_ = cli.ContainerStop(context.Background(), id, container.StopOptions{})
			appconfig.Logger().Printf("[docker] stopped container %s (last reference released)", shortID(id))
		}
	}
}

// ---------------------------------------------------------------------------
// DockerExecutor — runs everything inside a container via `docker exec`. File
// operations are shelled (cat / base64 / test), identical in spirit to
// SSHExecutor, so symlinks and text/binary content behave the same way.
// ---------------------------------------------------------------------------

type DockerExecutor struct {
	cli         *client.Client
	containerID string
	name        string // display name without the leading slash
	platform    string
	startedByUs bool
}

// AcquireDockerContainer binds to a container by id or name. A1 semantics: a
// stopped container is started as-is (no CMD override) and polled until it
// reports Running; a container whose main process exits immediately yields an
// error carrying its last logs. Containers we start are ref-counted and stopped
// on the last Close.
func AcquireDockerContainer(ctx context.Context, containerRef string) (*DockerExecutor, error) {
	cli, err := DockerClient()
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	info, err := cli.ContainerInspect(ctx, containerRef)
	if err != nil {
		return nil, fmt.Errorf("inspect container %q: %w", containerRef, err)
	}
	name := strings.TrimPrefix(info.Name, "/")
	running := info.State != nil && info.State.Running

	// A container is "ours" while we hold any ref on it (we started it earlier).
	dockerRefMu.Lock()
	alreadyOurs := dockerRefs[info.ID] > 0
	dockerRefMu.Unlock()

	startedByUs := false
	switch {
	case !running:
		appconfig.Logger().Printf("[docker] starting stopped container %s (%s)", shortID(info.ID), name)
		if err := cli.ContainerStart(ctx, info.ID, container.StartOptions{}); err != nil {
			return nil, fmt.Errorf("start container %q: %w", name, err)
		}
		dockerAcquireRef(info.ID)
		startedByUs = true
		if err := waitRunning(ctx, cli, info.ID); err != nil {
			// The container's main process exited right after start: it is not a
			// long-running container. Roll back our ref (it already stopped) and
			// surface the tail of its logs so the user understands why.
			dockerReleaseRef(info.ID, false)
			logs := tailDockerLogs(ctx, cli, info.ID, 20)
			if logs != "" {
				return nil, fmt.Errorf("%w\n--- container logs (last lines) ---\n%s", err, logs)
			}
			return nil, err
		}
	case alreadyOurs:
		// Running because WE started it for another engine/session: take an
		// additional ref so the container isn't stopped until every user releases.
		dockerAcquireRef(info.ID)
		startedByUs = true
	default:
		// Running independently of us (the user's own container): never counted,
		// never stopped by us.
	}

	platform := detectDockerPlatform(ctx, cli, info.ID)
	appconfig.Logger().Printf("[docker] bound container %s (%s) platform=%s startedByUs=%v", shortID(info.ID), name, platform, startedByUs)
	return &DockerExecutor{
		cli:         cli,
		containerID: info.ID,
		name:        name,
		platform:    platform,
		startedByUs: startedByUs,
	}, nil
}

// waitRunning polls until the container has been Running for the full settle
// window (catching one-shot containers that exit within milliseconds), or
// returns an error if it exits first.
func waitRunning(ctx context.Context, cli *client.Client, id string) error {
	deadline := time.Now().Add(3 * time.Second)
	for {
		info, err := cli.ContainerInspect(ctx, id)
		if err != nil {
			return fmt.Errorf("inspect after start: %w", err)
		}
		if info.State != nil && !info.State.Running {
			return fmt.Errorf("container %q exited immediately after start (exit code %d): its main process is not long-running, so it cannot host a workspace", strings.TrimPrefix(info.Name, "/"), info.State.ExitCode)
		}
		if info.State != nil && info.State.Running && time.Now().After(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (d *DockerExecutor) Close() error {
	if d.startedByUs {
		dockerReleaseRef(d.containerID, true)
	}
	return nil // never close the shared client
}

// ContainerID exposes the bound container id (used by the terminal backend).
func (d *DockerExecutor) ContainerID() string { return d.containerID }

func (d *DockerExecutor) ReadFile(ctx context.Context, path string) ([]byte, error) {
	out, serr, err := d.run(ctx, fmt.Sprintf("cat %s", ShellQuote(path)), 30*time.Second)
	if err != nil {
		if detail := strings.TrimSpace(serr); detail != "" {
			return nil, fmt.Errorf("%s", detail)
		}
		return nil, err
	}
	return []byte(out), nil
}

func (d *DockerExecutor) WriteFile(ctx context.Context, path string, data []byte, perm os.FileMode) error {
	mkdirCmd := fmt.Sprintf("mkdir -p %s", ShellQuote(filepath.Dir(path)))
	if _, _, err := d.run(ctx, mkdirCmd, 10*time.Second); err != nil {
		return fmt.Errorf("mkdir failed: %w", err)
	}
	encoded := base64Encode(data)
	writeCmd := fmt.Sprintf("echo %s | base64 -d > %s && chmod %o %s",
		ShellQuote(encoded), ShellQuote(path), perm, ShellQuote(path))
	if _, serr, err := d.run(ctx, writeCmd, 30*time.Second); err != nil {
		return fmt.Errorf("write failed: %s %w", serr, err)
	}
	return nil
}

func (d *DockerExecutor) MkdirAll(ctx context.Context, path string, _ os.FileMode) error {
	if _, serr, err := d.run(ctx, fmt.Sprintf("mkdir -p %s", ShellQuote(path)), 10*time.Second); err != nil {
		return fmt.Errorf("mkdir -p failed: %s %w", serr, err)
	}
	return nil
}

func (d *DockerExecutor) Stat(ctx context.Context, path string) (*FileInfo, error) {
	out, _, err := d.run(ctx, fmt.Sprintf(
		`if [ -e %s ]; then if [ -d %s ]; then echo "dir"; else echo "file"; fi; else echo "none"; fi`,
		ShellQuote(path), ShellQuote(path),
	), 5*time.Second)
	if err != nil {
		return nil, err
	}
	switch strings.TrimSpace(out) {
	case "dir":
		return &FileInfo{Exists: true, IsDir: true}, nil
	case "file":
		return &FileInfo{Exists: true, IsDir: false}, nil
	default:
		return &FileInfo{Exists: false}, nil
	}
}

func (d *DockerExecutor) Exec(ctx context.Context, command, workDir string, timeout time.Duration) (string, string, error) {
	envPrefix := "export GIT_TERMINAL_PROMPT=0 GIT_PAGER=cat PAGER=cat GIT_EDITOR=true; "
	fullCmd := envPrefix + command
	if workDir != "" {
		fullCmd = fmt.Sprintf("cd %s && %s", ShellQuote(workDir), envPrefix+command)
	}
	return d.run(ctx, fullCmd, timeout)
}

func (d *DockerExecutor) Platform() string { return d.platform }

// Name returns the container's display name.
func (d *DockerExecutor) Name() string { return d.name }

func (d *DockerExecutor) Label() string {
	if d.name != "" {
		return "docker:" + d.name
	}
	return "docker:" + shortID(d.containerID)
}

// ProjectLabel returns a stable, container-qualified session key.
func (d *DockerExecutor) ProjectLabel(pwd string) string {
	ref := d.name
	if ref == "" {
		ref = shortID(d.containerID)
	}
	return fmt.Sprintf("docker://%s%s", ref, normalizeAbs(pwd))
}

// run executes a command inside the container via `sh -c`, honoring both the
// context and the timeout. stdout/stderr are demultiplexed (Tty:false).
func (d *DockerExecutor) run(ctx context.Context, command string, timeout time.Duration) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := d.cli.ContainerExecCreate(ctx, d.containerID, container.ExecOptions{
		Cmd:          []string{"sh", "-c", command},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", "", fmt.Errorf("docker exec create: %w", err)
	}

	att, err := d.cli.ContainerExecAttach(ctx, resp.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", "", fmt.Errorf("docker exec attach: %w", err)
	}
	defer att.Close()

	var stdout, stderr bytes.Buffer
	copyDone := make(chan error, 1)
	go func() {
		_, e := stdcopy.StdCopy(&stdout, &stderr, att.Reader)
		copyDone <- e
	}()

	select {
	case <-copyDone:
		// stream drained: command finished
	case <-ctx.Done():
		att.Close() // unblock the StdCopy goroutine
		<-copyDone
		return stdout.String(), stderr.String(), fmt.Errorf("command timed out or cancelled: %w", ctx.Err())
	}

	// Use a fresh context for the inspect: the exec ctx may already be at its
	// deadline, but the exec itself completed and its exit code is available.
	inspect, ierr := d.cli.ContainerExecInspect(context.Background(), resp.ID)
	if ierr == nil && inspect.ExitCode != 0 {
		return stdout.String(), stderr.String(), fmt.Errorf("command exited with code %d", inspect.ExitCode)
	}
	return stdout.String(), stderr.String(), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func detectDockerPlatform(ctx context.Context, cli *client.Client, id string) string {
	platform := "linux/amd64"
	out, _, err := dockerExecCapture(ctx, cli, id, "uname -sm")
	if err != nil {
		return platform
	}
	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) == 2 {
		osName := strings.ToLower(parts[0])
		arch := strings.ToLower(parts[1])
		switch arch {
		case "x86_64":
			arch = "amd64"
		case "aarch64":
			arch = "arm64"
		}
		platform = osName + "/" + arch
	}
	return platform
}

// dockerExecCapture runs a one-off command and returns its stdout/stderr. Used
// before a DockerExecutor exists (platform/shell detection).
func dockerExecCapture(ctx context.Context, cli *client.Client, id, command string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := cli.ContainerExecCreate(ctx, id, container.ExecOptions{
		Cmd:          []string{"sh", "-c", command},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", "", err
	}
	att, err := cli.ContainerExecAttach(ctx, resp.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", "", err
	}
	defer att.Close()
	var stdout, stderr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdout, &stderr, att.Reader)
	return stdout.String(), stderr.String(), err
}

func tailDockerLogs(ctx context.Context, cli *client.Client, id string, lines int) string {
	rc, err := cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       fmt.Sprintf("%d", lines),
	})
	if err != nil {
		return ""
	}
	defer func() { _ = rc.Close() }()
	var buf bytes.Buffer
	_, _ = stdcopy.StdCopy(&buf, &buf, rc)
	return strings.TrimSpace(buf.String())
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func normalizeAbs(p string) string {
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}
