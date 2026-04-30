package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage projects",
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		projects, err := c.ListProjects(orgID())
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			fmt.Println("No projects found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tSLUG")
		for _, p := range projects {
			fmt.Fprintf(w, "%s\t%s\t%s\n", p.ID, p.Name, p.Slug)
		}
		return w.Flush()
	},
}

var projectCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		p, err := c.CreateProject(orgID(), args[0])
		if err != nil {
			return err
		}
		fmt.Printf("✔  Project created: %s (%s)\n", p.Name, p.ID)
		return nil
	},
}

var projectDeleteCmd = &cobra.Command{
	Use:   "delete <id|slug>",
	Short: "Delete a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		ref := args[0]
		c := apiClient()
		proj, err := c.GetProjectBySlugOrID(orgID(), ref)
		if err != nil {
			return err
		}
		if !yes {
			fmt.Printf("Delete project %q (%s)? This cannot be undone. [y/N]: ", proj.Name, proj.ID)
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		if err := c.DeleteProject(orgID(), proj.ID); err != nil {
			return err
		}
		fmt.Printf("✔  Project %q deleted.\n", proj.Name)
		return nil
	},
}

func init() {
	projectDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation")
	projectCmd.AddCommand(projectListCmd, projectCreateCmd, projectDeleteCmd)
	rootCmd.AddCommand(projectCmd)
}
