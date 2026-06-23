package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// dockerTestClient returns a client if a daemon is reachable, otherwise skips
// the test. This keeps the docker integration tests inert in CI environments
// without a Docker daemon.
func dockerTestClient(t *testing.T) *client.Client {
	t.Helper()
	cli, err := DockerClient()
	if err != nil {
		t.Skipf("docker client unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := cli.ContainerList(ctx, container.ListOptions{}); err != nil {
		t.Skipf("docker daemon unreachable: %v", err)
	}
	return cli
}

// createTestContainer creates (but does not start) a throwaway container running
// cmd from alpine. It registers cleanup and skips if alpine is not present
// locally (so the test never triggers a network pull).
func createTestContainer(t *testing.T, cli *client.Client, name string, cmd []string) string {
	t.Helper()
	ctx := context.Background()
	// Remove any leftover from a previous interrupted run.
	_ = cli.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})

	created, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "alpine:latest",
		Cmd:   cmd,
	}, &container.HostConfig{}, nil, nil, name)
	if err != nil {
		if strings.Contains(err.Error(), "No such image") {
			t.Skip("alpine:latest not present locally; skipping docker integration test")
		}
		t.Fatalf("create container: %v", err)
	}
	t.Cleanup(func() {
		_ = cli.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
	})
	return created.ID
}

// TestDockerExecutorSmoke exercises the full DockerExecutor surface against a
// real, self-created container: A1 start, exec, write/read roundtrip, stat, and
// ref-count auto-stop on Close.
func TestDockerExecutorSmoke(t *testing.T) {
	cli := dockerTestClient(t)
	ctx := context.Background()
	id := createTestContainer(t, cli, "jcode-docker-smoke-test", []string{"sleep", "600"})

	// The container is created but not running → AcquireDockerContainer must
	// start it (A1) and mark it as started-by-us.
	exec, err := AcquireDockerContainer(ctx, "jcode-docker-smoke-test")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !exec.startedByUs {
		t.Fatalf("expected startedByUs=true for a stopped container")
	}

	if !strings.HasPrefix(exec.Platform(), "linux/") {
		t.Errorf("platform = %q, want linux/*", exec.Platform())
	}

	out, _, err := exec.Exec(ctx, "echo hello-docker", "", 10*time.Second)
	if err != nil || strings.TrimSpace(out) != "hello-docker" {
		t.Fatalf("exec echo: out=%q err=%v", out, err)
	}

	content := []byte("line1\nline2\n")
	if err := exec.WriteFile(ctx, "/tmp/jcode/test.txt", content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := exec.ReadFile(ctx, "/tmp/jcode/test.txt")
	if err != nil || string(got) != string(content) {
		t.Fatalf("read roundtrip: got=%q err=%v", got, err)
	}

	if fi, err := exec.Stat(ctx, "/tmp/jcode"); err != nil || !fi.Exists || !fi.IsDir {
		t.Fatalf("stat dir: %+v err=%v", fi, err)
	}
	if fi, err := exec.Stat(ctx, "/tmp/jcode/test.txt"); err != nil || !fi.Exists || fi.IsDir {
		t.Fatalf("stat file: %+v err=%v", fi, err)
	}
	if fi, err := exec.Stat(ctx, "/no/such/path"); err != nil || fi.Exists {
		t.Fatalf("stat missing: %+v err=%v", fi, err)
	}

	if lbl := exec.ProjectLabel("/tmp"); !strings.HasPrefix(lbl, "docker://") {
		t.Errorf("ProjectLabel = %q, want docker://...", lbl)
	}

	// Close releases our ref-count; as the last holder it must stop the container.
	if err := exec.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		info, ierr := cli.ContainerInspect(ctx, id)
		if ierr == nil && info.State != nil && !info.State.Running {
			return // success: auto-stopped
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Errorf("container still running after Close; expected ref-count auto-stop")
}

// TestDockerExecutorOneShotFails verifies A1 semantics: a container whose main
// process exits immediately cannot host a workspace and yields an error.
func TestDockerExecutorOneShotFails(t *testing.T) {
	cli := dockerTestClient(t)
	ctx := context.Background()
	createTestContainer(t, cli, "jcode-docker-oneshot-test", []string{"true"})

	if _, err := AcquireDockerContainer(ctx, "jcode-docker-oneshot-test"); err == nil {
		t.Fatalf("expected an error for a one-shot container, got nil")
	}
}
