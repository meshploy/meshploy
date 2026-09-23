package edgeconfig

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/meshploy/packages/hostagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func snapshot(domains ...hostagent.EdgeDomain) hostagent.EdgeSnapshot {
	return hostagent.EdgeSnapshot{
		PublicIP: "203.0.113.10",
		MeshIP:   "100.64.0.1",
		Domains:  domains,
	}
}

func domain(name, mode string, primary bool) hostagent.EdgeDomain {
	return hostagent.EdgeDomain{
		BaseDomain: name, InternalSubdomain: "internal", DNSMode: mode, Primary: primary,
	}
}

// The golden files are the review surface for this package. A change to a
// template shows up as a diff in what a real gateway would be served, which is
// the only way to read a change to an edge configuration and know what it does.
func TestRenderMatchesGolden(t *testing.T) {
	cases := []struct {
		name string
		snap hostagent.EdgeSnapshot
	}{
		{"delegation", snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true))},
		{"ondemand", snapshot(domain("example.com", hostagent.EdgeDNSOnDemand, true))},
		// The case the file-swapping scheme could not express at all.
		{"mixed", snapshot(
			domain("example.com", hostagent.EdgeDNSDelegation, true),
			domain("second.test", hostagent.EdgeDNSOnDemand, false),
		)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files, err := Render(c.snap)
			require.NoError(t, err)

			dir := filepath.Join("testdata", c.name)
			if *update {
				require.NoError(t, os.RemoveAll(dir))
				for _, p := range files.Paths() {
					out := filepath.Join(dir, p)
					require.NoError(t, os.MkdirAll(filepath.Dir(out), 0o755))
					require.NoError(t, os.WriteFile(out, []byte(files[p]), 0o644))
				}
				return
			}
			for _, p := range files.Paths() {
				want, err := os.ReadFile(filepath.Join(dir, p))
				require.NoError(t, err, "no golden file for %s - run: go test ./internal/edgeconfig -update", p)
				assert.Equal(t, string(want), files[p], "%s differs from its golden file", p)
			}
			assert.Equal(t, goldenPaths(t, dir), files.Paths(), "the set of generated files changed")
		})
	}
}

func goldenPaths(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	require.NoError(t, filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	}))
	sort.Strings(out)
	return out
}

// Ties the generator to the files it replaced.
//
// testdata/retired holds the two Caddyfiles the deploy tarball used to ship,
// frozen at the commit that stopped shipping them. They are what every existing
// gateway is serving, so the first thing a generator has to do is not lose any
// of it: a dropped site block or a changed upstream is a gateway that stops
// answering on a name it used to. Compared by what the file does - which
// addresses it serves, where each one proxies, which TLS stanzas it carries -
// rather than byte for byte, because generation necessarily rewrites the
// comments and writes real domains where the old file left {$DOMAIN} for Caddy
// to expand.
func TestRenderCoversTheShippedCaddyfiles(t *testing.T) {
	cases := []struct {
		file string
		mode string
	}{
		{"Caddyfile", hostagent.EdgeDNSDelegation},
		{"Caddyfile.ondemand", hostagent.EdgeDNSOnDemand},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			shipped, err := os.ReadFile(filepath.Join("testdata", "retired", c.file))
			require.NoError(t, err)
			expanded := strings.NewReplacer(
				"{$DOMAIN}", "example.com",
				"{$MESH_IP}", "100.64.0.1",
				"{$PUBLIC_IP}", "203.0.113.10",
			).Replace(string(shipped))

			files, err := Render(snapshot(domain("example.com", c.mode, true)))
			require.NoError(t, err)
			got := files["caddy/Caddyfile"]

			// Both files explain themselves at length, and the prose names
			// directives. Only what Caddy reads is compared.
			expanded, got = withoutComments(expanded), withoutComments(got)

			assert.Equal(t, siteAddresses(expanded), siteAddresses(got),
				"the generated Caddyfile must serve exactly the addresses the shipped one does")
			assert.Equal(t, upstreams(expanded), upstreams(got),
				"every upstream in the shipped Caddyfile must still be proxied to")

			// The two stanzas that decide whether a certificate can be obtained
			// at all, and the one that stops a workload route being reachable
			// over the mesh.
			for _, directive := range tlsDirectives(expanded) {
				assert.Contains(t, got, directive,
					"the shipped %s uses %q and the generated one does not", c.file, directive)
			}
			assert.Contains(t, got, "bind 100.64.0.1", "the internal zone must stay bound to the mesh address")
		})
	}
}

// withoutComments drops comment lines, so prose that happens to name a
// directive is not read as one.
func withoutComments(caddyfile string) string {
	out := []string{}
	for _, line := range strings.Split(caddyfile, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// siteAddresses returns the address of every site block, in order. A site block
// opens at column 0 with the address and a brace.
var siteRe = regexp.MustCompile(`(?m)^([^\s#{][^{\n]*)\{\s*$`)

func siteAddresses(caddyfile string) []string {
	out := []string{}
	for _, m := range siteRe.FindAllStringSubmatch(caddyfile, -1) {
		addr := strings.TrimSpace(m[1])
		// Skip the ones that are not site addresses: `import` has no brace, but
		// a nested directive at column 0 would be a syntax error anyway.
		if addr == "" {
			continue
		}
		out = append(out, addr)
	}
	sort.Strings(out)
	return out
}

var upstreamRe = regexp.MustCompile(`reverse_proxy\s+(\S+)`)

func upstreams(caddyfile string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range upstreamRe.FindAllStringSubmatch(caddyfile, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// tlsDirectives picks the TLS lines whose absence would silently stop a
// certificate being issued.
func tlsDirectives(caddyfile string) []string {
	out := []string{}
	for _, line := range strings.Split(caddyfile, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "zone_file_path "),
			strings.HasPrefix(line, "resolvers "),
			line == "tls internal",
			line == "tls force_automate",
			line == "on_demand":
			out = append(out, line)
		}
	}
	return out
}

func TestRenderRefusesAnUnusableSnapshot(t *testing.T) {
	cases := map[string]hostagent.EdgeSnapshot{
		"no domains":  snapshot(),
		"no primary":  snapshot(domain("example.com", hostagent.EdgeDNSDelegation, false)),
		"two primary": snapshot(domain("a.test", hostagent.EdgeDNSDelegation, true), domain("b.test", hostagent.EdgeDNSDelegation, true)),
		"unknown mode": snapshot(hostagent.EdgeDomain{
			BaseDomain: "example.com", InternalSubdomain: "internal", DNSMode: "cloudflare", Primary: true,
		}),
		// A name carrying Caddy or zone-file syntax would be rendered straight
		// into the configuration of the thing that serves this machine.
		"a domain that is really a directive": snapshot(hostagent.EdgeDomain{
			BaseDomain:        "evil.test {\n}\n:80 {\n reverse_proxy attacker",
			InternalSubdomain: "internal", DNSMode: hostagent.EdgeDNSDelegation, Primary: true,
		}),
		"an internal subdomain with a dot": snapshot(hostagent.EdgeDomain{
			BaseDomain: "example.com", InternalSubdomain: "a.b", DNSMode: hostagent.EdgeDNSDelegation, Primary: true,
		}),
	}
	for name, snap := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Render(snap)
			require.Error(t, err)
		})
	}

	missingAddr := snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true))
	missingAddr.MeshIP = ""
	_, err := Render(missingAddr)
	require.Error(t, err)
}

// Rendering is compared with what is on disk and skipped when they match, so an
// unchanged snapshot must produce identical bytes. Ordering that followed the
// database's row order would rewrite the files, and reload the edge, for no
// change at all.
func TestRenderIsStableAndOrderIndependent(t *testing.T) {
	a := snapshot(
		domain("zebra.test", hostagent.EdgeDNSOnDemand, false),
		domain("example.com", hostagent.EdgeDNSDelegation, true),
		domain("alpha.test", hostagent.EdgeDNSDelegation, false),
	)
	b := snapshot(
		domain("alpha.test", hostagent.EdgeDNSDelegation, false),
		domain("zebra.test", hostagent.EdgeDNSOnDemand, false),
		domain("example.com", hostagent.EdgeDNSDelegation, true),
	)
	first, err := Render(a)
	require.NoError(t, err)
	second, err := Render(b)
	require.NoError(t, err)
	assert.Equal(t, first, second, "the same domains in a different order must render identically")

	again, err := Render(a)
	require.NoError(t, err)
	assert.Equal(t, first, again)

	// The primary leads, whatever it sorts as alphabetically.
	caddyfile := first["caddy/Caddyfile"]
	assert.Less(t, strings.Index(caddyfile, "example.com"), strings.Index(caddyfile, "alpha.test"))
}

// Every zone the Corefile serves must be a file the render produced, or CoreDNS
// starts with a block pointing at nothing.
func TestEveryZoneTheCorefileNamesIsGenerated(t *testing.T) {
	files, err := Render(snapshot(
		domain("example.com", hostagent.EdgeDNSDelegation, true),
		domain("second.test", hostagent.EdgeDNSOnDemand, false),
	))
	require.NoError(t, err)

	re := regexp.MustCompile(`(?m)^\s*file /etc/coredns/zones/(\S+)`)
	matches := re.FindAllStringSubmatch(files["coredns/Corefile"], -1)
	require.NotEmpty(t, matches)
	for _, m := range matches {
		_, ok := files["coredns/zones/"+m[1]]
		assert.True(t, ok, "the Corefile serves zone %q but no file was generated for it", m[1])
	}
	// Four zones for the delegated domain, one for the on-demand one.
	assert.Len(t, matches, 5)
}

// A domain whose DNS stays with the operator's provider gets its mesh zone and
// nothing else. Serving the public zone would make this gateway authoritative
// for a name it does not hold, so a resolver pointed at it directly would
// disagree with the operator's own provider.
func TestAnOnDemandDomainServesOnlyItsMeshZone(t *testing.T) {
	files, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSOnDemand, true)))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"caddy/Caddyfile",
		"coredns/Corefile",
		"coredns/zones/internal.example.com",
		HeadscaleConfig,
	}, files.Paths())

	corefile := files["coredns/Corefile"]
	assert.NotContains(t, corefile, "\nexample.com:53", "no public zone for a domain nobody delegates here")
	assert.NotContains(t, corefile, "_acme-challenge", "no challenge zone without DNS-01")
	assert.Contains(t, corefile, "internal.example.com:53")

	// Nothing may reference a file that is no longer rendered, or CoreDNS and
	// Caddy start pointing at zones that do not exist.
	assert.NotContains(t, files["caddy/Caddyfile"], "zone_file_path")

	// Flipping the mode back brings them straight back: they are not kept
	// against a later switch, because the switch re-renders.
	back, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)
	assert.Len(t, back.Paths(), 7)
}

// An operator's own configuration has to survive a regeneration, because the
// file it would otherwise live in is rewritten without warning.
func TestGeneratedFilesSayTheyAreGeneratedAndLeaveAWayIn(t *testing.T) {
	files, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)
	for _, p := range files.Paths() {
		assert.Contains(t, files[p], "generated", "%s must say it is generated", p)
	}
	assert.Contains(t, files["caddy/Caddyfile"], "import conf.d/*.caddy")
	for _, p := range files.Paths() {
		assert.True(t, strings.HasSuffix(files[p], "\n"), "%s must end with a newline", p)
		assert.NotContains(t, files[p], "\n\n\n", "%s has a run of blank lines", p)
	}
}

// Runs the real Caddy over every golden Caddyfile.
//
// The generator's output is only as good as Caddy's opinion of it, and a
// Caddyfile that does not parse takes the gateway off the internet. Skipped
// when no Caddy is on PATH; the module Meshploy's delegation config needs
// (`dns meshploy`) is not in the stock binary, so the image this repo builds is
// what to reach for:
//
//	podman run --rm -v "$PWD/testdata/mixed/caddy:/w:z" ghcr.io/meshploy/caddy \
//	  caddy validate --config /w/Caddyfile --adapter caddyfile
//
// All three golden configurations were validated that way on 2026-09-23,
// including the mixed one - which also confirmed that `import conf.d/*.caddy`
// matching no files is not an error, so a fresh install needs no placeholder.
func TestGoldenCaddyfilesAreValid(t *testing.T) {
	caddy, err := exec.LookPath("caddy")
	if err != nil {
		t.Skip("no caddy on PATH; see the comment above for the container command")
	}
	for _, c := range []string{"delegation", "ondemand", "mixed"} {
		t.Run(c, func(t *testing.T) {
			cmd := exec.Command(caddy, "validate",
				"--config", filepath.Join("testdata", c, "caddy", "Caddyfile"),
				"--adapter", "caddyfile")
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "caddy rejected the generated config:\n%s", out)
		})
	}
}

// Headscale tells each node which zones to ask the gateway's DNS for. CoreDNS
// serves every base domain's internal zone on the mesh address, but a node
// only asks for the zones listed here - so with the primary's alone, an
// internal route on any other domain never resolved.
func TestHeadscaleSendsEveryInternalZoneToTheMesh(t *testing.T) {
	files, err := Render(snapshot(
		domain("example.com", hostagent.EdgeDNSDelegation, true),
		domain("second.test", hostagent.EdgeDNSOnDemand, false),
	))
	require.NoError(t, err)
	hs := files[HeadscaleConfig]
	assert.Contains(t, hs, `"internal.example.com":`)
	assert.Contains(t, hs, `"internal.second.test":`)
	assert.Contains(t, hs, "server_url: https://headscale.example.com")
	assert.Contains(t, hs, "base_domain: mesh.example.com")
}

// Moving the primary moves where Headscale says it lives, and nothing that
// would rename a node.
func TestMovingThePrimaryKeepsTheMeshDomainAndTheOldPlatformNames(t *testing.T) {
	snap := snapshot(
		hostagent.EdgeDomain{BaseDomain: "old.test", InternalSubdomain: "internal", DNSMode: hostagent.EdgeDNSDelegation, ServesPlatform: true},
		domain("new.test", hostagent.EdgeDNSDelegation, true),
	)
	snap.MeshDomain = "mesh.old.test"
	files, err := Render(snap)
	require.NoError(t, err)

	hs := files[HeadscaleConfig]
	assert.Contains(t, hs, "server_url: https://headscale.new.test")
	assert.Contains(t, hs, "base_domain: mesh.old.test", "node names must not change when the primary moves")

	caddy := withoutComments(files["caddy/Caddyfile"])
	for _, host := range []string{"console.new.test", "headscale.new.test", "console.old.test", "headscale.old.test", "api.old.test"} {
		assert.Contains(t, caddy, host+" {", "%s must be served: the console in use and every worker's control URL depend on it", host)
	}
}

// A domain that was never primary serves no platform names.
func TestAnOrdinaryBaseDomainServesNoPlatformNames(t *testing.T) {
	files, err := Render(snapshot(
		domain("example.com", hostagent.EdgeDNSDelegation, true),
		domain("second.test", hostagent.EdgeDNSOnDemand, false),
	))
	require.NoError(t, err)
	caddy := withoutComments(files["caddy/Caddyfile"])
	assert.NotContains(t, caddy, "console.second.test")
	assert.NotContains(t, caddy, "headscale.second.test")
}

// Ties the generated Headscale config to the template it replaced, frozen in
// testdata/retired. For a single domain it must be the same file apart from
// comments - it is what every existing gateway's mesh runs on, and a stray
// difference here is a control plane that behaves differently after an upgrade.
// Checked against pnath-5-rt's live file on 2026-09-23 as well.
func TestRenderReproducesTheShippedHeadscaleConfig(t *testing.T) {
	shipped, err := os.ReadFile(filepath.Join("testdata", "retired", "headscale.yaml"))
	require.NoError(t, err)
	expanded := strings.ReplaceAll(string(shipped), "${DOMAIN}", "example.com")

	files, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)

	strip := func(s string) string {
		var out []string
		for _, l := range strings.Split(s, "\n") {
			l = strings.TrimRight(l, " \t")
			if t := strings.TrimSpace(l); t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			out = append(out, l)
		}
		return strings.Join(out, "\n")
	}
	assert.Equal(t, strip(expanded), strip(files[HeadscaleConfig]))
}
