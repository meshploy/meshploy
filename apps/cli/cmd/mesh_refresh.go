package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/meshploy/apps/cli/internal/mesh"
)

// Seams for tests: where this machine's mesh binaries are, whether this
// process may replace them, and how the mesh restarts on new ones.
var (
	meshBinaries = map[string]string{"tailscaled": mesh.Daemon, "tailscale": mesh.Client}
	meshAsRoot   = func() bool { return os.Geteuid() == 0 }
	meshRestart  = func() error { return mesh.WriteUnit(runQuiet) }
)

// cliRelease is the release this CLI came from: the mesh and the cluster agent
// it installs are that release's.
func cliRelease() string {
	if Channel == "stable" {
		return "v" + Version
	}
	return "cli-latest"
}

// meshRunsHere reports whether this machine has Meshploy's mesh installed.
func meshRunsHere() bool {
	for _, p := range meshBinaries {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

// refreshMesh brings Meshploy's mesh to the build release publishes, on a
// machine that runs it, and restarts it on the new binaries. Binaries that
// already match the release's SHA256SUMS are left alone, so an upgrade that
// carries the same Tailscale build costs no restart. A release without a
// checked mesh build leaves the mesh as it is: nothing could be verified.
func refreshMesh(w io.Writer, pat, release string, assets map[string]string) error {
	if !meshRunsHere() {
		return nil
	}
	sums := assets[checksumAsset]
	type stale struct{ url, want string }
	replace := map[string]stale{}
	for bin, dest := range meshBinaries {
		name := mesh.Asset(bin, runtime.GOARCH)
		url := assets[name]
		if url == "" || sums == "" {
			fmt.Fprintf(w, "Release %s publishes no checked mesh build: Meshploy's mesh stays as it is\n", release)
			return nil
		}
		want, err := expectedChecksum(pat, sums, name)
		if err != nil {
			return err
		}
		if got, _ := fileSHA256(dest); got != want {
			replace[dest] = stale{url, want}
		}
	}
	if len(replace) == 0 {
		fmt.Fprintln(w, "✔  Meshploy's mesh is current")
		return nil
	}
	for dest, s := range replace {
		if err := downloadReplace(pat, s.url, dest, s.want); err != nil {
			return fmt.Errorf("%s: %w", dest, err)
		}
	}
	if err := meshRestart(); err != nil {
		return fmt.Errorf("restart %s: %w", mesh.Unit, err)
	}
	fmt.Fprintf(w, "✔  Meshploy's mesh updated to release %s's build and restarted\n", release)
	return nil
}

// upgradeMesh is refreshMesh from this CLI's own release, for server-upgrade:
// the new CLI runs it, so the mesh follows a release even when the CLI that
// downloaded this one did not know to.
func upgradeMesh(o serverUpgradeOptions) error {
	if o.from != "" || !meshRunsHere() {
		return nil
	}
	release := cliRelease()
	assets, err := releaseAssets(o.pat, release)
	if err != nil {
		return err
	}
	return refreshMesh(os.Stdout, o.pat, release, assets)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
