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
	var (
		scope   string
		srvType string
		headers []string
		envs    []string
	)

	cmd := &cobra.Command{
		Use:          "add <name> <url-or-command> [args...]",
		Short:        "Add an MCP server (SSE/HTTP or stdio)",
		Args:         cobra.MinimumNArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleMCPAdd(args, srvType, headers, envs, scope)
		},
	}

	cmd.Flags().StringVarP(&scope, "scope", "s", "user", "Config scope (user)")
	cmd.Flags().StringVarP(&srvType, "type", "t", "", "Server type: sse, http, or stdio (auto-detect if empty)")
	cmd.Flags().StringArrayVar(&headers, "header", nil, "HTTP header in 'Key: Value' format (repeatable)")
	cmd.Flags().StringArrayVar(&envs, "env", nil, "Environment variable in 'KEY=VALUE' format (repeatable)")

	return cmd
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

func handleMCPAdd(args []string, srvType string, headers []string, envs []string, scope string) error {
	_ = scope // user-level only for now; reserved for future scope support
	_ = envs  // stored on MCPServer but auto-detect from URL vs command

	name := args[0]
	urlOrCmd := args[1]
	extraArgs := args[2:]

	srv := &config.MCPServer{}

	// If no explicit type, auto-detect from URL prefix
	if srvType == "" {
		if strings.HasPrefix(urlOrCmd, "http://") || strings.HasPrefix(urlOrCmd, "https://") {
			srvType = "sse"
		} else {
			srvType = "stdio"
		}
	}

	// Parse headers into a map
	headerMap := make(map[string]string)
	for _, h := range headers {
		if idx := strings.Index(h, ":"); idx > 0 {
			key := strings.TrimSpace(h[:idx])
			val := strings.TrimSpace(h[idx+1:])
			headerMap[key] = val
		}
	}

	switch srvType {
	case "http":
		srv.Type = "http"
		srv.URL = urlOrCmd
		if len(headerMap) > 0 {
			srv.Headers = headerMap
		}
	case "sse":
		srv.Type = "sse"
		srv.URL = urlOrCmd
		if len(headerMap) > 0 {
			srv.Headers = headerMap
		}
	case "stdio":
		srv.Type = "stdio"
		srv.Command = urlOrCmd
		srv.Args = extraArgs
		if len(envs) > 0 {
			srv.Env = envs
		}
	default:
		return fmt.Errorf("unknown server type: %s (supported: sse, http, stdio)", srvType)
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
		switch srv.Type {
		case "http", "sse":
			fmt.Printf("  %-20s  url=%s  type=%s\n", name, srv.URL, srv.Type)
			for k, v := range srv.Headers {
				fmt.Printf("  %-22s  header=%s: %s\n", "", k, v)
			}
		default:
			fmt.Printf("  %-20s  cmd=%s  args=%v  type=%s\n", name, srv.Command, srv.Args, srv.Type)
			if len(srv.Env) > 0 {
				fmt.Printf("  %-22s  env=%v\n", "", srv.Env)
			}
		}
	}
	return nil
}
