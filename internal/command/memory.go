package command

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/memory"
)

// NewMemoryCmd returns the `jcode memory` command group: inspect, clear and
// (M2+) synchronize the cross-session learned memory store.
func NewMemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Inspect and manage cross-session learned memory (~/.jcode/memory)",
	}
	cmd.AddCommand(newMemoryPathCmd(), newMemoryStatusCmd(), newMemoryClearCmd(), newMemorySyncCmd())
	return cmd
}

func memoryCwd() (string, error) {
	pwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}
	return pwd, nil
}

func newMemoryPathCmd() *cobra.Command {
	var format string
	c := &cobra.Command{
		Use:   "path",
		Short: "Print the memory location for the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			pwd, err := memoryCwd()
			if err != nil {
				return err
			}
			switch format {
			case "slug":
				fmt.Println(memory.ProjectSlug(pwd))
			case "root":
				fmt.Println(memory.Root())
			case "", "project":
				fmt.Println(memory.ProjectRoot(pwd))
			default:
				return fmt.Errorf("unknown --format %q (want project|slug|root)", format)
			}
			return nil
		},
	}
	c.Flags().StringVar(&format, "format", "project", "what to print: project|slug|root")
	return c
}

func newMemoryStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show memory status for the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			pwd, err := memoryCwd()
			if err != nil {
				return err
			}
			cfg, _ := config.LoadConfig()
			root := memory.ProjectRoot(pwd)
			fmt.Printf("enabled:        %v\n", config.MemoryEnabled(cfg))
			fmt.Printf("generate:       %v\n", config.MemoryGenerate(cfg))
			fmt.Printf("project root:   %s\n", root)
			fmt.Printf("global root:    %s\n", memory.GlobalRoot())
			summary := filepath.Join(root, memory.SummaryFile)
			if st, err := os.Stat(summary); err == nil {
				fmt.Printf("summary:        %s (%d bytes)\n", summary, st.Size())
			} else {
				fmt.Printf("summary:        (none yet)\n")
			}
			notes := memory.RecentNotes(root, 0)
			fmt.Printf("inbox notes:    %d\n", len(notes))
			for i, n := range notes {
				if i >= 5 {
					fmt.Printf("  ... and %d more\n", len(notes)-5)
					break
				}
				fmt.Printf("  - [%s] %s\n", n.Kind, n.Name)
			}
			st := memory.LoadState(root)
			fmt.Printf("tracked files:  %d (usage accounting)\n", len(st.Files))
			return nil
		},
	}
}

func newMemoryClearCmd() *cobra.Command {
	var clearGlobal, clearAll bool
	c := &cobra.Command{
		Use:   "clear",
		Short: "Delete learned memory (project scope by default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if clearAll {
				fmt.Printf("clearing all memory: %s\n", memory.Root())
				return os.RemoveAll(memory.Root())
			}
			if clearGlobal {
				fmt.Printf("clearing global memory: %s\n", memory.GlobalRoot())
				return os.RemoveAll(memory.GlobalRoot())
			}
			pwd, err := memoryCwd()
			if err != nil {
				return err
			}
			root := memory.ProjectRoot(pwd)
			// Don't delete out from under a running pipeline: take its lock
			// first so we can't remove the lock file mid-run and resurrect a
			// half-written scope.
			release, ok, lerr := memory.TryLockPipeline(root)
			if lerr == nil && !ok {
				return fmt.Errorf("memory pipeline is running for this project; try again shortly")
			}
			if release != nil {
				release()
			}
			fmt.Printf("clearing project memory: %s\n", root)
			return os.RemoveAll(root)
		},
	}
	c.Flags().BoolVar(&clearGlobal, "global", false, "clear the global scope instead of the project scope")
	c.Flags().BoolVar(&clearAll, "all", false, "clear the entire memory root")
	return c
}

func newMemorySyncCmd() *cobra.Command {
	var wait, includeRecent bool
	c := &cobra.Command{
		Use:   "sync",
		Short: "Run the memory distillation pipeline for the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			pwd, err := memoryCwd()
			if err != nil {
				return err
			}
			cfg, _ := config.LoadConfig()
			if !config.MemoryGenerate(cfg) {
				return fmt.Errorf("memory pipeline is disabled (memory.enabled/generate=false)")
			}
			return runMemorySync(cmd.Context(), cfg, pwd, wait, includeRecent)
		},
	}
	c.Flags().BoolVar(&wait, "wait", false, "run in the foreground and wait for completion")
	c.Flags().BoolVar(&includeRecent, "include-recent", false, "also extract recently-ended sessions (skip the idle gate)")
	return c
}
