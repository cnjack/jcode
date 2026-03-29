package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/tools"
)

func handleMCPSubcommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: coding mcp <add|list>")
		fmt.Println()
		fmt.Println("  coding mcp add <name> <url>               Add SSE/HTTP MCP server")
		fmt.Println("  coding mcp add <name> <command> [args...]  Add stdio MCP server")
		fmt.Println("  coding mcp list                            List configured MCP servers")
		os.Exit(1)
	}
	switch args[0] {
	case "add":
		handleMCPAdd(args[1:])
	case "list":
		handleMCPList()
	default:
		fmt.Fprintf(os.Stderr, "Unknown mcp subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func handleMCPAdd(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: coding mcp add <name> <url-or-command> [args...]")
		os.Exit(1)
	}
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
		fmt.Fprintf(os.Stderr, "❌ Connection test failed: %s\n", errMsg)
		os.Exit(1)
	}

	fmt.Printf("✅ Connected — %d tool(s) loaded\n", statuses[0].ToolCount)

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = make(map[string]*config.MCPServer)
	}
	cfg.MCPServers[name] = srv
	if err := config.SaveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ MCP server '%s' saved to config\n", name)
}

func handleMCPList() {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	if len(cfg.MCPServers) == 0 {
		fmt.Println("No MCP servers configured.")
		return
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
}
