package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An edge build carries the commit it was cut from as a "+sha" suffix. That
// suffix is the only thing distinguishing it from the release it names, and the
// only thing an edge update check has to compare against.
func TestBuildCommit(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0.8.0+a1b2c3d", "a1b2c3d"},
		{"0.8.0", ""}, // a release build
		{"dev", ""},   // a local build
		{"", ""},      // nothing recorded
	}
	for _, tc := range cases {
		if got := buildCommit(tc.in); got != tc.want {
			t.Errorf("buildCommit(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A stable build compares against releases; the edge path must not be reached
// for it, and an edge build with no recorded commit must not guess.
func TestEdgeCheckNeedsACommit(t *testing.T) {
	s := &SystemService{}
	info := s.edgeVersionInfo(t.Context(), VersionInfo{Current: "0.8.0", Channel: channelEdge})
	if info.UpdateAvailable {
		t.Error("claimed an update with no commit to compare — that is a guess, not a check")
	}
}

// serveBuildRuns stands in for GitHub's workflow-runs listing.
func serveBuildRuns(t *testing.T, status int, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	orig := githubBuildRunsURL
	githubBuildRunsURL = srv.URL
	t.Cleanup(func() { githubBuildRunsURL = orig })
}

// The edge check measures against the newest successful build, which is what
// an upgrade can actually pull, rather than the head of main.
func TestEdgeCheckComparesWithTheNewestBuild(t *testing.T) {
	// Latest starts as the running version, as fetchVersionInfo sets it, so a
	// check that learns nothing leaves it there.
	const running = "0.10.0+6c0a0d1"

	t.Run("running the newest build", func(t *testing.T) {
		serveBuildRuns(t, http.StatusOK, `{"workflow_runs":[{"head_sha":"6c0a0d1f00d"}]}`)
		info := (&SystemService{}).edgeVersionInfo(t.Context(), VersionInfo{Current: running, Channel: channelEdge, Latest: running})
		if info.UpdateAvailable {
			t.Errorf("offered an update to the build already running: %+v", info)
		}
	})

	t.Run("a newer build exists", func(t *testing.T) {
		serveBuildRuns(t, http.StatusOK, `{"workflow_runs":[{"head_sha":"dc82d3dbeef"}]}`)
		info := (&SystemService{}).edgeVersionInfo(t.Context(), VersionInfo{Current: running, Channel: channelEdge, Latest: running})
		if !info.UpdateAvailable || info.Latest != "dc82d3d" {
			t.Fatalf("got %+v, want an update to dc82d3d", info)
		}
		if want := githubCompareURL + "/6c0a0d1...dc82d3d"; info.ReleaseURL != want {
			t.Errorf("ReleaseURL = %q, want %q", info.ReleaseURL, want)
		}
	})

	// No answer is not an answer: none of these may claim an update.
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"no successful build yet": {http.StatusOK, `{"workflow_runs":[]}`},
		"rate limited":            {http.StatusForbidden, `{"message":"API rate limit exceeded"}`},
		"unreadable":              {http.StatusOK, `not json`},
		"short commit":            {http.StatusOK, `{"workflow_runs":[{"head_sha":"abc"}]}`},
	} {
		t.Run(name, func(t *testing.T) {
			serveBuildRuns(t, tc.status, tc.body)
			info := (&SystemService{}).edgeVersionInfo(t.Context(), VersionInfo{Current: running, Channel: channelEdge, Latest: running})
			if info.UpdateAvailable || info.Latest != running {
				t.Errorf("got %+v, want the running build left as it is", info)
			}
		})
	}
}

// Without these filters the check would count builds that failed, that are
// still running, or that ran for a release tag.
func TestEdgeCheckAsksForSuccessfulBuildsOfMain(t *testing.T) {
	for _, want := range []string{"/workflows/build.yml/runs", "branch=main", "status=success", "per_page=1"} {
		if !strings.Contains(githubBuildRunsURL, want) {
			t.Errorf("githubBuildRunsURL %q lacks %q", githubBuildRunsURL, want)
		}
	}
}
