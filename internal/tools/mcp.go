package tools

import (
	"context"
	"fmt"

	mcpp "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cnjack/jcode/internal/config"
	"github.com/mark3labs/mcp-go/mcp"
)

type MCPStatus struct {
	Name      string
	ToolCount int
	Error     error
	Running   bool
	// NeedsAuth is true when the server returned an OAuth authorization
	// challenge — the user must log in before tools become available.
	NeedsAuth bool
}

// LoadMCPTools establishes connections to configured MCP servers and fetches their tools.
func LoadMCPTools(ctx context.Context, mcpConfig map[string]*config.MCPServer) ([]tool.BaseTool, []MCPStatus) {
	var allTools []tool.BaseTool
	var statuses []MCPStatus

	for name, srv := range mcpConfig {
		if srv == nil || srv.Disabled {
			continue
		}

		status := MCPStatus{Name: name, Running: false}

		cli, err := buildMCPClient(name, srv)
		if err != nil {
			status.Error = fmt.Errorf("client create failed: %w", err)
			statuses = append(statuses, status)
			continue
		}

		if err := cli.Start(ctx); err != nil {
			if isMCPAuthError(err) {
				status.NeedsAuth = true
				status.Error = fmt.Errorf("authorization required — run login")
			} else {
				status.Error = fmt.Errorf("start failed: %w", err)
			}
			statuses = append(statuses, status)
			continue
		}

		initReq := mcp.InitializeRequest{}
		initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initReq.Params.ClientInfo = mcp.Implementation{
			Name:    "little-jack",
			Version: "1.0.0",
		}

		if _, err := cli.Initialize(ctx, initReq); err != nil {
			if isMCPAuthError(err) {
				status.NeedsAuth = true
				status.Error = fmt.Errorf("authorization required — run login")
			} else {
				status.Error = fmt.Errorf("init failed: %w", err)
			}
			statuses = append(statuses, status)
			continue
		}

		// fetch tools
		ts, err := mcpp.GetTools(ctx, &mcpp.Config{Cli: cli})
		if err != nil {
			status.Error = fmt.Errorf("fetch tools failed: %w", err)
			statuses = append(statuses, status)
			continue
		}

		status.Running = true
		status.ToolCount = len(ts)
		statuses = append(statuses, status)

		allTools = append(allTools, ts...)
	}

	return allTools, statuses
}
