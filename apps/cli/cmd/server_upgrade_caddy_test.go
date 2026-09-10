package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	delegationTmpl = "# delegation v2\n*.{$DOMAIN} { tls { dns meshploy } }\n"
	ondemandTmpl   = "# on-demand\nconsole.{$DOMAIN} { tls force_automate }\n"
)

// caddyInstall lays out /opt/meshploy/caddy as an install would have it.
// backup == "" means no delegation backup exists.
func caddyInstall(t *testing.T, live, backup string) string {
	t.Helper()
	dir := t.TempDir()
	orig := meshployInstDir
	meshployInstDir = dir
	t.Cleanup(func() { meshployInstDir = orig })

	c := filepath.Join(dir, "caddy")
	if err := os.MkdirAll(c, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(c, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Caddyfile", live)
	write("Caddyfile.ondemand", ondemandTmpl)
	if backup != "" {
		write("Caddyfile.delegation.bak", backup)
	}
	return c
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The bug this exists for: an upgrade unpacks the delegation template over an
// on-demand gateway's Caddyfile, and nothing put the on-demand one back.
func TestUpgradeRestoresOnDemandCaddyfileAfterUnpack(t *testing.T) {
	c := caddyInstall(t, delegationTmpl, "# delegation v1 (first install)\n")

	if err := applyDNSModeCaddyfile("ondemand"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(c, "Caddyfile")); got != ondemandTmpl {
		t.Fatalf("Caddyfile = %q, want the on-demand variant", got)
	}
	// And the backup follows the release, not the first install.
	if got := read(t, filepath.Join(c, "Caddyfile.delegation.bak")); got != delegationTmpl {
		t.Fatalf("backup = %q, want this release's delegation template", got)
	}
}

func TestOnDemandAlreadyInPlaceIsLeftAlone(t *testing.T) {
	c := caddyInstall(t, ondemandTmpl, "# delegation v1\n")

	if err := applyDNSModeCaddyfile("ondemand"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(c, "Caddyfile.delegation.bak")); got != "# delegation v1\n" {
		t.Fatalf("backup overwritten with %q; an on-demand copy must never become the backup", got)
	}
}

func TestDelegationKeepsAFreshTemplate(t *testing.T) {
	c := caddyInstall(t, delegationTmpl, "# delegation v1 (stale)\n")

	if err := applyDNSModeCaddyfile("delegation"); err != nil {
		t.Fatal(err)
	}
	// The stale backup must not replace the template that was just unpacked.
	if got := read(t, filepath.Join(c, "Caddyfile")); got != delegationTmpl {
		t.Fatalf("Caddyfile = %q, want the freshly unpacked template", got)
	}
}

func TestSwitchingBackToDelegationRestoresTheBackup(t *testing.T) {
	c := caddyInstall(t, ondemandTmpl, delegationTmpl)

	if err := applyDNSModeCaddyfile("delegation"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(c, "Caddyfile")); got != delegationTmpl {
		t.Fatalf("Caddyfile = %q, want the saved delegation config", got)
	}
}

// Every install from before DNS modes has no DNS_MODE at all.
func TestUnsetModeMeansDelegation(t *testing.T) {
	c := caddyInstall(t, delegationTmpl, "")

	if err := applyDNSModeCaddyfile(""); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(c, "Caddyfile")); got != delegationTmpl {
		t.Fatalf("Caddyfile = %q, want it untouched", got)
	}
}

func TestDelegationWithoutBackupKeepsServing(t *testing.T) {
	c := caddyInstall(t, ondemandTmpl, "")

	if err := applyDNSModeCaddyfile("delegation"); err != nil {
		t.Fatalf("must not fail the upgrade: %v", err)
	}
	if got := read(t, filepath.Join(c, "Caddyfile")); got != ondemandTmpl {
		t.Fatalf("Caddyfile = %q, want the working config kept", got)
	}
}

func TestOnDemandWithoutItsTemplateFails(t *testing.T) {
	c := caddyInstall(t, delegationTmpl, "")
	if err := os.Remove(filepath.Join(c, "Caddyfile.ondemand")); err != nil {
		t.Fatal(err)
	}
	if err := applyDNSModeCaddyfile("ondemand"); err == nil {
		t.Fatal("want an error: silently serving the delegation config is the bug")
	}
}

// Docker pins a single-file bind mount to an inode. Replacing the file instead
// of rewriting it would leave a restarted Caddy reading the old one.
func TestCaddyfileIsRewrittenInPlace(t *testing.T) {
	c := caddyInstall(t, delegationTmpl, "")
	live := filepath.Join(c, "Caddyfile")
	before, err := os.Stat(live)
	if err != nil {
		t.Fatal(err)
	}

	if err := applyDNSModeCaddyfile("ondemand"); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(live)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("Caddyfile was replaced, not rewritten; a bind mount would keep the old inode")
	}
}
