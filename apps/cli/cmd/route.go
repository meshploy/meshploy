package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/meshploy/packages/client"
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
		fmt.Fprintln(w, "ID\tHOSTNAME\tSTATE\tOWNERSHIP\tTARGET\tZONE")
		unproved := 0
		for _, r := range routes {
			if r.NeedsOwnershipProof() {
				unproved++
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s:%d\t%s\n", r.ID, r.Hostname, publishState(r.Published), ownershipState(r), r.TargetIP, r.TargetPort, r.Zone)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		if unproved > 0 {
			fmt.Printf("\n%d custom hostname(s) get no certificate until proved: meshploy route verify <id>\n", unproved)
		}
		return nil
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
		if r.NeedsOwnershipProof() {
			printOwnershipProof(*r, c.GatewayPublicIP(orgID()))
		}
		return nil
	},
}

// A route created from here names its whole hostname, so it is always a
// custom hostname, proved on its own. Until it is, no certificate is issued and
// the route fails TLS even once its DNS points here - which, printed nowhere,
// looks like a broken gateway.
var routeVerifyCmd = &cobra.Command{
	Use:   "verify <id|hostname>",
	Short: "Prove you own a custom hostname, so it gets a certificate",
	Long: `Looks for the TXT record that proves you own a route's custom hostname.

A route made with --hostname is a custom hostname: it is not a subdomain of a
base domain already proved for the whole zone, so it is proved on its own. Until
this finds the record, no certificate is issued for it and requests fail the TLS
handshake, even once its DNS points at the gateway.

Run it once the record is published. DNS can take a few minutes to propagate;
running it again is harmless.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(routeProject)
		route, err := c.FindRoute(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		if !route.IsCustomHostname() {
			fmt.Printf("✔  %s is on a base domain, which is already proved. Nothing to do.\n", route.Hostname)
			return nil
		}
		if route.CustomDomainVerified {
			fmt.Printf("✔  %s is already verified.\n", route.Hostname)
			return nil
		}
		verified, err := c.VerifyRouteHostname(orgID(), pid, route.ID)
		if err != nil {
			// The check can issue a token the route never had, so read it back
			// before printing the record to add.
			if fresh, ferr := c.FindRoute(orgID(), pid, route.ID); ferr == nil {
				route = fresh
			}
			printOwnershipProof(*route, c.GatewayPublicIP(orgID()))
			return fmt.Errorf("%w - DNS can take a few minutes to propagate; run this again once the record is published", err)
		}
		fmt.Printf("✔  %s is verified. Its certificate is issued on the first request.\n", verified.Hostname)
		return nil
	},
}

func ownershipState(r client.Route) string {
	switch {
	case !r.IsCustomHostname():
		return "-"
	case r.CustomDomainVerified:
		return "verified"
	default:
		return "unproved"
	}
}

// printOwnershipProof prints the two records a custom hostname needs.
func printOwnershipProof(r client.Route, publicIP string) {
	if publicIP == "" {
		publicIP = "<gateway public IP>"
	}
	fmt.Printf("\n!  %s gets no certificate until you prove you own it. Add these at its DNS provider:\n\n", r.Hostname)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "   NAME\tTYPE\tVALUE")
	if r.CustomDomainVerifyToken != "" {
		fmt.Fprintf(w, "   %s\tTXT\t%s\n", r.VerifyRecordName(), r.CustomDomainVerifyToken)
	}
	fmt.Fprintf(w, "   %s\tA\t%s\n", r.Hostname, publicIP)
	_ = w.Flush()
	if r.CustomDomainVerifyToken == "" {
		fmt.Println("\n   This route predates verification records; the next verify issues one.")
	}
	fmt.Printf("\n   Then: meshploy route verify %s\n", r.ID)
}

func publishState(published bool) string {
	if published {
		return "published"
	}
	return "paused"
}

var routePublishCmd = &cobra.Command{
	Use:   "publish <id>",
	Short: "Serve a paused route again",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := apiClient().PublishRoute(orgID(), resolveProjectID(routeProject), args[0])
		if err != nil {
			return err
		}
		fmt.Printf("✔  %s is published. The proxy serves it within 30 seconds.\n", r.Hostname)
		return nil
	},
}

var routePauseCmd = &cobra.Command{
	Use:   "pause <id>",
	Short: "Stop serving a route and keep it: 404, no certificate",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fmt.Printf("Pause route %q? Visitors get a 404 until it is published again. [y/N]: ", args[0])
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		r, err := apiClient().PauseRoute(orgID(), resolveProjectID(routeProject), args[0])
		if err != nil {
			return err
		}
		fmt.Printf("✔  %s is paused. The proxy stops serving it within 30 seconds.\n", r.Hostname)
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
	routePauseCmd.Flags().BoolP("yes", "y", false, "Skip confirmation")

	routeCmd.AddCommand(routeListCmd, routeCreateCmd, routeVerifyCmd, routePublishCmd, routePauseCmd, routeDeleteCmd)
	rootCmd.AddCommand(routeCmd)
}
