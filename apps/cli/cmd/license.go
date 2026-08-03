package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/meshploy/packages/client"
	"github.com/spf13/cobra"
)

// Licensing lives in the open-source CLI on purpose, mirroring the console's
// LicenseSection: these commands describe the *upgrade path*, not what the paid
// tier contains. They name no Enterprise feature, so nothing licensable is
// published under this repository's MIT licence.
//
// Both talk to the API as the logged-in user, unlike `server-upgrade`, which
// runs as root on the gateway. Do not sudo these — root has no saved
// credentials and the call would fail to authenticate.
var licenseCmd = &cobra.Command{
	Use:     "license",
	Aliases: []string{"licence"},
	Short:   "Inspect and activate this install's licence",
}

var licenseStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the licence status and entitlements of this install",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ent, err := apiClient().GetEntitlements()
		if err != nil {
			return err
		}
		printEntitlements(ent)
		return nil
	},
}

var licenseActivateCmd = &cobra.Command{
	Use:   "activate <token>",
	Short: "Install a licence token on this instance",
	Long: `Installs a licence token. The server verifies the signature, expiry, and
domain binding before storing it, so an invalid token is rejected immediately.

Only the Enterprise image can verify a licence — the Community image trusts no
signing key. On a Community install this reports what to run instead.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		token := strings.TrimSpace(args[0])
		c := apiClient()

		// Check first so the failure explains itself. Without this, a Community
		// install returns a bare verification error and the operator is left
		// thinking the token is bad when the image is the problem.
		if ent, err := c.GetEntitlements(); err == nil && !ent.CanActivate {
			fmt.Fprintln(os.Stderr, "This is the Community image. It cannot verify a licence, so")
			fmt.Fprintln(os.Stderr, "activation happens after switching to the Enterprise image.")
			fmt.Fprintln(os.Stderr, "\nRun on the gateway, then activate again:")
			fmt.Fprintln(os.Stderr, "\n  sudo meshploy server-upgrade --ee")
			return fmt.Errorf("licence cannot be activated on this image")
		}

		ent, err := c.ActivateLicense(token)
		if err != nil {
			return err
		}
		fmt.Println("✔  Licence activated")
		printEntitlements(ent)
		return nil
	},
}

func printEntitlements(e *client.Entitlements) {
	if !e.Licensed {
		fmt.Println("Edition:   Community")
		if !e.CanActivate {
			// The decisive detail: this build has no trust anchor, so no token
			// will ever work here regardless of what the customer holds. The
			// server reports the same thing in Problem when a licence row
			// survives an EE-to-CE rollback; printing both would just say it
			// twice, so the Problem line is reserved for anything else.
			fmt.Println("Licensing: unavailable — this image trusts no signing key")
			if e.Problem != "" && !strings.Contains(e.Problem, "signing key") {
				fmt.Printf("Problem:   %s\n", e.Problem)
			}
			fmt.Println("\nSwitch with: sudo meshploy server-upgrade --ee")
		} else {
			fmt.Println("Licensing: available — activate with: meshploy license activate <token>")
			if e.Problem != "" {
				fmt.Printf("Problem:   %s\n", e.Problem)
			}
		}
		return
	}

	fmt.Println("Edition:   Enterprise")
	if e.Customer != "" {
		fmt.Printf("Customer:  %s\n", e.Customer)
	}
	if e.Tier != "" {
		fmt.Printf("Tier:      %s\n", e.Tier)
	}
	if len(e.Features) > 0 {
		fmt.Printf("Features:  %s\n", strings.Join(e.Features, ", "))
	}
	if e.ExpiresAt != "" {
		expiry := e.ExpiresAt
		if e.Expired {
			expiry += "  (EXPIRED)"
		}
		fmt.Printf("Expires:   %s\n", expiry)
	}
	if e.NodeLimit > 0 {
		nodes := fmt.Sprintf("%d of %d", e.NodeCount, e.NodeLimit)
		if e.OverLimit {
			nodes += "  (OVER LIMIT)"
		}
		fmt.Printf("Nodes:     %s\n", nodes)
	}
	if e.RegistryScope != "" {
		fmt.Printf("Image:     %s\n", e.RegistryScope)
	}
	if e.Problem != "" {
		fmt.Printf("Problem:   %s\n", e.Problem)
	}
}

func init() {
	licenseCmd.AddCommand(licenseStatusCmd, licenseActivateCmd)
	rootCmd.AddCommand(licenseCmd)
}
