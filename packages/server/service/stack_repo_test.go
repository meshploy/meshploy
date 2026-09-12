package service

import (
	"os"
	"path/filepath"
	"testing"
)

// A stack's compose path, or a symlink in its repository, cannot read a file
// outside the checkout: it would land in the stack's spec for its editor to
// read, and the API's own environment is one such file.
func TestReadFileFromCloneStaysInTheCheckout(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "secret.env"), []byte("JWT_SECRET=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "secret.env"), filepath.Join(repo, "link.yml")); err != nil {
		t.Fatal(err)
	}

	if data, err := readFileFromClone(repo, "docker-compose.yml"); err != nil || string(data) != "services: {}\n" {
		t.Fatalf("a file in the checkout: %q, %v", data, err)
	}
	for _, p := range []string{"../secret.env", filepath.Join(base, "secret.env"), "link.yml"} {
		if data, err := readFileFromClone(repo, p); err == nil {
			t.Errorf("%s was read from outside the checkout: %q", p, data)
		}
	}
}
