package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/edgeconfig"
	"github.com/meshploy/packages/hostagent"
	"github.com/spf13/cobra"
)

// `meshploy domain apply` generates the gateway's Caddy and CoreDNS
// configuration from the edge snapshot and puts it in place.
//
// It replaces a scheme where that configuration was a set of files shipped in
// the deploy tarball and edited in place, which could express only one DNS mode
// for the whole server. Here the files are output: nothing is edited, so
// nothing needs restoring, and two base domains can be arranged differently.
//
// The snapshot is a file rather than a query because this runs where no
// database is reachable - at install, before Postgres exists, and during
// `server-upgrade`, while it is restarting.

var domainCmd = &cobra.Command{
	Use:   "domain",
	Short: "The base domains this gateway serves",
}

var (
	domainApplyFromEnv  bool
	domainApplyDryRun   bool
	domainApplyForce    bool
	domainApplyNoReload bool
)

var domainApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Generate and install this gateway's Caddy and CoreDNS configuration",
	Long: `Renders the Caddyfile, the CoreDNS Corefile and one set of zone files per
base domain from the edge snapshot, and installs them when they differ from
what is already there.

An apply that finds nothing to do does nothing: the edge is not reloaded, so
this is safe to run on every start and after every upgrade.

With --from-env the snapshot is built from DOMAIN, DNS_MODE, PUBLIC_IP and
MESH_IP in .env and written before rendering. That is the install-time path,
when there is one domain and no database to have recorded a second.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return errors.New("domain apply requires root - re-run with sudo")
		}
		return runDomainApply(cmd.OutOrStdout())
	},
}

// Seams replaced by tests.
var (
	validateCaddyfile = validateCaddyfileWithCaddy
	reloadEdge        = reloadEdgeWithCompose
)

func runDomainApply(out io.Writer) error {
	snap, err := loadEdgeSnapshot()
	if err != nil {
		return err
	}
	return applyEdgeSnapshot(out, *snap, domainApplyDryRun, !domainApplyNoReload)
}

// applyEdgeSnapshot renders a snapshot, installs what differs and puts it in
// service. The one path: the command, `server-upgrade` and the host agent all
// come through here, so a gateway's edge is only ever changed one way.
func applyEdgeSnapshot(out io.Writer, snap hostagent.EdgeSnapshot, dryRun, reload bool) error {
	_, err := applyEdgeSnapshotChanges(out, snap, dryRun, reload)
	return err
}

// applyEdgeSnapshotChanges is applyEdgeSnapshot, also returning what it wrote,
// for a caller that restarts the services itself and needs to know which.
func applyEdgeSnapshotChanges(out io.Writer, snap hostagent.EdgeSnapshot, dryRun, reload bool) ([]edgeconfig.Change, error) {
	files, err := edgeconfig.Render(snap)
	if err != nil {
		return nil, err
	}
	changes, err := edgeconfig.Plan(files, meshployInstDir)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 && !domainApplyForce {
		fmt.Fprintln(out, "Edge configuration is already what it should be. Nothing to do.")
		return nil, nil
	}
	for _, c := range changes {
		fmt.Fprintf(out, "  %s\n", c)
	}
	if dryRun {
		fmt.Fprintf(out, "\n%d change(s). Nothing written: --dry-run.\n", len(changes))
		return changes, nil
	}

	// Asked before anything is written, because a Caddyfile Caddy will not
	// accept takes the gateway off the internet, and the person who ran this is
	// most likely reaching it over that same gateway.
	if err := validateCaddyfile(files["caddy/Caddyfile"]); err != nil {
		return nil, fmt.Errorf("the generated Caddyfile was rejected, nothing was changed: %w", err)
	}
	fmt.Fprintln(out, "✔  Caddy accepts the generated configuration")

	if err := edgeconfig.Install(changes, meshployInstDir); err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "✔  %d file(s) written\n", len(changes))

	if !reload {
		fmt.Fprintln(out, "   Not reloading: the caller starts the edge itself.")
		return changes, nil
	}
	if err := reloadEdge(changes); err != nil {
		fmt.Fprintf(out, "!  the edge did not come up: %v\n", err)
		if rerr := edgeconfig.Restore(meshployInstDir); rerr != nil {
			return nil, fmt.Errorf("the edge did not come up (%w) and the previous configuration could not be put back: %v", err, rerr)
		}
		// The same services the failed change reached, back on the old files.
		if rerr := reloadEdge(changes); rerr != nil {
			return nil, fmt.Errorf("the edge did not come up (%w); the previous configuration is back on disk but did not start either: %v", err, rerr)
		}
		return nil, fmt.Errorf("the edge did not come up, so the previous configuration was put back: %w", err)
	}
	fmt.Fprintln(out, "✔  Edge reloaded")
	return changes, nil
}

// applyEdgeConfigFromEnv renders and installs without touching the edge, for a
// caller that restarts it itself. `server-upgrade` uses it: the edge
// configuration ships as code in this binary rather than as a file in the
// tarball, so a template change in a release reaches a server only by being
// rendered on upgrade.
//
// It renders from the installed snapshot when there is one, and seeds from .env
// only when there is not. Once the console has moved the primary or switched a
// domain's DNS mode, .env is stale - it records what the installer was told -
// and merging it on every upgrade would quietly undo those choices.
func applyEdgeConfigFromEnv(out io.Writer) ([]edgeconfig.Change, error) {
	prevFromEnv, prevNoReload, prevDry, prevForce := domainApplyFromEnv, domainApplyNoReload, domainApplyDryRun, domainApplyForce
	_, statErr := hostagent.ReadEdgeSnapshot(hostDir)
	domainApplyFromEnv, domainApplyNoReload, domainApplyDryRun, domainApplyForce = statErr != nil, true, false, false
	defer func() {
		domainApplyFromEnv, domainApplyNoReload, domainApplyDryRun, domainApplyForce = prevFromEnv, prevNoReload, prevDry, prevForce
	}()
	snap, err := loadEdgeSnapshot()
	if err != nil {
		return nil, err
	}
	return applyEdgeSnapshotChanges(out, *snap, false, false)
}

// loadEdgeSnapshot decides what to render from.
//
// Without --from-env the snapshot is taken as written: that is the path the API
// will use, where the set of domains is the database's to decide.
//
// With --from-env, .env's DOMAIN and DNS_MODE are merged in as the primary and
// the result is saved. Merged, not written over: an installer re-run on a
// gateway that has since added base domains must not silently stop serving
// them. .env is authoritative for which domain is primary, because it is the
// name the installer wrote and the one Headscale and the console are reached
// on; it says nothing about the others, so they are kept.
func loadEdgeSnapshot() (*hostagent.EdgeSnapshot, error) {
	existing, err := hostagent.ReadEdgeSnapshot(hostDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if !domainApplyFromEnv {
		if existing == nil {
			return nil, fmt.Errorf("no edge snapshot at %s - run with --from-env to write the first one from .env",
				filepath.Join(hostDir, hostagent.EdgeSnapshotFile))
		}
		return existing, nil
	}
	snap, err := mergeEnvIntoSnapshot(existing)
	if err != nil {
		return nil, err
	}
	if err := hostagent.WriteEdgeSnapshot(hostDir, *snap); err != nil {
		return nil, fmt.Errorf("write the edge snapshot: %w", err)
	}
	return snap, nil
}

// mergeEnvIntoSnapshot makes .env's domain the primary, keeping every other
// base domain the snapshot already had.
func mergeEnvIntoSnapshot(existing *hostagent.EdgeSnapshot) (*hostagent.EdgeSnapshot, error) {
	name := strings.TrimSpace(readEnvVar("DOMAIN"))
	if name == "" {
		return nil, fmt.Errorf("no DOMAIN in %s/.env", meshployInstDir)
	}
	mode := strings.TrimSpace(readEnvVar("DNS_MODE"))
	if mode == "" {
		// An install that recorded no mode predates the choice, and delegation
		// is what those gateways are running.
		mode = hostagent.EdgeDNSDelegation
	}
	meshIP := strings.TrimSpace(readEnvVar("MESH_IP"))
	if meshIP == "" {
		meshIP = "100.64.0.1"
	}

	publicIP := strings.TrimSpace(readEnvVar("PUBLIC_IP"))

	// A domain the snapshot already knows is not moved to primary. The console
	// may have moved the primary since the install, and .env - which records
	// only what the installer was told - would otherwise move it back. The
	// installer's choice of DNS mode for that domain still applies: re-running
	// it with a new mode is how a mode is changed from the terminal.
	if existing != nil {
		for i := range existing.Domains {
			if existing.Domains[i].BaseDomain != name {
				continue
			}
			snap := *existing
			snap.Domains = append([]hostagent.EdgeDomain(nil), existing.Domains...)
			snap.Domains[i].DNSMode = mode
			if publicIP != "" {
				snap.PublicIP = publicIP
			}
			snap.MeshIP = meshIP
			if snap.MeshDomain == "" {
				snap.MeshDomain = existing.MeshDomainOrDefault()
			}
			snap.WrittenAt = time.Now().UTC()
			if err := snap.Validate(); err != nil {
				return nil, fmt.Errorf("%s/.env does not describe a gateway that can be served: %w", meshployInstDir, err)
			}
			return &snap, nil
		}
	}

	snap := hostagent.EdgeSnapshot{
		PublicIP:  publicIP,
		MeshIP:    meshIP,
		WrittenAt: time.Now().UTC(),
		// The mesh domain nodes are named under is fixed at install. An
		// installer re-run with a new DOMAIN moves the primary, not the node
		// names.
		MeshDomain: "mesh." + name,
		Domains: []hostagent.EdgeDomain{{
			BaseDomain:        name,
			InternalSubdomain: "internal",
			DNSMode:           mode,
			Primary:           true,
		}},
	}
	if existing != nil {
		snap.MeshDomain = existing.MeshDomainOrDefault()
		for _, d := range existing.Domains {
			// The old primary keeps serving the platform's names: its console
			// may be the one in use, and workers joined through its headscale.
			if d.Primary {
				d.ServesPlatform = true
			}
			d.Primary = false
			snap.Domains = append(snap.Domains, d)
		}
	}
	if err := snap.Validate(); err != nil {
		return nil, fmt.Errorf("%s/.env does not describe a gateway that can be served: %w", meshployInstDir, err)
	}
	return &snap, nil
}

// validateCaddyfileWithCaddy asks Caddy itself whether it would accept the
// configuration, in a throwaway container from the image this gateway runs.
//
// Staged beside the real Caddyfile rather than somewhere temporary, because
// `import conf.d/*.caddy` resolves relative to the config file: validating
// anywhere else would not read what the operator added, and would pass a
// configuration that then fails on reload.
func validateCaddyfileWithCaddy(rendered string) error {
	staged := filepath.Join(meshployInstDir, filepath.FromSlash(edgeconfig.StagedCaddyfile))
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(staged, []byte(rendered), 0o644); err != nil {
		return err
	}
	defer os.Remove(staged)

	runtime := detectContainerRuntime()
	channel := readEnvVar("MESHPLOY_CHANNEL")
	if channel == "" {
		channel = "latest"
	}
	c := exec.Command(runtime, "run", "--rm",
		"-v", filepath.Join(meshployInstDir, "caddy")+":/w:ro,z",
		"ghcr.io/meshploy/caddy:"+channel,
		"caddy", "validate", "--config", "/w/"+filepath.Base(staged), "--adapter", "caddyfile")
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, lastLines(string(out), 12))
	}
	return nil
}

// reloadEdgeWithCompose puts the new configuration into service, touching only
// what the changes reach.
//
// Caddy is reloaded rather than restarted, so connections are not dropped and
// no certificate is re-fetched. CoreDNS re-reads its zone files on a timer by
// itself, but not the Corefile, so a changed zone set needs the container back.
// Headscale reads its configuration only at start; restarting it pauses the
// control plane for a moment, while tunnels already up keep carrying traffic.
func reloadEdgeWithCompose(changes []edgeconfig.Change) error {
	var caddy, coredns, headscale bool
	for _, c := range changes {
		switch {
		case strings.HasPrefix(c.Path, "caddy/"):
			caddy = true
		case strings.HasPrefix(c.Path, "coredns/"):
			coredns = true
		case c.Path == edgeconfig.HeadscaleConfig:
			headscale = true
		}
	}
	runtime := detectContainerRuntime()
	if coredns {
		if err := composeRun(runtime, "restart", "coredns"); err != nil {
			return fmt.Errorf("restart coredns: %w", err)
		}
	}
	if headscale {
		if err := composeRun(runtime, "restart", "headscale"); err != nil {
			return fmt.Errorf("restart headscale: %w", err)
		}
	}
	if caddy {
		if err := composeRun(runtime, "exec", "-T", "caddy",
			"caddy", "reload", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"); err != nil {
			return fmt.Errorf("reload caddy: %w", err)
		}
	}
	return nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func init() {
	domainApplyCmd.Flags().BoolVar(&domainApplyFromEnv, "from-env", false,
		"Build the snapshot from DOMAIN, DNS_MODE, PUBLIC_IP and MESH_IP in .env and write it before rendering")
	domainApplyCmd.Flags().BoolVar(&domainApplyDryRun, "dry-run", false, "Show what would change and write nothing")
	domainApplyCmd.Flags().BoolVar(&domainApplyForce, "force", false, "Write and reload even when nothing differs")
	domainApplyCmd.Flags().BoolVar(&domainApplyNoReload, "no-reload", false,
		"Write the files but leave the edge alone. For callers that start or restart it themselves, such as the installer")
	domainCmd.AddCommand(domainApplyCmd)
	rootCmd.AddCommand(domainCmd)
}
