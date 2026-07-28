package templates

import (
	"strings"
	"testing"
	"testing/fstest"
)

// fixtureFS is a self-contained template used to exercise the conversion engine.
// It is NOT a copy of the catalog — the real catalog lives in the meshploy-
// templates repo and is validated there. This only tests engine behaviour.
func fixtureFS() fstest.MapFS {
	return fstest.MapFS{
		"web-app/meta.yaml": &fstest.MapFile{Data: []byte(`id: web-app
name: Web App
category: application
version: 1.0.0
variables:
  - key: ADMIN_EMAIL
    prompt: "Admin email"
    required: true
  - key: ADMIN_PASSWORD
    generate: password
  - key: PRIMARY_DOMAIN
    generate: subdomain
    expose:
      service: app
      port: 8080
`)},
		"web-app/docker-compose.yml": &fstest.MapFile{Data: []byte(`services:
  app:
    image: example/app:1.2.3
    environment:
      ADMIN_USER: ${ADMIN_EMAIL}
      ADMIN_PASS: ${ADMIN_PASSWORD}
    x-meshploy:
      type: application
      deploy:
        port: 8080
`)},
	}
}

func TestPrepareSpec(t *testing.T) {
	tpl, err := Load(fixtureFS(), "web-app")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	spec, vars, exposes, err := tpl.PrepareSpec(
		map[string]string{"ADMIN_EMAIL": "admin@acme.com"},
		"acme.com",
	)
	if err != nil {
		t.Fatalf("PrepareSpec: %v", err)
	}

	if !strings.Contains(spec, "admin@acme.com") {
		t.Error("resolved spec missing prompted email")
	}
	if refs := References(spec); len(refs) != 0 {
		t.Errorf("resolved spec still has placeholders: %v", refs)
	}
	// The x-meshploy block survives: the compose IS the meshploy spec, no format
	// conversion — only substitution.
	if !strings.Contains(spec, "x-meshploy") {
		t.Error("resolved spec lost the x-meshploy block")
	}

	if pw := vars["ADMIN_PASSWORD"]; len(pw) < 16 || strings.Contains(spec, "${ADMIN_PASSWORD}") {
		t.Errorf("password not generated/substituted: %q", pw)
	}

	if len(exposes) != 1 {
		t.Fatalf("expected 1 expose, got %d", len(exposes))
	}
	e := exposes[0]
	if e.Service != "app" || e.Port != 8080 {
		t.Errorf("expose = %+v, want app:8080", e)
	}
	if !strings.HasPrefix(e.Hostname, "web-app-") || !strings.HasSuffix(e.Hostname, ".acme.com") {
		t.Errorf("hostname = %q, want web-app-<rand>.acme.com", e.Hostname)
	}
	if vars["PRIMARY_DOMAIN"] != e.Hostname {
		t.Errorf("PRIMARY_DOMAIN %q != expose hostname %q", vars["PRIMARY_DOMAIN"], e.Hostname)
	}
}

func TestPrepareSpec_MissingRequired(t *testing.T) {
	tpl, err := Load(fixtureFS(), "web-app")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := tpl.PrepareSpec(map[string]string{}, "acme.com"); err == nil {
		t.Fatal("expected error for missing required ADMIN_EMAIL")
	}
}

func TestSubstitute_Unresolved(t *testing.T) {
	if _, err := Substitute("image: ${MISSING}", map[string]string{}); err == nil {
		t.Fatal("expected unresolved-variable error")
	}
}

func TestManifestValidation(t *testing.T) {
	bad := []byte("id: x\nvariables:\n  - key: K\n    prompt: \"p\"\n    generate: password\n")
	if _, err := ParseManifest(bad); err == nil {
		t.Fatal("expected error for variable that is both prompt and generate")
	}
}
