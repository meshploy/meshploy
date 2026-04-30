package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/meshploy/apps/cli/internal/client"
	"github.com/spf13/cobra"
)

var routeCmd = &cobra.Command{
	Use:   "route",
	Short: "Manage HTTP routes",
}

var routeProject string

var routeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List routes in a project",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(routeProject)
		routes, err := c.ListRoutes(orgID(), pid)
		if err != nil {
			return err
		}
		if len(routes) == 0 {
			fmt.Println("No routes found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tHOSTNAME\tTARGET\tZONE")
		for _, r := range routes {
			fmt.Fprintf(w, "%s\t%s\t%s:%d\t%s\n", r.ID, r.Hostname, r.TargetIP, r.TargetPort, r.Zone)
		}
		return w.Flush()
	},
}

// route create — supports three targeting modes:
//
//	--service <name|id>          managed Meshploy service (IP resolved automatically)
//	--node <name|id> --port N    any process running on a mesh node (no service required)
//	--ip <mesh-ip> --port N      direct IP+port override (advanced)
var routeCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a route to a service or any process on a node",
	Long: `Create an HTTP route via Meshploy's proxy.

Target a managed service (IP resolved automatically):
  meshploy route create --hostname app.example.com --service my-api

Target any process already running on a node (no Meshploy service needed):
  meshploy route create --hostname app.example.com --node worker-1 --port 8080

Use a raw mesh IP directly:
  meshploy route create --hostname app.example.com --ip 100.64.0.5 --port 8080`,
	RunE: func(cmd *cobra.Command, args []string) error {
		hostname, _ := cmd.Flags().GetString("hostname")
		serviceRef, _ := cmd.Flags().GetString("service")
		nodeRef, _ := cmd.Flags().GetString("node")
		port, _ := cmd.Flags().GetInt("port")
		targetIP, _ := cmd.Flags().GetString("ip")
		zone, _ := cmd.Flags().GetString("zone")

		if hostname == "" {
			return fmt.Errorf("--hostname is required")
		}

		// Exactly one targeting mode must be provided.
		modes := 0
		if serviceRef != "" {
			modes++
		}
		if nodeRef != "" {
			modes++
		}
		if targetIP != "" {
			modes++
		}
		if modes != 1 {
			return fmt.Errorf("specify exactly one of --service, --node, or --ip")
		}
		if (nodeRef != "" || targetIP != "") && port == 0 {
			return fmt.Errorf("--port is required when using --node or --ip")
		}

		c := apiClient()
		pid := resolveProjectID(routeProject)
		body := client.CreateRouteBody{
			Hostname: &hostname,
			Zone:     zone,
		}

		switch {
		case serviceRef != "":
			svc, err := c.GetServiceByName(orgID(), pid, serviceRef)
			if err != nil {
				return err
			}
			body.ServiceID = &svc.ID

		case nodeRef != "":
			// Resolve node name → ID.
			nodes, err := c.ListNodes(orgID())
			if err != nil {
				return err
			}
			var nodeID string
			for _, n := range nodes {
				if n.ID == nodeRef || n.Name == nodeRef || n.TailscaleIP == nodeRef {
					nodeID = n.ID
					break
				}
			}
			if nodeID == "" {
				return fmt.Errorf("node %q not found", nodeRef)
			}
			body.NodeID = &nodeID
			body.Port = &port

		case targetIP != "":
			body.TargetIP = &targetIP
			body.TargetPort = &port
		}

		r, err := c.CreateRoute(orgID(), pid, body)
		if err != nil {
			return err
		}
		fmt.Printf("✔  Route created: %s → %s:%d (%s)\n", r.Hostname, r.TargetIP, r.TargetPort, r.ID)
		return nil
	},
}

var routeDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a route",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fmt.Printf("Delete route %q? [y/N]: ", args[0])
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		c := apiClient()
		pid := resolveProjectID(routeProject)
		if err := c.DeleteRoute(orgID(), pid, args[0]); err != nil {
			return err
		}
		fmt.Printf("✔  Route %s deleted.\n", args[0])
		return nil
	},
}

func init() {
	routeCmd.PersistentFlags().StringVarP(&routeProject, "project", "p", "", "Project ID or slug")

	routeCreateCmd.Flags().String("hostname", "", "Full hostname for the route (required)")
	routeCreateCmd.Flags().String("service", "", "Meshploy service name or ID (target mode)")
	routeCreateCmd.Flags().String("node", "", "Node name, ID, or mesh IP (target mode)")
	routeCreateCmd.Flags().Int("port", 0, "Port on the node or IP target")
	routeCreateCmd.Flags().String("ip", "", "Raw mesh IP for direct routing (target mode)")
	routeCreateCmd.Flags().String("zone", "public", "Route zone: public | internal | preview")

	routeDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation")

	routeCmd.AddCommand(routeListCmd, routeCreateCmd, routeDeleteCmd)
	rootCmd.AddCommand(routeCmd)
}
