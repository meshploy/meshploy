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
