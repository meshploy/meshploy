package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/meshploy/apps/cli/client"
	"github.com/spf13/cobra"
)

var (
	applyFile    string
	applyProject string
	applyName    string
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Deploy an app from a compose manifest (upsert a stack and reconcile it)",
	Long: `Apply a Docker Compose–style manifest (with x-meshploy extensions) to a project.

The manifest is upserted as a stack named --name (default: the manifest file's
base name) and reconciled into live services in a single call. Idempotent — run
it again to converge on the same spec.

Example:
  meshploy apply -f compose.yml --project my-project`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if applyFile == "" {
			return fmt.Errorf("-f/--file is required")
		}
		data, err := os.ReadFile(applyFile)
		if err != nil {
			return fmt.Errorf("read manifest: %w", err)
		}

		name := applyName
		if name == "" {
			base := filepath.Base(applyFile)
			name = strings.TrimSuffix(base, filepath.Ext(base))
		}

		c := apiClient()
		pid := resolveProjectID(applyProject)
		result, err := c.ApplyManifest(orgID(), pid, name, string(data))
		if err != nil {
			return err
		}

		fmt.Printf("Stack %q applied.\n", name)
		printApplyResult(result)
		return nil
	},
}

func printApplyResult(r *client.ApplyResult) {
	if len(r.Created) > 0 {
		fmt.Printf("  Created: %s\n", strings.Join(r.Created, ", "))
	}
	if len(r.Updated) > 0 {
		fmt.Printf("  Updated: %s\n", strings.Join(r.Updated, ", "))
	}
	if len(r.Deleted) > 0 {
		fmt.Printf("  Deleted: %s\n", strings.Join(r.Deleted, ", "))
	}
	if len(r.Errors) > 0 {
		fmt.Println("  Errors:")
		for _, e := range r.Errors {
			fmt.Printf("    - %s\n", e)
		}
	}
}

func init() {
	applyCmd.Flags().StringVarP(&applyFile, "file", "f", "", "Path to the compose manifest (required)")
	applyCmd.Flags().StringVar(&applyProject, "project", "", "Project name or ID")
	applyCmd.Flags().StringVar(&applyName, "name", "", "Stack name (default: manifest file base name)")
	rootCmd.AddCommand(applyCmd)
}
