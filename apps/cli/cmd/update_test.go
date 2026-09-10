package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testAsset = "meshploy-linux-amd64"

// serveRelease stands in for GitHub: a latest release carrying the binary and,
// when sums is not empty, a SHA256SUMS asset.
func serveRelease(t *testing.T, binary []byte, sums string) {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+githubRepo+"/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		assets := []map[string]string{{"name": testAsset, "url": srv.URL + "/assets/bin"}}
		if sums != "" {
			assets = append(assets, map[string]string{"name": checksumAsset, "url": srv.URL + "/assets/sums"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"assets": assets})
	})
	asset := func(body []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Without this header GitHub answers with JSON metadata, not the file.
			if r.Header.Get("Accept") != "application/octet-stream" {
				http.Error(w, "want octet-stream", http.StatusUnsupportedMediaType)
				return
			}
			_, _ = w.Write(body)
		}
	}
	mux.HandleFunc("/assets/bin", asset(binary))
	mux.HandleFunc("/assets/sums", asset([]byte(sums)))
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	orig := githubAPI
	githubAPI = srv.URL
	t.Cleanup(func() { githubAPI = orig })
}

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// installedCLI is a binary where os.Executable would point, in a directory
// that must hold nothing else afterwards.
func installedCLI(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meshploy")
	if err := os.WriteFile(path, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertCLI(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("binary = %q (%v), want %q", got, err, want)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("temp files left beside the binary: %v", entries)
	}
}

func TestUpdateCLIVerifiesTheDownload(t *testing.T) {
	bin := []byte("new binary")
	// Both formats sha256sum writes: text mode for one entry, binary for ours.
	serveRelease(t, bin, "0000  meshploy-linux-arm64\n"+sha256hex(bin)+" *"+testAsset+"\n")
	exe := installedCLI(t)

	var out bytes.Buffer
	if err := updateCLI(&out, "", false, exe, testAsset); err != nil {
		t.Fatal(err)
	}
	assertCLI(t, exe, "new binary")
	if fi, _ := os.Stat(exe); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	if !strings.Contains(out.String(), "Verified") {
		t.Errorf("output: %s", out.String())
	}
}

func TestUpdateCLIRefusesADownloadThatDoesNotMatch(t *testing.T) {
	serveRelease(t, []byte("tampered or truncated"), sha256hex([]byte("new binary"))+"  "+testAsset+"\n")
	exe := installedCLI(t)

	err := updateCLI(&bytes.Buffer{}, "", false, exe, testAsset)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("got %v", err)
	}
	assertCLI(t, exe, "old binary")
}

func TestUpdateCLIRefusesWhenTheChecksumsDoNotListIt(t *testing.T) {
	serveRelease(t, []byte("new binary"), "0000  meshploy-linux-arm64\n")
	exe := installedCLI(t)

	err := updateCLI(&bytes.Buffer{}, "", false, exe, testAsset)
	if err == nil || !strings.Contains(err.Error(), "lists no checksum") {
		t.Fatalf("got %v", err)
	}
	assertCLI(t, exe, "old binary")
}

// Releases published before checksums existed have none. Refusing them would
// strand every install on such a release, so the update goes ahead, said.
func TestUpdateCLIWarnsWhenTheReleaseHasNoChecksums(t *testing.T) {
	serveRelease(t, []byte("new binary"), "")
	exe := installedCLI(t)

	var out bytes.Buffer
	if err := updateCLI(&out, "", false, exe, testAsset); err != nil {
		t.Fatal(err)
	}
	assertCLI(t, exe, "new binary")
	if !strings.Contains(out.String(), "cannot be verified") {
		t.Errorf("no warning in: %s", out.String())
	}
}
