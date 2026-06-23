package command

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cnjack/jcode/internal/automation"
)

// NewAutomationCmd builds the `jcode automation` command tree for managing
// automations from the terminal. Definition management works standalone (it
// only touches ~/.jcode/automations.json); periodic firing is owned by a running
// `jcode web` process.
func NewAutomationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "automation",
		Aliases: []string{"automations", "auto"},
		Short:   "Manage automations (scheduled and manual agent tasks)",
	}
	cmd.AddCommand(
		newAutomationListCmd(),
		newAutomationShowCmd(),
		newAutomationTemplatesCmd(),
		newAutomationEnableCmd(true),
		newAutomationEnableCmd(false),
		newAutomationDeleteCmd(),
	)
	return cmd
}

func openAutomationStore() (*automation.Store, error) {
	return automation.NewStore()
}

func newAutomationListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all automations",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openAutomationStore()
			if err != nil {
				return err
			}
			list := st.List()
			if len(list) == 0 {
				fmt.Println("No automations yet.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tSCHEDULE\tENABLED\tLAST\tPROJECT")
			for _, a := range list {
				state := st.State(a.ID)
				last := state.LastStatus
				if last == "" {
					last = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%s\t%s\n",
					a.ID, a.Name, automation.HumanSchedule(a.Trigger), a.Enabled, last, a.ProjectPath)
			}
			return w.Flush()
		},
	}
}

func newAutomationShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show an automation's definition and last run state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openAutomationStore()
			if err != nil {
				return err
			}
			a := st.Get(args[0])
			if a == nil {
				return fmt.Errorf("automation %q not found", args[0])
			}
			state := st.State(a.ID)
			fmt.Printf("ID:        %s\nName:      %s\nSchedule:  %s\nEnabled:   %v\nProject:   %s\nMode:      %s\nSource:    %s\n",
				a.ID, a.Name, automation.HumanSchedule(a.Trigger), a.Enabled, a.ProjectPath, a.Mode, a.Source)
			fmt.Printf("Last run:  %s (%s)\nNext run:  %s\n", nz(state.LastRunAt), nz(state.LastStatus), nz(state.NextRunAt))
			if state.LastError != "" {
				fmt.Printf("Last error: %s\n", state.LastError)
			}
			fmt.Printf("\nPrompt:\n%s\n", a.Prompt)
			return nil
		},
	}
}

func newAutomationTemplatesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "templates",
		Short: "List built-in automation templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tBADGE\tDESCRIPTION")
			for _, t := range automation.BuiltinTemplates() {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.ID, t.Name, t.Badge, t.Description)
			}
			return w.Flush()
		},
	}
}

func newAutomationEnableCmd(enable bool) *cobra.Command {
	use, short := "enable <id>", "Enable an automation"
	if !enable {
		use, short = "disable <id>", "Disable an automation"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openAutomationStore()
			if err != nil {
				return err
			}
			a, err := st.SetEnabled(args[0], enable)
			if err != nil {
				return err
			}
			fmt.Printf("%s is now %s\n", a.Name, enabledWord(a.Enabled))
			return nil
		},
	}
}

func newAutomationDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "Delete an automation",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openAutomationStore()
			if err != nil {
				return err
			}
			if err := st.Delete(args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted automation %s\n", args[0])
			return nil
		},
	}
}

func enabledWord(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

func nz(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
