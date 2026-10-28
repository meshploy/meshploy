package edgeconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/meshploy/packages/hostagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const otherProviderID = "5d2a9e14-3b7c-4c08-8f6e-9a1b2c3d4e5f"

func providerSnapshot() hostagent.EdgeSnapshot {
	return snapshot(withProvider(domain("example.com", hostagent.EdgeDNSOnDemand, true)))
}

// A token is written root-only, rewritten only when it changes, and a
// provider with no token anywhere stops the apply before Caddy is asked.
func TestInstallSecrets(t *testing.T) {
	dir := t.TempDir()
	snap := providerSnapshot()

	_, err := InstallSecrets(snap, nil, dir)
	require.Error(t, err, "a provider with no token must stop the apply")

	changed, err := InstallSecrets(snap, hostagent.DNSCredentials{testProviderID: "tok-1\n"}, dir)
	require.NoError(t, err)
	assert.True(t, changed)
	path := filepath.Join(dir, SecretsDir, testProviderID)
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "tok-1", string(body), "stored trimmed, as Caddy reads it whole")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	info, err = os.Stat(filepath.Join(dir, SecretsDir))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	changed, err = InstallSecrets(snap, hostagent.DNSCredentials{testProviderID: "tok-1"}, dir)
	require.NoError(t, err)
	assert.False(t, changed, "the same token is no change, so Caddy is not reloaded for it")

	changed, err = InstallSecrets(snap, nil, dir)
	require.NoError(t, err)
	assert.False(t, changed, "an apply with no tokens keeps the ones already here")

	changed, err = InstallSecrets(snap, hostagent.DNSCredentials{testProviderID: "tok-2"}, dir)
	require.NoError(t, err)
	assert.True(t, changed, "a new token needs a reload even when the Caddyfile is the same")
}

// A provider no domain uses any more loses its token, and nothing else in the
// directory is touched by a snapshot that still names it.
func TestRemoveStaleSecrets(t *testing.T) {
	dir := t.TempDir()
	snap := providerSnapshot()
	_, err := InstallSecrets(snap, hostagent.DNSCredentials{testProviderID: "a"}, dir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, SecretsDir, otherProviderID), []byte("b"), 0o600))

	require.NoError(t, RemoveStaleSecrets(snap, dir))
	assert.FileExists(t, filepath.Join(dir, SecretsDir, testProviderID))
	assert.NoFileExists(t, filepath.Join(dir, SecretsDir, otherProviderID))

	require.NoError(t, RemoveStaleSecrets(snapshot(domain("example.com", hostagent.EdgeDNSOnDemand, true)), dir))
	assert.NoFileExists(t, filepath.Join(dir, SecretsDir, testProviderID))
}

// Caddy's DNS modules quote a token they reject; the apply's result must not.
func TestRedactHidesEveryToken(t *testing.T) {
	dir := t.TempDir()
	_, err := InstallSecrets(providerSnapshot(), hostagent.DNSCredentials{testProviderID: "on-disk"}, dir)
	require.NoError(t, err)
	got := Redact("API token 'on-disk' and 'handed-over' appear invalid", hostagent.DNSCredentials{otherProviderID: "handed-over"}, dir)
	assert.Equal(t, "API token '[token]' and '[token]' appear invalid", got)
}
