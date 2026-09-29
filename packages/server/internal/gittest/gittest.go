// Package gittest serves a throwaway git repository over smart HTTP, for tests
// that need a real clone and want to see which credentials it presented.
//
// Smart HTTP (git http-backend), not a static file server: the dumb protocol
// cannot serve a shallow clone, and every clone Meshploy makes is shallow.
package gittest

import (
	"fmt"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Serve starts a server holding one repository, repo.git, with a single commit
// on main. It returns the repository's URL and a function reporting the
// Authorization header of every request so far ("" for none). The test is
// skipped when git is not installed.
func Serve(t testing.TB) (repoURL string, authHeaders func() []string) {
	t.Helper()
	url, seen, _ := serve(t, 1, true)
	return url, seen
}

// ServeHistory is Serve with n commits on main, README reading "version 1"
// to "version n", and returns their hashes, oldest first. byHash says whether
// the server hands out a commit asked for by its hash, as GitHub and GitLab
// do; without it only what a branch reaches is fetched.
func ServeHistory(t testing.TB, n int, byHash bool) (repoURL string, authHeaders func() []string, commits []string) {
	t.Helper()
	return serve(t, n, byHash)
}

func serve(t testing.TB, n int, byHash bool) (repoURL string, authHeaders func() []string, commits []string) {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}

	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "HOME="+root, "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(src, "init", "-q", "-b", "main")
	for i := 1; i <= n; i++ {
		body := "hello\n"
		if n > 1 {
			body = fmt.Sprintf("version %d\n", i)
		}
		if err := os.WriteFile(filepath.Join(src, "README"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		run(src, "add", ".")
		run(src, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", fmt.Sprintf("version %d", i))
		commits = append(commits, run(src, "rev-parse", "HEAD"))
	}
	run(root, "clone", "-q", "--bare", src, filepath.Join(root, "repo.git"))
	if byHash {
		run(filepath.Join(root, "repo.git"), "config", "uploadpack.allowReachableSHA1InWant", "true")
	} else {
		// Refused over protocol v2 too, as a server that does not allow it.
		run(filepath.Join(root, "repo.git"), "config", "uploadpack.allowAnySHA1InWant", "false")
	}

	backend := &cgi.Handler{
		Path: gitBin,
		Args: []string{"http-backend"},
		Env:  []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
	}
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	return srv.URL + "/repo.git", func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}, commits
}
