package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/meshploy/apps/cli/internal/mesh"
	"github.com/spf13/cobra"
)

// meshCmd is Meshploy's own Tailscale. `install` and `uninstall` manage it;
// anything else goes to the bundled tailscale, on the mesh daemon's socket:
// `meshploy mesh status`, `meshploy mesh ip -4`, `meshploy mesh up ...`.
// A Tailscale of the machine's own stays on `tailscale` and tailscale0.
var meshCmd = &cobra.Command{
	Use:                "mesh [install | uninstall | <tailscale command>...]",
	Short:              "Meshploy's own mesh: install it, or run a tailscale command against it",
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
			return cmd.Help()
		}
		switch args[0] {
		case "install":
			return meshInstall(cmd.OutOrStdout(), args[1:])
		case "uninstall":
			return meshUninstall(cmd.OutOrStdout())
		}
		if !mesh.Installed() {
			return errors.New("the mesh is not installed here: run `sudo meshploy mesh install`")
		}
		c := mesh.Command(args...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				os.Exit(exit.ExitCode())
			}
			return err
		}
		return nil
	},
}

// meshInstall puts the mesh binaries in place and starts the service. They
// come from the release this CLI is from, checked against its SHA256SUMS, or
// from a directory with --from.
func meshInstall(w io.Writer, args []string) error {
	from, pat := "", os.Getenv("GITHUB_PAT")
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--from":
			if i+1 < len(args) {
				from, i = args[i+1], i+1
			}
		case "--token":
			if i+1 < len(args) {
				pat, i = args[i+1], i+1
			}
		default:
			return fmt.Errorf("unknown flag %q; mesh install takes --from <dir> and --token <pat>", args[i])
		}
	}
	if os.Geteuid() != 0 {
		return errors.New("mesh install needs root: run it with sudo")
	}
	if from != "" {
		if err := mesh.CopyBinaries(from, runtime.GOARCH); err != nil {
			return fmt.Errorf("install the mesh binaries from %s: %w", from, err)
		}
	} else if err := meshDownload(w, pat); err != nil {
		return err
	}
	if err := mesh.WriteUnit(runQuiet); err != nil {
		return fmt.Errorf("start %s: %w", mesh.Unit, err)
	}
	fmt.Fprintf(w, "✔  Meshploy's mesh runs as %s on %s\n", mesh.Unit, mesh.Interface)
	return nil
}

// meshDownload fetches the mesh binaries from this CLI's own release.
func meshDownload(w io.Writer, pat string) error {
	release := "cli-latest"
	if Channel == "stable" {
		release = "v" + Version
	}
	assets, err := releaseAssets(pat, release)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(mesh.BinDir, 0o755); err != nil {
		return err
	}
	for bin, dest := range map[string]string{"tailscaled": mesh.Daemon, "tailscale": mesh.Client} {
		name := mesh.Asset(bin, runtime.GOARCH)
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
	}
	return nil
}

// meshUninstall stops the mesh and removes it, its state with it: a machine
// that leaves the mesh rejoins with a new key.
func meshUninstall(w io.Writer) error {
	if os.Geteuid() != 0 {
		return errors.New("mesh uninstall needs root: run it with sudo")
	}
	if mesh.Installed() {
		_ = mesh.Command("logout").Run()
	}
	_ = runQuiet("systemctl", "disable", "--now", mesh.Unit)
	for _, p := range []string{mesh.UnitPath, mesh.Daemon, mesh.Client} {
		_ = os.Remove(p)
	}
	_ = os.RemoveAll(mesh.StateDir)
	_ = runQuiet("systemctl", "daemon-reload")
	fmt.Fprintln(w, "✔  Meshploy's mesh removed")
	return nil
}

func runQuiet(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, out)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(meshCmd)
}
