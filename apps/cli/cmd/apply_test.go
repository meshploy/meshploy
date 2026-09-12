package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// meshploy apply carries the files a manifest's configs and secrets name by
// file:, read relative to the manifest, keyed by the path as written.
func TestManifestFilesReadsConfigAndSecretFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("keycloak/realm.json", `{"realm":"pf"}`)
	write("secrets/api.txt", "s3cret")
	manifest := []byte(`
services:
  keycloak:
    image: quay.io/keycloak/keycloak:26.0
configs:
  realm:
    file: ./keycloak/realm.json
  inline:
    content: "not a file"
secrets:
  api:
    file: secrets/api.txt
`)
	files, err := manifestFiles(filepath.Join(dir, "compose.yml"), manifest)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"./keycloak/realm.json": `{"realm":"pf"}`, "secrets/api.txt": "s3cret"}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("files = %v", files)
	}

	// A missing file stops the apply, as it stops docker compose.
	missing := []byte("configs:\n  gone:\n    file: ./gone.json\n")
	if _, err := manifestFiles(filepath.Join(dir, "compose.yml"), missing); err == nil {
		t.Error("a missing file was not reported")
	}
}
