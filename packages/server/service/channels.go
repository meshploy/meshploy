package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/meshploy/packages/server/version"
)

// The two release channels side by side, for the console's channel switch.
//
// Everything here comes from GitHub, which allows 60 unauthenticated requests
// an hour per IP, shared with the sidebar's update check and server-upgrade. So
// the view is built only when someone opens it, the release list and newest
// build are cached for ten minutes, and each comparison is cached by its two
// refs: a comparison between fixed commits never changes.

var (
	ErrUnknownChannel = errors.New("the channel must be stable or edge")
	// ErrChannelSwitchRefused wraps the reason a switch is not allowed now.
	ErrChannelSwitchRefused = errors.New("cannot switch channel")
)

const (
	channelStable = "stable"

	channelsCacheTTL   = 10 * time.Minute
	channelsReleases   = 5
	channelsMaxShown   = 30
	channelsCompareCap = 64
)

// githubRepoAPI is the product repository on GitHub's API. A var so tests can
// point it at a local server.
var githubRepoAPI = "https://api.github.com/repos/meshploy/meshploy"

var githubClient = &http.Client{Timeout: 10 * time.Second}

type ChannelRelease struct {
	Tag         string `json:"tag"`
	PublishedAt string `json:"published_at"`
	URL         string `json:"url"`
}

type ChannelCommit struct {
	SHA     string `json:"sha"` // short
	Subject string `json:"subject"`
	Date    string `json:"date"`
}

type ChannelCurrent struct {
	Version string `json:"version"`
	// Channel is stable or edge, or empty for a development build.
	Channel string `json:"channel"`
	// Commit is the short commit an edge build was cut from; empty otherwise.
	Commit string `json:"commit"`
}

type StableChannel struct {
	// Releases are the newest first.
	Releases []ChannelRelease `json:"releases"`
}

type EdgeChannel struct {
	// Head is the newest commit an edge server can pull: the newest successful
	// image build of main.
	Head *ChannelCommit `json:"head,omitempty"`
	// AheadBy counts the commits on main since the latest release.
	AheadBy int `json:"ahead_by"`
	// Commits are those commits, newest first, at most 30.
	Commits []ChannelCommit `json:"commits"`
}

// ChannelSwitch is the move to the other channel.
type ChannelSwitch struct {
	To      string `json:"to"`
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
	// Changes are the commits the switch brings, newest first, at most 30 of
	// Total.
	Changes []ChannelCommit `json:"changes"`
	Total   int             `json:"total"`
}

type Channels struct {
	Current ChannelCurrent `json:"current"`
	Stable  StableChannel  `json:"stable"`
	Edge    EdgeChannel    `json:"edge"`
	// Switch is absent on a development build, which has no channel to leave.
	Switch *ChannelSwitch `json:"switch,omitempty"`
	// Unavailable says why part of the view is missing, when GitHub could not
	// be reached.
	Unavailable string `json:"unavailable,omitempty"`
}

// channelsCache holds what GetChannels fetched.
type channelsCache struct {
	mu         sync.Mutex
	releases   []ChannelRelease
	releasesAt time.Time
	built      string // full sha of the newest successful build of main
	builtAt    time.Time
	compares   map[string]githubCompare
}

type githubCompare struct {
	Status       string
	TotalCommits int
	Commits      []ChannelCommit // newest first
}

// GetChannels describes both channels, where this server is, and whether it
// may switch to the other one.
func (s *SystemService) GetChannels(ctx context.Context) Channels {
	cur := ChannelCurrent{
		Version: version.Current,
		Channel: upgradeChannel(),
		Commit:  buildCommit(version.Current),
	}
	out := Channels{
		Current: cur,
		Stable:  StableChannel{Releases: []ChannelRelease{}},
		Edge:    EdgeChannel{Commits: []ChannelCommit{}},
	}

	var problems []string
	releases, err := s.channelReleases(ctx)
	if err != nil {
		problems = append(problems, "the release list")
	}
	out.Stable.Releases = releases

	built, err := s.newestBuild(ctx)
	if err != nil {
		problems = append(problems, "the newest build of main")
	}

	var latest string
	if len(releases) > 0 {
		latest = releases[0].Tag
	}
	if latest != "" && built != "" {
		if cmp, err := s.compare(ctx, latest, built); err == nil {
			out.Edge.AheadBy = cmp.TotalCommits
			out.Edge.Commits = firstN(cmp.Commits, channelsMaxShown)
		} else {
			problems = append(problems, "the commits since "+latest)
		}
	}
	if built != "" {
		out.Edge.Head = &ChannelCommit{SHA: short(built)}
		if len(out.Edge.Commits) > 0 && out.Edge.Commits[0].SHA == short(built) {
			out.Edge.Head = &out.Edge.Commits[0]
		}
	}

	if cur.Channel != "" {
		sw := s.channelSwitch(ctx, cur, latest, built)
		out.Switch = &sw
	}
	if len(problems) > 0 {
		out.Unavailable = "GitHub could not be reached for " + strings.Join(problems, ", ")
	}
	return out
}

// channelSwitch works out the move to the other channel. Only forward: main
// contains every release, so stable to edge always is; edge to stable is only
// once the latest release contains the running build, since otherwise the
// server would run older code against a database the newer build migrated.
func (s *SystemService) channelSwitch(ctx context.Context, cur ChannelCurrent, latest, built string) ChannelSwitch {
	sw := ChannelSwitch{Changes: []ChannelCommit{}}

	if cur.Channel == channelStable {
		sw.To, sw.Allowed = channelEdge, true
		if built != "" {
			if cmp, err := s.compare(ctx, "v"+cur.Version, built); err == nil {
				sw.Changes, sw.Total = firstN(cmp.Commits, channelsMaxShown), cmp.TotalCommits
			}
		}
		return sw
	}

	sw.To = channelStable
	switch {
	case cur.Commit == "":
		sw.Reason = "this build did not record the commit it was cut from, so there is no telling whether a release includes it"
	case latest == "":
		sw.Reason = "GitHub could not be reached to check whether the latest release includes this build"
	default:
		// Base the build, head the release: the commits listed are what the
		// release has that this build does not. "behind" or "diverged" means
		// the build has commits the release lacks.
		cmp, err := s.compare(ctx, cur.Commit, latest)
		switch {
		case err != nil:
			sw.Reason = "GitHub could not be reached to check whether " + latest + " includes this build"
		case cmp.Status == "identical" || cmp.Status == "ahead":
			sw.Allowed = true
			sw.Changes, sw.Total = firstN(cmp.Commits, channelsMaxShown), cmp.TotalCommits
		default:
			sw.Reason = fmt.Sprintf("%s does not include this build (%s) yet, so switching now would install older code. It becomes available once a release includes it", latest, cur.Commit)
		}
	}
	return sw
}

// checkChannelSwitch returns nil when this server may move to channel, and the
// reason when it may not.
func (s *SystemService) checkChannelSwitch(ctx context.Context, channel string) error {
	if channel != channelStable && channel != channelEdge {
		return ErrUnknownChannel
	}
	cur := upgradeChannel()
	if cur == "" {
		return ErrUpgradeDevBuild
	}
	// Staying put, or stable to edge, which main always allows.
	if channel == cur || cur == channelStable {
		return nil
	}
	cc := ChannelCurrent{Version: version.Current, Channel: cur, Commit: buildCommit(version.Current)}
	var latest string
	if cc.Commit != "" {
		if releases, err := s.channelReleases(ctx); err == nil && len(releases) > 0 {
			latest = releases[0].Tag
		}
	}
	if sw := s.channelSwitch(ctx, cc, latest, ""); !sw.Allowed {
		return fmt.Errorf("%w to %s: %s", ErrChannelSwitchRefused, channel, sw.Reason)
	}
	return nil
}

func (s *SystemService) channelReleases(ctx context.Context) ([]ChannelRelease, error) {
	c := &s.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.releases != nil && time.Since(c.releasesAt) < channelsCacheTTL {
		return c.releases, nil
	}

	var raw []struct {
		TagName     string `json:"tag_name"`
		PublishedAt string `json:"published_at"`
		HTMLURL     string `json:"html_url"`
		Draft       bool   `json:"draft"`
		Prerelease  bool   `json:"prerelease"`
	}
	// Extra, because the edge CLI's rolling prerelease is listed too.
	url := fmt.Sprintf("%s/releases?per_page=%d", githubRepoAPI, channelsReleases+5)
	if err := githubGet(ctx, url, &raw); err != nil {
		return []ChannelRelease{}, err
	}
	out := []ChannelRelease{}
	for _, r := range raw {
		if r.Draft || r.Prerelease || !strings.HasPrefix(r.TagName, "v") {
			continue
		}
		out = append(out, ChannelRelease{Tag: r.TagName, PublishedAt: r.PublishedAt, URL: r.HTMLURL})
		if len(out) == channelsReleases {
			break
		}
	}
	c.releases, c.releasesAt = out, time.Now()
	return out, nil
}

// newestBuild returns the full commit of the newest successful image build of
// main: what an edge server would pull.
func (s *SystemService) newestBuild(ctx context.Context) (string, error) {
	c := &s.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.built != "" && time.Since(c.builtAt) < channelsCacheTTL {
		return c.built, nil
	}
	var runs struct {
		WorkflowRuns []struct {
			HeadSHA string `json:"head_sha"`
		} `json:"workflow_runs"`
	}
	if err := githubGet(ctx, githubBuildRunsURL, &runs); err != nil {
		return "", err
	}
	if len(runs.WorkflowRuns) == 0 || len(runs.WorkflowRuns[0].HeadSHA) < 7 {
		return "", errors.New("no successful build of main")
	}
	c.built, c.builtAt = runs.WorkflowRuns[0].HeadSHA, time.Now()
	return c.built, nil
}

// compare asks GitHub how head relates to base. The commits are the ones head
// has and base does not.
func (s *SystemService) compare(ctx context.Context, base, head string) (githubCompare, error) {
	key := base + "..." + head
	c := &s.channels
	c.mu.Lock()
	if cmp, ok := c.compares[key]; ok {
		c.mu.Unlock()
		return cmp, nil
	}
	c.mu.Unlock()

	var raw struct {
		Status       string `json:"status"`
		TotalCommits int    `json:"total_commits"`
		Commits      []struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
				Author  struct {
					Date string `json:"date"`
				} `json:"author"`
			} `json:"commit"`
		} `json:"commits"`
	}
	if err := githubGet(ctx, githubRepoAPI+"/compare/"+key, &raw); err != nil {
		return githubCompare{}, err
	}
	cmp := githubCompare{Status: raw.Status, TotalCommits: raw.TotalCommits, Commits: []ChannelCommit{}}
	// GitHub lists them oldest first.
	for i := len(raw.Commits) - 1; i >= 0; i-- {
		rc := raw.Commits[i]
		subject, _, _ := strings.Cut(rc.Commit.Message, "\n")
		cmp.Commits = append(cmp.Commits, ChannelCommit{SHA: short(rc.SHA), Subject: subject, Date: rc.Commit.Author.Date})
	}

	c.mu.Lock()
	if c.compares == nil || len(c.compares) >= channelsCompareCap {
		c.compares = map[string]githubCompare{}
	}
	c.compares[key] = cmp
	c.mu.Unlock()
	return cmp, nil
}

// githubGet fetches one GitHub API resource into v.
func githubGet(ctx context.Context, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := githubClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub answered %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func firstN(commits []ChannelCommit, n int) []ChannelCommit {
	if len(commits) > n {
		return commits[:n]
	}
	return commits
}
