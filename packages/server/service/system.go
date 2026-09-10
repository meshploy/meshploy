package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/version"
	"gorm.io/gorm"
)

const (
	githubReleaseURL = "https://api.github.com/repos/meshploy/meshploy/releases/latest"
	githubCompareURL = "https://github.com/meshploy/meshploy/compare"

	// updateCacheTTL bounds how long a new build or release goes unnoticed. Each
	// refresh is one unauthenticated GitHub request, and GitHub allows 60 an hour
	// per IP: two minutes is at most 30 an hour, and only while someone has the
	// console open, which leaves room for the gateway's other GitHub calls.
	updateCacheTTL = 2 * time.Minute

	// channelEdge is a build cut from main rather than from a release tag.
	channelEdge = "edge"
)

// githubBuildRunsURL asks for the newest successful run of the workflow that
// builds the images. Its commit is the newest one an edge server can pull: a
// commit on main that changed no image never runs that workflow, and one still
// building has no images yet. A var so tests can point it at a local server.
var githubBuildRunsURL = "https://api.github.com/repos/meshploy/meshploy/actions/workflows/build.yml/runs?branch=main&status=success&per_page=1"

type VersionInfo struct {
	Current string `json:"current"`
	Channel string `json:"channel"`
	// Latest is the newest release for a stable build, and the short commit of
	// the newest successful build of main for an edge one. In both cases, the
	// thing this build would move to.
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseURL      string `json:"release_url"`
}

// buildCommit returns the commit an edge build was cut from, taken from the
// "+sha" suffix its version carries. Empty when the build has none — a release
// build, a local one, or an image from before edge builds recorded it.
func buildCommit(current string) string {
	for i := 0; i < len(current); i++ {
		if current[i] == '+' {
			return current[i+1:]
		}
	}
	return ""
}

type SystemService struct {
	mu       sync.Mutex
	cached   *VersionInfo
	cachedAt time.Time
	db       *gorm.DB
	// cfg carries the gateway facts install.sh recorded, including the firewall
	// state the exposure advisory reads. Nil in tests and wherever New() was
	// called without a config.
	cfg *config.Config

	// upgradeMu serialises upgrade requests, so two clicks cannot both pass the
	// "nothing queued" check.
	upgradeMu sync.Mutex
}

func (s *SystemService) Ping(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	return s.db.WithContext(ctx).Exec("SELECT 1").Error
}

func (s *SystemService) GetVersionInfo(ctx context.Context) VersionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cached != nil && time.Since(s.cachedAt) < updateCacheTTL {
		return *s.cached
	}

	info := s.fetchVersionInfo(ctx)
	s.cached = &info
	s.cachedAt = time.Now()
	return info
}

func (s *SystemService) fetchVersionInfo(ctx context.Context) VersionInfo {
	current := version.Current
	info := VersionInfo{
		Current: current,
		Channel: version.Channel,
		Latest:  current,
	}

	// An edge build tracks main, so it is measured against main's newest build. Asking
	// whether a release is newer would be answering a question it did not ask:
	// the moment a release lands the comparison reads as parity, and at the next
	// one as "please upgrade" -- to code the build may already contain.
	if info.Channel == channelEdge {
		return s.edgeVersionInfo(ctx, info)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubReleaseURL, nil)
	if err != nil {
		return info
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("version check: %v", err)
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return info
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return info
	}

	latest := stripV(release.TagName)
	info.Latest = latest
	info.ReleaseURL = release.HTMLURL
	// An edge build tracks main, so releases are not the thing it is behind.
	// It carries the version of the release it was cut after, which means the
	// moment that release is published the comparison reads as parity and then,
	// at the next release, as "please upgrade" — to code the build may already
	// contain. Neither answer is true, so it is not offered: the channel and the
	// commit are shown instead, and "meshploy server-upgrade --edge" is how an
	// edge install moves forward.
	if info.Channel != channelEdge {
		info.UpdateAvailable = isNewer(latest, current)
	}
	return info
}

// stripV removes a leading "v" from a version string ("v0.2.0" → "0.2.0").
func stripV(s string) string {
	if len(s) > 0 && s[0] == 'v' {
		return s[1:]
	}
	return s
}

// isNewer returns true if latest is strictly greater than current using
// simple semver comparison. Both must be in "X.Y.Z" form.
func isNewer(latest, current string) bool {
	if latest == current || current == "dev" {
		return false
	}
	var lMaj, lMin, lPat int
	var cMaj, cMin, cPat int
	if _, err := fmt.Sscanf(latest, "%d.%d.%d", &lMaj, &lMin, &lPat); err != nil {
		return false
	}
	if _, err := fmt.Sscanf(current, "%d.%d.%d", &cMaj, &cMin, &cPat); err != nil {
		return false
	}
	if lMaj != cMaj {
		return lMaj > cMaj
	}
	if lMin != cMin {
		return lMin > cMin
	}
	return lPat > cPat
}

// edgeVersionInfo compares an edge build against the newest successful build
// of main.
//
// Not against the head of main: that moves on every push, including ones that
// change no image (docs, the CLI) and ones whose images are still building, so
// it offered upgrades that pulled nothing new. The build workflow rebuilds the
// API image on every run, so once an upgrade lands, the running commit equals
// the built one and the offer goes away.
//
// Reported as an EDGE update, distinctly from a release: it is unreviewed code
// that has not been cut into a version, and an operator choosing to take it
// should know that is what they are taking.
func (s *SystemService) edgeVersionInfo(ctx context.Context, info VersionInfo) VersionInfo {
	running := buildCommit(info.Current)
	if running == "" {
		// Nothing to compare against — an older edge image that did not record
		// its commit. Claiming either answer would be a guess.
		return info
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubBuildRunsURL, nil)
	if err != nil {
		return info
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("edge version check: %v", err)
		return info
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info
	}

	var runs struct {
		WorkflowRuns []struct {
			HeadSHA string `json:"head_sha"`
		} `json:"workflow_runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&runs); err != nil ||
		len(runs.WorkflowRuns) == 0 || len(runs.WorkflowRuns[0].HeadSHA) < 7 {
		return info
	}

	built := runs.WorkflowRuns[0].HeadSHA[:7]
	info.Latest = built
	info.ReleaseURL = fmt.Sprintf("%s/%s...%s", githubCompareURL, running, built)
	info.UpdateAvailable = built != running
	return info
}
