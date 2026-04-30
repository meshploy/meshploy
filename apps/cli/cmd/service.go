package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage services and databases",
}

var serviceProject string

var serviceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List services in a project",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		services, err := c.ListServices(orgID(), pid)
		if err != nil {
			return err
		}
		if len(services) == 0 {
			fmt.Println("No services found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tSTATUS\tIMAGE")
		for _, s := range services {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Name, s.Type, s.Status, s.Image)
		}
		return w.Flush()
	},
}

var serviceDeployCmd = &cobra.Command{
	Use:   "deploy <name|id>",
	Short: "Trigger a new deployment for a service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		d, err := c.Deploy(orgID(), pid, svc.ID)
		if err != nil {
			return err
		}
		fmt.Printf("✔  Deployment triggered: %s (status: %s)\n", d.ID, d.Status)
		return nil
	},
}

var serviceStartCmd = &cobra.Command{
	Use:   "start <name|id>",
	Short: "Start a stopped service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		if err := c.StartService(orgID(), pid, svc.ID); err != nil {
			return err
		}
		fmt.Printf("✔  Service %q starting.\n", svc.Name)
		return nil
	},
}

var serviceStopCmd = &cobra.Command{
	Use:   "stop <name|id>",
	Short: "Stop a running service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		if err := c.StopService(orgID(), pid, svc.ID); err != nil {
			return err
		}
		fmt.Printf("✔  Service %q stopped.\n", svc.Name)
		return nil
	},
}

var serviceLogsCmd = &cobra.Command{
	Use:   "logs <name|id>",
	Short: "Stream live container logs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Streaming logs for %q — press Ctrl+C to stop\n", svc.Name)
		return c.StreamLogs(orgID(), pid, svc.ID, os.Stdout)
	},
}

var serviceDeleteCmd = &cobra.Command{
	Use:   "delete <name|id>",
	Short: "Delete a service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fmt.Printf("Delete service %q? [y/N]: ", svc.Name)
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		if err := c.DeleteService(orgID(), pid, svc.ID); err != nil {
			return err
		}
		fmt.Printf("✔  Service %q deleted.\n", svc.Name)
		return nil
	},
}

func init() {
	serviceCmd.PersistentFlags().StringVarP(&serviceProject, "project", "p", "", "Project ID or slug")

	serviceDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation")

	serviceCmd.AddCommand(serviceListCmd, serviceDeployCmd, serviceStartCmd, serviceStopCmd, serviceLogsCmd, serviceDeleteCmd)
	rootCmd.AddCommand(serviceCmd)
}

// resolveProjectID resolves a project from the flag or linked .meshploy file,
// then looks it up by slug if needed.
func resolveProjectID(flagVal string) string {
	ref := resolveProject(flagVal)
	// If it looks like a UUID, use directly.
	if isUUID(ref) {
		return ref
	}
	// Otherwise resolve slug → ID.
	c := apiClient()
	proj, err := c.GetProjectBySlugOrID(orgID(), ref)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	return proj.ID
}

func isUUID(s string) bool {
	return len(s) == 36 && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}
