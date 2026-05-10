package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var stackCmd = &cobra.Command{
	Use:   "stack",
	Short: "Manage stacks (compose-style multi-service deployments)",
}

var stackProject string

var stackListCmd = &cobra.Command{
	Use:   "list",
	Short: "List stacks in a project",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(stackProject)
		stacks, err := c.ListStacks(orgID(), pid)
		if err != nil {
			return err
		}
		if len(stacks) == 0 {
			fmt.Println("No stacks found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tSTATUS\tLAST APPLIED")
		for _, s := range stacks {
			applied := "—"
			if s.LastAppliedAt != nil {
				applied = *s.LastAppliedAt
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.ID, s.Name, s.Status, applied)
		}
		return w.Flush()
	},
}

var stackGetCmd = &cobra.Command{
	Use:   "get <name|id>",
	Short: "Get details of a stack",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(stackProject)
		stack, err := c.GetStackByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		fmt.Printf("ID:     %s\n", stack.ID)
		fmt.Printf("Name:   %s\n", stack.Name)
		fmt.Printf("Status: %s\n", stack.Status)
		if stack.LastAppliedAt != nil {
			fmt.Printf("Last applied: %s\n", *stack.LastAppliedAt)
		}
		return nil
	},
}

var stackServicesCmd = &cobra.Command{
	Use:   "services <name|id>",
	Short: "List services belonging to a stack",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(stackProject)
		stack, err := c.GetStackByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		services, err := c.ListStackServices(orgID(), pid, stack.ID)
		if err != nil {
			return err
		}
		if len(services) == 0 {
			fmt.Println("No services in this stack.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tSTATUS")
		for _, s := range services {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.ID, s.Name, s.Type, s.Status)
		}
		return w.Flush()
	},
}

var stackApplyCmd = &cobra.Command{
	Use:   "apply <name|id>",
	Short: "Apply the stack spec — reconcile services",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(stackProject)
		stack, err := c.GetStackByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		result, err := c.ApplyStack(orgID(), pid, stack.ID)
		if err != nil {
			return err
		}
		fmt.Printf("✔  Stack %q applied.\n", stack.Name)
		if len(result.Created) > 0 {
			fmt.Printf("   Created: %v\n", result.Created)
		}
		if len(result.Updated) > 0 {
			fmt.Printf("   Updated: %v\n", result.Updated)
		}
		if len(result.Deleted) > 0 {
			fmt.Printf("   Deleted: %v\n", result.Deleted)
		}
		if len(result.Errors) > 0 {
			fmt.Printf("   Errors:  %v\n", result.Errors)
		}
		return nil
	},
}

var stackDeleteCmd = &cobra.Command{
	Use:   "delete <name|id>",
	Short: "Delete a stack",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(stackProject)
		stack, err := c.GetStackByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fmt.Printf("Delete stack %q? [y/N]: ", stack.Name)
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		if err := c.DeleteStack(orgID(), pid, stack.ID); err != nil {
			return err
		}
		fmt.Printf("✔  Stack %q deleted.\n", stack.Name)
		return nil
	},
}

func init() {
	stackCmd.PersistentFlags().StringVarP(&stackProject, "project", "p", "", "Project ID or slug")
	stackDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation")
	stackCmd.AddCommand(stackListCmd, stackGetCmd, stackServicesCmd, stackApplyCmd, stackDeleteCmd)
	rootCmd.AddCommand(stackCmd)
}
