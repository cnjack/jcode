package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

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
	// Visible width of "[JCODE]" is 7; pad manually to align the box border.
	visibleWidth := 7 // len("[JCODE]")
	padding := 35 - visibleWidth
	if padding < 0 {
		padding = 0
	}

	fmt.Println("┌─────────────────────────────────────┐")
	fmt.Printf("│  %s%s│\n", logo, fmt.Sprintf("%*s", padding, ""))
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
	if providers[providerName] == nil {
		fmt.Printf("  ✗ Provider %q not found in config\n", providerName)
		return
	}

	fmt.Println("\n  [1] Model Connection")
	// Both probes build through the same ModelFactory path the runtime uses
	// (baseURL/effort/alias resolution), each bounded by a timeout so a silent
	// endpoint cannot hang the doctor.
	factory := internalmodel.NewModelFactory(cfg, nil)
	if err := doctorProbeModel(context.Background(), factory, providerName+"/"+modelName); err != nil {
		fmt.Printf("      ✗ %s: %v\n", modelName, err)
	} else {
		fmt.Printf("      ✓ Connection successful (%s)\n", modelName)
	}
	// Probe the small model too when configured — it serves subagents (the
	// "small" alias) and session titles, so a broken ref should surface here
	// rather than as silently degraded behavior.
	if cfg.SmallModel != "" {
		if err := doctorProbeModel(context.Background(), factory, cfg.SmallModel); err != nil {
			fmt.Printf("      ✗ small_model %s: %v\n", cfg.SmallModel, err)
		} else {
			fmt.Printf("      ✓ small_model connection successful (%s)\n", cfg.SmallModel)
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

// doctorProbeTimeout bounds each connectivity probe: a TCP-accepting but
// silent endpoint must fail the check, not hang the doctor.
const doctorProbeTimeout = 60 * time.Second

// doctorProbeModel resolves the model through the shared factory — the same
// construction path the runtime uses — and issues one short generate.
func doctorProbeModel(ctx context.Context, f *internalmodel.ModelFactory, providerModel string) error {
	ctx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
	defer cancel()
	cm, err := f.GetModel(ctx, providerModel)
	if err != nil {
		return err
	}
	if cm == nil {
		return fmt.Errorf("model %q did not resolve", providerModel)
	}
	_, err = cm.Generate(ctx, []*schema.Message{schema.UserMessage("hi")})
	return err
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
