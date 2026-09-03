package templates

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// The zot template is the acceptance test for config files: it is the first
// template whose files carry a credential, and the first user of a derived
// generator. Run against the real catalog checkout when it is present, so a
// change to either side is caught here rather than on a deploy.
func TestZotTemplateResolvesWithWorkingHtpasswd(t *testing.T) {
	const dir = "../../../../meshploy-templates/templates/zot"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("meshploy-templates checkout not present")
	}
	meta, err := os.ReadFile(dir + "/meta.yaml")
	if err != nil {
		t.Fatal(err)
	}
	compose, err := os.ReadFile(dir + "/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(meta)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	tpl := &Template{Manifest: m, Compose: string(compose)}

	spec, vars, exposes, err := tpl.PrepareSpec(nil, "example.com")
	if err != nil {
		t.Fatalf("PrepareSpec: %v", err)
	}
	if refs := References(spec); len(refs) != 0 {
		t.Fatalf("unsubstituted placeholders remain: %v", refs)
	}
	if len(exposes) != 1 || exposes[0].Port != 5000 {
		t.Fatalf("want one exposed port 5000, got %+v", exposes)
	}

	// The generated password must open the htpasswd the template writes --
	// otherwise the registry deploys with a credential nobody can use.
	var doc struct {
		Services map[string]struct {
			XMeshploy struct {
				Files []struct {
					Path    string `yaml:"path"`
					Content string `yaml:"content"`
				} `yaml:"files"`
			} `yaml:"x-meshploy"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(spec), &doc); err != nil {
		t.Fatalf("resolved spec is not valid yaml: %v", err)
	}
	files := doc.Services["zot"].XMeshploy.Files
	if len(files) != 2 {
		t.Fatalf("want 2 config files, got %d", len(files))
	}

	byPath := map[string]string{}
	for _, f := range files {
		byPath[f.Path] = f.Content
	}

	htpasswd := strings.TrimSpace(byPath["/etc/zot/htpasswd"])
	user, hash, ok := strings.Cut(htpasswd, ":")
	if !ok || user != "admin" {
		t.Fatalf("htpasswd is not admin:<hash>: %q", htpasswd)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(vars["ZOT_PASSWORD"])); err != nil {
		t.Fatalf("generated password does not open the htpasswd file: %v", err)
	}
	if strings.Contains(htpasswd, vars["ZOT_PASSWORD"]) {
		t.Fatal("plaintext password leaked into the htpasswd file")
	}

	// The config has to be valid JSON and actually point at the htpasswd, or
	// zot starts wide open with no error anyone would notice.
	var cfg struct {
		HTTP struct {
			Auth struct {
				Htpasswd struct {
					Path string `json:"path"`
				} `json:"htpasswd"`
			} `json:"auth"`
		} `json:"http"`
	}
	if err := json.Unmarshal([]byte(byPath["/etc/zot/config.json"]), &cfg); err != nil {
		t.Fatalf("config.json is not valid json: %v", err)
	}
	if cfg.HTTP.Auth.Htpasswd.Path != "/etc/zot/htpasswd" {
		t.Fatalf("config does not reference the htpasswd file: %q", cfg.HTTP.Auth.Htpasswd.Path)
	}
}
