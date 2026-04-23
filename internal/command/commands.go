package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/cobra"

	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tools"
	util "github.com/cnjack/jcode/internal/util"
)

func printVersion() {
	fmt.Printf("JCODE — Coding Assistant\n")
	fmt.Printf("Version:    %s\n", Version)
	fmt.Printf("Build time: %s\n", BuildTime)
	fmt.Printf("Git commit: %s\n", GitCommit)
}

func runDoctorMode() {
	// ANSI color codes for [JCODE] logo
	orange := "\033[1;38;2;255;132;0m"
	gray := "\033[38;2;107;114;128m"
	reset := "\033[0m"
	logo := fmt.Sprintf("%s[%s%sJ%s%sCODE%s%s]%s", gray, reset, orange, reset, gray, reset, gray, reset)

	fmt.Println("┌─────────────────────────────────────┐")
	fmt.Printf("│  %-35s│\n", logo)
	fmt.Println("│  ─────────────────────────────────  │")
	fmt.Printf("│  Version:    %-23s│\n", Version)
	fmt.Printf("│  Build time: %-23s│\n", BuildTime)
	fmt.Printf("│  Git commit: %-23s│\n", GitCommit)
	fmt.Println("└─────────────────────────────────────┘")
	fmt.Println()

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("  ✗ Config load failed: %v\n", err)
		return
	}

	fmt.Printf("  ✓ Config loaded from: %s\n", config.ConfigPath())

	providerName, modelName := cfg.GetProviderModel()
	fmt.Printf("  ✓ Active model: %s / %s\n", providerName, modelName)

	providers := cfg.GetProviders()
	providerCfg := providers[providerName]
	if providerCfg == nil {
		fmt.Printf("  ✗ Provider %q not found in config\n", providerName)
		return
	}

	// Resolve base URL from config or registry
	baseURL := providerCfg.BaseURL
	if baseURL == "" {
		registry := internalmodel.NewModelRegistry()
		baseURL = registry.GetProviderAPI(providerName)
	}

	fmt.Println("\n  [1] Model Connection")
	chatModel, err := internalmodel.NewChatModel(context.Background(), &internalmodel.ChatModelConfig{
		Model: modelName, APIKey: providerCfg.APIKey, BaseURL: baseURL,
	})
	if err != nil {
		fmt.Printf("      ✗ Failed to initialize: %v\n", err)
	} else {
		msg := schema.UserMessage("hi")
		_, err := chatModel.Generate(context.Background(), []*schema.Message{msg})
		if err != nil {
			fmt.Printf("      ✗ Generate error: %v\n", err)
		} else {
			fmt.Printf("      ✓ Connection successful (%s)\n", modelName)
		}
	}

	fmt.Println("\n  [2] Required Tools")
	if rgPath, err := exec.LookPath("rg"); err != nil {
		fmt.Println("      ✗ ripgrep (rg) — not found")
		fmt.Println("        Install: https://github.com/BurntSushi/ripgrep#installation")
	} else {
		fmt.Printf("      ✓ ripgrep (rg) — %s\n", rgPath)
	}

	fmt.Println("\n  [3] MCP Servers")
	if len(cfg.MCPServers) == 0 {
		fmt.Println("      - No MCP servers configured")
	} else {
		_, statuses := tools.LoadMCPTools(context.Background(), cfg.MCPServers)
		for _, st := range statuses {
			if st.Running {
				fmt.Printf("      ✓ %s — running, %d tools\n", st.Name, st.ToolCount)
			} else {
				fmt.Printf("      ✗ %s — %v\n", st.Name, st.Error)
			}
		}
	}

	fmt.Println("\n  All checks complete.")
}

func handleListSessions() {
	pwd := util.GetWorkDir()
	metas, err := session.ListSessions(pwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading sessions: %v\n", err)
		os.Exit(1)
	}
	if len(metas) == 0 {
		fmt.Printf("No sessions found for project: %s\n", pwd)
		return
	}
	fmt.Printf("Sessions for %s:\n\n", pwd)
	for i, m := range metas {
		fmt.Printf("  [%d] UUID:      %s\n", i+1, m.UUID)
		fmt.Printf("      Started:   %s\n", m.StartTime)
		fmt.Printf("      Provider:  %s / %s\n", m.Provider, m.Model)
		fmt.Println()
	}
	fmt.Printf("Resume with: jcode --resume <UUID>\n")
}

func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			printVersion()
		},
	}
}

func NewDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run system check (tests model and MCP connections)",
		Run: func(cmd *cobra.Command, args []string) {
			runDoctorMode()
		},
	}
}

func NewSessionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List sessions for the current project",
		Run: func(cmd *cobra.Command, args []string) {
			handleListSessions()
		},
	}
}
