package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// .env is the only file in /opt/meshploy that survives an upgrade — the deploy
// tarball overwrites everything else — so these helpers are load-bearing. A
// rewrite that dropped or corrupted a line would take the install's database
// credentials with it.

func withEnvFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	orig := meshployInstDir
	meshployInstDir = dir
	t.Cleanup(func() { meshployInstDir = orig })
	return path
}

func readAll(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}

func TestSetEnvVarReplacesInPlaceAndKeepsEverythingElse(t *testing.T) {
	path := withEnvFile(t, "POSTGRES_PASSWORD=secret\nMESHPLOY_CHANNEL=latest\nDOMAIN=example.com\n")

	if err := setEnvVar("MESHPLOY_CHANNEL", "v1.2.3"); err != nil {
		t.Fatalf("setEnvVar: %v", err)
	}

	got := readAll(t, path)
	if !strings.Contains(got, "MESHPLOY_CHANNEL=v1.2.3") {
		t.Fatalf("value not updated:\n%s", got)
	}
	if strings.Contains(got, "MESHPLOY_CHANNEL=latest") {
		t.Fatalf("old value survived:\n%s", got)
	}
	// The rest of the file is other services' configuration; losing any of it
	// breaks the install.
	for _, keep := range []string{"POSTGRES_PASSWORD=secret", "DOMAIN=example.com"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("unrelated line %q was lost:\n%s", keep, got)
		}
	}
}

func TestSetEnvVarAppendsWhenAbsent(t *testing.T) {
	path := withEnvFile(t, "DOMAIN=example.com\n")

	if err := setEnvVar("MESHPLOY_API_IMAGE", "ghcr.io/meshploy/api-ee"); err != nil {
		t.Fatalf("setEnvVar: %v", err)
	}

	got := readAll(t, path)
	if !strings.Contains(got, "MESHPLOY_API_IMAGE=ghcr.io/meshploy/api-ee") {
		t.Fatalf("value not appended:\n%s", got)
	}
	if !strings.Contains(got, "DOMAIN=example.com") {
		t.Fatalf("existing line was lost:\n%s", got)
	}
}

// A key that is a prefix of another must not be confused for it — matching on a
// bare prefix would rewrite the wrong line.
func TestSetEnvVarDoesNotMatchAPrefixOfAnotherKey(t *testing.T) {
	path := withEnvFile(t, "MESHPLOY_API_IMAGE_OLD=keep-me\nDOMAIN=example.com\n")

	if err := setEnvVar("MESHPLOY_API_IMAGE", "ghcr.io/meshploy/api-ee"); err != nil {
		t.Fatalf("setEnvVar: %v", err)
	}

	got := readAll(t, path)
	if !strings.Contains(got, "MESHPLOY_API_IMAGE_OLD=keep-me") {
		t.Fatalf("a different key sharing the prefix was overwritten:\n%s", got)
	}
	if !strings.Contains(got, "MESHPLOY_API_IMAGE=ghcr.io/meshploy/api-ee") {
		t.Fatalf("the intended key was not set:\n%s", got)
	}
}

func TestReadEnvVar(t *testing.T) {
	withEnvFile(t, "MESHPLOY_API_IMAGE=ghcr.io/meshploy/api-ee\nDOMAIN=example.com\n")

	if got := readEnvVar("MESHPLOY_API_IMAGE"); got != "ghcr.io/meshploy/api-ee" {
		t.Fatalf("readEnvVar returned %q", got)
	}
	// Absent keys are not an error: an unset override means "use the default".
	if got := readEnvVar("NOT_PRESENT"); got != "" {
		t.Fatalf("absent key should read as empty, got %q", got)
	}
}

// The username install.sh recorded must win. Reading it from .env rather than
// the environment is the whole point: server-upgrade runs under sudo, which
// resets the environment, so an exported GHCR_USER would never arrive.
func TestGhcrUserPrefersTheRecordedAccount(t *testing.T) {
	withEnvFile(t, "GHCR_USER=meshploy-acme\nDOMAIN=example.com\n")
	if got := ghcrUser(); got != "meshploy-acme" {
		t.Fatalf("recorded username should win, got %q", got)
	}

	// An install that declined registry login records nothing. ghcr does not
	// validate this field, so the fallback only has to be well-formed.
	withEnvFile(t, "DOMAIN=example.com\n")
	if got := ghcrUser(); got != "x-access-token" {
		t.Fatalf("with nothing recorded the fallback should be x-access-token, got %q", got)
	}
}

// Probing :latest on an edge install would report the image unreachable when it
// is perfectly reachable under the tag compose actually resolves.
func TestPullChannelFollowsTheConfiguredChannel(t *testing.T) {
	withEnvFile(t, "MESHPLOY_CHANNEL=main\n")
	if got := pullChannel(); got != "main" {
		t.Fatalf("configured channel should win, got %q", got)
	}

	withEnvFile(t, "DOMAIN=example.com\n")
	if got := pullChannel(); got != "latest" {
		t.Fatalf("unset channel should fall back to latest, got %q", got)
	}
}

// An install with no override is a CE install; that is what decides whether the
// upgrade notice is shown.
func TestCurrentAPIImageFallsBackToCE(t *testing.T) {
	withEnvFile(t, "DOMAIN=example.com\n")
	if got := currentAPIImage(); got != ceImage {
		t.Fatalf("with no override the image should be %q, got %q", ceImage, got)
	}

	withEnvFile(t, "MESHPLOY_API_IMAGE=ghcr.io/meshploy/api-ee\n")
	if got := currentAPIImage(); got != "ghcr.io/meshploy/api-ee" {
		t.Fatalf("the override should win, got %q", got)
	}
}
