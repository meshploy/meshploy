package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/meshploy/apps/cli/internal/mesh"
)

// serveAssets stands in for GitHub: every release carries files, and a
// SHA256SUMS listing them, with wrong[name] listed in place of name's hash.
func serveAssets(t *testing.T, files map[string][]byte, wrong map[string]string) {
	t.Helper()
	var srv *httptest.Server
	var sums strings.Builder
	for name, b := range files {
		h := sha256hex(b)
		if w, ok := wrong[name]; ok {
			h = w
		}
		fmt.Fprintf(&sums, "%s  %s\n", h, name)
	}
	mux := http.NewServeMux()
	release := func(w http.ResponseWriter, _ *http.Request) {
		assets := []map[string]string{{"name": checksumAsset, "url": srv.URL + "/assets/" + checksumAsset}}
		for name := range files {
			assets = append(assets, map[string]string{"name": name, "url": srv.URL + "/assets/" + name})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"assets": assets})
	}
	mux.HandleFunc("/repos/"+githubRepo+"/releases/latest", release)
	mux.HandleFunc("/repos/"+githubRepo+"/releases/tags/", release)
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/assets/")
		if name == checksumAsset {
			_, _ = w.Write([]byte(sums.String()))
			return
		}
		_, _ = w.Write(files[name])
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	orig := githubAPI
	githubAPI = srv.URL
	t.Cleanup(func() { githubAPI = orig })
}

// installedMesh puts a mesh holding these binaries in a folder of its own, as
// root, and counts its restarts.
func installedMesh(t *testing.T, daemon, client string) *int {
	t.Helper()
	dir := t.TempDir()
	bins := map[string]string{"tailscaled": filepath.Join(dir, "tailscaled"), "tailscale": filepath.Join(dir, "tailscale")}
	for bin, body := range map[string]string{"tailscaled": daemon, "tailscale": client} {
		if err := os.WriteFile(bins[bin], []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	restarts := 0
	origBins, origRoot, origRestart := meshBinaries, meshAsRoot, meshRestart
	t.Cleanup(func() { meshBinaries, meshAsRoot, meshRestart = origBins, origRoot, origRestart })
	meshBinaries = bins
	meshAsRoot = func() bool { return true }
	meshRestart = func() error { restarts++; return nil }
	return &restarts
}

func assertMesh(t *testing.T, daemon, client string) {
	t.Helper()
	for bin, want := range map[string]string{"tailscaled": daemon, "tailscale": client} {
		if got, err := os.ReadFile(meshBinaries[bin]); err != nil || string(got) != want {
			t.Errorf("%s = %q (%v), want %q", bin, got, err, want)
		}
	}
}

func meshRelease(daemon, client string) map[string][]byte {
	return map[string][]byte{
		mesh.Asset("tailscaled", runtime.GOARCH): []byte(daemon),
		mesh.Asset("tailscale", runtime.GOARCH):  []byte(client),
	}
}

func refreshFromLatest(t *testing.T) (string, error) {
	t.Helper()
	assets, err := releaseAssets("", "latest")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = refreshMesh(&out, "", "latest", assets)
	return out.String(), err
}

// A release with another mesh build replaces it and restarts the mesh once.
func TestARefreshReplacesAnOlderMesh(t *testing.T) {
	restarts := installedMesh(t, "daemon 1.102", "client 1.102")
	serveAssets(t, meshRelease("daemon 1.104", "client 1.104"), nil)

	out, err := refreshFromLatest(t)
	if err != nil {
		t.Fatal(err)
	}
	assertMesh(t, "daemon 1.104", "client 1.104")
	if *restarts != 1 {
		t.Errorf("restarted %d times, want once", *restarts)
	}
	if !strings.Contains(out, "updated") {
		t.Errorf("output %q does not say the mesh was updated", out)
	}
}

// The same build costs no download and no restart.
func TestARefreshLeavesACurrentMeshRunning(t *testing.T) {
	restarts := installedMesh(t, "daemon 1.104", "client 1.104")
	serveAssets(t, meshRelease("daemon 1.104", "client 1.104"), nil)

	out, err := refreshFromLatest(t)
	if err != nil {
		t.Fatal(err)
	}
	if *restarts != 0 || !strings.Contains(out, "current") {
		t.Errorf("restarted %d times, output %q: want no restart, said current", *restarts, out)
	}
}

// A download that does not match the release's checksums is never installed,
// and the mesh keeps running on what it had.
func TestARefreshRefusesAMeshThatDoesNotMatch(t *testing.T) {
	restarts := installedMesh(t, "daemon 1.102", "client 1.102")
	daemon := mesh.Asset("tailscaled", runtime.GOARCH)
	serveAssets(t, meshRelease("daemon 1.104", "client 1.104"), map[string]string{daemon: strings.Repeat("0", 64)})

	if _, err := refreshFromLatest(t); err == nil {
		t.Fatal("refreshed from a download that does not match its checksum")
	}
	if got, _ := os.ReadFile(meshBinaries["tailscaled"]); string(got) != "daemon 1.102" {
		t.Errorf("tailscaled = %q, want the one installed", got)
	}
	if *restarts != 0 {
		t.Errorf("restarted %d times on a failed download", *restarts)
	}
}

// A release that publishes no mesh build leaves the mesh as it is.
func TestARefreshFromAReleaseWithoutAMesh(t *testing.T) {
	restarts := installedMesh(t, "daemon 1.102", "client 1.102")
	serveAssets(t, map[string][]byte{testAsset: []byte("cli")}, nil)

	if _, err := refreshFromLatest(t); err != nil {
		t.Fatal(err)
	}
	assertMesh(t, "daemon 1.102", "client 1.102")
	if *restarts != 0 {
		t.Errorf("restarted %d times", *restarts)
	}
}

// update brings the mesh along with the CLI, as root; without root it says how.
func TestUpdateBringsTheMeshAlong(t *testing.T) {
	files := meshRelease("daemon 1.104", "client 1.104")
	files[testAsset] = []byte("new binary")

	for _, root := range []bool{false, true} {
		t.Run(fmt.Sprintf("root=%v", root), func(t *testing.T) {
			restarts := installedMesh(t, "daemon 1.102", "client 1.102")
			meshAsRoot = func() bool { return root }
			serveAssets(t, files, nil)
			cli := installedCLI(t)

			var out bytes.Buffer
			if err := updateCLI(&out, "", false, cli, testAsset); err != nil {
				t.Fatal(err)
			}
			assertCLI(t, cli, "new binary")
			if root {
				assertMesh(t, "daemon 1.104", "client 1.104")
				if *restarts != 1 {
					t.Errorf("restarted %d times, want once", *restarts)
				}
				return
			}
			assertMesh(t, "daemon 1.102", "client 1.102")
			if !strings.Contains(out.String(), "sudo meshploy update") {
				t.Errorf("output %q does not say how to bring the mesh along", out.String())
			}
		})
	}
}
