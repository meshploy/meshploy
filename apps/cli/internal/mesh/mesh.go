// Package mesh is Meshploy's own Tailscale: the patched tailscaled and
// tailscale the release publishes (deploy/tailscale), installed beside any
// Tailscale the machine already has and run as its own service on its own
// interface, state, socket and port.
//
// A machine's own Tailscale keeps tailscale0 and the `tailscale` command.
// Meshploy's is reached through `meshploy mesh`, which passes everything to
// the bundled tailscale with this socket.
package mesh

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// The layout. Fixed, so every script and service agrees without asking.
const (
	BinDir    = "/usr/local/lib/meshploy"
	Daemon    = BinDir + "/tailscaled"
	Client    = BinDir + "/tailscale"
	StateDir  = "/var/lib/meshploy/tailscale"
	SocketDir = "/run/meshploy-tailscale"
	Socket    = SocketDir + "/tailscaled.sock"
	Interface = "meshploy0"
	Port      = 41642
	Unit      = "meshploy-tailscaled.service"
	UnitPath  = "/etc/systemd/system/" + Unit
)

// Asset is the release asset for one binary on this architecture.
func Asset(binary, goarch string) string {
	return fmt.Sprintf("meshploy-%s-linux-%s", binary, goarch)
}

// UnitFile is the systemd unit that runs the mesh daemon.
//
// --no-logs-no-support keeps it from sending its logs to Tailscale's servers,
// which a stock tailscaled does by default: this mesh is not Tailscale's.
func UnitFile() string {
	args := strings.Join([]string{
		Daemon,
		"--state=" + StateDir + "/tailscaled.state",
		"--statedir=" + StateDir,
		"--socket=" + Socket,
		"--tun=" + Interface,
		"--port=" + strconv.Itoa(Port),
		"--no-logs-no-support",
	}, " ")
	return `[Unit]
Description=Meshploy mesh (Meshploy's own tailscaled, on ` + Interface + `)
Documentation=https://docs.meshploy.com
Wants=network-pre.target
After=network-pre.target NetworkManager.service systemd-resolved.service

[Service]
ExecStart=` + args + `
ExecStopPost=` + Daemon + ` --cleanup --statedir=` + StateDir + ` --socket=` + Socket + ` --tun=` + Interface + `
Restart=on-failure
RuntimeDirectory=meshploy-tailscale
RuntimeDirectoryMode=0755
Type=notify

[Install]
WantedBy=multi-user.target
`
}

// Installed reports whether the mesh binaries are in place.
func Installed() bool {
	for _, p := range []string{Daemon, Client} {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

// WriteUnit writes the service and starts it, replacing an older unit.
func WriteUnit(run func(name string, args ...string) error) error {
	if err := os.MkdirAll(StateDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(UnitPath, []byte(UnitFile()), 0o644); err != nil {
		return err
	}
	if err := run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := run("systemctl", "enable", Unit); err != nil {
		return err
	}
	return run("systemctl", "restart", Unit)
}

// CopyBinaries installs binaries from a directory holding the release assets
// for this architecture, for a machine that fetched them some other way.
func CopyBinaries(dir, goarch string) error {
	if err := os.MkdirAll(BinDir, 0o755); err != nil {
		return err
	}
	for bin, dest := range map[string]string{"tailscaled": Daemon, "tailscale": Client} {
		b, err := os.ReadFile(filepath.Join(dir, Asset(bin, goarch)))
		if err != nil {
			return err
		}
		tmp := dest + ".new"
		if err := os.WriteFile(tmp, b, 0o755); err != nil {
			return err
		}
		if err := os.Rename(tmp, dest); err != nil {
			return err
		}
	}
	return nil
}

// Command is the bundled tailscale, talking to the mesh daemon.
func Command(args ...string) *exec.Cmd {
	return exec.Command(Client, append([]string{"--socket=" + Socket}, args...)...)
}

// IP is this machine's IPv4 address on the mesh, or empty before it joined.
func IP() string {
	out, err := Command("ip", "-4").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(bytes.SplitN(out, []byte("\n"), 2)[0]))
}
