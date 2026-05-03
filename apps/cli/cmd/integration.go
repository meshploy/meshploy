package cmd

import (
	"bufio"
	"fmt"
	"os"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var integrationCmd = &cobra.Command{
	Use:   "integration",
	Short: "Manage integrations (git, registry, storage)",
}

// ── git ───────────────────────────────────────────────────────────────────────

var integrationGitCmd = &cobra.Command{
	Use:   "git",
	Short: "Manage git source integrations",
}

var integrationGitListCmd = &cobra.Command{
	Use:   "list",
	Short: "List git integrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		items, err := apiClient().ListGitIntegrations(orgID())
		if err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Println("No git integrations found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tPROVIDER\tAUTH\tSTATUS")
		for _, g := range items {
			status := "connected"
			if !g.Connected {
				status = "pending"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", g.ID, g.Name, g.Provider, g.AuthMethod, status)
		}
		return w.Flush()
	},
}

var integrationGitAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a git integration (wizard)",
	RunE: func(cmd *cobra.Command, args []string) error {
		sc := bufio.NewScanner(os.Stdin)
		fmt.Println()
		provider := pickOne(sc, "Provider", []string{"github", "gitlab", "gitea"})
		switch provider {
		case "github":
			return wizardGitHub(sc)
		default:
			return wizardGitLabGitea(sc, provider)
		}
	},
}

var integrationGitDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a git integration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := apiClient().DeleteGitIntegration(orgID(), args[0]); err != nil {
			return err
		}
		fmt.Println("✔  Git integration deleted.")
		return nil
	},
}

// ── GitHub wizard ─────────────────────────────────────────────────────────────

func wizardGitHub(sc *bufio.Scanner) error {
	fmt.Println()
	fmt.Println("GitHub App setup — Meshploy registers a GitHub App on your account.")
	fmt.Println("A browser session on GitHub is required to authorize the app.")
	fmt.Println()

	githubOrg := promptDefault(sc, "GitHub organization (leave empty for personal account)", "")

	out, err := apiClient().InitGitHubIntegration(orgID(), githubOrg)
	if err != nil {
		return fmt.Errorf("create integration: %w", err)
	}

	fmt.Println()
	fmt.Printf("✔  Pending integration created (ID: %s)\n", out.Integration.ID)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Open the Meshploy web UI → Integrations")
	fmt.Println("  2. Click \"Install\" on the pending GitHub integration to register the app on GitHub")
	fmt.Println("  3. After installation the status will change to \"connected\"")
	return nil
}

// ── GitLab / Gitea wizard ─────────────────────────────────────────────────────

func wizardGitLabGitea(sc *bufio.Scanner, provider string) error {
	isGitea := provider == "gitea"
	providerLabel := map[string]string{"gitlab": "GitLab", "gitea": "Gitea"}[provider]

	fmt.Println()
	authMethod := pickOne(sc, "Auth method", []string{"pat", "oauth"})

	// Instance URL
	var baseURL string
	if isGitea {
		baseURL = prompt(sc, "Instance URL (e.g. https://gitea.example.com)")
		if baseURL == "" {
			return fmt.Errorf("instance URL is required for Gitea")
		}
	} else {
		baseURL = promptDefault(sc, "Instance URL (leave empty for gitlab.com)", "")
	}

	// Groups
	groupLabel := map[string]string{
		"gitlab": "Group name (optional, e.g. my-group/sub-group)",
		"gitea":  "Organization name (optional)",
	}[provider]
	groups := promptDefault(sc, groupLabel, "")

	// Label
	name := prompt(sc, "Label (e.g. my-"+provider+"-org)")
	if name == "" {
		return fmt.Errorf("label is required")
	}

	if authMethod == "pat" {
		fmt.Printf("Personal access token: ")
		tokenBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("read token: %w", err)
		}
		pat := string(tokenBytes)
		if pat == "" {
			return fmt.Errorf("token is required")
		}
		integration, err := apiClient().CreatePATIntegration(orgID(), provider, name, baseURL, groups, pat)
		if err != nil {
			return fmt.Errorf("create integration: %w", err)
		}
		fmt.Printf("\n✔  %s PAT integration %q connected (ID: %s)\n", providerLabel, integration.Name, integration.ID)
		return nil
	}

	// OAuth
	redirectURI := loadedCfg.APIURL + "/api/v1/" + provider + "/callback"
	fmt.Println()
	fmt.Printf("Redirect URI — paste this into your %s OAuth app settings:\n  %s\n\n", providerLabel, redirectURI)

	clientIDLabel := map[string]string{"gitlab": "Application ID", "gitea": "Client ID"}[provider]
	clientSecretLabel := map[string]string{"gitlab": "Application Secret", "gitea": "Client Secret"}[provider]

	clientID := prompt(sc, clientIDLabel)
	if clientID == "" {
		return fmt.Errorf("client ID is required")
	}

	fmt.Printf("%s: ", clientSecretLabel)
	secretBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("read secret: %w", err)
	}
	clientSecret := string(secretBytes)
	if clientSecret == "" {
		return fmt.Errorf("client secret is required")
	}

	authURL, err := apiClient().InitOAuthIntegration(orgID(), provider, name, baseURL, groups, redirectURI, clientID, clientSecret)
	if err != nil {
		return fmt.Errorf("create integration: %w", err)
	}

	fmt.Printf("\n✔  Integration created. Open this URL in your browser to authorize:\n\n  %s\n\n", authURL)
	fmt.Println("After authorizing, the integration will be marked as connected.")
	return nil
}

func init() {
	integrationGitCmd.AddCommand(integrationGitListCmd, integrationGitAddCmd, integrationGitDeleteCmd)
	integrationCmd.AddCommand(integrationGitCmd, integrationRegistryCmd, integrationStorageCmd)
	rootCmd.AddCommand(integrationCmd)
}
