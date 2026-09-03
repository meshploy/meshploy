package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// loadZotExt runs the real zot compose through the real compose-go loader and
// returns the decoded x-meshploy block for the zot service.
//
// The seam this covers: `files:` is a list of maps whose `content` is a YAML
// block scalar holding JSON. compose-go has to carry it through the extension
// map untouched, and decodeExt has to round-trip it through JSON. Nothing else
// in the suite exercises that, and the zot template is unusable if it breaks.
func loadZotExt(t *testing.T) *meshployExt {
	t.Helper()
	const path = "../../../../meshploy-templates/templates/zot/docker-compose.yml"
	spec, err := os.ReadFile(path)
	if err != nil {
		t.Skip("meshploy-templates checkout not present")
	}
	// The catalog spec is unresolved here; ${ZOT_HTPASSWD} is a placeholder,
	// which is fine -- this test is about structure surviving the parse.
	proj, err := loader.LoadWithContext(context.Background(), composetypes.ConfigDetails{
		ConfigFiles: []composetypes.ConfigFile{{Filename: "docker-compose.yml", Content: spec}},
	}, func(o *loader.Options) {
		o.SkipValidation = true
		o.SkipInterpolation = true
		o.SetProjectName("test", true)
	})
	if err != nil {
		t.Fatalf("compose load: %v", err)
	}
	svc, ok := proj.Services["zot"]
	if !ok {
		t.Fatal("no zot service in the parsed project")
	}
	ext := decodeExt(svc.Extensions)
	if ext == nil {
		t.Fatal("x-meshploy block did not survive the compose parse")
	}
	return ext
}

func TestZotComposeCarriesConfigFiles(t *testing.T) {
	ext := loadZotExt(t)

	if len(ext.Files) != 2 {
		t.Fatalf("want 2 files through the parser, got %d", len(ext.Files))
	}
	byPath := map[string]string{}
	for _, f := range ext.Files {
		if !strings.HasPrefix(f.Path, "/") {
			t.Errorf("path %q is not absolute -- the service layer rejects it", f.Path)
		}
		byPath[f.Path] = f.Content
	}

	cfg, ok := byPath["/etc/zot/config.json"]
	if !ok {
		t.Fatal("config.json missing after the parse")
	}
	// A block scalar that lost its newlines would still be a string, so check
	// the content actually parses rather than merely that it is non-empty.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(cfg), &parsed); err != nil {
		t.Fatalf("config.json did not survive as valid json: %v", err)
	}
	if _, ok := byPath["/etc/zot/htpasswd"]; !ok {
		t.Fatal("htpasswd missing after the parse")
	}
	if !strings.Contains(byPath["/etc/zot/htpasswd"], "${ZOT_HTPASSWD}") {
		t.Errorf("htpasswd placeholder was mangled: %q", byPath["/etc/zot/htpasswd"])
	}
}

// A config file's body is literal data, and compose-go expands $NAME in the
// spec. A bcrypt hash is $2a$10$<salt>… and the salt starts with a letter, so
// the interpolating parse the apply path uses reads it as an undefined variable
// and deletes it — leaving a truncated hash, an htpasswd that matches nothing,
// and a registry that rejects its own generated password with no error anywhere.
//
// This runs the loader exactly as Apply does, which the parse test above does
// not: that one sets SkipInterpolation, so it proved the block survives YAML
// while missing the thing that actually broke.
func TestConfigFileContentSurvivesComposeInterpolation(t *testing.T) {
	const password = "SomeGeneratedPass123456AB"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 4) // cheap for tests
	if err != nil {
		t.Fatal(err)
	}

	spec := `services:
  app:
    image: nginx:alpine
    environment:
      GREETING: ${GREETING}
    x-meshploy:
      type: application
      files:
        - path: /etc/app/htpasswd
          content: |
            admin:` + string(hash) + `
        - path: /etc/nginx/snippet.conf
          content: |
            proxy_set_header Host $host;
            add_header X-Real-IP $remote_addr;
`

	svc := &StackService{}
	files := svc.uninterpolatedFiles(context.Background(), spec)["app"]
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d", len(files))
	}

	byPath := map[string]string{}
	for _, f := range files {
		byPath[f.Path] = f.Content
	}

	_, gotHash, ok := strings.Cut(strings.TrimSpace(byPath["/etc/app/htpasswd"]), ":")
	if !ok {
		t.Fatalf("htpasswd is not user:hash: %q", byPath["/etc/app/htpasswd"])
	}
	if gotHash != string(hash) {
		t.Errorf("hash was altered:\n  want %q\n  got  %q", hash, gotHash)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(gotHash), []byte(password)); err != nil {
		t.Errorf("the password no longer opens its own hash: %v", err)
	}

	// nginx directives are the same hazard with a friendlier name.
	nginx := byPath["/etc/nginx/snippet.conf"]
	for _, want := range []string{"$host", "$remote_addr"} {
		if !strings.Contains(nginx, want) {
			t.Errorf("%s was eaten by interpolation: %q", want, nginx)
		}
	}
}

// Interpolation must stay on for everything that is not a file: service
// environment blocks are how a stack variable reaches a container, and turning
// it off wholesale to fix the files would have broken that instead.
func TestStackEnvironmentStillInterpolates(t *testing.T) {
	spec := `services:
  app:
    image: nginx:alpine
    environment:
      GREETING: ${GREETING}
`
	project, err := loader.LoadWithContext(context.Background(), composetypes.ConfigDetails{
		WorkingDir:  "/",
		ConfigFiles: []composetypes.ConfigFile{{Filename: "docker-compose.yml", Content: []byte(spec)}},
		Environment: map[string]string{"GREETING": "hello"},
	}, loader.WithSkipValidation)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := project.Services["app"].Environment["GREETING"]
	if got == nil || *got != "hello" {
		t.Fatalf("stack variables must still reach the environment, got %v", got)
	}
}
