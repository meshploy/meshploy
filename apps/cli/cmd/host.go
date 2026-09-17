package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/meshploy/packages/hostagent"
	"github.com/spf13/cobra"
)

// The host agent does on the gateway what the API, in its container, cannot:
// it looks at the host and reports. It listens on nothing. The API reads what
// it writes from a directory mounted read-only.
//
// It reports the host firewall, and starts the upgrades the console asks for
// (internal-docs/plans/host-agent.md, phases 1 and 2).
const hostUnit = "meshploy-host.service"

// Seams replaced by tests.
var (
	hostDir = hostagent.DefaultDir
	// hostExec runs a command and returns what it printed.
	hostExec = func(name string, args ...string) (string, error) {
		out, err := exec.Command(name, args...).CombinedOutput()
		return string(out), err
	}
	hostLookPath = exec.LookPath
	// hostSystemdRun starts a transient unit.
	hostSystemdRun = func(args ...string) (string, error) {
		out, err := exec.Command("systemd-run", args...).CombinedOutput()
		return string(out), err
	}
	// upgradeWatchInterval is how often the agent looks for a request.
	upgradeWatchInterval = 2 * time.Second
	// upgradeRelaunchAfter is how long a request that is still waiting after a
	// launch is left alone. The runner consumes a request as it starts, so one
	// still there means the run never started; retrying at once would loop.
	upgradeRelaunchAfter = 2 * time.Minute
)

var hostCmd = &cobra.Command{
	Use:   "host",
	Short: "The gateway's host agent: what the console needs from the host",
	Long: `The host agent runs on the gateway as a systemd service and reports what the
API cannot see from its container, starting with the host firewall, so the
console can say whether a published port is blocked.

  start    install the service and start it
  stop     stop the service
  status   whether it runs, and what it last reported
  serve    run the agent in the foreground (what the service runs)`,
}

var hostServeCmd = &cobra.Command{
	Use:    "serve",
	Short:  "Run the host agent in the foreground",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("host serve requires root: reading the firewall does")
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return hostServe(ctx, cmd.ErrOrStderr())
	},
}

var hostStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Install the host agent service and start it",
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("host start requires root; re-run with sudo")
		}
		return hostStart(cmd.OutOrStdout())
	},
}

var hostStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the host agent service",
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("host stop requires root; re-run with sudo")
		}
		if err := systemctl("disable", "--now", hostUnit); err != nil {
			return fmt.Errorf("disable %s: %w", hostUnit, err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "✔  Host agent stopped. The console will say the host could not be checked.")
		return nil
	},
}

var hostStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether the host agent runs, and what it last reported",
	RunE: func(cmd *cobra.Command, args []string) error {
		return printHostStatus(cmd.OutOrStdout(), time.Now())
	},
}

// ── serve ────────────────────────────────────────────────────────────────────

func hostServe(ctx context.Context, logw io.Writer) error {
	state := hostagent.StateDir(hostDir)
	if err := os.MkdirAll(state, 0755); err != nil {
		return err
	}
	agent := hostagent.Agent{Version: Version, StartedAt: time.Now().UTC(), Tasks: map[string]hostagent.Task{}}
	var mu sync.Mutex
	writeAgent := func() {
		mu.Lock()
		defer mu.Unlock()
		agent.HeartbeatAt = time.Now().UTC()
		if err := writeHostJSON(filepath.Join(state, hostagent.AgentFile), agent); err != nil {
			fmt.Fprintf(logw, "agent status: %v\n", err)
		}
	}
	setTask := func(name string, t hostagent.Task) {
		mu.Lock()
		agent.Tasks[name] = t
		mu.Unlock()
	}

	upgrades := &upgradeLauncher{}
	go func() {
		t := time.NewTicker(upgradeWatchInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if task, changed := upgrades.check(time.Now()); changed {
					if task.Error != "" {
						fmt.Fprintf(logw, "upgrades: %s\n", task.Error)
					}
					setTask("upgrades", task)
					writeAgent()
				}
			}
		}
	}()

	tick := func() {
		fw := collectFirewall()
		task := hostagent.Task{OK: fw.Error == "", Error: fw.Error, At: fw.CheckedAt}
		if err := writeHostJSON(filepath.Join(state, hostagent.FirewallFile), fw); err != nil {
			task = hostagent.Task{Error: err.Error(), At: time.Now().UTC()}
		}
		if task.Error != "" {
			fmt.Fprintf(logw, "firewall: %s\n", task.Error)
		}
		setTask("firewall", task)
		writeAgent()
	}

	tick()
	t := time.NewTicker(hostagent.FirewallInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			tick()
		}
	}
}

// upgradeLauncher starts the upgrade the console queued, as a transient unit
// the agent does not own: the run replaces this binary and restarts this
// service, and must outlive both.
type upgradeLauncher struct {
	launched   string // the request last launched, by modification time and size
	launchedAt time.Time
}

// check launches a waiting request. It reports a task result only when it did
// something, so an idle agent does not rewrite its status every two seconds.
func (l *upgradeLauncher) check(now time.Time) (hostagent.Task, bool) {
	if !fileExists(filepath.Join(upgradeStateDir(), upgradeEnabledFile)) {
		return hostagent.Task{}, false
	}
	// Lstat, not Stat: the API can write this directory and could plant a
	// symlink. The runner refuses one too; the agent does not act on it.
	info, err := os.Lstat(filepath.Join(upgradeInboxDir(), upgradeRequestFile))
	if err != nil || !info.Mode().IsRegular() {
		return hostagent.Task{}, false
	}
	switch systemctlState("is-active", upgradeServiceUnit) {
	case "active", "activating", "deactivating", "reloading":
		return hostagent.Task{}, false
	}
	key := fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size())
	if key == l.launched && now.Sub(l.launchedAt) < upgradeRelaunchAfter {
		return hostagent.Task{}, false
	}
	l.launched, l.launchedAt = key, now

	out, err := hostSystemdRun(upgradeRunArgs()...)
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return hostagent.Task{Error: "start the upgrade: " + clipLine(msg), At: now.UTC()}, true
	}
	return hostagent.Task{OK: true, At: now.UTC()}, true
}

// upgradeRunArgs are systemd-run's arguments for one upgrade. A oneshot unit
// is "activating" for the length of the run, which is what `updater status`
// and the stale-run check read.
func upgradeRunArgs() []string {
	return []string{
		"--unit=" + strings.TrimSuffix(upgradeServiceUnit, ".service"),
		"--collect",
		"--no-block",
		"--service-type=oneshot",
		"--description=Upgrade Meshploy (requested from the console)",
		// A hung image pull must not hold the upgrade lock forever.
		"--property=TimeoutStartSec=30min",
		// docker compose reads registry credentials from root's home, and a
		// system unit does not always have HOME set.
		"--setenv=HOME=/root",
		// Optional; only a private fork needs it, for GITHUB_PAT.
		"--property=EnvironmentFile=-/etc/meshploy/updater.env",
		updaterCLIPath, "updater", "run", "--queued",
	}
}

func writeHostJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'), 0644)
}

// collectFirewall asks whichever firewall manages the host. ufw first, as the
// installer sets it up; then firewalld; then the kernel's own INPUT policy.
func collectFirewall() hostagent.Firewall {
	now := time.Now().UTC()
	failed := func(tool, out string, err error) hostagent.Firewall {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return hostagent.Firewall{Tool: tool, Error: clipLine(msg), CheckedAt: now}
	}

	if _, err := hostLookPath("ufw"); err == nil {
		out, err := hostExec("ufw", "status", "verbose")
		if err != nil {
			return failed(hostagent.ToolUFW, out, err)
		}
		if fw := hostagent.ParseUFWStatus(out, resolveUFWApp); fw.Active {
			fw.CheckedAt = now
			return fw
		}
	}

	if _, err := hostLookPath("firewall-cmd"); err == nil {
		if out, err := hostExec("firewall-cmd", "--state"); err == nil && strings.TrimSpace(out) == "running" {
			zone, err := hostExec("firewall-cmd", "--get-default-zone")
			if err != nil {
				return failed(hostagent.ToolFirewalld, zone, err)
			}
			list, err := hostExec("firewall-cmd", "--zone="+strings.TrimSpace(zone), "--list-all")
			if err != nil {
				return failed(hostagent.ToolFirewalld, list, err)
			}
			fw := hostagent.ParseFirewalldZone(list, resolveFirewalldService)
			fw.CheckedAt = now
			return fw
		}
	}

	// Neither manages the host. A DROP policy, or a DROP or REJECT rule
	// directly in INPUT, is a firewall the agent does not interpret, so no port
	// is called open because of it.
	if _, err := hostLookPath("iptables"); err == nil {
		out, err := hostExec("iptables", "-S", "INPUT")
		if err != nil {
			return failed(hostagent.ToolIptables, out, err)
		}
		if iptablesFilters(out) {
			return hostagent.Firewall{Tool: hostagent.ToolIptables, Active: true, DefaultIncoming: "deny", Unparsed: true, CheckedAt: now}
		}
	}
	return hostagent.Firewall{Tool: hostagent.ToolNone, DefaultIncoming: "allow", CheckedAt: now}
}

// iptablesFilters reports whether `iptables -S INPUT` drops anything itself.
// Jumps into Kubernetes' chains filter pod traffic, not the host's ports.
func iptablesFilters(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "-P INPUT DROP" {
			return true
		}
		if strings.HasPrefix(line, "-A INPUT") && (strings.HasSuffix(line, "-j DROP") || strings.HasSuffix(line, "-j REJECT") || strings.Contains(line, "-j REJECT ")) {
			return true
		}
	}
	return false
}

// resolveUFWApp returns an application profile's ports ("80,443/tcp"), or ""
// when the profile lists more than one line of them or cannot be read.
func resolveUFWApp(app string) string {
	out, err := hostExec("ufw", "app", "info", app)
	if err != nil {
		return ""
	}
	var ports []string
	inPorts := false
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Ports:") {
			inPorts = true
			continue
		}
		if inPorts && trimmed != "" {
			ports = append(ports, trimmed)
		}
	}
	if len(ports) != 1 {
		return ""
	}
	return ports[0]
}

// resolveFirewalldService returns a service's ports ("22/tcp 80/tcp").
func resolveFirewalldService(svc string) string {
	out, err := hostExec("firewall-cmd", "--info-service="+svc)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), ":"); ok && key == "ports" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func clipLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// ── start / status ───────────────────────────────────────────────────────────

func hostUnitFile(cliPath string) string {
	return fmt.Sprintf(`[Unit]
Description=Meshploy host agent: reports the console reads from this gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s host serve
Restart=always
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
`, cliPath)
}

// ensureHostDirs creates the state directory docker-compose mounts into the
// API. Podman refuses to start a container whose bind-mount source is missing.
func ensureHostDirs() error {
	return os.MkdirAll(hostagent.StateDir(hostDir), 0755)
}

// hostStart installs the unit and starts it. Running it again rewrites the unit
// and restarts the agent, which is how the agent follows the CLI version.
func hostStart(out io.Writer) error {
	if _, err := os.Stat(filepath.Join(meshployInstDir, ".env")); err != nil {
		return fmt.Errorf("no Meshploy server found at %s: the host agent runs on the gateway", meshployInstDir)
	}
	if err := ensureHostDirs(); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(systemdUnitDir, hostUnit), []byte(hostUnitFile(updaterCLIPath)), 0644); err != nil {
		return fmt.Errorf("write %s: %w", hostUnit, err)
	}
	if err := systemctl("daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if err := systemctl("enable", hostUnit); err != nil {
		return fmt.Errorf("enable %s: %w", hostUnit, err)
	}
	if err := systemctl("restart", hostUnit); err != nil {
		return fmt.Errorf("start %s: %w", hostUnit, err)
	}
	fmt.Fprintln(out, "✔  Host agent on: the console can see whether this gateway's firewall blocks a port.")
	fmt.Fprintln(out, "   Check on it with:  meshploy host status")
	return nil
}

func printHostStatus(out io.Writer, now time.Time) error {
	fmt.Fprintf(out, "Service:   %s\n", orDash(systemctlState("is-active", hostUnit)))
	agent, fw, err := hostagent.ReadState(hostDir)
	if err != nil {
		return err
	}
	if agent == nil {
		fmt.Fprintln(out, "Reports:   none yet (start it with: sudo meshploy host start)")
		return nil
	}
	fmt.Fprintf(out, "Agent:     %s, started %s, last report %s ago\n",
		agent.Version, agent.StartedAt.Format(time.RFC3339), now.Sub(agent.HeartbeatAt).Round(time.Second))
	if fw != nil {
		switch {
		case fw.Error != "":
			fmt.Fprintf(out, "Firewall:  %s, could not be read: %s\n", fw.Tool, fw.Error)
		case !fw.Active:
			fmt.Fprintln(out, "Firewall:  none active on the host")
		default:
			fmt.Fprintf(out, "Firewall:  %s, default incoming %s, %d rules\n", fw.Tool, fw.DefaultIncoming, len(fw.Rules))
		}
	}
	return nil
}

func init() {
	hostCmd.AddCommand(hostServeCmd, hostStartCmd, hostStopCmd, hostStatusCmd)
	rootCmd.AddCommand(hostCmd)
}
