package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// setupTokenCmd manages the one-time token that gates the first registration.
//
// The first account created on a server becomes its owner, so that moment needs
// a credential only the operator has. install.sh prints one and the API refuses
// registration without it while the instance has no users.
//
// There is deliberately no "show" subcommand. A token readable from the box
// without authenticating is not much of a credential, and anyone who can read
// /opt/meshploy/.env can read it there anyway — making it a CLI feature would
// only make it look sanctioned.
var setupTokenCmd = &cobra.Command{
	Use:   "setup-token",
	Short: "Manage the one-time token that gates first-run registration",
	Long: `The first account registered on a Meshploy server owns it.

install.sh prints a setup token once; registration is refused without it until
an owner exists. If it was lost before anyone registered, rotate it here to
issue a new one.`,
}

var setupTokenRotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Issue a new setup token, invalidating the previous one",
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("setup-token rotate requires root — re-run with sudo")
		}
		if readEnvVar("SETUP_TOKEN") == "" {
			// Distinguish "never had one" from "lost it": writing a token into
			// an install whose API was not started with one would appear to work
			// and then not be enforced.
			fmt.Println("This install has no setup token configured.")
			fmt.Println("Registration is not gated by one, so there is nothing to rotate.")
			return nil
		}

		token, err := newSetupToken()
		if err != nil {
			return err
		}
		if err := setEnvVar("SETUP_TOKEN", token); err != nil {
			return fmt.Errorf("write .env: %w", err)
		}

		fmt.Printf("\n  New setup token:  %s\n\n", token)
		fmt.Println("  The previous token no longer works.")
		fmt.Println("  Restart the API for it to take effect:")
		fmt.Println("      cd /opt/meshploy && docker compose up -d api")
		return nil
	},
}

func newSetupToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return "ms_" + hex.EncodeToString(b), nil
}

func init() {
	setupTokenCmd.AddCommand(setupTokenRotateCmd)
	rootCmd.AddCommand(setupTokenCmd)
}
