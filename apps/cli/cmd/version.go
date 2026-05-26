package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X github.com/meshploy/apps/cli/cmd.Version=...".
// Falls back to "dev" for local builds.
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the meshploy CLI version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("meshploy", Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
