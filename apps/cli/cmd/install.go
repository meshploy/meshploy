package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install <component>",
	Short: "Install optional Meshploy components on this node",
	Long: `Install optional components that extend Meshploy's capabilities on this node.

Available components:
  node-exporter    Prometheus node_exporter for live CPU/memory/disk/network metrics`,
}

var installNodeExporterCmd = &cobra.Command{
	Use:   "node-exporter",
	Short: "Install Prometheus node_exporter for live system metrics",
	Long: `Downloads and installs node_exporter as a systemd service bound to the
Tailscale mesh interface. Once installed, live metrics become available in
the Meshploy dashboard on the node detail page.

Must be run as root (sudo meshploy install node-exporter).`,
	RunE: runInstallNodeExporter,
}

const nodeExporterVersion = "1.8.2"

func runInstallNodeExporter(_ *cobra.Command, _ []string) error {
	if os.Getuid() != 0 {
		return fmt.Errorf("must be run as root — try: sudo meshploy install node-exporter")
	}

	// Idempotent: skip only if already running AND binary exists.
	// Re-run if the binary is missing (partial install) or if the user wants to update the config.
	alreadyRunning := false
	if out, err := exec.Command("systemctl", "is-active", "node_exporter").Output(); err == nil {
		alreadyRunning = strings.TrimSpace(string(out)) == "active"
	}
	if alreadyRunning {
		if _, err := os.Stat("/usr/local/bin/node_exporter"); err == nil {
			fmt.Println("✔  node_exporter is already running")
			fmt.Println("   To reconfigure (e.g. add Docker bridge listen), stop it first:")
			fmt.Println("   sudo systemctl stop node_exporter && sudo meshploy install node-exporter")
			return nil
		}
	}

	arch := runtime.GOARCH
	tarball := fmt.Sprintf("node_exporter-%s.linux-%s.tar.gz", nodeExporterVersion, arch)
	url := fmt.Sprintf("https://github.com/prometheus/node_exporter/releases/download/v%s/%s", nodeExporterVersion, tarball)
	tmpTar := "/tmp/" + tarball
	tmpDir := fmt.Sprintf("/tmp/node_exporter-%s.linux-%s", nodeExporterVersion, arch)

	fmt.Printf("Downloading node_exporter %s (%s)…\n", nodeExporterVersion, arch)
	if err := sysCmd("curl", "-fsSL", url, "-o", tmpTar); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	if err := sysCmd("tar", "xzf", tmpTar, "-C", "/tmp"); err != nil {
		return fmt.Errorf("extract failed: %w", err)
	}
	if err := sysCmd("install", "-m", "755", tmpDir+"/node_exporter", "/usr/local/bin/node_exporter"); err != nil {
		return fmt.Errorf("install binary: %w", err)
	}
	_ = os.Remove(tmpTar)
	_ = os.RemoveAll(tmpDir)

	// Verify Tailscale is running (mesh IP used only for the success message).
	meshIP, err := getMeshIP()
	if err != nil {
		return fmt.Errorf("could not get mesh IP (is Tailscale running?): %w", err)
	}

	unit := `[Unit]
Description=Prometheus node_exporter
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/node_exporter --web.listen-address=0.0.0.0:9100
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`

	if err := os.WriteFile("/etc/systemd/system/node_exporter.service", []byte(unit), 0644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}
	if err := sysCmd("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := sysCmd("systemctl", "enable", "--now", "node_exporter"); err != nil {
		return fmt.Errorf("start node_exporter: %w", err)
	}

	// Allow Docker/Podman compose bridge networks to reach port 9100 so the
	// API container can scrape metrics on gateway nodes.
	allowNodeExporterFromBridges()

	fmt.Printf("✔  node_exporter installed and running on %s:9100\n", meshIP)
	return nil
}

// allowNodeExporterFromBridges adds a firewall rule permitting container bridge
// networks (Docker: 172.16.0.0/12, Podman: 10.88.0.0/16) to reach port 9100.
// Uses UFW, firewalld, or raw iptables depending on what's available.
func allowNodeExporterFromBridges() {
	type rule struct{ cidr, comment string }
	rules := []rule{
		{"172.16.0.0/12", "node_exporter — Docker bridge"},
		{"10.88.0.0/16", "node_exporter — Podman bridge"},
	}

	// UFW
	if _, err := exec.LookPath("ufw"); err == nil {
		if out, _ := exec.Command("ufw", "status").Output(); strings.Contains(string(out), "Status: active") {
			if !strings.Contains(string(out), "9100") {
				for _, r := range rules {
					_ = sysCmd("ufw", "allow", "from", r.cidr, "to", "any", "port", "9100", "comment", r.comment)
				}
			}
			return
		}
	}

	// firewalld
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		if out, _ := exec.Command("firewall-cmd", "--state").Output(); strings.TrimSpace(string(out)) == "running" {
			if existing, _ := exec.Command("firewall-cmd", "--list-rich-rules").Output(); !strings.Contains(string(existing), "9100") {
				for _, r := range rules {
					_ = sysCmd("firewall-cmd", "--permanent", "--add-rich-rule",
						fmt.Sprintf(`rule family="ipv4" source address="%s" port port="9100" protocol="tcp" accept`, r.cidr))
				}
				_ = sysCmd("firewall-cmd", "--reload")
			}
			return
		}
	}

	// Raw iptables fallback
	for _, r := range rules {
		check := exec.Command("iptables", "-C", "INPUT", "-p", "tcp", "--dport", "9100", "-s", r.cidr, "-j", "ACCEPT")
		if check.Run() != nil {
			_ = sysCmd("iptables", "-I", "INPUT", "-p", "tcp", "--dport", "9100", "-s", r.cidr, "-j", "ACCEPT")
		}
	}
	if _, err := exec.LookPath("netfilter-persistent"); err == nil {
		_ = sysCmd("netfilter-persistent", "save")
	}
}

// getMeshIP returns the Tailscale IPv4 address assigned to this node.
func getMeshIP() (string, error) {
	out, err := exec.Command("tailscale", "ip", "-4").Output()
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "", fmt.Errorf("no IPv4 address returned")
	}
	return ip, nil
}

// sysCmd runs an external command with its output wired to stdout/stderr.
func sysCmd(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func init() {
	installCmd.AddCommand(installNodeExporterCmd)
	rootCmd.AddCommand(installCmd)
}
