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

	// Idempotent: skip if already running.
	if out, err := exec.Command("systemctl", "is-active", "node_exporter").Output(); err == nil {
		if strings.TrimSpace(string(out)) == "active" {
			fmt.Println("✔  node_exporter is already running")
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

	meshIP, err := getMeshIP()
	if err != nil {
		return fmt.Errorf("could not get mesh IP (is Tailscale running?): %w", err)
	}
	listenAddr := meshIP + ":9100"

	unit := fmt.Sprintf(`[Unit]
Description=Prometheus node_exporter
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/node_exporter --web.listen-address=%s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`, listenAddr)

	if err := os.WriteFile("/etc/systemd/system/node_exporter.service", []byte(unit), 0644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}
	if err := sysCmd("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := sysCmd("systemctl", "enable", "--now", "node_exporter"); err != nil {
		return fmt.Errorf("start node_exporter: %w", err)
	}

	fmt.Printf("✔  node_exporter installed and running on %s\n", listenAddr)
	return nil
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
