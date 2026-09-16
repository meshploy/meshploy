package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/meshploy/packages/client"
	"github.com/spf13/cobra"
)

// A TCP route is a port on the gateway, not a hostname, so it sits under its
// own subcommand rather than as another target mode of `route create`: the two
// share no flags beyond --project.
var routeTCPCmd = &cobra.Command{
	Use:   "tcp",
	Short: "Publish a TCP port on the gateway",
	Long: `Publish a port on the gateway and forward it over the mesh, for what does
not speak HTTP: Postgres, Redis, SSH and the like. Anything HTTP takes a domain
route instead, which gives it a hostname and TLS.

  meshploy route tcp create --service primary-db --port 5432 --allow 203.0.113.7
  meshploy route tcp list
  meshploy route tcp delete <id>

The host firewall, and a cloud security group where there is one, must also
allow the port: the gateway listens on it, but neither of those knows that.`,
}

var routeTCPListCmd = &cobra.Command{
	Use:   "list",
	Short: "List published TCP ports",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(routeProject)
		routes, err := c.ListTCPRoutes(orgID(), pid)
		if err != nil {
			return err
		}
		if len(routes) == 0 {
			fmt.Println("No TCP ports published.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tPORT\tTARGET\tSTATE\tALLOWED FROM")
		for _, r := range routes {
			allowed := "anyone"
			if len(r.AllowedCIDRs) > 0 {
				allowed = strings.Join(r.AllowedCIDRs, ",")
			}
			state := r.Status
			if r.Status == "failed" && r.LastError != "" {
				state = "failed: " + r.LastError
			}
			fmt.Fprintf(w, "%s\t:%d\t%s:%d\t%s\t%s\n", r.ID, r.GatewayPort, r.TargetIP, r.TargetPort, state, allowed)
		}
		return w.Flush()
	},
}

var routeTCPCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Publish a port on the gateway",
	RunE: func(cmd *cobra.Command, args []string) error {
		serviceRef, _ := cmd.Flags().GetString("service")
		nodeRef, _ := cmd.Flags().GetString("node")
		nodePort, _ := cmd.Flags().GetInt("node-port")
		port, _ := cmd.Flags().GetInt("port")
		allow, _ := cmd.Flags().GetStringSlice("allow")

		if serviceRef == "" && nodeRef == "" {
			return fmt.Errorf("--service or --node is required")
		}
		if port == 0 {
			return fmt.Errorf("--port is required: the port the gateway listens on")
		}

		c := apiClient()
		pid := resolveProjectID(routeProject)
		body := client.CreateTCPRouteBody{GatewayPort: port, AllowedCIDRs: allow}

		if serviceRef != "" {
			svc, err := c.GetServiceByName(orgID(), pid, serviceRef)
			if err != nil {
				return err
			}
			body.ServiceID = &svc.ID
		} else {
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
			if nodePort == 0 {
				return fmt.Errorf("--node-port is required with --node: the port on that node")
			}
			body.NodeID = &nodeID
			body.NodePort = &nodePort
		}

		r, err := c.CreateTCPRoute(orgID(), pid, body)
		if err != nil {
			return err
		}
		fmt.Printf("✔  Publishing :%d → %s:%d (%s)\n", r.GatewayPort, r.TargetIP, r.TargetPort, r.ID)
		if len(r.AllowedCIDRs) == 0 {
			fmt.Println("   Open to anyone who can reach the gateway. Use --allow to restrict it.")
		}
		fmt.Println("   The gateway opens the port within a minute; check with: meshploy route tcp list")
		return nil
	},
}

var routeTCPDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Stop publishing a port",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fmt.Printf("Stop publishing %q? [y/N]: ", args[0])
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		c := apiClient()
		pid := resolveProjectID(routeProject)
		if err := c.DeleteTCPRoute(orgID(), pid, args[0]); err != nil {
			return err
		}
		fmt.Printf("✔  %s is no longer published.\n", args[0])
		return nil
	},
}

func init() {
	routeTCPCreateCmd.Flags().String("service", "", "Service name or ID to forward to")
	routeTCPCreateCmd.Flags().String("node", "", "Node name, ID or mesh IP to forward to instead")
	routeTCPCreateCmd.Flags().Int("node-port", 0, "Port on that node (with --node)")
	routeTCPCreateCmd.Flags().Int("port", 0, "Port the gateway listens on (required)")
	routeTCPCreateCmd.Flags().StringSlice("allow", nil, "Addresses or ranges allowed to connect (repeatable). Empty means anyone")

	routeTCPDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation")

	routeTCPCmd.AddCommand(routeTCPListCmd, routeTCPCreateCmd, routeTCPDeleteCmd)
	routeCmd.AddCommand(routeTCPCmd)
}
