package mcpserver

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meshploy/packages/client"
)

// fakeAPI answers what deploy_folder and share_app ask of the API: one
// project, services made on request, an upload, a deployment that finishes
// with finalStatus, and a route to the service.
type fakeAPI struct {
	mu       sync.Mutex
	services []client.Service
	created  map[string]any
	upload   struct {
		name, deploy string
		files        []string
	}
	polls       int
	finalStatus string
	shareStatus int // 0: no sharing route, as on Community
	shared      map[string]any
}

func (f *fakeAPI) server(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	base := "/api/v1/orgs/org-1/projects"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		j(w, 200, []client.Project{{ID: "p1", Name: "Tools", Slug: "tools"}})
	})
	mux.HandleFunc("GET "+base+"/p1/services", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		j(w, 200, f.services)
	})
	mux.HandleFunc("POST "+base+"/p1/services", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&f.created)
		svc := client.Service{ID: "s1", Name: f.created["name"].(string), Type: "application"}
		f.services = append(f.services, svc)
		j(w, 201, svc)
	})
	mux.HandleFunc("POST "+base+"/p1/services/s1/source", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.upload.name, f.upload.deploy = r.URL.Query().Get("name"), r.URL.Query().Get("deploy")
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Errorf("upload is not gzip: %v", err)
			return
		}
		tr := tar.NewReader(zr)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("upload is not a tar: %v", err)
				return
			}
			if h.Typeflag == tar.TypeReg {
				f.upload.files = append(f.upload.files, h.Name)
			}
		}
		j(w, 201, client.UploadResult{
			Source:     client.UploadedSource{Digest: "sha256:abc", Name: f.upload.name, Files: len(f.upload.files)},
			Deployment: &client.Deployment{ID: "d1", Status: "pending"},
		})
	})
	mux.HandleFunc("GET "+base+"/p1/services/s1/deployments/d1", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.polls++
		status := "building"
		if f.polls > 1 {
			status = f.finalStatus
		}
		j(w, 200, client.Deployment{ID: "d1", Status: status, Log: "step 1\nstep 2\nnpm ERR! missing script: start\n"})
	})
	mux.HandleFunc("GET "+base+"/p1/routes", func(w http.ResponseWriter, r *http.Request) {
		s1 := "s1"
		j(w, 200, []client.Route{
			{ID: "r1", Hostname: "todo.example.com", ServiceID: &s1, Published: true},
			{ID: "r2", Hostname: "paused.example.com", ServiceID: &s1, Published: false},
		})
	})
	mux.HandleFunc("POST "+base+"/p1/services/s1/shares", func(w http.ResponseWriter, r *http.Request) {
		if f.shareStatus == 0 {
			http.NotFound(w, r) // the router's plain-text 404, as on Community
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&f.shared)
		j(w, f.shareStatus, map[string]any{"shared": f.shared["emails"]})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func appFolder(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Todo App")
	for name, body := range map[string]string{
		"package.json": "{}", "src/index.js": "x", "node_modules/a/index.js": "dep", ".env": "SECRET=1",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func fastPolling(t *testing.T) {
	saved := deployPoll
	deployPoll = time.Millisecond
	t.Cleanup(func() { deployPoll = saved })
}

// A folder becomes a service named after it, is uploaded without what must
// not leave the machine, and the tool answers once it is up, with its link.
func TestDeployFolderMakesTheServiceAndReturnsItsLink(t *testing.T) {
	fastPolling(t)
	f := &fakeAPI{finalStatus: "success"}
	s := &srv{c: client.New(f.server(t).URL, "t"), orgID: "org-1"}

	res := callTool(t, s.handleDeployFolder, map[string]any{"project_id": "tools", "path": appFolder(t), "port": "8080"})
	if res.IsError {
		t.Fatalf("deploy_folder failed: %+v", res.Content)
	}
	if f.created["name"] != "todo-app" || f.created["from_upload"] != true {
		t.Errorf("service made as %v", f.created)
	}
	if ports, _ := f.created["ports"].([]any); len(ports) != 1 || ports[0].(map[string]any)["port"] != float64(8080) {
		t.Errorf("ports %v", f.created["ports"])
	}
	if f.upload.name != "Todo App" || f.upload.deploy != "true" {
		t.Errorf("upload %+v", f.upload)
	}
	if strings.Join(f.upload.files, ",") != "package.json,src/index.js" {
		t.Errorf("uploaded %v", f.upload.files)
	}
	b, _ := json.Marshal(res.StructuredContent)
	for _, want := range []string{`"status":"success"`, `"links":["https://todo.example.com"]`, `"service_created":true`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("answer lacks %s: %s", want, b)
		}
	}
}

// A build that fails comes back as an error carrying the end of its log, for
// the agent to fix and try again.
func TestDeployFolderReportsAFailedBuildWithItsLog(t *testing.T) {
	fastPolling(t)
	f := &fakeAPI{finalStatus: "failed"}
	s := &srv{c: client.New(f.server(t).URL, "t"), orgID: "org-1"}
	res := callTool(t, s.handleDeployFolder, map[string]any{"project_id": "tools", "path": appFolder(t)})
	if !res.IsError {
		t.Fatal("a failed deployment reported as done")
	}
	b, _ := json.Marshal(res.Content)
	if !strings.Contains(string(b), "missing script: start") {
		t.Errorf("no log in the error: %s", b)
	}
}

// On Community there is no sharing: the tool says so, as an answer to pass
// on, not a failure.
func TestShareAppOnCommunitySaysWhereSharingIs(t *testing.T) {
	f := &fakeAPI{services: []client.Service{{ID: "s1", Name: "todo-app"}}}
	s := &srv{c: client.New(f.server(t).URL, "t"), orgID: "org-1"}
	res := callTool(t, s.handleShareApp, map[string]any{"project_id": "tools", "service": "todo-app", "emails": "asha@example.com, ravi@example.com"})
	if res.IsError {
		t.Fatalf("an answer reported as a failure: %+v", res.Content)
	}
	b, _ := json.Marshal(res.Content)
	for _, want := range []string{"Community", "Enterprise", "Meshploy Cloud", "asha@example.com, ravi@example.com"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("answer lacks %q: %s", want, b)
		}
	}
}

// Where sharing exists, the addresses go to it and its answer comes back.
func TestShareAppSendsTheAddresses(t *testing.T) {
	f := &fakeAPI{services: []client.Service{{ID: "s1", Name: "todo-app"}}, shareStatus: 201}
	s := &srv{c: client.New(f.server(t).URL, "t"), orgID: "org-1"}
	res := callTool(t, s.handleShareApp, map[string]any{"project_id": "tools", "service": "todo-app", "emails": "asha@example.com ravi@example.com", "role": "editor"})
	if res.IsError {
		t.Fatalf("share failed: %+v", res.Content)
	}
	if f.shared["role"] != "editor" || len(f.shared["emails"].([]any)) != 2 {
		t.Errorf("sent %v", f.shared)
	}

	for _, args := range []map[string]any{
		{"project_id": "tools", "service": "todo-app", "emails": "not-an-address"},
		{"project_id": "tools", "service": "todo-app", "emails": "a@example.com", "role": "owner"},
	} {
		if res := callTool(t, s.handleShareApp, args); !res.IsError {
			t.Errorf("accepted %v", args)
		}
	}
}

func TestAppToolsAreRegistered(t *testing.T) {
	ms := New(client.New("http://127.0.0.1:0", "t"), "org-1")
	for _, name := range []string{"deploy_folder", "share_app"} {
		if ms.GetTool(name) == nil {
			t.Errorf("%s is not on the MCP surface", name)
		}
	}
}
