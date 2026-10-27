package cmd

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"crypto/sha512"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var installSandboxCmd = &cobra.Command{
	Use:   "sandbox",
	Short: "Prepare this node to run sandboxed projects in gVisor",
	Long: `Installs gVisor, adds its runtime to the containerd that k3s runs, restarts
k3s, and labels this node meshploy.com/sandbox=true so sandboxed projects can
be scheduled here.

Restarting k3s keeps pods running; on the gateway the cluster API is away for
a few seconds. Run it again to upgrade gVisor.

Must be run as root (sudo meshploy install sandbox).`,
	RunE: runInstallSandbox,
}

const (
	gvisorReleases   = "https://storage.googleapis.com/gvisor/releases/release/latest"
	k3sAgentDir      = "/var/lib/rancher/k3s/agent"
	containerdDir    = k3sAgentDir + "/etc/containerd"
	sandboxConfName  = "meshploy-sandbox.toml"
	sandboxConfMark  = "# Written by meshploy install sandbox."
	sandboxNodeLabel = "meshploy.com/sandbox"
)

func runInstallSandbox(_ *cobra.Command, _ []string) error {
	if os.Getuid() != 0 {
		return fmt.Errorf("must be run as root - try: sudo meshploy install sandbox")
	}
	unit := k3sUnit()
	if unit == "" {
		return fmt.Errorf("k3s is not running here: only a node in the cluster can run sandboxed projects")
	}

	arch, ok := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	if !ok {
		return fmt.Errorf("gVisor has no release for %s", runtime.GOARCH)
	}
	fmt.Printf("Downloading gVisor (%s)…\n", arch)
	if err := installGVisor(gvisorReleases+"/"+arch, "/usr/local/bin"); err != nil {
		return err
	}
	out, err := exec.Command("/usr/local/bin/runsc", "--version").Output()
	if err != nil {
		return fmt.Errorf("runsc does not run on this machine: %w", err)
	}
	fmt.Printf("✔  %s\n", strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0])

	generated, err := os.ReadFile(filepath.Join(containerdDir, "config.toml"))
	if err != nil {
		return fmt.Errorf("read k3s's containerd config: %w", err)
	}
	path, body, err := containerdRuntimeFile(string(generated), containerdDir, readIfExists)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Printf("✔  gVisor's runtime added to containerd (%s)\n", path)

	fmt.Printf("Restarting %s…\n", unit)
	if err := sysCmd("systemctl", "restart", unit); err != nil {
		return fmt.Errorf("restart %s: %w", unit, err)
	}

	cert, err := os.ReadFile(filepath.Join(k3sAgentDir, "client-kubelet.crt"))
	if err != nil {
		return fmt.Errorf("read the kubelet's certificate: %w", err)
	}
	node, err := kubeletNodeName(cert)
	if err != nil {
		return err
	}
	// The kubelet may label its own node, so a worker needs no other
	// credential. It is ready a few seconds after the restart.
	label := []string{"kubectl", "--kubeconfig", filepath.Join(k3sAgentDir, "kubelet.kubeconfig"),
		"label", "node", node, sandboxNodeLabel + "=true", "--overwrite"}
	var lastErr error
	for range 30 {
		out, err := exec.Command("k3s", label...).CombinedOutput()
		if err == nil {
			fmt.Printf("✔  %s can run sandboxed projects\n", node)
			return nil
		}
		lastErr = fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("gVisor is installed, but %s could not be labelled %s=true: %w", node, sandboxNodeLabel, lastErr)
}

// k3sUnit is the k3s service running on this machine: k3s on the gateway,
// k3s-agent on a worker, or "" for neither.
func k3sUnit() string {
	for _, unit := range []string{"k3s", "k3s-agent"} {
		if exec.Command("systemctl", "is-active", "--quiet", unit).Run() == nil {
			return unit
		}
	}
	return ""
}

// installGVisor downloads gVisor's release tarball from base, checks it
// against its published sha512, and installs runsc, its containerd shim and
// the gvisor-bin folder runsc expects beside it into bin.
func installGVisor(base, bin string) error {
	tarball, err := httpGet(base + "/gvisor.tar.bz2")
	if err != nil {
		return err
	}
	sum, err := httpGet(base + "/gvisor.tar.bz2.sha512")
	if err != nil {
		return err
	}
	if err := checkSHA512(tarball, sum); err != nil {
		return err
	}
	tr := tar.NewReader(bzip2.NewReader(bytes.NewReader(tarball)))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read gVisor's tarball: %w", err)
		}
		name := filepath.Clean(h.Name)
		if !gvisorFile(name) {
			continue
		}
		dest := filepath.Join(bin, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			// Written beside and renamed over, so a runsc that is running
			// keeps its file.
			tmp := dest + ".new"
			f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err == nil {
				err = os.Rename(tmp, dest)
			}
			if err != nil {
				_ = os.Remove(tmp)
				return fmt.Errorf("install %s: %w", dest, err)
			}
		}
	}
}

// gvisorFile reports whether a tarball entry is one installGVisor installs.
// Anything else, a path out of the folder above all, is skipped.
func gvisorFile(name string) bool {
	return name == "runsc" || name == "containerd-shim-runsc-v1" ||
		name == "gvisor-bin" || strings.HasPrefix(name, "gvisor-bin/") && !strings.Contains(name, "..")
}

func checkSHA512(data, published []byte) error {
	fields := strings.Fields(string(published))
	if len(fields) == 0 {
		return fmt.Errorf("gVisor's published checksum is empty")
	}
	got := sha512.Sum512(data)
	if !strings.EqualFold(hex.EncodeToString(got[:]), fields[0]) {
		return fmt.Errorf("gVisor's tarball does not match its published sha512; nothing was installed")
	}
	return nil
}

func httpGet(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

var containerdImports = regexp.MustCompile(`(?m)^imports\s*=\s*\[\s*"([^"]+)/\*\.toml"`)

// containerdRuntimeFile is where gVisor's runtime goes in k3s's containerd
// configuration, given the config.toml k3s generated, and what to write
// there. A k3s whose config imports a drop-in folder gets a file of its own
// in it. An older one gets the template k3s renders its config from, built on
// k3s's own base, unless the operator already has a template, which is theirs
// to change.
func containerdRuntimeFile(generated, dir string, read func(string) ([]byte, bool)) (path, body string, err error) {
	v3 := regexp.MustCompile(`(?m)^version\s*=\s*3\b`).MatchString(generated)
	runtimeTable := `[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]`
	version := "version = 2"
	if v3 {
		runtimeTable = `[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.runsc]`
		version = "version = 3"
	}
	runtimeConf := runtimeTable + "\n  runtime_type = \"io.containerd.runsc.v1\"\n"

	if m := containerdImports.FindStringSubmatch(generated); m != nil {
		return filepath.Join(m[1], sandboxConfName), sandboxConfMark + "\n" + version + "\n\n" + runtimeConf, nil
	}

	template := filepath.Join(dir, "config.toml.tmpl")
	if v3 {
		template = filepath.Join(dir, "config-v3.toml.tmpl")
	}
	if have, ok := read(template); ok && !bytes.HasPrefix(have, []byte(sandboxConfMark)) {
		return "", "", fmt.Errorf("%s is already customised; add gVisor's runtime to it yourself:\n\n%s", template, runtimeConf)
	}
	return template, sandboxConfMark + "\n{{ template \"base\" . }}\n\n" + runtimeConf, nil
}

func readIfExists(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	return b, err == nil
}

// kubeletNodeName is the node a kubelet's client certificate speaks for: its
// common name is system:node:<name>.
func kubeletNodeName(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", fmt.Errorf("the kubelet's certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse the kubelet's certificate: %w", err)
	}
	name, ok := strings.CutPrefix(cert.Subject.CommonName, "system:node:")
	if !ok || name == "" {
		return "", fmt.Errorf("the kubelet's certificate names %q, not a node", cert.Subject.CommonName)
	}
	return name, nil
}

func init() {
	installCmd.AddCommand(installSandboxCmd)
}
