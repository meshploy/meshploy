package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/meshploy/packages/server/version"
)

// fakeGitHub stands in for the product repository's API: two releases (and the
// edge CLI's rolling prerelease, which is not one), a newest build of main, and
// the comparisons the channel view asks for.
type fakeGitHub struct {
	compares map[string]string // "base...head" to the response body
	down     bool
	hits     atomic.Int32
}

const (
	fakeBuilt    = "dddddddeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	fakeReleases = `[
		{"tag_name":"cli-latest","prerelease":true},
		{"tag_name":"v0.11.0","published_at":"2026-09-01T00:00:00Z","html_url":"https://example/v0.11.0"},
		{"tag_name":"v0.10.0","published_at":"2026-08-01T00:00:00Z","html_url":"https://example/v0.10.0"}
	]`
)

// A comparison whose commits GitHub lists oldest first.
func compareBody(status string, shas ...string) string {
	var b strings.Builder
	b.WriteString(`{"status":"` + status + `","total_commits":` + strconv.Itoa(len(shas)) + `,"commits":[`)
	for i, sha := range shas {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"sha":"` + sha + `","commit":{"message":"feat: ` + sha[:3] + `\n\nbody","author":{"date":"2026-09-0` + strconv.Itoa(i+1) + `T00:00:00Z"}}}`)
	}
	b.WriteString("]}")
	return b.String()
}

func serveGitHub(t *testing.T, gh *fakeGitHub) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gh.hits.Add(1)
		if gh.down {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		switch {
		case r.URL.Path == "/releases":
			w.Write([]byte(fakeReleases))
		case r.URL.Path == "/runs":
			w.Write([]byte(`{"workflow_runs":[{"head_sha":"` + fakeBuilt + `"}]}`))
		case strings.HasPrefix(r.URL.Path, "/compare/"):
			body, ok := gh.compares[strings.TrimPrefix(r.URL.Path, "/compare/")]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write([]byte(body))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	origRepo, origRuns := githubRepoAPI, githubBuildRunsURL
	githubRepoAPI, githubBuildRunsURL = srv.URL, srv.URL+"/runs"
	t.Cleanup(func() { githubRepoAPI, githubBuildRunsURL = origRepo, origRuns })
}

func runningBuild(t *testing.T, current, channel string) {
	t.Helper()
	origCur, origCh := version.Current, version.Channel
	version.Current, version.Channel = current, channel
	t.Cleanup(func() { version.Current, version.Channel = origCur, origCh })
}

// A stable server may always move to edge, and is told what main adds.
func TestChannelsFromStable(t *testing.T) {
	runningBuild(t, "0.11.0", "stable")
	serveGitHub(t, &fakeGitHub{compares: map[string]string{
		"v0.11.0..." + fakeBuilt: compareBody("ahead", "aaaaaaa1", "bbbbbbb2", fakeBuilt),
	}})

	ch := (&SystemService{}).GetChannels(t.Context())

	if len(ch.Stable.Releases) != 2 || ch.Stable.Releases[0].Tag != "v0.11.0" {
		t.Errorf("releases = %+v, want v0.11.0 and v0.10.0 without the CLI prerelease", ch.Stable.Releases)
	}
	if ch.Edge.AheadBy != 3 || ch.Edge.Commits[0].SHA != "ddddddd" || ch.Edge.Commits[2].SHA != "aaaaaaa" {
		t.Errorf("edge = %+v, want three commits newest first", ch.Edge)
	}
	if ch.Edge.Head == nil || ch.Edge.Head.Subject != "feat: ddd" {
		t.Errorf("edge head = %+v, want the newest build with its subject", ch.Edge.Head)
	}
	sw := ch.Switch
	if sw == nil || sw.To != "edge" || !sw.Allowed || sw.Total != 3 || len(sw.Changes) != 3 {
		t.Fatalf("switch = %+v, want an allowed move to edge bringing three commits", sw)
	}
	if ch.Unavailable != "" {
		t.Errorf("unavailable = %q", ch.Unavailable)
	}
}

// Edge to stable is allowed only once the latest release contains the build,
// or the server would run older code on a database the newer build migrated.
func TestChannelsFromEdge(t *testing.T) {
	t.Run("the release contains the build", func(t *testing.T) {
		runningBuild(t, "0.10.0+ccccccc", "edge")
		serveGitHub(t, &fakeGitHub{compares: map[string]string{
			"ccccccc...v0.11.0": compareBody("ahead", "eeeeeee1"),
		}})
		sw := (&SystemService{}).GetChannels(t.Context()).Switch
		if sw == nil || sw.To != "stable" || !sw.Allowed || len(sw.Changes) != 1 {
			t.Fatalf("switch = %+v, want an allowed move bringing the release's commit", sw)
		}
	})

	t.Run("the build is ahead of the release", func(t *testing.T) {
		runningBuild(t, "0.11.0+ccccccc", "edge")
		serveGitHub(t, &fakeGitHub{compares: map[string]string{
			"ccccccc...v0.11.0": compareBody("behind"),
		}})
		sw := (&SystemService{}).GetChannels(t.Context()).Switch
		if sw == nil || sw.Allowed || !strings.Contains(sw.Reason, "v0.11.0 does not include this build (ccccccc)") {
			t.Fatalf("switch = %+v, want a refusal naming the release and the build", sw)
		}
	})

	t.Run("a build that recorded no commit", func(t *testing.T) {
		runningBuild(t, "0.11.0", "edge")
		gh := &fakeGitHub{}
		serveGitHub(t, gh)
		err := (&SystemService{}).checkChannelSwitch(t.Context(), "stable")
		if !errors.Is(err, ErrChannelSwitchRefused) || !strings.Contains(err.Error(), "did not record") {
			t.Fatalf("got %v, want a refusal for the missing commit", err)
		}
		if gh.hits.Load() != 0 {
			t.Errorf("asked GitHub %d times with nothing to ask about", gh.hits.Load())
		}
	})
}

// Without GitHub the view says so, and an edge server is not let down to a
// release no one could check. Moving to edge needs no check at all.
func TestChannelsWithoutGitHub(t *testing.T) {
	runningBuild(t, "0.11.0+ccccccc", "edge")
	serveGitHub(t, &fakeGitHub{down: true})
	s := &SystemService{}

	ch := s.GetChannels(t.Context())
	if ch.Unavailable == "" || ch.Switch == nil || ch.Switch.Allowed {
		t.Fatalf("got %+v, want the gap reported and the switch refused", ch)
	}
	if err := s.checkChannelSwitch(t.Context(), "stable"); !errors.Is(err, ErrChannelSwitchRefused) {
		t.Errorf("edge to stable without GitHub: got %v, want a refusal", err)
	}

	runningBuild(t, "0.11.0", "stable")
	if err := s.checkChannelSwitch(t.Context(), "edge"); err != nil {
		t.Errorf("stable to edge must not depend on GitHub: %v", err)
	}
}

func TestCheckChannelSwitch(t *testing.T) {
	gh := &fakeGitHub{}
	serveGitHub(t, gh)
	s := &SystemService{}

	runningBuild(t, "0.11.0", "stable")
	if err := s.checkChannelSwitch(t.Context(), "nightly"); !errors.Is(err, ErrUnknownChannel) {
		t.Errorf("unknown channel: got %v", err)
	}
	if err := s.checkChannelSwitch(t.Context(), "stable"); err != nil {
		t.Errorf("staying on stable: %v", err)
	}

	runningBuild(t, "dev", "dev")
	if err := s.checkChannelSwitch(t.Context(), "edge"); !errors.Is(err, ErrUpgradeDevBuild) {
		t.Errorf("development build: got %v", err)
	}
	if gh.hits.Load() != 0 {
		t.Errorf("asked GitHub %d times for switches that need no check", gh.hits.Load())
	}
}

// GitHub allows 60 requests an hour per IP, so a second look at the view must
// not repeat the first one's requests.
func TestChannelsAreCached(t *testing.T) {
	runningBuild(t, "0.11.0", "stable")
	gh := &fakeGitHub{compares: map[string]string{
		"v0.11.0..." + fakeBuilt: compareBody("ahead", fakeBuilt),
	}}
	serveGitHub(t, gh)
	s := &SystemService{}

	s.GetChannels(t.Context())
	first := gh.hits.Load()
	s.GetChannels(t.Context())
	if again := gh.hits.Load(); again != first {
		t.Errorf("the second view made %d more requests", again-first)
	}
}
