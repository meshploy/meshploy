package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSplitImageRefFollowsDockerHubsDefaults(t *testing.T) {
	for ref, want := range map[string][3]string{
		"ghcr.io/meshploy/builder:main": {"ghcr.io", "meshploy/builder", "main"},
		"postgres:18":                   {"registry-1.docker.io", "library/postgres", "18"},
		"redis":                         {"registry-1.docker.io", "library/redis", "latest"},
		"bitnami/redis:7":               {"registry-1.docker.io", "bitnami/redis", "7"},
		"docker.io/library/nginx":       {"registry-1.docker.io", "library/nginx", "latest"},
		"100.64.0.1:5000/shop/web:abc":  {"100.64.0.1:5000", "shop/web", "abc"},
		"ghcr.io/a/b@sha256:0123":       {"ghcr.io", "a/b", "sha256:0123"},
	} {
		h, r, tag := splitImageRef(ref)
		if [3]string{h, r, tag} != want {
			t.Errorf("%s: got %s %s %s, want %v", ref, h, r, tag, want)
		}
	}
}

// A public image's size is read the way a node pulls it: an anonymous token
// when the registry asks for one, then the index, then the node's own
// platform's manifest.
func TestAnImagesDownloadSizeIsItsPlatformsLayers(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if r.URL.Query().Get("scope") != "repository:meshploy/builder:pull" {
				http.Error(w, "scope", http.StatusBadRequest)
				return
			}
			w.Write([]byte(`{"token":"anon"}`))
		case r.Header.Get("Authorization") != "Bearer anon":
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="test",scope="repository:meshploy/builder:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/v2/meshploy/builder/manifests/main":
			w.Write([]byte(`{"manifests":[
				{"digest":"sha256:arm","platform":{"os":"linux","architecture":"arm64"}},
				{"digest":"sha256:amd","platform":{"os":"linux","architecture":"amd64"}},
				{"digest":"sha256:att","platform":{"os":"unknown","architecture":"unknown"}}]}`))
		case r.URL.Path == "/v2/meshploy/builder/manifests/sha256:amd":
			w.Write([]byte(`{"config":{"size":100},"layers":[{"size":1000},{"size":2000}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	size, err := registrySize(t.Context(), srv.Client(), srv.URL, "meshploy/builder", "main", "linux", "amd64")
	if err != nil || size != 3100 {
		t.Fatalf("size %d, err %v; want 3100", size, err)
	}
	if _, err := registrySize(t.Context(), srv.Client(), srv.URL, "meshploy/builder", "main", "linux", "riscv64"); err == nil {
		t.Fatal("a platform the image is not built for has no size")
	}
}

// The log's estimate: bytes received against the image's size, at the rate
// of the last half minute.
func TestAPullsProgressIsEstimatedFromWhatTheNodeReceived(t *testing.T) {
	start := time.Now().Add(-time.Minute)
	w := &pullWatch{image: "ghcr.io/meshploy/builder:main", node: "vps", size: 200 << 20, since: start, pulling: true}
	if w.line() != "Downloading 200 MB" {
		t.Fatalf("before any sample: %q", w.line())
	}
	w.add(start, 1<<30)
	for i := 1; i <= 12; i++ { // a minute at 100 KB/s
		w.add(start.Add(time.Duration(i)*5*time.Second), 1<<30+int64(i)*500<<10)
	}
	got, rate, ok := w.progress()
	if !ok || got != 6000<<10 || rate < 99<<10 || rate > 101<<10 {
		t.Fatalf("got %d at %.0f/s", got, rate)
	}
	line := w.line()
	if !strings.HasPrefix(line, "Downloaded about 6 MB of 200 MB at 100 KB/s, about 3") {
		t.Fatalf("line: %q", line)
	}
	if !w.outlasts(30*time.Minute) || w.outlasts(2*time.Hour) {
		t.Fatal("about 35 minutes in all: past 30 minutes, within two hours")
	}
	if !strings.Contains(slowPullMessage(w), `turn off "Act as build node" on vps`) {
		t.Fatalf("slow message: %q", slowPullMessage(w))
	}

	// Begun well before the watch, what the node received is not the whole
	// download, so no share of it is claimed.
	late := &pullWatch{size: 200 << 20, since: start, partial: true}
	late.add(start, 0)
	late.add(start.Add(10*time.Second), 1000<<10)
	if l := late.line(); !strings.HasPrefix(l, "Downloading 200 MB at 100 KB/s") {
		t.Fatalf("late line: %q", l)
	}
}
