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

	meshIP, err := getMeshIP()
	if err != nil {
		return fmt.Errorf("could not get mesh IP (is Tailscale running?): %w", err)
	}

	// On the gateway node the API runs in Docker and cannot reach the Tailscale
	// interface directly. Add a second listen address on the Docker bridge gateway
	// IP so the API container can scrape metrics.
	listenArgs := "--web.listen-address=" + meshIP + ":9100"
	if dockerBridgeIP := getDockerBridgeIP(); dockerBridgeIP != "" {
		listenArgs += " --web.listen-address=" + dockerBridgeIP + ":9100"
		fmt.Printf("Gateway detected — also listening on Docker bridge %s:9100\n", dockerBridgeIP)
	}

	unit := fmt.Sprintf(`[Unit]
Description=Prometheus node_exporter
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/node_exporter %s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`, listenArgs)

	if err := os.WriteFile("/etc/systemd/system/node_exporter.service", []byte(unit), 0644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}
	if err := sysCmd("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := sysCmd("systemctl", "enable", "--now", "node_exporter"); err != nil {
		return fmt.Errorf("start node_exporter: %w", err)
	}

	fmt.Printf("✔  node_exporter installed and running on %s:9100\n", meshIP)
	return nil
}

// getDockerBridgeIP returns the Docker bridge gateway IP if Docker is present,
// or empty string if not available.
func getDockerBridgeIP() string {
	out, err := exec.Command("docker", "network", "inspect", "bridge",
		"--format", "{{range .IPAM.Config}}{{.Gateway}}{{end}}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
