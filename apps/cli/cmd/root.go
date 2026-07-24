package cmd

import (
	"fmt"
	"os"

	"github.com/meshploy/apps/cli/client"
	"github.com/meshploy/apps/cli/internal/config"
	"github.com/spf13/cobra"
)

// resolveProject returns the project ID from the --project flag, the local
// .meshploy file, or exits with a helpful error.
func resolveProject(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	link, err := config.LoadProjectLink()
	if err == nil && link != nil && link.ProjectID != "" {
		return link.ProjectID
	}
	fmt.Fprintln(os.Stderr, "error: no project specified. Use --project <id|slug> or run: meshploy link <project>")
	os.Exit(1)
	return ""
}

var (
	cfgAPIURL string // --api-url flag override
	loadedCfg *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "meshploy",
	Short: "Meshploy CLI — manage your infrastructure from the terminal",
	Long: `meshploy is the command-line interface for Meshploy.

Authenticate once with:
  meshploy auth login --api-url https://meshploy.example.com

Then manage nodes, clusters, and more.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgAPIURL, "api-url", "", "Meshploy API base URL (overrides saved config)")
}

func initConfig() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not load config: %v\n", err)
		cfg = &config.Config{}
	}
	loadedCfg = cfg
	if cfgAPIURL != "" {
		loadedCfg.APIURL = cfgAPIURL
	}
}

// apiClient returns an authenticated client or exits with an error.
func apiClient() *client.Client {
	if loadedCfg.APIURL == "" {
		fmt.Fprintln(os.Stderr, "error: not logged in. Run: meshploy auth login --api-url <url>")
		os.Exit(1)
	}
	return client.New(loadedCfg.APIURL, loadedCfg.Token)
}

// orgID returns the saved org ID or exits with a helpful error.
func orgID() string {
	if loadedCfg.OrgID == "" {
		fmt.Fprintln(os.Stderr, "error: not logged in. Run: meshploy auth login --api-url <url>")
		os.Exit(1)
	}
	return loadedCfg.OrgID
}
