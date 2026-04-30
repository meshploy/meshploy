package cmd

import (
	"fmt"
	"os"

	"github.com/meshploy/apps/cli/internal/config"
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:   "link [project-id|slug]",
	Short: "Link current directory to a Meshploy project",
	Long: `Creates a .meshploy file in the current directory that stores the project ID.
Commands like "service list", "secret set", etc. will use this project by default.

  meshploy link my-project-slug
  meshploy link 0193abcd-1234-...`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if unlink, _ := cmd.Flags().GetBool("unlink"); unlink {
			if err := config.RemoveProjectLink(); err != nil {
				return err
			}
			fmt.Println("✔  Unlinked (removed .meshploy).")
			return nil
		}

		var ref string
		if len(args) == 1 {
			ref = args[0]
		}

		c := apiClient()
		projects, err := c.ListProjects(orgID())
		if err != nil {
			return err
		}

		if ref == "" {
			if len(projects) == 0 {
				return fmt.Errorf("no projects found — create one with: meshploy project create <name>")
			}
			fmt.Println("Select a project to link:")
			for i, p := range projects {
				fmt.Printf("  [%d] %s (%s)\n", i+1, p.Name, p.Slug)
			}
			fmt.Print("Enter number: ")
			var n int
			if _, err := fmt.Scanln(&n); err != nil || n < 1 || n > len(projects) {
				fmt.Fprintln(os.Stderr, "Invalid selection.")
				os.Exit(1)
			}
			ref = projects[n-1].ID
		} else {
			// Resolve slug → ID if needed.
			if !isUUID(ref) {
				proj, err := c.GetProjectBySlugOrID(orgID(), ref)
				if err != nil {
					return err
				}
				ref = proj.ID
			}
		}

		if err := config.SaveProjectLink(ref); err != nil {
			return err
		}

		// Show the resolved project name.
		for _, p := range projects {
			if p.ID == ref {
				fmt.Printf("✔  Linked to project %q (%s). Saved to .meshploy\n", p.Name, p.Slug)
				return nil
			}
		}
		fmt.Printf("✔  Linked to project %s. Saved to .meshploy\n", ref)
		return nil
	},
}

func init() {
	linkCmd.Flags().Bool("unlink", false, "Remove the .meshploy link file")
	rootCmd.AddCommand(linkCmd)
}
