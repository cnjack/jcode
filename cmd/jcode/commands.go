package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tools"
	util "github.com/cnjack/jcode/internal/util"
)

func printVersion() {
	fmt.Printf("Little Jack — Coding Assistant\n")
	fmt.Printf("Version:    %s\n", Version)
	fmt.Printf("Build time: %s\n", BuildTime)
	fmt.Printf("Git commit: %s\n", GitCommit)
}

func runDoctorMode() {
	fmt.Printf("🚀 Little Jack — Coding Assistant\n")
	fmt.Printf("   Version:    %s\n", Version)
	fmt.Printf("   Build time: %s\n", BuildTime)
	fmt.Printf("   Git commit: %s\n", GitCommit)
	fmt.Println("----------------------------------------")
	fmt.Println("Running system check (Doctor Mode)...")
	fmt.Println()

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("✗ Config load failed: %v\n", err)
		return
	}

	fmt.Printf("✓ Config loaded from: %s\n", config.ConfigPath())
	fmt.Printf("✓ Active Model: %s / %s\n", cfg.Provider, cfg.Model)

	providerCfg := cfg.Models[cfg.Provider]
	if providerCfg == nil {
		fmt.Printf("✗ Provider %q not found in config\n", cfg.Provider)
		return
	}

	fmt.Println("\n[1] Testing Model Connection...")
	chatModel, err := internalmodel.NewChatModel(context.Background(), &internalmodel.ChatModelConfig{
		Model: cfg.Model, APIKey: providerCfg.APIKey, BaseURL: providerCfg.BaseURL,
	})
	if err != nil {
		fmt.Printf("  ✗ Failed to initialize model: %v\n", err)
	} else {
		msg := schema.UserMessage("hi")
		_, err := chatModel.Generate(context.Background(), []*schema.Message{msg})
		if err != nil {
			fmt.Printf("  ✗ Model generate error: %v\n", err)
		} else {
			fmt.Printf("  ✅ Model connection successful! (%s)\n", cfg.Model)
		}
	}

	fmt.Println("\n[2] Testing MCP Servers...")
	if len(cfg.MCPServers) == 0 {
		fmt.Println("  ℹ No MCP servers configured.")
	} else {
		_, statuses := tools.LoadMCPTools(context.Background(), cfg.MCPServers)
		for _, st := range statuses {
			if st.Running {
				fmt.Printf("  ✅ Server: %s (Running, %d tools loaded)\n", st.Name, st.ToolCount)
			} else {
				fmt.Printf("  ❌ Server: %s (Failed: %v)\n", st.Name, st.Error)
			}
		}
	}

	fmt.Println("\n✨ Doctor check complete.")
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
	fmt.Printf("Resume with: coding --resume <UUID>\n")
}
