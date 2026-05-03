package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/meshploy/apps/cli/internal/client"
	"github.com/meshploy/apps/cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to a Meshploy instance and save credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		apiURL, _ := cmd.Flags().GetString("api-url")
		if apiURL == "" {
			apiURL = loadedCfg.APIURL
		}
		if apiURL == "" {
			return fmt.Errorf("--api-url is required on first login")
		}
		apiURL = strings.TrimRight(apiURL, "/")

		email, _ := cmd.Flags().GetString("email")
		if email == "" {
			fmt.Print("Email: ")
			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			email = strings.TrimSpace(scanner.Text())
		}

		fmt.Print("Password: ")
		passBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}

		c := client.New(apiURL, "")
		result, err := c.Login(email, string(passBytes))
		if err != nil {
			return fmt.Errorf("login failed: %w", err)
		}

		token := result.Token
		if result.TOTPRequired {
			fmt.Print("Two-factor code: ")
			codeBytes, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("read code: %w", err)
			}
			token, err = c.CompleteTOTPLogin(result.MFAToken, strings.TrimSpace(string(codeBytes)))
			if err != nil {
				return fmt.Errorf("2FA verification failed: %w", err)
			}
		}

		// Resolve the single org for this CE install.
		authed := client.New(apiURL, token)
		orgs, err := authed.ListOrgs()
		if err != nil || len(orgs) == 0 {
			return fmt.Errorf("login succeeded but could not resolve org: %w", err)
		}

		if err := config.Save(&config.Config{
			APIURL: apiURL,
			Token:  token,
			OrgID:  orgs[0].ID,
		}); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		fmt.Printf("✔  Logged in as org %s. Config saved to ~/.meshploy/config.json\n", orgs[0].Slug)
		return nil
	},
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove saved credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Clear(); err != nil {
			return err
		}
		fmt.Println("✔  Logged out.")
		return nil
	},
}

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Print the saved API URL and authentication status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil || cfg.APIURL == "" {
			fmt.Println("Not logged in. Run: meshploy auth login --api-url <url>")
			return nil
		}
		fmt.Println("API URL:", cfg.APIURL)
		if cfg.Token != "" {
			preview := cfg.Token
			if len(preview) > 20 {
				preview = preview[:20] + "…"
			}
			fmt.Println("Token:  ", preview)
		}
		return nil
	},
}

func init() {
	loginCmd.Flags().String("api-url", "", "Meshploy API base URL")
	loginCmd.Flags().StringP("email", "e", "", "Email address")
	authCmd.AddCommand(loginCmd, logoutCmd, whoamiCmd)
	rootCmd.AddCommand(authCmd)
}
