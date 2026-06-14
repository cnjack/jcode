package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/tools"
	util "github.com/cnjack/jcode/internal/util"
)

func NewMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "mcp",
		Short:        "Manage MCP servers",
		SilenceUsage: true,
	}
	cmd.AddCommand(newMCPAddCmd(), newMCPListCmd(), newMCPLoginCmd())
	return cmd
}

// mcpAddFlags bundles the flags accepted by `mcp add`.
type mcpAddFlags struct {
	scope        string
	srvType      string
	headers      []string
	envs         []string
	oauth        bool
	clientID     string
	clientSecret string
	scopes       []string
}

func newMCPAddCmd() *cobra.Command {
	var f mcpAddFlags

	cmd := &cobra.Command{
		Use:          "add <name> <url-or-command> [args...]",
		Short:        "Add an MCP server (SSE/HTTP or stdio)",
		Args:         cobra.MinimumNArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleMCPAdd(args, &f)
		},
	}

	cmd.Flags().StringVarP(&f.scope, "scope", "s", "user", "Config scope (user)")
	cmd.Flags().StringVarP(&f.srvType, "type", "t", "", "Server type: sse, http, or stdio (auto-detect if empty)")
	cmd.Flags().StringArrayVar(&f.headers, "header", nil, "HTTP header in 'Key: Value' format (repeatable)")
	cmd.Flags().StringArrayVar(&f.envs, "env", nil, "Environment variable in 'KEY=VALUE' format (repeatable)")
	cmd.Flags().BoolVar(&f.oauth, "oauth", false, "Authenticate via OAuth after adding (http/sse only)")
	cmd.Flags().StringVar(&f.clientID, "client-id", "", "OAuth client id (manual fallback when dynamic registration is unsupported)")
	cmd.Flags().StringVar(&f.clientSecret, "client-secret", "", "OAuth client secret (confidential clients)")
	cmd.Flags().StringArrayVar(&f.scopes, "scope-oauth", nil, "OAuth scope to request (repeatable)")

	return cmd
}

func newMCPLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "login <name>",
		Short:        "Authenticate an existing http/sse MCP server via OAuth",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleMCPLogin(args[0])
		},
	}
}

// mcpLoginBrowser opens the authorization URL in the browser and prints it as a
// fallback. Used as the onAuthURL callback for CLI OAuth flows.
func mcpLoginBrowser(authURL string) {
	fmt.Printf("Opening browser for authorization. If it does not open, visit:\n  %s\n", authURL)
	if err := util.OpenURL(authURL); err != nil {
		config.Logger().Printf("[mcp] open browser: %v", err)
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

func handleMCPAdd(args []string, f *mcpAddFlags) error {
	_ = f.scope // user-level only for now; reserved for future scope support

	name := args[0]
	urlOrCmd := args[1]
	extraArgs := args[2:]

	srv := &config.MCPServer{}

	srvType := f.srvType
	// If no explicit type, auto-detect from URL prefix.
	if srvType == "" {
		switch {
		case strings.HasPrefix(urlOrCmd, "http://") || strings.HasPrefix(urlOrCmd, "https://"):
			// OAuth servers use the modern streamable-http transport by default.
			if f.oauth {
				srvType = "http"
			} else {
				srvType = "sse"
			}
		default:
			srvType = "stdio"
		}
	}

	// Parse headers into a map
	headerMap := make(map[string]string)
	for _, h := range f.headers {
		if idx := strings.Index(h, ":"); idx > 0 {
			key := strings.TrimSpace(h[:idx])
			val := strings.TrimSpace(h[idx+1:])
			headerMap[key] = val
		}
	}

	switch srvType {
	case "http", "sse":
		srv.Type = srvType
		srv.URL = urlOrCmd
		if len(headerMap) > 0 {
			srv.Headers = headerMap
		}
	case "stdio":
		srv.Type = "stdio"
		srv.Command = urlOrCmd
		srv.Args = extraArgs
		if len(f.envs) > 0 {
			srv.Env = f.envs
		}
	default:
		return fmt.Errorf("unknown server type: %s (supported: sse, http, stdio)", srvType)
	}

	if f.oauth {
		if srv.Type != "http" && srv.Type != "sse" {
			return fmt.Errorf("--oauth only applies to http/sse servers")
		}
		srv.OAuth = &config.MCPOAuthConfig{
			Enabled:      true,
			ClientID:     f.clientID,
			ClientSecret: f.clientSecret,
			Scopes:       f.scopes,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		fmt.Printf("Starting OAuth login for '%s'...\n", name)
		if err := tools.PerformMCPOAuthLogin(ctx, name, srv, mcpLoginBrowser); err != nil {
			return fmt.Errorf("oauth login failed: %w", err)
		}
		fmt.Println("Authorized.")
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

// handleMCPLogin runs the OAuth authorization flow for an existing http/sse
// server and persists the (possibly dynamically registered) client + token.
func handleMCPLogin(name string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	srv := cfg.MCPServers[name]
	if srv == nil {
		return fmt.Errorf("MCP server %q not found", name)
	}
	if srv.URL == "" || (srv.Type != "http" && srv.Type != "sse") {
		return fmt.Errorf("OAuth login only applies to http/sse servers")
	}
	if srv.OAuth == nil {
		srv.OAuth = &config.MCPOAuthConfig{Enabled: true}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fmt.Printf("Starting OAuth login for '%s'...\n", name)
	if err := tools.PerformMCPOAuthLogin(ctx, name, srv, mcpLoginBrowser); err != nil {
		return fmt.Errorf("oauth login failed: %w", err)
	}
	if err := config.SaveConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Printf("MCP server '%s' authorized\n", name)
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
