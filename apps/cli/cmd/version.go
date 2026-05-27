package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X github.com/meshploy/apps/cli/cmd.Version=...".
// Falls back to "dev" for local builds.
var Version = "dev"
var Channel = "edge" // overridden to "stable" at build time for release builds

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the meshploy CLI version",
	Run: func(cmd *cobra.Command, args []string) {
		if Channel == "edge" {
			fmt.Printf("meshploy %s (edge)\n", Version)
		} else {
			fmt.Printf("meshploy %s\n", Version)
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
