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

// Console names are written into the Caddyfile and the zones as they are, so
// only a plain label the platform does not already serve gets through.
func TestEdgeSnapshotChecksConsoleNames(t *testing.T) {
	for name, ok := range map[string]bool{
		"apps": true, "my-apps": true,
		"apps.more": false, "": false, "a b": false, "{": false,
		"console": false, "api": false, "headscale": false, "internal": false,
	} {
		s := okSnapshot()
		s.ConsoleNames = []string{name}
		if err := s.Validate(); (err == nil) != ok {
			t.Errorf("%q: Validate() = %v, want ok=%v", name, err, ok)
		}
	}
}

// Project console names are written into the Caddyfile as they are: two plain
// labels, never inside the mesh-only zone, never twice.
func TestEdgeSnapshotChecksProjectConsoleNames(t *testing.T) {
	for _, c := range []struct {
		names []string
		ok    bool
	}{
		{[]string{"apps.shop"}, true},
		{[]string{"apps.shop", "apps.team-tracker"}, true},
		{[]string{"apps"}, false},
		{[]string{"apps.shop.more"}, false},
		{[]string{"apps.internal"}, false},
		{[]string{"apps.sh op"}, false},
		{[]string{"apps.{"}, false},
		{[]string{"apps.shop", "apps.shop"}, false},
	} {
		s := okSnapshot()
		s.ProjectConsoleNames = c.names
		if err := s.Validate(); (err == nil) != c.ok {
			t.Errorf("%q: Validate() = %v, want ok=%v", c.names, err, c.ok)
		}
	}
}

const testProviderID = "0b6f3c2e-6a1d-4f7e-9a51-2c8d4e7f1a90"

// A provider is accepted only where it can do something, and only with an ID
// that is safe as a file name and a Caddyfile path.
func TestEdgeSnapshotDNSProvider(t *testing.T) {
	withProvider := func(mode string, p EdgeDNSProvider) EdgeSnapshot {
		s := okSnapshot()
		s.Domains[0].DNSMode = mode
		s.Domains[0].DNSProvider = &p
		return s
	}
	if err := withProvider(EdgeDNSOnDemand, EdgeDNSProvider{ID: testProviderID, Kind: EdgeDNSCloudflare}).Validate(); err != nil {
		t.Fatalf("an on-demand domain with a Cloudflare provider must be accepted: %v", err)
	}
	for name, s := range map[string]EdgeSnapshot{
		"delegated domain": withProvider(EdgeDNSDelegation, EdgeDNSProvider{ID: testProviderID, Kind: EdgeDNSCloudflare}),
		"unknown kind":     withProvider(EdgeDNSOnDemand, EdgeDNSProvider{ID: testProviderID, Kind: "route53"}),
		"path in the ID":   withProvider(EdgeDNSOnDemand, EdgeDNSProvider{ID: "../../etc/passwd", Kind: EdgeDNSCloudflare}),
		"brace in the ID":  withProvider(EdgeDNSOnDemand, EdgeDNSProvider{ID: testProviderID + "}", Kind: EdgeDNSCloudflare}),
	} {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}
}

// The tokens are read once and gone: a second apply finds nothing, and the
// file was readable by root only while it existed.
func TestDNSCredentialsAreTakenOnce(t *testing.T) {
	dir := t.TempDir()
	if creds, err := TakeDNSCredentials(dir); err != nil || creds != nil {
		t.Fatalf("no file must be no credentials, got %v, %v", creds, err)
	}
	if err := WriteDNSCredentials(dir, DNSCredentials{testProviderID: "tok"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, DNSCredentialsFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("credentials file mode %v, want 0600", info.Mode().Perm())
	}
	creds, err := TakeDNSCredentials(dir)
	if err != nil || creds[testProviderID] != "tok" {
		t.Fatalf("got %v, %v", creds, err)
	}
	if again, _ := TakeDNSCredentials(dir); again != nil {
		t.Error("the credentials must be removed once taken")
	}
	if err := WriteDNSCredentials(dir, DNSCredentials{"../x": "tok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := TakeDNSCredentials(dir); err == nil {
		t.Error("a key that is not a provider ID must be refused")
	}
}

// The address goes into the Caddyfile's global options as it is, so anything
// Caddy would read as syntax is refused.
func TestEdgeSnapshotACMEEmail(t *testing.T) {
	ok := okSnapshot()
	ok.ACMEEmail = "ops+edge@example.co.uk"
	if err := ok.Validate(); err != nil {
		t.Fatalf("a plain address must be accepted: %v", err)
	}
	for _, bad := range []string{"ops", "ops@example", "ops@example.com\n}", "o ps@example.com", "{env.X}@example.com"} {
		s := okSnapshot()
		s.ACMEEmail = bad
		if err := s.Validate(); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

// A domain the platform's names are served on cannot be one project's.
func TestEdgeSnapshotProjectDomainIsNotThePlatforms(t *testing.T) {
	s := okSnapshot()
	s.Domains[0].Project = true
	if err := s.Validate(); err == nil {
		t.Fatal("the primary must not be one project's")
	}
}
