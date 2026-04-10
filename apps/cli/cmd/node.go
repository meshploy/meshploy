package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

const (
	installScript   = "/opt/meshploy/install.sh"
	uninstallScript = "/opt/meshploy/uninstall.sh"
)

var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Manage mesh nodes",
}

// ── node list ─────────────────────────────────────────────────────────────────

var nodeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List nodes in the cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		nodes, err := c.ListNodes(orgID())
		if err != nil {
			return err
		}
		if len(nodes) == 0 {
			fmt.Println("No nodes found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tSTATUS\tROLE\tIP")
		for _, n := range nodes {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", n.ID, n.Name, n.Status, n.K3sRole, n.TailscaleIP)
		}
		return w.Flush()
	},
}

// ── node delete ───────────────────────────────────────────────────────────────

var nodeDeleteCmd = &cobra.Command{
	Use:   "delete <node-id>",
	Short: "Delete a node from the cluster and Meshploy",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		nodeID := args[0]
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fmt.Printf("Delete node %s? This removes it from Headscale, k3s, and the Meshploy DB. [y/N]: ", nodeID)
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		c := apiClient()
		if err := c.DeleteNode(orgID(), nodeID); err != nil {
			return err
		}
		fmt.Printf("✔  Node %s deleted.\n", nodeID)
		return nil
	},
}

// ── node status ───────────────────────────────────────────────────────────────

var nodeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show this node's local identity (/etc/meshploy/node.conf)",
	RunE: func(cmd *cobra.Command, args []string) error {
		const confFile = "/etc/meshploy/node.conf"
		data, err := os.ReadFile(confFile)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("Not registered as a worker node (/etc/meshploy/node.conf not found).")
				return nil
			}
			return err
		}
		fmt.Printf("Node identity (%s):\n%s\n", confFile, string(data))
		return nil
	},
}

// ── node install ──────────────────────────────────────────────────────────────

var nodeInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install and join this machine as a Meshploy node",
	Long: `Runs /opt/meshploy/install.sh interactively.

Requires root. The install script handles dependency installation,
k3s setup, Headscale registration, and saves /etc/meshploy/node.conf.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("node install requires root — re-run with sudo")
		}
		return runScript(installScript, nil)
	},
}

// ── node uninstall ────────────────────────────────────────────────────────────

var nodeUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall this node and deregister from the cluster",
	Long: `Runs /opt/meshploy/uninstall.sh interactively.

Deregisters the node from Meshploy (via API), removes k3s agent,
and disconnects from the Headscale mesh.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("node uninstall requires root — re-run with sudo")
		}
		var extraArgs []string
		if yes, _ := cmd.Flags().GetBool("yes"); yes {
			extraArgs = append(extraArgs, "--yes")
		}
		return runScript(uninstallScript, extraArgs)
	},
}

// ── node token ────────────────────────────────────────────────────────────────

var nodeTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Show or rotate the node registration token",
}

var nodeTokenGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Show the current registration token",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		token, err := c.GetRegistrationToken(orgID())
		if err != nil {
			return err
		}
		if token == "" {
			fmt.Println("No registration token found. Run: meshploy node token rotate")
			return nil
		}
		fmt.Println(token)
		return nil
	},
}

var nodeTokenRotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Generate a new registration token (invalidates the old one)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		token, err := c.RotateRegistrationToken(orgID())
		if err != nil {
			return err
		}
		fmt.Println(token)
		return nil
	},
}

func init() {
	nodeDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	nodeUninstallCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompts")

	nodeTokenCmd.AddCommand(nodeTokenGetCmd, nodeTokenRotateCmd)
	nodeCmd.AddCommand(nodeListCmd, nodeDeleteCmd, nodeStatusCmd, nodeInstallCmd, nodeUninstallCmd, nodeTokenCmd)
	rootCmd.AddCommand(nodeCmd)
}

// runScript execs a bash script, inheriting stdin/stdout/stderr so interactive
// prompts work correctly.
func runScript(scriptPath string, args []string) error {
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return fmt.Errorf("script not found: %s\n\nMeshploy must be installed at /opt/meshploy/", scriptPath)
	}
	cmdArgs := append([]string{scriptPath}, args...)
	c := exec.Command("bash", cmdArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
