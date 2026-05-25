package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/meshploy/apps/api/internal/version"
)

const (
	githubReleaseURL = "https://api.github.com/repos/meshploy/meshploy/releases/latest"
	updateCacheTTL   = time.Hour
)

type VersionInfo struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseURL      string `json:"release_url"`
}

type SystemService struct {
	mu         sync.Mutex
	cached     *VersionInfo
	cachedAt   time.Time
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
		Latest:  current,
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
	info.UpdateAvailable = isNewer(latest, current)
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
