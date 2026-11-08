package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

// ClusterAgentBin is where the cluster agent is installed, on a server whose
// API runs somewhere else.
const ClusterAgentBin = "/usr/local/bin/meshploy-cluster-agent"

// clusterAgentAsset is the agent's binary in a CLI release.
func clusterAgentAsset(goarch string) string {
	return "meshploy-cluster-agent-linux-" + goarch
}

var installClusterAgentCmd = &cobra.Command{
	Use:   "cluster-agent",
	Short: "Install the cluster agent, which an API elsewhere reaches this cluster through",
	Long: `Puts meshploy-cluster-agent in ` + ClusterAgentBin + `, from the release this
CLI is from (checked against its SHA256SUMS), or from a file with --from.

The edge install runs it, then the agent's own init, register and serve.
Must be run as root.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		from, _ := cmd.Flags().GetString("from")
		pat, _ := cmd.Flags().GetString("token")
		if pat == "" {
			pat = os.Getenv("GITHUB_PAT")
		}
		return installClusterAgent(cmd.OutOrStdout(), from, pat, ClusterAgentBin)
	},
}

func installClusterAgent(w io.Writer, from, pat, dest string) error {
	if os.Geteuid() != 0 {
		return errors.New("installing the cluster agent needs root: run it with sudo")
	}
	if from != "" {
		if err := copyExecutable(from, dest); err != nil {
			return fmt.Errorf("install the cluster agent from %s: %w", from, err)
		}
		fmt.Fprintf(w, "✔  The cluster agent is at %s\n", dest)
		return nil
	}
	release := "cli-latest"
	if Channel == "stable" {
		release = "v" + Version
	}
	assets, err := releaseAssets(pat, release)
	if err != nil {
		return err
	}
	name := clusterAgentAsset(runtime.GOARCH)
	url := assets[name]
	if url == "" {
		return fmt.Errorf("release %s has no %s", release, name)
	}
	want := ""
	if sums := assets[checksumAsset]; sums != "" {
		if want, err = expectedChecksum(pat, sums, name); err != nil {
			return err
		}
	}
	fmt.Fprintf(w, "Downloading %s…\n", name)
	if err := downloadReplace(pat, url, dest, want); err != nil {
		return err
	}
	fmt.Fprintf(w, "✔  The cluster agent is at %s\n", dest)
	return nil
}

// copyExecutable puts src at dest as an executable, replacing it whole.
func copyExecutable(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".meshploy-install-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

func init() {
	installClusterAgentCmd.Flags().String("from", "", "install this file instead of downloading the release's")
	installClusterAgentCmd.Flags().String("token", "", "GitHub personal access token (or set GITHUB_PAT)")
	installCmd.AddCommand(installClusterAgentCmd)
}
