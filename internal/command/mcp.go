package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/tools"
)

func NewMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "mcp",
		Short:        "Manage MCP servers",
		SilenceUsage: true,
	}
	cmd.AddCommand(newMCPAddCmd(), newMCPListCmd())
	return cmd
}

func newMCPAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "add <name> <url-or-command> [args...]",
		Short:        "Add an MCP server (SSE/HTTP or stdio)",
		Args:         cobra.MinimumNArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleMCPAdd(args)
		},
	}
}

func newMCPListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List configured MCP servers",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleMCPList()
		},
	}
}

func handleMCPAdd(args []string) error {
	name := args[0]
	urlOrCmd := args[1]
	extraArgs := args[2:]

	srv := &config.MCPServer{}
	if strings.HasPrefix(urlOrCmd, "http://") || strings.HasPrefix(urlOrCmd, "https://") {
		srv.URL = urlOrCmd
		srv.Type = "sse"
	} else {
		srv.Command = urlOrCmd
		srv.Args = extraArgs
		srv.Type = "stdio"
	}

	fmt.Printf("Testing MCP server '%s'...\n", name)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testServers := map[string]*config.MCPServer{name: srv}
	_, statuses := tools.LoadMCPTools(ctx, testServers)

	if len(statuses) == 0 || statuses[0].Error != nil {
		errMsg := "unknown error"
		if len(statuses) > 0 && statuses[0].Error != nil {
			errMsg = statuses[0].Error.Error()
		}
		return fmt.Errorf("connection test failed: %s", errMsg)
	}

	fmt.Printf("Connected — %d tool(s) loaded\n", statuses[0].ToolCount)

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = make(map[string]*config.MCPServer)
	}
	cfg.MCPServers[name] = srv
	if err := config.SaveConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Printf("MCP server '%s' saved to config\n", name)
	return nil
}

func handleMCPList() error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if len(cfg.MCPServers) == 0 {
		fmt.Println("No MCP servers configured.")
		return nil
	}
	fmt.Println("Configured MCP servers:")
	fmt.Println()
	for name, srv := range cfg.MCPServers {
		if srv.URL != "" {
			fmt.Printf("  %-20s  url=%s  type=%s\n", name, srv.URL, srv.Type)
		} else {
			fmt.Printf("  %-20s  cmd=%s  args=%v  type=%s\n", name, srv.Command, srv.Args, srv.Type)
		}
	}
	return nil
}
