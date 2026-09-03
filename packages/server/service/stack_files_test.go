package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

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
