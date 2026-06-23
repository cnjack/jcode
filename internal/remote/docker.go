package remote

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/container"

	"github.com/cnjack/jcode/internal/tools"
)

// ContainerInfo is a UI-friendly summary of a Docker container for the
// remote-connect wizard's container picker.
type ContainerInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`  // created / running / exited / ...
	Status  string `json:"status"` // human string, e.g. "Up 3 hours"
	Running bool   `json:"running"`
}

// ListContainers returns all containers (running and stopped), most useful
// first is left to the caller. It talks to the daemon configured via DOCKER_HOST
// (local socket by default).
func ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	cli, err := tools.DockerClient()
	if err != nil {
		return nil, err
	}
	summaries, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	out := make([]ContainerInfo, 0, len(summaries))
	for _, s := range summaries {
		name := ""
		if len(s.Names) > 0 {
			name = strings.TrimPrefix(s.Names[0], "/")
		}
		out = append(out, ContainerInfo{
			ID:      s.ID,
			Name:    name,
			Image:   s.Image,
			State:   s.State,
			Status:  s.Status,
			Running: s.State == "running",
		})
	}
	return out, nil
}

// ConnectDocker binds to a container by id or name, starting it if stopped
// (A1 semantics; see tools.AcquireDockerContainer).
func ConnectDocker(ctx context.Context, containerRef string) (*tools.DockerExecutor, error) {
	return tools.AcquireDockerContainer(ctx, containerRef)
}
