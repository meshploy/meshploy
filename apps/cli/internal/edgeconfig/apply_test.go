package edgeconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/meshploy/packages/hostagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
}

func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	require.NoError(t, err)
	return string(b)
}

func paths(changes []Change) []string {
	out := make([]string, 0, len(changes))
	for _, c := range changes {
		out = append(out, c.String())
	}
	return out
}

// An apply that finds nothing to do must do nothing. The snapshot is rewritten
// whenever anything about a domain changes, including changes that never reach
// the edge, so applying unconditionally would reload Caddy and restart CoreDNS
// for nothing.
func TestPlanIsEmptyWhenNothingChanged(t *testing.T) {
	dir := t.TempDir()
	files, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)

	changes, err := Plan(files, dir)
	require.NoError(t, err)
	require.Len(t, changes, len(files), "an empty directory means every file is new")
	require.NoError(t, Install(changes, dir))

	changes, err = Plan(files, dir)
	require.NoError(t, err)
	assert.Empty(t, changes, "a second plan over the same render must find nothing")
}

func TestPlanReportsWhatActuallyDiffers(t *testing.T) {
	dir := t.TempDir()
	files, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)
	changes, _ := Plan(files, dir)
	require.NoError(t, Install(changes, dir))

	writeFile(t, dir, "coredns/Corefile", "somebody edited this\n")
	changes, err = Plan(files, dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"update coredns/Corefile"}, paths(changes))
}

// Switching a domain to on-demand stops three of its zones being served. The
// files must go, or the directory fills with zones nothing reads and the next
// person cannot tell which are live.
func TestPlanRemovesZonesTheRenderNoLongerProduces(t *testing.T) {
	dir := t.TempDir()
	delegated, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)
	changes, _ := Plan(delegated, dir)
	require.NoError(t, Install(changes, dir))

	onDemand, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSOnDemand, true)))
	require.NoError(t, err)
	changes, err = Plan(onDemand, dir)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"update caddy/Caddyfile",
		"update coredns/Corefile",
		"remove coredns/zones/_acme-challenge.example.com",
		"remove coredns/zones/_acme-challenge.internal.example.com",
		"remove coredns/zones/example.com",
	}, paths(changes))

	require.NoError(t, Install(changes, dir))
	_, err = os.Stat(filepath.Join(dir, "coredns/zones/example.com"))
	assert.True(t, os.IsNotExist(err))
	assert.FileExists(t, filepath.Join(dir, "coredns/zones/internal.example.com"))
}

// Deciding what is stale must never decide that about a file it did not write.
// An older install left {DOMAIN}-named templates in this directory, and an
// operator may have put a zone here by hand.
func TestPlanLeavesFilesItDidNotWriteAlone(t *testing.T) {
	dir := t.TempDir()
	files, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)
	changes, _ := Plan(files, dir)
	require.NoError(t, Install(changes, dir))

	writeFile(t, dir, "coredns/zones/{DOMAIN}", "; the template an older installer left\n")
	writeFile(t, dir, "coredns/zones/hand-written.test", "; somebody's own zone\n$TTL 300\n")

	changes, err = Plan(files, dir)
	require.NoError(t, err)
	assert.Empty(t, changes, "neither file is ours, so neither is stale")
}

func TestInstallAndRestoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	delegated, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)
	changes, _ := Plan(delegated, dir)
	require.NoError(t, Install(changes, dir))
	before := readFile(t, dir, "caddy/Caddyfile")

	// The apply a rollback has to undo: one file rewritten, three removed.
	onDemand, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSOnDemand, true)))
	require.NoError(t, err)
	changes, err = Plan(onDemand, dir)
	require.NoError(t, err)
	require.NoError(t, Install(changes, dir))
	require.NotEqual(t, before, readFile(t, dir, "caddy/Caddyfile"))

	require.NoError(t, Restore(dir))
	assert.Equal(t, before, readFile(t, dir, "caddy/Caddyfile"), "the Caddyfile must be exactly what it was")
	assert.FileExists(t, filepath.Join(dir, "coredns/zones/example.com"), "a removed zone must come back")
	assert.FileExists(t, filepath.Join(dir, "coredns/zones/_acme-challenge.example.com"))

	// And the restored tree is once again what the delegated render wants.
	changes, err = Plan(delegated, dir)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

// A file that did not exist before the apply has to be removed by a restore,
// not written back empty. Adding a second base domain creates zones; putting
// the server back must not leave them behind for CoreDNS to serve.
func TestRestoreRemovesWhatTheApplyCreated(t *testing.T) {
	dir := t.TempDir()
	one, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)
	changes, _ := Plan(one, dir)
	require.NoError(t, Install(changes, dir))

	two, err := Render(snapshot(
		domain("example.com", hostagent.EdgeDNSDelegation, true),
		domain("second.test", hostagent.EdgeDNSDelegation, false),
	))
	require.NoError(t, err)
	changes, err = Plan(two, dir)
	require.NoError(t, err)
	require.NoError(t, Install(changes, dir))
	assert.FileExists(t, filepath.Join(dir, "coredns/zones/second.test"))

	require.NoError(t, Restore(dir))
	_, err = os.Stat(filepath.Join(dir, "coredns/zones/second.test"))
	assert.True(t, os.IsNotExist(err), "a zone the apply created must not survive the rollback")

	changes, err = Plan(one, dir)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

// Only one generation is kept, so a rollback cannot reach past the apply that
// is being undone and put back something older than what was actually running.
func TestInstallKeepsOnlyTheLastGeneration(t *testing.T) {
	dir := t.TempDir()
	first, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)
	changes, _ := Plan(first, dir)
	require.NoError(t, Install(changes, dir))

	second, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSOnDemand, true)))
	require.NoError(t, err)
	changes, _ = Plan(second, dir)
	require.NoError(t, Install(changes, dir))
	middle := readFile(t, dir, "caddy/Caddyfile")

	third, err := Render(snapshot(domain("other.test", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)
	changes, _ = Plan(third, dir)
	require.NoError(t, Install(changes, dir))

	require.NoError(t, Restore(dir))
	assert.Equal(t, middle, readFile(t, dir, "caddy/Caddyfile"),
		"a restore must put back what was running, not an older generation")
}

func TestInstallWritesNothingElse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".env", "DOMAIN=example.com\n")
	writeFile(t, dir, "docker-compose.yml", "services: {}\n")
	writeFile(t, dir, "caddy/conf.d/mine.caddy", "# my own block\n")

	files, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)
	changes, _ := Plan(files, dir)
	require.NoError(t, Install(changes, dir))

	assert.Equal(t, "DOMAIN=example.com\n", readFile(t, dir, ".env"))
	assert.Equal(t, "services: {}\n", readFile(t, dir, "docker-compose.yml"))
	assert.Equal(t, "# my own block\n", readFile(t, dir, "caddy/conf.d/mine.caddy"),
		"an operator's own configuration must survive every apply")
}

// A DNS-01 challenge is answered by Caddy writing a TXT record into the zone
// file and removing it afterwards. Rewriting one would delete a challenge in
// flight, and would mean an apply never finding nothing to do, because any
// certificate being issued at that moment is a difference.
func TestPlanSeedsChallengeZonesAndThenLeavesThemAlone(t *testing.T) {
	dir := t.TempDir()
	files, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)
	changes, _ := Plan(files, dir)
	require.NoError(t, Install(changes, dir))
	assert.FileExists(t, filepath.Join(dir, "coredns/zones/_acme-challenge.example.com"),
		"the zone has to exist before Caddy can write into it")

	// Caddy, mid-issuance.
	live := readFile(t, dir, "coredns/zones/_acme-challenge.example.com") +
		"\n@   IN TXT  \"a-challenge-in-flight\"\n"
	writeFile(t, dir, "coredns/zones/_acme-challenge.example.com", live)

	changes, err = Plan(files, dir)
	require.NoError(t, err)
	assert.Empty(t, changes, "a challenge zone Caddy has written to is not a difference")

	// And a real change elsewhere must not sweep it up.
	writeFile(t, dir, "coredns/Corefile", "edited\n")
	changes, err = Plan(files, dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"update coredns/Corefile"}, paths(changes))
	require.NoError(t, Install(changes, dir))
	assert.Equal(t, live, readFile(t, dir, "coredns/zones/_acme-challenge.example.com"))
}

// Seeded, not managed, is still not permanent: a domain that stops being
// delegated no longer has a challenge zone, and leaving it would serve one for
// a name nothing challenges.
func TestAChallengeZoneStillGoesWhenTheDomainNoLongerHasOne(t *testing.T) {
	dir := t.TempDir()
	delegated, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSDelegation, true)))
	require.NoError(t, err)
	changes, _ := Plan(delegated, dir)
	require.NoError(t, Install(changes, dir))
	writeFile(t, dir, "coredns/zones/_acme-challenge.example.com",
		readFile(t, dir, "coredns/zones/_acme-challenge.example.com")+"\n@ IN TXT \"live\"\n")

	onDemand, err := Render(snapshot(domain("example.com", hostagent.EdgeDNSOnDemand, true)))
	require.NoError(t, err)
	changes, err = Plan(onDemand, dir)
	require.NoError(t, err)
	assert.Contains(t, paths(changes), "remove coredns/zones/_acme-challenge.example.com")
}
