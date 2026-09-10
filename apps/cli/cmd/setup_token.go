package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// localAPI is where the API answers on the gateway itself: docker-compose
// publishes it on port 4000 on every interface.
const localAPI = "http://127.0.0.1:4000"

// setupTokenCmd manages the one-time token that gates the first registration.
//
// The first account created on a server becomes its owner, so that moment needs
// a credential only the operator has. install.sh prints one and the API refuses
// registration without it while the instance has no users.
//
// `show` requires root, the same as reading /opt/meshploy/.env where the token
// lives, so it exposes it to nobody who could not already read it. Without it
// the only way back to a lost token was `rotate`, which throws away one the
// operator may still have and needs an API restart before the new one works.
var setupTokenCmd = &cobra.Command{
	Use:   "setup-token",
	Short: "Manage the one-time token that gates first-run registration",
	Long: `The first account registered on a Meshploy server owns it.

install.sh prints a setup token once; registration is refused without it until
an owner exists. If it was lost before anyone registered, show prints it again.
If it may have leaked, rotate issues a new one.`,
}

var setupTokenShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the setup token, while the instance has no owner",
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("setup-token show requires root; re-run with sudo")
		}
		return printSetupToken(cmd.OutOrStdout(), readEnvVar("SETUP_TOKEN"), func() (bool, error) {
			return registrationOpen(localAPI)
		})
	},
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

// printSetupToken writes the token while it is still useful, and explains
// instead when it is not.
//
// Whether the instance is claimed comes from the API rather than being assumed:
// once an owner exists registration is closed and the token is never accepted
// again, so printing it would hand the operator a string that looks usable and
// is not. When the API cannot be reached the token is printed anyway, with that
// said, because an operator asking for it is usually still mid-setup.
func printSetupToken(w io.Writer, token string, open func() (bool, error)) error {
	if token == "" {
		fmt.Fprintln(w, "This install has no setup token configured.")
		fmt.Fprintln(w, "Registration is not gated by one, so the first account needs no token.")
		return nil
	}

	isOpen, err := open()
	switch {
	case err != nil:
		fmt.Fprintf(w, "\n  Setup token:  %s\n\n", token)
		fmt.Fprintf(w, "  Could not ask the API whether the instance already has an owner (%v).\n", err)
		fmt.Fprintln(w, "  If it does, registration is closed and this token is no longer accepted.")
	case !isOpen:
		fmt.Fprintln(w, "This instance already has an owner, so registration is closed and the")
		fmt.Fprintln(w, "setup token is no longer accepted. Sign in with the owner account instead.")
	default:
		fmt.Fprintf(w, "\n  Setup token:  %s\n\n", token)
		fmt.Fprintln(w, "  Enter it on the console's registration page to create the first account.")
		fmt.Fprintln(w, "  That account will own this instance.")
	}
	return nil
}

// registrationOpen asks the API whether the instance still has no owner.
//
// A response without registration_open is an error, not "claimed": treating it
// as claimed would withhold the token from exactly the operator who needs it.
func registrationOpen(baseURL string) (bool, error) {
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(baseURL + "/api/v1/auth/status")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("auth status returned %d", resp.StatusCode)
	}
	var st struct {
		RegistrationOpen *bool `json:"registration_open"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return false, fmt.Errorf("decode auth status: %w", err)
	}
	if st.RegistrationOpen == nil {
		return false, fmt.Errorf("auth status did not report registration_open")
	}
	return *st.RegistrationOpen, nil
}

func newSetupToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return "ms_" + hex.EncodeToString(b), nil
}

func init() {
	setupTokenCmd.AddCommand(setupTokenShowCmd)
	setupTokenCmd.AddCommand(setupTokenRotateCmd)
	rootCmd.AddCommand(setupTokenCmd)
}
