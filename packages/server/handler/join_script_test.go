package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/middleware"
)

// The Windows and macOS join scripts are served like install.sh: anonymously,
// to a machine holding nothing but a provisioning token, with this gateway's
// address written in so the command needs no --api.
func TestJoinScriptsAreServedWithTheGatewaysAddress(t *testing.T) {
	dir := t.TempDir()
	for name, script := range joinScripts {
		body := "first line\n" + script.placeholder + "\nlast line\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A file in the directory that is not a join script must never be served.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("private"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := joinScriptDir
	joinScriptDir = dir
	t.Cleanup(func() { joinScriptDir = old })

	h := New(&config.Config{APIBaseURL: "https://api.example.com"}, nil)
	r := chi.NewRouter()
	r.Use(middleware.RequireAuth)
	h.RegisterRaw(r)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	t.Run("macOS, filled in", func(t *testing.T) {
		rec := get("/join/macos.sh")
		if rec.Code != http.StatusOK {
			t.Fatalf("anonymous GET = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `MESHPLOY_API_BASE="${MESHPLOY_API_BASE:-https://api.example.com}"`) {
			t.Errorf("address not written in:\n%s", rec.Body.String())
		}
	})

	t.Run("Windows, filled in", func(t *testing.T) {
		rec := get("/join/windows.ps1")
		if rec.Code != http.StatusOK {
			t.Fatalf("anonymous GET = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `$DefaultApiBase = "https://api.example.com"`) {
			t.Errorf("address not written in:\n%s", rec.Body.String())
		}
	})

	t.Run("nothing else under /join/ is public or served", func(t *testing.T) {
		if rec := get("/join/notes.txt"); rec.Code == http.StatusOK {
			t.Fatalf("a file that is not a join script was served: %s", rec.Body.String())
		}
		if rec := get("/join/../install.sh"); rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "private") {
			t.Fatal("path traversal reached another file")
		}
	})
}

// The real scripts must carry the lines the handler fills in, and send what
// registration needs to record the machine as mesh only. They are written in
// bash and PowerShell, in another directory, and only a test keeps them in step
// with this file.
func TestTheRealJoinScriptsAgreeWithTheAPI(t *testing.T) {
	root := filepath.Join("..", "..", "..", "deploy", "join")
	checks := map[string][]string{
		"macos.sh":    {`"os\":\"darwin\"`, `\"mesh_role\":\"mesh\"`, "--token=*)", "NODE_SECRET=", "/api/v1/nodes/provision"},
		"windows.ps1": {`os = "windows"`, `mesh_role = "mesh"`, "[string]$Token", "NODE_SECRET=", "/api/v1/nodes/provision", "--unattended"},
	}
	for name, script := range joinScripts {
		body, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("deploy/join/%s: %v", name, err)
		}
		if !strings.Contains(string(body), script.placeholder) {
			t.Errorf("deploy/join/%s no longer contains %q, so the gateway cannot write its address into it", name, script.placeholder)
		}
		for _, want := range checks[name] {
			if !strings.Contains(string(body), want) {
				t.Errorf("deploy/join/%s does not contain %q", name, want)
			}
		}
	}
}
