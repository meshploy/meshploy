package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nodePortFixture points the drop-in at a temporary k3s directory and records
// restarts instead of performing them.
func nodePortFixture(t *testing.T, k3sExists bool) (path string, restarts *[]string) {
	t.Helper()
	origDropin, origRestart, origDir, origFlannel := nodePortDropin, systemctlRestart, meshployInstDir, flannelDropin
	t.Cleanup(func() {
		nodePortDropin, systemctlRestart, meshployInstDir, flannelDropin = origDropin, origRestart, origDir, origFlannel
	})

	root := t.TempDir()
	nodePortDropin = filepath.Join(root, "rancher", "k3s", "config.yaml.d", "20-nodeport-addresses.yaml")
	flannelDropin = filepath.Join(root, "rancher", "k3s", "config.yaml.d", "10-flannel-iface.yaml")
	if k3sExists {
		if err := os.MkdirAll(filepath.Join(root, "rancher", "k3s"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seen := []string{}
	systemctlRestart = func(unit string) error { seen = append(seen, unit); return nil }

	meshployInstDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(meshployInstDir, ".env"), []byte("DOMAIN=example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return nodePortDropin, &seen
}

// The upgrade is what brings an existing gateway in line: it writes the
// drop-in, records the ranges for the console, and restarts k3s once.
func TestRestrictNodePortsWritesTheDropIn(t *testing.T) {
	dropin, restarts := nodePortFixture(t, true)

	if err := restrictNodePorts(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, err := os.ReadFile(dropin)
	if err != nil {
		t.Fatalf("drop-in not written: %v", err)
	}
	if !strings.Contains(string(body), "nodeport-addresses="+nodePortCIDRs) {
		t.Errorf("drop-in does not carry the mesh ranges: %s", body)
	}
	if readEnvVar("NODEPORT_ADDRESSES") != nodePortCIDRs {
		t.Error("the console cannot tell published ports are restricted without the env record")
	}
	if len(*restarts) != 1 || (*restarts)[0] != "k3s" {
		t.Errorf("want one k3s restart, got %v", *restarts)
	}
}

// Every later upgrade passes straight through: restarting the control plane
// because a file already says what it should is a blip for nothing.
func TestRestrictNodePortsIsANoOpOnceWritten(t *testing.T) {
	_, restarts := nodePortFixture(t, true)

	if err := restrictNodePorts(); err != nil {
		t.Fatal(err)
	}
	if err := restrictNodePorts(); err != nil {
		t.Fatal(err)
	}
	if len(*restarts) != 1 {
		t.Errorf("want one restart across two upgrades, got %d", len(*restarts))
	}
}

// A machine that is not a gateway has no kube-proxy of its own to configure,
// and writing k3s configuration onto one would be a surprise.
func TestRestrictNodePortsSkipsWithoutK3s(t *testing.T) {
	dropin, restarts := nodePortFixture(t, false)

	if err := restrictNodePorts(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(dropin); err == nil {
		t.Error("wrote a k3s drop-in on a machine with no k3s")
	}
	if len(*restarts) != 0 {
		t.Errorf("restarted something on a machine with no k3s: %v", *restarts)
	}
}

// A gateway on Meshploy's own mesh keeps its narrower ranges through an
// upgrade: widening them would let a Tailscale of the machine's own answer for
// the cluster's ports.
func TestRestrictNodePortsKeepsTheOwnMeshNarrow(t *testing.T) {
	dropin, _ := nodePortFixture(t, true)
	if err := os.MkdirAll(filepath.Dir(flannelDropin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flannelDropin, []byte("flannel-iface: meshploy0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := restrictNodePorts(); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(dropin)
	if !strings.Contains(string(body), "nodeport-addresses="+ownMeshNodePortCIDRs) {
		t.Errorf("drop-in = %s", body)
	}
}
