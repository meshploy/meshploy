package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/meshploy/packages/client"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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

		files, err := manifestFiles(applyFile, data)
		if err != nil {
			return err
		}

		c := apiClient()
		pid := resolveProjectID(applyProject)
		result, err := c.ApplyManifest(orgID(), pid, name, string(data), files)
		if err != nil {
			return err
		}

		fmt.Printf("Stack %q applied.\n", name)
		printApplyResult(result)
		return nil
	},
}

// manifestFiles reads what a manifest's configs: and secrets: name by file:,
// relative to the manifest, so they reach the server with it: the server has
// no access to this machine. A file that is missing stops the apply, as it
// stops docker compose.
func manifestFiles(manifestPath string, data []byte) (map[string]string, error) {
	type fileRef struct {
		File string `yaml:"file"`
	}
	var doc struct {
		Configs map[string]fileRef `yaml:"configs"`
		Secrets map[string]fileRef `yaml:"secrets"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil // the server reports a manifest it cannot read
	}
	var paths []string
	for _, refs := range []map[string]fileRef{doc.Configs, doc.Secrets} {
		for _, r := range refs {
			if r.File != "" {
				paths = append(paths, r.File)
			}
		}
	}
	files := map[string]string{}
	total := 0
	for _, p := range paths {
		if _, done := files[p]; done {
			continue
		}
		full := p
		if !filepath.IsAbs(p) {
			full = filepath.Join(filepath.Dir(manifestPath), p)
		}
		b, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		if len(b) > 1<<20 {
			return nil, fmt.Errorf("%s is larger than 1 MiB, the most a config file can hold", p)
		}
		if total += len(b); total > 6<<20 {
			return nil, fmt.Errorf("the manifest's files come to more than 6 MiB")
		}
		files[p] = string(b)
	}
	return files, nil
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
	if len(r.Warnings) > 0 {
		fmt.Println("  Warnings:")
		for _, w := range r.Warnings {
			fmt.Printf("    - %s\n", w)
		}
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
