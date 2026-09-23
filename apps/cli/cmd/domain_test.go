package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/meshploy/apps/cli/internal/edgeconfig"
	"github.com/meshploy/packages/hostagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// domainTestServer points the command at temporary directories and stubs the
// two things that need a container runtime.
func domainTestServer(t *testing.T, env string) (inst string, validated *[]string, reloads *int) {
	t.Helper()
	inst, host := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(inst, ".env"), []byte(env), 0o600))

	origInst, origHost := meshployInstDir, hostDir
	origValidate, origReload := validateCaddyfile, reloadEdge
	origFromEnv, origDry, origForce, origNoReload := domainApplyFromEnv, domainApplyDryRun, domainApplyForce, domainApplyNoReload
	meshployInstDir, hostDir = inst, host
	t.Cleanup(func() {
		meshployInstDir, hostDir = origInst, origHost
		validateCaddyfile, reloadEdge = origValidate, origReload
		domainApplyFromEnv, domainApplyDryRun, domainApplyForce, domainApplyNoReload = origFromEnv, origDry, origForce, origNoReload
	})

	seen := []string{}
	count := 0
	validateCaddyfile = func(rendered string) error { seen = append(seen, rendered); return nil }
	reloadEdge = func([]edgeconfig.Change) error { count++; return nil }
	domainApplyFromEnv, domainApplyDryRun, domainApplyForce, domainApplyNoReload = false, false, false, false
	return inst, &seen, &count
}

const gatewayEnv = "DOMAIN=example.com\nDNS_MODE=delegation\nPUBLIC_IP=203.0.113.10\nMESH_IP=100.64.0.1\n"

func TestDomainApplyFromEnvWritesTheSnapshotAndTheConfig(t *testing.T) {
	inst, validated, reloads := domainTestServer(t, gatewayEnv)
	domainApplyFromEnv = true

	var out bytes.Buffer
	require.NoError(t, runDomainApply(&out))

	snap, err := hostagent.ReadEdgeSnapshot(hostDir)
	require.NoError(t, err, "--from-env must leave a snapshot for later runs to read")
	require.Len(t, snap.Domains, 1)
	assert.Equal(t, "example.com", snap.Domains[0].BaseDomain)
	assert.True(t, snap.Domains[0].Primary, "the domain in .env is the one the platform is served on")
	assert.Equal(t, "delegation", snap.Domains[0].DNSMode)

	assert.FileExists(t, filepath.Join(inst, "caddy/Caddyfile"))
	assert.FileExists(t, filepath.Join(inst, "coredns/Corefile"))
	assert.FileExists(t, filepath.Join(inst, "coredns/zones/example.com"))
	assert.Len(t, *validated, 1, "Caddy must be asked before anything is written")
	assert.Equal(t, 1, *reloads)
}

// Safe to run on every start and after every upgrade: an apply that finds
// nothing to do must not reload the edge.
func TestDomainApplyDoesNothingWhenNothingChanged(t *testing.T) {
	_, _, reloads := domainTestServer(t, gatewayEnv)
	domainApplyFromEnv = true
	var out bytes.Buffer
	require.NoError(t, runDomainApply(&out))
	require.Equal(t, 1, *reloads)

	out.Reset()
	require.NoError(t, runDomainApply(&out))
	assert.Equal(t, 1, *reloads, "a second apply must not touch the edge")
	assert.Contains(t, out.String(), "Nothing to do")
}

func TestDomainApplyDryRunWritesNothing(t *testing.T) {
	inst, validated, reloads := domainTestServer(t, gatewayEnv)
	domainApplyFromEnv, domainApplyDryRun = true, true

	var out bytes.Buffer
	require.NoError(t, runDomainApply(&out))
	assert.Contains(t, out.String(), "--dry-run")
	assert.Contains(t, out.String(), "create caddy/Caddyfile")

	_, err := os.Stat(filepath.Join(inst, "caddy/Caddyfile"))
	assert.True(t, os.IsNotExist(err))
	assert.Empty(t, *validated)
	assert.Zero(t, *reloads)
}

// The person running this is most likely reaching the gateway over the gateway.
// A configuration Caddy will not accept must never be written.
func TestDomainApplyWritesNothingWhenCaddyRejectsTheConfig(t *testing.T) {
	inst, _, reloads := domainTestServer(t, gatewayEnv)
	domainApplyFromEnv = true
	validateCaddyfile = func(string) error { return errors.New("adapting config: unrecognized directive") }

	var out bytes.Buffer
	err := runDomainApply(&out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nothing was changed")
	_, statErr := os.Stat(filepath.Join(inst, "caddy/Caddyfile"))
	assert.True(t, os.IsNotExist(statErr))
	assert.Zero(t, *reloads)
}

// A valid configuration that nonetheless does not come up has to be undone, or
// the gateway is unreachable by the one person who could fix it.
func TestDomainApplyPutsThePreviousConfigBackWhenTheEdgeDoesNotComeUp(t *testing.T) {
	inst, _, _ := domainTestServer(t, gatewayEnv)
	domainApplyFromEnv = true
	var out bytes.Buffer
	require.NoError(t, runDomainApply(&out))
	good, err := os.ReadFile(filepath.Join(inst, "caddy/Caddyfile"))
	require.NoError(t, err)

	// A mode switch, which rewrites the Caddyfile and drops three zones.
	require.NoError(t, os.WriteFile(filepath.Join(inst, ".env"),
		[]byte("DOMAIN=example.com\nDNS_MODE=ondemand\nPUBLIC_IP=203.0.113.10\nMESH_IP=100.64.0.1\n"), 0o600))
	failures := 0
	reloadEdge = func([]edgeconfig.Change) error {
		failures++
		if failures == 1 {
			return errors.New("caddy exited during reload")
		}
		return nil
	}

	out.Reset()
	err = runDomainApply(&out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "previous configuration was put back")

	back, err := os.ReadFile(filepath.Join(inst, "caddy/Caddyfile"))
	require.NoError(t, err)
	assert.Equal(t, string(good), string(back), "the Caddyfile that was serving must be exactly back")
	assert.FileExists(t, filepath.Join(inst, "coredns/zones/_acme-challenge.example.com"),
		"the zones the apply removed must be back too")
	assert.Equal(t, 2, failures, "the restored configuration must be reloaded, not left on disk")
}

func TestDomainApplyNeedsASnapshotOrFromEnv(t *testing.T) {
	domainTestServer(t, gatewayEnv)
	var out bytes.Buffer
	err := runDomainApply(&out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--from-env")
}

func TestSnapshotFromEnvRefusesAnIncompleteInstall(t *testing.T) {
	cases := map[string]string{
		"no domain":    "DNS_MODE=delegation\nPUBLIC_IP=203.0.113.10\n",
		"no public IP": "DOMAIN=example.com\nDNS_MODE=delegation\n",
		"bogus mode":   "DOMAIN=example.com\nDNS_MODE=cloudflare\nPUBLIC_IP=203.0.113.10\n",
		"bogus domain": "DOMAIN=not a domain\nPUBLIC_IP=203.0.113.10\n",
		"a bare label": "DOMAIN=localhost\nPUBLIC_IP=203.0.113.10\n",
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			domainTestServer(t, env)
			_, err := mergeEnvIntoSnapshot(nil)
			require.Error(t, err)
		})
	}
}

// An install that recorded no DNS mode predates the choice, and delegation is
// what those servers are running.
func TestSnapshotFromEnvDefaultsTheModeAndTheMeshAddress(t *testing.T) {
	domainTestServer(t, "DOMAIN=example.com\nPUBLIC_IP=203.0.113.10\n")
	snap, err := mergeEnvIntoSnapshot(nil)
	require.NoError(t, err)
	assert.Equal(t, hostagent.EdgeDNSDelegation, snap.Domains[0].DNSMode)
	assert.Equal(t, "100.64.0.1", snap.MeshIP)
}

// The staged file has to sit beside the real one, or `import conf.d/*.caddy`
// resolves against a directory with none of the operator's configuration in it
// and validation passes something that then fails on reload.
func TestStagedCaddyfileSitsBesideTheRealOne(t *testing.T) {
	assert.Equal(t, "caddy", filepath.Dir(edgeconfig.StagedCaddyfile))
}

// An installer re-run on a gateway that has since added base domains must not
// silently stop serving them. .env is authoritative only for which domain is
// primary; it says nothing about the others.
func TestApplyFromEnvKeepsTheOtherBaseDomains(t *testing.T) {
	inst, _, _ := domainTestServer(t, gatewayEnv)
	require.NoError(t, hostagent.WriteEdgeSnapshot(hostDir, hostagent.EdgeSnapshot{
		PublicIP: "203.0.113.10", MeshIP: "100.64.0.1",
		Domains: []hostagent.EdgeDomain{
			{BaseDomain: "example.com", InternalSubdomain: "internal", DNSMode: "delegation", Primary: true},
			{BaseDomain: "second.test", InternalSubdomain: "internal", DNSMode: "ondemand"},
		},
	}))

	domainApplyFromEnv = true
	var out bytes.Buffer
	require.NoError(t, runDomainApply(&out))

	snap, err := hostagent.ReadEdgeSnapshot(hostDir)
	require.NoError(t, err)
	require.Len(t, snap.Domains, 2, "the second base domain must survive an installer re-run")
	assert.FileExists(t, filepath.Join(inst, "coredns/zones/internal.second.test"))
}

// Changing DOMAIN in .env moves the primary. The domain that was primary stays
// a base domain and keeps serving - it is not removed, because routes on it are
// still answering and nothing has said to retire it.
func TestApplyFromEnvMovesThePrimaryWithoutDroppingTheOldOne(t *testing.T) {
	_, _, _ = domainTestServer(t, "DOMAIN=new.test\nDNS_MODE=delegation\nPUBLIC_IP=203.0.113.10\nMESH_IP=100.64.0.1\n")
	require.NoError(t, hostagent.WriteEdgeSnapshot(hostDir, hostagent.EdgeSnapshot{
		PublicIP: "203.0.113.10", MeshIP: "100.64.0.1",
		Domains: []hostagent.EdgeDomain{
			{BaseDomain: "old.test", InternalSubdomain: "internal", DNSMode: "delegation", Primary: true},
		},
	}))

	domainApplyFromEnv = true
	var out bytes.Buffer
	require.NoError(t, runDomainApply(&out))

	snap, err := hostagent.ReadEdgeSnapshot(hostDir)
	require.NoError(t, err)
	require.Len(t, snap.Domains, 2)
	assert.Equal(t, "new.test", snap.Primary().BaseDomain)
	var old *hostagent.EdgeDomain
	for i := range snap.Domains {
		if snap.Domains[i].BaseDomain == "old.test" {
			old = &snap.Domains[i]
		}
	}
	require.NotNil(t, old, "the previous primary must remain a base domain")
	assert.False(t, old.Primary)
}

// The installer writes the files and starts the containers itself, so a reload
// at that point would be aimed at containers that do not exist yet.
func TestApplyNoReloadWritesTheFilesAndLeavesTheEdgeAlone(t *testing.T) {
	inst, validated, reloads := domainTestServer(t, gatewayEnv)
	domainApplyFromEnv, domainApplyNoReload = true, true

	var out bytes.Buffer
	require.NoError(t, runDomainApply(&out))
	assert.FileExists(t, filepath.Join(inst, "caddy/Caddyfile"))
	assert.Len(t, *validated, 1, "the config is still checked before it is written")
	assert.Zero(t, *reloads)
	assert.Contains(t, out.String(), "Not reloading")
}

// The host agent's path. The API can leave a domain set in the inbox and
// nothing else: it has no root on the host and no Docker socket, so the edge
// changes only when this request asks for it.
func TestDomainApplyRequestServesWhatTheAPILeftInTheInbox(t *testing.T) {
	inst, validated, reloads := domainTestServer(t, gatewayEnv)

	desired := hostagent.EdgeSnapshot{
		PublicIP: "203.0.113.10", MeshIP: "100.64.0.1",
		Domains: []hostagent.EdgeDomain{
			{BaseDomain: "example.com", InternalSubdomain: "internal", DNSMode: "delegation", Primary: true},
			{BaseDomain: "second.test", InternalSubdomain: "internal", DNSMode: "ondemand"},
		},
	}
	require.NoError(t, hostagent.WriteDesiredEdgeSnapshot(hostDir, desired))

	// Writing it changes nothing on its own.
	_, err := os.Stat(filepath.Join(inst, "caddy/Caddyfile"))
	require.True(t, os.IsNotExist(err))

	body, err := runDomainApplyRequest(false)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(inst, "caddy/Caddyfile"))
	assert.FileExists(t, filepath.Join(inst, "coredns/zones/internal.second.test"))
	assert.Len(t, *validated, 1)
	assert.Equal(t, 1, *reloads)

	// state/ records what is installed, so the console can read back what the
	// gateway is actually serving.
	installed, err := hostagent.ReadEdgeSnapshot(hostDir)
	require.NoError(t, err)
	assert.Len(t, installed.Domains, 2)

	var report map[string]any
	require.NoError(t, json.Unmarshal(body, &report))
	assert.Equal(t, false, report["dry_run"])
}

// state/ must never claim the gateway is serving something it is not.
func TestDomainApplyRequestRecordsNothingWhenTheApplyFails(t *testing.T) {
	domainTestServer(t, gatewayEnv)
	require.NoError(t, hostagent.WriteDesiredEdgeSnapshot(hostDir, hostagent.EdgeSnapshot{
		PublicIP: "203.0.113.10", MeshIP: "100.64.0.1",
		Domains: []hostagent.EdgeDomain{{BaseDomain: "example.com", InternalSubdomain: "internal", DNSMode: "delegation", Primary: true}},
	}))
	validateCaddyfile = func(string) error { return errors.New("unrecognized directive") }

	_, err := runDomainApplyRequest(false)
	require.Error(t, err)
	_, statErr := hostagent.ReadEdgeSnapshot(hostDir)
	assert.True(t, os.IsNotExist(statErr), "nothing must be recorded as installed")
}

func TestDomainApplyRequestDryRunWritesNothing(t *testing.T) {
	inst, _, reloads := domainTestServer(t, gatewayEnv)
	require.NoError(t, hostagent.WriteDesiredEdgeSnapshot(hostDir, hostagent.EdgeSnapshot{
		PublicIP: "203.0.113.10", MeshIP: "100.64.0.1",
		Domains: []hostagent.EdgeDomain{{BaseDomain: "example.com", InternalSubdomain: "internal", DNSMode: "delegation", Primary: true}},
	}))

	body, err := runDomainApplyRequest(true)
	require.NoError(t, err)
	_, statErr := os.Stat(filepath.Join(inst, "caddy/Caddyfile"))
	assert.True(t, os.IsNotExist(statErr))
	assert.Zero(t, *reloads)
	_, statErr = hostagent.ReadEdgeSnapshot(hostDir)
	assert.True(t, os.IsNotExist(statErr))
	assert.Contains(t, string(body), "\"dry_run\":true")
}

func TestDomainApplyRequestNeedsADomainSet(t *testing.T) {
	domainTestServer(t, gatewayEnv)
	_, err := runDomainApplyRequest(false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), hostagent.EdgeDesiredFile)
}

// The API chooses the contents of that file, so the agent trusts nothing about
// it but its shape. A name carrying Caddy or zone syntax would otherwise be
// written straight into the configuration of what serves this machine.
func TestDomainApplyRequestRefusesADomainSetItCannotTrust(t *testing.T) {
	domainTestServer(t, gatewayEnv)
	// Past WriteDesiredEdgeSnapshot's own validation, by writing the file raw.
	raw := `{"public_ip":"203.0.113.10","mesh_ip":"100.64.0.1","domains":[` +
		`{"base_domain":"evil.test {\n}\n:80 {\nreverse_proxy attacker","internal_subdomain":"internal","dns_mode":"delegation","primary":true}]}`
	path := filepath.Join(hostDir, hostagent.EdgeDesiredFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o644))

	_, err := runDomainApplyRequest(false)
	require.Error(t, err)
}
