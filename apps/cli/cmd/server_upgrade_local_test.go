package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withInstDir(t *testing.T, env string) string {
	t.Helper()
	dir := t.TempDir()
	orig := meshployInstDir
	meshployInstDir = dir
	t.Cleanup(func() { meshployInstDir = orig })
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A bundle's deploy/ is staged like a release, never carrying a file an
// upgrade must not write.
func TestLocalBundleStagesDeployWithoutProtectedFiles(t *testing.T) {
	from := t.TempDir()
	for name, body := range map[string]string{
		"deploy/docker-compose.yml":           "services: {}",
		"deploy/install.sh":                   "#!/bin/sh",
		"deploy/caddy/Caddyfile":              "rendered on the server",
		"deploy/headscale/data/db.sqlite":     "state",
		"deploy/headscale/config/config.yaml": "rendered",
		"deploy/caddy/conf.d/operator.caddy":  "the operator's",
	} {
		p := filepath.Join(from, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	staged := t.TempDir()
	if err := stageLocalDeploy(from, staged); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"docker-compose.yml", "install.sh"} {
		if _, err := os.Stat(filepath.Join(staged, want)); err != nil {
			t.Errorf("%s not staged", want)
		}
	}
	for _, never := range []string{"caddy/Caddyfile", "headscale/data/db.sqlite", "headscale/config/config.yaml", "caddy/conf.d/operator.caddy"} {
		if _, err := os.Stat(filepath.Join(staged, never)); err == nil {
			t.Errorf("%s staged", never)
		}
	}
	if err := stageLocalDeploy(t.TempDir(), staged); err == nil {
		t.Error("a folder with no deploy/ taken as a bundle")
	}
}

// An image on the local tag that was not shipped comes from edge; one that is
// here stays; the rest are pulled.
func TestLocalImagesNotShippedComeFromEdge(t *testing.T) {
	dir := withInstDir(t, "MESHPLOY_CHANNEL=local\n")
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(`services:
  api:
    image: ghcr.io/meshploy/api:${MESHPLOY_CHANNEL:-latest}
  web:
    image: ghcr.io/meshploy/web:${MESHPLOY_CHANNEL:-latest}
  registry:
    image: docker.io/library/registry:2
`), 0o644)
	var ran []string
	origOut, origExec := runtimeOutput, runtimeExec
	t.Cleanup(func() { runtimeOutput, runtimeExec = origOut, origExec })
	runtimeOutput = func(_, _ string, args ...string) ([]byte, error) {
		if args[0] == "image" && args[2] == "ghcr.io/meshploy/api:local" {
			return []byte("[]"), nil // shipped
		}
		return nil, os.ErrNotExist
	}
	runtimeExec = func(_ string, args ...string) error {
		ran = append(ran, strings.Join(args, " "))
		return nil
	}
	if err := ensureLocalImages(dir, "docker"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"pull --quiet ghcr.io/meshploy/web:main",
		"tag ghcr.io/meshploy/web:main ghcr.io/meshploy/web:local",
		"pull --quiet docker.io/library/registry:2",
	}
	if strings.Join(ran, "\n") != strings.Join(want, "\n") {
		t.Errorf("ran:\n%s\nwant:\n%s", strings.Join(ran, "\n"), strings.Join(want, "\n"))
	}
}

// A shipped builder goes to the gateway's registry and the API is pointed at
// it by its mesh name; an ordinary upgrade points it back.
func TestLocalBuilderIsPublishedAndForgotten(t *testing.T) {
	withInstDir(t, "MESH_IP=100.64.0.1\n")
	var ran []string
	origExec := runtimeExec
	t.Cleanup(func() { runtimeExec = origExec })
	runtimeExec = func(_ string, args ...string) error { ran = append(ran, strings.Join(args, " ")); return nil }

	if err := publishLocalBuilder("docker", []string{"ghcr.io/meshploy/api:local"}); err != nil || len(ran) != 0 {
		t.Fatalf("published a builder that was not shipped: %v %v", ran, err)
	}
	if err := publishLocalBuilder("docker", []string{"ghcr.io/meshploy/builder:local"}); err != nil {
		t.Fatal(err)
	}
	if got := readEnvVar("BUILDER_IMAGE"); got != "100.64.0.1:5000/meshploy/builder:local" {
		t.Errorf("BUILDER_IMAGE = %q", got)
	}
	if !strings.Contains(strings.Join(ran, "\n"), "push --quiet 127.0.0.1:5000/meshploy/builder:local") {
		t.Errorf("ran %v", ran)
	}
	forgetLocalBuilder()
	if got := readEnvVar("BUILDER_IMAGE"); got != "" {
		t.Errorf("BUILDER_IMAGE kept: %q", got)
	}
}

// Only services whose image was shipped are recreated; a local build
// upgrades to edge, the channel it was made from.
func TestLocalRecreatesShippedServicesAndUpgradesToEdge(t *testing.T) {
	got := localServicesToRecreate([]string{"ghcr.io/meshploy/web:local", "ghcr.io/meshploy/builder:local", "ghcr.io/meshploy/api:local"})
	if strings.Join(got, ",") != "api,web" {
		t.Errorf("recreate %v", got)
	}
	withInstDir(t, "MESHPLOY_CHANNEL=local\n")
	if c := currentUpgradeChannel(); c != channelEdge {
		t.Errorf("a local build upgrades to %s", c)
	}
}
