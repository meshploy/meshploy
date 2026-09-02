package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/meshploy/packages/server/version"
	"gorm.io/gorm"
)

const (
	githubReleaseURL = "https://api.github.com/repos/meshploy/meshploy/releases/latest"
	githubMainURL    = "https://api.github.com/repos/meshploy/meshploy/commits/main"
	githubCompareURL = "https://github.com/meshploy/meshploy/compare"
	updateCacheTTL   = time.Hour

	// channelEdge is a build cut from main rather than from a release tag.
	channelEdge = "edge"
)

type VersionInfo struct {
	Current string `json:"current"`
	Channel string `json:"channel"`
	// Latest is the newest release for a stable build, and the short commit at
	// the head of main for an edge one — in both cases, the thing this build
	// would move to.
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

	// An edge build tracks main, so main is what it is measured against. Asking
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

// edgeVersionInfo compares an edge build against the head of main.
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubMainURL, nil)
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

	var commit struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&commit); err != nil || len(commit.SHA) < 7 {
		return info
	}

	head := commit.SHA[:7]
	info.Latest = head
	info.ReleaseURL = fmt.Sprintf("%s/%s...%s", githubCompareURL, running, head)
	info.UpdateAvailable = head != running
	return info
}
