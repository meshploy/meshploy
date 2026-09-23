package hostagent

import (
	"os"
	"path/filepath"
	"testing"
)

func okSnapshot() EdgeSnapshot {
	return EdgeSnapshot{
		PublicIP: "203.0.113.10", MeshIP: "100.64.0.1",
		Domains: []EdgeDomain{
			{BaseDomain: "example.com", InternalSubdomain: "internal", DNSMode: EdgeDNSDelegation, Primary: true},
		},
	}
}

func TestEdgeSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadEdgeSnapshot(dir); !os.IsNotExist(err) {
		t.Fatalf("a host directory with no snapshot must report not-exist, got %v", err)
	}
	if err := WriteEdgeSnapshot(dir, okSnapshot()); err != nil {
		t.Fatal(err)
	}
	got, err := ReadEdgeSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Domains) != 1 || got.Domains[0].BaseDomain != "example.com" {
		t.Fatalf("round trip lost the domain: %+v", got)
	}
	if got.WrittenAt.IsZero() {
		t.Error("the snapshot must record when it was written")
	}
	if got.Primary() == nil || !got.Primary().Primary {
		t.Error("Primary must find the primary domain")
	}
	// No leftover temporary file: the generator globs this directory.
	entries, _ := os.ReadDir(filepath.Join(dir, "state"))
	if len(entries) != 1 {
		t.Errorf("expected only edge.json in state/, got %d entries", len(entries))
	}
}

// An unusable snapshot must not reach the file the generator reads: it is the
// input to the configuration of the thing that serves this machine.
func TestWriteEdgeSnapshotRefusesAnInvalidOne(t *testing.T) {
	dir := t.TempDir()
	bad := okSnapshot()
	bad.Domains[0].Primary = false
	if err := WriteEdgeSnapshot(dir, bad); err == nil {
		t.Fatal("a snapshot with no primary must be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, EdgeSnapshotFile)); !os.IsNotExist(err) {
		t.Error("nothing must be written when validation fails")
	}
}

func TestEdgeSnapshotSortsPrimaryFirstThenAlphabetically(t *testing.T) {
	snap := EdgeSnapshot{
		PublicIP: "203.0.113.10", MeshIP: "100.64.0.1",
		Domains: []EdgeDomain{
			{BaseDomain: "zebra.test", InternalSubdomain: "internal", DNSMode: EdgeDNSOnDemand},
			{BaseDomain: "alpha.test", InternalSubdomain: "internal", DNSMode: EdgeDNSDelegation},
			{BaseDomain: "middle.test", InternalSubdomain: "internal", DNSMode: EdgeDNSDelegation, Primary: true},
		},
	}
	want := []string{"middle.test", "alpha.test", "zebra.test"}
	for i, d := range snap.Sorted() {
		if d.BaseDomain != want[i] {
			t.Errorf("position %d: got %s, want %s", i, d.BaseDomain, want[i])
		}
	}
}

func TestEdgeSnapshotRejectsSyntaxInAName(t *testing.T) {
	for _, name := range []string{
		"", "UPPER.test", "has space.test", "brace{.test", "semi;colon.test",
		"-lead.test", "trail-.test", "double..dot", ".leading", "trailing.",
		"line\nbreak.test", "tab\ttest.com",
		// A single label cannot be delegated or certified, whatever it is.
		"localhost", "gateway",
	} {
		snap := okSnapshot()
		snap.Domains[0].BaseDomain = name
		if err := snap.Validate(); err == nil {
			t.Errorf("%q must be refused as a base domain", name)
		}
	}
}
