package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cnjack/jcode/internal/command"
)

var (
	Version   = "0.2.0"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	var (
		prompt     string
		resumeUUID string
		unsafeMode bool
	)

	rootCmd := &cobra.Command{
		Use:           "jcode",
		Short:         "Little Jack — AI coding assistant",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return command.RunInteractive(prompt, resumeUUID, unsafeMode)
		},
	}
	rootCmd.Flags().StringVarP(&prompt, "prompt", "p", "", "One-shot prompt (non-interactive)")
	rootCmd.Flags().StringVar(&resumeUUID, "resume", "", "Resume a previous session by UUID")
	rootCmd.Flags().BoolVar(&unsafeMode, "unsafe", false, "Auto-approve all tool calls (overrides config)")

	// Propagate build-time variables into the command package.
	command.Version = Version
	command.BuildTime = BuildTime
	command.GitCommit = GitCommit

	rootCmd.AddCommand(
		command.NewMCPCmd(),
		command.NewACPCmd(),
		command.NewWebCmd(),
		command.NewVersionCmd(),
		command.NewDoctorCmd(),
		command.NewSessionsCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
